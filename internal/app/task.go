package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	taskdomain "github.com/DCLee87/family-dashboard/internal/task"
)

type taskRequest struct {
	Title      string   `json:"title"`
	Notes      string   `json:"notes"`
	Priority   string   `json:"priority"`
	DueKind    string   `json:"dueKind"`
	DueDate    string   `json:"dueDate"`
	DueMinute  int      `json:"dueMinute"`
	Assignees  []string `json:"assignees"`
	RepeatKind string   `json:"repeatKind"`
	StartsOn   string   `json:"startsOn"`
	EndsOn     string   `json:"endsOn"`
	Weekdays   []int    `json:"weekdays"`
	Version    int64    `json:"version"`
}

type taskView struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Notes         string     `json:"notes,omitempty"`
	Priority      string     `json:"priority"`
	DueKind       string     `json:"dueKind"`
	DueDate       string     `json:"dueDate,omitempty"`
	DueMinute     int        `json:"dueMinute,omitempty"`
	Assignees     []string   `json:"assignees"`
	RepeatKind    string     `json:"repeatKind"`
	StartsOn      string     `json:"startsOn,omitempty"`
	EndsOn        string     `json:"endsOn,omitempty"`
	Weekdays      []int      `json:"weekdays,omitempty"`
	Version       int64      `json:"version"`
	OccurrenceKey string     `json:"occurrenceKey"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	Overdue       bool       `json:"overdue"`
}

func (a *App) createTask(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	request, item, ok := decodeTaskRequest(w, r)
	if !ok {
		return
	}
	_ = request
	item.ID = security.NewToken()
	created, err := a.db.CreateTask(r.Context(), item, device.ID, time.Now().UTC())
	if errors.Is(err, database.ErrInvalidParticipant) || errors.Is(err, taskdomain.ErrInvalidTask) {
		writeAPIError(w, http.StatusBadRequest, "invalid_task", "할 일 입력값을 다시 확인해 주세요.")
		return
	}
	if err != nil {
		a.logger.Error("task creation failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "할 일을 저장하지 못했습니다.")
		return
	}
	writeJSON(w, http.StatusCreated, taskItemView(created, "single", nil, false))
}

func (a *App) updateTask(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	_, item, ok := decodeTaskRequest(w, r)
	if !ok {
		return
	}
	item.ID = r.PathValue("id")
	if item.Version < 1 {
		writeAPIError(w, http.StatusBadRequest, "version_required", "할 일 버전이 필요합니다.")
		return
	}
	updated, err := a.db.UpdateTask(r.Context(), item, device.ID, time.Now().UTC())
	switch {
	case errors.Is(err, database.ErrTaskNotFound):
		writeAPIError(w, http.StatusNotFound, "task_not_found", "할 일을 찾을 수 없습니다.")
	case errors.Is(err, database.ErrTaskConflict):
		writeAPIError(w, http.StatusConflict, "task_version_conflict", "다른 기기에서 할 일이 변경되었습니다.")
	case err != nil:
		writeAPIError(w, http.StatusBadRequest, "invalid_task", "할 일 입력값을 다시 확인해 주세요.")
	default:
		writeJSON(w, http.StatusOK, taskItemView(updated, "single", nil, false))
	}
}

func (a *App) listTaskOccurrences(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedDevice(w, r); !ok {
		return
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	now := time.Now().In(location)
	date := now
	if raw := r.URL.Query().Get("date"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, location)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_date", "조회 날짜를 다시 확인해 주세요.")
			return
		}
		date = parsed
	}
	items, err := a.db.ActiveTasks(r.Context())
	if err != nil {
		a.logger.Error("task list failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "할 일을 불러오지 못했습니다.")
		return
	}
	views := make([]taskView, 0, len(items))
	for _, item := range items {
		if !taskdomain.AppliesOn(item, date) {
			continue
		}
		key := "single"
		if item.RepeatKind != taskdomain.RepeatNone {
			key = date.Format("2006-01-02")
		}
		completedAt, err := a.db.TaskOccurrenceCompletedAt(r.Context(), item.ID, key)
		if err != nil {
			continue
		}
		if completedAt != nil && completedAt.Before(now.AddDate(0, 0, -7)) {
			continue
		}
		views = append(views, taskItemView(item, key, completedAt, taskOverdue(item, date, now, completedAt != nil, location)))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].CompletedAt != nil && views[j].CompletedAt == nil {
			return false
		}
		if views[i].CompletedAt == nil && views[j].CompletedAt != nil {
			return true
		}
		return views[i].Priority == taskdomain.PriorityImportant && views[j].Priority != taskdomain.PriorityImportant
	})
	writeJSON(w, http.StatusOK, map[string]any{"date": date.Format("2006-01-02"), "tasks": views})
}

func (a *App) completeTaskOccurrence(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	completed := !strings.HasSuffix(r.URL.Path, "/reopen")
	if err := a.db.SetTaskOccurrenceCompleted(r.Context(), r.PathValue("id"), r.PathValue("key"), device.ID, completed, time.Now().UTC()); err != nil {
		if errors.Is(err, database.ErrTaskNotFound) {
			writeAPIError(w, http.StatusNotFound, "task_not_found", "할 일을 찾을 수 없습니다.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "완료 상태를 변경하지 못했습니다.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeTaskRequest(w http.ResponseWriter, r *http.Request) (taskRequest, taskdomain.Item, bool) {
	var request taskRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 형식이 올바르지 않습니다.")
		return request, taskdomain.Item{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "요청 본문은 하나만 허용됩니다.")
		return request, taskdomain.Item{}, false
	}
	item := taskdomain.Item{
		Title: strings.TrimSpace(request.Title), Notes: strings.TrimSpace(request.Notes),
		Priority: request.Priority, DueKind: request.DueKind, DueDate: request.DueDate,
		DueMinute: request.DueMinute, Assignees: request.Assignees, RepeatKind: request.RepeatKind,
		StartsOn: request.StartsOn, EndsOn: request.EndsOn, Weekdays: request.Weekdays, Version: request.Version,
	}
	if err := taskdomain.Validate(item); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_task", "할 일 입력값을 다시 확인해 주세요.")
		return request, item, false
	}
	return request, item, true
}

func taskItemView(item taskdomain.Item, key string, completedAt *time.Time, overdue bool) taskView {
	return taskView{ID: item.ID, Title: item.Title, Notes: item.Notes, Priority: item.Priority,
		DueKind: item.DueKind, DueDate: item.DueDate, DueMinute: item.DueMinute,
		Assignees: item.Assignees, RepeatKind: item.RepeatKind, StartsOn: item.StartsOn,
		EndsOn: item.EndsOn, Weekdays: item.Weekdays, Version: item.Version,
		OccurrenceKey: key, CompletedAt: completedAt, Overdue: overdue}
}

func taskOverdue(item taskdomain.Item, occurrenceDate, now time.Time, completed bool, location *time.Location) bool {
	if completed || item.DueKind == taskdomain.DueNone {
		return false
	}
	dueDate := item.DueDate
	if item.RepeatKind != taskdomain.RepeatNone {
		dueDate = occurrenceDate.Format("2006-01-02")
	}
	date, err := time.ParseInLocation("2006-01-02", dueDate, location)
	if err != nil {
		return false
	}
	if item.DueKind == taskdomain.DueDate {
		return now.Format("2006-01-02") > dueDate
	}
	due := date.Add(time.Duration(item.DueMinute) * time.Minute)
	return now.After(due)
}
