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

var (
	ErrTaskNotFound = errors.New("task not found")
	ErrTaskConflict = errors.New("task version conflict")
)

type TrashedTask struct {
	Item      taskdomain.Item
	DeletedAt time.Time
}

func (d *Database) CreateTask(ctx context.Context, item taskdomain.Item, deviceID string, now time.Time) (taskdomain.Item, error) {
	if err := taskdomain.Validate(item); err != nil {
		return taskdomain.Item{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return taskdomain.Item{}, err
	}
	defer tx.Rollback()
	if err := validateParticipants(ctx, tx, item.Assignees); err != nil {
		return taskdomain.Item{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks(
		id, title, notes, priority, due_kind, due_date, due_minute, repeat_kind,
		starts_on, ends_on, created_by_device_id, updated_by_device_id,
		version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`, item.ID,
		strings.TrimSpace(item.Title), nullable(item.Notes), item.Priority, item.DueKind,
		nullable(item.DueDate), nullableInt(item.DueKind == taskdomain.DueDateTime, item.DueMinute),
		item.RepeatKind, nullable(item.StartsOn), nullable(item.EndsOn), deviceID, deviceID,
		databaseTime(now), databaseTime(now))
	if err != nil {
		return taskdomain.Item{}, fmt.Errorf("insert task: %w", err)
	}
	if err := replaceTaskRelations(ctx, tx, item); err != nil {
		return taskdomain.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskdomain.Item{}, err
	}
	return d.TaskByID(ctx, item.ID)
}

func (d *Database) UpdateTask(ctx context.Context, item taskdomain.Item, deviceID string, now time.Time) (taskdomain.Item, error) {
	if err := taskdomain.Validate(item); err != nil {
		return taskdomain.Item{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return taskdomain.Item{}, err
	}
	defer tx.Rollback()
	if err := validateParticipants(ctx, tx, item.Assignees); err != nil {
		return taskdomain.Item{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ?, notes = ?, priority = ?,
		due_kind = ?, due_date = ?, due_minute = ?, repeat_kind = ?, starts_on = ?, ends_on = ?,
		updated_by_device_id = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND deleted_at IS NULL`, strings.TrimSpace(item.Title),
		nullable(item.Notes), item.Priority, item.DueKind, nullable(item.DueDate),
		nullableInt(item.DueKind == taskdomain.DueDateTime, item.DueMinute), item.RepeatKind,
		nullable(item.StartsOn), nullable(item.EndsOn), deviceID, databaseTime(now), item.ID, item.Version)
	if err != nil {
		return taskdomain.Item{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return taskdomain.Item{}, taskWriteError(ctx, tx, item.ID)
	}
	if err := replaceTaskRelations(ctx, tx, item); err != nil {
		return taskdomain.Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskdomain.Item{}, err
	}
	return d.TaskByID(ctx, item.ID)
}

func replaceTaskRelations(ctx context.Context, tx *sql.Tx, item taskdomain.Item) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_assignees WHERE task_id = ?`, item.ID); err != nil {
		return err
	}
	for _, assignee := range unique(item.Assignees) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_assignees(task_id, family_member_id) VALUES (?, ?)`, item.ID, assignee); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_recurrence_days WHERE task_id = ?`, item.ID); err != nil {
		return err
	}
	if item.RepeatKind == taskdomain.RepeatWeekly {
		seen := map[int]struct{}{}
		for _, weekday := range item.Weekdays {
			if weekday < 0 || weekday > 6 {
				return taskdomain.ErrInvalidTask
			}
			if _, ok := seen[weekday]; ok {
				continue
			}
			seen[weekday] = struct{}{}
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_recurrence_days(task_id, weekday) VALUES (?, ?)`, item.ID, weekday); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Database) TaskByID(ctx context.Context, id string) (taskdomain.Item, error) {
	items, err := d.queryTasks(ctx, `WHERE t.id = ? AND t.deleted_at IS NULL`, id)
	if err != nil {
		return taskdomain.Item{}, err
	}
	if len(items) == 0 {
		return taskdomain.Item{}, ErrTaskNotFound
	}
	return items[0], nil
}

func (d *Database) ActiveTasks(ctx context.Context) ([]taskdomain.Item, error) {
	return d.queryTasks(ctx, `WHERE t.deleted_at IS NULL ORDER BY
		CASE t.priority WHEN 'important' THEN 0 ELSE 1 END, t.created_at, t.id`)
}

func (d *Database) queryTasks(ctx context.Context, clause string, args ...any) ([]taskdomain.Item, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT t.id, t.title, t.notes, t.priority, t.due_kind,
		t.due_date, t.due_minute, t.repeat_kind, t.starts_on, t.ends_on, t.version,
		t.completed_at, t.created_at, t.updated_at FROM tasks t `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()
	var items []taskdomain.Item
	for rows.Next() {
		var item taskdomain.Item
		var notes, dueDate, startsOn, endsOn, completedAt sql.NullString
		var dueMinute sql.NullInt64
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Title, &notes, &item.Priority, &item.DueKind,
			&dueDate, &dueMinute, &item.RepeatKind, &startsOn, &endsOn, &item.Version,
			&completedAt, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Notes, item.DueDate, item.DueMinute = notes.String, dueDate.String, int(dueMinute.Int64)
		item.StartsOn, item.EndsOn = startsOn.String, endsOn.String
		item.CreatedAt, err = parseDatabaseTime(createdAt)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = parseDatabaseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		if completedAt.Valid {
			value, err := parseDatabaseTime(completedAt.String)
			if err != nil {
				return nil, err
			}
			item.CompletedAt = &value
		}
		item.Assignees, err = d.taskAssignees(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.Weekdays, err = d.taskWeekdays(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) taskAssignees(ctx context.Context, id string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT family_member_id FROM task_assignees WHERE task_id = ? ORDER BY family_member_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (d *Database) taskWeekdays(ctx context.Context, id string) ([]int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT weekday FROM task_recurrence_days WHERE task_id = ? ORDER BY weekday`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []int
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (d *Database) SetTaskOccurrenceCompleted(ctx context.Context, taskID, key, deviceID string, completed bool, now time.Time) error {
	if _, err := d.TaskByID(ctx, taskID); err != nil {
		return err
	}
	if completed {
		_, err := d.db.ExecContext(ctx, `INSERT INTO task_occurrence_states(
			task_id, occurrence_key, status, completed_at, completed_by_device_id, updated_at
		) VALUES (?, ?, 'completed', ?, ?, ?)
		ON CONFLICT(task_id, occurrence_key) DO UPDATE SET status = 'completed',
			completed_at = excluded.completed_at, completed_by_device_id = excluded.completed_by_device_id,
			updated_at = excluded.updated_at`, taskID, key, databaseTime(now), deviceID, databaseTime(now))
		return err
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM task_occurrence_states WHERE task_id = ? AND occurrence_key = ?`, taskID, key)
	return err
}

func (d *Database) TaskOccurrenceCompletedAt(ctx context.Context, taskID, key string) (*time.Time, error) {
	var raw string
	err := d.db.QueryRowContext(ctx, `SELECT completed_at FROM task_occurrence_states
		WHERE task_id = ? AND occurrence_key = ? AND status = 'completed'`, taskID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value, err := parseDatabaseTime(raw)
	return &value, err
}

func taskWriteError(ctx context.Context, tx *sql.Tx, id string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrTaskNotFound
	}
	return ErrTaskConflict
}

func nullableInt(valid bool, value int) any {
	if !valid {
		return nil
	}
	return value
}

func (d *Database) TrashTask(ctx context.Context, id string, version int64, deviceID string, now time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE tasks SET deleted_at = ?, updated_at = ?,
		updated_by_device_id = ?, version = version + 1
		WHERE id = ? AND version = ? AND deleted_at IS NULL`, databaseTime(now), databaseTime(now), deviceID, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		return nil
	}
	var exists int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrTaskNotFound
	}
	return ErrTaskConflict
}

func (d *Database) TrashedTasks(ctx context.Context) ([]TrashedTask, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id, deleted_at FROM tasks WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	type row struct{ id, deletedAt string }
	var stored []row
	for rows.Next() {
		var value row
		if err := rows.Scan(&value.id, &value.deletedAt); err != nil {
			rows.Close()
			return nil, err
		}
		stored = append(stored, value)
	}
	rows.Close()
	result := make([]TrashedTask, 0, len(stored))
	for _, value := range stored {
		items, err := d.queryTasks(ctx, `WHERE t.id = ? AND t.deleted_at IS NOT NULL`, value.id)
		if err != nil || len(items) != 1 {
			if err != nil {
				return nil, err
			}
			continue
		}
		deletedAt, err := parseDatabaseTime(value.deletedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, TrashedTask{Item: items[0], DeletedAt: deletedAt})
	}
	return result, nil
}

func (d *Database) RestoreTask(ctx context.Context, id string, version int64, deviceID string, now time.Time) (taskdomain.Item, error) {
	result, err := d.db.ExecContext(ctx, `UPDATE tasks SET deleted_at = NULL, updated_at = ?,
		updated_by_device_id = ?, version = version + 1
		WHERE id = ? AND version = ? AND deleted_at IS NOT NULL AND deleted_at >= ?`,
		databaseTime(now), deviceID, id, version, databaseTime(now.Add(-ScheduleTrashRetention)))
	if err != nil {
		return taskdomain.Item{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return taskdomain.Item{}, ErrTaskConflict
	}
	return d.TaskByID(ctx, id)
}

func (d *Database) PermanentlyDeleteTask(ctx context.Context, id string, version int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND version = ? AND deleted_at IS NOT NULL`, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrTaskConflict
	}
	return nil
}

func (d *Database) PurgeExpiredTrashedTasks(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `DELETE FROM tasks WHERE deleted_at IS NOT NULL AND deleted_at < ?`, databaseTime(now.Add(-ScheduleTrashRetention)))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
