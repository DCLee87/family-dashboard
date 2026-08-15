package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (d *Database) NotificationWindow(ctx context.Context, worker string, now time.Time, maximum time.Duration) (time.Time, error) {
	var stored string
	err := d.db.QueryRowContext(ctx, `SELECT last_checked_at FROM notification_worker_state WHERE worker=?`, worker).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return now.Add(-2 * time.Minute), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	last, err := parseDatabaseTime(stored)
	if err != nil {
		return time.Time{}, err
	}
	if last.Before(now.Add(-maximum)) {
		last = now.Add(-maximum)
	}
	return last.Add(-15 * time.Second), nil
}

func (d *Database) MarkNotificationWorker(ctx context.Context, worker string, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO notification_worker_state(worker,last_checked_at) VALUES(?,?)
		ON CONFLICT(worker) DO UPDATE SET last_checked_at=excluded.last_checked_at`, worker, databaseTime(now))
	return err
}
