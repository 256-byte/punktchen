// Package realtime ingests the VBB GTFS-RT feed (ARCHITECTURE §3.3). The
// poller is the only place that talks to the feed (CLAUDE.md rule 7).
package realtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/256-byte/punktchen/backend/internal/config"
)

const (
	// MinRequestGap is the shortest time between two requests, whatever
	// triggers them: the provider allows 7 per minute (DATA_MODEL §7 #20);
	// 10 s keeps us at 6.
	MinRequestGap = config.MinGTFSRTPollInterval
	// DefaultTimeout bounds one request (the full feed is ~9 MB, ~2 MB gzip).
	DefaultTimeout = 20 * time.Second
	// DefaultMaxBackoff caps the wait after failed requests.
	DefaultMaxBackoff = 2 * time.Minute
	// DefaultMaxBytes caps a decompressed feed (~9 MB on 2026-11-09).
	DefaultMaxBytes = 64 << 20
)

// Feed is one changed answer of the feed, not parsed yet (task 4.2).
type Feed struct {
	Body      []byte // decompressed protobuf FeedMessage
	ETag      string
	FetchedAt time.Time
}

// Handler receives each changed feed (HTTP 200). On an error the poller
// forgets the ETag, so the next poll fetches the feed again.
type Handler func(ctx context.Context, f Feed) error

// Result of one poll, also the label of livestop_gtfsrt_poll_total.
type Result string

const (
	ResultUpdated     Result = "updated"      // 200, handed to the handler
	ResultNotModified Result = "not_modified" // 304
	ResultRejected    Result = "rejected"     // 200, the handler failed
	ResultError       Result = "error"        // network error, timeout, other status
)

// PollerConfig configures a Poller. Zero values get defaults.
type PollerConfig struct {
	URL        string        // required
	UserAgent  string        // required: VBB asks for an informative one
	Interval   time.Duration // default config.DefaultGTFSRTPollInterval; at least MinRequestGap
	Timeout    time.Duration // per request; default DefaultTimeout
	MaxBackoff time.Duration // default DefaultMaxBackoff
	MaxBytes   int64         // default DefaultMaxBytes
	Client     *http.Client  // default: a client without timeout (Timeout bounds each request)

	// now and wait replace the clock in tests.
	now  func() time.Time
	wait func(ctx context.Context, d time.Duration) error
}

// Poller fetches the feed every Interval with If-None-Match and hands
// changed feeds to a Handler. Failed requests back off exponentially up to
// MaxBackoff; the last good state is the handler's to keep. Not safe for
// concurrent use: one goroutine calls Run.
type Poller struct {
	cfg     PollerConfig
	handle  Handler
	metrics *Metrics
	log     *slog.Logger

	etag     string
	failures int       // failed polls in a row
	last     time.Time // start of the last request
	retryAt  time.Time // earliest next request after a Retry-After
}

// NewPoller checks cfg and fills in defaults.
func NewPoller(cfg PollerConfig, handle Handler, m *Metrics, log *slog.Logger) (*Poller, error) {
	switch {
	case cfg.URL == "":
		return nil, errors.New("poller: URL is required")
	case cfg.UserAgent == "":
		return nil, errors.New("poller: UserAgent is required")
	case handle == nil:
		return nil, errors.New("poller: handler is required")
	}
	if cfg.Interval == 0 {
		cfg.Interval = config.DefaultGTFSRTPollInterval
	}
	if cfg.Interval < MinRequestGap {
		return nil, fmt.Errorf("poller: interval %s below %s (provider limit)", cfg.Interval, MinRequestGap)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxBackoff < cfg.Interval {
		cfg.MaxBackoff = max(DefaultMaxBackoff, cfg.Interval)
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.wait == nil {
		cfg.wait = sleep
	}
	return &Poller{cfg: cfg, handle: handle, metrics: m, log: log}, nil
}

// Run polls until ctx is done and returns ctx.Err().
func (p *Poller) Run(ctx context.Context) error {
	for {
		_, _ = p.Poll(ctx)
		if err := p.cfg.wait(ctx, p.nextDelay()); err != nil {
			return err
		}
	}
}

// Poll sends one request (waiting first if the last one was less than
// MinRequestGap ago, or a Retry-After asks for longer) and handles the
// answer.
func (p *Poller) Poll(ctx context.Context) (Result, error) {
	if err := p.cfg.wait(ctx, p.gapLeft()); err != nil {
		return ResultError, err
	}
	p.last = p.cfg.now()
	res, err := p.fetch(ctx)
	p.metrics.polled(res)
	switch {
	case res == ResultError || res == ResultRejected:
		p.failures++
		p.log.Warn("gtfs-rt poll failed", "result", string(res), "failures", p.failures, "error", err.Error())
	case p.failures > 0:
		p.log.Info("gtfs-rt poll recovered", "result", string(res), "after_failures", p.failures)
		p.failures = 0
	}
	return res, err
}

func (p *Poller) fetch(ctx context.Context) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.URL, nil)
	if err != nil {
		return ResultError, err
	}
	req.Header.Set("User-Agent", p.cfg.UserAgent)
	if p.etag != "" {
		req.Header.Set("If-None-Match", p.etag)
	}
	// No Accept-Encoding here: net/http then asks for gzip itself and
	// decompresses the answer transparently. Note: net/http replays a GET
	// once when a reused keep-alive connection closes before the answer, so
	// a poll can rarely send two requests. MinRequestGap (6/min) leaves room
	// under the provider's 7/min for that.
	resp, err := p.cfg.Client.Do(req)
	if err != nil {
		return ResultError, fmt.Errorf("get %s: %w", p.cfg.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return ResultNotModified, nil
	case http.StatusOK:
	default:
		p.retryAt = retryAfter(resp, p.cfg.now())
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return ResultError, fmt.Errorf("get %s: %s", p.cfg.URL, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxBytes+1))
	if err != nil {
		return ResultError, fmt.Errorf("read %s: %w", p.cfg.URL, err)
	}
	if int64(len(body)) > p.cfg.MaxBytes {
		return ResultError, fmt.Errorf("get %s: feed larger than %d bytes", p.cfg.URL, p.cfg.MaxBytes)
	}
	etag := resp.Header.Get("ETag")
	if err := p.handle(ctx, Feed{Body: body, ETag: etag, FetchedAt: p.cfg.now()}); err != nil {
		p.etag = "" // fetch it again next time
		return ResultRejected, fmt.Errorf("handle feed: %w", err)
	}
	p.etag = etag
	return ResultUpdated, nil
}

// nextDelay is the time from now to the next poll: the interval, or after
// failures interval·2^failures up to MaxBackoff (15 s → 30 s → 1 min → 2 min).
func (p *Poller) nextDelay() time.Duration {
	d := p.cfg.Interval
	for i := 0; i < p.failures && d < p.cfg.MaxBackoff; i++ {
		d *= 2
	}
	return min(d, p.cfg.MaxBackoff)
}

// gapLeft is how long to wait before the next request may start.
func (p *Poller) gapLeft() time.Duration {
	now := p.cfg.now()
	earliest := p.last.Add(MinRequestGap)
	if p.retryAt.After(earliest) {
		earliest = p.retryAt
	}
	if p.last.IsZero() && p.retryAt.IsZero() {
		return 0
	}
	return max(0, earliest.Sub(now))
}

// retryAfter reads a Retry-After of seconds (429, 503); zero time if none.
func retryAfter(resp *http.Response, now time.Time) time.Time {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(secs) * time.Second)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
