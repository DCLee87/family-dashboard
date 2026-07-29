package schedule

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	VisibilityFamily      = "family"
	VisibilityTVSummary   = "tv_summary"
	VisibilityParentsOnly = "parents_only"
)

var (
	ErrInvalidTitle         = errors.New("schedule title is required")
	ErrInvalidTimeRange     = errors.New("schedule end must be after start")
	ErrParticipantsRequired = errors.New("at least one participant is required")
	ErrInvalidVisibility    = errors.New("invalid schedule visibility")
)

type Item struct {
	ID           string
	Title        string
	LocationName string
	Notes        string
	Visibility   string
	StartsAt     time.Time
	EndsAt       time.Time
	Participants []string
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type View struct {
	ID           string     `json:"id,omitempty"`
	Title        string     `json:"title"`
	LocationName string     `json:"locationName,omitempty"`
	Notes        string     `json:"notes,omitempty"`
	Visibility   string     `json:"visibility,omitempty"`
	StartsAt     *time.Time `json:"startsAt,omitempty"`
	EndsAt       *time.Time `json:"endsAt,omitempty"`
	Participants []string   `json:"participants,omitempty"`
	Version      int64      `json:"version,omitempty"`
	Summary      bool       `json:"summary,omitempty"`
	Overlap      bool       `json:"overlap,omitempty"`
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
	if !item.EndsAt.After(item.StartsAt) {
		return ErrInvalidTimeRange
	}
	if len(uniqueStrings(item.Participants)) == 0 {
		return ErrParticipantsRequired
	}
	switch item.Visibility {
	case VisibilityFamily, VisibilityTVSummary, VisibilityParentsOnly:
		return nil
	default:
		return ErrInvalidVisibility
	}
}

func Overlap(left, right Item) bool {
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
		return View{
			Title: "개인 일정", StartsAt: timePointer(item.StartsAt), EndsAt: timePointer(item.EndsAt),
			Summary: true,
		}, true
	}
	return View{
		ID: item.ID, Title: item.Title, LocationName: item.LocationName,
		Notes: item.Notes, Visibility: item.Visibility, StartsAt: timePointer(item.StartsAt),
		EndsAt: timePointer(item.EndsAt), Participants: item.Participants,
		Version: item.Version,
	}, true
}

func ActiveStatus(items []Item, memberID string, at time.Time, audience Audience) (View, bool) {
	var candidates []Item
	for _, item := range items {
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
