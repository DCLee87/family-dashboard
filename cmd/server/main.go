package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/DCLee87/family-dashboard/internal/app"
	"github.com/DCLee87/family-dashboard/internal/security"
)

var version = "dev"

func main() {
	var checkDB bool
	var generateVAPIDKeys bool
	flag.BoolVar(&checkDB, "check-db", false, "check database integrity and exit")
	flag.BoolVar(&generateVAPIDKeys, "generate-vapid-keys", false, "generate VAPID environment values and exit")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if generateVAPIDKeys {
		privateKey, publicKey, err := security.GenerateVAPIDKeys()
		if err != nil {
			logger.Error("VAPID key generation failed", "error", err)
			os.Exit(1)
		}
		_, _ = fmt.Fprintf(
			os.Stdout,
			"FAMILY_DASHBOARD_VAPID_PUBLIC_KEY=%s\nFAMILY_DASHBOARD_VAPID_PRIVATE_KEY=%s\n",
			publicKey,
			privateKey,
		)
		return
	}
	localNetworks, err := parsePrefixes(os.Getenv("FAMILY_DASHBOARD_LOCAL_NETWORKS"))
	if err != nil {
		logger.Error("invalid local network configuration", "error", err)
		os.Exit(1)
	}
	secureCookies, err := envBool("FAMILY_DASHBOARD_SECURE_COOKIES", true)
	if err != nil {
		logger.Error("invalid secure cookie configuration", "error", err)
		os.Exit(1)
	}
	cfg := app.Config{
		Address:         env("FAMILY_DASHBOARD_ADDRESS", ":8080"),
		DataDir:         env("FAMILY_DASHBOARD_DATA_DIR", "./runtime/data"),
		Version:         version,
		StartedAt:       time.Now().UTC(),
		RecordStart:     !checkDB,
		LocalNetworks:   localNetworks,
		SecureCookies:   secureCookies,
		VAPIDPublicKey:  os.Getenv("FAMILY_DASHBOARD_VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("FAMILY_DASHBOARD_VAPID_PRIVATE_KEY"),
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

func envBool(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}
	return parsed, nil
}

func parsePrefixes(value string) ([]netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	values := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, item := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}
