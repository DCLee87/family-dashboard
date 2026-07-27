package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func (a *App) refreshDevice(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	cookie, err := r.Cookie("family_dashboard_refresh")
	if err != nil || cookie.Value == "" {
		clearDeviceCookies(w, a.config.SecureCookies)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return
	}
	now := time.Now().UTC()
	accessToken := security.NewToken()
	refreshToken := security.NewToken()
	err = a.db.RotateRefreshCredential(
		r.Context(),
		security.TokenHash(cookie.Value),
		database.CredentialRotation{
			AccessID:         security.NewToken(),
			AccessHash:       security.TokenHash(accessToken),
			AccessExpiresAt:  now.Add(accessLifetime),
			RefreshID:        security.NewToken(),
			RefreshHash:      security.TokenHash(refreshToken),
			RefreshExpiresAt: now.Add(refreshLifetime),
		},
		now,
	)
	if errors.Is(err, database.ErrUnauthenticated) ||
		errors.Is(err, database.ErrRefreshReuse) {
		clearDeviceCookies(w, a.config.SecureCookies)
		status := "unauthorized"
		if errors.Is(err, database.ErrRefreshReuse) {
			status = "refresh_reuse_detected"
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": status})
		return
	}
	if err != nil {
		a.logger.Error("device credential refresh failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	setDeviceCookies(w, accessToken, refreshToken, a.config.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func clearDeviceCookies(w http.ResponseWriter, secure bool) {
	for _, cookie := range []struct {
		name string
		path string
	}{
		{"family_dashboard_device", "/"},
		{"family_dashboard_refresh", "/api/auth"},
	} {
		http.SetCookie(w, &http.Cookie{
			Name: cookie.name, Value: "", Path: cookie.path, MaxAge: -1,
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
		})
	}
	clearAdminCookies(w, secure)
}
