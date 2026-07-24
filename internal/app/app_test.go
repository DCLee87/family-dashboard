package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
