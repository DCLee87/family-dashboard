package schedule

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidWeeklyRule = errors.New("invalid weekly recurrence rule")
	ErrInvalidOccurrence = errors.New("invalid recurrence occurrence")
)

// WeeklyRule stores the weekly pattern in the family system's local time.
// StartMinute and EndMinute are minutes since midnight; an end value not
// greater than the start value represents an end time on the following day.
type WeeklyRule struct {
	Item        Item
	StartsOn    time.Time
	EndsOn      *time.Time
	Weekdays    []time.Weekday
	StartMinute int
	EndMinute   int
}

type OccurrenceException struct {
	OccurrenceKey string
	Cancelled     bool
	Override      *Item
	Version       int64
}

type Occurrence struct {
	Item
	OccurrenceKey string
	OriginalStart time.Time
}

func ValidateWeeklyRule(rule WeeklyRule, location *time.Location) error {
	if location == nil || rule.StartMinute < 0 || rule.StartMinute >= 24*60 ||
		rule.EndMinute < 0 || rule.EndMinute >= 24*60 || len(uniqueWeekdays(rule.Weekdays)) == 0 {
		return ErrInvalidWeeklyRule
	}
	if rule.EndsOn != nil && localDate(*rule.EndsOn, location).Before(localDate(rule.StartsOn, location)) {
		return ErrInvalidWeeklyRule
	}
	return nil
}

// ExpandWeekly returns final occurrences that intersect [from, to). The
// occurrence key always identifies the original local start, even after an
// override moves that occurrence.
func ExpandWeekly(
	rule WeeklyRule,
	from, to time.Time,
	location *time.Location,
	exceptions []OccurrenceException,
) ([]Occurrence, error) {
	if !to.After(from) || ValidateWeeklyRule(rule, location) != nil {
		return nil, ErrInvalidWeeklyRule
	}
	exceptionByKey := make(map[string]OccurrenceException, len(exceptions))
	for _, exception := range exceptions {
		if exception.OccurrenceKey == "" || (exception.Cancelled && exception.Override != nil) {
			return nil, ErrInvalidOccurrence
		}
		if _, exists := exceptionByKey[exception.OccurrenceKey]; exists {
			return nil, ErrInvalidOccurrence
		}
		exceptionByKey[exception.OccurrenceKey] = exception
	}

	allowedDays := make(map[time.Weekday]struct{}, len(rule.Weekdays))
	for _, day := range uniqueWeekdays(rule.Weekdays) {
		allowedDays[day] = struct{}{}
	}
	first := localDate(from.In(location), location).AddDate(0, 0, -1)
	last := localDate(to.Add(-time.Nanosecond).In(location), location)
	startsOn := localDate(rule.StartsOn.In(location), location)
	endsOn := last
	if rule.EndsOn != nil {
		endsOn = localDate(rule.EndsOn.In(location), location)
	}
	if first.Before(startsOn) {
		first = startsOn
	}
	if last.After(endsOn) {
		last = endsOn
	}

	var result []Occurrence
	for date := first; !date.After(last); date = date.AddDate(0, 0, 1) {
		if _, ok := allowedDays[date.Weekday()]; !ok {
			continue
		}
		start, err := localDateTime(date, rule.StartMinute, location)
		if err != nil {
			return nil, err
		}
		endDate := date
		if rule.EndMinute <= rule.StartMinute {
			endDate = endDate.AddDate(0, 0, 1)
		}
		end, err := localDateTime(endDate, rule.EndMinute, location)
		if err != nil {
			return nil, err
		}
		item := rule.Item
		item.StartsAt, item.EndsAt = start.UTC(), end.UTC()
		if err := Validate(item); err != nil {
			return nil, err
		}
		key := occurrenceKey(start, location)
		if exception, ok := exceptionByKey[key]; ok {
			if exception.Cancelled {
				continue
			}
			if exception.Override != nil {
				item = *exception.Override
				if err := Validate(item); err != nil {
					return nil, err
				}
			}
		}
		item.OccurrenceKey = key
		if exception, ok := exceptionByKey[key]; ok {
			item.OccurrenceVersion = exception.Version
		}
		if item.EndsAt.After(from) && item.StartsAt.Before(to) {
			result = append(result, Occurrence{Item: item, OccurrenceKey: key, OriginalStart: start.UTC()})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartsAt.Equal(result[j].StartsAt) {
			return result[i].OccurrenceKey < result[j].OccurrenceKey
		}
		return result[i].StartsAt.Before(result[j].StartsAt)
	})
	return result, nil
}

func localDate(value time.Time, location *time.Location) time.Time {
	value = value.In(location)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
}

func localDateTime(date time.Time, minute int, location *time.Location) (time.Time, error) {
	value := time.Date(date.Year(), date.Month(), date.Day(), minute/60, minute%60, 0, 0, location)
	if value.Year() != date.Year() || value.Month() != date.Month() || value.Day() != date.Day() ||
		value.Hour() != minute/60 || value.Minute() != minute%60 {
		return time.Time{}, fmt.Errorf("%w: local time does not exist", ErrInvalidWeeklyRule)
	}
	return value, nil
}

func occurrenceKey(start time.Time, location *time.Location) string {
	return start.In(location).Format("2006-01-02T15:04")
}

func uniqueWeekdays(values []time.Weekday) []time.Weekday {
	seen := make(map[time.Weekday]struct{}, len(values))
	result := make([]time.Weekday, 0, len(values))
	for _, value := range values {
		if value < time.Sunday || value > time.Saturday {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
