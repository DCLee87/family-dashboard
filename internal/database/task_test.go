package database

import (
	"context"
	"testing"
	"time"

	taskdomain "github.com/DCLee87/family-dashboard/internal/task"
)

func TestTaskRepositoryCreateAndLoadRelationsWithSingleConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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

	now := time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)
	created, err := db.CreateTask(ctx, taskdomain.Item{
		ID: "weekly-task", Title: "주간 할 일", Priority: taskdomain.PriorityImportant,
		DueKind: taskdomain.DueDateTime, DueDate: "2026-08-07", DueMinute: 23*60 + 50,
		Assignees: []string{"dad", "mom"}, RepeatKind: taskdomain.RepeatWeekly,
		StartsOn: "2026-08-07", EndsOn: "2026-08-28", Weekdays: []int{5},
	}, "pc", now)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(created.Assignees) != 2 || len(created.Weekdays) != 1 {
		t.Fatalf("unexpected created task: %#v", created)
	}
	setting, err := db.TaskNotificationSetting(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Enabled || setting.TimedLeadMinutes != 60 || setting.DateHour != 9 || len(setting.Recipients) != 2 {
		t.Fatalf("unexpected default notification: %#v", setting)
	}

	items, err := db.ActiveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "weekly-task" || len(items[0].Assignees) != 2 {
		t.Fatalf("unexpected active tasks: %#v", items)
	}
}

func TestTaskNotificationRecipientsAndOccurrenceStates(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO devices(id, name, device_type, local_only)
		VALUES ('pc', 'Home PC', 'trusted_pc', 0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)
	created, err := db.CreateTask(ctx, taskdomain.Item{
		ID: "dad-task", Title: "아빠 할 일", Priority: taskdomain.PriorityNormal,
		DueKind: taskdomain.DueDate, DueDate: "2026-08-07", Assignees: []string{"dad"},
		RepeatKind: taskdomain.RepeatDaily, StartsOn: "2026-08-07",
	}, "pc", now)
	if err != nil {
		t.Fatal(err)
	}
	setting, err := db.TaskNotificationSetting(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(setting.Recipients) != 1 || setting.Recipients[0] != "dad" {
		t.Fatalf("dad-only default recipients: %#v", setting.Recipients)
	}
	setting.Recipients = []string{"mom"}
	setting.DateHour = 8
	if err := db.SaveTaskNotificationSetting(ctx, created.ID, setting, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated, err := db.TaskNotificationSetting(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DateHour != 8 || len(updated.Recipients) != 1 || updated.Recipients[0] != "mom" {
		t.Fatalf("updated notification: %#v", updated)
	}
	claimed, err := db.ClaimTaskNotification(ctx, "delivery-1", created.ID, "2026-08-07", "pc", now, now)
	if err != nil || !claimed {
		t.Fatalf("first notification claim: claimed=%v err=%v", claimed, err)
	}
	claimed, err = db.ClaimTaskNotification(ctx, "delivery-2", created.ID, "2026-08-07", "pc", now, now)
	if err != nil || claimed {
		t.Fatalf("duplicate notification claim: claimed=%v err=%v", claimed, err)
	}

	if err := db.SetTaskOccurrenceSkipped(ctx, created.ID, "2026-08-07", "pc", true, now); err != nil {
		t.Fatal(err)
	}
	status, err := db.TaskOccurrenceStatus(ctx, created.ID, "2026-08-07")
	if err != nil || status != "skipped" {
		t.Fatalf("skipped occurrence: status=%q err=%v", status, err)
	}
	skipped, err := db.SkippedTaskOccurrences(ctx)
	if err != nil || len(skipped) != 1 || skipped[0].OccurrenceKey != "2026-08-07" {
		t.Fatalf("skipped occurrence list: %#v err=%v", skipped, err)
	}
	if err := db.SetTaskOccurrenceSkipped(ctx, created.ID, "2026-08-07", "pc", false, now); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskOccurrenceCompleted(ctx, created.ID, "2026-08-07", "pc", true, now); err != nil {
		t.Fatal(err)
	}
	history, err := db.CompletedTaskOccurrences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].OccurrenceKey != "2026-08-07" || history[0].Item.ID != created.ID {
		t.Fatalf("completed history: %#v", history)
	}
}
