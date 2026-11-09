package departures

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// fakeRealtime delays one trip and reports a feed time; it counts calls and
// can be switched to another delay.
type fakeRealtime struct {
	trip     string
	delay    atomic.Int32
	feedTime time.Time
	calls    atomic.Int32
}

func (f *fakeRealtime) merge(deps []Departure, _ time.Time) ([]Departure, time.Time) {
	f.calls.Add(1)
	out := slices.Clone(deps)
	for i := range out {
		if out[i].TripID == f.trip {
			out[i].Delay, out[i].Realtime = f.delay.Load(), true
		}
	}
	return out, f.feedTime
}

func TestBuilderRealtime(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	f := &fakeLoader{scheduleID: 1, all: fakeSchedule(start, "a")}
	rt := &fakeRealtime{trip: "a-120", feedTime: start.Add(time.Hour - 20*time.Second)}
	rt.delay.Store(150) // a-120 at 09:00:00 on pole fakeStops[0] → 09:02:30
	b := NewBuilder(BuilderConfig{Realtime: rt.merge}, f, NewMetrics(prometheus.NewRegistry()), slog.New(slog.DiscardHandler))
	c := &testClock{}
	c.set(start.Add(time.Hour))
	b.now = c.now

	if b.Republish() != nil {
		t.Fatal("republished before the first refresh")
	}
	s1 := refresh(t, b)
	_ = f.takeCalls()
	if !s1.RealtimeFeedTime.Equal(rt.feedTime) {
		t.Errorf("feed time %s, want %s", s1.RealtimeFeedTime, rt.feedTime)
	}

	// Pole fakeStops[0] has every third trip: a-120 09:00, a-123 09:01:30,
	// a-126 09:03. The late a-120 (09:02:30) now leaves after a-123.
	trips := func(s *Snapshot) []string {
		var out []string
		for _, d := range s.Departures(fakeStops[0]) {
			if !d.Scheduled.Before(start.Add(59*time.Minute)) && d.Scheduled.Before(start.Add(64*time.Minute)) {
				out = append(out, d.TripID)
			}
		}
		return out
	}
	if got, want := trips(s1), []string{"a-123", "a-120", "a-126"}; !slices.Equal(got, want) {
		t.Errorf("pole order %v, want %v (by predicted time)", got, want)
	}

	// New realtime data: same window again, no database load.
	rt.delay.Store(0)
	s2 := b.Republish()
	if s2.Version != s1.Version+1 || !s2.From.Equal(s1.From) || !s2.To.Equal(s1.To) || b.Current() != s2 {
		t.Fatalf("republished v%d [%s, %s), after v%d", s2.Version, s2.From, s2.To, s1.Version)
	}
	if calls := f.takeCalls(); len(calls) != 0 {
		t.Errorf("republish loaded %v", calls)
	}
	if got, want := trips(s2), []string{"a-120", "a-123", "a-126"}; !slices.Equal(got, want) {
		t.Errorf("pole order %v, want %v", got, want)
	}
	if !slices.Equal(trips(s1), []string{"a-123", "a-120", "a-126"}) {
		t.Error("an older snapshot changed")
	}
}

func TestBuilderRunRealtimeChanged(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	f := &fakeLoader{scheduleID: 1, all: fakeSchedule(start, "a")}
	rt := &fakeRealtime{trip: "a-1"}
	b := NewBuilder(BuilderConfig{Realtime: rt.merge, Every: time.Hour}, f, nil, slog.New(slog.DiscardHandler))
	c := &testClock{}
	c.set(start.Add(time.Hour))
	b.now = c.now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	first := waitFor(t, "first snapshot", b.Current).Version
	b.RealtimeChanged()
	b.RealtimeChanged() // coalesces with the pending one
	waitFor(t, "republished snapshot", func() *Snapshot {
		if s := b.Current(); s.Version > first {
			return s
		}
		return nil
	})
	if loads := len(f.takeCalls()); loads != 1 {
		t.Errorf("%d loads, want only the first refresh's", loads)
	}
}
