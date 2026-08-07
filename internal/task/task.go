package task

import (
	"errors"
	"strings"
	"time"
)

const (
	PriorityNormal    = "normal"
	PriorityImportant = "important"
	DueNone           = "none"
	DueDate           = "date"
	DueDateTime       = "datetime"
	RepeatNone        = "none"
	RepeatDaily       = "daily"
	RepeatWeekly      = "weekly"
)

var ErrInvalidTask = errors.New("invalid task")

type Item struct {
	ID          string
	Title       string
	Notes       string
	Priority    string
	DueKind     string
	DueDate     string
	DueMinute   int
	Assignees   []string
	RepeatKind  string
	StartsOn    string
	EndsOn      string
	Weekdays    []int
	Version     int64
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Occurrence struct {
	Item
	OccurrenceKey string     `json:"occurrenceKey"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	Overdue       bool       `json:"overdue"`
}

func Validate(item Item) error {
	if strings.TrimSpace(item.Title) == "" || len([]rune(strings.TrimSpace(item.Title))) > 200 || len(item.Assignees) == 0 {
		return ErrInvalidTask
	}
	if item.Priority != PriorityNormal && item.Priority != PriorityImportant {
		return ErrInvalidTask
	}
	if item.DueKind != DueNone && item.DueKind != DueDate && item.DueKind != DueDateTime {
		return ErrInvalidTask
	}
	if item.DueKind != DueNone {
		if _, err := time.Parse("2006-01-02", item.DueDate); err != nil {
			return ErrInvalidTask
		}
	}
	if item.DueKind == DueDateTime && (item.DueMinute < 0 || item.DueMinute > 1439) {
		return ErrInvalidTask
	}
	if item.RepeatKind != RepeatNone && item.RepeatKind != RepeatDaily && item.RepeatKind != RepeatWeekly {
		return ErrInvalidTask
	}
	if item.RepeatKind != RepeatNone {
		start, err := time.Parse("2006-01-02", item.StartsOn)
		if err != nil {
			return ErrInvalidTask
		}
		if item.EndsOn != "" {
			end, err := time.Parse("2006-01-02", item.EndsOn)
			if err != nil || end.Before(start) {
				return ErrInvalidTask
			}
		}
	}
	if item.RepeatKind == RepeatWeekly && len(item.Weekdays) == 0 {
		return ErrInvalidTask
	}
	return nil
}

func AppliesOn(item Item, date time.Time) bool {
	day := date.Format("2006-01-02")
	switch item.RepeatKind {
	case RepeatNone:
		return true
	case RepeatDaily:
		return day >= item.StartsOn && (item.EndsOn == "" || day <= item.EndsOn)
	case RepeatWeekly:
		if day < item.StartsOn || (item.EndsOn != "" && day > item.EndsOn) {
			return false
		}
		for _, weekday := range item.Weekdays {
			if weekday == int(date.Weekday()) {
				return true
			}
		}
	}
	return false
}
