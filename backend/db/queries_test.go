package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// TestLastSuccessfulFeedImport runs the generated sqlc code against a
// migrated database: it checks the query logic and the Go type mapping
// (nullable text → *string, timestamptz → time.Time, jsonb → RawMessage).
func TestLastSuccessfulFeedImport(t *testing.T) {
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	q := queries.New(pool)

	if _, err := q.LastSuccessfulFeedImport(ctx, "vbb-gtfs"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("empty table: err = %v, want pgx.ErrNoRows", err)
	}

	sha := func(c string) string { return strings.Repeat(c, 64) }
	// Newer attempts that must not win: failed, still running, other feed.
	_, err = pool.Exec(ctx, `
		INSERT INTO meta.feed_imports
			(source, url, etag, sha256, started_at, finished_at, status, row_counts, error)
		VALUES
			('vbb-gtfs', 'https://a', NULL,   $1,   '2026-11-01 03:30+01', '2026-11-01 03:40+01', 'succeeded', NULL, NULL),
			('vbb-gtfs', 'https://a', '"v2"', $2,   '2026-11-02 03:30+01', '2026-11-02 03:41+01', 'succeeded', '{"stops": 41000}', NULL),
			('vbb-gtfs', 'https://a', NULL,   NULL, '2026-11-03 03:30+01', '2026-11-03 03:31+01', 'failed',    NULL, 'boom'),
			('vbb-gtfs', 'https://a', NULL,   NULL, '2026-11-04 03:30+01', NULL,                  'running',   NULL, NULL),
			('vbb-rt',   'https://b', NULL,   NULL, '2026-11-05 03:30+01', '2026-11-05 03:30+01', 'succeeded', NULL, NULL)`,
		sha("a"), sha("b"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := q.LastSuccessfulFeedImport(ctx, "vbb-gtfs")
	if err != nil {
		t.Fatal(err)
	}

	berlin := platform.Berlin
	wantStart := time.Date(2026, 11, 2, 3, 30, 0, 0, berlin)
	wantFinish := time.Date(2026, 11, 2, 3, 41, 0, 0, berlin)
	switch {
	case got.Source != "vbb-gtfs" || got.URL != "https://a" || got.Status != "succeeded":
		t.Errorf("got %s %s %s", got.Source, got.URL, got.Status)
	case got.Etag == nil || *got.Etag != `"v2"`:
		t.Errorf("Etag = %v, want \"v2\"", got.Etag)
	case got.LastModified != nil || got.Error != nil:
		t.Errorf("LastModified = %v, Error = %v, want nil", got.LastModified, got.Error)
	case got.SHA256 == nil || *got.SHA256 != sha("b"):
		t.Errorf("SHA256 = %v, want the newer import's hash", got.SHA256)
	case !got.StartedAt.Equal(wantStart):
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, wantStart)
	case got.FinishedAt == nil || !got.FinishedAt.Equal(wantFinish):
		t.Errorf("FinishedAt = %v, want %v", got.FinishedAt, wantFinish)
	}

	var counts map[string]int
	if err := json.Unmarshal(got.RowCounts, &counts); err != nil || counts["stops"] != 41000 {
		t.Errorf("RowCounts = %s (err %v), want {\"stops\": 41000}", got.RowCounts, err)
	}
}
