package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

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
