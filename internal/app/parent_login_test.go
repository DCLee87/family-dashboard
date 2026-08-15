package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DCLee87/family-dashboard/internal/security"
)

func TestParentLoginRequiresSameOriginAndIssuesStrictCookies(t *testing.T) {
	application, err := New(Config{DataDir: t.TempDir(), StartedAt: time.Now(), RecordStart: false}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	hash, err := security.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := application.db.SetParentPassword(t.Context(), "dad", hash, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"owner":"dad","password":"correct horse battery staple","deviceName":"Dad phone"}`)
	cross := httptest.NewRequest(http.MethodPost, "http://dashboard.test/api/auth/login", bytes.NewReader(body))
	cross.Header.Set("Origin", "http://evil.test")
	crossResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(crossResponse, cross)
	if crossResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status: %d", crossResponse.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "http://dashboard.test/api/auth/login", bytes.NewReader(body))
	request.Header.Set("Origin", "http://dashboard.test")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		var result map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		t.Fatalf("login status: %d body=%v", response.Code, result)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookie count: %d", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("insecure login cookie: %#v", cookie)
		}
	}
}
