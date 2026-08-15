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
		`CREATE TABLE IF NOT EXISTS family_members (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE CHECK (length(slug) BETWEEN 1 AND 40),
			display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
			role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
			active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO family_members(id, slug, display_name, role) VALUES
		 ('dad', 'dad', '아빠', 'parent'),
		 ('mom', 'mom', '엄마', 'parent'),
		 ('daughter', 'daughter', '딸', 'child')
		 ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
			location_name TEXT CHECK (location_name IS NULL OR length(location_name) <= 200),
			notes TEXT CHECK (notes IS NULL OR length(notes) <= 2000),
			visibility TEXT NOT NULL DEFAULT 'family' CHECK (
				visibility IN ('family', 'tv_summary', 'parents_only')
			),
			time_kind TEXT NOT NULL DEFAULT 'timed' CHECK (time_kind = 'timed'),
			starts_at TEXT NOT NULL,
			ends_at TEXT NOT NULL,
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT,
			CHECK (ends_at > starts_at)
		)`,
		`CREATE INDEX IF NOT EXISTS schedules_time_range
		 ON schedules(starts_at, ends_at) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS schedule_participants (
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			family_member_id TEXT NOT NULL REFERENCES family_members(id),
			PRIMARY KEY(schedule_id, family_member_id)
		)`,
		`CREATE INDEX IF NOT EXISTS schedule_participants_member
		 ON schedule_participants(family_member_id, schedule_id)`,
		`INSERT INTO schema_migrations(version) VALUES (4)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_recurrence_rules (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			recurrence_kind TEXT NOT NULL CHECK (recurrence_kind = 'weekly'),
			starts_on TEXT NOT NULL,
			ends_on TEXT,
			start_minute INTEGER NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
			end_minute INTEGER NOT NULL CHECK (end_minute BETWEEN 0 AND 1439),
			timezone TEXT NOT NULL DEFAULT 'Asia/Seoul',
			CHECK (ends_on IS NULL OR ends_on >= starts_on)
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_recurrence_days (
			schedule_id TEXT NOT NULL REFERENCES schedule_recurrence_rules(schedule_id) ON DELETE CASCADE,
			weekday INTEGER NOT NULL CHECK (weekday BETWEEN 0 AND 6),
			PRIMARY KEY(schedule_id, weekday)
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_occurrence_exceptions (
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			occurrence_key TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('cancelled', 'overridden')),
			override_title TEXT,
			override_location_name TEXT,
			override_notes TEXT,
			override_visibility TEXT CHECK (
				override_visibility IN ('family', 'tv_summary', 'parents_only') OR override_visibility IS NULL
			),
			override_starts_at TEXT,
			override_ends_at TEXT,
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			CHECK (
				(status = 'cancelled' AND override_title IS NULL AND override_location_name IS NULL AND
				 override_notes IS NULL AND override_visibility IS NULL AND override_starts_at IS NULL AND override_ends_at IS NULL)
				OR
				(status = 'overridden' AND override_starts_at IS NOT NULL AND override_ends_at IS NOT NULL AND override_ends_at > override_starts_at)
			),
			PRIMARY KEY(schedule_id, occurrence_key)
		)`,
		`CREATE INDEX IF NOT EXISTS schedule_occurrence_exceptions_schedule
		 ON schedule_occurrence_exceptions(schedule_id, occurrence_key)`,
		`INSERT INTO schema_migrations(version) VALUES (5)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_all_day_dates (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			start_date TEXT NOT NULL,
			end_date TEXT NOT NULL,
			CHECK (end_date >= start_date)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (6)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_occurrence_all_day_overrides (
			schedule_id TEXT NOT NULL,
			occurrence_key TEXT NOT NULL,
			start_date TEXT NOT NULL,
			end_date TEXT NOT NULL,
			CHECK (end_date >= start_date),
			PRIMARY KEY(schedule_id, occurrence_key),
			FOREIGN KEY(schedule_id, occurrence_key)
			 REFERENCES schedule_occurrence_exceptions(schedule_id, occurrence_key) ON DELETE CASCADE
		)`,
		`INSERT INTO schema_migrations(version) VALUES (7)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_deletion_metadata (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			deleted_by_device_id TEXT NOT NULL REFERENCES devices(id),
			deleted_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS schedule_deletion_metadata_deleted
		 ON schedule_deletion_metadata(deleted_at)`,
		`INSERT INTO schema_migrations(version) VALUES (8)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_notification_settings (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
			timed_lead_minutes INTEGER NOT NULL DEFAULT 30 CHECK (timed_lead_minutes BETWEEN 0 AND 10080),
			all_day_hour INTEGER NOT NULL DEFAULT 20 CHECK (all_day_hour BETWEEN 0 AND 23),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT OR IGNORE INTO schedule_notification_settings(schedule_id)
		 SELECT id FROM schedules`,
		`CREATE TABLE IF NOT EXISTS schedule_notification_deliveries (
			id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			occurrence_key TEXT NOT NULL DEFAULT '',
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			notify_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			sent_at TEXT,
			UNIQUE(schedule_id, occurrence_key, device_id, notify_at)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (9)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
			notes TEXT CHECK (notes IS NULL OR length(notes) <= 2000),
			priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal', 'important')),
			due_kind TEXT NOT NULL DEFAULT 'none' CHECK (due_kind IN ('none', 'date', 'datetime')),
			due_date TEXT,
			due_minute INTEGER CHECK (due_minute IS NULL OR due_minute BETWEEN 0 AND 1439),
			repeat_kind TEXT NOT NULL DEFAULT 'none' CHECK (repeat_kind IN ('none', 'daily', 'weekly')),
			starts_on TEXT,
			ends_on TEXT,
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			completed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT,
			CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on)
		)`,
		`CREATE TABLE IF NOT EXISTS task_assignees (
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			family_member_id TEXT NOT NULL REFERENCES family_members(id),
			PRIMARY KEY(task_id, family_member_id)
		)`,
		`CREATE TABLE IF NOT EXISTS task_recurrence_days (
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			weekday INTEGER NOT NULL CHECK (weekday BETWEEN 0 AND 6),
			PRIMARY KEY(task_id, weekday)
		)`,
		`CREATE TABLE IF NOT EXISTS task_occurrence_states (
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			occurrence_key TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('completed', 'skipped')),
			completed_at TEXT,
			completed_by_device_id TEXT REFERENCES devices(id),
			updated_at TEXT NOT NULL,
			PRIMARY KEY(task_id, occurrence_key)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (10)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS task_notification_settings (
			task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
			timed_lead_minutes INTEGER NOT NULL DEFAULT 60 CHECK (timed_lead_minutes BETWEEN 0 AND 10080),
			date_hour INTEGER NOT NULL DEFAULT 9 CHECK (date_hour BETWEEN 0 AND 23),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS task_notification_recipients (
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			owner TEXT NOT NULL CHECK (owner IN ('dad', 'mom')),
			PRIMARY KEY(task_id, owner)
		)`,
		`CREATE TABLE IF NOT EXISTS task_notification_deliveries (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			occurrence_key TEXT NOT NULL,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			notify_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			sent_at TEXT,
			UNIQUE(task_id, occurrence_key, device_id, notify_at)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (11)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS board_items (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK (kind IN ('notice', 'memo')),
			title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
			body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
			priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal', 'important')),
			visibility TEXT NOT NULL DEFAULT 'family' CHECK (visibility IN ('family', 'tv_summary', 'parents_only')),
			starts_on TEXT NOT NULL,
			ends_on TEXT,
			push_enabled INTEGER NOT NULL DEFAULT 0 CHECK (push_enabled IN (0, 1)),
			status TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'archived', 'trashed')),
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived_at TEXT,
			deleted_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS board_items_status_dates
		 ON board_items(status, starts_on, ends_on)`,
		`CREATE TABLE IF NOT EXISTS board_push_deliveries (
			board_item_id TEXT NOT NULL REFERENCES board_items(id) ON DELETE CASCADE,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			sent_at TEXT,
			PRIMARY KEY(board_item_id, device_id)
		)`,
		`CREATE TABLE IF NOT EXISTS places (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
			region_label TEXT NOT NULL CHECK (length(region_label) BETWEEN 1 AND 120),
			latitude REAL NOT NULL CHECK (latitude BETWEEN -90 AND 90),
			longitude REAL NOT NULL CHECK (longitude BETWEEN -180 AND 180),
			is_home INTEGER NOT NULL DEFAULT 0 CHECK (is_home IN (0, 1)),
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS places_single_home ON places(is_home) WHERE is_home = 1`,
		`CREATE TABLE IF NOT EXISTS weather_cache (
			place_id TEXT PRIMARY KEY REFERENCES places(id) ON DELETE CASCADE,
			payload TEXT NOT NULL,
			fetched_at TEXT NOT NULL,
			last_error_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS dashboard_preferences (
			device_type TEXT PRIMARY KEY CHECK (device_type IN ('trusted_pc', 'parent_mobile', 'shared_tablet', 'tv')),
			widget_order TEXT NOT NULL,
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_at TEXT NOT NULL
		)`,
		`INSERT INTO schema_migrations(version) VALUES (12)
		 ON CONFLICT(version) DO NOTHING`,
		`DELETE FROM schedule_recurrence_days
		 WHERE NOT EXISTS (SELECT 1 FROM schedule_recurrence_rules r WHERE r.schedule_id = schedule_recurrence_days.schedule_id)`,
		`DELETE FROM schedule_occurrence_all_day_overrides
		 WHERE NOT EXISTS (SELECT 1 FROM schedule_occurrence_exceptions e
		  WHERE e.schedule_id = schedule_occurrence_all_day_overrides.schedule_id
		    AND e.occurrence_key = schedule_occurrence_all_day_overrides.occurrence_key)`,
		`DELETE FROM schedule_participants
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_participants.schedule_id)`,
		`DELETE FROM schedule_recurrence_rules
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_recurrence_rules.schedule_id)`,
		`DELETE FROM schedule_occurrence_exceptions
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_occurrence_exceptions.schedule_id)`,
		`DELETE FROM schedule_all_day_dates
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_all_day_dates.schedule_id)`,
		`DELETE FROM schedule_deletion_metadata
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_deletion_metadata.schedule_id)`,
		`DELETE FROM schedule_notification_settings
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_notification_settings.schedule_id)`,
		`DELETE FROM schedule_notification_deliveries
		 WHERE NOT EXISTS (SELECT 1 FROM schedules s WHERE s.id = schedule_notification_deliveries.schedule_id)`,
		`DELETE FROM task_assignees
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_assignees.task_id)`,
		`DELETE FROM task_recurrence_days
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_recurrence_days.task_id)`,
		`DELETE FROM task_occurrence_states
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_occurrence_states.task_id)`,
		`DELETE FROM task_notification_settings
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_notification_settings.task_id)`,
		`DELETE FROM task_notification_recipients
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_notification_recipients.task_id)`,
		`DELETE FROM task_notification_deliveries
		 WHERE NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = task_notification_deliveries.task_id)`,
		`DELETE FROM board_push_deliveries
		 WHERE NOT EXISTS (SELECT 1 FROM board_items b WHERE b.id = board_push_deliveries.board_item_id)`,
		`DELETE FROM weather_cache
		 WHERE NOT EXISTS (SELECT 1 FROM places p WHERE p.id = weather_cache.place_id)`,
		`CREATE TABLE IF NOT EXISTS schedule_places (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			place_id TEXT NOT NULL REFERENCES places(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS schedule_places_place ON schedule_places(place_id)`,
		`CREATE TABLE IF NOT EXISTS schedule_notification_times (
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			kind TEXT NOT NULL CHECK (kind IN ('timed', 'all_day')),
			value INTEGER NOT NULL CHECK (value BETWEEN 0 AND 10080),
			PRIMARY KEY(schedule_id, kind, value)
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_notification_recipients (
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			owner TEXT NOT NULL CHECK (owner IN ('dad', 'mom')),
			PRIMARY KEY(schedule_id, owner)
		)`,
		`INSERT OR IGNORE INTO schedule_notification_times(schedule_id,kind,value)
		 SELECT schedule_id,'timed',timed_lead_minutes FROM schedule_notification_settings s WHERE enabled=1
		 AND NOT EXISTS (SELECT 1 FROM schedule_notification_times t WHERE t.schedule_id=s.schedule_id AND t.kind='timed')`,
		`INSERT OR IGNORE INTO schedule_notification_times(schedule_id,kind,value)
		 SELECT schedule_id,'all_day',all_day_hour FROM schedule_notification_settings s WHERE enabled=1
		 AND NOT EXISTS (SELECT 1 FROM schedule_notification_times t WHERE t.schedule_id=s.schedule_id AND t.kind='all_day')`,
		`INSERT OR IGNORE INTO schedule_notification_recipients(schedule_id,owner)
		 SELECT s.schedule_id,o.owner FROM schedule_notification_settings s
		 CROSS JOIN (SELECT 'dad' AS owner UNION ALL SELECT 'mom') o WHERE s.enabled=1
		 AND NOT EXISTS (SELECT 1 FROM schedule_notification_recipients r WHERE r.schedule_id=s.schedule_id)`,
		`CREATE TABLE IF NOT EXISTS notification_worker_state (
			worker TEXT PRIMARY KEY,
			last_checked_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS device_weather_preferences (
			device_type TEXT PRIMARY KEY CHECK (device_type IN ('trusted_pc','parent_mobile','shared_tablet','tv')),
			daily_days INTEGER NOT NULL CHECK (daily_days BETWEEN 1 AND 7),
			hourly_hours INTEGER NOT NULL CHECK (hourly_hours BETWEEN 0 AND 48),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS board_notification_recipients (
			board_item_id TEXT NOT NULL REFERENCES board_items(id) ON DELETE CASCADE,
			owner TEXT NOT NULL CHECK (owner IN ('dad','mom')),
			PRIMARY KEY(board_item_id,owner)
		)`,
		`INSERT INTO schema_migrations(version) VALUES (13)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS finance_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			salary_amount INTEGER NOT NULL DEFAULT 0 CHECK (salary_amount >= 0),
			budget_start_day INTEGER NOT NULL DEFAULT 21 CHECK (budget_start_day BETWEEN 1 AND 28),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO finance_settings(id) VALUES (1) ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS finance_transactions (
			id TEXT PRIMARY KEY,
			amount INTEGER NOT NULL CHECK (amount > 0),
			category TEXT NOT NULL CHECK (category IN ('food','living','transport','education','medical','leisure','utilities','loan_payment','savings','other')),
			payer TEXT NOT NULL CHECK (payer IN ('dad','mom','family')),
			occurred_on TEXT NOT NULL,
			memo TEXT CHECK (memo IS NULL OR length(memo) <= 500),
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS finance_transactions_date ON finance_transactions(occurred_on DESC, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS finance_accounts (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK (kind IN ('loan','installment_savings')),
			name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
			balance_amount INTEGER NOT NULL DEFAULT 0 CHECK (balance_amount >= 0),
			interest_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (interest_basis_points BETWEEN 0 AND 100000),
			monthly_amount INTEGER NOT NULL DEFAULT 0 CHECK (monthly_amount >= 0),
			payment_day INTEGER NOT NULL DEFAULT 21 CHECK (payment_day BETWEEN 1 AND 31),
			started_on TEXT,
			maturity_on TEXT,
			notes TEXT CHECK (notes IS NULL OR length(notes) <= 1000),
			created_by_device_id TEXT NOT NULL REFERENCES devices(id),
			updated_by_device_id TEXT NOT NULL REFERENCES devices(id),
			version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			CHECK (maturity_on IS NULL OR started_on IS NULL OR maturity_on >= started_on)
		)`,
		`CREATE INDEX IF NOT EXISTS finance_accounts_kind ON finance_accounts(kind, created_at)`,
		`INSERT INTO schema_migrations(version) VALUES (14)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS schedule_tags (
			schedule_id TEXT PRIMARY KEY REFERENCES schedules(id) ON DELETE CASCADE,
			tag TEXT NOT NULL DEFAULT 'general' CHECK (tag IN ('general','academy','after_school'))
		)`,
		`INSERT OR IGNORE INTO schedule_tags(schedule_id,tag)
		 SELECT id,'general' FROM schedules`,
		`CREATE INDEX IF NOT EXISTS schedule_tags_tag ON schedule_tags(tag,schedule_id)`,
		`INSERT INTO schema_migrations(version) VALUES (15)
		 ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS parent_accounts (
			owner TEXT PRIMARY KEY CHECK (owner IN ('dad','mom')),
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
			failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
			blocked_until TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			password_changed_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS parent_login_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner TEXT CHECK (owner IN ('dad','mom') OR owner IS NULL),
			result TEXT NOT NULL CHECK (result IN ('success','failure','blocked')),
			reason TEXT NOT NULL,
			remote_address TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS parent_login_devices (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			owner TEXT NOT NULL REFERENCES parent_accounts(owner),
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS parent_login_devices_owner
		 ON parent_login_devices(owner,device_id)`,
		`CREATE INDEX IF NOT EXISTS parent_login_events_created
		 ON parent_login_events(created_at)`,
		`INSERT INTO schema_migrations(version) VALUES (16)
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
	var table string
	if err := d.db.QueryRowContext(ctx, `SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&table); err == nil {
		return fmt.Errorf("foreign key check failed: %s", table)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run foreign key check: %w", err)
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
