package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
	"github.com/DCLee87/family-dashboard/internal/webui"
	webpush "github.com/SherClockHolmes/webpush-go"
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
	VAPIDPublicKey   string
	VAPIDPrivateKey  string
}

type App struct {
	config     Config
	db         *database.Database
	logger     *slog.Logger
	pushClient webpush.HTTPClient
	workerStop context.CancelFunc
	workerWG   sync.WaitGroup
}

func New(config Config, logger *slog.Logger) (*App, error) {
	if _, err := time.LoadLocation("Asia/Seoul"); err != nil {
		return nil, fmt.Errorf("load family timezone: %w", err)
	}
	if (config.VAPIDPublicKey == "") != (config.VAPIDPrivateKey == "") {
		return nil, fmt.Errorf("configure VAPID keys: %w", security.ErrInvalidVAPIDKeys)
	}
	if config.VAPIDPublicKey != "" {
		if err := security.ValidateVAPIDKeys(config.VAPIDPrivateKey, config.VAPIDPublicKey); err != nil {
			return nil, fmt.Errorf("configure VAPID keys: %w", err)
		}
	}
	db, err := database.Open(config.DataDir, config.RecordStart)
	if err != nil {
		return nil, err
	}
	if purged, err := db.PurgeExpiredTrashedSchedules(context.Background(), time.Now().UTC()); err != nil {
		db.Close()
		return nil, fmt.Errorf("purge expired schedule trash: %w", err)
	} else if purged > 0 {
		logger.Info("expired trashed schedules purged", "count", purged)
	}
	application := &App{
		config: config, db: db, logger: logger, pushClient: http.DefaultClient,
	}
	if config.RecordStart {
		if err := application.ensureInitialSetupCode(context.Background()); err != nil {
			db.Close()
			return nil, err
		}
		application.startBackgroundWorkers()
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
	mux.HandleFunc("POST /api/auth/refresh", a.refreshDevice)
	mux.HandleFunc("GET /api/push/config", a.pushConfig)
	mux.HandleFunc("PUT /api/push/subscription", a.replacePushSubscription)
	mux.HandleFunc("DELETE /api/push/subscription", a.revokePushSubscription)
	mux.HandleFunc("POST /api/push/test", a.sendTestPush)
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
	mux.HandleFunc("GET /api/v1/family-members", a.listFamilyMembers)
	mux.HandleFunc("POST /api/v1/schedules", a.createSchedule)
	mux.HandleFunc("PUT /api/v1/schedules/{id}", a.updateSchedule)
	mux.HandleFunc("DELETE /api/v1/schedules/{id}", a.deleteSchedule)
	mux.HandleFunc("GET /api/v1/schedules/{id}", a.getSchedule)
	mux.HandleFunc("GET /api/v1/schedules/{id}/notification", a.getScheduleNotificationSetting)
	mux.HandleFunc("PUT /api/v1/schedules/{id}/notification", a.updateScheduleNotificationSetting)
	mux.HandleFunc("PUT /api/v1/schedules/{id}/occurrences/{key}", a.updateScheduleOccurrence)
	mux.HandleFunc("POST /api/v1/schedules/{id}/occurrences/{key}/cancel", a.cancelScheduleOccurrence)
	mux.HandleFunc("GET /api/v1/schedule-occurrences", a.listScheduleOccurrences)
	mux.HandleFunc("GET /api/v1/family-status", a.familyStatus)
	mux.HandleFunc("GET /api/admin/schedule-trash", a.listScheduleTrash)
	mux.HandleFunc("POST /api/admin/schedule-trash/{id}/restore", a.restoreSchedule)
	mux.HandleFunc("DELETE /api/admin/schedule-trash/{id}", a.permanentlyDeleteSchedule)

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
	if a.workerStop != nil {
		a.workerStop()
		a.workerWG.Wait()
	}
	return a.db.Close()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
