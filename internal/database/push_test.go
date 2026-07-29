package database

import (
	"context"
	"testing"
	"time"
)

func TestReplaceAndRevokePushSubscription(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if _, err := database.db.ExecContext(
		ctx,
		`INSERT INTO devices(id, name, device_type, owner, local_only)
		 VALUES ('phone', 'Parent phone', 'parent_mobile', 'dad', 0)`,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	first := PushSubscription{
		ID: "first", DeviceID: "phone", Endpoint: "https://push.example/first",
		EndpointHash: [32]byte{1}, P256DH: "first-key", Auth: "first-auth", Now: now,
	}
	if err := database.ReplacePushSubscription(ctx, first); err != nil {
		t.Fatal(err)
	}
	active, err := database.PushSubscriptionActive(ctx, "phone")
	if err != nil || !active {
		t.Fatalf("first subscription active: got %v, err %v", active, err)
	}

	second := PushSubscription{
		ID: "second", DeviceID: "phone", Endpoint: "https://push.example/second",
		EndpointHash: [32]byte{2}, P256DH: "second-key", Auth: "second-auth",
		Now: now.Add(time.Minute),
	}
	if err := database.ReplacePushSubscription(ctx, second); err != nil {
		t.Fatal(err)
	}
	var activeID string
	if err := database.db.QueryRowContext(
		ctx,
		`SELECT id FROM push_subscriptions
		 WHERE device_id = 'phone' AND revoked_at IS NULL`,
	).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	if activeID != "second" {
		t.Fatalf("active subscription: got %q, want second", activeID)
	}

	if err := database.RevokePushSubscription(ctx, "phone", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	active, err = database.PushSubscriptionActive(ctx, "phone")
	if err != nil || active {
		t.Fatalf("revoked subscription active: got %v, err %v", active, err)
	}
}

func TestDeviceRevocationRevokesPushSubscription(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if _, err := database.db.ExecContext(
		ctx,
		`INSERT INTO devices(id, name, device_type, local_only)
		 VALUES
		   ('trusted-pc', 'Trusted PC', 'trusted_pc', 0),
		   ('phone', 'Parent phone', 'parent_mobile', 0)`,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	if err := database.ReplacePushSubscription(ctx, PushSubscription{
		ID: "subscription", DeviceID: "phone", Endpoint: "https://push.example/device",
		EndpointHash: [32]byte{3}, P256DH: "key", Auth: "auth", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RevokeDevice(ctx, "phone", "trusted-pc", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	active, err := database.PushSubscriptionActive(ctx, "phone")
	if err != nil || active {
		t.Fatalf("device revocation left push active: got %v, err %v", active, err)
	}
}
