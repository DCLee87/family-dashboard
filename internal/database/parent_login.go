package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrParentLoginBlocked = errors.New("parent login temporarily blocked")

type ParentAccount struct {
	Owner        string
	PasswordHash string
	Enabled      bool
	FailedCount  int
	BlockedUntil sql.NullTime
}

type ParentAccountStatus struct {
	Owner      string `json:"owner"`
	Configured bool   `json:"configured"`
	Enabled    bool   `json:"enabled"`
}

type ParentLoginDevice struct {
	Owner            string
	PasswordHash     string
	DeviceID         string
	DeviceName       string
	AccessID         string
	AccessHash       [32]byte
	AccessExpiresAt  time.Time
	RefreshID        string
	RefreshHash      [32]byte
	RefreshFamilyID  string
	RefreshExpiresAt time.Time
	RemoteAddress    string
	Now              time.Time
}

func validParentOwner(owner string) bool { return owner == "dad" || owner == "mom" }

func (d *Database) SetParentPassword(ctx context.Context, owner, passwordHash string, now time.Time) error {
	if !validParentOwner(owner) || passwordHash == "" {
		return fmt.Errorf("invalid parent account")
	}
	nowValue := databaseTime(now)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin parent password update: %w", err)
	}
	defer tx.Rollback()
	if err := revokeParentLoginDevices(ctx, tx, owner, nowValue); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO parent_accounts(owner,password_hash,enabled,failed_count,blocked_until,created_at,updated_at,password_changed_at)
	 VALUES (?,?,1,0,NULL,?,?,?)
	 ON CONFLICT(owner) DO UPDATE SET password_hash=excluded.password_hash,enabled=1,failed_count=0,blocked_until=NULL,updated_at=excluded.updated_at,password_changed_at=excluded.password_changed_at`, owner, passwordHash, nowValue, nowValue, nowValue)
	if err != nil {
		return fmt.Errorf("set parent password: %w", err)
	}
	return tx.Commit()
}

func (d *Database) DisableParentAccount(ctx context.Context, owner string, now time.Time) error {
	if !validParentOwner(owner) {
		return fmt.Errorf("invalid parent account")
	}
	nowValue := databaseTime(now)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := revokeParentLoginDevices(ctx, tx, owner, nowValue); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE parent_accounts SET enabled=0,failed_count=0,blocked_until=NULL,updated_at=? WHERE owner=?`, nowValue, owner)
	if err != nil {
		return fmt.Errorf("disable parent account: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrUnauthenticated
	}
	return tx.Commit()
}

func revokeParentLoginDevices(ctx context.Context, tx *sql.Tx, owner, nowValue string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE device_credentials SET revoked_at=? WHERE revoked_at IS NULL AND device_id IN (SELECT device_id FROM parent_login_devices WHERE owner=?)`, nowValue, owner); err != nil {
		return fmt.Errorf("revoke parent login credentials: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at=? WHERE revoked_at IS NULL AND device_id IN (SELECT device_id FROM parent_login_devices WHERE owner=?)`, nowValue, owner); err != nil {
		return fmt.Errorf("revoke parent login sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET status='revoked',revoked_at=? WHERE status='active' AND id IN (SELECT device_id FROM parent_login_devices WHERE owner=?)`, nowValue, owner); err != nil {
		return fmt.Errorf("revoke parent login devices: %w", err)
	}
	return nil
}

func (d *Database) ParentAccountStatuses(ctx context.Context) ([]ParentAccountStatus, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT owner,enabled FROM parent_accounts ORDER BY owner`)
	if err != nil {
		return nil, fmt.Errorf("list parent accounts: %w", err)
	}
	defer rows.Close()
	statuses := map[string]ParentAccountStatus{"dad": {Owner: "dad"}, "mom": {Owner: "mom"}}
	for rows.Next() {
		var owner string
		var enabled int
		if err := rows.Scan(&owner, &enabled); err != nil {
			return nil, err
		}
		statuses[owner] = ParentAccountStatus{Owner: owner, Configured: true, Enabled: enabled == 1}
	}
	return []ParentAccountStatus{statuses["dad"], statuses["mom"]}, rows.Err()
}

func (d *Database) ParentAccountForLogin(ctx context.Context, owner string, now time.Time) (ParentAccount, error) {
	var account ParentAccount
	var enabled int
	var blocked sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT owner,password_hash,enabled,failed_count,blocked_until FROM parent_accounts WHERE owner=?`, owner).Scan(&account.Owner, &account.PasswordHash, &enabled, &account.FailedCount, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return ParentAccount{}, ErrUnauthenticated
	}
	if err != nil {
		return ParentAccount{}, fmt.Errorf("read parent account: %w", err)
	}
	account.Enabled = enabled == 1
	if blocked.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, blocked.String)
		if err != nil {
			return ParentAccount{}, fmt.Errorf("parse parent login block: %w", err)
		}
		account.BlockedUntil = sql.NullTime{Time: parsed, Valid: true}
		if now.Before(parsed) {
			return account, ErrParentLoginBlocked
		}
	}
	if !account.Enabled {
		return ParentAccount{}, ErrUnauthenticated
	}
	return account, nil
}

func (d *Database) RecordParentLoginFailure(ctx context.Context, owner, remote string, now time.Time) error {
	if !validParentOwner(owner) {
		owner = ""
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if owner != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE parent_accounts SET failed_count=failed_count+1,blocked_until=CASE WHEN failed_count+1>=5 THEN ? ELSE blocked_until END,updated_at=? WHERE owner=?`, databaseTime(now.Add(15*time.Minute)), databaseTime(now), owner); err != nil {
			return fmt.Errorf("record parent login failure: %w", err)
		}
	}
	var ownerValue any = nil
	if owner != "" {
		ownerValue = owner
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO parent_login_events(owner,result,reason,remote_address,created_at) VALUES (?,'failure','invalid_credentials',NULLIF(?,''),?)`, ownerValue, remote, databaseTime(now)); err != nil {
		return fmt.Errorf("record parent login event: %w", err)
	}
	return tx.Commit()
}

func (d *Database) RecordParentLoginBlocked(ctx context.Context, owner, remote string, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO parent_login_events(owner,result,reason,remote_address,created_at) VALUES (?,'blocked','rate_limit',NULLIF(?,''),?)`, owner, remote, databaseTime(now))
	return err
}

func (d *Database) CreateParentLoginDevice(ctx context.Context, login ParentLoginDevice) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin parent login: %w", err)
	}
	defer tx.Rollback()
	now := databaseTime(login.Now)
	result, err := tx.ExecContext(ctx, `UPDATE parent_accounts SET failed_count=0,blocked_until=NULL,updated_at=? WHERE owner=? AND enabled=1 AND password_hash=?`, now, login.Owner, login.PasswordHash)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrUnauthenticated
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO devices(id,name,device_type,owner,local_only,status,created_at,last_used_at) VALUES (?,?,'parent_mobile',?,0,'active',?,?)`, login.DeviceID, login.DeviceName, login.Owner, now, now); err != nil {
		return fmt.Errorf("create parent login device: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO parent_login_devices(device_id,owner,created_at) VALUES (?,?,?)`, login.DeviceID, login.Owner, now); err != nil {
		return fmt.Errorf("mark parent login device: %w", err)
	}
	for _, c := range []struct {
		id, kind string
		hash     [32]byte
		expires  time.Time
	}{{login.AccessID, "access", login.AccessHash, login.AccessExpiresAt}, {login.RefreshID, "refresh", login.RefreshHash, login.RefreshExpiresAt}} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO device_credentials(id,device_id,credential_type,token_hash,family_id,expires_at,created_at) VALUES (?,?,?,?,?,?,?)`, c.id, login.DeviceID, c.kind, c.hash[:], login.RefreshFamilyID, databaseTime(c.expires), now); err != nil {
			return fmt.Errorf("create parent login credential: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO parent_login_events(owner,result,reason,remote_address,created_at) VALUES (?,'success','password_login',NULLIF(?,''),?)`, login.Owner, login.RemoteAddress, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) LogoutDevice(ctx context.Context, deviceID string, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nowValue := databaseTime(now)
	if _, err = tx.ExecContext(ctx, `UPDATE device_credentials SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowValue, deviceID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowValue, deviceID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE devices SET status='revoked',revoked_at=? WHERE id=? AND status='active'`, nowValue, deviceID); err != nil {
		return err
	}
	return tx.Commit()
}
