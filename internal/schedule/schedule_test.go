package schedule

import (
	"errors"
	"testing"
	"time"
)

func TestOverlapUsesHalfOpenIntervalsAndSharedParticipants(t *testing.T) {
	base := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	left := Item{StartsAt: base, EndsAt: base.Add(time.Hour), Participants: []string{"dad"}}
	touching := Item{
		StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour),
		Participants: []string{"dad"},
	}
	if Overlap(left, touching) {
		t.Fatal("touching half-open intervals overlapped")
	}
	crossing := touching
	crossing.StartsAt = base.Add(30 * time.Minute)
	if !Overlap(left, crossing) {
		t.Fatal("crossing intervals for the same participant did not overlap")
	}
	crossing.Participants = []string{"mom"}
	if Overlap(left, crossing) {
		t.Fatal("different participants overlapped")
	}
}

func TestValidateRequiresCoreFields(t *testing.T) {
	valid := Item{
		Title: "학교", Visibility: VisibilityFamily,
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
		Participants: []string{"daughter"},
	}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	valid.Tag = "unknown"
	if !errors.Is(Validate(valid), ErrInvalidTag) {
		t.Fatalf("invalid tag: got %v", Validate(valid))
	}
	valid.Tag = TagAcademy
	valid.Participants = nil
	if !errors.Is(Validate(valid), ErrParticipantsRequired) {
		t.Fatalf("missing participants: got %v", Validate(valid))
	}
}

func TestAllDayScheduleUsesInclusiveDatesWithoutActiveStatus(t *testing.T) {
	item := Item{
		ID: "trip", Title: "가족여행", LocationName: "제주",
		Visibility: VisibilityFamily, TimeKind: TimeKindAllDay,
		StartDate: "2026-08-10", EndDate: "2026-08-12", Participants: []string{"dad"},
	}
	if err := Validate(item); err != nil {
		t.Fatal(err)
	}
	view, ok := Project(item, AudienceParent)
	if !ok || view.TimeKind != TimeKindAllDay || view.StartDate != "2026-08-10" ||
		view.EndDate != "2026-08-12" || view.StartsAt != nil || view.EndsAt != nil {
		t.Fatalf("unexpected all-day projection: %#v", view)
	}
	if _, active := ActiveStatus([]Item{item}, "dad", time.Now(), AudienceParent); active {
		t.Fatal("all-day schedule affected family status without an explicit status interval")
	}
	item.EndDate = "2026-08-09"
	if !errors.Is(Validate(item), ErrInvalidTimeRange) {
		t.Fatalf("invalid all-day range was accepted: %v", Validate(item))
	}
}

func TestProjectionDoesNotLeakPrivateDetails(t *testing.T) {
	item := Item{
		ID: "secret", Title: "병원", LocationName: "비밀 장소", Notes: "비밀 메모",
		Visibility: VisibilityParentsOnly, StartsAt: time.Now(),
		EndsAt: time.Now().Add(time.Hour), Participants: []string{"dad"},
	}
	if view, ok := Project(item, AudienceShared); ok || view.Title != "" {
		t.Fatalf("private schedule was projected: %#v", view)
	}
	item.Visibility = VisibilityTVSummary
	view, ok := Project(item, AudienceTV)
	if !ok || view.Title != "개인 일정" || view.ID != "" ||
		view.LocationName != "" || view.Notes != "" {
		t.Fatalf("unsafe TV summary: %#v", view)
	}
}

func TestActiveStatusUsesLatestStartThenNewestCreation(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	items := []Item{
		{
			ID: "older", Title: "학교", LocationName: "학교",
			Visibility: VisibilityFamily, StartsAt: now.Add(-time.Hour),
			EndsAt: now.Add(time.Hour), Participants: []string{"daughter"},
			CreatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID: "newer", Title: "병원", LocationName: "병원",
			Visibility: VisibilityFamily, StartsAt: now.Add(-time.Hour),
			EndsAt: now.Add(time.Hour), Participants: []string{"daughter"},
			CreatedAt: now.Add(-time.Hour),
		},
	}
	view, ok := ActiveStatus(items, "daughter", now, AudienceShared)
	if !ok || view.Title != "병원" || !view.Overlap {
		t.Fatalf("unexpected status: %#v, %v", view, ok)
	}
	if _, ok := ActiveStatus(items, "daughter", now.Add(time.Hour), AudienceShared); ok {
		t.Fatal("end boundary remained active")
	}
}

func TestPrivateActiveStatusContainsOnlyLabelAndEndTime(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	view, ok := ActiveStatus([]Item{{
		ID: "private", Title: "비밀", LocationName: "비밀 장소", Notes: "비밀 메모",
		Visibility: VisibilityParentsOnly, StartsAt: now.Add(-time.Hour),
		EndsAt: now.Add(time.Hour), Participants: []string{"dad"},
	}}, "dad", now, AudienceShared)
	if !ok {
		t.Fatal("private active status was omitted")
	}
	if view.Title != "개인 일정" || view.ID != "" || view.StartsAt != nil ||
		view.EndsAt == nil || view.LocationName != "" || view.Notes != "" ||
		len(view.Participants) != 0 {
		t.Fatalf("private status leaked details: %#v", view)
	}
}
