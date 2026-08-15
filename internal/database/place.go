package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPlaceNotFound = errors.New("place not found")

type Place struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RegionLabel string    `json:"regionLabel"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	IsHome      bool      `json:"isHome"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
type WeatherCache struct {
	Payload   []byte
	FetchedAt time.Time
}

func validPlace(p Place) bool {
	return p.ID != "" && len(p.Name) >= 1 && len(p.Name) <= 80 && len(p.RegionLabel) >= 1 && len(p.RegionLabel) <= 120 && p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180
}

func (d *Database) UpsertPlace(ctx context.Context, p Place, deviceID string, now time.Time) (Place, error) {
	if !validPlace(p) {
		return Place{}, errors.New("invalid place")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Place{}, err
	}
	defer tx.Rollback()
	if p.IsHome {
		if _, err = tx.ExecContext(ctx, `UPDATE places SET is_home=0 WHERE is_home=1 AND id<>?`, p.ID); err != nil {
			return Place{}, err
		}
	}
	if p.Version == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO places(id,name,region_label,latitude,longitude,is_home,created_by_device_id,updated_by_device_id,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?)`, p.ID, p.Name, p.RegionLabel, p.Latitude, p.Longitude, p.IsHome, deviceID, deviceID, databaseTime(now), databaseTime(now))
	} else {
		result, e := tx.ExecContext(ctx, `UPDATE places SET name=?,region_label=?,latitude=?,longitude=?,is_home=?,updated_by_device_id=?,updated_at=?,version=version+1 WHERE id=? AND version=?`, p.Name, p.RegionLabel, p.Latitude, p.Longitude, p.IsHome, deviceID, databaseTime(now), p.ID, p.Version)
		err = e
		if err == nil {
			changed, _ := result.RowsAffected()
			if changed != 1 {
				err = ErrPlaceNotFound
			}
		}
	}
	if err != nil {
		return Place{}, err
	}
	if err = tx.Commit(); err != nil {
		return Place{}, err
	}
	return d.PlaceByID(ctx, p.ID)
}
func (d *Database) Places(ctx context.Context) ([]Place, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,name,region_label,latitude,longitude,is_home,version,updated_at FROM places ORDER BY is_home DESC,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Place
	for rows.Next() {
		var p Place
		var updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.RegionLabel, &p.Latitude, &p.Longitude, &p.IsHome, &p.Version, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt, _ = parseDatabaseTime(updated)
		result = append(result, p)
	}
	return result, rows.Err()
}
func (d *Database) PlaceByID(ctx context.Context, id string) (Place, error) {
	var p Place
	var updated string
	err := d.db.QueryRowContext(ctx, `SELECT id,name,region_label,latitude,longitude,is_home,version,updated_at FROM places WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.RegionLabel, &p.Latitude, &p.Longitude, &p.IsHome, &p.Version, &updated)
	if err != nil {
		return Place{}, ErrPlaceNotFound
	}
	p.UpdatedAt, _ = parseDatabaseTime(updated)
	return p, nil
}
func (d *Database) DeletePlace(ctx context.Context, id string, version int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM places WHERE id=? AND version=?`, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrPlaceNotFound
	}
	return nil
}
func (d *Database) WeatherCache(ctx context.Context, placeID string) (WeatherCache, error) {
	var payload, fetched string
	err := d.db.QueryRowContext(ctx, `SELECT payload,fetched_at FROM weather_cache WHERE place_id=?`, placeID).Scan(&payload, &fetched)
	if err != nil {
		return WeatherCache{}, err
	}
	at, _ := parseDatabaseTime(fetched)
	return WeatherCache{Payload: []byte(payload), FetchedAt: at}, nil
}
func (d *Database) SaveWeatherCache(ctx context.Context, placeID string, payload []byte, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO weather_cache(place_id,payload,fetched_at) VALUES(?,?,?) ON CONFLICT(place_id) DO UPDATE SET payload=excluded.payload,fetched_at=excluded.fetched_at,last_error_at=NULL`, placeID, string(payload), databaseTime(now))
	return err
}
func (d *Database) DashboardPreferences(ctx context.Context, deviceType string) ([]string, error) {
	var value string
	err := d.db.QueryRowContext(ctx, `SELECT widget_order FROM dashboard_preferences WHERE device_type=?`, deviceType).Scan(&value)
	if err != nil {
		return nil, err
	}
	return strings.Split(value, ","), nil
}
func (d *Database) SaveDashboardPreferences(ctx context.Context, deviceType string, widgets []string, deviceID string, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO dashboard_preferences(device_type,widget_order,updated_by_device_id,updated_at) VALUES(?,?,?,?) ON CONFLICT(device_type) DO UPDATE SET widget_order=excluded.widget_order,updated_by_device_id=excluded.updated_by_device_id,updated_at=excluded.updated_at`, deviceType, strings.Join(widgets, ","), deviceID, databaseTime(now))
	return err
}

func (d *Database) WeatherPreferences(ctx context.Context, deviceType string) (int, int, error) {
	var days, hours int
	err := d.db.QueryRowContext(ctx, `SELECT daily_days,hourly_hours FROM device_weather_preferences WHERE device_type=?`, deviceType).Scan(&days, &hours)
	return days, hours, err
}

func (d *Database) SaveWeatherPreferences(ctx context.Context, deviceType string, days, hours int, deviceID string, now time.Time) error {
	if days < 1 || days > 7 || hours < 0 || hours > 48 {
		return fmt.Errorf("invalid weather preferences")
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO device_weather_preferences(device_type,daily_days,hourly_hours,updated_by_device_id,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(device_type) DO UPDATE SET daily_days=excluded.daily_days,hourly_hours=excluded.hourly_hours,updated_by_device_id=excluded.updated_by_device_id,updated_at=excluded.updated_at`, deviceType, days, hours, deviceID, databaseTime(now))
	return err
}
