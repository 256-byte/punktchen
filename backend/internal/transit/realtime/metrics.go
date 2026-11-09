package realtime

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics of the GTFS-RT ingestor (ARCHITECTURE §3.7). A nil *Metrics
// records nothing.
type Metrics struct {
	polls *prometheus.CounterVec
}

// NewMetrics registers the realtime metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		polls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "livestop_gtfsrt_poll_total",
			Help: "GTFS-RT polls by result (updated, not_modified, rejected, error).",
		}, []string{"result"}),
	}
	for _, r := range []Result{ResultUpdated, ResultNotModified, ResultRejected, ResultError} {
		m.polls.WithLabelValues(string(r)) // export zeros from the start
	}
	reg.MustRegister(m.polls)
	return m
}

func (m *Metrics) polled(r Result) {
	if m == nil {
		return
	}
	m.polls.WithLabelValues(string(r)).Inc()
}

// RegisterFeedAge exports livestop_gtfsrt_feed_age_seconds: now minus the
// header.timestamp of the store's current state, read at scrape time; 0
// before the first feed.
func RegisterFeedAge(reg prometheus.Registerer, s *Store, now func() time.Time) {
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "livestop_gtfsrt_feed_age_seconds",
		Help: "Age of the applied GTFS-RT feed by its header.timestamp; 0 before the first feed.",
	}, func() float64 {
		st := s.Current()
		if st == nil {
			return 0
		}
		return now().Sub(st.FeedTime).Seconds()
	}))
}
