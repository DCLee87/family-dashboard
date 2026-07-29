package database

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Database struct {
	db *sql.DB
}

var ErrInvalidInitialSetup = errors.New("invalid or expired initial setup")
var ErrUnauthenticated = errors.New("device is not authenticated")

type Device struct {
	ID        string
	Name      string
	Type      string
	Owner     sql.NullString
	LocalOnly bool
}

type InitialSetup struct {
	CodeHash         [32]byte
	PINHash          string
	DeviceID         string
	DeviceName       string
	AccessID         string
	AccessHash       [32]byte
	AccessExpiresAt  time.Time
	RefreshID        string
	RefreshHash      [32]byte
	RefreshFamilyID  string
	RefreshExpiresAt time.Time
	RecoveryID       string
	RecoveryHash     [32]byte
	Now              time.Time
}

func Open(dataDir string, recordStart bool) (*Database, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	path := filepath.Join(dataDir, "family-dashboard.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	database := &Database{db: db}
	if err := database.initialize(context.Background(), recordStart); err != nil {
		db.Close()
		return nil, err
	}
	return database, nil
}

func (d *Database) initialize(ctx context.Context, recordStart bool) error {
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, statement := range pragmas {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin database migration: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS runtime_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			start_count INTEGER NOT NULL DEFAULT 0,
			last_started_at TEXT NOT NULL
		)`,
		`INSERT INTO schema_migrations(version) VALUES (1)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS security_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			setup_complete INTEGER NOT NULL DEFAULT 0 CHECK (setup_complete IN (0, 1)),
			pin_hash TEXT,
			initial_code_hash BLOB,
			initial_code_expires_at TEXT,
			initial_code_consumed_at TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CHECK (pin_hash IS NULL OR setup_complete = 1),
			CHECK (initial_code_hash IS NULL OR length(initial_code_hash) = 32)
		)`,
		`INSERT INTO security_state(id) VALUES (1)
		 ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
			device_type TEXT NOT NULL CHECK (
				device_type IN ('trusted_pc', 'parent_mobile', 'shared_tablet', 'tv')
			),
			owner TEXT CHECK (owner IN ('dad', 'mom') OR owner IS NULL),
			local_only INTEGER NOT NULL CHECK (local_only IN (0, 1)),
			status TEXT NOT NULL DEFAULT 'active' CHECK (
				status IN ('active', 'revoked')
			),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at TEXT,
			revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS device_credentials (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			credential_type TEXT NOT NULL CHECK (
				credential_type IN ('access', 'refresh')
			),
			token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
			family_id TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			consumed_at TEXT,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS device_credentials_device
		 ON device_credentials(device_id, credential_type)`,
		`CREATE TABLE IF NOT EXISTS enrollment_requests (
			id TEXT PRIMARY KEY,
			code_hash BLOB NOT NULL UNIQUE CHECK (length(code_hash) = 32),
			requested_type TEXT NOT NULL CHECK (
				requested_type IN ('parent_mobile', 'shared_tablet', 'tv')
			),
			device_name TEXT,
			owner TEXT CHECK (owner IN ('dad', 'mom') OR owner IS NULL),
			status TEXT NOT NULL DEFAULT 'pending' CHECK (
				status IN ('pending', 'submitted', 'approved', 'rejected', 'expired')
			),
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			submitted_at TEXT,
			decided_at TEXT,
			approved_by_device_id TEXT REFERENCES devices(id)
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			session_hash BLOB NOT NULL UNIQUE CHECK (length(session_hash) = 32),
			csrf_hash BLOB NOT NULL CHECK (length(csrf_hash) = 32),
			idle_expires_at TEXT NOT NULL,
			absolute_expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS pin_attempts (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
			blocked_until TEXT,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS recovery_codes (
			id TEXT PRIMARY KEY,
			code_hash BLOB NOT NULL UNIQUE CHECK (length(code_hash) = 32),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			consumed_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS security_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			device_id TEXT REFERENCES devices(id) ON DELETE SET NULL,
			result TEXT NOT NULL CHECK (result IN ('success', 'failure', 'blocked')),
			reason TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS security_events_created
		 ON security_events(created_at)`,
		`INSERT INTO schema_migrations(version) VALUES (2)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			endpoint TEXT NOT NULL,
			endpoint_hash BLOB NOT NULL UNIQUE CHECK (length(endpoint_hash) = 32),
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS push_subscriptions_active_device
		 ON push_subscriptions(device_id) WHERE revoked_at IS NULL`,
		`INSERT INTO schema_migrations(version) VALUES (3)
		 ON CONFLICT(version) DO NOTHING`,
	}
	if recordStart {
		statements = append(statements, `INSERT INTO runtime_state(id, start_count, last_started_at)
		 VALUES (1, 1, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET
		   start_count = start_count + 1,
		   last_started_at = CURRENT_TIMESTAMP`)
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit database migration: %w", err)
	}
	return nil
}

func (d *Database) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *Database) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := d.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity check result: %s", result)
	}
	return nil
}

func (d *Database) StartCount(ctx context.Context) (int64, error) {
	var count int64
	if err := d.db.QueryRowContext(ctx, `SELECT start_count FROM runtime_state WHERE id = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("read start count: %w", err)
	}
	return count, nil
}

func (d *Database) SetupRequired(ctx context.Context) (bool, error) {
	var complete int
	if err := d.db.QueryRowContext(
		ctx,
		`SELECT setup_complete FROM security_state WHERE id = 1`,
	).Scan(&complete); err != nil {
		return false, fmt.Errorf("read setup state: %w", err)
	}
	return complete == 0, nil
}

func (d *Database) EnsureInitialSetupCode(
	ctx context.Context,
	hash [32]byte,
	expiresAt time.Time,
) (bool, error) {
	now := time.Now().UTC()
	result, err := d.db.ExecContext(
		ctx,
		`UPDATE security_state
		 SET initial_code_hash = ?, initial_code_expires_at = ?,
		     initial_code_consumed_at = NULL, updated_at = ?
		 WHERE id = 1 AND setup_complete = 0
		   AND (initial_code_hash IS NULL OR initial_code_expires_at <= ?)`,
		hash[:],
		databaseTime(expiresAt),
		databaseTime(now),
		databaseTime(now),
	)
	if err != nil {
		return false, fmt.Errorf("store initial setup code: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read initial setup code result: %w", err)
	}
	return changed == 1, nil
}

func (d *Database) ValidateInitialSetupCode(
	ctx context.Context,
	hash [32]byte,
	now time.Time,
) (bool, error) {
	var storedHash []byte
	var expiresAt sql.NullString
	var setupComplete int
	var consumedAt sql.NullString
	if err := d.db.QueryRowContext(
		ctx,
		`SELECT setup_complete, initial_code_hash, initial_code_expires_at,
		        initial_code_consumed_at
		 FROM security_state WHERE id = 1`,
	).Scan(&setupComplete, &storedHash, &expiresAt, &consumedAt); err != nil {
		return false, fmt.Errorf("read initial setup code: %w", err)
	}
	if setupComplete != 0 || consumedAt.Valid || !expiresAt.Valid {
		return false, nil
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt.String)
	if err != nil {
		return false, fmt.Errorf("parse initial setup expiry: %w", err)
	}
	valid := now.Before(expires) &&
		subtle.ConstantTimeCompare(storedHash, hash[:]) == 1
	return valid, nil
}

func (d *Database) CompleteInitialSetup(ctx context.Context, setup InitialSetup) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin initial setup: %w", err)
	}
	defer tx.Rollback()

	var storedHash []byte
	var expiresAt sql.NullString
	var setupComplete int
	var consumedAt sql.NullString
	if err := tx.QueryRowContext(
		ctx,
		`SELECT setup_complete, initial_code_hash, initial_code_expires_at,
		        initial_code_consumed_at
		 FROM security_state WHERE id = 1`,
	).Scan(&setupComplete, &storedHash, &expiresAt, &consumedAt); err != nil {
		return fmt.Errorf("read initial setup: %w", err)
	}
	var expires time.Time
	if expiresAt.Valid {
		expires, err = time.Parse(time.RFC3339Nano, expiresAt.String)
	}
	if !expiresAt.Valid || err != nil ||
		setupComplete != 0 ||
		consumedAt.Valid ||
		!setup.Now.Before(expires) ||
		subtle.ConstantTimeCompare(storedHash, setup.CodeHash[:]) != 1 {
		return ErrInvalidInitialSetup
	}

	now := databaseTime(setup.Now)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE security_state
		 SET setup_complete = 1, pin_hash = ?, initial_code_hash = NULL,
		     initial_code_consumed_at = ?, updated_at = ?
		 WHERE id = 1 AND setup_complete = 0`,
		setup.PINHash,
		now,
		now,
	); err != nil {
		return fmt.Errorf("complete security setup: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO devices(
			id, name, device_type, owner, local_only, status, created_at, last_used_at
		 ) VALUES (?, ?, 'trusted_pc', NULL, 0, 'active', ?, ?)`,
		setup.DeviceID,
		setup.DeviceName,
		now,
		now,
	); err != nil {
		return fmt.Errorf("create trusted PC: %w", err)
	}
	credentials := []struct {
		id             string
		credentialType string
		hash           [32]byte
		familyID       string
		expiresAt      time.Time
	}{
		{
			id:             setup.AccessID,
			credentialType: "access",
			hash:           setup.AccessHash,
			familyID:       setup.RefreshFamilyID,
			expiresAt:      setup.AccessExpiresAt,
		},
		{
			id:             setup.RefreshID,
			credentialType: "refresh",
			hash:           setup.RefreshHash,
			familyID:       setup.RefreshFamilyID,
			expiresAt:      setup.RefreshExpiresAt,
		},
	}
	for _, credential := range credentials {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO device_credentials(
				id, device_id, credential_type, token_hash, family_id,
				expires_at, created_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			credential.id,
			setup.DeviceID,
			credential.credentialType,
			credential.hash[:],
			credential.familyID,
			databaseTime(credential.expiresAt),
			now,
		); err != nil {
			return fmt.Errorf("create device credential: %w", err)
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO recovery_codes(id, code_hash, created_at)
		 VALUES (?, ?, ?)`,
		setup.RecoveryID,
		setup.RecoveryHash[:],
		now,
	); err != nil {
		return fmt.Errorf("create recovery code: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('initial_setup', ?, 'success', 'trusted_pc_created', ?)`,
		setup.DeviceID,
		now,
	); err != nil {
		return fmt.Errorf("record initial setup event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit initial setup: %w", err)
	}
	return nil
}

func (d *Database) DeviceByAccessToken(
	ctx context.Context,
	hash [32]byte,
	now time.Time,
) (Device, error) {
	var device Device
	var localOnly int
	err := d.db.QueryRowContext(
		ctx,
		`SELECT d.id, d.name, d.device_type, d.owner, d.local_only
		 FROM device_credentials c
		 JOIN devices d ON d.id = c.device_id
		 WHERE c.credential_type = 'access'
		   AND c.token_hash = ?
		   AND c.consumed_at IS NULL
		   AND c.revoked_at IS NULL
		   AND c.expires_at > ?
		   AND d.status = 'active'`,
		hash[:],
		databaseTime(now),
	).Scan(
		&device.ID,
		&device.Name,
		&device.Type,
		&device.Owner,
		&localOnly,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrUnauthenticated
	}
	if err != nil {
		return Device{}, fmt.Errorf("authenticate device: %w", err)
	}
	device.LocalOnly = localOnly == 1
	if _, err := d.db.ExecContext(
		ctx,
		`UPDATE devices SET last_used_at = ? WHERE id = ?`,
		databaseTime(now),
		device.ID,
	); err != nil {
		return Device{}, fmt.Errorf("update device use: %w", err)
	}
	return device, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func databaseTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
