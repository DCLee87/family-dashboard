package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Database struct {
	db *sql.DB
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
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
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
	}
	if recordStart {
		statements = append(statements, `INSERT INTO runtime_state(id, start_count, last_started_at)
		 VALUES (1, 1, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET
		   start_count = start_count + 1,
		   last_started_at = CURRENT_TIMESTAMP`)
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite: %w", err)
		}
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

func (d *Database) Close() error {
	return d.db.Close()
}
