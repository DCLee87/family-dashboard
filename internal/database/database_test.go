package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
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
	if version != 16 {
		t.Fatalf("schema version: got %d, want 16", version)
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
		"push_subscriptions",
		"family_members",
		"schedules",
		"schedule_participants",
		"schedule_tags",
		"schedule_recurrence_rules",
		"schedule_recurrence_days",
		"schedule_occurrence_exceptions",
		"schedule_all_day_dates",
		"schedule_occurrence_all_day_overrides",
		"task_notification_settings",
		"task_notification_recipients",
		"task_notification_deliveries",
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
	if version != 16 {
		t.Fatalf("migrated schema version: got %d, want 16", version)
	}
}

func TestDatabaseRepairsLegacyOrphanRecordsBeforeC6Migration(t *testing.T) {
	dataDir := t.TempDir()
	database, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", filepath.Join(dataDir, "family-dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DELETE FROM schema_migrations WHERE version = 13`,
		`INSERT INTO schedule_notification_settings(schedule_id, enabled) VALUES ('missing-schedule', 1)`,
		`INSERT INTO task_assignees(task_id, family_member_id) VALUES ('missing-task', 'dad')`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			_ = legacy.Close()
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	repaired, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer repaired.Close()
	if err := repaired.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM schedule_notification_settings WHERE schedule_id = 'missing-schedule'`,
		`SELECT COUNT(*) FROM task_assignees WHERE task_id = 'missing-task'`,
	} {
		var count int
		if err := repaired.db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("orphan record was not removed for query %q", query)
		}
	}
}

func TestIntegrityCheckReportsForeignKeyViolations(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO task_assignees(task_id, family_member_id) VALUES ('missing-task', 'dad')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if err := database.IntegrityCheck(context.Background()); err == nil {
		t.Fatal("foreign key violation was not reported")
	}
}

func TestDatabaseCreatesDefaultFamilyMembers(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	rows, err := database.db.Query(
		`SELECT slug, display_name, role FROM family_members ORDER BY slug`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var slug, name, role string
		if err := rows.Scan(&slug, &name, &role); err != nil {
			t.Fatal(err)
		}
		got = append(got, slug+":"+name+":"+role)
	}
	want := []string{
		"dad:아빠:parent",
		"daughter:딸:child",
		"mom:엄마:parent",
	}
	if len(got) != len(want) {
		t.Fatalf("members: got %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("member %d: got %q, want %q", index, got[index], want[index])
		}
	}
}

func TestParentMobileEnrollmentAndIndependentRevocation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	setupCode := [32]byte{1}
	if _, err := database.EnsureInitialSetupCode(ctx, setupCode, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	pcAccess := [32]byte{2}
	if err := database.CompleteInitialSetup(ctx, InitialSetup{
		CodeHash: setupCode, PINHash: "test-pin-hash", DeviceID: "trusted-pc",
		DeviceName: "Home Mac", AccessID: "pc-access", AccessHash: pcAccess,
		AccessExpiresAt: now.Add(time.Hour), RefreshID: "pc-refresh",
		RefreshHash: [32]byte{3}, RefreshFamilyID: "pc-family",
		RefreshExpiresAt: now.Add(time.Hour), RecoveryID: "recovery",
		RecoveryHash: [32]byte{4}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	codeHash := [32]byte{5}
	if err := database.CreateEnrollment(
		ctx, "enrollment", codeHash, "parent_mobile", now.Add(10*time.Minute), now,
	); err != nil {
		t.Fatal(err)
	}
	claimHash := [32]byte{6}
	submitted, err := database.SubmitEnrollment(
		ctx, codeHash, claimHash, "Dad Phone", "dad", "", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != "submitted" || submitted.Owner.String != "dad" {
		t.Fatalf("unexpected submission: %#v", submitted)
	}
	if _, err := database.SubmitEnrollment(
		ctx, codeHash, [32]byte{7}, "Other Phone", "mom", "", now,
	); !errors.Is(err, ErrInvalidEnrollment) {
		t.Fatalf("enrollment code reuse: got %v, want %v", err, ErrInvalidEnrollment)
	}
	if err := database.ApproveEnrollment(ctx, "enrollment", "trusted-pc", now); err != nil {
		t.Fatal(err)
	}

	mobileAccess := [32]byte{8}
	mobileSetup := InitialSetup{
		DeviceID: "dad-phone", AccessID: "mobile-access", AccessHash: mobileAccess,
		AccessExpiresAt: now.Add(time.Hour), RefreshID: "mobile-refresh",
		RefreshHash: [32]byte{9}, RefreshFamilyID: "mobile-family",
		RefreshExpiresAt: now.Add(time.Hour), Now: now,
	}
	if err := database.CompleteEnrollment(
		ctx, claimHash, [32]byte{10}, mobileSetup, "dad",
	); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteEnrollment(
		ctx, claimHash, [32]byte{11}, mobileSetup, "dad",
	); !errors.Is(err, ErrInvalidEnrollment) {
		t.Fatalf("claim reuse: got %v, want %v", err, ErrInvalidEnrollment)
	}
	if _, err := database.DeviceByAccessToken(ctx, mobileAccess, now); err != nil {
		t.Fatalf("mobile credential was not active: %v", err)
	}
	rotatedAccess := [32]byte{19}
	if err := database.RotateRefreshCredential(
		ctx,
		[32]byte{9},
		CredentialRotation{
			AccessID:         "rotated-access",
			AccessHash:       rotatedAccess,
			AccessExpiresAt:  now.Add(2 * time.Hour),
			RefreshID:        "rotated-refresh",
			RefreshHash:      [32]byte{20},
			RefreshExpiresAt: now.Add(2 * time.Hour),
		},
		true,
		now.Add(time.Minute),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DeviceByAccessToken(
		ctx,
		mobileAccess,
		now.Add(time.Minute),
	); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old access credential survived rotation: got %v", err)
	}
	if _, err := database.DeviceByAccessToken(
		ctx,
		rotatedAccess,
		now.Add(time.Minute),
	); err != nil {
		t.Fatalf("rotated access credential was rejected: %v", err)
	}
	if err := database.RotateRefreshCredential(
		ctx,
		[32]byte{9},
		CredentialRotation{
			AccessID:         "reuse-access",
			AccessHash:       [32]byte{21},
			AccessExpiresAt:  now.Add(2 * time.Hour),
			RefreshID:        "reuse-refresh",
			RefreshHash:      [32]byte{22},
			RefreshExpiresAt: now.Add(2 * time.Hour),
		},
		true,
		now.Add(2*time.Minute),
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("refresh reuse: got %v, want %v", err, ErrRefreshReuse)
	}
	if _, err := database.DeviceByAccessToken(
		ctx,
		rotatedAccess,
		now.Add(2*time.Minute),
	); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("refresh reuse did not revoke token family: got %v", err)
	}
	if err := database.RevokeDevice(ctx, "dad-phone", "trusted-pc", now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DeviceByAccessToken(ctx, mobileAccess, now); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked mobile credential: got %v, want %v", err, ErrUnauthenticated)
	}
	if _, err := database.DeviceByAccessToken(ctx, pcAccess, now); err != nil {
		t.Fatalf("revoking mobile affected trusted PC: %v", err)
	}
	activeDevices, err := database.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeDevices) != 1 ||
		activeDevices[0].ID != "trusted-pc" ||
		activeDevices[0].Status != "active" {
		t.Fatalf("device list exposed revoked devices: %#v", activeDevices)
	}

	sessionHash := [32]byte{12}
	if err := database.CreateAdminSession(
		ctx,
		"trusted-pc",
		"admin-session",
		sessionHash,
		[32]byte{13},
		now.Add(10*time.Minute),
		now.Add(time.Hour),
		now,
	); err != nil {
		t.Fatal(err)
	}
	active, err := database.ValidateAdminSession(
		ctx,
		"trusted-pc",
		sessionHash,
		nil,
		now.Add(9*time.Minute),
		false,
	)
	if err != nil || !active {
		t.Fatalf("admin session expired early: active=%v err=%v", active, err)
	}
	active, err = database.ValidateAdminSession(
		ctx,
		"trusted-pc",
		sessionHash,
		nil,
		now.Add(11*time.Minute),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("read-only polling kept an idle admin session active")
	}

	rejectedCode := [32]byte{14}
	if err := database.CreateEnrollment(
		ctx,
		"rejected-enrollment",
		rejectedCode,
		"parent_mobile",
		now.Add(10*time.Minute),
		now,
	); err != nil {
		t.Fatal(err)
	}
	rejectedClaim := [32]byte{15}
	if _, err := database.SubmitEnrollment(
		ctx,
		rejectedCode,
		rejectedClaim,
		"Rejected Phone",
		"mom",
		"",
		now,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.RejectEnrollment(
		ctx,
		"rejected-enrollment",
		"trusted-pc",
		now,
	); err != nil {
		t.Fatal(err)
	}
	rejected, err := database.EnrollmentByClaim(ctx, rejectedClaim, now)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("rejected enrollment status: got %q", rejected.Status)
	}
	if err := database.CompleteEnrollment(
		ctx,
		rejectedClaim,
		[32]byte{16},
		mobileSetup,
		"mom",
	); !errors.Is(err, ErrInvalidEnrollment) {
		t.Fatalf("rejected enrollment issued credentials: got %v", err)
	}

	expiredCode := [32]byte{17}
	if err := database.CreateEnrollment(
		ctx,
		"expired-enrollment",
		expiredCode,
		"parent_mobile",
		now.Add(-time.Second),
		now.Add(-time.Minute),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SubmitEnrollment(
		ctx,
		expiredCode,
		[32]byte{18},
		"Expired Phone",
		"dad",
		"",
		now,
	); !errors.Is(err, ErrInvalidEnrollment) {
		t.Fatalf("expired enrollment was submitted: got %v", err)
	}
}

func TestSharedTabletEnrollmentAndExternalRefreshRejection(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	setupCode := [32]byte{30}
	if _, err := database.EnsureInitialSetupCode(ctx, setupCode, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteInitialSetup(ctx, InitialSetup{
		CodeHash: setupCode, PINHash: "test-pin-hash", DeviceID: "trusted-pc",
		DeviceName: "Home Mac", AccessID: "pc-access", AccessHash: [32]byte{31},
		AccessExpiresAt: now.Add(time.Hour), RefreshID: "pc-refresh",
		RefreshHash: [32]byte{32}, RefreshFamilyID: "pc-family",
		RefreshExpiresAt: now.Add(time.Hour), RecoveryID: "recovery",
		RecoveryHash: [32]byte{33}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	codeHash := [32]byte{34}
	claimHash := [32]byte{35}
	if err := database.CreateEnrollment(
		ctx, "tablet-enrollment", codeHash, "shared_tablet", now.Add(10*time.Minute), now,
	); err != nil {
		t.Fatal(err)
	}
	submitted, err := database.SubmitEnrollment(
		ctx, codeHash, claimHash, "Living Room Tablet", "", "", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.RequestedType != "shared_tablet" || submitted.Owner.Valid {
		t.Fatalf("unexpected tablet submission: %#v", submitted)
	}
	if err := database.ApproveEnrollment(
		ctx, "tablet-enrollment", "trusted-pc", now,
	); err != nil {
		t.Fatal(err)
	}

	tabletAccess := [32]byte{36}
	tabletRefresh := [32]byte{37}
	if err := database.CompleteEnrollment(
		ctx,
		claimHash,
		[32]byte{38},
		InitialSetup{
			DeviceID: "shared-tablet", AccessID: "tablet-access",
			AccessHash: tabletAccess, AccessExpiresAt: now.Add(time.Hour),
			RefreshID: "tablet-refresh", RefreshHash: tabletRefresh,
			RefreshFamilyID: "tablet-family", RefreshExpiresAt: now.Add(time.Hour),
			Now: now,
		},
		"",
	); err != nil {
		t.Fatal(err)
	}
	device, err := database.DeviceByAccessToken(ctx, tabletAccess, now)
	if err != nil {
		t.Fatal(err)
	}
	if device.Type != "shared_tablet" || !device.LocalOnly {
		t.Fatalf("unexpected tablet device: %#v", device)
	}

	err = database.RotateRefreshCredential(
		ctx,
		tabletRefresh,
		CredentialRotation{
			AccessID: "external-access", AccessHash: [32]byte{39},
			AccessExpiresAt: now.Add(2 * time.Hour), RefreshID: "external-refresh",
			RefreshHash: [32]byte{40}, RefreshExpiresAt: now.Add(2 * time.Hour),
		},
		false,
		now.Add(time.Minute),
	)
	if !errors.Is(err, ErrLocalNetworkRequired) {
		t.Fatalf("external tablet refresh: got %v, want %v", err, ErrLocalNetworkRequired)
	}

	if err := database.RotateRefreshCredential(
		ctx,
		tabletRefresh,
		CredentialRotation{
			AccessID: "local-access", AccessHash: [32]byte{41},
			AccessExpiresAt: now.Add(2 * time.Hour), RefreshID: "local-refresh",
			RefreshHash: [32]byte{42}, RefreshExpiresAt: now.Add(2 * time.Hour),
		},
		true,
		now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("local tablet refresh failed: %v", err)
	}
}
