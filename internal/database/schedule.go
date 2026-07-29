package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

var (
	ErrScheduleNotFound   = errors.New("schedule not found")
	ErrScheduleConflict   = errors.New("schedule version conflict")
	ErrInvalidParticipant = errors.New("invalid schedule participant")
)

type FamilyMember struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func (d *Database) FamilyMembers(ctx context.Context) ([]FamilyMember, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, slug, display_name, role
		FROM family_members WHERE active = 1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list family members: %w", err)
	}
	defer rows.Close()
	var members []FamilyMember
	for rows.Next() {
		var member FamilyMember
		if err := rows.Scan(&member.ID, &member.Slug, &member.DisplayName, &member.Role); err != nil {
			return nil, fmt.Errorf("scan family member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (d *Database) CreateSchedule(
	ctx context.Context,
	item scheduledomain.Item,
	deviceID string,
	now time.Time,
) (scheduledomain.Item, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("begin schedule creation: %w", err)
	}
	defer tx.Rollback()
	if err := validateParticipants(ctx, tx, item.Participants); err != nil {
		return scheduledomain.Item{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schedules(
			id, title, location_name, notes, visibility, time_kind,
			starts_at, ends_at, created_by_device_id, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'timed', ?, ?, ?, 1, ?, ?)`,
		item.ID, strings.TrimSpace(item.Title), nullable(item.LocationName),
		nullable(item.Notes), item.Visibility, databaseTime(item.StartsAt),
		databaseTime(item.EndsAt), deviceID, databaseTime(now), databaseTime(now),
	)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("insert schedule: %w", err)
	}
	for _, participant := range unique(item.Participants) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schedule_participants(schedule_id, family_member_id)
			VALUES (?, ?)`, item.ID, participant); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("insert schedule participant: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("commit schedule creation: %w", err)
	}
	return d.ScheduleByID(ctx, item.ID)
}

func (d *Database) UpdateSchedule(
	ctx context.Context,
	item scheduledomain.Item,
	deviceID string,
	now time.Time,
) (scheduledomain.Item, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("begin schedule update: %w", err)
	}
	defer tx.Rollback()
	if err := validateParticipants(ctx, tx, item.Participants); err != nil {
		return scheduledomain.Item{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE schedules SET
			title = ?, location_name = ?, notes = ?, visibility = ?,
			starts_at = ?, ends_at = ?, version = version + 1, updated_at = ?,
			created_by_device_id = ?
		WHERE id = ? AND version = ? AND deleted_at IS NULL`,
		strings.TrimSpace(item.Title), nullable(item.LocationName), nullable(item.Notes),
		item.Visibility, databaseTime(item.StartsAt), databaseTime(item.EndsAt),
		databaseTime(now), deviceID, item.ID, item.Version,
	)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("update schedule: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("read schedule update result: %w", err)
	}
	if changed != 1 {
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schedules WHERE id = ? AND deleted_at IS NULL`,
			item.ID,
		).Scan(&exists); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("check schedule update: %w", err)
		}
		if exists == 0 {
			return scheduledomain.Item{}, ErrScheduleNotFound
		}
		return scheduledomain.Item{}, ErrScheduleConflict
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM schedule_participants WHERE schedule_id = ?`, item.ID,
	); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("replace schedule participants: %w", err)
	}
	for _, participant := range unique(item.Participants) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schedule_participants(schedule_id, family_member_id)
			VALUES (?, ?)`, item.ID, participant); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("insert schedule participant: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("commit schedule update: %w", err)
	}
	return d.ScheduleByID(ctx, item.ID)
}

func (d *Database) ScheduleByID(ctx context.Context, id string) (scheduledomain.Item, error) {
	items, err := d.querySchedules(ctx, `
		SELECT id, title, location_name, notes, visibility, starts_at, ends_at,
		       version, created_at, updated_at
		FROM schedules WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return scheduledomain.Item{}, err
	}
	if len(items) == 0 {
		return scheduledomain.Item{}, ErrScheduleNotFound
	}
	return items[0], nil
}

func (d *Database) SchedulesBetween(
	ctx context.Context,
	from, to time.Time,
	memberID string,
) ([]scheduledomain.Item, error) {
	query := `
		SELECT DISTINCT s.id, s.title, s.location_name, s.notes, s.visibility,
		       s.starts_at, s.ends_at, s.version, s.created_at, s.updated_at
		FROM schedules s
		JOIN schedule_participants p ON p.schedule_id = s.id
		WHERE s.deleted_at IS NULL AND s.ends_at > ? AND s.starts_at < ?`
	args := []any{databaseTime(from), databaseTime(to)}
	if memberID != "" {
		query += ` AND p.family_member_id = ?`
		args = append(args, memberID)
	}
	query += ` ORDER BY s.starts_at, s.id`
	return d.querySchedules(ctx, query, args...)
}

func (d *Database) OverlappingSchedules(
	ctx context.Context,
	item scheduledomain.Item,
) ([]scheduledomain.Item, error) {
	if len(item.Participants) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique(item.Participants))), ",")
	query := `
		SELECT DISTINCT s.id, s.title, s.location_name, s.notes, s.visibility,
		       s.starts_at, s.ends_at, s.version, s.created_at, s.updated_at
		FROM schedules s
		JOIN schedule_participants p ON p.schedule_id = s.id
		WHERE s.deleted_at IS NULL AND s.id <> ? AND s.ends_at > ? AND s.starts_at < ?
		  AND p.family_member_id IN (` + placeholders + `)
		ORDER BY s.starts_at, s.id`
	args := []any{item.ID, databaseTime(item.StartsAt), databaseTime(item.EndsAt)}
	for _, participant := range unique(item.Participants) {
		args = append(args, participant)
	}
	return d.querySchedules(ctx, query, args...)
}

func (d *Database) querySchedules(
	ctx context.Context,
	query string,
	args ...any,
) ([]scheduledomain.Item, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query schedules: %w", err)
	}
	var items []scheduledomain.Item
	for rows.Next() {
		var item scheduledomain.Item
		var location, notes sql.NullString
		var startsAt, endsAt, createdAt, updatedAt string
		if err := rows.Scan(
			&item.ID, &item.Title, &location, &notes, &item.Visibility,
			&startsAt, &endsAt, &item.Version, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		var err error
		if item.StartsAt, err = parseDatabaseTime(startsAt); err != nil {
			return nil, err
		}
		if item.EndsAt, err = parseDatabaseTime(endsAt); err != nil {
			return nil, err
		}
		if item.CreatedAt, err = parseDatabaseTime(createdAt); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseDatabaseTime(updatedAt); err != nil {
			return nil, err
		}
		item.LocationName, item.Notes = location.String, notes.String
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close schedule rows: %w", err)
	}
	for index := range items {
		participants, err := d.scheduleParticipants(ctx, items[index].ID)
		if err != nil {
			return nil, err
		}
		items[index].Participants = participants
	}
	return items, nil
}

func (d *Database) scheduleParticipants(ctx context.Context, scheduleID string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT family_member_id FROM schedule_participants
		WHERE schedule_id = ? ORDER BY family_member_id`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("query schedule participants: %w", err)
	}
	defer rows.Close()
	var participants []string
	for rows.Next() {
		var participant string
		if err := rows.Scan(&participant); err != nil {
			return nil, fmt.Errorf("scan schedule participant: %w", err)
		}
		participants = append(participants, participant)
	}
	return participants, rows.Err()
}

func validateParticipants(ctx context.Context, tx *sql.Tx, participants []string) error {
	participants = unique(participants)
	if len(participants) == 0 {
		return ErrInvalidParticipant
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(participants)), ",")
	args := make([]any, len(participants))
	for index, participant := range participants {
		args[index] = participant
	}
	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM family_members WHERE active = 1 AND id IN (`+placeholders+`)`,
		args...,
	).Scan(&count); err != nil {
		return fmt.Errorf("validate schedule participants: %w", err)
	}
	if count != len(participants) {
		return ErrInvalidParticipant
	}
	return nil
}

func parseDatabaseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database time: %w", err)
	}
	return parsed, nil
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
