package database

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestDatabasePersistsStartCount(t *testing.T) {
	dataDir := t.TempDir()

	first, err := Open(dataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	check, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	count, err := check.StartCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("check-only open changed start count: got %d, want 1", count)
	}
	if err := check.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := check.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(dataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	count, err = second.StartCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("start count did not persist: got %d, want 2", count)
	}
}

func TestExpiredInitialSetupCodeRotates(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var first [32]byte
	first[0] = 1
	created, err := database.EnsureInitialSetupCode(
		context.Background(),
		first,
		time.Now().Add(-time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first setup code was not stored")
	}

	var second [32]byte
	second[0] = 2
	created, err = database.EnsureInitialSetupCode(
		context.Background(),
		second,
		time.Now().Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expired setup code was not rotated")
	}

	var stored []byte
	if err := database.db.QueryRow(
		`SELECT initial_code_hash FROM security_state WHERE id = 1`,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, second[:]) {
		t.Fatal("rotated setup code hash was not stored")
	}
}

func TestDatabaseCreatesSecuritySchema(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var version int
	if err := database.db.QueryRow(
		`SELECT MAX(version) FROM schema_migrations`,
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("schema version: got %d, want 2", version)
	}

	tables := []string{
		"security_state",
		"devices",
		"device_credentials",
		"enrollment_requests",
		"admin_sessions",
		"pin_attempts",
		"recovery_codes",
		"security_events",
	}
	for _, table := range tables {
		var count int
		if err := database.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %q was not created", table)
		}
	}

	if _, err := database.db.Exec(
		`INSERT INTO devices(id, name, device_type, local_only)
		 VALUES ('invalid', 'Invalid', 'unknown', 0)`,
	); err == nil {
		t.Fatal("invalid device type was accepted")
	}
}

func TestDatabaseMigratesE1Schema(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "family-dashboard.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO schema_migrations(version) VALUES (1)`,
		`CREATE TABLE runtime_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			start_count INTEGER NOT NULL DEFAULT 0,
			last_started_at TEXT NOT NULL
		)`,
		`INSERT INTO runtime_state(id, start_count, last_started_at)
		 VALUES (1, 7, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()

	count, err := migrated.StartCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("migration changed start count: got %d, want 7", count)
	}
	var version int
	if err := migrated.db.QueryRow(
		`SELECT MAX(version) FROM schema_migrations`,
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("migrated schema version: got %d, want 2", version)
	}
}
