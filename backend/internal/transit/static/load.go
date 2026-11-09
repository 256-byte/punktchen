package static

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/geo"
)

// StagingSchema receives each import before the swap (ADR-0004).
const StagingSchema = "gtfs_staging"

// toStaging rewrites SQL written for schema "gtfs" (db/gtfs/*.sql) to the
// staging schema.
func toStaging(sql string) string {
	sql = strings.ReplaceAll(sql, "CREATE SCHEMA gtfs;", "CREATE SCHEMA "+StagingSchema+";")
	return strings.ReplaceAll(sql, "gtfs.", StagingSchema+".")
}

// derivedTables are built by db/gtfs/derived.sql after loading.
var derivedTables = []string{"service_dates", "trip_info", "pole_directions", "stations"}

// Loader streams the CSV files of a GTFS zip into the staging schema.
type Loader struct {
	pool  *pgxpool.Pool
	scope geo.BBox
	log   *slog.Logger
}

// NewLoader returns a Loader that writes through pool and flags stops inside
// scope as in_scope (DATA_MODEL §4).
func NewLoader(pool *pgxpool.Pool, scope geo.BBox, log *slog.Logger) *Loader {
	return &Loader{pool: pool, scope: scope, log: log}
}

// LoadResult describes a loaded staging schema.
type LoadResult struct {
	Rows            map[string]int64 // per table
	InScopePoles    int64
	InScopeStations int64 // grouped as in DATA_MODEL §3.4
}

// Load recreates the staging schema from the zip in one transaction: create
// tables, COPY every file, flag the scope, add keys and indexes, ANALYZE. On
// error the previous staging schema is left as it was.
//
// Files are read straight from the zip (no extraction) and rows go to Postgres
// with COPY, one row at a time, so memory use does not grow with file size.
func (l *Loader) Load(ctx context.Context, zipPath string) (*LoadResult, error) {
	start := time.Now()
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", zipPath, err)
	}
	defer func() { _ = z.Close() }()

	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	if _, err := tx.Exec(ctx, "DROP SCHEMA IF EXISTS "+StagingSchema+" CASCADE"); err != nil {
		return nil, fmt.Errorf("drop %s: %w", StagingSchema, err)
	}
	if _, err := tx.Exec(ctx, toStaging(db.GTFSSchema)); err != nil {
		return nil, fmt.Errorf("create %s: %w", StagingSchema, err)
	}

	res := &LoadResult{Rows: make(map[string]int64, len(tables))}
	for _, spec := range tables {
		t0 := time.Now()
		n, err := copyTable(ctx, tx, &z.Reader, spec)
		if err != nil {
			return nil, err
		}
		res.Rows[spec.table] = n
		l.log.Info("gtfs table loaded", "table", spec.table, "rows", n, "duration", time.Since(t0).Round(time.Millisecond))
	}

	if err := l.finish(ctx, tx, res); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit %s: %w", StagingSchema, err)
	}
	l.log.Info("gtfs staging loaded", "duration", time.Since(start).Round(time.Millisecond),
		"in_scope_poles", res.InScopePoles, "in_scope_stations", res.InScopeStations)
	return res, nil
}

// finish flags the scope, builds keys and indexes and refreshes statistics.
func (l *Loader) finish(ctx context.Context, tx pgx.Tx, res *LoadResult) error {
	t0 := time.Now()
	b := l.scope
	// A pole is in scope if it lies in the bbox; a station if any child pole does.
	if _, err := tx.Exec(ctx, `UPDATE `+StagingSchema+`.stops SET in_scope = true
		WHERE coalesce(location_type, 0) = 0
		  AND stop_lon BETWEEN $1 AND $3 AND stop_lat BETWEEN $2 AND $4`,
		b.MinLon, b.MinLat, b.MaxLon, b.MaxLat); err != nil {
		return fmt.Errorf("scope poles: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE `+StagingSchema+`.stops st SET in_scope = true
		WHERE st.location_type = 1
		  AND EXISTS (SELECT 1 FROM `+StagingSchema+`.stops p WHERE p.parent_station = st.stop_id AND p.in_scope)`); err != nil {
		return fmt.Errorf("scope stations: %w", err)
	}
	// More memory per index build and sort than the defaults; only for this tx.
	if _, err := tx.Exec(ctx, `SET LOCAL maintenance_work_mem = '256MB'; SET LOCAL work_mem = '64MB'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, toStaging(db.GTFSIndexes)); err != nil {
		return fmt.Errorf("keys and indexes: %w", err)
	}
	// Statistics first, so the derived-table queries get good plans.
	if err := analyze(ctx, tx, loadedTables()); err != nil {
		return err
	}
	l.log.Info("gtfs staging indexed", "duration", time.Since(t0).Round(time.Millisecond))

	t1 := time.Now()
	if _, err := tx.Exec(ctx, toStaging(db.GTFSDerived)); err != nil {
		return fmt.Errorf("derived tables: %w", err)
	}
	if err := analyze(ctx, tx, derivedTables); err != nil {
		return err
	}
	for _, table := range derivedTables {
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{StagingSchema, table}.Sanitize()).Scan(&n); err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}
		res.Rows[table] = n
	}
	if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM `+StagingSchema+`.stops WHERE in_scope AND coalesce(location_type, 0) = 0),
			(SELECT count(*) FROM `+StagingSchema+`.stations WHERE in_scope)`).Scan(&res.InScopePoles, &res.InScopeStations); err != nil {
		return fmt.Errorf("count scope: %w", err)
	}
	l.log.Info("gtfs derived tables built", "duration", time.Since(t1).Round(time.Millisecond),
		"service_dates", res.Rows["service_dates"], "pole_directions", res.Rows["pole_directions"], "stations", res.Rows["stations"])
	return nil
}

func loadedTables() []string {
	names := make([]string, len(tables))
	for i, spec := range tables {
		names[i] = spec.table
	}
	return names
}

func analyze(ctx context.Context, tx pgx.Tx, names []string) error {
	for _, name := range names {
		if _, err := tx.Exec(ctx, `ANALYZE `+pgx.Identifier{StagingSchema, name}.Sanitize()); err != nil {
			return fmt.Errorf("analyze %s: %w", name, err)
		}
	}
	return nil
}

func copyTable(ctx context.Context, tx pgx.Tx, z *zip.Reader, spec tableSpec) (int64, error) {
	f, err := z.Open(spec.file)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", spec.file, err)
	}
	defer func() { _ = f.Close() }()

	src, err := newCSVSource(f, spec)
	if err != nil {
		return 0, err
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{StagingSchema, spec.table}, spec.dbColumns(), src)
	if err != nil {
		// A conversion error from the source is more useful than COPY's wrapper.
		if src.err != nil {
			return 0, src.err
		}
		return 0, fmt.Errorf("copy %s: %w", spec.file, err)
	}
	return n, nil
}

// csvSource adapts a GTFS CSV file to pgx.CopyFromSource.
type csvSource struct {
	r    *csv.Reader
	spec tableSpec
	idx  []int // CSV field index per spec column, -1 if absent
	vals []any
	line int
	err  error
}

func newCSVSource(r io.Reader, spec tableSpec) (*csvSource, error) {
	br, err := skipBOM(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.file, err)
	}
	cr := csv.NewReader(br)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: read header: %w", spec.file, err)
	}
	pos := make(map[string]int, len(header))
	for i, h := range header {
		pos[strings.TrimSpace(h)] = i
	}
	s := &csvSource{r: cr, spec: spec, idx: make([]int, len(spec.cols)), line: 1}
	for i, c := range spec.cols {
		p, ok := pos[c.name]
		if !ok && c.required {
			return nil, fmt.Errorf("%s: required column %s missing", spec.file, c.name)
		}
		if !ok {
			p = -1
		}
		s.idx[i] = p
	}
	return s, nil
}

func (s *csvSource) Next() bool {
	rec, err := s.r.Read()
	if errors.Is(err, io.EOF) {
		return false
	}
	s.line++
	if err != nil {
		s.err = fmt.Errorf("%s line %d: %w", s.spec.file, s.line, err)
		return false
	}
	vals := s.vals[:0]
	for i, c := range s.spec.cols {
		raw := ""
		if p := s.idx[i]; p >= 0 && p < len(rec) {
			raw = rec[p]
		}
		if vals, err = c.convert(vals, raw); err != nil {
			s.err = fmt.Errorf("%s line %d: %w", s.spec.file, s.line, err)
			return false
		}
	}
	s.vals = vals
	return true
}

// skipBOM drops a leading UTF-8 byte order mark. It must go before the CSV
// parser sees it: with a quoted first header field (`"stop_id"`), a BOM makes
// the parser fail with "bare \" in non-quoted-field".
func skipBOM(r io.Reader) (io.Reader, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	head, err := br.Peek(3)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if bytes.Equal(head, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}
	return br, nil
}

func (s *csvSource) Values() ([]any, error) { return s.vals, nil }

func (s *csvSource) Err() error { return s.err }
