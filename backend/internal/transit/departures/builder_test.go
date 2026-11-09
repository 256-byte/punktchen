package departures

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/256-byte/punktchen/backend/internal/transit/static/statictest"
)

// fakeLoader serves departures from memory, like Loader serves the live
// schedule, and records every window it was asked for.
type fakeLoader struct {
	mu         sync.Mutex
	scheduleID int64
	all        []Departure // sorted by time
	err        error
	calls      []span
}

type span struct{ from, to time.Time }

func (f *fakeLoader) Load(_ context.Context, from, to time.Time) (*Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, span{from, to})
	if f.err != nil {
		return nil, f.err
	}
	w := &Window{From: from, To: to, ScheduleID: f.scheduleID}
	for _, d := range f.all {
		if !d.Scheduled.Before(from) && d.Scheduled.Before(to) {
			w.Departures = append(w.Departures, d)
		}
	}
	return w, nil
}

// takeCalls returns the windows loaded since the last call.
func (f *fakeLoader) takeCalls() []span {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

func (f *fakeLoader) set(scheduleID int64, all []Departure, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduleID, f.all, f.err = scheduleID, all, err
}

var fakeStops = []string{"pole-a", "pole-b", "pole-c"}

// fakeSchedule: one departure every 30 s for 6 hours, round robin over the
// poles; tag makes the trips of different schedules tell apart.
func fakeSchedule(start time.Time, tag string) []Departure {
	var out []Departure
	for i := range 720 {
		out = append(out, Departure{
			TripID:    fmt.Sprintf("%s-%d", tag, i),
			StopID:    fakeStops[i%len(fakeStops)],
			RouteID:   "M10",
			Headsign:  "Gamma",
			Scheduled: start.Add(time.Duration(i) * 30 * time.Second),
		})
	}
	return out
}

// testClock is the builder's clock, safe for concurrent use.
type testClock struct{ ns atomic.Int64 }

func (c *testClock) now() time.Time          { return time.Unix(0, c.ns.Load()).In(berlin) }
func (c *testClock) set(t time.Time)         { c.ns.Store(t.UnixNano()) }
func (c *testClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func newTestBuilder(t *testing.T, f *fakeLoader, start time.Time) (*Builder, *testClock, *Metrics) {
	t.Helper()
	m := NewMetrics(prometheus.NewRegistry())
	b := NewBuilder(BuilderConfig{}, f, m, slog.New(slog.DiscardHandler))
	c := &testClock{}
	c.set(start)
	b.now = c.now
	return b, c, m
}

func refresh(t *testing.T, b *Builder) *Snapshot {
	t.Helper()
	s, err := b.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// checkSnapshot asserts the snapshot's invariants and that it holds exactly
// want.
func checkSnapshot(t *testing.T, s *Snapshot, want []Departure) {
	t.Helper()
	n := 0
	for stop, group := range s.byStop {
		n += len(group)
		if cap(group) != len(group) {
			t.Errorf("pole %s: cap %d > len %d, append by a reader would overwrite the next pole", stop, cap(group), len(group))
		}
		for i, d := range group {
			if d.StopID != stop {
				t.Errorf("pole %s holds a departure of %s", stop, d.StopID)
			}
			if d.Scheduled.Before(s.From) || !d.Scheduled.Before(s.To) {
				t.Errorf("pole %s: %s outside [%s, %s)", stop, d.Scheduled, s.From, s.To)
			}
			if i > 0 && d.Scheduled.Before(group[i-1].Scheduled) {
				t.Errorf("pole %s: not sorted by time", stop)
			}
		}
	}
	if n != s.Len() {
		t.Errorf("groups hold %d departures, Len %d", n, s.Len())
	}
	if got, want := flatten(s), keys(want); !slices.Equal(got, want) {
		t.Errorf("snapshot v%d [%s, %s): %d departures, want %d", s.Version, s.From.Format(time.TimeOnly), s.To.Format(time.TimeOnly), len(got), len(want))
	}
}

// flatten lists the snapshot's departures in a canonical order.
func flatten(s *Snapshot) []string {
	var out []string
	for _, group := range s.byStop {
		out = append(out, keys(group)...)
	}
	slices.Sort(out)
	return out
}

func keys(ds []Departure) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, fmt.Sprintf("%s|%s|%s|%s", d.Scheduled.UTC().Format(time.RFC3339), d.StopID, d.TripID, d.ServiceDate))
	}
	slices.Sort(out)
	return out
}

// inWindow is what a full load of [from, to) returns.
func inWindow(all []Departure, from, to time.Time) []Departure {
	var out []Departure
	for _, d := range all {
		if !d.Scheduled.Before(from) && d.Scheduled.Before(to) {
			out = append(out, d)
		}
	}
	return out
}

func window(now time.Time) (time.Time, time.Time) {
	return now.Add(-DefaultLookBack), now.Add(DefaultLookAhead)
}

func TestBuilderRefresh(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	sched := fakeSchedule(start, "a")
	f := &fakeLoader{scheduleID: 1, all: sched}
	b, clock, m := newTestBuilder(t, f, start.Add(time.Hour))
	ctx := context.Background()

	if b.Current() != nil {
		t.Fatal("snapshot before the first refresh")
	}

	// First refresh: the whole window.
	s1 := refresh(t, b)
	from, to := window(clock.now())
	if calls := f.takeCalls(); !slices.Equal(calls, []span{{from, to}}) {
		t.Fatalf("first refresh loaded %v, want [%s, %s)", calls, from, to)
	}
	if b.Current() != s1 || !s1.From.Equal(from) || !s1.To.Equal(to) || s1.ScheduleID != 1 {
		t.Fatalf("published %+v", s1)
	}
	checkSnapshot(t, s1, inWindow(sched, from, to))
	if s1.Len() != 184 || s1.Stops() != 3 {
		t.Errorf("Len %d Stops %d, want 184 and 3", s1.Len(), s1.Stops())
	}

	t.Run("same window publishes nothing", func(t *testing.T) {
		clock.advance(300 * time.Millisecond) // same second
		if s := refresh(t, b); s != s1 {
			t.Errorf("new snapshot v%d for the same window", s.Version)
		}
		if calls := f.takeCalls(); len(calls) != 0 {
			t.Errorf("loaded %v", calls)
		}
	})

	t.Run("moving window loads only the new part", func(t *testing.T) {
		prev := s1
		for _, step := range []time.Duration{time.Minute, 37 * time.Second, time.Second, 5 * time.Minute, 89 * time.Minute} {
			clock.advance(step)
			s := refresh(t, b)
			from, to := window(clock.now().Truncate(time.Second))
			if calls := f.takeCalls(); !slices.Equal(calls, []span{{prev.To, to}}) {
				t.Errorf("+%s: loaded %v, want only [%s, %s)", step, calls, prev.To, to)
			}
			if s.Version != prev.Version+1 {
				t.Errorf("+%s: version %d after %d", step, s.Version, prev.Version)
			}
			checkSnapshot(t, s, inWindow(sched, from, to))
			prev = s
		}
		if got := testutil.ToFloat64(m.version); got != float64(prev.Version) {
			t.Errorf("version metric %v, want %d", got, prev.Version)
		}
		if got := testutil.CollectAndCount(m.build); got != 2 { // full + incremental
			t.Errorf("build metric has %d series, want 2", got)
		}
	})

	t.Run("new schedule loads the whole window", func(t *testing.T) {
		newSched := fakeSchedule(start.Add(15*time.Second), "b")
		f.set(2, newSched, nil)
		prevTo := b.Current().To
		clock.advance(time.Minute)
		s := refresh(t, b)
		from, to := window(clock.now().Truncate(time.Second))
		if calls := f.takeCalls(); !slices.Equal(calls, []span{{prevTo, to}, {from, to}}) {
			t.Errorf("loaded %v, want the new part, then the whole window", calls)
		}
		if s.ScheduleID != 2 {
			t.Errorf("ScheduleID %d, want 2", s.ScheduleID)
		}
		checkSnapshot(t, s, inWindow(newSched, from, to))
		sched = newSched
	})

	t.Run("reload loads the whole window", func(t *testing.T) {
		clock.advance(time.Minute)
		s, kind, err := b.refresh(ctx, true)
		if err != nil || kind != loadFull {
			t.Fatalf("kind %q err %v", kind, err)
		}
		from, to := window(clock.now().Truncate(time.Second))
		if calls := f.takeCalls(); !slices.Equal(calls, []span{{from, to}}) {
			t.Errorf("loaded %v, want the whole window", calls)
		}
		checkSnapshot(t, s, inWindow(sched, from, to))
	})

	t.Run("clock jumps load the whole window", func(t *testing.T) {
		for _, jump := range []time.Duration{3 * time.Hour, -2 * time.Hour, -time.Second} {
			clock.advance(jump)
			s := refresh(t, b)
			from, to := window(clock.now().Truncate(time.Second))
			if calls := f.takeCalls(); !slices.Equal(calls, []span{{from, to}}) {
				t.Errorf("jump %s: loaded %v, want the whole window", jump, calls)
			}
			checkSnapshot(t, s, inWindow(sched, from, to))
		}
	})

	t.Run("failure keeps the last snapshot", func(t *testing.T) {
		last := b.Current()
		boom := errors.New("db down")
		f.set(2, sched, boom)
		for range 3 {
			clock.advance(time.Minute)
			if _, err := b.Refresh(ctx); !errors.Is(err, boom) {
				t.Fatalf("err %v, want %v", err, boom)
			}
		}
		if b.Current() != last {
			t.Error("a failed refresh replaced the snapshot")
		}
		if got := testutil.ToFloat64(m.failures); got != 3 {
			t.Errorf("failure metric %v, want 3", got)
		}

		// Recovery continues where the last good window ended.
		f.set(2, sched, nil)
		f.takeCalls()
		s := refresh(t, b)
		from, to := window(clock.now().Truncate(time.Second))
		if calls := f.takeCalls(); !slices.Equal(calls, []span{{last.To, to}}) {
			t.Errorf("loaded %v, want [%s, %s)", calls, last.To, to)
		}
		if s.Version != last.Version+1 {
			t.Errorf("version %d after %d", s.Version, last.Version)
		}
		checkSnapshot(t, s, inWindow(sched, from, to))
	})

	t.Run("no schedule", func(t *testing.T) {
		empty := &fakeLoader{err: ErrNoSchedule}
		b, _, _ := newTestBuilder(t, empty, start)
		if _, err := b.Refresh(ctx); !errors.Is(err, ErrNoSchedule) {
			t.Errorf("err %v, want ErrNoSchedule", err)
		}
		if b.Current() != nil {
			t.Error("snapshot without a schedule")
		}
	})
}

// TestBuilderVersions: versions start at the Unix time in ms, so a new
// process continues above an older one.
func TestBuilderVersions(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	f := &fakeLoader{scheduleID: 1, all: fakeSchedule(start, "a")}

	before := uint64(time.Now().UnixMilli())
	old, clock, _ := newTestBuilder(t, f, start.Add(time.Hour))
	var last uint64
	for range 5 {
		clock.advance(time.Minute)
		last = refresh(t, old).Version
	}
	if last <= before {
		t.Errorf("version %d not above the start time %d ms", last, before)
	}

	time.Sleep(10 * time.Millisecond) // the restart: > 5 snapshots later in ms
	restarted, _, _ := newTestBuilder(t, f, start.Add(time.Hour))
	if v := refresh(t, restarted).Version; v <= last {
		t.Errorf("after a restart version %d, not above %d", v, last)
	}
}

// TestSnapshotImmutable: a published snapshot never changes, whatever the
// builder does later, and a reader appending to a pole's slice cannot reach
// another pole's departures.
func TestSnapshotImmutable(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	sched := fakeSchedule(start, "a")
	f := &fakeLoader{scheduleID: 1, all: sched}
	b, clock, _ := newTestBuilder(t, f, start.Add(time.Hour))

	s := refresh(t, b)
	want := flatten(s)

	for i := range 20 {
		clock.advance(time.Minute)
		if i == 10 {
			f.set(2, fakeSchedule(start, "b"), nil)
		}
		refresh(t, b)
	}
	if !slices.Equal(flatten(s), want) {
		t.Error("an old snapshot changed after later refreshes")
	}

	for _, stop := range fakeStops {
		_ = append(s.Departures(stop), Departure{TripID: "intruder", StopID: stop})
	}
	if !slices.Equal(flatten(s), want) {
		t.Error("append to one pole's departures changed the snapshot")
	}
	if s.Departures("unknown") != nil {
		t.Error("departures for an unknown pole")
	}
}

// TestBuilderConcurrentReads is the PLAN 2.3 criterion: readers use
// snapshots while the builder publishes new ones. Run with -race.
func TestBuilderConcurrentReads(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	f := &fakeLoader{scheduleID: 1, all: fakeSchedule(start, "a")}
	b, clock, _ := newTestBuilder(t, f, start.Add(time.Hour))

	done := make(chan struct{})
	var wg sync.WaitGroup
	var reads atomic.Int64
	for range 8 {
		wg.Go(func() {
			var last uint64
			for {
				select {
				case <-done:
					return
				default:
				}
				s := b.Current()
				if s == nil {
					continue
				}
				if s.Version < last {
					t.Errorf("version went back from %d to %d", last, s.Version)
					return
				}
				last = s.Version
				n := 0
				for _, stop := range fakeStops {
					for _, d := range s.Departures(stop) {
						if d.StopID != stop || d.Scheduled.Before(s.From) || !d.Scheduled.Before(s.To) {
							t.Errorf("v%d: bad departure %+v", s.Version, d)
							return
						}
						n++
					}
				}
				if n != s.Len() {
					t.Errorf("v%d: read %d departures, Len %d", s.Version, n, s.Len())
					return
				}
				reads.Add(1)
			}
		})
	}

	for i := range 300 {
		clock.advance(time.Duration(1+i%7) * 11 * time.Second)
		if i%50 == 49 {
			f.set(int64(i), fakeSchedule(start, fmt.Sprint(i)), nil)
		}
		if _, err := b.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	if reads.Load() == 0 {
		t.Error("readers never saw a snapshot")
	}
}

// TestBuilderRun: Run publishes at once, follows the ticker and Reload, and
// stops with its context.
func TestBuilderRun(t *testing.T) {
	start := at(t, "2026-11-10T08:00:00+01:00")
	sched := fakeSchedule(start, "a")
	f := &fakeLoader{scheduleID: 1, all: sched}
	b := NewBuilder(BuilderConfig{Every: time.Hour}, f, nil, slog.New(slog.DiscardHandler))
	clock := &testClock{}
	clock.set(start.Add(time.Hour))
	b.now = clock.now

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		b.Run(ctx)
	}()

	first := waitFor(t, "first snapshot", func() *Snapshot { return b.Current() })
	clock.advance(time.Minute)
	f.set(2, sched, nil) // a new schedule, as after an import
	b.Reload()
	waitFor(t, "snapshot after Reload", func() *Snapshot {
		if s := b.Current(); s != first {
			return s
		}
		return nil
	})
	from, to := window(clock.now())
	if calls := f.takeCalls(); len(calls) != 2 || calls[1] != (span{from, to}) {
		t.Errorf("loaded %v, want the whole window twice", calls)
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func waitFor(t *testing.T, what string, get func() *Snapshot) *Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := get(); s != nil {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
	return nil
}

// TestBuilderWedding: on real data, a window moved minute by minute across
// midnight (and so across service days) holds exactly what a full load of
// the same window holds. A new import makes the builder load everything from
// the new schedule.
func TestBuilderWedding(t *testing.T) {
	pool := statictest.NewDB(t)
	zip := filepath.Join("..", "..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip")
	statictest.LoadZip(t, pool, zip)
	l := NewLoader(pool)
	b := NewBuilder(BuilderConfig{}, l, nil, slog.New(slog.DiscardHandler))
	clock := &testClock{}
	clock.set(at(t, "2026-11-10T23:30:00+01:00"))
	b.now = clock.now
	ctx := context.Background()

	check := func(s *Snapshot) {
		t.Helper()
		full, err := l.Load(ctx, s.From, s.To)
		if err != nil {
			t.Fatal(err)
		}
		if full.ScheduleID != s.ScheduleID {
			t.Fatalf("schedule %d, snapshot %d", full.ScheduleID, s.ScheduleID)
		}
		checkSnapshot(t, s, full.Departures)
	}

	s := refresh(t, b)
	check(s)
	first := s
	for i := range 75 { // to 00:45 + odd steps
		step := time.Minute
		if i%10 == 9 {
			step = 47 * time.Second
		}
		clock.advance(step)
		s = refresh(t, b)
		check(s)
	}
	if s.Len() == 0 || first.Len() == 0 {
		t.Fatalf("empty snapshots: %d at 23:30, %d at the end", first.Len(), s.Len())
	}
	t.Logf("23:30 window: %d departures at %d poles; %s window: %d at %d poles",
		first.Len(), first.Stops(), s.From.Add(DefaultLookBack).Format("15:04:05"), s.Len(), s.Stops())

	statictest.LoadZip(t, pool, zip) // a new import: new schedule id
	clock.advance(time.Minute)
	s2 := refresh(t, b)
	if s2.ScheduleID == s.ScheduleID {
		t.Fatal("snapshot still on the old schedule after an import")
	}
	check(s2)
}
