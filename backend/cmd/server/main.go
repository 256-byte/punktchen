// Command server is the LiveStop backend: one binary that will run the GTFS
// importer, the realtime poller and the HTTP API (ADR-0002).
//
// Startup order: config → logger → database → migrations → listeners → serve.
// Each step fails fast; SIGINT/SIGTERM triggers a graceful shutdown.
//
// Usage:
//
//	server              run the backend
//	server healthcheck  exit 0 if the local API answers /v1/health (Docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/platform"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

// shutdownTimeout stays below Docker's default 10 s stop grace period.
const shutdownTimeout = 8 * time.Second

const healthPath = "/v1/health"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		// No configured logger yet: log the error as JSON with defaults.
		platform.NewLogger(os.Stderr, slog.LevelInfo).Error("startup failed", "error", err.Error())
		return 1
	}

	log := platform.NewLogger(os.Stdout, cfg.LogLevel)
	log.Info("starting livestop", "version", version, "go", runtime.Version(), "config", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, log, cfg); err != nil {
		log.Error("stopped with error", "error", err.Error())
		return 1
	}
	log.Info("stopped")
	return 0
}

func serve(ctx context.Context, log *slog.Logger, cfg config.Config) error {
	pool, err := platform.NewPool(ctx, cfg.DatabaseURL.Value())
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("database connected")

	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, log.With("component", "migrate")); err != nil {
		return err
	}

	metrics := platform.NewMetricsRegistry()

	api, err := platform.Listen("api", cfg.HTTPAddr, apiHandler())
	if err != nil {
		return err
	}
	metricsSrv, err := platform.Listen("metrics", cfg.MetricsAddr, platform.MetricsHandler(metrics))
	if err != nil {
		return errors.Join(err, api.Close())
	}

	return platform.Serve(ctx, log, shutdownTimeout, api, metricsSrv)
}

// apiHandler is temporary: task 0.5 replaces it with the handlers generated
// from api/openapi.yaml (internal/api).
func apiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+healthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	return mux
}

// healthcheck calls the local health endpoint and fails on any non-200 answer.
// The distroless runtime image has no shell, curl or wget, so the binary
// checks itself. It reads only LIVESTOP_HTTP_ADDR, not the full config.
func healthcheck() error {
	addr := os.Getenv(config.EnvHTTPAddr)
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse %s %q: %w", config.EnvHTTPAddr, addr, err)
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + healthPath)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
