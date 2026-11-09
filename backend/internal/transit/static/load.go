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
)

// StagingSchema receives each import before the swap (ADR-0004).
const StagingSchema = "gtfs_staging"

// stagingDDL is db/gtfs/schema.sql for the staging schema.
func stagingDDL() string {
	ddl := strings.ReplaceAll(db.GTFSSchema, "CREATE SCHEMA gtfs;", "CREATE SCHEMA "+StagingSchema+";")
	return strings.ReplaceAll(ddl, "gtfs.", StagingSchema+".")
}

// Loader streams the CSV files of a GTFS zip into the staging schema.
type Loader struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewLoader returns a Loader that writes through pool.
func NewLoader(pool *pgxpool.Pool, log *slog.Logger) *Loader {
	return &Loader{pool: pool, log: log}
}

// Load recreates the staging schema and copies all tables of the zip into it,
// in one transaction: on error the previous staging schema is left as it was.
// It returns the number of rows per table.
//
// Files are read straight from the zip (no extraction) and rows go to Postgres
// with COPY, one row at a time, so memory use does not grow with file size.
func (l *Loader) Load(ctx context.Context, zipPath string) (map[string]int64, error) {
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
	if _, err := tx.Exec(ctx, stagingDDL()); err != nil {
		return nil, fmt.Errorf("create %s: %w", StagingSchema, err)
	}

	counts := make(map[string]int64, len(tables))
	for _, spec := range tables {
		t0 := time.Now()
		n, err := copyTable(ctx, tx, &z.Reader, spec)
		if err != nil {
			return nil, err
		}
		counts[spec.table] = n
		l.log.Info("gtfs table loaded", "table", spec.table, "rows", n, "duration", time.Since(t0).Round(time.Millisecond))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit %s: %w", StagingSchema, err)
	}
	l.log.Info("gtfs staging loaded", "duration", time.Since(start).Round(time.Millisecond))
	return counts, nil
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
