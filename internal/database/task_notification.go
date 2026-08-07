package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	taskdomain "github.com/DCLee87/family-dashboard/internal/task"
)

type TaskNotificationSetting struct {
	Enabled          bool     `json:"enabled"`
	TimedLeadMinutes int      `json:"timedLeadMinutes"`
	DateHour         int      `json:"dateHour"`
	Recipients       []string `json:"recipients"`
}

func createDefaultTaskNotification(ctx context.Context, tx *sql.Tx, item taskdomain.Item, now time.Time) error {
	enabled := item.DueKind != taskdomain.DueNone
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_notification_settings(
		task_id, enabled, timed_lead_minutes, date_hour, updated_at
	) VALUES (?, ?, 60, 9, ?)`, item.ID, boolInt(enabled), databaseTime(now)); err != nil {
		return fmt.Errorf("create task notification setting: %w", err)
	}
	for _, owner := range defaultTaskNotificationRecipients(item.Assignees) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_notification_recipients(task_id, owner) VALUES (?, ?)`, item.ID, owner); err != nil {
			return fmt.Errorf("create task notification recipient: %w", err)
		}
	}
	return nil
}

func defaultTaskNotificationRecipients(assignees []string) []string {
	if len(assignees) == 1 && assignees[0] == "dad" {
		return []string{"dad"}
	}
	if len(assignees) == 1 && assignees[0] == "mom" {
		return []string{"mom"}
	}
	return []string{"dad", "mom"}
}

func (d *Database) TaskNotificationSetting(ctx context.Context, taskID string) (TaskNotificationSetting, error) {
	var setting TaskNotificationSetting
	var enabled int
	err := d.db.QueryRowContext(ctx, `SELECT enabled, timed_lead_minutes, date_hour
		FROM task_notification_settings WHERE task_id = ?`, taskID).
		Scan(&enabled, &setting.TimedLeadMinutes, &setting.DateHour)
	if errors.Is(err, sql.ErrNoRows) {
		item, itemErr := d.TaskByID(ctx, taskID)
		if itemErr != nil {
			return setting, itemErr
		}
		return TaskNotificationSetting{
			Enabled: item.DueKind != taskdomain.DueNone, TimedLeadMinutes: 60,
			DateHour: 9, Recipients: defaultTaskNotificationRecipients(item.Assignees),
		}, nil
	}
	if err != nil {
		return setting, fmt.Errorf("read task notification setting: %w", err)
	}
	setting.Enabled = enabled == 1
	rows, err := d.db.QueryContext(ctx, `SELECT owner FROM task_notification_recipients WHERE task_id = ? ORDER BY owner`, taskID)
	if err != nil {
		return setting, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return setting, err
		}
		setting.Recipients = append(setting.Recipients, owner)
	}
	return setting, rows.Err()
}

func (d *Database) SaveTaskNotificationSetting(ctx context.Context, taskID string, setting TaskNotificationSetting, now time.Time) error {
	if setting.TimedLeadMinutes < 0 || setting.TimedLeadMinutes > 10080 || setting.DateHour < 0 || setting.DateHour > 23 {
		return fmt.Errorf("invalid task notification setting")
	}
	recipients := uniqueOwners(setting.Recipients)
	if setting.Enabled && len(recipients) == 0 {
		return fmt.Errorf("invalid task notification recipients")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ? AND deleted_at IS NULL`, taskID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrTaskNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_notification_settings(
		task_id, enabled, timed_lead_minutes, date_hour, updated_at
	) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(task_id) DO UPDATE SET enabled = excluded.enabled,
		timed_lead_minutes = excluded.timed_lead_minutes,
		date_hour = excluded.date_hour, updated_at = excluded.updated_at`,
		taskID, boolInt(setting.Enabled), setting.TimedLeadMinutes, setting.DateHour, databaseTime(now)); err != nil {
		return fmt.Errorf("save task notification setting: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_notification_recipients WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, owner := range recipients {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_notification_recipients(task_id, owner) VALUES (?, ?)`, taskID, owner); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func uniqueOwners(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, 2)
	for _, value := range values {
		if (value == "dad" || value == "mom") && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (d *Database) ActiveTaskPushSubscriptions(ctx context.Context, owners []string) ([]PushSubscription, error) {
	owners = uniqueOwners(owners)
	if len(owners) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(owners)), ",")
	args := make([]any, len(owners))
	for index, owner := range owners {
		args[index] = owner
	}
	rows, err := d.db.QueryContext(ctx, `SELECT p.id, p.device_id, p.endpoint, p.endpoint_hash, p.p256dh, p.auth
		FROM push_subscriptions p JOIN devices d ON d.id = p.device_id
		WHERE p.revoked_at IS NULL AND d.status = 'active' AND d.device_type = 'parent_mobile'
		  AND d.owner IN (`+placeholders+`) ORDER BY p.device_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PushSubscription
	for rows.Next() {
		var subscription PushSubscription
		var endpointHash []byte
		if err := rows.Scan(&subscription.ID, &subscription.DeviceID, &subscription.Endpoint, &endpointHash, &subscription.P256DH, &subscription.Auth); err != nil {
			return nil, err
		}
		if len(endpointHash) != len(subscription.EndpointHash) {
			return nil, fmt.Errorf("read task push subscription: invalid endpoint hash")
		}
		copy(subscription.EndpointHash[:], endpointHash)
		result = append(result, subscription)
	}
	return result, rows.Err()
}

func (d *Database) ClaimTaskNotification(ctx context.Context, id, taskID, occurrenceKey, deviceID string, notifyAt, now time.Time) (bool, error) {
	result, err := d.db.ExecContext(ctx, `INSERT INTO task_notification_deliveries(
		id, task_id, occurrence_key, device_id, notify_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(task_id, occurrence_key, device_id, notify_at) DO NOTHING`,
		id, taskID, occurrenceKey, deviceID, databaseTime(notifyAt), databaseTime(now))
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (d *Database) CompleteTaskNotification(ctx context.Context, id string, sentAt time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE task_notification_deliveries SET sent_at = ? WHERE id = ?`, databaseTime(sentAt), id)
	return err
}

func (d *Database) ReleaseTaskNotification(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM task_notification_deliveries WHERE id = ? AND sent_at IS NULL`, id)
	return err
}
