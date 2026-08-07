package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

const (
	adminIdleLifetime     = 10 * time.Minute
	adminAbsoluteLifetime = time.Hour
)

func (a *App) unlockAdmin(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	if device.Type == "tv" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	var request struct {
		PIN string `json:"pin"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}

	now := time.Now().UTC()
	pinHash, err := a.db.PINChallenge(r.Context(), device.ID, now)
	if errors.Is(err, database.ErrPINBlocked) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"status": "temporarily_blocked"})
		return
	}
	if err != nil {
		a.logger.Error("PIN challenge failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	valid, err := security.VerifyPIN(pinHash, request.PIN)
	if err != nil {
		a.logger.Error("PIN verification failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if !valid {
		blocked, err := a.db.RecordPINFailure(r.Context(), device.ID, now)
		if err != nil {
			a.logger.Error("PIN failure recording failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}
		if blocked {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"status": "temporarily_blocked"})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_pin"})
		return
	}

	sessionToken := security.NewToken()
	csrfToken := security.NewToken()
	if err := a.db.CreateAdminSession(
		r.Context(),
		device.ID,
		security.NewToken(),
		security.TokenHash(sessionToken),
		security.TokenHash(csrfToken),
		now.Add(adminIdleLifetime),
		now.Add(adminAbsoluteLifetime),
		now,
	); err != nil {
		a.logger.Error("admin session creation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	setAdminCookies(w, sessionToken, csrfToken, a.config.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "unlocked",
		"idleExpiresAt":     now.Add(adminIdleLifetime),
		"absoluteExpiresAt": now.Add(adminAbsoluteLifetime),
	})
}

func (a *App) lockAdmin(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	sessionCookie, sessionHash, ok := adminSessionCookie(r)
	if !ok {
		clearAdminCookies(w, a.config.SecureCookies)
		writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
		return
	}
	csrf := r.Header.Get("X-CSRF-Token")
	csrfHash := security.TokenHash(csrf)
	valid, err := a.db.ValidateAdminSession(
		r.Context(),
		device.ID,
		sessionHash,
		&csrfHash,
		time.Now().UTC(),
		true,
	)
	if err != nil {
		a.logger.Error("admin lock validation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if !valid || csrf == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	if err := a.db.RevokeAdminSession(
		r.Context(),
		device.ID,
		security.TokenHash(sessionCookie.Value),
		time.Now().UTC(),
	); err != nil {
		a.logger.Error("admin lock failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	clearAdminCookies(w, a.config.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
}

func (a *App) authenticatedDevice(w http.ResponseWriter, r *http.Request) (database.Device, bool) {
	cookie, err := r.Cookie("family_dashboard_device")
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return database.Device{}, false
	}
	device, err := a.db.DeviceByAccessToken(
		r.Context(),
		security.TokenHash(cookie.Value),
		time.Now().UTC(),
	)
	if err != nil {
		if !errors.Is(err, database.ErrUnauthenticated) {
			a.logger.Error("device authentication failed", "error", err)
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return database.Device{}, false
	}
	if device.LocalOnly && !requestInNetworks(r, a.config.LocalNetworks) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return database.Device{}, false
	}
	return device, true
}

func (a *App) adminSessionActive(r *http.Request, deviceID string) bool {
	_, hash, ok := adminSessionCookie(r)
	if !ok {
		return false
	}
	active, err := a.db.ValidateAdminSession(r.Context(), deviceID, hash, nil, time.Now().UTC(), false)
	if err != nil {
		a.logger.Error("admin session validation failed", "error", err)
		return false
	}
	return active
}

func (a *App) requireAdmin(
	w http.ResponseWriter,
	r *http.Request,
	stateChange bool,
) (database.Device, bool) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return database.Device{}, false
	}
	if device.Type != "trusted_pc" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	_, sessionHash, ok := adminSessionCookie(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	var csrfHash *[32]byte
	if stateChange {
		if !validSameOrigin(r, a.config.SecureCookies) {
			writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
			return database.Device{}, false
		}
		csrf := r.Header.Get("X-CSRF-Token")
		if csrf == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
			return database.Device{}, false
		}
		hash := security.TokenHash(csrf)
		csrfHash = &hash
	}
	active, err := a.db.ValidateAdminSession(
		r.Context(),
		device.ID,
		sessionHash,
		csrfHash,
		time.Now().UTC(),
		stateChange,
	)
	if err != nil {
		a.logger.Error("administrator authorization failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return database.Device{}, false
	}
	if !active {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	return device, true
}

func (a *App) requireContentAdmin(
	w http.ResponseWriter,
	r *http.Request,
) (database.Device, bool) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return database.Device{}, false
	}
	if device.Type == "tv" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	_, sessionHash, ok := adminSessionCookie(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	csrf := r.Header.Get("X-CSRF-Token")
	if csrf == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	csrfHash := security.TokenHash(csrf)
	active, err := a.db.ValidateAdminSession(
		r.Context(), device.ID, sessionHash, &csrfHash,
		time.Now().UTC(), true,
	)
	if err != nil {
		a.logger.Error("content administrator authorization failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return database.Device{}, false
	}
	if !active {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	return device, true
}

func (a *App) requireContentAdminRead(
	w http.ResponseWriter,
	r *http.Request,
) (database.Device, bool) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return database.Device{}, false
	}
	if device.Type == "tv" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return database.Device{}, false
	}
	if !a.adminSessionActive(r, device.ID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "admin_required"})
		return database.Device{}, false
	}
	return device, true
}

func adminSessionCookie(r *http.Request) (*http.Cookie, [32]byte, bool) {
	cookie, err := r.Cookie("family_dashboard_admin")
	if err != nil || cookie.Value == "" {
		return nil, [32]byte{}, false
	}
	return cookie, security.TokenHash(cookie.Value), true
}

func setAdminCookies(w http.ResponseWriter, session, csrf string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: "family_dashboard_admin", Value: session, Path: "/",
		MaxAge: int(adminAbsoluteLifetime.Seconds()), HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "family_dashboard_csrf", Value: csrf, Path: "/",
		MaxAge: int(adminAbsoluteLifetime.Seconds()), HttpOnly: false,
		Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

func clearAdminCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{"family_dashboard_admin", "family_dashboard_csrf"} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: name == "family_dashboard_admin",
			Secure:   secure, SameSite: http.SameSiteStrictMode,
		})
	}
}

func validSameOrigin(r *http.Request, secure bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host != r.Host {
		return false
	}
	expectedScheme := "http"
	if secure {
		expectedScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, expectedScheme)
}
