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

	items, err := db.ActiveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "weekly-task" || len(items[0].Assignees) != 2 {
		t.Fatalf("unexpected active tasks: %#v", items)
	}
}
