package amenities

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

// Trigger names a reason for a run (metric label).
type Trigger string

const (
	TriggerStartup   Trigger = "startup"   // a dataset has no rows
	TriggerScheduled Trigger = "scheduled" // daily run
	TriggerRetry     Trigger = "retry"     // after a failed run
	TriggerManual    Trigger = "manual"    // POST /internal/import (dev)
)

// Run results (metric label).
const (
	resultSucceeded = "succeeded" // new rows stored
	resultSkipped   = "skipped"   // same answer as before
	resultFailed    = "failed"
)

// DefaultRetryAfter is the wait after a failed run, unless the daily run
// comes first.
const DefaultRetryAfter = time.Hour

// Job is one dataset import (an *Importer[R]).
type Job interface {
	Source() string
	Empty(ctx context.Context) (bool, error)
	Run(ctx context.Context, force bool) (Result, error)
}

// SchedulerConfig sets when imports run. Times are in Loc (Europe/Berlin).
type SchedulerConfig struct {
	Hour, Minute int
	Loc          *time.Location
	RetryAfter   time.Duration // default DefaultRetryAfter
}

// Scheduler imports every dataset at startup when it has no rows, daily at
// the configured time, an hour after a failed run, and on demand (ADR-0010).
// The datasets run one after the other; each is small (DATA_MODEL §7 #34).
type Scheduler struct {
	cfg     SchedulerConfig
	jobs    []Job
	metrics *Metrics
	log     *slog.Logger

	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	trigger chan struct{}
	running atomic.Bool
}

// NewScheduler returns a Scheduler for jobs; call Run to start it.
func NewScheduler(cfg SchedulerConfig, jobs []Job, m *Metrics, log *slog.Logger) *Scheduler {
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = DefaultRetryAfter
	}
	if cfg.Loc == nil {
		cfg.Loc = time.UTC
	}
	return &Scheduler{
		cfg: cfg, jobs: jobs, metrics: m, log: log,
		now: time.Now, after: time.After,
		trigger: make(chan struct{}, 1),
	}
}

// Trigger asks for a run of all datasets now. It reports false if a run is
// in progress or already queued (nothing to do then).
func (s *Scheduler) Trigger() bool {
	if s.running.Load() {
		return false
	}
	select {
	case s.trigger <- struct{}{}:
		return true
	default:
		return false
	}
}

// Run blocks until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	failed := !s.runAll(ctx, TriggerStartup, true)
	for {
		daily := platform.NextDaily(s.now(), s.cfg.Hour, s.cfg.Minute, s.cfg.Loc)
		next, why := daily, TriggerScheduled
		if retry := s.now().Add(s.cfg.RetryAfter); failed && retry.Before(daily) {
			next, why = retry, TriggerRetry
		}
		s.log.Info("next amenities import", "at", next.Format(time.RFC3339), "trigger", string(why))
		select {
		case <-ctx.Done():
			return
		case <-s.after(next.Sub(s.now())):
			failed = !s.runAll(ctx, why, false)
		case <-s.trigger:
			failed = !s.runAll(ctx, TriggerManual, false)
		}
	}
}

// runAll runs the jobs and reports whether all ended without error. With
// onlyEmpty (startup) only datasets without rows run. A dataset without rows
// is always forced: its last answer may be unchanged while the table was
// emptied (e.g. a restored database).
func (s *Scheduler) runAll(ctx context.Context, why Trigger, onlyEmpty bool) bool {
	s.running.Store(true)
	defer s.running.Store(false)
	ok := true
	for _, j := range s.jobs {
		if ctx.Err() != nil {
			return ok
		}
		log := s.log.With("source", j.Source(), "trigger", string(why))
		empty, err := j.Empty(ctx)
		if err != nil {
			log.Error("cannot count stored rows; importing", "error", err.Error())
			empty = true
		}
		if onlyEmpty && !empty {
			continue
		}
		res, err := j.Run(ctx, empty)
		result := resultSkipped
		switch {
		case err != nil:
			result, ok = resultFailed, false
			if ctx.Err() == nil {
				log.Error("amenities import failed", "error", err.Error())
			}
		case res.Changed:
			result = resultSucceeded
		}
		s.metrics.observe(j.Source(), why, result, res.Rows, s.now())
	}
	return ok
}

// Metrics of the amenity imports (ARCHITECTURE §3.7).
type Metrics struct {
	runs        *prometheus.CounterVec
	lastSuccess *prometheus.GaugeVec
	rows        *prometheus.GaugeVec
}

// NewMetrics registers the amenity import metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "punktchen_amenities_import_runs_total",
			Help: "Amenity import runs by source, trigger and result (succeeded, skipped, failed).",
		}, []string{"source", "trigger", "result"}),
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "punktchen_amenities_import_last_success_timestamp_seconds",
			Help: "Unix time when a dataset was last confirmed current (import succeeded or answer unchanged).",
		}, []string{"source"}),
		rows: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "punktchen_amenities_rows",
			Help: "Rows stored by the last successful import of a dataset.",
		}, []string{"source"}),
	}
	reg.MustRegister(m.runs, m.lastSuccess, m.rows)
	return m
}

func (m *Metrics) observe(source string, why Trigger, result string, rows int, now time.Time) {
	if m == nil {
		return
	}
	m.runs.WithLabelValues(source, string(why), result).Inc()
	if result == resultSucceeded || result == resultSkipped {
		m.lastSuccess.WithLabelValues(source).Set(float64(now.Unix()))
	}
	if result == resultSucceeded {
		m.rows.WithLabelValues(source).Set(float64(rows))
	}
}
