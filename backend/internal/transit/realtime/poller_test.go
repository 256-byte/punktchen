package realtime

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const ua = "livestop/test (+https://github.com/256-byte/punktchen)"

// clock is a fake clock: wait advances it instead of sleeping and records
// each wait; after stopAfter waits it cancels the context.
type clock struct {
	mu        sync.Mutex
	now       time.Time
	waits     []time.Duration
	stopAfter int
	cancel    context.CancelFunc
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.waits = append(c.waits, d)
		c.now = c.now.Add(d)
	}
	if c.cancel != nil && len(c.waits) >= c.stopAfter {
		c.cancel()
	}
	return ctx.Err()
}

// server answers requests in order with the given handlers (the last one
// repeats) and records each request.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func newServer(t *testing.T, answers ...http.HandlerFunc) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		i := len(s.requests)
		s.requests = append(s.requests, r)
		s.mu.Unlock()
		answers[min(i, len(answers)-1)](w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) request(i int) *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[i]
}

func feed(etag, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		_, _ = w.Write([]byte(body))
	}
}

func status(code int, header ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.WriteHeader(code)
	}
}

func notModified(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotModified) }

type sink struct {
	mu    sync.Mutex
	feeds []Feed
	err   error
}

func (s *sink) handle(_ context.Context, f Feed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeds = append(s.feeds, f)
	return s.err
}

func newPoller(t *testing.T, url string, h Handler, c *clock, mod func(*PollerConfig)) (*Poller, *prometheus.Registry) {
	t.Helper()
	cfg := PollerConfig{URL: url, UserAgent: ua, now: c.Now, wait: c.Wait}
	if mod != nil {
		mod(&cfg)
	}
	reg := prometheus.NewRegistry()
	p, err := NewPoller(cfg, h, NewMetrics(reg), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return p, reg
}

func t0() *clock { return &clock{now: time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC)} }

func polls(t *testing.T, reg *prometheus.Registry, r Result) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "livestop_gtfsrt_poll_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			for _, l := range metric.GetLabel() {
				if l.GetName() == "result" && l.GetValue() == string(r) {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("no poll_total{result=%q}", r)
	return 0
}

func TestPollNotModified(t *testing.T) {
	srv := newServer(t, feed(`"v1"`, "feed-1"), notModified, feed(`"v2"`, "feed-2"))
	var s sink
	c := t0()
	p, reg := newPoller(t, srv.URL, s.handle, c, nil)
	ctx := context.Background()

	for i, want := range []Result{ResultUpdated, ResultNotModified, ResultUpdated} {
		if got, err := p.Poll(ctx); got != want || err != nil {
			t.Fatalf("poll %d: %s, %v; want %s", i+1, got, err, want)
		}
	}
	if r := srv.request(0); r.Header.Get("If-None-Match") != "" || r.Header.Get("User-Agent") != ua {
		t.Errorf("first request: If-None-Match %q, User-Agent %q", r.Header.Get("If-None-Match"), r.Header.Get("User-Agent"))
	}
	for i := 1; i <= 2; i++ {
		if got := srv.request(i).Header.Get("If-None-Match"); got != `"v1"` {
			t.Errorf("request %d: If-None-Match %q, want the kept ETag", i+1, got)
		}
	}
	if len(s.feeds) != 2 || string(s.feeds[0].Body) != "feed-1" || string(s.feeds[1].Body) != "feed-2" || s.feeds[1].ETag != `"v2"` {
		t.Errorf("handled %+v; want feed-1 and feed-2, not the 304", s.feeds)
	}
	// Polls right after each other wait for the gap the provider allows.
	if want := []time.Duration{MinRequestGap, MinRequestGap}; !slices.Equal(c.waits, want) {
		t.Errorf("waits %v, want %v", c.waits, want)
	}
	if polls(t, reg, ResultUpdated) != 2 || polls(t, reg, ResultNotModified) != 1 || polls(t, reg, ResultError) != 0 {
		t.Errorf("metrics: updated %v, not_modified %v", polls(t, reg, ResultUpdated), polls(t, reg, ResultNotModified))
	}
}

func TestPollGzip(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			http.Error(w, "gzip expected", http.StatusBadRequest)
			return
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(strings.Repeat("protobuf", 1000)))
		_ = zw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	})
	var s sink
	p, _ := newPoller(t, srv.URL, s.handle, t0(), nil)
	if got, err := p.Poll(context.Background()); got != ResultUpdated || err != nil {
		t.Fatalf("%s, %v", got, err)
	}
	if string(s.feeds[0].Body) != strings.Repeat("protobuf", 1000) {
		t.Errorf("body not decompressed: %d bytes", len(s.feeds[0].Body))
	}
}

// TestRunBacksOff is the error path of task 4.1: failed polls wait
// 15 s → 30 s → 1 min → 2 min (cap), a success resets to 15 s, and the
// handler keeps its last good feed meanwhile.
func TestRunBacksOff(t *testing.T) {
	srv := newServer(t,
		feed(`"v1"`, "good"),
		status(http.StatusInternalServerError),
		status(http.StatusBadGateway),
		// Not a dropped connection: net/http replays a GET once when a reused
		// connection closes before the answer, which would be a second request.
		status(http.StatusGatewayTimeout),
		status(http.StatusServiceUnavailable),
		status(http.StatusServiceUnavailable),
		notModified,
	)
	var s sink
	c := t0()
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.stopAfter = cancel, 7
	p, reg := newPoller(t, srv.URL, s.handle, c, nil)

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 2 * time.Minute, 2 * time.Minute, 15 * time.Second}
	if !slices.Equal(c.waits, want) {
		t.Errorf("waits %v\n want %v", c.waits, want)
	}
	if len(s.feeds) != 1 || string(s.feeds[0].Body) != "good" {
		t.Errorf("handled %d feeds; errors must not reach the handler", len(s.feeds))
	}
	if got := srv.request(6).Header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("after errors If-None-Match %q, want the kept ETag", got)
	}
	if polls(t, reg, ResultError) != 5 || polls(t, reg, ResultNotModified) != 1 {
		t.Errorf("metrics: error %v, not_modified %v", polls(t, reg, ResultError), polls(t, reg, ResultNotModified))
	}
}

func TestPollRetryAfter(t *testing.T) {
	srv := newServer(t, status(http.StatusTooManyRequests, "Retry-After", "45"), notModified)
	c := t0()
	p, _ := newPoller(t, srv.URL, (&sink{}).handle, c, nil)
	ctx := context.Background()
	if got, _ := p.Poll(ctx); got != ResultError {
		t.Fatalf("429: %s", got)
	}
	if got, _ := p.Poll(ctx); got != ResultNotModified {
		t.Fatalf("after 429: %s", got)
	}
	if !slices.Equal(c.waits, []time.Duration{45 * time.Second}) {
		t.Errorf("waits %v, want the 45 s of Retry-After", c.waits)
	}
}

func TestPollHandlerError(t *testing.T) {
	srv := newServer(t, feed(`"v1"`, "broken"), feed(`"v1"`, "broken"))
	s := sink{err: errors.New("not a FeedMessage")}
	p, reg := newPoller(t, srv.URL, s.handle, t0(), nil)
	ctx := context.Background()
	if got, err := p.Poll(ctx); got != ResultRejected || err == nil {
		t.Fatalf("%s, %v", got, err)
	}
	_, _ = p.Poll(ctx)
	if got := srv.request(1).Header.Get("If-None-Match"); got != "" {
		t.Errorf("If-None-Match %q after a rejected feed; it must be fetched again", got)
	}
	if polls(t, reg, ResultRejected) != 2 {
		t.Errorf("rejected %v", polls(t, reg, ResultRejected))
	}
	if p.nextDelay() != 4*15*time.Second {
		t.Errorf("next delay %s after 2 rejected feeds", p.nextDelay())
	}
}

func TestPollLimits(t *testing.T) {
	t.Run("too large", func(t *testing.T) {
		srv := newServer(t, feed("", strings.Repeat("x", 101)))
		p, _ := newPoller(t, srv.URL, (&sink{}).handle, t0(), func(c *PollerConfig) { c.MaxBytes = 100 })
		if got, err := p.Poll(context.Background()); got != ResultError || err == nil || !strings.Contains(err.Error(), "larger than 100 bytes") {
			t.Errorf("%s, %v", got, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		defer close(release)
		p, _ := newPoller(t, srv.URL, (&sink{}).handle, t0(), func(c *PollerConfig) { c.Timeout = 50 * time.Millisecond })
		if got, err := p.Poll(context.Background()); got != ResultError || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s, %v", got, err)
		}
	})
}

func TestNewPoller(t *testing.T) {
	h := (&sink{}).handle
	log := slog.New(slog.DiscardHandler)
	for name, cfg := range map[string]PollerConfig{
		"no URL":        {UserAgent: ua},
		"no user agent": {URL: "http://x"},
		"too often":     {URL: "http://x", UserAgent: ua, Interval: 5 * time.Second},
	} {
		if _, err := NewPoller(cfg, h, nil, log); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := NewPoller(PollerConfig{URL: "http://x", UserAgent: ua}, nil, nil, log); err == nil {
		t.Error("no handler: no error")
	}
	p, err := NewPoller(PollerConfig{URL: "http://x", UserAgent: ua}, h, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.Interval != 15*time.Second || p.cfg.MaxBackoff != 2*time.Minute || p.cfg.Timeout != DefaultTimeout {
		t.Errorf("defaults %+v", p.cfg)
	}
	// 60 s / 10 s = 6 requests per minute at most, under the provider's 7.
	if time.Minute/MinRequestGap > 7 {
		t.Errorf("MinRequestGap %s allows more than 7 requests per minute", MinRequestGap)
	}
}

func TestSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if err := sleep(ctx, time.Millisecond); err != nil {
		t.Errorf("sleep: %v", err)
	}
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleep after cancel: %v", err)
	}
	if err := sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("sleep 0 after cancel: %v", err)
	}
}
