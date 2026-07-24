package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DCLee87/family-dashboard/internal/app"
)

var version = "dev"

func main() {
	var checkDB bool
	flag.BoolVar(&checkDB, "check-db", false, "check database integrity and exit")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := app.Config{
		Address:     env("FAMILY_DASHBOARD_ADDRESS", ":8080"),
		DataDir:     env("FAMILY_DASHBOARD_DATA_DIR", "./runtime/data"),
		Version:     version,
		StartedAt:   time.Now().UTC(),
		RecordStart: !checkDB,
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("application initialization failed", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	if checkDB {
		if err := application.CheckDatabase(context.Background()); err != nil {
			logger.Error("database integrity check failed", "error", err)
			os.Exit(1)
		}
		logger.Info("database integrity check passed")
		return
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("server started", "address", cfg.Address, "version", cfg.Version)
		errs <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
