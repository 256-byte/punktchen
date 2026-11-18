// Package importlog records import attempts of upstream feeds in
// meta.feed_imports (DATA_MODEL §3.3): the static GTFS (ARCHITECTURE §3.1)
// and the amenity datasets (ADR-0010).
package importlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/256-byte/punktchen/backend/gen/queries"
)

// Import attempt states in meta.feed_imports (DATA_MODEL §3.3).
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

// bookkeepingTimeout bounds the writes that close an attempt. They run even
// when the caller's context is already cancelled, so no row stays "running".
const bookkeepingTimeout = 5 * time.Second

// Log records import attempts in meta.feed_imports.
type Log struct {
	q *queries.Queries
}

// New returns a Log on db (a pool or a transaction).
func New(db queries.DBTX) *Log {
	return &Log{q: queries.New(db)}
}

// LastSuccessful returns the newest succeeded attempt of source, or nil.
func (l *Log) LastSuccessful(ctx context.Context, source string) (*queries.MetaFeedImport, error) {
	row, err := l.q.LastSuccessfulFeedImport(ctx, source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last successful import of %s: %w", source, err)
	}
	return &row, nil
}

// Start opens a "running" attempt before anything is downloaded.
func (l *Log) Start(ctx context.Context, source, url string) (int64, error) {
	id, err := l.q.StartFeedImport(ctx, queries.StartFeedImportParams{Source: source, URL: url})
	if err != nil {
		return 0, fmt.Errorf("start import of %s: %w", source, err)
	}
	return id, nil
}

// RecordDownload stores the validators as received and the SHA-256 of the
// download (empty: nothing downloaded, e.g. 304).
func (l *Log) RecordDownload(ctx context.Context, id int64, etag, lastModified, sha256 string) error {
	n, err := l.q.RecordFeedDownload(ctx, queries.RecordFeedDownloadParams{
		ID: id, Etag: nonEmpty(etag), LastModified: nonEmpty(lastModified), SHA256: nonEmpty(sha256),
	})
	return checkOneRow(id, n, err)
}

// Succeed closes attempt id as succeeded with the imported row counts.
func (l *Log) Succeed(ctx context.Context, id int64, rowCounts map[string]int64) error {
	counts, err := json.Marshal(rowCounts)
	if err != nil {
		return err
	}
	return l.finish(ctx, id, StatusSucceeded, counts, nil)
}

// Fail closes attempt id as failed with the cause.
func (l *Log) Fail(ctx context.Context, id int64, cause error) error {
	msg := "unknown error"
	if cause != nil {
		msg = cause.Error()
	}
	return l.finish(ctx, id, StatusFailed, nil, &msg)
}

// Skip closes attempt id as skipped: upstream did not change.
func (l *Log) Skip(ctx context.Context, id int64) error {
	return l.finish(ctx, id, StatusSkipped, nil, nil)
}

func (l *Log) finish(ctx context.Context, id int64, status string, rowCounts json.RawMessage, errMsg *string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	n, err := l.q.FinishFeedImport(ctx, queries.FinishFeedImportParams{
		ID: id, Status: status, RowCounts: rowCounts, Error: errMsg,
	})
	return checkOneRow(id, n, err)
}

func checkOneRow(id, n int64, err error) error {
	if err != nil {
		return fmt.Errorf("update import %d: %w", id, err)
	}
	if n != 1 {
		return fmt.Errorf("update import %d: not running", id)
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
