package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const BoardTrashRetention = 30 * 24 * time.Hour

var (
	ErrBoardItemNotFound = errors.New("board item not found")
	ErrBoardItemConflict = errors.New("board item version conflict")
)

type BoardItem struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Priority    string     `json:"priority"`
	Visibility  string     `json:"visibility"`
	StartsOn    string     `json:"startsOn"`
	EndsOn      string     `json:"endsOn"`
	Status      string     `json:"status"`
	PushEnabled bool       `json:"pushEnabled"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
	Recipients  []string   `json:"recipients"`
}

func validBoardItem(item BoardItem) bool {
	item.Title, item.Body = strings.TrimSpace(item.Title), strings.TrimSpace(item.Body)
	if item.ID == "" || len(item.Title) < 1 || len(item.Title) > 120 || len(item.Body) < 1 || len(item.Body) > 4000 {
		return false
	}
	if item.Kind != "notice" && item.Kind != "memo" {
		return false
	}
	if item.Priority != "normal" && item.Priority != "important" {
		return false
	}
	if item.Visibility != "family" && item.Visibility != "tv_summary" && item.Visibility != "parents_only" {
		return false
	}
	start, err := time.Parse("2006-01-02", item.StartsOn)
	if err != nil {
		return false
	}
	if item.EndsOn != "" {
		if end, err := time.Parse("2006-01-02", item.EndsOn); err != nil || end.Before(start) {
			return false
		}
	}
	return true
}

func (d *Database) CreateBoardItem(ctx context.Context, item BoardItem, deviceID string, now time.Time) (BoardItem, error) {
	if !validBoardItem(item) {
		return BoardItem{}, errors.New("invalid board item")
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO board_items(id,kind,title,body,priority,visibility,starts_on,ends_on,push_enabled,status,created_by_device_id,updated_by_device_id,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'published',?,?,1,?,?)`, item.ID, item.Kind, strings.TrimSpace(item.Title), strings.TrimSpace(item.Body), item.Priority, item.Visibility, item.StartsOn, nullable(item.EndsOn), item.PushEnabled, deviceID, deviceID, databaseTime(now), databaseTime(now))
	if err != nil {
		return BoardItem{}, fmt.Errorf("create board item: %w", err)
	}
	return d.BoardItemByID(ctx, item.ID)
}

func (d *Database) UpdateBoardItem(ctx context.Context, item BoardItem, deviceID string, now time.Time) (BoardItem, error) {
	if !validBoardItem(item) {
		return BoardItem{}, errors.New("invalid board item")
	}
	result, err := d.db.ExecContext(ctx, `UPDATE board_items SET kind=?,title=?,body=?,priority=?,visibility=?,starts_on=?,ends_on=?,push_enabled=?,updated_by_device_id=?,updated_at=?,version=version+1 WHERE id=? AND version=? AND status='published'`, item.Kind, strings.TrimSpace(item.Title), strings.TrimSpace(item.Body), item.Priority, item.Visibility, item.StartsOn, nullable(item.EndsOn), item.PushEnabled, deviceID, databaseTime(now), item.ID, item.Version)
	if err != nil {
		return BoardItem{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return BoardItem{}, d.boardWriteError(ctx, item.ID)
	}
	return d.BoardItemByID(ctx, item.ID)
}

func (d *Database) BoardItemByID(ctx context.Context, id string) (BoardItem, error) {
	items, err := d.queryBoardItems(ctx, `WHERE id=?`, id)
	if err != nil {
		return BoardItem{}, err
	}
	if len(items) == 0 {
		return BoardItem{}, ErrBoardItemNotFound
	}
	return items[0], nil
}
func (d *Database) PublishedBoardItems(ctx context.Context, on string) ([]BoardItem, error) {
	return d.queryBoardItems(ctx, `WHERE status='published' AND starts_on<=? AND (ends_on IS NULL OR ends_on>=?) ORDER BY CASE priority WHEN 'important' THEN 0 ELSE 1 END, created_at DESC`, on, on)
}
func (d *Database) ArchivedBoardItems(ctx context.Context) ([]BoardItem, error) {
	return d.queryBoardItems(ctx, `WHERE status='archived' ORDER BY archived_at DESC`)
}
func (d *Database) TrashedBoardItems(ctx context.Context) ([]BoardItem, error) {
	return d.queryBoardItems(ctx, `WHERE status='trashed' ORDER BY deleted_at DESC`)
}

func (d *Database) queryBoardItems(ctx context.Context, clause string, args ...any) ([]BoardItem, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,kind,title,body,priority,visibility,starts_on,ends_on,push_enabled,status,version,created_at,updated_at,archived_at,deleted_at FROM board_items `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []BoardItem
	for rows.Next() {
		var item BoardItem
		var ends, archived, deleted sql.NullString
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Body, &item.Priority, &item.Visibility, &item.StartsOn, &ends, &item.PushEnabled, &item.Status, &item.Version, &created, &updated, &archived, &deleted); err != nil {
			return nil, err
		}
		item.EndsOn = ends.String
		item.CreatedAt, _ = parseDatabaseTime(created)
		item.UpdatedAt, _ = parseDatabaseTime(updated)
		item.ArchivedAt = parseNullableDBTime(archived)
		item.DeletedAt = parseNullableDBTime(deleted)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		recipientRows, recipientErr := d.db.QueryContext(ctx, `SELECT owner FROM board_notification_recipients WHERE board_item_id=? ORDER BY owner`, result[index].ID)
		if recipientErr != nil {
			return nil, recipientErr
		}
		for recipientRows.Next() {
			var owner string
			if err := recipientRows.Scan(&owner); err != nil {
				recipientRows.Close()
				return nil, err
			}
			result[index].Recipients = append(result[index].Recipients, owner)
		}
		recipientRows.Close()
		if len(result[index].Recipients) == 0 {
			result[index].Recipients = []string{"dad", "mom"}
		}
	}
	return result, nil
}

func (d *Database) SaveBoardRecipients(ctx context.Context, itemID string, recipients []string) error {
	recipients = uniqueOwners(recipients)
	if len(recipients) == 0 {
		return errors.New("invalid board recipients")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM board_notification_recipients WHERE board_item_id=?`, itemID); err != nil {
		return err
	}
	for _, owner := range recipients {
		if _, err = tx.ExecContext(ctx, `INSERT INTO board_notification_recipients(board_item_id,owner) VALUES(?,?)`, itemID, owner); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func parseNullableDBTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := parseDatabaseTime(value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func (d *Database) SetBoardItemStatus(ctx context.Context, id, status string, version int64, deviceID string, now time.Time) error {
	if status != "published" && status != "archived" && status != "trashed" {
		return errors.New("invalid board status")
	}
	archived, deleted := any(nil), any(nil)
	if status == "archived" {
		archived = databaseTime(now)
	}
	if status == "trashed" {
		deleted = databaseTime(now)
	}
	result, err := d.db.ExecContext(ctx, `UPDATE board_items SET status=?,archived_at=?,deleted_at=?,updated_by_device_id=?,updated_at=?,version=version+1 WHERE id=? AND version=?`, status, archived, deleted, deviceID, databaseTime(now), id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return d.boardWriteError(ctx, id)
	}
	return nil
}
func (d *Database) PermanentlyDeleteBoardItem(ctx context.Context, id string, version int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM board_items WHERE id=? AND version=? AND status='trashed'`, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return d.boardWriteError(ctx, id)
	}
	return nil
}
func (d *Database) PurgeExpiredBoardItems(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `DELETE FROM board_items WHERE status='trashed' AND deleted_at<=?`, databaseTime(now.Add(-BoardTrashRetention)))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (d *Database) ArchiveExpiredBoardItems(ctx context.Context, today string, now time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `UPDATE board_items SET status='archived',archived_at=?,updated_at=?,version=version+1 WHERE status='published' AND ends_on IS NOT NULL AND ends_on<?`, databaseTime(now), databaseTime(now), today)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (d *Database) boardWriteError(ctx context.Context, id string) error {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM board_items WHERE id=?`, id).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrBoardItemNotFound
	}
	return ErrBoardItemConflict
}
