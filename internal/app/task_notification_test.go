package app

import (
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	taskdomain "github.com/DCLee87/family-dashboard/internal/task"
)

func TestTaskNotificationCandidateDefaults(t *testing.T) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	setting := database.TaskNotificationSetting{Enabled: true, TimedLeadMinutes: 60, DateHour: 9}
	timed := taskdomain.Item{
		DueKind: taskdomain.DueDateTime, DueDate: "2026-08-08", DueMinute: 10 * 60,
		RepeatKind: taskdomain.RepeatNone,
	}
	candidates := taskNotificationCandidates(timed, setting, time.Date(2026, 8, 7, 9, 0, 0, 0, location), location)
	want := time.Date(2026, 8, 8, 9, 0, 0, 0, location).UTC()
	if len(candidates) != 1 || candidates[0].key != "single" || !candidates[0].notifyAt.Equal(want) {
		t.Fatalf("timed candidates: %#v, want %s", candidates, want)
	}

	dateOnly := timed
	dateOnly.DueKind = taskdomain.DueDate
	candidates = taskNotificationCandidates(dateOnly, setting, time.Date(2026, 8, 7, 9, 0, 0, 0, location), location)
	if len(candidates) != 1 || !candidates[0].notifyAt.Equal(want) {
		t.Fatalf("date-only candidates: %#v, want %s", candidates, want)
	}
}

func TestRecurringTaskNotificationsUseOccurrenceDate(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Seoul")
	item := taskdomain.Item{
		DueKind: taskdomain.DueDateTime, DueMinute: 18 * 60,
		RepeatKind: taskdomain.RepeatWeekly, StartsOn: "2026-08-07", EndsOn: "2026-08-28",
		Weekdays: []int{5},
	}
	setting := database.TaskNotificationSetting{Enabled: true, TimedLeadMinutes: 30, DateHour: 9}
	candidates := taskNotificationCandidates(item, setting, time.Date(2026, 8, 7, 12, 0, 0, 0, location), location)
	if len(candidates) != 2 || candidates[0].key != "2026-08-07" || candidates[1].key != "2026-08-14" {
		t.Fatalf("weekly candidates: %#v", candidates)
	}
	want := time.Date(2026, 8, 7, 17, 30, 0, 0, location).UTC()
	if !candidates[0].notifyAt.Equal(want) {
		t.Fatalf("first weekly notification: got %s want %s", candidates[0].notifyAt, want)
	}
}

func TestImportantOverdueTaskGetsDailyReminder(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, location)
	item := taskdomain.Item{Priority: taskdomain.PriorityImportant, DueKind: taskdomain.DueDate, DueDate: "2026-08-07", RepeatKind: taskdomain.RepeatNone}
	candidates := importantTaskReminderCandidates(item, now, location)
	want := time.Date(2026, 8, 8, 9, 0, 0, 0, location).UTC()
	if len(candidates) != 1 || candidates[0].key != "single" || !candidates[0].notifyAt.Equal(want) {
		t.Fatalf("overdue candidates=%#v want=%s", candidates, want)
	}
}

func TestTaskNotificationBackfillsWorkerWindow(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Seoul")
	item := taskdomain.Item{DueKind: taskdomain.DueDateTime, DueMinute: 10 * 60, RepeatKind: taskdomain.RepeatDaily, StartsOn: "2026-08-01"}
	setting := database.TaskNotificationSetting{Enabled: true, TimedLeadMinutes: 60}
	from := time.Date(2026, 8, 3, 0, 0, 0, 0, location)
	to := time.Date(2026, 8, 8, 0, 0, 0, 0, location)
	candidates := taskNotificationCandidatesBetween(item, setting, from, to, location)
	if len(candidates) != 6 || candidates[0].key != "2026-08-03" || candidates[5].key != "2026-08-08" {
		t.Fatalf("backfill candidates=%#v", candidates)
	}
}
