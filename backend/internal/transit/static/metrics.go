package static

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Run results (metric label).
const (
	resultSucceeded = "succeeded" // new feed imported
	resultSkipped   = "skipped"   // upstream unchanged
	resultFailed    = "failed"
	resultBusy      = "busy" // another import held the lock
)

// Metrics of the import (ARCHITECTURE §3.7).
type Metrics struct {
	runs        *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	lastSuccess prometheus.Gauge
}

// NewMetrics registers the import metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "livestop_import_runs_total",
			Help: "GTFS import runs by trigger and result (succeeded, skipped, failed, busy).",
		}, []string{"trigger", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "livestop_import_duration_seconds",
			Help:    "Duration of GTFS import runs, by result.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 180, 300, 600},
		}, []string{"result"}),
		lastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "livestop_import_last_success_timestamp_seconds",
			Help: "Unix time when the schedule was last confirmed current (import succeeded or feed unchanged).",
		}),
	}
	reg.MustRegister(m.runs, m.duration, m.lastSuccess)
	return m
}

func (m *Metrics) observe(why Trigger, result string, d time.Duration, now time.Time) {
	if m == nil {
		return
	}
	m.runs.WithLabelValues(string(why), result).Inc()
	m.duration.WithLabelValues(result).Observe(d.Seconds())
	if result == resultSucceeded || result == resultSkipped {
		m.lastSuccess.Set(float64(now.Unix()))
	}
}

func resultOf(out *Outcome, err error) string {
	switch {
	case errors.Is(err, ErrImportRunning):
		return resultBusy
	case err != nil:
		return resultFailed
	case out != nil && out.Changed:
		return resultSucceeded
	default:
		return resultSkipped
	}
}
