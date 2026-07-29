package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
	"github.com/DCLee87/family-dashboard/internal/security"
)

const maxScheduleRange = 62 * 24 * time.Hour

type scheduleRequest struct {
	Title          string   `json:"title"`
	LocationName   string   `json:"locationName"`
	Notes          string   `json:"notes"`
	Visibility     string   `json:"visibility"`
	StartsAt       string   `json:"startsAt"`
	EndsAt         string   `json:"endsAt"`
	Participants   []string `json:"participants"`
	Version        int64    `json:"version"`
	ConfirmOverlap bool     `json:"confirmOverlap"`
}

func (a *App) listFamilyMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedDevice(w, r); !ok {
		return
	}
	members, err := a.db.FamilyMembers(r.Context())
	if err != nil {
		a.logger.Error("family member list failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "가족 구성원을 불러오지 못했습니다.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (a *App) createSchedule(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, item, ok := decodeScheduleRequest(w, r)
	if !ok {
		return
	}
	item.ID = security.NewToken()
	overlaps, err := a.db.OverlappingSchedules(r.Context(), item)
	if err != nil {
		a.scheduleInternalError(w, "schedule overlap check failed", err)
		return
	}
	if len(overlaps) > 0 && !request.ConfirmOverlap {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": "overlap_warning", "message": "겹치는 일정이 있습니다.",
			"overlaps": projectSchedules(overlaps, scheduledomain.AudienceParent),
		})
		return
	}
	created, err := a.db.CreateSchedule(r.Context(), item, device.ID, time.Now().UTC())
	if errors.Is(err, database.ErrInvalidParticipant) {
		writeAPIError(w, http.StatusBadRequest, "invalid_participant", "대상 가족을 다시 확인해 주세요.")
		return
	}
	if err != nil {
		a.scheduleInternalError(w, "schedule creation failed", err)
		return
	}
	view, _ := scheduledomain.Project(created, scheduledomain.AudienceParent)
	writeJSON(w, http.StatusCreated, view)
}

func (a *App) updateSchedule(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, item, ok := decodeScheduleRequest(w, r)
	if !ok {
		return
	}
	item.ID = r.PathValue("id")
	if item.Version < 1 {
		writeAPIError(w, http.StatusBadRequest, "version_required", "일정 버전이 필요합니다.")
		return
	}
	overlaps, err := a.db.OverlappingSchedules(r.Context(), item)
	if err != nil {
		a.scheduleInternalError(w, "schedule overlap check failed", err)
		return
	}
	if len(overlaps) > 0 && !request.ConfirmOverlap {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": "overlap_warning", "message": "겹치는 일정이 있습니다.",
			"overlaps": projectSchedules(overlaps, scheduledomain.AudienceParent),
		})
		return
	}
	updated, err := a.db.UpdateSchedule(r.Context(), item, device.ID, time.Now().UTC())
	switch {
	case errors.Is(err, database.ErrScheduleNotFound):
		writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
		return
	case errors.Is(err, database.ErrScheduleConflict):
		writeAPIError(w, http.StatusConflict, "schedule_version_conflict", "다른 기기에서 일정이 변경되었습니다.")
		return
	case errors.Is(err, database.ErrInvalidParticipant):
		writeAPIError(w, http.StatusBadRequest, "invalid_participant", "대상 가족을 다시 확인해 주세요.")
		return
	case err != nil:
		a.scheduleInternalError(w, "schedule update failed", err)
		return
	}
	view, _ := scheduledomain.Project(updated, scheduledomain.AudienceParent)
	writeJSON(w, http.StatusOK, view)
}

func (a *App) getSchedule(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	item, err := a.db.ScheduleByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, database.ErrScheduleNotFound) {
		writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
		return
	}
	if err != nil {
		a.scheduleInternalError(w, "schedule read failed", err)
		return
	}
	view, visible := scheduledomain.Project(item, audienceForDevice(device))
	if !visible {
		writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *App) listScheduleOccurrences(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	from, to, ok := parseScheduleRange(w, r)
	if !ok {
		return
	}
	items, err := a.db.SchedulesBetween(r.Context(), from, to, r.URL.Query().Get("member"))
	if err != nil {
		a.scheduleInternalError(w, "schedule list failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"timezone":    "Asia/Seoul",
		"occurrences": projectSchedules(items, audienceForDevice(device)),
	})
}

func (a *App) familyStatus(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	at := time.Now().UTC()
	if raw := r.URL.Query().Get("at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_time", "기준 시각이 올바르지 않습니다.")
			return
		}
		at = parsed.UTC()
	}
	items, err := a.db.SchedulesBetween(r.Context(), at, at.Add(time.Nanosecond), "")
	if err != nil {
		a.scheduleInternalError(w, "family status schedule query failed", err)
		return
	}
	members, err := a.db.FamilyMembers(r.Context())
	if err != nil {
		a.scheduleInternalError(w, "family status member query failed", err)
		return
	}
	type memberStatus struct {
		Member database.FamilyMember `json:"member"`
		Status any                   `json:"status"`
		Source string                `json:"source"`
	}
	statuses := make([]memberStatus, 0, len(members))
	for _, member := range members {
		view, active := scheduledomain.ActiveStatus(items, member.ID, at, audienceForDevice(device))
		var status any
		if active {
			status = view
		}
		statuses = append(statuses, memberStatus{
			Member: member, Status: status, Source: "일정 기준",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"at": at, "timezone": "Asia/Seoul", "members": statuses,
	})
}

func decodeScheduleRequest(
	w http.ResponseWriter,
	r *http.Request,
) (scheduleRequest, scheduledomain.Item, bool) {
	var request scheduleRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 형식이 올바르지 않습니다.")
		return request, scheduledomain.Item{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 본문은 하나만 허용됩니다.")
		return request, scheduledomain.Item{}, false
	}
	startsAt, startErr := time.Parse(time.RFC3339, request.StartsAt)
	endsAt, endErr := time.Parse(time.RFC3339, request.EndsAt)
	if startErr != nil || endErr != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_time", "시작·종료 시각이 올바르지 않습니다.")
		return request, scheduledomain.Item{}, false
	}
	item := scheduledomain.Item{
		Title: strings.TrimSpace(request.Title), LocationName: strings.TrimSpace(request.LocationName),
		Notes: strings.TrimSpace(request.Notes), Visibility: request.Visibility,
		StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC(),
		Participants: request.Participants, Version: request.Version,
	}
	if err := scheduledomain.Validate(item); err != nil {
		code := "invalid_schedule"
		if errors.Is(err, scheduledomain.ErrInvalidTimeRange) {
			code = "invalid_time_range"
		} else if errors.Is(err, scheduledomain.ErrParticipantsRequired) {
			code = "schedule_participants_required"
		}
		writeAPIError(w, http.StatusBadRequest, code, "일정 입력값을 다시 확인해 주세요.")
		return request, scheduledomain.Item{}, false
	}
	return request, item, true
}

func parseScheduleRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	from, fromErr := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, toErr := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if fromErr != nil || toErr != nil || !to.After(from) {
		writeAPIError(w, http.StatusBadRequest, "invalid_range", "조회 기간이 올바르지 않습니다.")
		return time.Time{}, time.Time{}, false
	}
	if to.Sub(from) > maxScheduleRange {
		writeAPIError(w, http.StatusBadRequest, "schedule_range_too_large", "조회 기간은 62일 이하여야 합니다.")
		return time.Time{}, time.Time{}, false
	}
	return from.UTC(), to.UTC(), true
}

func projectSchedules(items []scheduledomain.Item, audience scheduledomain.Audience) []scheduledomain.View {
	views := make([]scheduledomain.View, 0, len(items))
	for _, item := range items {
		if view, ok := scheduledomain.Project(item, audience); ok {
			views = append(views, view)
		}
	}
	return views
}

func audienceForDevice(device database.Device) scheduledomain.Audience {
	switch device.Type {
	case "trusted_pc", "parent_mobile":
		return scheduledomain.AudienceParent
	case "tv":
		return scheduledomain.AudienceTV
	default:
		return scheduledomain.AudienceShared
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func (a *App) scheduleInternalError(w http.ResponseWriter, message string, err error) {
	a.logger.Error(message, "error", err)
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "일정 요청을 처리하지 못했습니다.")
}
