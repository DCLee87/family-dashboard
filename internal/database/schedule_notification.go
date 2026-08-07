package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ScheduleNotificationSetting struct {
	Enabled          bool `json:"enabled"`
	TimedLeadMinutes int  `json:"timedLeadMinutes"`
	AllDayHour       int  `json:"allDayHour"`
}

func (d *Database) ScheduleNotificationSetting(ctx context.Context, scheduleID string) (ScheduleNotificationSetting, error) {
	var setting ScheduleNotificationSetting
	var enabled int
	err := d.db.QueryRowContext(ctx, `SELECT enabled, timed_lead_minutes, all_day_hour
		FROM schedule_notification_settings WHERE schedule_id = ?`, scheduleID).
		Scan(&enabled, &setting.TimedLeadMinutes, &setting.AllDayHour)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if checkErr := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE id = ? AND deleted_at IS NULL`, scheduleID).Scan(&exists); checkErr != nil {
			return setting, checkErr
		}
		if exists == 0 {
			return setting, ErrScheduleNotFound
		}
		return ScheduleNotificationSetting{Enabled: true, TimedLeadMinutes: 30, AllDayHour: 20}, nil
	}
	if err != nil {
		return setting, fmt.Errorf("read schedule notification setting: %w", err)
	}
	setting.Enabled = enabled == 1
	return setting, nil
}

func (d *Database) SaveScheduleNotificationSetting(
	ctx context.Context,
	scheduleID string,
	setting ScheduleNotificationSetting,
	now time.Time,
) error {
	if setting.TimedLeadMinutes < 0 || setting.TimedLeadMinutes > 10080 || setting.AllDayHour < 0 || setting.AllDayHour > 23 {
		return fmt.Errorf("invalid schedule notification setting")
	}
	var exists int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE id = ? AND deleted_at IS NULL`, scheduleID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrScheduleNotFound
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO schedule_notification_settings(
		schedule_id, enabled, timed_lead_minutes, all_day_hour, updated_at
	) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(schedule_id) DO UPDATE SET enabled = excluded.enabled,
		timed_lead_minutes = excluded.timed_lead_minutes,
		all_day_hour = excluded.all_day_hour, updated_at = excluded.updated_at`,
		scheduleID, boolInt(setting.Enabled), setting.TimedLeadMinutes, setting.AllDayHour, databaseTime(now))
	if err != nil {
		return fmt.Errorf("save schedule notification setting: %w", err)
	}
	return nil
}

func (d *Database) ActiveParentPushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT p.id, p.device_id, p.endpoint, p.endpoint_hash, p.p256dh, p.auth
		FROM push_subscriptions p
		JOIN devices d ON d.id = p.device_id
		WHERE p.revoked_at IS NULL AND d.status = 'active' AND d.device_type = 'parent_mobile'
		ORDER BY p.device_id`)
	if err != nil {
		return nil, fmt.Errorf("list parent push subscriptions: %w", err)
	}
	defer rows.Close()
	var result []PushSubscription
	for rows.Next() {
		var subscription PushSubscription
		var endpointHash []byte
		if err := rows.Scan(&subscription.ID, &subscription.DeviceID, &subscription.Endpoint,
			&endpointHash, &subscription.P256DH, &subscription.Auth); err != nil {
			return nil, err
		}
		if len(endpointHash) != len(subscription.EndpointHash) {
			return nil, fmt.Errorf("read parent push subscription: invalid endpoint hash")
		}
		copy(subscription.EndpointHash[:], endpointHash)
		result = append(result, subscription)
	}
	return result, rows.Err()
}

func (d *Database) ClaimScheduleNotification(
	ctx context.Context,
	id, scheduleID, occurrenceKey, deviceID string,
	notifyAt, now time.Time,
) (bool, error) {
	result, err := d.db.ExecContext(ctx, `INSERT INTO schedule_notification_deliveries(
		id, schedule_id, occurrence_key, device_id, notify_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(schedule_id, occurrence_key, device_id, notify_at) DO NOTHING`,
		id, scheduleID, occurrenceKey, deviceID, databaseTime(notifyAt), databaseTime(now))
	if err != nil {
		return false, fmt.Errorf("claim schedule notification: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (d *Database) CompleteScheduleNotification(ctx context.Context, id string, sentAt time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE schedule_notification_deliveries SET sent_at = ? WHERE id = ?`, databaseTime(sentAt), id)
	return err
}

func (d *Database) ReleaseScheduleNotification(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM schedule_notification_deliveries WHERE id = ? AND sent_at IS NULL`, id)
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
