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
	defer db.Close()
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
