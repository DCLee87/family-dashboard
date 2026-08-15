package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func (a *App) loginParent(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	var request struct {
		Owner      string `json:"owner"`
		Password   string `json:"password"`
		DeviceName string `json:"deviceName"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	request.Owner = strings.TrimSpace(request.Owner)
	request.DeviceName = strings.TrimSpace(request.DeviceName)
	if (request.Owner != "dad" && request.Owner != "mom") || !security.ValidPassword(request.Password) || utf8.RuneCountInString(request.DeviceName) < 1 || utf8.RuneCountInString(request.DeviceName) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	now := time.Now().UTC()
	remote := requestRemoteAddress(r)
	account, err := a.db.ParentAccountForLogin(r.Context(), request.Owner, now)
	if errors.Is(err, database.ErrParentLoginBlocked) {
		_ = a.db.RecordParentLoginBlocked(r.Context(), request.Owner, remote, now)
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"status": "temporarily_blocked"})
		return
	}
	if errors.Is(err, database.ErrUnauthenticated) {
		_ = a.db.RecordParentLoginFailure(r.Context(), request.Owner, remote, now)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_credentials"})
		return
	}
	if err != nil {
		a.logger.Error("parent account lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	valid, err := security.VerifyPassword(account.PasswordHash, request.Password)
	if err != nil {
		a.logger.Error("parent password verification failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if !valid {
		if err := a.db.RecordParentLoginFailure(r.Context(), request.Owner, remote, now); err != nil {
			a.logger.Error("parent login failure recording failed", "error", err)
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_credentials"})
		return
	}
	accessToken := security.NewToken()
	refreshToken := security.NewToken()
	login := database.ParentLoginDevice{Owner: request.Owner, PasswordHash: account.PasswordHash, DeviceID: security.NewToken(), DeviceName: request.DeviceName, AccessID: security.NewToken(), AccessHash: security.TokenHash(accessToken), AccessExpiresAt: now.Add(accessLifetime), RefreshID: security.NewToken(), RefreshHash: security.TokenHash(refreshToken), RefreshFamilyID: security.NewToken(), RefreshExpiresAt: now.Add(refreshLifetime), RemoteAddress: remote, Now: now}
	if err := a.db.CreateParentLoginDevice(r.Context(), login); err != nil {
		if errors.Is(err, database.ErrUnauthenticated) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_credentials"})
			return
		}
		a.logger.Error("parent login device creation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	setDeviceCookies(w, accessToken, refreshToken, a.config.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated", "owner": request.Owner})
}

func (a *App) logoutDevice(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	if err := a.db.LogoutDevice(r.Context(), device.ID, time.Now().UTC()); err != nil {
		a.logger.Error("device logout failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	clearDeviceCookies(w, a.config.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *App) listParentAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireDeviceAdmin(w, r, false); !ok {
		return
	}
	statuses, err := a.db.ParentAccountStatuses(r.Context())
	if err != nil {
		a.logger.Error("parent account list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": statuses})
}

func (a *App) setParentPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireDeviceAdmin(w, r, true); !ok {
		return
	}
	owner := r.PathValue("owner")
	if owner != "dad" && owner != "mom" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	hash, err := security.HashPassword(request.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_password"})
		return
	}
	if err = a.db.SetParentPassword(r.Context(), owner, hash, time.Now().UTC()); err != nil {
		a.logger.Error("parent password update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "configured"})
}

func (a *App) disableParentAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireDeviceAdmin(w, r, true); !ok {
		return
	}
	if err := a.db.DisableParentAccount(r.Context(), r.PathValue("owner"), time.Now().UTC()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func requestRemoteAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(host) > 64 {
		return ""
	}
	return host
}
