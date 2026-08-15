package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ScheduleNotificationSetting struct {
	Enabled          bool     `json:"enabled"`
	TimedLeadMinutes int      `json:"timedLeadMinutes"`
	AllDayHour       int      `json:"allDayHour"`
	TimedLeadOptions []int    `json:"timedLeadOptions"`
	AllDayHours      []int    `json:"allDayHours"`
	Recipients       []string `json:"recipients"`
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
		return ScheduleNotificationSetting{Enabled: true, TimedLeadMinutes: 30, AllDayHour: 20, TimedLeadOptions: []int{30}, AllDayHours: []int{20}, Recipients: []string{"dad", "mom"}}, nil
	}
	if err != nil {
		return setting, fmt.Errorf("read schedule notification setting: %w", err)
	}
	setting.Enabled = enabled == 1
	rows, rowsErr := d.db.QueryContext(ctx, `SELECT kind,value FROM schedule_notification_times WHERE schedule_id=? ORDER BY kind,value`, scheduleID)
	if rowsErr != nil {
		return setting, rowsErr
	}
	for rows.Next() {
		var kind string
		var value int
		if err := rows.Scan(&kind, &value); err != nil {
			rows.Close()
			return setting, err
		}
		if kind == "timed" {
			setting.TimedLeadOptions = append(setting.TimedLeadOptions, value)
		} else {
			setting.AllDayHours = append(setting.AllDayHours, value)
		}
	}
	rows.Close()
	if len(setting.TimedLeadOptions) == 0 {
		setting.TimedLeadOptions = []int{setting.TimedLeadMinutes}
	}
	if len(setting.AllDayHours) == 0 {
		setting.AllDayHours = []int{setting.AllDayHour}
	}
	ownerRows, ownerErr := d.db.QueryContext(ctx, `SELECT owner FROM schedule_notification_recipients WHERE schedule_id=? ORDER BY owner`, scheduleID)
	if ownerErr != nil {
		return setting, ownerErr
	}
	defer ownerRows.Close()
	for ownerRows.Next() {
		var owner string
		if err := ownerRows.Scan(&owner); err != nil {
			return setting, err
		}
		setting.Recipients = append(setting.Recipients, owner)
	}
	if len(setting.Recipients) == 0 {
		setting.Recipients = []string{"dad", "mom"}
	}
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
	if len(setting.TimedLeadOptions) == 0 {
		setting.TimedLeadOptions = []int{setting.TimedLeadMinutes}
	}
	if len(setting.AllDayHours) == 0 {
		setting.AllDayHours = []int{setting.AllDayHour}
	}
	for _, value := range setting.TimedLeadOptions {
		if value < 0 || value > 10080 {
			return fmt.Errorf("invalid schedule notification setting")
		}
	}
	for _, value := range setting.AllDayHours {
		if value < 0 || value > 23 {
			return fmt.Errorf("invalid schedule notification setting")
		}
	}
	recipients := uniqueOwners(setting.Recipients)
	if setting.Enabled && len(recipients) == 0 {
		return fmt.Errorf("invalid schedule notification recipients")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_notification_settings(
		schedule_id, enabled, timed_lead_minutes, all_day_hour, updated_at
	) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(schedule_id) DO UPDATE SET enabled = excluded.enabled,
		timed_lead_minutes = excluded.timed_lead_minutes,
		all_day_hour = excluded.all_day_hour, updated_at = excluded.updated_at`,
		scheduleID, boolInt(setting.Enabled), setting.TimedLeadMinutes, setting.AllDayHour, databaseTime(now))
	if err != nil {
		return fmt.Errorf("save schedule notification setting: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM schedule_notification_times WHERE schedule_id=?`, scheduleID); err != nil {
		return err
	}
	for _, value := range setting.TimedLeadOptions {
		if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_notification_times(schedule_id,kind,value) VALUES(?,'timed',?)`, scheduleID, value); err != nil {
			return err
		}
	}
	for _, value := range setting.AllDayHours {
		if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_notification_times(schedule_id,kind,value) VALUES(?,'all_day',?)`, scheduleID, value); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM schedule_notification_recipients WHERE schedule_id=?`, scheduleID); err != nil {
		return err
	}
	for _, owner := range recipients {
		if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_notification_recipients(schedule_id,owner) VALUES(?,?)`, scheduleID, owner); err != nil {
			return err
		}
	}
	return tx.Commit()
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
