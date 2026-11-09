// Package statictest gives other packages' integration tests a database with
// a live GTFS schedule: a fresh PostGIS container, migrated, with a feed
// loaded by the real importer steps and made live. Needs Docker; skipped with
// `go test -short`.
package statictest

import (
	"archive/zip"
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/geo"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
	"github.com/256-byte/punktchen/backend/internal/transit/static"
)

// NewDB starts a migrated database without a schedule.
func NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return pool
}

// LoadZip loads a GTFS zip (scope: the default Berlin bbox) and makes it the
// live schedule, replacing the current one.
func LoadZip(t testing.TB, pool *pgxpool.Pool, path string) {
	t.Helper()
	ctx := context.Background()
	scope, err := geo.ParseBBox(config.DefaultScopeBBox)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := static.NewLoader(pool, scope, slog.New(slog.DiscardHandler)).Load(ctx, path); err != nil {
		t.Fatal(err)
	}
	// What the importer's swap does, without its sanity checks and logging.
	for _, s := range []string{
		`DROP SCHEMA IF EXISTS ` + static.LiveSchema + ` CASCADE`,
		`ALTER SCHEMA ` + static.StagingSchema + ` RENAME TO ` + static.LiveSchema,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// LoadFiles writes files (name → CSV content) to a zip and loads it like
// LoadZip.
func LoadFiles(t testing.TB, pool *pgxpool.Pool, files map[string]string) {
	t.Helper()
	LoadZip(t, pool, WriteZip(t, files))
}

// WriteZip writes files to a zip in a temp dir, in sorted order.
func WriteZip(t testing.TB, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gtfs.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
