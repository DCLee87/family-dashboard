package schedule

import (
	"testing"
	"time"
)

func TestExpandWeeklyIncludesEndDateAndSelectedWeekdays(t *testing.T) {
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	rule := WeeklyRule{
		Item:     Item{ID: "school", Title: "학원", Visibility: VisibilityFamily, Participants: []string{"daughter"}},
		StartsOn: time.Date(2026, 8, 4, 0, 0, 0, 0, location),
		EndsOn:   timePointer(time.Date(2026, 8, 6, 0, 0, 0, 0, location)),
		Weekdays: []time.Weekday{time.Tuesday, time.Thursday}, StartMinute: 17 * 60, EndMinute: 18 * 60,
	}
	items, err := ExpandWeekly(rule,
		time.Date(2026, 8, 3, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC), location, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].OccurrenceKey != "2026-08-04T17:00" || items[1].OccurrenceKey != "2026-08-06T17:00" {
		t.Fatalf("unexpected occurrences: %#v", items)
	}
}

func TestExpandWeeklyAppliesCancellationAndStableOverrideKey(t *testing.T) {
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	rule := WeeklyRule{
		Item:     Item{ID: "lesson", Title: "수업", Visibility: VisibilityFamily, Participants: []string{"dad"}},
		StartsOn: time.Date(2026, 8, 3, 0, 0, 0, 0, location),
		Weekdays: []time.Weekday{time.Tuesday, time.Thursday}, StartMinute: 10 * 60, EndMinute: 11 * 60,
	}
	override := rule.Item
	override.Title = "이동 수업"
	override.StartsAt = time.Date(2026, 8, 5, 2, 0, 0, 0, time.UTC)
	override.EndsAt = time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC)
	items, err := ExpandWeekly(rule,
		time.Date(2026, 8, 3, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC), location, []OccurrenceException{
			{OccurrenceKey: "2026-08-04T10:00", Override: &override},
			{OccurrenceKey: "2026-08-06T10:00", Cancelled: true},
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OccurrenceKey != "2026-08-04T10:00" || items[0].Title != "이동 수업" {
		t.Fatalf("unexpected final occurrences: %#v", items)
	}
}

func TestExpandWeeklyIncludesPreviousDayCrossMidnightOccurrence(t *testing.T) {
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	rule := WeeklyRule{
		Item:     Item{ID: "trip", Title: "여행", Visibility: VisibilityFamily, Participants: []string{"dad"}},
		StartsOn: time.Date(2026, 8, 3, 0, 0, 0, 0, location),
		Weekdays: []time.Weekday{time.Monday}, StartMinute: 23 * 60, EndMinute: 60,
	}
	items, err := ExpandWeekly(rule,
		time.Date(2026, 8, 3, 15, 30, 0, 0, time.UTC),
		time.Date(2026, 8, 3, 16, 30, 0, 0, time.UTC), location, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].EndsAt.After(time.Date(2026, 8, 3, 15, 30, 0, 0, time.UTC)) {
		t.Fatalf("cross-midnight occurrence missing: %#v", items)
	}
}
