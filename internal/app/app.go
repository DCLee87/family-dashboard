package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/webui"
)

type Config struct {
	Address     string
	DataDir     string
	Version     string
	StartedAt   time.Time
	RecordStart bool
}

type App struct {
	config Config
	db     *database.Database
	logger *slog.Logger
}

func New(config Config, logger *slog.Logger) (*App, error) {
	db, err := database.Open(config.DataDir, config.RecordStart)
	if err != nil {
		return nil, err
	}
	return &App{config: config, db: db, logger: logger}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/runtime", a.runtime)

	dist, err := fs.Sub(webui.Files, "dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", spaHandler(dist))
	return requestLogger(a.logger, mux)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error", "database": "error",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "database": "ok",
	})
}

func (a *App) runtime(w http.ResponseWriter, r *http.Request) {
	count, err := a.db.StartCount(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       a.config.Version,
		"uptimeSeconds": int64(time.Since(a.config.StartedAt).Seconds()),
		"startCount":    count,
	})
}

func (a *App) CheckDatabase(ctx context.Context) error {
	return a.db.IntegrityCheck(ctx)
}

func (a *App) Close() error {
	return a.db.Close()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
