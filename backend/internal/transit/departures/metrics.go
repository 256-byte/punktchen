package departures

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics of the snapshot builder (ARCHITECTURE §3.7). A nil *Metrics
// records nothing.
type Metrics struct {
	build      *prometheus.HistogramVec
	version    prometheus.Gauge
	departures prometheus.Gauge
	failures   prometheus.Counter
}

// NewMetrics registers the snapshot metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		build: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "livestop_snapshot_build_seconds",
			Help:    "Time to load departures and publish a snapshot, by load (full, incremental).",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"load"}),
		version: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "livestop_snapshot_version",
			Help: "Version of the published departures snapshot.",
		}),
		departures: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "livestop_snapshot_departures",
			Help: "Departures in the published snapshot.",
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "livestop_snapshot_refresh_failures_total",
			Help: "Snapshot refreshes that failed; the last snapshot stays published.",
		}),
	}
	reg.MustRegister(m.build, m.version, m.departures, m.failures)
	return m
}

func (m *Metrics) published(load string, d time.Duration, s *Snapshot) {
	if m == nil {
		return
	}
	m.build.WithLabelValues(load).Observe(d.Seconds())
	m.version.Set(float64(s.Version))
	m.departures.Set(float64(s.Len()))
}

func (m *Metrics) failed() {
	if m == nil {
		return
	}
	m.failures.Inc()
}
