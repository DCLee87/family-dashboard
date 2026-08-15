package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

func TestDeviceCanManageEnrollments(t *testing.T) {
	tests := []struct {
		name   string
		device database.Device
		want   bool
	}{
		{name: "trusted PC", device: database.Device{Type: "trusted_pc"}, want: true},
		{name: "dad parent mobile", device: database.Device{Type: "parent_mobile", Owner: sql.NullString{String: "dad", Valid: true}}, want: true},
		{name: "mom parent mobile", device: database.Device{Type: "parent_mobile", Owner: sql.NullString{String: "mom", Valid: true}}, want: false},
		{name: "ownerless parent mobile", device: database.Device{Type: "parent_mobile"}, want: false},
		{name: "shared tablet", device: database.Device{Type: "shared_tablet"}, want: false},
		{name: "TV", device: database.Device{Type: "tv"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deviceCanManageEnrollments(test.device); got != test.want {
				t.Fatalf("deviceCanManageEnrollments() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHealthAndRuntime(t *testing.T) {
	application, err := New(Config{
		DataDir:     t.TempDir(),
		Version:     "test",
		StartedAt:   time.Now().Add(-time.Second),
		RecordStart: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	server := httptest.NewServer(application.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status: got %d, want %d", response.StatusCode, http.StatusOK)
	}

	var health map[string]string
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" || health["database"] != "ok" {
		t.Fatalf("unexpected health response: %#v", health)
	}
}

func TestInitialSetup(t *testing.T) {
	application, err := New(Config{
		DataDir:          t.TempDir(),
		Version:          "test",
		StartedAt:        time.Now(),
		RecordStart:      true,
		LocalNetworks:    []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		SecureCookies:    true,
		InitialSetupCode: "test-initial-setup-code",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	server := httptest.NewServer(application.Handler())
	defer server.Close()

	invalidBody := bytes.NewBufferString(
		`{"initialSetupCode":"wrong-code","pin":"4826","deviceName":"Home Mac"}`,
	)
	invalid, err := http.Post(server.URL+"/api/setup/complete", "application/json", invalidBody)
	if err != nil {
		t.Fatal(err)
	}
	invalid.Body.Close()
	if invalid.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid code status: got %d, want %d", invalid.StatusCode, http.StatusUnauthorized)
	}

	body := bytes.NewBufferString(
		`{"initialSetupCode":"test-initial-setup-code","pin":"4826","deviceName":"Home Mac"}`,
	)
	response, err := http.Post(server.URL+"/api/setup/complete", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status: got %d, want %d", response.StatusCode, http.StatusCreated)
	}
	var result struct {
		Status       string `json:"status"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "configured" || result.RecoveryCode == "" {
		t.Fatalf("unexpected setup response: %#v", result)
	}
	if len(response.Cookies()) != 2 {
		t.Fatalf("cookies: got %d, want 2", len(response.Cookies()))
	}
	for _, cookie := range response.Cookies() {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("insecure cookie attributes: %#v", cookie)
		}
	}

	deviceRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/device", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range response.Cookies() {
		deviceRequest.AddCookie(cookie)
	}
	deviceResponse, err := http.DefaultClient.Do(deviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer deviceResponse.Body.Close()
	if deviceResponse.StatusCode != http.StatusOK {
		t.Fatalf("device status: got %d, want %d", deviceResponse.StatusCode, http.StatusOK)
	}
	var deviceResult struct {
		Status      string `json:"status"`
		Permissions struct {
			View  bool `json:"view"`
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(deviceResponse.Body).Decode(&deviceResult); err != nil {
		t.Fatal(err)
	}
	if deviceResult.Status != "authenticated" ||
		!deviceResult.Permissions.View ||
		deviceResult.Permissions.Admin {
		t.Fatalf("unexpected device response: %#v", deviceResult)
	}

	statusResponse, err := http.Get(server.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResponse.Body.Close()
	var status map[string]bool
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["setupRequired"] {
		t.Fatal("setup remained required after completion")
	}

	replayBody := bytes.NewBufferString(
		`{"initialSetupCode":"test-initial-setup-code","pin":"4826","deviceName":"Other Mac"}`,
	)
	replay, err := http.Post(server.URL+"/api/setup/complete", "application/json", replayBody)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("setup replay status: got %d, want %d", replay.StatusCode, http.StatusUnauthorized)
	}
}

func TestDeviceEndpointRequiresCredential(t *testing.T) {
	application, err := New(Config{
		DataDir:     t.TempDir(),
		StartedAt:   time.Now(),
		RecordStart: false,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	request := httptest.NewRequest(http.MethodGet, "/api/auth/device", nil)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("device status: got %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestInitialSetupIsDeniedWithoutLocalNetwork(t *testing.T) {
	application, err := New(Config{
		DataDir:          t.TempDir(),
		StartedAt:        time.Now(),
		RecordStart:      true,
		InitialSetupCode: "test-initial-setup-code",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/setup/complete",
		bytes.NewBufferString(`{}`),
	)
	request.RemoteAddr = "192.0.2.10:12345"
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("setup status: got %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestTailscaleServeRequestIsNotLocal(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://dashboard.example/", nil)
	request.RemoteAddr = "172.20.0.1:12345"
	request.Header.Set("Tailscale-User-Login", "family-member@example.test")
	networks := []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16")}
	if requestInNetworks(request, networks) {
		t.Fatal("Tailscale Serve request was classified as local")
	}
}

func TestParentEnrollmentIsAllowedThroughTailscaleServe(t *testing.T) {
	application, err := New(Config{
		DataDir:       t.TempDir(),
		StartedAt:     time.Now(),
		RecordStart:   false,
		SecureCookies: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	now := time.Now().UTC()
	code := "12345678"
	if err := application.db.CreateEnrollment(
		context.Background(), "tailscale-parent", security.TokenHash(code),
		"parent_mobile", now.Add(10*time.Minute), now,
	); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"https://dashboard.example/api/enrollments/submit",
		bytes.NewBufferString(`{"code":"12345678","deviceName":"Dad Phone","owner":"dad","type":"parent_mobile"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://dashboard.example")
	request.Header.Set("Tailscale-User-Login", "family-member@example.test")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("parent enrollment status: got %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
}

func TestSharedTabletEnrollmentIsDeniedThroughTailscaleServe(t *testing.T) {
	application, err := New(Config{
		DataDir:       t.TempDir(),
		StartedAt:     time.Now(),
		RecordStart:   false,
		SecureCookies: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	now := time.Now().UTC()
	code := "87654321"
	if err := application.db.CreateEnrollment(
		context.Background(), "tailscale-tablet", security.TokenHash(code),
		"shared_tablet", now.Add(10*time.Minute), now,
	); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"https://dashboard.example/api/enrollments/submit",
		bytes.NewBufferString(`{"code":"87654321","deviceName":"Tablet","owner":"","type":"shared_tablet"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://dashboard.example")
	request.Header.Set("Tailscale-User-Login", "family-member@example.test")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("tablet enrollment status: got %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
	var result map[string]string
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "local_network_required" {
		t.Fatalf("tablet enrollment response: %#v", result)
	}
}

func TestAdminUnlockLockAndPINFailureLimit(t *testing.T) {
	application, err := New(Config{
		DataDir:          t.TempDir(),
		StartedAt:        time.Now(),
		RecordStart:      true,
		LocalNetworks:    []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		SecureCookies:    false,
		InitialSetupCode: "test-initial-setup-code",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	server := httptest.NewServer(application.Handler())
	defer server.Close()

	setupRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/setup/complete",
		bytes.NewBufferString(
			`{"initialSetupCode":"test-initial-setup-code","pin":"4826","deviceName":"Home Mac"}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	setupRequest.Header.Set("Content-Type", "application/json")
	setupResponse, err := http.DefaultClient.Do(setupRequest)
	if err != nil {
		t.Fatal(err)
	}
	setupResponse.Body.Close()
	if setupResponse.StatusCode != http.StatusCreated {
		t.Fatalf("setup status: got %d, want %d", setupResponse.StatusCode, http.StatusCreated)
	}
	deviceCookies := setupResponse.Cookies()

	unlock := func(pin string) *http.Response {
		t.Helper()
		request, err := http.NewRequest(
			http.MethodPost,
			server.URL+"/api/admin/unlock",
			bytes.NewBufferString(`{"pin":"`+pin+`"}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", server.URL)
		for _, cookie := range deviceCookies {
			request.AddCookie(cookie)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	unlockResponse := unlock("4826")
	unlockResponse.Body.Close()
	if unlockResponse.StatusCode != http.StatusOK {
		t.Fatalf("unlock status: got %d, want %d", unlockResponse.StatusCode, http.StatusOK)
	}
	adminCookies := unlockResponse.Cookies()
	if len(adminCookies) != 2 {
		t.Fatalf("admin cookies: got %d, want 2", len(adminCookies))
	}

	deviceRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/device", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range append(deviceCookies, adminCookies...) {
		deviceRequest.AddCookie(cookie)
	}
	deviceResponse, err := http.DefaultClient.Do(deviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	var deviceResult struct {
		Permissions struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(deviceResponse.Body).Decode(&deviceResult); err != nil {
		t.Fatal(err)
	}
	deviceResponse.Body.Close()
	if !deviceResult.Permissions.Admin {
		t.Fatal("admin permission was not enabled after unlock")
	}

	var csrf string
	for _, cookie := range adminCookies {
		if cookie.Name == "family_dashboard_csrf" {
			csrf = cookie.Value
		}
	}
	lockRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/lock", nil)
	if err != nil {
		t.Fatal(err)
	}
	lockRequest.Header.Set("X-CSRF-Token", csrf)
	for _, cookie := range append(deviceCookies, adminCookies...) {
		lockRequest.AddCookie(cookie)
	}
	lockResponse, err := http.DefaultClient.Do(lockRequest)
	if err != nil {
		t.Fatal(err)
	}
	lockResponse.Body.Close()
	if lockResponse.StatusCode != http.StatusOK {
		t.Fatalf("lock status: got %d, want %d", lockResponse.StatusCode, http.StatusOK)
	}

	for attempt := 1; attempt <= 5; attempt++ {
		response := unlock("0000")
		response.Body.Close()
		want := http.StatusUnauthorized
		if attempt == 5 {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("failed attempt %d: got %d, want %d", attempt, response.StatusCode, want)
		}
	}
	blocked := unlock("4826")
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct PIN during block: got %d, want %d", blocked.StatusCode, http.StatusTooManyRequests)
	}
}

func TestAdminUnlockRejectsCrossOrigin(t *testing.T) {
	application, err := New(Config{
		DataDir:     t.TempDir(),
		StartedAt:   time.Now(),
		RecordStart: false,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	request := httptest.NewRequest(
		http.MethodPost,
		"https://dashboard.example/api/admin/unlock",
		bytes.NewBufferString(`{"pin":"4826"}`),
	)
	request.Host = "dashboard.example"
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin unlock: got %d, want %d", response.Code, http.StatusForbidden)
	}
}
