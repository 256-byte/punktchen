package static

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

// Trigger names a reason for an import run (metric label).
type Trigger string

const (
	TriggerStartup   Trigger = "startup"   // no current schedule (none yet, or another layout)
	TriggerScheduled Trigger = "scheduled" // daily run
	TriggerRetry     Trigger = "retry"     // after a failed run
	TriggerManual    Trigger = "manual"    // POST /internal/import
)

// DefaultRetryAfter is the wait after a failed run, unless the next daily run
// comes first.
const DefaultRetryAfter = time.Hour

// SchedulerConfig sets when imports run. Times are in loc (Europe/Berlin).
type SchedulerConfig struct {
	Hour, Minute int
	Loc          *time.Location
	RetryAfter   time.Duration // default DefaultRetryAfter
}

// RunFunc runs one import (Importer.Run).
type RunFunc func(ctx context.Context) (*Outcome, error)

// Scheduler runs the import daily, at startup when there is no current
// schedule (none yet, or another Layout), after failures, and on demand
// (ARCHITECTURE §3.1).
type Scheduler struct {
	cfg     SchedulerConfig
	run     RunFunc
	current func(ctx context.Context) (bool, error)
	metrics *Metrics
	log     *slog.Logger

	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	trigger chan struct{}
	running atomic.Bool
}

// NewScheduler returns a Scheduler; call Run to start it.
func NewScheduler(cfg SchedulerConfig, run RunFunc, current func(ctx context.Context) (bool, error), m *Metrics, log *slog.Logger) *Scheduler {
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = DefaultRetryAfter
	}
	if cfg.Loc == nil {
		cfg.Loc = time.UTC
	}
	return &Scheduler{
		cfg: cfg, run: run, current: current, metrics: m, log: log,
		now: time.Now, after: time.After,
		trigger: make(chan struct{}, 1),
	}
}

// Trigger asks for an import now. It returns ErrImportRunning if one is
// running or already queued.
func (s *Scheduler) Trigger() error {
	if s.running.Load() {
		return ErrImportRunning
	}
	select {
	case s.trigger <- struct{}{}:
		return nil
	default:
		return ErrImportRunning
	}
}

// Run blocks until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	failed := false
	if ok, err := s.current(ctx); err != nil {
		s.log.Error("cannot check for schedule data; importing", "error", err.Error())
		failed = !s.runOnce(ctx, TriggerStartup)
	} else if !ok {
		s.log.Info("no current schedule (none yet, or another layout); importing now")
		failed = !s.runOnce(ctx, TriggerStartup)
	}

	for {
		next, why := s.next(s.now(), failed)
		s.log.Info("next gtfs import", "at", next.Format(time.RFC3339), "trigger", string(why))
		select {
		case <-ctx.Done():
			return
		case <-s.after(next.Sub(s.now())):
			failed = !s.runOnce(ctx, why)
		case <-s.trigger:
			failed = !s.runOnce(ctx, TriggerManual)
		}
	}
}

// next is the time of the next run: the daily slot, or a retry if the last
// run failed and the retry comes earlier.
func (s *Scheduler) next(now time.Time, lastFailed bool) (time.Time, Trigger) {
	daily := nextDaily(now, s.cfg.Hour, s.cfg.Minute, s.cfg.Loc)
	if retry := now.Add(s.cfg.RetryAfter); lastFailed && retry.Before(daily) {
		return retry, TriggerRetry
	}
	return daily, TriggerScheduled
}

// nextDaily is the first hour:minute in loc strictly after now. time.Date
// keeps the wall clock across DST changes (03:30 stays 03:30).
func nextDaily(now time.Time, hour, minute int, loc *time.Location) time.Time {
	l := now.In(loc)
	t := time.Date(l.Year(), l.Month(), l.Day(), hour, minute, 0, 0, loc)
	if !t.After(now) {
		t = time.Date(l.Year(), l.Month(), l.Day()+1, hour, minute, 0, 0, loc)
	}
	return t
}

// runOnce runs one import and reports whether it ended without error.
func (s *Scheduler) runOnce(ctx context.Context, why Trigger) bool {
	s.running.Store(true)
	defer s.running.Store(false)
	start := time.Now()
	log := s.log.With("trigger", string(why))
	log.Info("gtfs import started")

	out, err := s.run(ctx)
	result := resultOf(out, err)
	s.metrics.observe(why, result, time.Since(start), s.now())
	switch {
	case errors.Is(err, ErrImportRunning):
		log.Warn("gtfs import skipped: another import is running")
		return true // not a failure of the data; no retry
	case err != nil:
		if ctx.Err() == nil {
			log.Error("gtfs import failed", "error", err.Error(), "duration", time.Since(start).Round(time.Second))
		}
		return false
	}
	log.Info("gtfs import finished", "result", result, "import_id", out.ImportID, "duration", time.Since(start).Round(time.Second))
	return true
}
