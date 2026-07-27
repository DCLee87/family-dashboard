package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrRefreshReuse = errors.New("refresh token reuse detected")

type CredentialRotation struct {
	AccessID         string
	AccessHash       [32]byte
	AccessExpiresAt  time.Time
	RefreshID        string
	RefreshHash      [32]byte
	RefreshExpiresAt time.Time
}

func (d *Database) RotateRefreshCredential(
	ctx context.Context,
	oldHash [32]byte,
	rotation CredentialRotation,
	now time.Time,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin credential rotation: %w", err)
	}
	defer tx.Rollback()

	var deviceID string
	var familyID string
	var consumedAt sql.NullString
	var revokedAt sql.NullString
	var expiresValue string
	var deviceStatus string
	err = tx.QueryRowContext(
		ctx,
		`SELECT c.device_id, c.family_id, c.consumed_at, c.revoked_at,
		        c.expires_at, d.status
		 FROM device_credentials c
		 JOIN devices d ON d.id = c.device_id
		 WHERE c.credential_type = 'refresh' AND c.token_hash = ?`,
		oldHash[:],
	).Scan(
		&deviceID,
		&familyID,
		&consumedAt,
		&revokedAt,
		&expiresValue,
		&deviceStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("read refresh credential: %w", err)
	}
	nowValue := databaseTime(now)
	if consumedAt.Valid {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE device_credentials SET revoked_at = ?
			 WHERE family_id = ? AND revoked_at IS NULL`,
			nowValue,
			familyID,
		); err != nil {
			return fmt.Errorf("revoke reused credential family: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE admin_sessions SET revoked_at = ?
			 WHERE device_id = ? AND revoked_at IS NULL`,
			nowValue,
			deviceID,
		); err != nil {
			return fmt.Errorf("revoke reused credential sessions: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
			 VALUES ('token_refresh', ?, 'failure', 'refresh_reuse_detected', ?)`,
			deviceID,
			nowValue,
		); err != nil {
			return fmt.Errorf("record refresh reuse: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit refresh reuse revocation: %w", err)
		}
		return ErrRefreshReuse
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresValue)
	if err != nil {
		return fmt.Errorf("parse refresh expiry: %w", err)
	}
	if revokedAt.Valid || deviceStatus != "active" || !now.Before(expiresAt) {
		return ErrUnauthenticated
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE device_credentials SET consumed_at = ?
		 WHERE token_hash = ? AND consumed_at IS NULL AND revoked_at IS NULL`,
		nowValue,
		oldHash[:],
	)
	if err != nil {
		return fmt.Errorf("consume refresh credential: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read refresh consume result: %w", err)
	}
	if changed != 1 {
		return ErrUnauthenticated
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE device_credentials SET revoked_at = ?
		 WHERE device_id = ? AND credential_type = 'access'
		   AND revoked_at IS NULL`,
		nowValue,
		deviceID,
	); err != nil {
		return fmt.Errorf("revoke prior access credentials: %w", err)
	}
	for _, credential := range []struct {
		id, kind string
		hash     [32]byte
		expires  time.Time
	}{
		{rotation.AccessID, "access", rotation.AccessHash, rotation.AccessExpiresAt},
		{rotation.RefreshID, "refresh", rotation.RefreshHash, rotation.RefreshExpiresAt},
	} {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO device_credentials(
			   id, device_id, credential_type, token_hash, family_id,
			   expires_at, created_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			credential.id,
			deviceID,
			credential.kind,
			credential.hash[:],
			familyID,
			databaseTime(credential.expires),
			nowValue,
		); err != nil {
			return fmt.Errorf("create rotated credential: %w", err)
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('token_refresh', ?, 'success', 'credentials_rotated', ?)`,
		deviceID,
		nowValue,
	); err != nil {
		return fmt.Errorf("record credential rotation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit credential rotation: %w", err)
	}
	return nil
}
