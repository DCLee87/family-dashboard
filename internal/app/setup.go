package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

const (
	accessLifetime  = 30 * 24 * time.Hour
	refreshLifetime = 90 * 24 * time.Hour
)

func (a *App) setupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := a.db.SetupRequired(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if required {
		if err := a.ensureInitialSetupCode(r.Context()); err != nil {
			a.logger.Error("initial setup code refresh failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setupRequired": required})
}

func (a *App) currentDevice(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("family_dashboard_device")
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return
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
		return
	}
	if device.LocalOnly && !requestInNetworks(r, a.config.LocalNetworks) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return
	}
	owner := ""
	if device.Owner.Valid {
		owner = device.Owner.String
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "authenticated",
		"device": map[string]any{
			"id":        device.ID,
			"name":      device.Name,
			"type":      device.Type,
			"owner":     owner,
			"localOnly": device.LocalOnly,
		},
		"permissions": map[string]bool{
			"view":           true,
			"admin":          false,
			"canUnlockAdmin": device.Type != "tv",
		},
	})
}

func (a *App) completeSetup(w http.ResponseWriter, r *http.Request) {
	if !requestInNetworks(r, a.config.LocalNetworks) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}

	var request struct {
		InitialSetupCode string `json:"initialSetupCode"`
		PIN              string `json:"pin"`
		DeviceName       string `json:"deviceName"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	request.DeviceName = strings.TrimSpace(request.DeviceName)
	if utf8.RuneCountInString(request.DeviceName) < 1 ||
		utf8.RuneCountInString(request.DeviceName) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	now := time.Now().UTC()
	codeHash := security.TokenHash(request.InitialSetupCode)
	validCode, err := a.db.ValidateInitialSetupCode(r.Context(), codeHash, now)
	if err != nil {
		a.logger.Error("initial setup code validation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if !validCode {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"status": "invalid_or_expired_code",
		})
		return
	}
	pinHash, err := security.HashPIN(request.PIN)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}

	accessToken := security.NewToken()
	refreshToken := security.NewToken()
	recoveryCode := security.NewToken()
	deviceID := security.NewToken()
	refreshFamilyID := security.NewToken()
	setup := database.InitialSetup{
		CodeHash:         codeHash,
		PINHash:          pinHash,
		DeviceID:         deviceID,
		DeviceName:       request.DeviceName,
		AccessID:         security.NewToken(),
		AccessHash:       security.TokenHash(accessToken),
		AccessExpiresAt:  now.Add(accessLifetime),
		RefreshID:        security.NewToken(),
		RefreshHash:      security.TokenHash(refreshToken),
		RefreshFamilyID:  refreshFamilyID,
		RefreshExpiresAt: now.Add(refreshLifetime),
		RecoveryID:       security.NewToken(),
		RecoveryHash:     security.TokenHash(recoveryCode),
		Now:              now,
	}
	if err := a.db.CompleteInitialSetup(r.Context(), setup); err != nil {
		if errors.Is(err, database.ErrInvalidInitialSetup) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"status": "invalid_or_expired_code",
			})
			return
		}
		a.logger.Error("initial setup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "family_dashboard_device",
		Value:    accessToken,
		Path:     "/",
		MaxAge:   int(accessLifetime.Seconds()),
		HttpOnly: true,
		Secure:   a.config.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "family_dashboard_refresh",
		Value:    refreshToken,
		Path:     "/api/auth",
		MaxAge:   int(refreshLifetime.Seconds()),
		HttpOnly: true,
		Secure:   a.config.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"status":       "configured",
		"recoveryCode": recoveryCode,
		"device": map[string]string{
			"id":   deviceID,
			"name": request.DeviceName,
			"type": "trusted_pc",
		},
	})
}

func requestInNetworks(r *http.Request, networks []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	for _, network := range networks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
