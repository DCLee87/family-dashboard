package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	maxPushEndpointLength = 2048
	maxPushKeyLength      = 256
)

func (a *App) pushConfig(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	eligible := pushEligibleDevice(device)
	subscribed := false
	if eligible {
		var err error
		subscribed, err = a.db.PushSubscriptionActive(r.Context(), device.ID)
		if err != nil {
			a.logger.Error("push subscription state failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}
	}
	enabled := eligible && validVAPIDPublicKey(a.config.VAPIDPublicKey)
	publicKey := ""
	if enabled {
		publicKey = a.config.VAPIDPublicKey
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled, "eligible": eligible, "subscribed": subscribed,
		"publicKey": publicKey,
	})
}

func (a *App) replacePushSubscription(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	if !pushEligibleDevice(device) || !validVAPIDPublicKey(a.config.VAPIDPublicKey) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "push_unavailable"})
		return
	}
	var request struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	request.Endpoint = strings.TrimSpace(request.Endpoint)
	if !validPushEndpoint(request.Endpoint) ||
		!validPushKey(request.Keys.P256DH, 65) ||
		!validPushKey(request.Keys.Auth, 16) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_subscription"})
		return
	}
	if err := a.db.ReplacePushSubscription(r.Context(), database.PushSubscription{
		ID:           security.NewToken(),
		DeviceID:     device.ID,
		Endpoint:     request.Endpoint,
		EndpointHash: sha256.Sum256([]byte(request.Endpoint)),
		P256DH:       request.Keys.P256DH,
		Auth:         request.Keys.Auth,
		Now:          time.Now().UTC(),
	}); err != nil {
		a.logger.Error("push subscription replacement failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

func (a *App) revokePushSubscription(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	if !pushEligibleDevice(device) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "push_unavailable"})
		return
	}
	if err := a.db.RevokePushSubscription(r.Context(), device.ID, time.Now().UTC()); err != nil {
		a.logger.Error("push subscription revocation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

func (a *App) sendTestPush(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requirePushAdmin(w, r)
	if !ok {
		return
	}
	subscription, err := a.db.ActivePushSubscription(r.Context(), device.ID)
	if errors.Is(err, database.ErrUnauthenticated) {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "not_subscribed"})
		return
	}
	if err != nil {
		a.logger.Error("test push subscription lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	payload, err := json.Marshal(map[string]string{
		"title": "우리 가족 대시보드",
		"body":  "테스트 알림이 정상 연결되었습니다.",
		"path":  "/",
		"tag":   "e3-web-push-test",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	response, err := webpush.SendNotificationWithContext(
		r.Context(),
		payload,
		&webpush.Subscription{
			Endpoint: subscription.Endpoint,
			Keys: webpush.Keys{
				P256dh: subscription.P256DH,
				Auth:   subscription.Auth,
			},
		},
		&webpush.Options{
			HTTPClient:      a.pushClient,
			Subscriber:      "https://family-dashboard.local",
			TTL:             60,
			Topic:           "e3-web-push-test",
			VAPIDPublicKey:  a.config.VAPIDPublicKey,
			VAPIDPrivateKey: a.config.VAPIDPrivateKey,
		},
	)
	if err != nil {
		a.logger.Error("test push delivery failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"status": "delivery_failed"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		if err := a.db.RevokePushSubscription(r.Context(), device.ID, time.Now().UTC()); err != nil {
			a.logger.Error("expired push subscription revocation failed", "error", err)
		}
		writeJSON(w, http.StatusGone, map[string]string{"status": "subscription_expired"})
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		a.logger.Warn("test push service rejected request", "status", response.StatusCode)
		writeJSON(w, http.StatusBadGateway, map[string]string{"status": "delivery_rejected"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (a *App) requirePushAdmin(
	w http.ResponseWriter,
	r *http.Request,
) (database.Device, bool) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return database.Device{}, false
	}
	if !pushEligibleDevice(device) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "push_unavailable"})
		return database.Device{}, false
	}
	_, sessionHash, ok := adminSessionCookie(r)
	csrf := r.Header.Get("X-CSRF-Token")
	if !ok || csrf == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	csrfHash := security.TokenHash(csrf)
	active, err := a.db.ValidateAdminSession(
		r.Context(),
		device.ID,
		sessionHash,
		&csrfHash,
		time.Now().UTC(),
		true,
	)
	if err != nil {
		a.logger.Error("test push administrator authorization failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return database.Device{}, false
	}
	if !active {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	return device, true
}

func pushEligibleDevice(device database.Device) bool {
	return device.Type == "parent_mobile" || device.Type == "shared_tablet"
}

func validVAPIDPublicKey(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 65 && decoded[0] == 4
}

func validPushKey(value string, expectedLength int) bool {
	if len(value) == 0 || len(value) > maxPushKeyLength {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == expectedLength
}

func validPushEndpoint(value string) bool {
	if len(value) == 0 || len(value) > maxPushEndpointLength {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" &&
		parsed.User == nil && parsed.Fragment == ""
}
