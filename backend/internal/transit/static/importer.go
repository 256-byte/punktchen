package static

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

// Extra keys in meta.feed_imports.row_counts next to the table names.
const (
	keyInScopePoles    = "in_scope_poles"
	keyInScopeStations = "in_scope_stations"
)

// importLockKey is the pg_advisory_lock key that serializes imports across
// all backend processes ("GTFS" in ASCII).
const importLockKey int64 = 0x47544653

// ErrImportRunning is returned when another import holds the lock.
var ErrImportRunning = errors.New("another gtfs import is running")

// Importer runs one complete import: download → load into staging → sanity
// checks → swap → record the outcome (ARCHITECTURE §3.1).
type Importer struct {
	pool   *pgxpool.Pool
	dl     *Downloader
	loader *Loader
	il     *ImportLog
	limits SanityLimits
	log    *slog.Logger
	now    func() time.Time
}

// NewImporter wires the steps. il must write to the same database as pool.
func NewImporter(pool *pgxpool.Pool, dl *Downloader, loader *Loader, il *ImportLog, limits SanityLimits, log *slog.Logger) *Importer {
	return &Importer{pool: pool, dl: dl, loader: loader, il: il, limits: limits, log: log, now: time.Now}
}

// Outcome of a Run.
type Outcome struct {
	ImportID int64
	Changed  bool // false: upstream unchanged, nothing replaced
	Rows     map[string]int64
}

// Run imports the feed if it changed. On any error the attempt is recorded as
// failed and the live schedule stays untouched.
func (im *Importer) Run(ctx context.Context) (*Outcome, error) {
	conn, err := im.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, importLockKey).Scan(&locked); err != nil {
		return nil, fmt.Errorf("import lock: %w", err)
	}
	if !locked {
		return nil, ErrImportRunning
	}
	defer func() {
		// Session-level lock: release on the same connection, even if ctx is done.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
		defer cancel()
		_, _ = conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, importLockKey)
	}()

	start := time.Now()
	dl, err := im.dl.Fetch(ctx)
	if err != nil {
		return nil, err // the downloader already recorded the failure
	}
	defer func() { _ = dl.Remove() }()
	if !dl.Changed {
		return &Outcome{ImportID: dl.ImportID}, nil
	}

	rows, err := im.loadCheckSwap(ctx, dl)
	if err != nil {
		im.log.Error("gtfs import failed, live schedule unchanged", "import_id", dl.ImportID, "error", err.Error())
		return nil, errors.Join(err, im.il.Fail(ctx, dl.ImportID, err))
	}
	if err := im.il.Succeed(ctx, dl.ImportID, rows); err != nil {
		return nil, err
	}
	im.log.Info("gtfs import succeeded", "import_id", dl.ImportID, "duration", time.Since(start).Round(time.Second),
		"stop_times", rows["stop_times"], keyInScopePoles, rows[keyInScopePoles])
	return &Outcome{ImportID: dl.ImportID, Changed: true, Rows: rows}, nil
}

func (im *Importer) loadCheckSwap(ctx context.Context, dl *Download) (map[string]int64, error) {
	res, err := im.loader.Load(ctx, dl.Path)
	if err != nil {
		return nil, err
	}
	prev, err := im.previousCounts(ctx)
	if err != nil {
		return nil, err
	}
	today := im.now().In(platform.Berlin)
	if err := check(ctx, im.pool, res, prev, today, im.limits); err != nil {
		return nil, err
	}
	if err := swap(ctx, im.pool, im.log); err != nil {
		return nil, err
	}
	rows := make(map[string]int64, len(res.Rows)+2)
	for k, v := range res.Rows {
		rows[k] = v
	}
	rows[keyInScopePoles] = res.InScopePoles
	rows[keyInScopeStations] = res.InScopeStations
	return rows, nil
}

// previousCounts are the row counts of the last successful import, or nil.
func (im *Importer) previousCounts(ctx context.Context) (map[string]int64, error) {
	last, err := im.il.lastSuccessful(ctx, im.dl.cfg.Source)
	if err != nil || last == nil || len(last.RowCounts) == 0 {
		return nil, err
	}
	var counts map[string]int64
	if err := json.Unmarshal(last.RowCounts, &counts); err != nil {
		return nil, fmt.Errorf("row_counts of import %d: %w", last.ID, err)
	}
	return counts, nil
}
