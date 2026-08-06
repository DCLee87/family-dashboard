package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func TestScheduleAPIRequiresAdminAndReturnsCreatedSchedule(t *testing.T) {
	application, deviceToken, adminToken, csrfToken := scheduleTestApp(t)
	defer application.Close()

	body := `{
		"title":"병원",
		"locationName":"가족 병원",
		"visibility":"family",
		"startsAt":"2026-07-30T01:00:00Z",
		"endsAt":"2026-07-30T02:00:00Z",
		"participants":["dad"]
	}`
	unauthorized := httptest.NewRequest(
		http.MethodPost, "http://dashboard.test/api/v1/schedules", bytes.NewBufferString(body),
	)
	unauthorized.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
	unauthorizedResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusForbidden {
		t.Fatalf("unauthorized create: got %d, want %d", unauthorizedResponse.Code, http.StatusForbidden)
	}

	request := httptest.NewRequest(
		http.MethodPost, "http://dashboard.test/api/v1/schedules", bytes.NewBufferString(body),
	)
	request.Header.Set("Origin", "http://dashboard.test")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
	request.AddCookie(&http.Cookie{Name: "family_dashboard_admin", Value: adminToken})
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	var created struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Participants []string `json:"participants"`
		Version      int64    `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Title != "병원" ||
		len(created.Participants) != 1 || created.Version != 1 {
		t.Fatalf("unexpected created schedule: %#v", created)
	}

	list := httptest.NewRequest(
		http.MethodGet,
		"http://dashboard.test/api/v1/schedule-occurrences?from=2026-07-30T00:00:00Z&to=2026-07-31T00:00:00Z&member=dad",
		nil,
	)
	list.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
	listResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list: got %d, want %d; body=%s", listResponse.Code, http.StatusOK, listResponse.Body.String())
	}
	var result struct {
		Occurrences []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"occurrences"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Occurrences) != 1 || result.Occurrences[0].ID != created.ID {
		t.Fatalf("unexpected schedule list: %#v", result)
	}
}

func TestScheduleAPIReportsOverlapAndVersionConflict(t *testing.T) {
	application, deviceToken, adminToken, csrfToken := scheduleTestApp(t)
	defer application.Close()
	handler := application.Handler()

	request := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://dashboard.test"+target, bytes.NewBufferString(body))
		r.Header.Set("Origin", "http://dashboard.test")
		r.Header.Set("X-CSRF-Token", csrfToken)
		r.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
		r.AddCookie(&http.Cookie{Name: "family_dashboard_admin", Value: adminToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	first := request(http.MethodPost, "/api/v1/schedules", `{
		"title":"학교","visibility":"family",
		"startsAt":"2026-07-30T00:00:00Z","endsAt":"2026-07-30T06:00:00Z",
		"participants":["daughter"]
	}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", first.Code, first.Body.String())
	}

	overlapBody := `{
		"title":"병원","visibility":"family",
		"startsAt":"2026-07-30T05:00:00Z","endsAt":"2026-07-30T07:00:00Z",
		"participants":["daughter"]
	}`
	warning := request(http.MethodPost, "/api/v1/schedules", overlapBody)
	if warning.Code != http.StatusConflict {
		t.Fatalf("overlap warning: got %d, want %d", warning.Code, http.StatusConflict)
	}
	var warningResult struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(warning.Body).Decode(&warningResult); err != nil {
		t.Fatal(err)
	}
	if warningResult.Code != "overlap_warning" {
		t.Fatalf("unexpected warning: %#v", warningResult)
	}

	confirmed := request(
		http.MethodPost, "/api/v1/schedules",
		bytesToConfirmedJSON(t, overlapBody),
	)
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirmed create: %d %s", confirmed.Code, confirmed.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(confirmed.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	stale := request(http.MethodPut, "/api/v1/schedules/"+created.ID, `{
		"title":"병원 수정","visibility":"family","version":99,
		"startsAt":"2026-07-30T07:00:00Z","endsAt":"2026-07-30T08:00:00Z",
		"participants":["daughter"]
	}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update: got %d, want %d; body=%s", stale.Code, http.StatusConflict, stale.Body.String())
	}
}

func TestScheduleAPICreatesWeeklyOccurrences(t *testing.T) {
	application, deviceToken, adminToken, csrfToken := scheduleTestApp(t)
	defer application.Close()
	handler := application.Handler()

	create := httptest.NewRequest(http.MethodPost, "http://dashboard.test/api/v1/schedules", bytes.NewBufferString(`{
		"title":"등교","visibility":"family",
		"startsAt":"2026-08-03T00:00:00Z","endsAt":"2026-08-03T01:00:00Z",
		"participants":["daughter"],
		"recurrence":{"kind":"weekly","weekdays":[1,3],"endsOn":"2026-08-10"}
	}`))
	create.Header.Set("Origin", "http://dashboard.test")
	create.Header.Set("X-CSRF-Token", csrfToken)
	create.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
	create.AddCookie(&http.Cookie{Name: "family_dashboard_admin", Value: adminToken})
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create weekly: got %d; body=%s", created.Code, created.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet,
		"http://dashboard.test/api/v1/schedule-occurrences?from=2026-08-02T15:00:00Z&to=2026-08-11T15:00:00Z&member=daughter", nil)
	list.AddCookie(&http.Cookie{Name: "family_dashboard_device", Value: deviceToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, list)
	if response.Code != http.StatusOK {
		t.Fatalf("list weekly: got %d; body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Occurrences []struct {
			Title         string `json:"title"`
			Recurring     bool   `json:"recurring"`
			OccurrenceKey string `json:"occurrenceKey"`
		} `json:"occurrences"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Occurrences) != 3 {
		t.Fatalf("got %d occurrences, want 3: %#v", len(result.Occurrences), result)
	}
	for _, occurrence := range result.Occurrences {
		if occurrence.Title != "등교" || !occurrence.Recurring || occurrence.OccurrenceKey == "" {
			t.Fatalf("unexpected recurring occurrence: %#v", occurrence)
		}
	}
}

func scheduleTestApp(t *testing.T) (*App, string, string, string) {
	t.Helper()
	application, err := New(Config{
		DataDir: t.TempDir(), StartedAt: time.Now(), RecordStart: false,
		SecureCookies: false,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	setupHash := security.TokenHash("setup")
	if _, err := application.db.EnsureInitialSetupCode(ctx, setupHash, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	deviceToken := "device-token"
	if err := application.db.CompleteInitialSetup(ctx, database.InitialSetup{
		CodeHash: setupHash, PINHash: "unused", DeviceID: "trusted-pc",
		DeviceName: "Home PC", AccessID: "access",
		AccessHash: security.TokenHash(deviceToken), AccessExpiresAt: now.Add(time.Hour),
		RefreshID: "refresh", RefreshHash: security.TokenHash("refresh-token"),
		RefreshFamilyID: "family", RefreshExpiresAt: now.Add(time.Hour),
		RecoveryID: "recovery", RecoveryHash: security.TokenHash("recovery-token"),
		Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	adminToken, csrfToken := "admin-token", "csrf-token"
	if err := application.db.CreateAdminSession(
		ctx, "trusted-pc", "admin-session", security.TokenHash(adminToken),
		security.TokenHash(csrfToken), now.Add(time.Hour), now.Add(time.Hour), now,
	); err != nil {
		t.Fatal(err)
	}
	return application, deviceToken, adminToken, csrfToken
}

func bytesToConfirmedJSON(t *testing.T, raw string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	value["confirmOverlap"] = true
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
