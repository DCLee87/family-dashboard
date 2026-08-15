package app

import (
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

func TestScheduleNotificationSupportsMultipleTimes(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Seoul")
	starts := time.Date(2026, 8, 10, 14, 0, 0, 0, location).UTC()
	item := scheduledomain.Item{TimeKind: scheduledomain.TimeKindTimed, StartsAt: starts}
	setting := database.ScheduleNotificationSetting{Enabled: true, TimedLeadOptions: []int{10, 60}}
	times := scheduleNotifyTimes(item, setting, location)
	if len(times) != 2 || !times[0].Equal(starts.Add(-10*time.Minute)) || !times[1].Equal(starts.Add(-time.Hour)) {
		t.Fatalf("times=%v", times)
	}
}
