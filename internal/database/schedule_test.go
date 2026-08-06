package database

import (
	"context"
	"errors"
	"testing"
	"time"

	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

func TestScheduleRepositoryCreateListOverlapAndUpdate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.db.Exec(`
		INSERT INTO devices(id, name, device_type, local_only)
		VALUES
		 ('pc', 'Home PC', 'trusted_pc', 0),
		 ('tablet', 'Shared Tablet', 'shared_tablet', 1)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	created, err := db.CreateSchedule(ctx, scheduledomain.Item{
		ID: "school", Title: "학교", LocationName: "학교",
		Visibility: scheduledomain.VisibilityFamily,
		StartsAt:   now, EndsAt: now.Add(6 * time.Hour),
		Participants: []string{"daughter"},
	}, "pc", now)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(created.Participants) != 1 {
		t.Fatalf("unexpected created schedule: %#v", created)
	}

	candidate := scheduledomain.Item{
		ID: "hospital", Title: "병원",
		Visibility: scheduledomain.VisibilityParentsOnly,
		StartsAt:   now.Add(5 * time.Hour), EndsAt: now.Add(7 * time.Hour),
		Participants: []string{"daughter"},
	}
	overlaps, err := db.OverlappingSchedules(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 || overlaps[0].ID != "school" {
		t.Fatalf("unexpected overlaps: %#v", overlaps)
	}

	items, err := db.SchedulesBetween(ctx, now.Add(-time.Hour), now.Add(24*time.Hour), "daughter")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "school" {
		t.Fatalf("unexpected schedule list: %#v", items)
	}

	created.Title = "학교 수정"
	updated, err := db.UpdateSchedule(ctx, created, "tablet", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Title != "학교 수정" {
		t.Fatalf("unexpected updated schedule: %#v", updated)
	}
	var createdBy, updatedBy string
	if err := db.db.QueryRow(`
		SELECT created_by_device_id, updated_by_device_id
		FROM schedules WHERE id = 'school'`).Scan(&createdBy, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "pc" || updatedBy != "tablet" {
		t.Fatalf("unexpected audit devices: created=%q updated=%q", createdBy, updatedBy)
	}
	if _, err := db.UpdateSchedule(ctx, created, "pc", now.Add(2*time.Minute)); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("stale update: got %v, want %v", err, ErrScheduleConflict)
	}
}

func TestScheduleRepositoryRejectsUnknownParticipantAtomically(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`
		INSERT INTO devices(id, name, device_type, local_only)
		VALUES ('pc', 'Home PC', 'trusted_pc', 0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = db.CreateSchedule(ctx, scheduledomain.Item{
		ID: "invalid", Title: "잘못된 일정",
		Visibility: scheduledomain.VisibilityFamily,
		StartsAt:   now, EndsAt: now.Add(time.Hour),
		Participants: []string{"unknown"},
	}, "pc", now)
	if !errors.Is(err, ErrInvalidParticipant) {
		t.Fatalf("invalid participant: got %v, want %v", err, ErrInvalidParticipant)
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM schedules WHERE id = 'invalid'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed schedule creation left a partial schedule")
	}
}

func TestWeeklyScheduleCreationExpandsWithoutDuplicatingTemplate(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.db.Exec(`INSERT INTO devices(id, name, device_type, local_only) VALUES ('pc', 'Home PC', 'trusted_pc', 0)`); err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	startsOn := time.Date(2026, 8, 3, 0, 0, 0, 0, location)
	endsOn := time.Date(2026, 8, 10, 0, 0, 0, 0, location)
	item := scheduledomain.Item{
		ID: "weekly-school", Title: "등교", Visibility: scheduledomain.VisibilityFamily,
		StartsAt:     time.Date(2026, 8, 3, 9, 0, 0, 0, location).UTC(),
		EndsAt:       time.Date(2026, 8, 3, 10, 0, 0, 0, location).UTC(),
		Participants: []string{"daughter"},
	}
	created, err := db.CreateWeeklySchedule(ctx, scheduledomain.WeeklyRule{
		Item: item, StartsOn: startsOn, EndsOn: &endsOn,
		Weekdays: []time.Weekday{time.Monday, time.Wednesday}, StartMinute: 9 * 60, EndMinute: 10 * 60,
	}, "pc", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 {
		t.Fatalf("unexpected version: %d", created.Version)
	}
	items, err := db.SchedulesBetween(ctx, startsOn.UTC(), endsOn.AddDate(0, 0, 1).UTC(), "daughter")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d occurrences, want 3: %#v", len(items), items)
	}
	for _, occurrence := range items {
		if occurrence.ID != item.ID || occurrence.OccurrenceKey == "" {
			t.Fatalf("unexpected occurrence: %#v", occurrence)
		}
	}
	override := item
	override.Title = "특별 등교"
	override.StartsAt = time.Date(2026, 8, 5, 11, 0, 0, 0, location).UTC()
	override.EndsAt = time.Date(2026, 8, 5, 12, 0, 0, 0, location).UTC()
	version, err := db.SaveOccurrenceException(ctx, item.ID, scheduledomain.OccurrenceException{
		OccurrenceKey: "2026-08-05T09:00", Override: &override,
	}, 0, time.Now().UTC())
	if err != nil || version != 1 {
		t.Fatalf("save override: version=%d error=%v", version, err)
	}
	if _, err := db.SaveOccurrenceException(ctx, item.ID, scheduledomain.OccurrenceException{
		OccurrenceKey: "2026-08-05T09:00", Override: &override,
	}, 0, time.Now().UTC()); !errors.Is(err, ErrOccurrenceConflict) {
		t.Fatalf("stale occurrence update: got %v, want %v", err, ErrOccurrenceConflict)
	}
	if _, err := db.SaveOccurrenceException(ctx, item.ID, scheduledomain.OccurrenceException{
		OccurrenceKey: "2026-08-10T09:00", Cancelled: true,
	}, 0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	items, err = db.SchedulesBetween(ctx, startsOn.UTC(), endsOn.AddDate(0, 0, 1).UTC(), "daughter")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d final occurrences after exceptions, want 2: %#v", len(items), items)
	}
	if items[1].Title != "특별 등교" || items[1].OccurrenceKey != "2026-08-05T09:00" ||
		items[1].OccurrenceVersion != 1 || items[1].StartsAt.Hour() != 2 {
		t.Fatalf("override was not persisted with stable key: %#v", items[1])
	}
}

func TestAllDayScheduleAppearsOnEveryIncludedDate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO devices(id, name, device_type, local_only) VALUES ('pc', 'Home PC', 'trusted_pc', 0)`); err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	item := scheduledomain.Item{
		ID: "trip", Title: "가족여행", Visibility: scheduledomain.VisibilityFamily,
		TimeKind: scheduledomain.TimeKindAllDay, StartDate: "2026-08-10", EndDate: "2026-08-12",
		StartsAt:     time.Date(2026, 8, 10, 0, 0, 0, 0, location).UTC(),
		EndsAt:       time.Date(2026, 8, 13, 0, 0, 0, 0, location).UTC(),
		Participants: []string{"dad"},
	}
	if _, err := db.CreateAllDaySchedule(ctx, item, "pc", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for day := 10; day <= 12; day++ {
		from := time.Date(2026, 8, day, 0, 0, 0, 0, location)
		items, err := db.SchedulesBetween(ctx, from.UTC(), from.AddDate(0, 0, 1).UTC(), "dad")
		if err != nil || len(items) != 1 || items[0].TimeKind != scheduledomain.TimeKindAllDay {
			t.Fatalf("day %d: items=%#v error=%v", day, items, err)
		}
	}
}
