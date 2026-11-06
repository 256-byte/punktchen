package platform

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// newTestPool starts a fresh database and returns a pool to it.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := NewPool(context.Background(), pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func migrateReal(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if err := Migrate(context.Background(), pool, db.Migrations, db.MigrationsDir, discardLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func exists(t *testing.T, pool *pgxpool.Pool, query string, args ...any) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT EXISTS ("+query+")", args...).Scan(&ok); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return ok
}

func tableExists(t *testing.T, pool *pgxpool.Pool, schema, table string) bool {
	t.Helper()
	return exists(t, pool,
		"SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2", schema, table)
}

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()

	migrateReal(t, pool)
	migrateReal(t, pool) // second run: no change, no error

	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version, dirty FROM meta.schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read meta.schema_migrations: %v", err)
	}
	if version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
	if !tableExists(t, pool, "meta", "feed_imports") {
		t.Fatal("meta.feed_imports missing")
	}
	if !exists(t, pool, "SELECT 1 FROM pg_extension WHERE extname = 'postgis'") {
		t.Fatal("postgis extension not installed")
	}
	if tableExists(t, pool, "public", "schema_migrations") {
		t.Fatal("schema_migrations must live in schema meta, not public")
	}
}

func TestFeedImportsConstraints(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	migrateReal(t, pool)
	ctx := context.Background()

	const insert = `INSERT INTO meta.feed_imports (source, url, sha256, status, finished_at, error)
		VALUES ('vbb-gtfs', 'https://example.test/gtfs.zip', $1, $2, $3, $4)`
	sha := strings.Repeat("ab", 32)

	valid := []struct {
		name string
		args []any
	}{
		{"running", []any{nil, "running", nil, nil}},
		{"succeeded", []any{sha, "succeeded", "2026-11-06T03:31:00+01:00", nil}},
		{"skipped without download", []any{nil, "skipped", "2026-11-06T03:30:01+01:00", nil}},
		{"failed with error", []any{nil, "failed", "2026-11-06T03:30:05+01:00", "timeout"}},
	}
	for _, tt := range valid {
		if _, err := pool.Exec(ctx, insert, tt.args...); err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
		}
	}

	invalid := []struct {
		name string
		args []any
	}{
		{"unknown status", []any{nil, "done", "2026-11-06T03:31:00+01:00", nil}},
		{"running but finished", []any{nil, "running", "2026-11-06T03:31:00+01:00", nil}},
		{"succeeded without finished_at", []any{sha, "succeeded", nil, nil}},
		{"error on success", []any{sha, "succeeded", "2026-11-06T03:31:00+01:00", "oops"}},
		{"bad sha256", []any{"ABC", "succeeded", "2026-11-06T03:31:00+01:00", nil}},
	}
	for _, tt := range invalid {
		if _, err := pool.Exec(ctx, insert, tt.args...); err == nil {
			t.Errorf("%s: want constraint violation, got nil", tt.name)
		}
	}
}

func TestMigrateDownThenUp(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	migrateReal(t, pool)

	m, err := newMigrator(ctx, pool, db.Migrations, db.MigrationsDir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Down(); err != nil {
		t.Fatalf("Down: %v", err)
	}
	_, _ = m.Close()
	if tableExists(t, pool, "meta", "feed_imports") {
		t.Fatal("meta.feed_imports still exists after down")
	}

	migrateReal(t, pool)
	if !tableExists(t, pool, "meta", "feed_imports") {
		t.Fatal("meta.feed_imports missing after up again")
	}
}

// A failing migration must leave nothing behind (implicit transaction) and
// mark the database dirty, so the next start refuses to continue.
func TestMigrateFailureIsAtomicAndBlocksNextRun(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()

	broken := fstest.MapFS{
		"m/0001_broken.up.sql":   {Data: []byte("CREATE TABLE meta.half_done (x int);\nSELECT * FROM no_such_table;")},
		"m/0001_broken.down.sql": {Data: []byte("DROP TABLE IF EXISTS meta.half_done;")},
	}
	if err := Migrate(ctx, pool, broken, "m", discardLogger()); err == nil {
		t.Fatal("want error from broken migration")
	}
	if tableExists(t, pool, "meta", "half_done") {
		t.Fatal("first statement of the failed migration was not rolled back")
	}

	err := Migrate(ctx, pool, broken, "m", discardLogger())
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("want dirty error on next run, got %v", err)
	}
}
