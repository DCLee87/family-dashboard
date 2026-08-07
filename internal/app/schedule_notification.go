package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
	"github.com/DCLee87/family-dashboard/internal/security"
	webpush "github.com/SherClockHolmes/webpush-go"
)

func (a *App) startBackgroundWorkers() {
	ctx, cancel := context.WithCancel(context.Background())
	a.workerStop = cancel
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.processScheduleNotifications(ctx, time.Now().UTC())
		a.processTaskNotifications(ctx, time.Now().UTC())
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				a.processScheduleNotifications(ctx, now.UTC())
				a.processTaskNotifications(ctx, now.UTC())
			}
		}
	}()
}

func (a *App) processScheduleNotifications(ctx context.Context, now time.Time) {
	if a.config.VAPIDPublicKey == "" || a.config.VAPIDPrivateKey == "" {
		return
	}
	items, err := a.db.SchedulesBetween(ctx, now.Add(-2*time.Hour), now.Add(36*time.Hour), "")
	if err != nil {
		a.logger.Error("schedule notification query failed", "error", err)
		return
	}
	subscriptions, err := a.db.ActiveParentPushSubscriptions(ctx)
	if err != nil {
		a.logger.Error("schedule notification subscription query failed", "error", err)
		return
	}
	if len(subscriptions) == 0 {
		return
	}
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		a.logger.Error("schedule notification timezone failed", "error", err)
		return
	}
	for _, item := range items {
		setting, err := a.db.ScheduleNotificationSetting(ctx, item.ID)
		if err != nil || !setting.Enabled {
			continue
		}
		notifyAt, err := scheduleNotifyAt(item, setting, location)
		if err != nil || notifyAt.Before(now.Add(-2*time.Minute)) || notifyAt.After(now.Add(15*time.Second)) {
			continue
		}
		for _, subscription := range subscriptions {
			a.deliverScheduleNotification(ctx, item, notifyAt, subscription, now)
		}
	}
}

func scheduleNotifyAt(
	item scheduledomain.Item,
	setting database.ScheduleNotificationSetting,
	location *time.Location,
) (time.Time, error) {
	if item.TimeKind == scheduledomain.TimeKindAllDay {
		start, err := time.ParseInLocation("2006-01-02", item.StartDate, location)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(start.Year(), start.Month(), start.Day()-1, setting.AllDayHour, 0, 0, 0, location).UTC(), nil
	}
	return item.StartsAt.Add(-time.Duration(setting.TimedLeadMinutes) * time.Minute), nil
}

func (a *App) deliverScheduleNotification(
	ctx context.Context,
	item scheduledomain.Item,
	notifyAt time.Time,
	subscription database.PushSubscription,
	now time.Time,
) {
	deliveryID := security.NewToken()
	claimed, err := a.db.ClaimScheduleNotification(
		ctx, deliveryID, item.ID, item.OccurrenceKey, subscription.DeviceID, notifyAt, now,
	)
	if err != nil || !claimed {
		return
	}
	payload, err := json.Marshal(map[string]string{
		"title": item.Title,
		"body":  scheduleNotificationBody(item),
		"path":  "/",
		"tag":   schedulePushTopic(item.ID),
	})
	if err != nil {
		_ = a.db.ReleaseScheduleNotification(ctx, deliveryID)
		return
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &webpush.Options{
		HTTPClient: a.pushClient, Subscriber: "https://family-dashboard.local", TTL: 3600,
		Topic: schedulePushTopic(item.ID), VAPIDPublicKey: a.config.VAPIDPublicKey,
		VAPIDPrivateKey: a.config.VAPIDPrivateKey,
	})
	if err != nil {
		_ = a.db.ReleaseScheduleNotification(ctx, deliveryID)
		a.logger.Warn("schedule push delivery failed", "schedule_id", item.ID, "error", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		_ = a.db.RevokePushSubscription(ctx, subscription.DeviceID, now)
		_ = a.db.ReleaseScheduleNotification(ctx, deliveryID)
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = a.db.ReleaseScheduleNotification(ctx, deliveryID)
		a.logger.Warn("schedule push rejected", "schedule_id", item.ID, "status", response.StatusCode)
		return
	}
	if err := a.db.CompleteScheduleNotification(ctx, deliveryID, now); err != nil {
		a.logger.Error("schedule notification completion failed", "schedule_id", item.ID, "error", err)
	}
}

func scheduleNotificationBody(item scheduledomain.Item) string {
	if item.TimeKind == scheduledomain.TimeKindAllDay {
		if item.LocationName != "" {
			return fmt.Sprintf("내일 종일 · %s", item.LocationName)
		}
		return "내일 종일 일정이 있습니다."
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	message := item.StartsAt.In(location).Format("15:04")
	if item.LocationName != "" {
		message += " · " + item.LocationName
	}
	return message
}

func schedulePushTopic(scheduleID string) string {
	if len(scheduleID) > 20 {
		scheduleID = scheduleID[:20]
	}
	return "schedule-" + scheduleID
}

func (a *App) getScheduleNotificationSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdminRead(w, r); !ok {
		return
	}
	setting, err := a.db.ScheduleNotificationSetting(r.Context(), r.PathValue("id"))
	if errors.Is(err, database.ErrScheduleNotFound) {
		writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
		return
	}
	if err != nil {
		a.scheduleInternalError(w, "schedule notification setting read failed", err)
		return
	}
	writeJSON(w, http.StatusOK, setting)
}

func (a *App) updateScheduleNotificationSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var setting database.ScheduleNotificationSetting
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&setting); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "알림 설정을 다시 확인해 주세요.")
		return
	}
	if err := a.db.SaveScheduleNotificationSetting(r.Context(), r.PathValue("id"), setting, time.Now().UTC()); err != nil {
		if errors.Is(err, database.ErrScheduleNotFound) {
			writeAPIError(w, http.StatusNotFound, "schedule_not_found", "일정을 찾을 수 없습니다.")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_notification_setting", "알림 설정을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, http.StatusOK, setting)
}
