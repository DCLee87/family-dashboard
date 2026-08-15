package database

import (
	"context"
	"testing"
	"time"

	scheduledomain "github.com/DCLee87/family-dashboard/internal/schedule"
)

func TestC5BoardPlaceAndPreferences(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.db.Exec(`INSERT INTO devices(id,name,device_type,local_only,status) VALUES('pc','PC','trusted_pc',0,'active')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	item, err := db.CreateBoardItem(ctx, BoardItem{ID: "notice-1", Kind: "notice", Title: "준비물", Body: "물통을 챙겨요", Priority: "important", Visibility: "family", StartsOn: "2026-08-08", PushEnabled: true}, "pc", now)
	if err != nil {
		t.Fatal(err)
	}
	if item.Version != 1 || item.Title != "준비물" {
		t.Fatalf("unexpected board item: %+v", item)
	}
	if err := db.SaveBoardRecipients(ctx, item.ID, []string{"dad"}); err != nil {
		t.Fatal(err)
	}
	storedBoard, err := db.BoardItemByID(ctx, item.ID)
	if err != nil || len(storedBoard.Recipients) != 1 || storedBoard.Recipients[0] != "dad" {
		t.Fatalf("board recipients=%v err=%v", storedBoard.Recipients, err)
	}
	if err := db.SetBoardItemStatus(ctx, item.ID, "archived", item.Version, "pc", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	archived, err := db.ArchivedBoardItems(ctx)
	if err != nil || len(archived) != 1 {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	place, err := db.UpsertPlace(ctx, Place{ID: "home", Name: "집", RegionLabel: "서울특별시", Latitude: 37.56, Longitude: 126.97, IsHome: true}, "pc", now)
	if err != nil || !place.IsHome {
		t.Fatalf("place=%v err=%v", place, err)
	}
	schedule, err := db.CreateSchedule(ctx, scheduledomain.Item{ID: "schedule-place", Title: "외출", LocationName: "집", PlaceID: place.ID, Visibility: "family", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Participants: []string{"dad"}}, "pc", now)
	if err != nil || schedule.PlaceID != place.ID {
		t.Fatalf("schedule place=%q err=%v", schedule.PlaceID, err)
	}
	setting := ScheduleNotificationSetting{Enabled: true, TimedLeadMinutes: 30, AllDayHour: 20, TimedLeadOptions: []int{10, 30}, AllDayHours: []int{9, 20}, Recipients: []string{"dad"}}
	if err := db.SaveScheduleNotificationSetting(ctx, schedule.ID, setting, now); err != nil {
		t.Fatal(err)
	}
	storedSetting, err := db.ScheduleNotificationSetting(ctx, schedule.ID)
	if err != nil || len(storedSetting.TimedLeadOptions) != 2 || len(storedSetting.Recipients) != 1 || storedSetting.Recipients[0] != "dad" {
		t.Fatalf("schedule notification=%+v err=%v", storedSetting, err)
	}
	if err := db.initialize(ctx, false); err != nil {
		t.Fatal(err)
	}
	storedSetting, err = db.ScheduleNotificationSetting(ctx, schedule.ID)
	if err != nil || len(storedSetting.Recipients) != 1 || storedSetting.Recipients[0] != "dad" {
		t.Fatalf("recipients changed after restart: %+v err=%v", storedSetting, err)
	}
	if err := db.SaveDashboardPreferences(ctx, "parent_mobile", []string{"board", "weather", "schedule"}, "pc", now); err != nil {
		t.Fatal(err)
	}
	widgets, err := db.DashboardPreferences(ctx, "parent_mobile")
	if err != nil || len(widgets) != 3 || widgets[0] != "board" {
		t.Fatalf("widgets=%v err=%v", widgets, err)
	}
	if err := db.SaveWeatherPreferences(ctx, "parent_mobile", 4, 12, "pc", now); err != nil {
		t.Fatal(err)
	}
	days, hours, err := db.WeatherPreferences(ctx, "parent_mobile")
	if err != nil || days != 4 || hours != 12 {
		t.Fatalf("weather preferences=%d/%d err=%v", days, hours, err)
	}
	window, err := db.NotificationWindow(ctx, "task", now, time.Hour)
	if err != nil || !window.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("initial window=%s err=%v", window, err)
	}
	if err := db.MarkNotificationWorker(ctx, "task", now); err != nil {
		t.Fatal(err)
	}
	window, err = db.NotificationWindow(ctx, "task", now.Add(10*time.Minute), time.Hour)
	if err != nil || !window.Equal(now.Add(-15*time.Second)) {
		t.Fatalf("stored window=%s err=%v", window, err)
	}
}
