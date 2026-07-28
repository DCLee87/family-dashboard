package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidEnrollment = errors.New("invalid or expired enrollment")

type Enrollment struct {
	ID            string
	RequestedType string
	DeviceName    sql.NullString
	Owner         sql.NullString
	Status        string
	ExpiresAt     time.Time
}

type DeviceSummary struct {
	ID         string
	Name       string
	Type       string
	Owner      sql.NullString
	Status     string
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
}

func (d *Database) CreateEnrollment(
	ctx context.Context,
	id string,
	codeHash [32]byte,
	requestedType string,
	expiresAt time.Time,
	now time.Time,
) error {
	_, err := d.db.ExecContext(
		ctx,
		`INSERT INTO enrollment_requests(
		   id, code_hash, requested_type, status, expires_at, created_at
		 ) VALUES (?, ?, ?, 'pending', ?, ?)`,
		id,
		codeHash[:],
		requestedType,
		databaseTime(expiresAt),
		databaseTime(now),
	)
	if err != nil {
		return fmt.Errorf("create enrollment: %w", err)
	}
	return nil
}

func (d *Database) SubmitEnrollment(
	ctx context.Context,
	codeHash [32]byte,
	claimHash [32]byte,
	deviceName string,
	owner string,
	now time.Time,
) (Enrollment, error) {
	var enrollment Enrollment
	var expiresValue string
	err := d.db.QueryRowContext(
		ctx,
		`UPDATE enrollment_requests
		 SET code_hash = ?, device_name = ?, owner = NULLIF(?, ''), status = 'submitted',
		     submitted_at = ?
		 WHERE code_hash = ? AND status = 'pending' AND expires_at > ?
		   AND (
		     (requested_type = 'parent_mobile' AND ? IN ('dad', 'mom'))
		     OR (requested_type = 'shared_tablet' AND ? = '')
		   )
		 RETURNING id, requested_type, device_name, owner, status, expires_at`,
		claimHash[:],
		deviceName,
		owner,
		databaseTime(now),
		codeHash[:],
		databaseTime(now),
		owner,
		owner,
	).Scan(
		&enrollment.ID,
		&enrollment.RequestedType,
		&enrollment.DeviceName,
		&enrollment.Owner,
		&enrollment.Status,
		&expiresValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Enrollment{}, ErrInvalidEnrollment
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("submit enrollment: %w", err)
	}
	enrollment.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresValue)
	if err != nil {
		return Enrollment{}, fmt.Errorf("parse enrollment expiry: %w", err)
	}
	return enrollment, nil
}

func (d *Database) EnrollmentByClaim(
	ctx context.Context,
	claimHash [32]byte,
	now time.Time,
) (Enrollment, error) {
	var enrollment Enrollment
	var expiresValue string
	err := d.db.QueryRowContext(
		ctx,
		`SELECT id, requested_type, device_name, owner, status, expires_at
		 FROM enrollment_requests
		 WHERE code_hash = ? AND expires_at > ?
		   AND status IN ('submitted', 'approved', 'rejected')`,
		claimHash[:],
		databaseTime(now),
	).Scan(
		&enrollment.ID,
		&enrollment.RequestedType,
		&enrollment.DeviceName,
		&enrollment.Owner,
		&enrollment.Status,
		&expiresValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Enrollment{}, ErrInvalidEnrollment
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("read enrollment claim: %w", err)
	}
	enrollment.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresValue)
	if err != nil {
		return Enrollment{}, fmt.Errorf("parse enrollment expiry: %w", err)
	}
	return enrollment, nil
}

func (d *Database) ListEnrollments(ctx context.Context, now time.Time) ([]Enrollment, error) {
	rows, err := d.db.QueryContext(
		ctx,
		`SELECT id, requested_type, device_name, owner, status, expires_at
		 FROM enrollment_requests
		 WHERE expires_at > ? AND status IN ('pending', 'submitted', 'approved')
		 ORDER BY created_at DESC`,
		databaseTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("list enrollments: %w", err)
	}
	defer rows.Close()
	var result []Enrollment
	for rows.Next() {
		var item Enrollment
		var expiresValue string
		if err := rows.Scan(
			&item.ID,
			&item.RequestedType,
			&item.DeviceName,
			&item.Owner,
			&item.Status,
			&expiresValue,
		); err != nil {
			return nil, fmt.Errorf("scan enrollment: %w", err)
		}
		item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresValue)
		if err != nil {
			return nil, fmt.Errorf("parse enrollment expiry: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *Database) ApproveEnrollment(
	ctx context.Context,
	id string,
	approvedBy string,
	now time.Time,
) error {
	result, err := d.db.ExecContext(
		ctx,
		`UPDATE enrollment_requests
		 SET status = 'approved', approved_by_device_id = ?, decided_at = ?
		 WHERE id = ? AND status = 'submitted' AND expires_at > ?`,
		approvedBy,
		databaseTime(now),
		id,
		databaseTime(now),
	)
	if err != nil {
		return fmt.Errorf("approve enrollment: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read enrollment approval: %w", err)
	}
	if changed != 1 {
		return ErrInvalidEnrollment
	}
	return nil
}

func (d *Database) RejectEnrollment(
	ctx context.Context,
	id string,
	rejectedBy string,
	now time.Time,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enrollment rejection: %w", err)
	}
	defer tx.Rollback()
	nowValue := databaseTime(now)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE enrollment_requests
		 SET status = 'rejected', approved_by_device_id = ?, decided_at = ?
		 WHERE id = ? AND status = 'submitted' AND expires_at > ?`,
		rejectedBy,
		nowValue,
		id,
		nowValue,
	)
	if err != nil {
		return fmt.Errorf("reject enrollment: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read enrollment rejection: %w", err)
	}
	if changed != 1 {
		return ErrInvalidEnrollment
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('device_enrollment', ?, 'failure', 'administrator_rejected', ?)`,
		rejectedBy,
		nowValue,
	); err != nil {
		return fmt.Errorf("record enrollment rejection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enrollment rejection: %w", err)
	}
	return nil
}

func (d *Database) CompleteEnrollment(
	ctx context.Context,
	claimHash [32]byte,
	tombstoneHash [32]byte,
	setup InitialSetup,
	owner string,
) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enrollment completion: %w", err)
	}
	defer tx.Rollback()
	now := databaseTime(setup.Now)
	var enrollmentID string
	var deviceName string
	var requestedType string
	err = tx.QueryRowContext(
		ctx,
		`UPDATE enrollment_requests
		 SET code_hash = ?
		 WHERE code_hash = ? AND status = 'approved' AND expires_at > ?
		 RETURNING id, device_name, requested_type`,
		tombstoneHash[:],
		claimHash[:],
		now,
	).Scan(&enrollmentID, &deviceName, &requestedType)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidEnrollment
	}
	if err != nil {
		return fmt.Errorf("consume enrollment claim: %w", err)
	}
	localOnly := 0
	if requestedType == "shared_tablet" || requestedType == "tv" {
		localOnly = 1
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO devices(
		   id, name, device_type, owner, local_only, status, created_at, last_used_at
		 ) VALUES (?, ?, ?, NULLIF(?, ''), ?, 'active', ?, ?)`,
		setup.DeviceID,
		deviceName,
		requestedType,
		owner,
		localOnly,
		now,
		now,
	); err != nil {
		return fmt.Errorf("create enrolled device: %w", err)
	}
	for _, credential := range []struct {
		id, kind, family string
		hash             [32]byte
		expires          time.Time
	}{
		{setup.AccessID, "access", setup.RefreshFamilyID, setup.AccessHash, setup.AccessExpiresAt},
		{setup.RefreshID, "refresh", setup.RefreshFamilyID, setup.RefreshHash, setup.RefreshExpiresAt},
	} {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO device_credentials(
			   id, device_id, credential_type, token_hash, family_id,
			   expires_at, created_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			credential.id,
			setup.DeviceID,
			credential.kind,
			credential.hash[:],
			credential.family,
			databaseTime(credential.expires),
			now,
		); err != nil {
			return fmt.Errorf("create enrolled credential: %w", err)
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('device_enrollment', ?, 'success', ?, ?)`,
		setup.DeviceID,
		requestedType+"_approved",
		now,
	); err != nil {
		return fmt.Errorf("record enrollment event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enrollment completion: %w", err)
	}
	return nil
}

func (d *Database) ListDevices(ctx context.Context) ([]DeviceSummary, error) {
	rows, err := d.db.QueryContext(
		ctx,
		`SELECT id, name, device_type, owner, status, created_at, last_used_at
		 FROM devices
		 WHERE status = 'active'
		 ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	var result []DeviceSummary
	for rows.Next() {
		var item DeviceSummary
		var created string
		var lastUsed sql.NullString
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Type, &item.Owner, &item.Status,
			&created, &lastUsed,
		); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse device creation: %w", err)
		}
		if lastUsed.Valid {
			value, err := time.Parse(time.RFC3339Nano, lastUsed.String)
			if err != nil {
				return nil, fmt.Errorf("parse device use: %w", err)
			}
			item.LastUsedAt = sql.NullTime{Time: value, Valid: true}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *Database) RevokeDevice(
	ctx context.Context,
	targetID string,
	actorID string,
	now time.Time,
) error {
	if targetID == actorID {
		return fmt.Errorf("cannot revoke current device")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device revocation: %w", err)
	}
	defer tx.Rollback()
	nowValue := databaseTime(now)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE devices SET status = 'revoked', revoked_at = ?
		 WHERE id = ? AND status = 'active'`,
		nowValue,
		targetID,
	)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read device revocation: %w", err)
	}
	if changed != 1 {
		return ErrUnauthenticated
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE device_credentials SET revoked_at = ?
		 WHERE device_id = ? AND revoked_at IS NULL`,
		nowValue,
		targetID,
	); err != nil {
		return fmt.Errorf("revoke device credentials: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE admin_sessions SET revoked_at = ?
		 WHERE device_id = ? AND revoked_at IS NULL`,
		nowValue,
		targetID,
	); err != nil {
		return fmt.Errorf("revoke device sessions: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO security_events(event_type, device_id, result, reason, created_at)
		 VALUES ('device_revocation', ?, 'success', 'administrator_action', ?)`,
		targetID,
		nowValue,
	); err != nil {
		return fmt.Errorf("record device revocation: %w", err)
	}
	return tx.Commit()
}
