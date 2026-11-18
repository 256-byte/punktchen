// Package amenities imports and serves the public toilet and drinking
// fountain layers from Berlin open data (ADR-0010, DATA_MODEL §3.7).
package amenities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/geo"
	"github.com/256-byte/punktchen/backend/internal/platform/importlog"
)

const (
	// DefaultMaxBytes caps a download; the answers are 0.1–0.3 MB (DATA_MODEL §7 #34).
	DefaultMaxBytes = 32 << 20
	// DefaultTimeout bounds one whole download.
	DefaultTimeout = time.Minute
)

// SanityLimits are checked before a new answer replaces the stored rows; a
// failed check keeps the old rows (ADR-0010).
type SanityLimits struct {
	MinRows int     // absolute floor
	MaxDrop float64 // max relative drop vs. the last successful import, e.g. 0.3
}

// Dataset describes one WFS dataset: where it comes from, how its answer
// becomes rows R, and how the rows replace the table's content.
type Dataset[R any] struct {
	Source  string // meta.feed_imports source
	URL     string
	Limits  SanityLimits
	Parse   func(body []byte, scope geo.BBox) ([]R, error)
	Replace func(ctx context.Context, q *queries.Queries, rows []R) error
	Count   func(q *queries.Queries, ctx context.Context) (int32, error) // e.g. (*queries.Queries).CountToilets
}

// Toilets is the public toilet dataset (525 rows on 2026-11-18).
func Toilets(url string) Dataset[queries.InsertToiletsParams] {
	return Dataset[queries.InsertToiletsParams]{
		Source: SourceToilets, URL: url, Parse: ParseToilets,
		Limits: SanityLimits{MinRows: 200, MaxDrop: 0.3},
		Count:  (*queries.Queries).CountToilets,
		Replace: func(ctx context.Context, q *queries.Queries, rows []queries.InsertToiletsParams) error {
			if err := q.DeleteToilets(ctx); err != nil {
				return err
			}
			_, err := q.InsertToilets(ctx, rows)
			return err
		},
	}
}

// Fountains is the drinking fountain dataset (242 rows on 2026-11-18).
func Fountains(url string) Dataset[queries.InsertFountainsParams] {
	return Dataset[queries.InsertFountainsParams]{
		Source: SourceFountains, URL: url, Parse: ParseFountains,
		Limits: SanityLimits{MinRows: 100, MaxDrop: 0.3},
		Count:  (*queries.Queries).CountFountains,
		Replace: func(ctx context.Context, q *queries.Queries, rows []queries.InsertFountainsParams) error {
			if err := q.DeleteFountains(ctx); err != nil {
				return err
			}
			_, err := q.InsertFountains(ctx, rows)
			return err
		},
	}
}

// Config configures an Importer. Zero values get defaults.
type Config struct {
	UserAgent string       // required: names the project (CLAUDE.md rule 7 spirit)
	Scope     geo.BBox     // every point must lie inside
	MaxBytes  int64        // default DefaultMaxBytes
	Client    *http.Client // default: timeout DefaultTimeout
}

// Importer downloads one dataset and replaces its rows when it changed.
type Importer[R any] struct {
	pool *pgxpool.Pool
	ds   Dataset[R]
	cfg  Config
	il   *importlog.Log
	log  *slog.Logger
}

// NewImporter returns an Importer for ds that logs attempts in il.
func NewImporter[R any](pool *pgxpool.Pool, ds Dataset[R], cfg Config, il *importlog.Log, log *slog.Logger) (*Importer[R], error) {
	switch {
	case ds.Source == "" || ds.URL == "" || ds.Parse == nil || ds.Replace == nil || ds.Count == nil:
		return nil, errors.New("amenities: dataset needs source, URL, Parse, Replace and Count")
	case cfg.UserAgent == "":
		return nil, errors.New("amenities: UserAgent is required")
	}
	if err := cfg.Scope.Validate(); err != nil {
		return nil, fmt.Errorf("amenities: scope: %w", err)
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Importer[R]{pool: pool, ds: ds, cfg: cfg, il: il, log: log.With("source", ds.Source)}, nil
}

// Source is the dataset's meta.feed_imports source.
func (im *Importer[R]) Source() string { return im.ds.Source }

// Empty reports whether the dataset has no stored rows (then the scheduler
// imports at startup with force).
func (im *Importer[R]) Empty(ctx context.Context) (bool, error) {
	n, err := im.ds.Count(queries.New(im.pool), ctx)
	return n == 0, err
}

// Result of one Run.
type Result struct {
	ImportID int64
	Changed  bool // false: same SHA-256 as the last successful import ("skipped")
	Rows     int  // rows stored when Changed
}

// Run downloads the dataset and replaces the rows if the answer changed.
// Upstream sends no ETag or Last-Modified (DATA_MODEL §7 #34), so the
// SHA-256 of the features (contentHash) is compared with the last successful import. With
// force the rows are replaced even if the hash is the same (e.g. the table
// is empty). Parse errors and failed sanity checks keep the old rows; the
// attempt is then "failed" in meta.feed_imports.
func (im *Importer[R]) Run(ctx context.Context, force bool) (Result, error) {
	last, err := im.il.LastSuccessful(ctx, im.ds.Source)
	if err != nil {
		return Result{}, err
	}
	id, err := im.il.Start(ctx, im.ds.Source, im.ds.URL)
	if err != nil {
		return Result{}, err
	}
	res, err := im.run(ctx, id, last, force)
	if err != nil {
		return Result{ImportID: id}, errors.Join(err, im.il.Fail(ctx, id, err))
	}
	return res, nil
}

func (im *Importer[R]) run(ctx context.Context, id int64, last *queries.MetaFeedImport, force bool) (Result, error) {
	start := time.Now()
	body, etag, lastModified, err := im.download(ctx)
	if err != nil {
		return Result{}, err
	}
	hash := contentHash(body)
	if err := im.il.RecordDownload(ctx, id, etag, lastModified, hash); err != nil {
		return Result{}, err
	}
	if !force && last != nil && last.SHA256 != nil && *last.SHA256 == hash {
		im.log.Info("amenities unchanged", "import_id", id, "bytes", len(body))
		return Result{ImportID: id}, im.il.Skip(ctx, id)
	}

	rows, err := im.ds.Parse(body, im.cfg.Scope)
	if err != nil {
		return Result{}, err
	}
	if err := check(len(rows), previousRows(last), im.ds.Limits); err != nil {
		return Result{}, err
	}
	tx, err := im.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := im.ds.Replace(ctx, queries.New(tx), rows); err != nil {
		return Result{}, fmt.Errorf("replace %s: %w", im.ds.Source, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit %s: %w", im.ds.Source, err)
	}
	if err := im.il.Succeed(ctx, id, map[string]int64{rowsKey: int64(len(rows))}); err != nil {
		return Result{}, err
	}
	im.log.Info("amenities imported", "import_id", id, "rows", len(rows), "bytes", len(body),
		"sha256", hash, "duration", time.Since(start).Round(time.Millisecond))
	return Result{ImportID: id, Changed: true, Rows: len(rows)}, nil
}

// contentHash is the SHA-256 of the answer's "features" array. The WFS adds
// a "timeStamp" of the request to every answer (DATA_MODEL §7 #34), so the
// whole body never repeats; the features do while the data is unchanged.
// A body without a readable "features" member is hashed whole (Parse rejects
// it anyway).
func contentHash(body []byte) string {
	var fc struct {
		Features json.RawMessage `json:"features"`
	}
	content := body
	if json.Unmarshal(body, &fc) == nil && len(fc.Features) > 0 {
		content = fc.Features
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// rowsKey is the row_counts key of an amenity import.
const rowsKey = "rows"

func (im *Importer[R]) download(ctx context.Context) (body []byte, etag, lastModified string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, im.ds.URL, nil)
	if err != nil {
		return nil, "", "", err
	}
	// No Accept-Encoding here: net/http then asks for gzip and decompresses.
	req.Header.Set("User-Agent", im.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := im.cfg.Client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("GET %s: %w", im.ds.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("GET %s: unexpected status %s", im.ds.URL, resp.Status)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, im.cfg.MaxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("download %s: %w", im.ds.URL, err)
	}
	if int64(len(body)) > im.cfg.MaxBytes {
		return nil, "", "", fmt.Errorf("download %s: larger than %d bytes", im.ds.URL, im.cfg.MaxBytes)
	}
	return body, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}

// previousRows is the row count of the last successful import (0: none).
func previousRows(last *queries.MetaFeedImport) int64 {
	if last == nil || len(last.RowCounts) == 0 {
		return 0
	}
	var counts map[string]int64
	if json.Unmarshal(last.RowCounts, &counts) != nil {
		return 0
	}
	return counts[rowsKey]
}

// check returns all problems with a new answer of n rows at once.
func check(n int, prev int64, lim SanityLimits) error {
	var problems []error
	if n < lim.MinRows {
		problems = append(problems, fmt.Errorf("only %d rows, want at least %d", n, lim.MinRows))
	}
	if prev > 0 && float64(n) < float64(prev)*(1-lim.MaxDrop) {
		problems = append(problems, fmt.Errorf("rows dropped from %d to %d (more than %.0f %%)", prev, n, lim.MaxDrop*100))
	}
	if err := errors.Join(problems...); err != nil {
		return fmt.Errorf("sanity check: %w", err)
	}
	return nil
}
