package amenities

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

// fakeJob records its runs; the test decides emptiness and errors.
type fakeJob struct {
	source string
	mu     sync.Mutex
	empty  bool
	err    error
	runs   []bool // force flag of each run
}

func (j *fakeJob) Source() string { return j.source }

func (j *fakeJob) Empty(context.Context) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.empty, nil
}

func (j *fakeJob) Run(_ context.Context, force bool) (Result, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.runs = append(j.runs, force)
	if j.err != nil {
		return Result{}, j.err
	}
	j.empty = false
	return Result{ImportID: int64(len(j.runs)), Changed: true, Rows: 42}, nil
}

func (j *fakeJob) forces() []bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]bool(nil), j.runs...)
}

type schedTest struct {
	t      *testing.T
	s      *Scheduler
	now    time.Time
	waits  chan time.Duration
	fire   chan time.Time
	done   chan struct{}
	stop   context.CancelFunc
	reg    *prometheus.Registry
	toilet *fakeJob
	water  *fakeJob
}

// newSchedTest starts a scheduler at 12:00 Berlin with a daily slot at 04:15.
func newSchedTest(t *testing.T, toilet, water *fakeJob) *schedTest {
	st := &schedTest{
		t: t, now: time.Date(2026, 11, 18, 12, 0, 0, 0, platform.Berlin),
		waits: make(chan time.Duration, 10), fire: make(chan time.Time),
		done: make(chan struct{}), reg: prometheus.NewRegistry(), toilet: toilet, water: water,
	}
	st.s = NewScheduler(SchedulerConfig{Hour: 4, Minute: 15, Loc: platform.Berlin},
		[]Job{toilet, water}, NewMetrics(st.reg), slog.New(slog.DiscardHandler))
	st.s.now = func() time.Time { return st.now }
	st.s.after = func(d time.Duration) <-chan time.Time { st.waits <- d; return st.fire }
	ctx, cancel := context.WithCancel(context.Background())
	st.stop = cancel
	go func() { defer close(st.done); st.s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-st.done })
	return st
}

func (st *schedTest) expectWait() time.Duration {
	st.t.Helper()
	select {
	case d := <-st.waits:
		return d
	case <-time.After(5 * time.Second):
		st.t.Fatal("scheduler is not waiting")
		return 0
	}
}

func (st *schedTest) count(source string, why Trigger, result string) float64 {
	return testutil.ToFloat64(st.s.metrics.runs.WithLabelValues(source, string(why), result))
}

const toDaily = 16*time.Hour + 15*time.Minute // 12:00 → 04:15

func TestSchedulerImportsEmptyDatasetsAtStartup(t *testing.T) {
	toilet, water := &fakeJob{source: SourceToilets, empty: true}, &fakeJob{source: SourceFountains}
	st := newSchedTest(t, toilet, water)
	if d := st.expectWait(); d != toDaily {
		t.Fatalf("wait %s, want %s", d, toDaily)
	}
	// Only the empty dataset ran, forced; the other one waits for the daily run.
	if got := toilet.forces(); len(got) != 1 || !got[0] {
		t.Fatalf("toilet runs %v, want one forced run", got)
	}
	if got := water.forces(); len(got) != 0 {
		t.Fatalf("fountain runs %v, want none at startup", got)
	}
	if st.count(SourceToilets, TriggerStartup, resultSucceeded) != 1 ||
		testutil.ToFloat64(st.s.metrics.rows.WithLabelValues(SourceToilets)) != 42 {
		t.Fatal("startup run not in the metrics")
	}

	// The daily slot runs both, not forced (they have rows now).
	st.fire <- st.now
	st.expectWait()
	if got := toilet.forces(); len(got) != 2 || got[1] {
		t.Fatalf("toilet runs %v, want a second, unforced run", got)
	}
	if got := water.forces(); len(got) != 1 || got[0] {
		t.Fatalf("fountain runs %v, want one unforced run", got)
	}
	if st.count(SourceFountains, TriggerScheduled, resultSucceeded) != 1 ||
		testutil.ToFloat64(st.s.metrics.lastSuccess.WithLabelValues(SourceFountains)) == 0 {
		t.Fatal("scheduled run not in the metrics")
	}
}

func TestSchedulerRetriesAfterFailure(t *testing.T) {
	toilet := &fakeJob{source: SourceToilets, empty: true, err: errors.New("gdi.berlin.de down")}
	water := &fakeJob{source: SourceFountains, empty: true}
	st := newSchedTest(t, toilet, water)
	// One failed dataset does not stop the others.
	if d := st.expectWait(); d != DefaultRetryAfter {
		t.Fatalf("wait %s after a failure, want %s", d, DefaultRetryAfter)
	}
	if len(water.forces()) != 1 || st.count(SourceToilets, TriggerStartup, resultFailed) != 1 {
		t.Fatal("fountains not imported or failure not counted")
	}
	toilet.mu.Lock()
	toilet.err = nil
	toilet.mu.Unlock()
	st.fire <- st.now // the retry
	if d := st.expectWait(); d != toDaily {
		t.Fatalf("wait %s after a good retry, want the daily slot", d)
	}
	if st.count(SourceToilets, TriggerRetry, resultSucceeded) != 1 {
		t.Fatal("retry not counted")
	}
	// The empty toilet table is still forced on the retry.
	if got := toilet.forces(); len(got) != 2 || !got[1] {
		t.Fatalf("toilet runs %v, want a forced retry", got)
	}
}

func TestSchedulerManualTrigger(t *testing.T) {
	toilet, water := &fakeJob{source: SourceToilets}, &fakeJob{source: SourceFountains}
	st := newSchedTest(t, toilet, water)
	st.expectWait()
	if !st.s.Trigger() {
		t.Fatal("Trigger refused")
	}
	st.expectWait()
	if len(toilet.forces()) != 1 || len(water.forces()) != 1 || st.count(SourceToilets, TriggerManual, resultSucceeded) != 1 {
		t.Fatal("manual run did not run both datasets")
	}
}

func TestSchedulerStopsWithContext(t *testing.T) {
	st := newSchedTest(t, &fakeJob{source: SourceToilets}, &fakeJob{source: SourceFountains})
	st.expectWait()
	st.stop()
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
