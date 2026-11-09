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
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/api"
	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
	"github.com/256-byte/punktchen/backend/internal/transit/static"
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

	// Static GTFS import (ARCHITECTURE §3.1): one scheduler per process; the
	// advisory lock in Importer.Run guards against other processes.
	importLog := log.With("component", "import")
	il := static.NewImportLog(pool)
	dl, err := static.NewDownloader(static.DownloaderConfig{URL: cfg.GTFSURL, UserAgent: userAgent()}, il, importLog)
	if err != nil {
		return err
	}
	importer := static.NewImporter(pool, dl, static.NewLoader(pool, cfg.ScopeBBox, importLog), il, static.DefaultSanityLimits, importLog)

	// Departures snapshot (ARCHITECTURE §3.2, §3.4): moves the window every
	// minute and loads the whole window again after an import.
	builder := departures.NewBuilder(departures.BuilderConfig{}, departures.NewLoader(pool),
		departures.NewMetrics(metrics), log.With("component", "departures"))
	runImport := func(ctx context.Context) (*static.Outcome, error) {
		out, err := importer.Run(ctx)
		if err == nil && out != nil && out.Changed {
			builder.Reload()
		}
		return out, err
	}

	// Map layers (ADR-0008): only transit for now.
	transitLayer := transit.NewProvider(pool, builder)
	layers, err := layer.NewRegistry(transitLayer)
	if err != nil {
		return err
	}

	status := static.NewStatus(pool, static.SourceGTFS)
	scheduler := static.NewScheduler(static.SchedulerConfig{
		Hour: cfg.GTFSImportTime.Hour, Minute: cfg.GTFSImportTime.Minute, Loc: platform.Berlin,
	}, runImport, status.Current, static.NewMetrics(metrics), importLog)

	apiHandler := api.NewHandler(api.NewServer(api.Config{
		Version: version, DB: pool, Schedule: status, Layers: layers, Transit: transitLayer,
		Imports: scheduler, Dev: cfg.Dev,
		Log: log.With("component", "api"),
	}))
	apiSrv, err := platform.Listen("api", cfg.HTTPAddr, apiHandler)
	if err != nil {
		return err
	}
	metricsSrv, err := platform.Listen("metrics", cfg.MetricsAddr, platform.MetricsHandler(metrics))
	if err != nil {
		return errors.Join(err, apiSrv.Close())
	}

	// Background loops stop with ctx; wait for them before the pool closes,
	// so a running import can record its outcome.
	defer runBackground(ctx, scheduler.Run)()
	defer runBackground(ctx, builder.Run)()

	return platform.Serve(ctx, log, shutdownTimeout, apiSrv, metricsSrv)
}

// runBackground runs fn in a goroutine. The returned stop cancels fn's
// context and waits for fn to return.
func runBackground(ctx context.Context, fn func(context.Context)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// userAgent names the project to upstream servers, as VBB asks
// (DATA_MODEL §7 #20). No personal contact data.
func userAgent() string {
	return "livestop/" + version + " (+https://github.com/256-byte/punktchen)"
}

// healthcheck calls the local health endpoint and fails on any non-200 answer.
// It is a liveness check: /v1/health answers 200 even when the data is
// "down", so a container is not restarted during e.g. a long first import.
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
