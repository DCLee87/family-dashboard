package database

import (
	"context"
	"testing"
	"time"
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
	if err := db.SaveDashboardPreferences(ctx, "parent_mobile", []string{"board", "weather", "schedule"}, "pc", now); err != nil {
		t.Fatal(err)
	}
	widgets, err := db.DashboardPreferences(ctx, "parent_mobile")
	if err != nil || len(widgets) != 3 || widgets[0] != "board" {
		t.Fatalf("widgets=%v err=%v", widgets, err)
	}
}
