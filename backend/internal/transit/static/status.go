package static

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/queries"
)

// ScheduleState is what the health endpoint reports about the schedule.
type ScheduleState struct {
	HasData   bool       // the live schema exists
	CheckedAt *time.Time // last import that succeeded or found the feed unchanged
}

// Status reads the schedule state of source.
type Status struct {
	pool   *pgxpool.Pool
	source string
}

// NewStatus returns a Status for the static feed source (SourceGTFS).
func NewStatus(pool *pgxpool.Pool, source string) *Status {
	return &Status{pool: pool, source: source}
}

// HasData reports whether the live schema exists.
func (s *Status) HasData(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT to_regclass('`+LiveSchema+`.stops') IS NOT NULL`).Scan(&ok)
	return ok, err
}

// Current reports whether the live schema exists and has this binary's
// Layout. If not, the scheduler imports at once (ARCHITECTURE §3.1).
func (s *Status) Current(ctx context.Context) (bool, error) {
	return liveLayoutCurrent(ctx, s.pool)
}

// ScheduleState returns whether data exists and when it was last confirmed.
func (s *Status) ScheduleState(ctx context.Context) (ScheduleState, error) {
	ok, err := s.HasData(ctx)
	if err != nil {
		return ScheduleState{}, err
	}
	at, err := queries.New(s.pool).LastScheduleCheck(ctx, s.source)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduleState{HasData: ok}, nil
	}
	if err != nil {
		return ScheduleState{}, fmt.Errorf("last schedule check: %w", err)
	}
	return ScheduleState{HasData: ok, CheckedAt: at}, nil
}
