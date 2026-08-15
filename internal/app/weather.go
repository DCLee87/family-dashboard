package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

type placeRequest struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	RegionLabel string  `json:"regionLabel"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	IsHome      bool    `json:"isHome"`
	Version     int64   `json:"version"`
}

func (a *App) listPlaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedDevice(w, r); !ok {
		return
	}
	places, err := a.db.Places(r.Context())
	if err != nil {
		writeAPIError(w, 500, "place_list_failed", "장소를 불러오지 못했습니다.")
		return
	}
	writeJSON(w, 200, map[string]any{"places": places})
}
func (a *App) savePlace(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request placeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		writeAPIError(w, 400, "invalid_place", "장소 정보를 다시 확인해 주세요.")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		id = security.NewToken()
		request.Version = 0
	}
	place, err := a.db.UpsertPlace(r.Context(), database.Place{ID: id, Name: request.Name, RegionLabel: request.RegionLabel, Latitude: request.Latitude, Longitude: request.Longitude, IsHome: request.IsHome, Version: request.Version}, device.ID, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 400, "invalid_place", "장소 정보를 다시 확인해 주세요.")
		return
	}
	status := 200
	if request.Version == 0 {
		status = 201
	}
	writeJSON(w, status, place)
}
func (a *App) deletePlace(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil {
		writeAPIError(w, 400, "invalid_request", "버전을 확인해 주세요.")
		return
	}
	if err = a.db.DeletePlace(r.Context(), r.PathValue("id"), version); err != nil {
		writeAPIError(w, 404, "place_not_found", "장소를 찾을 수 없습니다.")
		return
	}
	w.WriteHeader(204)
}

func (a *App) searchPlaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdminRead(w, r); !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		writeAPIError(w, 400, "invalid_query", "지역명을 두 글자 이상 입력해 주세요.")
		return
	}
	endpoint, _ := url.Parse("https://geocoding-api.open-meteo.com/v1/search")
	values := endpoint.Query()
	values.Set("name", query)
	values.Set("count", "8")
	values.Set("language", "ko")
	values.Set("format", "json")
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		writeAPIError(w, 500, "place_search_failed", "장소를 검색하지 못했습니다.")
		return
	}
	response, err := a.weatherClient.Do(request)
	if err != nil {
		writeAPIError(w, 502, "place_search_failed", "장소 검색 서비스에 연결하지 못했습니다.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeAPIError(w, 502, "place_search_failed", "장소 검색 서비스가 응답하지 않습니다.")
		return
	}
	var source struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Admin2    string  `json:"admin2"`
			Admin3    string  `json:"admin3"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 128*1024)).Decode(&source) != nil {
		writeAPIError(w, 502, "place_search_failed", "장소 검색 결과를 읽지 못했습니다.")
		return
	}
	type result struct {
		Name        string  `json:"name"`
		RegionLabel string  `json:"regionLabel"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	results := make([]result, 0, len(source.Results))
	for _, item := range source.Results {
		parts := []string{}
		for _, part := range []string{item.Admin1, item.Admin2, item.Admin3, item.Name} {
			if part != "" && (len(parts) == 0 || parts[len(parts)-1] != part) {
				parts = append(parts, part)
			}
		}
		results = append(results, result{Name: item.Name, RegionLabel: strings.Join(parts, " "), Latitude: item.Latitude, Longitude: item.Longitude})
	}
	writeJSON(w, 200, map[string]any{"results": results})
}

func (a *App) weather(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedDevice(w, r); !ok {
		return
	}
	placeID := r.URL.Query().Get("placeId")
	var place database.Place
	var err error
	if placeID != "" {
		place, err = a.db.PlaceByID(r.Context(), placeID)
	} else {
		places, e := a.db.Places(r.Context())
		err = e
		for _, candidate := range places {
			if candidate.IsHome {
				place = candidate
				break
			}
		}
		if place.ID == "" && len(places) > 0 {
			place = places[0]
		}
	}
	if err != nil || place.ID == "" {
		writeJSON(w, 200, map[string]any{"status": "unconfigured"})
		return
	}
	cache, cacheErr := a.db.WeatherCache(r.Context(), place.ID)
	now := time.Now().UTC()
	if cacheErr == nil && now.Sub(cache.FetchedAt) < 30*time.Minute {
		a.writeWeather(w, place, cache, "fresh")
		return
	}
	payload, fetchErr := a.fetchWeather(r, place)
	if fetchErr == nil {
		if err = a.db.SaveWeatherCache(r.Context(), place.ID, payload, now); err == nil {
			cache = database.WeatherCache{Payload: payload, FetchedAt: now}
			a.writeWeather(w, place, cache, "fresh")
			return
		}
	}
	if cacheErr == nil {
		status := "stale"
		if now.Sub(cache.FetchedAt) >= 24*time.Hour {
			status = "unavailable"
		}
		a.writeWeather(w, place, cache, status)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "unavailable", "place": place, "message": "날씨 정보를 가져올 수 없음"})
}

func (a *App) fetchWeather(r *http.Request, place database.Place) ([]byte, error) {
	endpoint, err := url.Parse(a.weatherBaseURL)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(place.Latitude, 'f', 5, 64))
	query.Set("longitude", strconv.FormatFloat(place.Longitude, 'f', 5, 64))
	query.Set("timezone", "Asia/Seoul")
	query.Set("forecast_days", "7")
	query.Set("current", "temperature_2m,weather_code")
	query.Set("hourly", "temperature_2m,precipitation_probability,weather_code")
	query.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	var response *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		response, err = a.weatherClient.Do(request.Clone(r.Context()))
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			break
		}
		if response != nil {
			response.Body.Close()
		}
		if attempt < 2 {
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(time.Duration(attempt+1) * 150 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("weather status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if json.Unmarshal(payload, &value) != nil {
		return nil, errors.New("invalid weather response")
	}
	return payload, nil
}
func (a *App) writeWeather(w http.ResponseWriter, place database.Place, cache database.WeatherCache, status string) {
	var forecast any
	if status != "unavailable" {
		_ = json.Unmarshal(cache.Payload, &forecast)
	}
	writeJSON(w, 200, map[string]any{"status": status, "place": place, "fetchedAt": cache.FetchedAt, "forecast": forecast})
}

var allowedWidgets = map[string]bool{"board": true, "schedule": true, "tasks": true, "weather": true, "status": true}

func defaultWidgets(deviceType string) []string {
	switch deviceType {
	case "parent_mobile":
		return []string{"board", "schedule", "tasks", "weather"}
	case "shared_tablet":
		return []string{"schedule", "tasks", "board", "weather", "status"}
	case "tv":
		return []string{"status", "schedule", "weather", "tasks", "board"}
	default:
		return []string{"status", "schedule", "tasks", "board", "weather"}
	}
}
func defaultWeatherPreferences(deviceType string) (int, int) {
	switch deviceType {
	case "parent_mobile":
		return 2, 24
	case "shared_tablet":
		return 3, 24
	default:
		return 5, 24
	}
}
func validDeviceType(value string) bool {
	return value == "trusted_pc" || value == "parent_mobile" || value == "shared_tablet" || value == "tv"
}
func (a *App) dashboardPreferences(w http.ResponseWriter, r *http.Request) {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return
	}
	target := r.URL.Query().Get("deviceType")
	if target == "" {
		target = device.Type
	}
	if !validDeviceType(target) {
		writeAPIError(w, 400, "invalid_device_type", "기기 유형을 확인해 주세요.")
		return
	}
	widgets, err := a.db.DashboardPreferences(r.Context(), target)
	if errors.Is(err, sql.ErrNoRows) {
		widgets = defaultWidgets(target)
	} else if err != nil {
		writeAPIError(w, 500, "preferences_failed", "대시보드 설정을 불러오지 못했습니다.")
		return
	}
	days, hours, weatherErr := a.db.WeatherPreferences(r.Context(), target)
	if errors.Is(weatherErr, sql.ErrNoRows) {
		days, hours = defaultWeatherPreferences(target)
	} else if weatherErr != nil {
		writeAPIError(w, 500, "preferences_failed", "날씨 설정을 불러오지 못했습니다.")
		return
	}
	writeJSON(w, 200, map[string]any{"deviceType": target, "widgets": widgets, "dailyDays": days, "hourlyHours": hours})
}
func (a *App) saveDashboardPreferences(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		DeviceType  string   `json:"deviceType"`
		Widgets     []string `json:"widgets"`
		DailyDays   int      `json:"dailyDays"`
		HourlyHours int      `json:"hourlyHours"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request) != nil || !validDeviceType(request.DeviceType) {
		writeAPIError(w, 400, "invalid_preferences", "대시보드 설정을 확인해 주세요.")
		return
	}
	seen := map[string]bool{}
	for _, widget := range request.Widgets {
		if !allowedWidgets[widget] || seen[widget] {
			writeAPIError(w, 400, "invalid_preferences", "위젯 구성을 확인해 주세요.")
			return
		}
		seen[widget] = true
	}
	if err := a.db.SaveDashboardPreferences(r.Context(), request.DeviceType, request.Widgets, device.ID, time.Now().UTC()); err != nil {
		writeAPIError(w, 500, "preferences_failed", "대시보드 설정을 저장하지 못했습니다.")
		return
	}
	if request.DailyDays == 0 {
		request.DailyDays, request.HourlyHours = defaultWeatherPreferences(request.DeviceType)
	}
	if err := a.db.SaveWeatherPreferences(r.Context(), request.DeviceType, request.DailyDays, request.HourlyHours, device.ID, time.Now().UTC()); err != nil {
		writeAPIError(w, 400, "invalid_preferences", "날씨 표시 범위를 확인해 주세요.")
		return
	}
	writeJSON(w, 200, request)
}
