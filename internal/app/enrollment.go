package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func (a *App) createEnrollment(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r, true); !ok {
		return
	}
	var request struct {
		Type string `json:"type"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
			return
		}
	}
	if request.Type == "" {
		request.Type = "parent_mobile"
	}
	if request.Type != "parent_mobile" && request.Type != "shared_tablet" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	code, err := security.NewDisplayCode(8)
	if err != nil {
		a.logger.Error("enrollment code generation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	now := time.Now().UTC()
	id := security.NewToken()
	expiresAt := now.Add(10 * time.Minute)
	if err := a.db.CreateEnrollment(
		r.Context(),
		id,
		security.TokenHash(code),
		request.Type,
		expiresAt,
		now,
	); err != nil {
		a.logger.Error("enrollment creation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "type": request.Type, "status": "pending", "code": code,
		"expiresAt": expiresAt,
	})
}

func (a *App) submitEnrollment(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	var request struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
		Owner      string `json:"owner"`
		Type       string `json:"type"`
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
	request.Code = strings.TrimSpace(request.Code)
	request.DeviceName = strings.TrimSpace(request.DeviceName)
	if len(request.Code) != 8 ||
		utf8.RuneCountInString(request.DeviceName) < 1 ||
		utf8.RuneCountInString(request.DeviceName) > 80 ||
		(request.Type != "" && request.Type != "parent_mobile" && request.Type != "shared_tablet") ||
		(request.Owner != "" && request.Owner != "dad" && request.Owner != "mom") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	requiredType := request.Type
	if !requestInNetworks(r, a.config.LocalNetworks) {
		if !requestFromTailscale(r) || request.Type != "parent_mobile" {
			writeJSON(w, http.StatusForbidden, map[string]string{"status": "local_network_required"})
			return
		}
	}
	claimToken := security.NewToken()
	enrollment, err := a.db.SubmitEnrollment(
		r.Context(),
		security.TokenHash(request.Code),
		security.TokenHash(claimToken),
		request.DeviceName,
		request.Owner,
		requiredType,
		time.Now().UTC(),
	)
	if errors.Is(err, database.ErrInvalidEnrollment) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_or_expired_code"})
		return
	}
	if err != nil {
		a.logger.Error("enrollment submission failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": enrollment.ID, "status": "submitted", "claimToken": claimToken,
		"expiresAt": enrollment.ExpiresAt,
	})
}

func (a *App) claimEnrollment(w http.ResponseWriter, r *http.Request) {
	if !validSameOrigin(r, a.config.SecureCookies) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
		return
	}
	var request struct {
		ClaimToken string `json:"claimToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
		return
	}
	now := time.Now().UTC()
	claimHash := security.TokenHash(request.ClaimToken)
	enrollment, err := a.db.EnrollmentByClaim(r.Context(), claimHash, now)
	if errors.Is(err, database.ErrInvalidEnrollment) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_or_expired_claim"})
		return
	}
	if err != nil {
		a.logger.Error("enrollment claim lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if enrollment.Status == "submitted" {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "awaiting_approval"})
		return
	}
	if enrollment.Status == "rejected" {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "rejected"})
		return
	}
	if (enrollment.RequestedType == "shared_tablet" || enrollment.RequestedType == "tv") &&
		!requestInNetworks(r, a.config.LocalNetworks) {
		writeJSON(w, http.StatusForbidden, map[string]string{"status": "local_network_required"})
		return
	}
	accessToken := security.NewToken()
	refreshToken := security.NewToken()
	owner := enrollment.Owner.String
	setup := database.InitialSetup{
		DeviceID:         security.NewToken(),
		AccessID:         security.NewToken(),
		AccessHash:       security.TokenHash(accessToken),
		AccessExpiresAt:  now.Add(accessLifetime),
		RefreshID:        security.NewToken(),
		RefreshHash:      security.TokenHash(refreshToken),
		RefreshFamilyID:  security.NewToken(),
		RefreshExpiresAt: now.Add(refreshLifetime),
		Now:              now,
	}
	if err := a.db.CompleteEnrollment(
		r.Context(),
		claimHash,
		security.TokenHash(security.NewToken()),
		setup,
		owner,
	); errors.Is(err, database.ErrInvalidEnrollment) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "invalid_or_expired_claim"})
		return
	} else if err != nil {
		a.logger.Error("enrollment completion failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	setDeviceCookies(w, accessToken, refreshToken, a.config.SecureCookies)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

func (a *App) listEnrollments(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r, false); !ok {
		return
	}
	items, err := a.db.ListEnrollments(r.Context(), time.Now().UTC())
	if err != nil {
		a.logger.Error("enrollment list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id": item.ID, "type": item.RequestedType, "name": item.DeviceName.String,
			"owner": item.Owner.String, "status": item.Status, "expiresAt": item.ExpiresAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"enrollments": result})
}

func (a *App) approveEnrollment(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireAdmin(w, r, true)
	if !ok {
		return
	}
	if err := a.db.ApproveEnrollment(
		r.Context(),
		r.PathValue("id"),
		device.ID,
		time.Now().UTC(),
	); errors.Is(err, database.ErrInvalidEnrollment) {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "not_approvable"})
		return
	} else if err != nil {
		a.logger.Error("enrollment approval failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (a *App) rejectEnrollment(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireAdmin(w, r, true)
	if !ok {
		return
	}
	if err := a.db.RejectEnrollment(
		r.Context(),
		r.PathValue("id"),
		device.ID,
		time.Now().UTC(),
	); errors.Is(err, database.ErrInvalidEnrollment) {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "not_rejectable"})
		return
	} else if err != nil {
		a.logger.Error("enrollment rejection failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (a *App) listDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r, false); !ok {
		return
	}
	items, err := a.db.ListDevices(r.Context())
	if err != nil {
		a.logger.Error("device list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		var lastUsed any
		if item.LastUsedAt.Valid {
			lastUsed = item.LastUsedAt.Time
		}
		result = append(result, map[string]any{
			"id": item.ID, "name": item.Name, "type": item.Type,
			"owner": item.Owner.String, "status": item.Status,
			"createdAt": item.CreatedAt, "lastUsedAt": lastUsed,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": result})
}

func (a *App) revokeDevice(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r, true)
	if !ok {
		return
	}
	if err := a.db.RevokeDevice(
		r.Context(),
		r.PathValue("id"),
		actor.ID,
		time.Now().UTC(),
	); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "not_revokable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func setDeviceCookies(w http.ResponseWriter, access, refresh string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: "family_dashboard_device", Value: access, Path: "/",
		MaxAge: int(accessLifetime.Seconds()), HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "family_dashboard_refresh", Value: refresh, Path: "/api/auth",
		MaxAge: int(refreshLifetime.Seconds()), HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}
