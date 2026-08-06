package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

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
