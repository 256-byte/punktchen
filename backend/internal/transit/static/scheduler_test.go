package static

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

func TestNextDaily(t *testing.T) {
	berlin := platform.Berlin
	at := func(s string) time.Time {
		t.Helper()
		v, err := time.ParseInLocation("2006-01-02 15:04", s, berlin)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct{ now, want string }{
		{"2026-11-09 01:00", "2026-11-09 03:30"}, // later today
		{"2026-11-09 03:30", "2026-11-10 03:30"}, // exactly now → tomorrow
		{"2026-11-09 18:00", "2026-11-10 03:30"},
		{"2027-03-27 12:00", "2027-03-28 03:30"}, // DST starts 2027-03-28 02:00
		{"2026-10-24 12:00", "2026-10-25 03:30"}, // DST ends 2026-10-25 03:00
		{"2026-12-31 23:00", "2027-01-01 03:30"},
	}
	for _, c := range cases {
		got := nextDaily(at(c.now), 3, 30, berlin)
		if !got.Equal(at(c.want)) || got.In(berlin).Format("15:04") != "03:30" {
			t.Errorf("nextDaily(%s) = %s, want %s", c.now, got.In(berlin), c.want)
		}
	}
}

// schedTest drives a Scheduler with a fake clock: each call of after() is
// reported on waits, and the test fires timers through fire.
type schedTest struct {
	t     *testing.T
	s     *Scheduler
	now   time.Time
	waits chan time.Duration
	fire  chan time.Time
	runs  chan chan error // each run waits for the test to answer
	done  chan struct{}
	stop  context.CancelFunc
	reg   *prometheus.Registry
}

func newSchedTest(t *testing.T, hasData bool) *schedTest {
	st := &schedTest{
		t: t, now: time.Date(2026, 11, 9, 12, 0, 0, 0, platform.Berlin),
		waits: make(chan time.Duration, 10), fire: make(chan time.Time),
		runs: make(chan chan error), done: make(chan struct{}), reg: prometheus.NewRegistry(),
	}
	run := func(ctx context.Context) (*Outcome, error) {
		answer := make(chan error)
		select {
		case st.runs <- answer:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := <-answer; err != nil {
			return nil, err
		}
		return &Outcome{ImportID: 1, Changed: true}, nil
	}
	st.s = NewScheduler(SchedulerConfig{Hour: 3, Minute: 30, Loc: platform.Berlin}, run,
		func(context.Context) (bool, error) { return hasData, nil }, NewMetrics(st.reg), slog.New(slog.DiscardHandler))
	st.s.now = func() time.Time { return st.now }
	st.s.after = func(d time.Duration) <-chan time.Time { st.waits <- d; return st.fire }
	ctx, cancel := context.WithCancel(context.Background())
	st.stop = cancel
	go func() { defer close(st.done); st.s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-st.done })
	return st
}

// expectRun waits for the next run and lets it end with err.
func (st *schedTest) expectRun(err error) {
	st.t.Helper()
	select {
	case answer := <-st.runs:
		answer <- err
	case <-time.After(5 * time.Second):
		st.t.Fatal("no import run")
	}
}

// expectWait returns the next timer duration the scheduler asks for.
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

func (st *schedTest) count(trigger Trigger, result string) float64 {
	return testutil.ToFloat64(st.s.metrics.runs.WithLabelValues(string(trigger), result))
}

func TestSchedulerImportsAtStartupWithoutData(t *testing.T) {
	st := newSchedTest(t, false)
	st.expectRun(nil)
	if d := st.expectWait(); d != 15*time.Hour+30*time.Minute { // 12:00 → 03:30
		t.Fatalf("wait %s after startup import, want 15h30m", d)
	}
	if st.count(TriggerStartup, resultSucceeded) != 1 {
		t.Fatal("startup run not counted")
	}
}

func TestSchedulerWaitsWithData(t *testing.T) {
	st := newSchedTest(t, true)
	if d := st.expectWait(); d != 15*time.Hour+30*time.Minute {
		t.Fatalf("wait %s, want the daily slot", d)
	}
	select {
	case <-st.runs:
		t.Fatal("imported at startup although data exists")
	default:
	}
	st.fire <- st.now // the daily slot
	st.expectRun(nil)
	st.expectWait()
	if st.count(TriggerScheduled, resultSucceeded) != 1 {
		t.Fatal("scheduled run not counted")
	}
	if testutil.ToFloat64(st.s.metrics.lastSuccess) == 0 {
		t.Fatal("last success timestamp not set")
	}
}

func TestSchedulerRetriesAfterFailure(t *testing.T) {
	st := newSchedTest(t, false)
	st.expectRun(errors.New("upstream down"))
	if d := st.expectWait(); d != DefaultRetryAfter {
		t.Fatalf("wait %s after failure, want %s", d, DefaultRetryAfter)
	}
	if st.count(TriggerStartup, resultFailed) != 1 {
		t.Fatal("failed run not counted")
	}
	// A retry never goes past the daily slot: at 03:00, retry 04:00 > 03:30.
	if next, why := st.s.next(time.Date(2026, 11, 10, 3, 0, 0, 0, platform.Berlin), true); why != TriggerScheduled || next.Format("15:04") != "03:30" {
		t.Fatalf("next = %s %s, want the 03:30 daily run", next, why)
	}
}

func TestSchedulerBusyIsNotAFailure(t *testing.T) {
	st := newSchedTest(t, false)
	st.expectRun(ErrImportRunning)
	if d := st.expectWait(); d == DefaultRetryAfter {
		t.Fatal("busy run scheduled a retry")
	}
	if st.count(TriggerStartup, resultBusy) != 1 {
		t.Fatal("busy run not counted")
	}
}

func TestSchedulerManualTrigger(t *testing.T) {
	st := newSchedTest(t, true)
	st.expectWait()
	if err := st.s.Trigger(); err != nil {
		t.Fatal(err)
	}
	answer := <-st.runs // the manual run is in progress now
	if err := st.s.Trigger(); !errors.Is(err, ErrImportRunning) {
		t.Fatalf("Trigger during a run: %v, want ErrImportRunning", err)
	}
	answer <- nil
	st.expectWait()
	if st.count(TriggerManual, resultSucceeded) != 1 {
		t.Fatal("manual run not counted")
	}
}

func TestSchedulerStopsWithContext(t *testing.T) {
	st := newSchedTest(t, true)
	st.expectWait()
	st.stop()
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
