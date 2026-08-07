package app

import (
	"context"
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
	Title             string                   `json:"title"`
	LocationName      string                   `json:"locationName"`
	Notes             string                   `json:"notes"`
	Visibility        string                   `json:"visibility"`
	StartsAt          string                   `json:"startsAt"`
	EndsAt            string                   `json:"endsAt"`
	Participants      []string                 `json:"participants"`
	Version           int64                    `json:"version"`
	OccurrenceVersion int64                    `json:"occurrenceVersion"`
	ConfirmOverlap    bool                     `json:"confirmOverlap"`
	Recurrence        *weeklyRecurrenceRequest `json:"recurrence,omitempty"`
	TimeKind          string                   `json:"timeKind,omitempty"`
	StartDate         string                   `json:"startDate,omitempty"`
	EndDate           string                   `json:"endDate,omitempty"`
}

type scheduleVersionRequest struct {
	Version int64 `json:"version"`
}

func (a *App) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeScheduleVersionRequest(w, r)
	if !ok {
		return
	}
	err := a.db.TrashSchedule(r.Context(), r.PathValue("id"), request.Version, device.ID, time.Now().UTC())
	switch {
	case errors.Is(err, database.ErrScheduleNotFound):
		writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
	case errors.Is(err, database.ErrScheduleConflict):
		writeAPIError(w, http.StatusConflict, "schedule_version_conflict", "다른 기기에서 일정이 변경되었습니다.")
	case err != nil:
		a.scheduleInternalError(w, "schedule deletion failed", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) listScheduleTrash(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdminRead(w, r); !ok {
		return
	}
	entries, err := a.db.TrashedSchedules(r.Context())
	if err != nil {
		a.scheduleInternalError(w, "schedule trash list failed", err)
		return
	}
	type trashView struct {
		Schedule     scheduledomain.View `json:"schedule"`
		DeletedAt    time.Time           `json:"deletedAt"`
		RestoreUntil time.Time           `json:"restoreUntil"`
	}
	views := make([]trashView, 0, len(entries))
	for _, entry := range entries {
		view, _ := scheduledomain.Project(entry.Item, scheduledomain.AudienceParent)
		views = append(views, trashView{
			Schedule: view, DeletedAt: entry.DeletedAt,
			RestoreUntil: entry.DeletedAt.Add(database.ScheduleTrashRetention),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": views})
}

func (a *App) restoreSchedule(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeScheduleVersionRequest(w, r)
	if !ok {
		return
	}
	item, err := a.db.RestoreSchedule(r.Context(), r.PathValue("id"), request.Version, device.ID, time.Now().UTC())
	switch {
	case errors.Is(err, database.ErrScheduleNotFound):
		writeAPIError(w, http.StatusNotFound, "trashed_schedule_not_found", "휴지통 일정을 찾을 수 없습니다.")
	case errors.Is(err, database.ErrScheduleConflict):
		writeAPIError(w, http.StatusConflict, "schedule_version_conflict", "다른 기기에서 일정이 변경되었습니다.")
	case err != nil:
		a.scheduleInternalError(w, "schedule restore failed", err)
	default:
		view, _ := scheduledomain.Project(item, scheduledomain.AudienceParent)
		writeJSON(w, http.StatusOK, view)
	}
}

func (a *App) permanentlyDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	request, ok := decodeScheduleVersionRequest(w, r)
	if !ok {
		return
	}
	err := a.db.PermanentlyDeleteSchedule(r.Context(), r.PathValue("id"), request.Version)
	switch {
	case errors.Is(err, database.ErrScheduleNotFound):
		writeAPIError(w, http.StatusNotFound, "trashed_schedule_not_found", "휴지통 일정을 찾을 수 없습니다.")
	case errors.Is(err, database.ErrScheduleConflict):
		writeAPIError(w, http.StatusConflict, "schedule_version_conflict", "다른 기기에서 일정이 변경되었습니다.")
	case err != nil:
		a.scheduleInternalError(w, "schedule permanent deletion failed", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) updateScheduleOccurrence(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, item, ok := decodeScheduleRequest(w, r)
	if !ok {
		return
	}
	original, err := a.recurringOccurrence(r.Context(), r.PathValue("id"), r.PathValue("key"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "occurrence_not_found", "반복 일정 회차를 찾을 수 없습니다.")
		return
	}
	item.ID, item.Participants = original.ID, original.Participants
	overlaps, err := a.overlappingOccurrence(r.Context(), item, original.OccurrenceKey)
	if err != nil {
		a.scheduleInternalError(w, "occurrence overlap check failed", err)
		return
	}
	if len(overlaps) > 0 && !request.ConfirmOverlap {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": "overlap_warning", "message": "겹치는 일정이 있습니다.",
			"overlaps": projectSchedules(overlaps, scheduledomain.AudienceParent),
		})
		return
	}
	version, err := a.db.SaveOccurrenceException(r.Context(), original.ID, scheduledomain.OccurrenceException{
		OccurrenceKey: original.OccurrenceKey, Override: &item,
	}, request.OccurrenceVersion, time.Now().UTC())
	if errors.Is(err, database.ErrOccurrenceConflict) {
		writeAPIError(w, http.StatusConflict, "occurrence_version_conflict", "다른 기기에서 이 회차가 변경되었습니다.")
		return
	}
	if err != nil {
		a.scheduleInternalError(w, "occurrence update failed", err)
		return
	}
	item.OccurrenceKey, item.OccurrenceVersion = original.OccurrenceKey, version
	view, _ := scheduledomain.Project(item, scheduledomain.AudienceParent)
	writeJSON(w, http.StatusOK, view)
}

func (a *App) cancelScheduleOccurrence(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var request struct {
		OccurrenceVersion int64 `json:"occurrenceVersion"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 형식이 올바르지 않습니다.")
		return
	}
	original, err := a.recurringOccurrence(r.Context(), r.PathValue("id"), r.PathValue("key"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "occurrence_not_found", "반복 일정 회차를 찾을 수 없습니다.")
		return
	}
	_, err = a.db.SaveOccurrenceException(r.Context(), original.ID, scheduledomain.OccurrenceException{
		OccurrenceKey: original.OccurrenceKey, Cancelled: true,
	}, request.OccurrenceVersion, time.Now().UTC())
	if errors.Is(err, database.ErrOccurrenceConflict) {
		writeAPIError(w, http.StatusConflict, "occurrence_version_conflict", "다른 기기에서 이 회차가 변경되었습니다.")
		return
	}
	if err != nil {
		a.scheduleInternalError(w, "occurrence cancellation failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) recurringOccurrence(ctx context.Context, scheduleID, key string) (scheduledomain.Item, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return scheduledomain.Item{}, err
	}
	originalStart, err := time.ParseInLocation("2006-01-02T15:04", key, location)
	if err != nil {
		return scheduledomain.Item{}, scheduledomain.ErrInvalidOccurrence
	}
	rules, err := a.db.WeeklyRules(ctx)
	if err != nil {
		return scheduledomain.Item{}, err
	}
	for _, rule := range rules {
		if rule.Item.ID != scheduleID {
			continue
		}
		occurrences, err := scheduledomain.ExpandWeekly(rule, originalStart.UTC(), originalStart.Add(24*time.Hour).UTC(), location, nil)
		if err != nil {
			return scheduledomain.Item{}, err
		}
		for _, occurrence := range occurrences {
			if occurrence.OccurrenceKey == key {
				return occurrence.Item, nil
			}
		}
	}
	return scheduledomain.Item{}, scheduledomain.ErrInvalidOccurrence
}

func (a *App) overlappingOccurrence(ctx context.Context, candidate scheduledomain.Item, originalKey string) ([]scheduledomain.Item, error) {
	items, err := a.db.SchedulesBetween(ctx, candidate.StartsAt, candidate.EndsAt, "")
	if err != nil {
		return nil, err
	}
	var overlaps []scheduledomain.Item
	for _, item := range items {
		if item.ID == candidate.ID && item.OccurrenceKey == originalKey {
			continue
		}
		if scheduledomain.Overlap(candidate, item) {
			overlaps = append(overlaps, item)
		}
	}
	return overlaps, nil
}

type weeklyRecurrenceRequest struct {
	Kind     string `json:"kind"`
	Weekdays []int  `json:"weekdays"`
	EndsOn   string `json:"endsOn,omitempty"`
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
	var rule *scheduledomain.WeeklyRule
	if request.Recurrence != nil {
		parsed, err := buildWeeklyRule(item, *request.Recurrence)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_recurrence", "반복 요일과 종료일을 다시 확인해 주세요.")
			return
		}
		rule = &parsed
	}
	var overlaps []scheduledomain.Item
	var err error
	if item.TimeKind == scheduledomain.TimeKindAllDay {
		overlaps = nil
	} else if rule == nil {
		overlaps, err = a.overlappingOccurrence(r.Context(), item, "")
	} else {
		overlaps, err = a.overlappingWeeklySchedules(r.Context(), *rule)
	}
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
	var created scheduledomain.Item
	if rule != nil {
		created, err = a.db.CreateWeeklySchedule(r.Context(), *rule, device.ID, time.Now().UTC())
	} else if item.TimeKind == scheduledomain.TimeKindAllDay {
		created, err = a.db.CreateAllDaySchedule(r.Context(), item, device.ID, time.Now().UTC())
	} else {
		created, err = a.db.CreateSchedule(r.Context(), item, device.ID, time.Now().UTC())
	}
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

func buildWeeklyRule(item scheduledomain.Item, request weeklyRecurrenceRequest) (scheduledomain.WeeklyRule, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil || request.Kind != "weekly" {
		return scheduledomain.WeeklyRule{}, scheduledomain.ErrInvalidWeeklyRule
	}
	localStart, localEnd := item.StartsAt.In(location), item.EndsAt.In(location)
	if item.TimeKind != scheduledomain.TimeKindAllDay && item.EndsAt.Sub(item.StartsAt) > 24*time.Hour {
		return scheduledomain.WeeklyRule{}, scheduledomain.ErrInvalidWeeklyRule
	}
	startDate := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), 0, 0, 0, 0, location)
	endDate := time.Date(localEnd.Year(), localEnd.Month(), localEnd.Day(), 0, 0, 0, 0, location)
	startMinute, endMinute := 0, 0
	if item.TimeKind != scheduledomain.TimeKindAllDay {
		dayDifference := int(endDate.Sub(startDate) / (24 * time.Hour))
		startMinute = localStart.Hour()*60 + localStart.Minute()
		endMinute = localEnd.Hour()*60 + localEnd.Minute()
		if dayDifference < 0 || dayDifference > 1 ||
			(dayDifference == 0 && endMinute <= startMinute) ||
			(dayDifference == 1 && endMinute > startMinute) {
			return scheduledomain.WeeklyRule{}, scheduledomain.ErrInvalidWeeklyRule
		}
	}
	days := make([]time.Weekday, 0, len(request.Weekdays))
	for _, day := range request.Weekdays {
		if day < int(time.Sunday) || day > int(time.Saturday) {
			return scheduledomain.WeeklyRule{}, scheduledomain.ErrInvalidWeeklyRule
		}
		days = append(days, time.Weekday(day))
	}
	rule := scheduledomain.WeeklyRule{
		Item: item, StartsOn: startDate, Weekdays: days,
		StartMinute: startMinute, EndMinute: endMinute,
	}
	if request.EndsOn != "" {
		endsOn, err := time.ParseInLocation("2006-01-02", request.EndsOn, location)
		if err != nil {
			return scheduledomain.WeeklyRule{}, scheduledomain.ErrInvalidWeeklyRule
		}
		rule.EndsOn = &endsOn
	}
	if err := scheduledomain.ValidateWeeklyRule(rule, location); err != nil {
		return scheduledomain.WeeklyRule{}, err
	}
	return rule, nil
}

func (a *App) overlappingWeeklySchedules(ctx context.Context, rule scheduledomain.WeeklyRule) ([]scheduledomain.Item, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return nil, err
	}
	from := rule.StartsOn.In(location).UTC()
	to := from.Add(maxScheduleRange)
	if rule.EndsOn != nil {
		endExclusive := rule.EndsOn.In(location).AddDate(0, 0, 2).UTC()
		if endExclusive.Before(to) {
			to = endExclusive
		}
	}
	candidates, err := scheduledomain.ExpandWeekly(rule, from, to, location, nil)
	if err != nil {
		return nil, err
	}
	existing, err := a.db.SchedulesBetween(ctx, from, to, "")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var overlaps []scheduledomain.Item
	for _, candidate := range candidates {
		for _, current := range existing {
			if !scheduledomain.Overlap(candidate.Item, current) {
				continue
			}
			key := current.ID + "|" + current.StartsAt.Format(time.RFC3339Nano)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			overlaps = append(overlaps, current)
		}
	}
	return overlaps, nil
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
	var overlaps []scheduledomain.Item
	var err error
	if item.TimeKind != scheduledomain.TimeKindAllDay {
		overlaps, err = a.overlappingOccurrence(r.Context(), item, "")
	}
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
	var updated scheduledomain.Item
	if item.TimeKind == scheduledomain.TimeKindAllDay {
		updated, err = a.db.UpdateAllDaySchedule(r.Context(), item, device.ID, time.Now().UTC())
	} else {
		updated, err = a.db.UpdateSchedule(r.Context(), item, device.ID, time.Now().UTC())
	}
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
	item := scheduledomain.Item{
		Title: strings.TrimSpace(request.Title), LocationName: strings.TrimSpace(request.LocationName),
		Notes: strings.TrimSpace(request.Notes), Visibility: request.Visibility,
		Participants: request.Participants, Version: request.Version,
	}
	if request.TimeKind == scheduledomain.TimeKindAllDay {
		location, _ := time.LoadLocation("Asia/Seoul")
		startDate, startErr := time.ParseInLocation("2006-01-02", request.StartDate, location)
		endDate, endErr := time.ParseInLocation("2006-01-02", request.EndDate, location)
		if startErr != nil || endErr != nil || endDate.Before(startDate) {
			writeAPIError(w, http.StatusBadRequest, "invalid_date_range", "종일 일정 날짜를 다시 확인해 주세요.")
			return request, scheduledomain.Item{}, false
		}
		item.TimeKind, item.StartDate, item.EndDate = scheduledomain.TimeKindAllDay, request.StartDate, request.EndDate
		item.StartsAt, item.EndsAt = startDate.UTC(), endDate.AddDate(0, 0, 1).UTC()
	} else {
		startsAt, startErr := time.Parse(time.RFC3339, request.StartsAt)
		endsAt, endErr := time.Parse(time.RFC3339, request.EndsAt)
		if startErr != nil || endErr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_time", "시작·종료 시각이 올바르지 않습니다.")
			return request, scheduledomain.Item{}, false
		}
		item.TimeKind, item.StartsAt, item.EndsAt = scheduledomain.TimeKindTimed, startsAt.UTC(), endsAt.UTC()
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

func decodeScheduleVersionRequest(w http.ResponseWriter, r *http.Request) (scheduleVersionRequest, bool) {
	var request scheduleVersionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 형식이 올바르지 않습니다.")
		return request, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 본문은 하나만 허용됩니다.")
		return request, false
	}
	if request.Version < 1 {
		writeAPIError(w, http.StatusBadRequest, "version_required", "일정 버전이 필요합니다.")
		return request, false
	}
	return request, true
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
