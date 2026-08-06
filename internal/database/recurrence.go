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

var ErrOccurrenceConflict = errors.New("occurrence version conflict")

func (d *Database) OccurrenceExceptions(ctx context.Context, scheduleID string) ([]scheduledomain.OccurrenceException, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT e.occurrence_key, e.status, e.override_title,
		e.override_location_name, e.override_notes, e.override_visibility, e.override_starts_at,
		e.override_ends_at, e.version, a.start_date, a.end_date
		FROM schedule_occurrence_exceptions e
		LEFT JOIN schedule_occurrence_all_day_overrides a
		  ON a.schedule_id = e.schedule_id AND a.occurrence_key = e.occurrence_key
		WHERE e.schedule_id = ? ORDER BY e.occurrence_key`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list occurrence exceptions: %w", err)
	}
	type storedException struct {
		key, status                                          string
		title, location, notes, visibility, startsAt, endsAt sql.NullString
		startDate, endDate                                   sql.NullString
		version                                              int64
	}
	var stored []storedException
	for rows.Next() {
		var value storedException
		if err := rows.Scan(&value.key, &value.status, &value.title, &value.location,
			&value.notes, &value.visibility, &value.startsAt, &value.endsAt, &value.version,
			&value.startDate, &value.endDate); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan occurrence exception: %w", err)
		}
		stored = append(stored, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	base, err := d.ScheduleByID(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	result := make([]scheduledomain.OccurrenceException, 0, len(stored))
	for _, value := range stored {
		exception := scheduledomain.OccurrenceException{
			OccurrenceKey: value.key, Cancelled: value.status == "cancelled", Version: value.version,
		}
		if !exception.Cancelled {
			override := base
			override.Title, override.LocationName, override.Notes, override.Visibility =
				value.title.String, value.location.String, value.notes.String, value.visibility.String
			override.StartsAt, err = parseDatabaseTime(value.startsAt.String)
			if err != nil {
				return nil, err
			}
			override.EndsAt, err = parseDatabaseTime(value.endsAt.String)
			if err != nil {
				return nil, err
			}
			if override.TimeKind == scheduledomain.TimeKindAllDay {
				override.StartDate, override.EndDate = value.startDate.String, value.endDate.String
				if !value.startDate.Valid || !value.endDate.Valid {
					return nil, scheduledomain.ErrInvalidOccurrence
				}
			}
			exception.Override = &override
		}
		result = append(result, exception)
	}
	return result, nil
}

func (d *Database) SaveOccurrenceException(
	ctx context.Context,
	scheduleID string,
	exception scheduledomain.OccurrenceException,
	expectedVersion int64,
	now time.Time,
) (int64, error) {
	if exception.OccurrenceKey == "" || (exception.Cancelled && exception.Override != nil) ||
		(!exception.Cancelled && exception.Override == nil) {
		return 0, scheduledomain.ErrInvalidOccurrence
	}
	if exception.Override != nil {
		if err := scheduledomain.Validate(*exception.Override); err != nil {
			return 0, err
		}
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin occurrence exception save: %w", err)
	}
	defer tx.Rollback()
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM schedule_occurrence_exceptions
		WHERE schedule_id = ? AND occurrence_key = ?`, scheduleID, exception.OccurrenceKey).Scan(&currentVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows) && expectedVersion != 0:
		return 0, ErrOccurrenceConflict
	case err == nil && currentVersion != expectedVersion:
		return 0, ErrOccurrenceConflict
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("read occurrence exception version: %w", err)
	}
	newVersion := currentVersion + 1
	status := "overridden"
	var title, location, notes, visibility, startsAt, endsAt any
	if exception.Cancelled {
		status = "cancelled"
	} else {
		override := exception.Override
		title, location, notes, visibility = override.Title, nullable(override.LocationName), nullable(override.Notes), override.Visibility
		startsAt, endsAt = databaseTime(override.StartsAt), databaseTime(override.EndsAt)
	}
	if currentVersion == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO schedule_occurrence_exceptions(
			schedule_id, occurrence_key, status, override_title, override_location_name,
			override_notes, override_visibility, override_starts_at, override_ends_at,
			version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`, scheduleID, exception.OccurrenceKey,
			status, title, location, notes, visibility, startsAt, endsAt, databaseTime(now), databaseTime(now))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE schedule_occurrence_exceptions SET
			status = ?, override_title = ?, override_location_name = ?, override_notes = ?,
			override_visibility = ?, override_starts_at = ?, override_ends_at = ?,
			version = version + 1, updated_at = ?
			WHERE schedule_id = ? AND occurrence_key = ? AND version = ?`, status, title, location,
			notes, visibility, startsAt, endsAt, databaseTime(now), scheduleID, exception.OccurrenceKey, expectedVersion)
	}
	if err != nil {
		return 0, fmt.Errorf("save occurrence exception: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_occurrence_all_day_overrides WHERE schedule_id = ? AND occurrence_key = ?`, scheduleID, exception.OccurrenceKey); err != nil {
		return 0, fmt.Errorf("replace occurrence all-day dates: %w", err)
	}
	if exception.Override != nil && exception.Override.TimeKind == scheduledomain.TimeKindAllDay {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_occurrence_all_day_overrides(schedule_id, occurrence_key, start_date, end_date) VALUES (?, ?, ?, ?)`, scheduleID, exception.OccurrenceKey, exception.Override.StartDate, exception.Override.EndDate); err != nil {
			return 0, fmt.Errorf("insert occurrence all-day dates: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit occurrence exception: %w", err)
	}
	return newVersion, nil
}

func (d *Database) CreateWeeklySchedule(
	ctx context.Context,
	rule scheduledomain.WeeklyRule,
	deviceID string,
	now time.Time,
) (scheduledomain.Item, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("load family timezone: %w", err)
	}
	if err := scheduledomain.Validate(rule.Item); err != nil {
		return scheduledomain.Item{}, err
	}
	if err := scheduledomain.ValidateWeeklyRule(rule, location); err != nil {
		return scheduledomain.Item{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("begin weekly schedule creation: %w", err)
	}
	defer tx.Rollback()
	if err := validateParticipants(ctx, tx, rule.Item.Participants); err != nil {
		return scheduledomain.Item{}, err
	}
	item := rule.Item
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schedules(
			id, title, location_name, notes, visibility, time_kind,
			starts_at, ends_at, created_by_device_id, updated_by_device_id,
			version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'timed', ?, ?, ?, ?, 1, ?, ?)`,
		item.ID, strings.TrimSpace(item.Title), nullable(item.LocationName), nullable(item.Notes),
		item.Visibility, databaseTime(item.StartsAt), databaseTime(item.EndsAt), deviceID, deviceID,
		databaseTime(now), databaseTime(now),
	)
	if err != nil {
		return scheduledomain.Item{}, fmt.Errorf("insert recurring schedule: %w", err)
	}
	if item.TimeKind == scheduledomain.TimeKindAllDay {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_all_day_dates(schedule_id, start_date, end_date) VALUES (?, ?, ?)`, item.ID, item.StartDate, item.EndDate); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("insert recurring all-day dates: %w", err)
		}
	}
	for _, participant := range unique(item.Participants) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_participants(schedule_id, family_member_id) VALUES (?, ?)`, item.ID, participant); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("insert recurring schedule participant: %w", err)
		}
	}
	var endsOn any
	if rule.EndsOn != nil {
		endsOn = rule.EndsOn.In(location).Format("2006-01-02")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_recurrence_rules(
		schedule_id, recurrence_kind, starts_on, ends_on, start_minute, end_minute, timezone
	) VALUES (?, 'weekly', ?, ?, ?, ?, 'Asia/Seoul')`, item.ID,
		rule.StartsOn.In(location).Format("2006-01-02"), endsOn, rule.StartMinute, rule.EndMinute); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("insert weekly rule: %w", err)
	}
	for _, weekday := range uniqueWeekdaysForStorage(rule.Weekdays) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_recurrence_days(schedule_id, weekday) VALUES (?, ?)`, item.ID, int(weekday)); err != nil {
			return scheduledomain.Item{}, fmt.Errorf("insert weekly day: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return scheduledomain.Item{}, fmt.Errorf("commit weekly schedule creation: %w", err)
	}
	return d.ScheduleByID(ctx, item.ID)
}

func uniqueWeekdaysForStorage(values []time.Weekday) []time.Weekday {
	seen := make(map[time.Weekday]struct{}, len(values))
	result := make([]time.Weekday, 0, len(values))
	for _, value := range values {
		if value < time.Sunday || value > time.Saturday {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (d *Database) RecurringOccurrencesBetween(ctx context.Context, from, to time.Time, memberID string) ([]scheduledomain.Item, error) {
	rules, err := d.WeeklyRules(ctx)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return nil, err
	}
	var items []scheduledomain.Item
	for _, rule := range rules {
		exceptions, err := d.OccurrenceExceptions(ctx, rule.Item.ID)
		if err != nil {
			return nil, err
		}
		occurrences, err := scheduledomain.ExpandWeekly(rule, from, to, location, exceptions)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range occurrences {
			if memberID != "" {
				found := false
				for _, participant := range occurrence.Participants {
					if participant == memberID {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			items = append(items, occurrence.Item)
		}
	}
	return items, nil
}

func (d *Database) SaveWeeklyRule(ctx context.Context, rule scheduledomain.WeeklyRule) error {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return fmt.Errorf("load family timezone: %w", err)
	}
	if err := scheduledomain.ValidateWeeklyRule(rule, location); err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin weekly rule save: %w", err)
	}
	defer tx.Rollback()
	var endsOn any
	if rule.EndsOn != nil {
		endsOn = rule.EndsOn.In(location).Format("2006-01-02")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_recurrence_rules(
		schedule_id, recurrence_kind, starts_on, ends_on, start_minute, end_minute, timezone
	) VALUES (?, 'weekly', ?, ?, ?, ?, 'Asia/Seoul')`,
		rule.Item.ID, rule.StartsOn.In(location).Format("2006-01-02"), endsOn,
		rule.StartMinute, rule.EndMinute)
	if err != nil {
		return fmt.Errorf("insert weekly rule: %w", err)
	}
	for _, weekday := range rule.Weekdays {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_recurrence_days(schedule_id, weekday) VALUES (?, ?)`, rule.Item.ID, int(weekday)); err != nil {
			return fmt.Errorf("insert weekly day: %w", err)
		}
	}
	return tx.Commit()
}

func (d *Database) WeeklyRules(ctx context.Context) ([]scheduledomain.WeeklyRule, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT r.schedule_id, r.starts_on, r.ends_on, r.start_minute, r.end_minute
		FROM schedule_recurrence_rules r ORDER BY r.schedule_id`)
	if err != nil {
		return nil, fmt.Errorf("list weekly rules: %w", err)
	}
	type row struct {
		id, starts             string
		ends                   sql.NullString
		startMinute, endMinute int
	}
	var stored []row
	for rows.Next() {
		var value row
		if err := rows.Scan(&value.id, &value.starts, &value.ends, &value.startMinute, &value.endMinute); err != nil {
			rows.Close()
			return nil, err
		}
		stored = append(stored, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	var rules []scheduledomain.WeeklyRule
	for _, value := range stored {
		var rule scheduledomain.WeeklyRule
		rule.StartMinute, rule.EndMinute = value.startMinute, value.endMinute
		item, err := d.ScheduleByID(ctx, value.id)
		if err != nil {
			return nil, err
		}
		rule.Item = item
		rule.StartsOn, err = time.ParseInLocation("2006-01-02", value.starts, location)
		if err != nil {
			return nil, err
		}
		if value.ends.Valid {
			end, err := time.ParseInLocation("2006-01-02", value.ends.String, location)
			if err != nil {
				return nil, err
			}
			rule.EndsOn = &end
		}
		days, err := d.weeklyDays(ctx, value.id)
		if err != nil {
			return nil, err
		}
		rule.Weekdays = days
		rules = append(rules, rule)
	}
	return rules, nil
}

func (d *Database) weeklyDays(ctx context.Context, scheduleID string) ([]time.Weekday, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT weekday FROM schedule_recurrence_days WHERE schedule_id = ? ORDER BY weekday`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []time.Weekday
	for rows.Next() {
		var day int
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		days = append(days, time.Weekday(day))
	}
	return days, rows.Err()
}
