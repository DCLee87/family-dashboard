package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PushSubscription struct {
	ID           string
	DeviceID     string
	Endpoint     string
	EndpointHash [32]byte
	P256DH       string
	Auth         string
	Now          time.Time
}

func (d *Database) PushSubscriptionActive(
	ctx context.Context,
	deviceID string,
) (bool, error) {
	var count int
	if err := d.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM push_subscriptions
		 WHERE device_id = ? AND revoked_at IS NULL`,
		deviceID,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("read push subscription state: %w", err)
	}
	return count == 1, nil
}

func (d *Database) ActivePushSubscription(
	ctx context.Context,
	deviceID string,
) (PushSubscription, error) {
	var subscription PushSubscription
	var endpointHash []byte
	err := d.db.QueryRowContext(
		ctx,
		`SELECT id, device_id, endpoint, endpoint_hash, p256dh, auth
		 FROM push_subscriptions
		 WHERE device_id = ? AND revoked_at IS NULL`,
		deviceID,
	).Scan(
		&subscription.ID,
		&subscription.DeviceID,
		&subscription.Endpoint,
		&endpointHash,
		&subscription.P256DH,
		&subscription.Auth,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PushSubscription{}, ErrUnauthenticated
	}
	if err != nil {
		return PushSubscription{}, fmt.Errorf("read active push subscription: %w", err)
	}
	if len(endpointHash) != len(subscription.EndpointHash) {
		return PushSubscription{}, fmt.Errorf("read active push subscription: invalid endpoint hash")
	}
	copy(subscription.EndpointHash[:], endpointHash)
	return subscription, nil
}

func (d *Database) ReplacePushSubscription(
	ctx context.Context,
	subscription PushSubscription,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin push subscription replacement: %w", err)
	}
	defer tx.Rollback()

	nowValue := databaseTime(subscription.Now)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE push_subscriptions SET revoked_at = ?, updated_at = ?
		 WHERE device_id = ? AND revoked_at IS NULL`,
		nowValue,
		nowValue,
		subscription.DeviceID,
	); err != nil {
		return fmt.Errorf("revoke prior push subscription: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO push_subscriptions(
		   id, device_id, endpoint, endpoint_hash, p256dh, auth,
		   created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		subscription.ID,
		subscription.DeviceID,
		subscription.Endpoint,
		subscription.EndpointHash[:],
		subscription.P256DH,
		subscription.Auth,
		nowValue,
		nowValue,
	); err != nil {
		return fmt.Errorf("create push subscription: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit push subscription replacement: %w", err)
	}
	return nil
}

func (d *Database) RevokePushSubscription(
	ctx context.Context,
	deviceID string,
	now time.Time,
) error {
	nowValue := databaseTime(now)
	if _, err := d.db.ExecContext(
		ctx,
		`UPDATE push_subscriptions SET revoked_at = ?, updated_at = ?
		 WHERE device_id = ? AND revoked_at IS NULL`,
		nowValue,
		nowValue,
		deviceID,
	); err != nil {
		return fmt.Errorf("revoke push subscription: %w", err)
	}
	return nil
}
