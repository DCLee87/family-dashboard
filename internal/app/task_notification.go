package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	taskdomain "github.com/DCLee87/family-dashboard/internal/task"
	webpush "github.com/SherClockHolmes/webpush-go"
)

func (a *App) processTaskNotifications(ctx context.Context, now time.Time) {
	if a.config.VAPIDPublicKey == "" || a.config.VAPIDPrivateKey == "" {
		return
	}
	windowStart, err := a.db.NotificationWindow(ctx, "task", now, 7*24*time.Hour)
	if err != nil {
		a.logger.Error("task notification watermark failed", "error", err)
		return
	}
	defer func() {
		if err := a.db.MarkNotificationWorker(ctx, "task", now); err != nil {
			a.logger.Error("task notification watermark save failed", "error", err)
		}
	}()
	items, err := a.db.ActiveTasks(ctx)
	if err != nil {
		a.logger.Error("task notification query failed", "error", err)
		return
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	localNow := now.In(location)
	for _, item := range items {
		if item.DueKind == taskdomain.DueNone {
			continue
		}
		setting, err := a.db.TaskNotificationSetting(ctx, item.ID)
		if err != nil || !setting.Enabled {
			continue
		}
		subscriptions, err := a.db.ActiveTaskPushSubscriptions(ctx, setting.Recipients)
		if err != nil || len(subscriptions) == 0 {
			continue
		}
		candidates := taskNotificationCandidatesBetween(item, setting, windowStart.In(location).AddDate(0, 0, -1), localNow.AddDate(0, 0, 9), location)
		if item.Priority == taskdomain.PriorityImportant {
			candidates = append(candidates, importantTaskReminderCandidates(item, localNow, location)...)
		}
		for _, candidate := range candidates {
			status, err := a.db.TaskOccurrenceStatus(ctx, item.ID, candidate.key)
			if err != nil || status != "pending" {
				continue
			}
			if candidate.notifyAt.Before(windowStart) || candidate.notifyAt.After(now.Add(15*time.Second)) {
				continue
			}
			for _, subscription := range subscriptions {
				a.deliverTaskNotification(ctx, item, candidate.key, candidate.notifyAt, subscription, now, location)
			}
		}
	}
}

func importantTaskReminderCandidates(item taskdomain.Item, now time.Time, location *time.Location) []taskNotificationCandidate {
	reminder := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, location).UTC()
	if reminder.After(now.UTC()) {
		return nil
	}
	if item.RepeatKind == taskdomain.RepeatNone {
		dueDate, err := time.ParseInLocation("2006-01-02", item.DueDate, location)
		if err != nil {
			return nil
		}
		if taskDueAt(item, dueDate, location).Before(reminder) {
			return []taskNotificationCandidate{{key: "single", notifyAt: reminder}}
		}
		return nil
	}
	start := now.AddDate(0, 0, -31)
	if parsed, err := time.ParseInLocation("2006-01-02", item.StartsOn, location); err == nil && parsed.After(start) {
		start = parsed
	}
	result := make([]taskNotificationCandidate, 0, 8)
	for day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location); !day.After(now); day = day.AddDate(0, 0, 1) {
		if taskdomain.AppliesOn(item, day) && taskDueAt(item, day, location).Before(reminder) {
			result = append(result, taskNotificationCandidate{key: day.Format("2006-01-02"), notifyAt: reminder})
		}
	}
	return result
}

func taskDueAt(item taskdomain.Item, date time.Time, location *time.Location) time.Time {
	if item.DueKind == taskdomain.DueDate {
		return time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 0, 0, location).UTC()
	}
	return time.Date(date.Year(), date.Month(), date.Day(), item.DueMinute/60, item.DueMinute%60, 0, 0, location).UTC()
}

type taskNotificationCandidate struct {
	key      string
	notifyAt time.Time
}

func taskNotificationCandidates(item taskdomain.Item, setting database.TaskNotificationSetting, now time.Time, location *time.Location) []taskNotificationCandidate {
	return taskNotificationCandidatesBetween(item, setting, now.AddDate(0, 0, -1), now.AddDate(0, 0, 9), location)
}

func taskNotificationCandidatesBetween(item taskdomain.Item, setting database.TaskNotificationSetting, from, to time.Time, location *time.Location) []taskNotificationCandidate {
	if item.DueKind == taskdomain.DueNone {
		return nil
	}
	if item.RepeatKind == taskdomain.RepeatNone {
		date, err := time.ParseInLocation("2006-01-02", item.DueDate, location)
		if err != nil {
			return nil
		}
		return []taskNotificationCandidate{{key: "single", notifyAt: taskNotifyAt(item, setting, date, location)}}
	}
	result := make([]taskNotificationCandidate, 0, 16)
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, location)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		if !taskdomain.AppliesOn(item, date) {
			continue
		}
		result = append(result, taskNotificationCandidate{
			key: date.Format("2006-01-02"), notifyAt: taskNotifyAt(item, setting, date, location),
		})
	}
	return result
}

func taskNotifyAt(item taskdomain.Item, setting database.TaskNotificationSetting, date time.Time, location *time.Location) time.Time {
	if item.DueKind == taskdomain.DueDate {
		return time.Date(date.Year(), date.Month(), date.Day(), setting.DateHour, 0, 0, 0, location).UTC()
	}
	due := time.Date(date.Year(), date.Month(), date.Day(), item.DueMinute/60, item.DueMinute%60, 0, 0, location)
	return due.Add(-time.Duration(setting.TimedLeadMinutes) * time.Minute).UTC()
}

func (a *App) deliverTaskNotification(ctx context.Context, item taskdomain.Item, key string, notifyAt time.Time, subscription database.PushSubscription, now time.Time, location *time.Location) {
	deliveryID := security.NewToken()
	claimed, err := a.db.ClaimTaskNotification(ctx, deliveryID, item.ID, key, subscription.DeviceID, notifyAt, now)
	if err != nil || !claimed {
		return
	}
	payload, err := json.Marshal(map[string]string{
		"title": item.Title, "body": taskNotificationBody(item, key, location),
		"path": "/", "tag": taskPushTopic(item.ID),
	})
	if err != nil {
		_ = a.db.ReleaseTaskNotification(ctx, deliveryID)
		return
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &webpush.Options{
		HTTPClient: a.pushClient, Subscriber: "https://family-dashboard.local", TTL: 3600,
		Topic: taskPushTopic(item.ID), VAPIDPublicKey: a.config.VAPIDPublicKey,
		VAPIDPrivateKey: a.config.VAPIDPrivateKey,
	})
	if err != nil {
		_ = a.db.ReleaseTaskNotification(ctx, deliveryID)
		a.logger.Warn("task push delivery failed", "task_id", item.ID, "error", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		_ = a.db.RevokePushSubscription(ctx, subscription.DeviceID, now)
		_ = a.db.ReleaseTaskNotification(ctx, deliveryID)
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = a.db.ReleaseTaskNotification(ctx, deliveryID)
		return
	}
	if err := a.db.CompleteTaskNotification(ctx, deliveryID, now); err != nil {
		a.logger.Error("task notification completion failed", "task_id", item.ID, "error", err)
	}
}

func taskNotificationBody(item taskdomain.Item, key string, location *time.Location) string {
	if item.DueKind == taskdomain.DueDate {
		return "오늘까지 완료할 할 일이 있습니다."
	}
	date, err := time.ParseInLocation("2006-01-02", key, location)
	if key == "single" || err != nil {
		date, _ = time.ParseInLocation("2006-01-02", item.DueDate, location)
	}
	due := time.Date(date.Year(), date.Month(), date.Day(), item.DueMinute/60, item.DueMinute%60, 0, 0, location)
	return fmt.Sprintf("%s 마감 예정", due.Format("15:04"))
}

func taskPushTopic(taskID string) string {
	if len(taskID) > 24 {
		taskID = taskID[:24]
	}
	return "task-" + taskID
}

func (a *App) getTaskNotificationSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdminRead(w, r); !ok {
		return
	}
	setting, err := a.db.TaskNotificationSetting(r.Context(), r.PathValue("id"))
	if errors.Is(err, database.ErrTaskNotFound) {
		writeAPIError(w, http.StatusNotFound, "task_not_found", "할 일을 찾을 수 없습니다.")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "할 일 알림 설정을 불러오지 못했습니다.")
		return
	}
	writeJSON(w, http.StatusOK, setting)
}

func (a *App) updateTaskNotificationSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var setting database.TaskNotificationSetting
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&setting); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "알림 설정을 다시 확인해 주세요.")
		return
	}
	if err := a.db.SaveTaskNotificationSetting(r.Context(), r.PathValue("id"), setting, time.Now().UTC()); err != nil {
		if errors.Is(err, database.ErrTaskNotFound) {
			writeAPIError(w, http.StatusNotFound, "task_not_found", "할 일을 찾을 수 없습니다.")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_notification_setting", "알림 설정을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, http.StatusOK, setting)
}
