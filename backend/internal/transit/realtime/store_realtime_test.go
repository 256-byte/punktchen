package realtime

import (
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

func TestStoreOnApply(t *testing.T) {
	s := NewStore(slog.New(slog.DiscardHandler))
	calls := 0
	s.OnApply(func() { calls++ })
	for _, f := range []string{
		"vbb-20261108T225938Z-wedding.pb", // applied
		"vbb-empty-stale.pb",              // no data
		"vbb-20261108T225938Z-wedding.pb", // outdated
		"vbb-20261108T230009Z-wedding.pb", // applied
	} {
		_ = s.Apply(t.Context(), Feed{Body: readFixture(t, f)})
	}
	if calls != 2 {
		t.Errorf("OnApply called %d times, want 2 (applied feeds only)", calls)
	}
}

func TestMerger(t *testing.T) {
	s := NewStore(slog.New(slog.DiscardHandler))
	m := NewMetrics(prometheus.NewRegistry())
	merge := s.Merger(DefaultMaxAge, m)
	ratio := func() float64 { return testutil.ToFloat64(m.matchRatio) }
	day := departures.ServiceDay{Year: 2026, Month: time.November, Day: 8}
	feed := state().FeedTime
	deps := []departures.Departure{
		{TripID: "T1", ServiceDate: day, StopID: "p1", StopSequence: 1, Scheduled: feed.Add(8 * time.Minute)},
		{TripID: "T2", ServiceDate: day, StopID: "p1", StopSequence: 4, Scheduled: feed.Add(9 * time.Minute)}, // no TripUpdate
	}

	if out, ft := merge(deps, time.Now()); !ft.IsZero() || len(out) != 2 || out[0].Realtime || ratio() != 0 {
		t.Fatalf("no state: %+v %s, ratio %v", out, ft, ratio())
	}

	d := int32(90)
	st := state(&TripUpdate{Key: TripKey{"T1", day}, Stops: []StopUpdate{{Sequence: 1, StopID: "p1", Departure: Event{Delay: d, HasDelay: true}}}})
	s.cur.Store(st)
	if out, ft := merge(deps, st.FeedTime.Add(DefaultMaxAge)); !ft.Equal(st.FeedTime) || !out[0].Realtime || out[0].Delay != 90 {
		t.Errorf("fresh enough: %+v %s", out, ft)
	}
	if ratio() != 0.5 {
		t.Errorf("match ratio %v, want 0.5 (1 of 2 departures has a TripUpdate)", ratio())
	}
	out, ft := merge(deps, st.FeedTime.Add(DefaultMaxAge+time.Second))
	if !ft.Equal(st.FeedTime) || out[0].Realtime {
		t.Errorf("older than %s: %+v %s; want the schedule and the old feed time", DefaultMaxAge, out, ft)
	}
	if ratio() != 0 {
		t.Errorf("match ratio %v after the state got too old, want 0", ratio())
	}
	s.Merger(DefaultMaxAge, nil)(deps, time.Now()) // nil metrics record nothing
	if deps[0].Realtime {
		t.Error("Merger changed its input")
	}
}

func TestFeedAgeMetric(t *testing.T) {
	s := NewStore(slog.New(slog.DiscardHandler))
	reg := prometheus.NewRegistry()
	now := time.Date(2026, 11, 8, 23, 0, 40, 0, time.UTC)
	RegisterFeedAge(reg, s, func() time.Time { return now })
	if n, err := testutil.GatherAndCount(reg, "livestop_gtfsrt_feed_age_seconds"); err != nil || n != 1 {
		t.Fatalf("gauge: %d, %v", n, err)
	}
	mfs, _ := reg.Gather()
	if v := mfs[0].GetMetric()[0].GetGauge().GetValue(); v != 0 {
		t.Errorf("before the first feed: %v", v)
	}
	s.cur.Store(&State{FeedTime: now.Add(-42 * time.Second)})
	mfs, _ = reg.Gather()
	if v := mfs[0].GetMetric()[0].GetGauge().GetValue(); v != 42 {
		t.Errorf("age %v, want 42", v)
	}
}
