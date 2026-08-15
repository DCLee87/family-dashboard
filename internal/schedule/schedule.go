package schedule

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	TimeKindTimed         = "timed"
	TimeKindAllDay        = "all_day"
	VisibilityFamily      = "family"
	VisibilityTVSummary   = "tv_summary"
	VisibilityParentsOnly = "parents_only"
	TagGeneral            = "general"
	TagAcademy            = "academy"
	TagAfterSchool        = "after_school"
)

var (
	ErrInvalidTitle         = errors.New("schedule title is required")
	ErrInvalidTimeRange     = errors.New("schedule end must be after start")
	ErrParticipantsRequired = errors.New("at least one participant is required")
	ErrInvalidVisibility    = errors.New("invalid schedule visibility")
	ErrInvalidTag           = errors.New("invalid schedule tag")
)

type Item struct {
	ID                string
	Title             string
	LocationName      string
	PlaceID           string
	Notes             string
	Visibility        string
	Tag               string
	StartsAt          time.Time
	EndsAt            time.Time
	Participants      []string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	OccurrenceKey     string
	OccurrenceVersion int64
	TimeKind          string
	StartDate         string
	EndDate           string
}

type View struct {
	ID                string     `json:"id,omitempty"`
	Title             string     `json:"title"`
	LocationName      string     `json:"locationName,omitempty"`
	PlaceID           string     `json:"placeId,omitempty"`
	Notes             string     `json:"notes,omitempty"`
	Visibility        string     `json:"visibility,omitempty"`
	Tag               string     `json:"tag,omitempty"`
	StartsAt          *time.Time `json:"startsAt,omitempty"`
	EndsAt            *time.Time `json:"endsAt,omitempty"`
	Participants      []string   `json:"participants,omitempty"`
	Version           int64      `json:"version,omitempty"`
	Summary           bool       `json:"summary,omitempty"`
	Overlap           bool       `json:"overlap,omitempty"`
	OccurrenceKey     string     `json:"occurrenceKey,omitempty"`
	Recurring         bool       `json:"recurring,omitempty"`
	OccurrenceVersion int64      `json:"occurrenceVersion,omitempty"`
	TimeKind          string     `json:"timeKind,omitempty"`
	StartDate         string     `json:"startDate,omitempty"`
	EndDate           string     `json:"endDate,omitempty"`
}

type Audience string

const (
	AudienceParent Audience = "parent"
	AudienceShared Audience = "shared"
	AudienceTV     Audience = "tv"
)

func Validate(item Item) error {
	if strings.TrimSpace(item.Title) == "" {
		return ErrInvalidTitle
	}
	if item.TimeKind == TimeKindAllDay {
		start, startErr := time.Parse("2006-01-02", item.StartDate)
		end, endErr := time.Parse("2006-01-02", item.EndDate)
		if startErr != nil || endErr != nil || end.Before(start) {
			return ErrInvalidTimeRange
		}
	} else if !item.EndsAt.After(item.StartsAt) {
		return ErrInvalidTimeRange
	}
	if len(uniqueStrings(item.Participants)) == 0 {
		return ErrParticipantsRequired
	}
	switch item.Tag {
	case "", TagGeneral, TagAcademy, TagAfterSchool:
	default:
		return ErrInvalidTag
	}
	switch item.Visibility {
	case VisibilityFamily, VisibilityTVSummary, VisibilityParentsOnly:
		return nil
	default:
		return ErrInvalidVisibility
	}
}

func Overlap(left, right Item) bool {
	if left.TimeKind == TimeKindAllDay || right.TimeKind == TimeKindAllDay {
		return false
	}
	if !left.StartsAt.Before(right.EndsAt) || !right.StartsAt.Before(left.EndsAt) {
		return false
	}
	members := make(map[string]struct{}, len(left.Participants))
	for _, member := range left.Participants {
		members[member] = struct{}{}
	}
	for _, member := range right.Participants {
		if _, ok := members[member]; ok {
			return true
		}
	}
	return false
}

func Project(item Item, audience Audience) (View, bool) {
	if item.Visibility == VisibilityParentsOnly && audience != AudienceParent {
		return View{}, false
	}
	if item.Visibility == VisibilityTVSummary &&
		(audience == AudienceShared || audience == AudienceTV) {
		view := View{Title: "개인 일정", Summary: true, TimeKind: normalizedTimeKind(item.TimeKind)}
		setViewTime(&view, item)
		return view, true
	}
	view := View{
		ID: item.ID, Title: item.Title, LocationName: item.LocationName, PlaceID: item.PlaceID,
		Notes: item.Notes, Visibility: item.Visibility, Tag: normalizedTag(item.Tag), Participants: item.Participants,
		Version: item.Version, OccurrenceKey: item.OccurrenceKey,
		Recurring: item.OccurrenceKey != "", OccurrenceVersion: item.OccurrenceVersion,
		TimeKind: normalizedTimeKind(item.TimeKind),
	}
	setViewTime(&view, item)
	return view, true
}

func normalizedTag(value string) string {
	if value == TagAcademy || value == TagAfterSchool {
		return value
	}
	return TagGeneral
}

func ActiveStatus(items []Item, memberID string, at time.Time, audience Audience) (View, bool) {
	var candidates []Item
	for _, item := range items {
		if item.TimeKind == TimeKindAllDay {
			continue
		}
		if item.LocationName == "" || at.Before(item.StartsAt) || !at.Before(item.EndsAt) {
			continue
		}
		for _, participant := range item.Participants {
			if participant == memberID {
				candidates = append(candidates, item)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return View{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].StartsAt.Equal(candidates[j].StartsAt) {
			if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return candidates[i].StartsAt.After(candidates[j].StartsAt)
	})
	selected := candidates[0]
	if selected.Visibility == VisibilityParentsOnly && audience != AudienceParent {
		return View{
			Title: "개인 일정", EndsAt: timePointer(selected.EndsAt), Summary: true,
			Overlap: len(candidates) > 1,
		}, true
	}
	view, ok := Project(selected, audience)
	if !ok {
		return View{}, false
	}
	view.Overlap = len(candidates) > 1
	return view, true
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func normalizedTimeKind(value string) string {
	if value == TimeKindAllDay {
		return value
	}
	return TimeKindTimed
}

func setViewTime(view *View, item Item) {
	if item.TimeKind == TimeKindAllDay {
		view.StartDate, view.EndDate = item.StartDate, item.EndDate
		return
	}
	view.StartsAt, view.EndsAt = timePointer(item.StartsAt), timePointer(item.EndsAt)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
