package database

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrPINBlocked = errors.New("PIN attempts are temporarily blocked")

func (d *Database) PINChallenge(
	ctx context.Context,
	deviceID string,
	now time.Time,
) (string, error) {
	var pinHash string
	var blockedUntil sql.NullString
	err := d.db.QueryRowContext(
		ctx,
		`SELECT s.pin_hash, p.blocked_until
		 FROM security_state s
		 LEFT JOIN pin_attempts p ON p.device_id = ?
		 WHERE s.id = 1 AND s.setup_complete = 1`,
		deviceID,
	).Scan(&pinHash, &blockedUntil)
	if err != nil {
		return "", fmt.Errorf("read PIN challenge: %w", err)
	}
	if blockedUntil.Valid {
		blocked, err := time.Parse(time.RFC3339Nano, blockedUntil.String)
		if err != nil {
			return "", fmt.Errorf("parse PIN block expiry: %w", err)
		}
		if now.Before(blocked) {
			return "", ErrPINBlocked
		}
	}
	return pinHash, nil
}

func (d *Database) RecordPINFailure(
	ctx context.Context,
	deviceID string,
	now time.Time,
) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin PIN failure: %w", err)
	}
	defer tx.Rollback()

	nowValue := databaseTime(now)
	blockedUntil := databaseTime(now.Add(5 * time.Minute))
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO pin_attempts(device_id, failed_count, blocked_until, updated_at)
		 VALUES (?, 1, NULL, ?)
		 ON CONFLICT(device_id) DO UPDATE SET
		   failed_count = CASE
		     WHEN pin_attempts.blocked_until IS NOT NULL
		      AND pin_attempts.blocked_until <= excluded.updated_at THEN 1
		     ELSE pin_attempts.failed_count + 1
		   END,
		   blocked_until = CASE
		     WHEN (
		       CASE
		         WHEN pin_attempts.blocked_until IS NOT NULL
		          AND pin_attempts.blocked_until <= excluded.updated_at THEN 1
		         ELSE pin_attempts.failed_count + 1
		       END
		     ) >= 5 THEN ?
		     ELSE NULL
		   END,
		   updated_at = excluded.updated_at`,
		deviceID,
		nowValue,
		blockedUntil,
	); err != nil {
		return false, fmt.Errorf("record PIN failure: %w", err)
	}

	var blocked sql.NullString
	if err := tx.QueryRowContext(
		ctx,
		`SELECT blocked_until FROM pin_attempts WHERE device_id = ?`,
		deviceID,
	).Scan(&blocked); err != nil {
		return false, fmt.Errorf("read PIN failure result: %w", err)
	}
	result := "failure"
	if blocked.Valid {
		result = "blocked"
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('pin_unlock', ?, ?, 'invalid_pin', ?)`,
		deviceID,
		result,
		nowValue,
	); err != nil {
		return false, fmt.Errorf("record PIN event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit PIN failure: %w", err)
	}
	return blocked.Valid, nil
}

func (d *Database) CreateAdminSession(
	ctx context.Context,
	deviceID string,
	sessionID string,
	sessionHash [32]byte,
	csrfHash [32]byte,
	idleExpiresAt time.Time,
	absoluteExpiresAt time.Time,
	now time.Time,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin admin session: %w", err)
	}
	defer tx.Rollback()
	nowValue := databaseTime(now)
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO pin_attempts(device_id, failed_count, blocked_until, updated_at)
		 VALUES (?, 0, NULL, ?)
		 ON CONFLICT(device_id) DO UPDATE SET
		   failed_count = 0, blocked_until = NULL, updated_at = excluded.updated_at`,
		deviceID,
		nowValue,
	); err != nil {
		return fmt.Errorf("reset PIN attempts: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE admin_sessions SET revoked_at = ?
		 WHERE device_id = ? AND revoked_at IS NULL`,
		nowValue,
		deviceID,
	); err != nil {
		return fmt.Errorf("revoke prior admin sessions: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO admin_sessions(
		   id, device_id, session_hash, csrf_hash, idle_expires_at,
		   absolute_expires_at, created_at, last_used_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		deviceID,
		sessionHash[:],
		csrfHash[:],
		databaseTime(idleExpiresAt),
		databaseTime(absoluteExpiresAt),
		nowValue,
		nowValue,
	); err != nil {
		return fmt.Errorf("create admin session: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('admin_unlock', ?, 'success', 'pin_verified', ?)`,
		deviceID,
		nowValue,
	); err != nil {
		return fmt.Errorf("record admin unlock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin session: %w", err)
	}
	return nil
}

func (d *Database) ValidateAdminSession(
	ctx context.Context,
	deviceID string,
	sessionHash [32]byte,
	csrfHash *[32]byte,
	now time.Time,
	refreshIdle bool,
) (bool, error) {
	var storedCSRF []byte
	var absoluteValue string
	err := d.db.QueryRowContext(
		ctx,
		`SELECT csrf_hash, absolute_expires_at
		 FROM admin_sessions
		 WHERE device_id = ? AND session_hash = ? AND revoked_at IS NULL
		   AND idle_expires_at > ? AND absolute_expires_at > ?`,
		deviceID,
		sessionHash[:],
		databaseTime(now),
		databaseTime(now),
	).Scan(&storedCSRF, &absoluteValue)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate admin session: %w", err)
	}
	if csrfHash != nil && subtle.ConstantTimeCompare(storedCSRF, csrfHash[:]) != 1 {
		return false, nil
	}
	if refreshIdle {
		absolute, err := time.Parse(time.RFC3339Nano, absoluteValue)
		if err != nil {
			return false, fmt.Errorf("parse admin expiry: %w", err)
		}
		idle := now.Add(10 * time.Minute)
		if idle.After(absolute) {
			idle = absolute
		}
		if _, err := d.db.ExecContext(
			ctx,
			`UPDATE admin_sessions SET idle_expires_at = ?, last_used_at = ?
			 WHERE device_id = ? AND session_hash = ? AND revoked_at IS NULL`,
			databaseTime(idle),
			databaseTime(now),
			deviceID,
			sessionHash[:],
		); err != nil {
			return false, fmt.Errorf("refresh admin session: %w", err)
		}
	}
	return true, nil
}

func (d *Database) RevokeAdminSession(
	ctx context.Context,
	deviceID string,
	sessionHash [32]byte,
	now time.Time,
) error {
	nowValue := databaseTime(now)
	result, err := d.db.ExecContext(
		ctx,
		`UPDATE admin_sessions SET revoked_at = ?
		 WHERE device_id = ? AND session_hash = ? AND revoked_at IS NULL`,
		nowValue,
		deviceID,
		sessionHash[:],
	)
	if err != nil {
		return fmt.Errorf("revoke admin session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read admin revoke result: %w", err)
	}
	if changed == 1 {
		if _, err := d.db.ExecContext(
			ctx,
			`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
			 VALUES ('admin_lock', ?, 'success', 'explicit_lock', ?)`,
			deviceID,
			nowValue,
		); err != nil {
			return fmt.Errorf("record admin lock: %w", err)
		}
	}
	return nil
}
