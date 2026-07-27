package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	"github.com/DCLee87/family-dashboard/internal/webui"
)

type Config struct {
	Address          string
	DataDir          string
	Version          string
	StartedAt        time.Time
	RecordStart      bool
	LocalNetworks    []netip.Prefix
	SecureCookies    bool
	InitialSetupCode string
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
	application := &App{config: config, db: db, logger: logger}
	if config.RecordStart {
		if err := application.ensureInitialSetupCode(context.Background()); err != nil {
			db.Close()
			return nil, err
		}
	}
	return application, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/runtime", a.runtime)
	mux.HandleFunc("GET /api/setup/status", a.setupStatus)
	mux.HandleFunc("POST /api/setup/complete", a.completeSetup)
	mux.HandleFunc("GET /api/auth/device", a.currentDevice)
	mux.HandleFunc("POST /api/admin/unlock", a.unlockAdmin)
	mux.HandleFunc("POST /api/admin/lock", a.lockAdmin)
	mux.HandleFunc("POST /api/enrollments/submit", a.submitEnrollment)
	mux.HandleFunc("POST /api/enrollments/claim", a.claimEnrollment)
	mux.HandleFunc("POST /api/admin/enrollments", a.createEnrollment)
	mux.HandleFunc("GET /api/admin/enrollments", a.listEnrollments)
	mux.HandleFunc("POST /api/admin/enrollments/{id}/approve", a.approveEnrollment)
	mux.HandleFunc("POST /api/admin/enrollments/{id}/reject", a.rejectEnrollment)
	mux.HandleFunc("GET /api/admin/devices", a.listDevices)
	mux.HandleFunc("POST /api/admin/devices/{id}/revoke", a.revokeDevice)

	dist, err := fs.Sub(webui.Files, "dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", spaHandler(dist))
	return requestLogger(a.logger, mux)
}

func (a *App) ensureInitialSetupCode(ctx context.Context) error {
	required, err := a.db.SetupRequired(ctx)
	if err != nil || !required {
		return err
	}
	code := a.config.InitialSetupCode
	if code == "" {
		code = rand.Text()
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	created, err := a.db.EnsureInitialSetupCode(
		ctx,
		security.TokenHash(code),
		expiresAt,
	)
	if err != nil {
		return err
	}
	if created {
		a.logger.Warn(
			"initial setup required",
			"initial_setup_code", code,
			"expires_at", expiresAt,
		)
	}
	return nil
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
