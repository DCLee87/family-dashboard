package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

const ScheduleTrashRetention = 30 * 24 * time.Hour

type TrashedSchedule struct {
	Item      scheduledomain.Item
	DeletedAt time.Time
}

func (d *Database) TrashSchedule(
	ctx context.Context,
	id string,
	expectedVersion int64,
	deviceID string,
	now time.Time,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schedule deletion: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET
		deleted_at = ?, updated_at = ?, updated_by_device_id = ?, version = version + 1
		WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		databaseTime(now), databaseTime(now), deviceID, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("trash schedule: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read schedule deletion result: %w", err)
	}
	if changed != 1 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
			return fmt.Errorf("check schedule deletion: %w", err)
		}
		if exists == 0 {
			return ErrScheduleNotFound
		}
		return ErrScheduleConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_deletion_metadata(
		schedule_id, deleted_by_device_id, deleted_at
	) VALUES (?, ?, ?)
	ON CONFLICT(schedule_id) DO UPDATE SET
		deleted_by_device_id = excluded.deleted_by_device_id,
		deleted_at = excluded.deleted_at`, id, deviceID, databaseTime(now)); err != nil {
		return fmt.Errorf("record schedule deletion: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schedule deletion: %w", err)
	}
	return nil
}

func (d *Database) TrashedSchedules(ctx context.Context) ([]TrashedSchedule, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT s.id, s.deleted_at
		FROM schedules s
		WHERE s.deleted_at IS NOT NULL
		ORDER BY s.deleted_at DESC, s.id`)
	if err != nil {
		return nil, fmt.Errorf("list trashed schedules: %w", err)
	}
	type deletedRow struct {
		id        string
		deletedAt string
	}
	var stored []deletedRow
	for rows.Next() {
		var row deletedRow
		if err := rows.Scan(&row.id, &row.deletedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan trashed schedule: %w", err)
		}
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]TrashedSchedule, 0, len(stored))
	for _, row := range stored {
		items, err := d.querySchedules(ctx, `SELECT id, title, location_name, notes, visibility,
			starts_at, ends_at, version, created_at, updated_at
			FROM schedules WHERE id = ? AND deleted_at IS NOT NULL`, row.id)
		if err != nil {
			return nil, err
		}
		if len(items) != 1 {
			continue
		}
		deletedAt, err := parseDatabaseTime(row.deletedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, TrashedSchedule{Item: items[0], DeletedAt: deletedAt})
	}
	return result, nil
}

func (d *Database) RestoreSchedule(
	ctx context.Context,
	id string,
	expectedVersion int64,
	deviceID string,
	now time.Time,
) (scheduledomain.Item, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("begin schedule restore: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET
		deleted_at = NULL, updated_at = ?, updated_by_device_id = ?, version = version + 1
		WHERE id = ? AND version = ? AND deleted_at IS NOT NULL
		  AND deleted_at >= ?`, databaseTime(now), deviceID, id, expectedVersion,
		databaseTime(now.Add(-ScheduleTrashRetention)))
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("restore schedule: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return scheduledomain.Item{}, err
	}
	if changed != 1 {
		var version int64
		err := tx.QueryRowContext(ctx, `SELECT version FROM schedules WHERE id = ? AND deleted_at IS NOT NULL`, id).Scan(&version)
		if errors.Is(err, sql.ErrNoRows) {
			return scheduledomain.Item{}, ErrScheduleNotFound
		}
		if err != nil {
			return scheduledomain.Item{}, err
		}
		return scheduledomain.Item{}, ErrScheduleConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_deletion_metadata WHERE schedule_id = ?`, id); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("clear schedule deletion metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("commit schedule restore: %w", err)
	}
	return d.ScheduleByID(ctx, id)
}

func (d *Database) PermanentlyDeleteSchedule(ctx context.Context, id string, expectedVersion int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM schedules
		WHERE id = ? AND version = ? AND deleted_at IS NOT NULL`, id, expectedVersion)
	if err != nil {
		return fmt.Errorf("permanently delete schedule: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	var exists int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE id = ? AND deleted_at IS NOT NULL`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrScheduleNotFound
	}
	return ErrScheduleConflict
}

func (d *Database) PurgeExpiredTrashedSchedules(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `DELETE FROM schedules
		WHERE deleted_at IS NOT NULL AND deleted_at < ?`, databaseTime(now.Add(-ScheduleTrashRetention)))
	if err != nil {
		return 0, fmt.Errorf("purge expired trashed schedules: %w", err)
	}
	return result.RowsAffected()
}
