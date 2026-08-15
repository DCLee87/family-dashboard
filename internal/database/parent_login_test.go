package database

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParentLoginAccountLockAndCredentialCreation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SetParentPassword(ctx, "mom", "password-hash", now); err != nil {
		t.Fatal(err)
	}
	account, err := db.ParentAccountForLogin(ctx, "mom", now)
	if err != nil || account.PasswordHash != "password-hash" {
		t.Fatalf("account lookup: %#v %v", account, err)
	}
	for i := 0; i < 5; i++ {
		if err := db.RecordParentLoginFailure(ctx, "mom", "192.0.2.10", now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ParentAccountForLogin(ctx, "mom", now.Add(time.Minute)); !errors.Is(err, ErrParentLoginBlocked) {
		t.Fatalf("locked account error: %v", err)
	}
	later := now.Add(16 * time.Minute)
	login := ParentLoginDevice{Owner: "mom", PasswordHash: "password-hash", DeviceID: "mom-login", DeviceName: "Mom phone", AccessID: "access", AccessHash: [32]byte{1}, AccessExpiresAt: later.Add(time.Hour), RefreshID: "refresh", RefreshHash: [32]byte{2}, RefreshFamilyID: "family", RefreshExpiresAt: later.Add(24 * time.Hour), RemoteAddress: "192.0.2.10", Now: later}
	if err := db.CreateParentLoginDevice(ctx, login); err != nil {
		t.Fatal(err)
	}
	device, err := db.DeviceByAccessToken(ctx, [32]byte{1}, later)
	if err != nil {
		t.Fatal(err)
	}
	if device.Type != "parent_mobile" || !device.Owner.Valid || device.Owner.String != "mom" {
		t.Fatalf("unexpected login device: %#v", device)
	}
	if err := db.LogoutDevice(ctx, device.ID, later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeviceByAccessToken(ctx, [32]byte{1}, later.Add(2*time.Minute)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("logged out credential remained valid: %v", err)
	}
}
