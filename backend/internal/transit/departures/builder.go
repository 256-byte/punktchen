package departures

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults of the departures window (ARCHITECTURE §3.2).
const (
	// DefaultLookBack keeps departures this long after their scheduled time,
	// so a late vehicle stays listed until it leaves: realtime delays went up
	// to 42 min in the recorded feeds (DATA_MODEL §7 #18). Readers drop the
	// ones that left by predicted time.
	DefaultLookBack     = 30 * time.Minute
	DefaultLookAhead    = 90 * time.Minute // load departures this far ahead
	DefaultRefreshEvery = time.Minute
)

// How a refresh got its departures (metric label).
const (
	loadFull        = "full"        // whole window from the database
	loadIncremental = "incremental" // only the new part of the window
	loadRealtime    = "realtime"    // same window, new realtime data merged
)

// RealtimeFunc merges realtime data into the departures of a window at time
// now (realtime.Store.Merger). It returns the departures to publish (deps
// itself when there is nothing to merge; deps must not be changed) and the
// header.timestamp of the feed used, zero if none.
type RealtimeFunc func(deps []Departure, now time.Time) ([]Departure, time.Time)

// BuilderConfig sets the window [now − LookBack, now + LookAhead) and how
// often it moves. Zero values take the defaults.
type BuilderConfig struct {
	LookBack, LookAhead, Every time.Duration
	// Now is the clock (default time.Now); tests fix it.
	Now func() time.Time
	// Realtime merges realtime data into every snapshot; nil: schedule only.
	Realtime RealtimeFunc
}

// WindowLoader loads the departures of a time window (*Loader).
type WindowLoader interface {
	Load(ctx context.Context, from, to time.Time) (*Window, error)
}

// Builder keeps the departures window current and publishes it as a
// Snapshot (ARCHITECTURE §3.4, ADR-0005).
//
// Each refresh moves the window to now: it drops departures that fell out at
// the start and loads only the new part at the end. A full load happens at
// the start, after an import (Reload, or a changed ScheduleID), and when the
// clock jumped. Realtime data is merged into every snapshot; after new
// realtime data (RealtimeChanged) the same window is published again with it,
// without touching the database. A failed refresh keeps the last snapshot.
//
// Readers call Current at any time from any goroutine; it never blocks.
type Builder struct {
	cfg     BuilderConfig
	loader  WindowLoader
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time

	current  atomic.Pointer[Snapshot]
	reload   chan struct{}
	realtime chan struct{}

	mu      sync.Mutex // one refresh at a time; guards the fields below
	version uint64     // of the last published snapshot
	win     *Window    // the departures of current, sorted by time
}

// NewBuilder returns a Builder; call Run to keep it current.
//
// Versions start at the Unix time in milliseconds and count up by one per
// snapshot. So they also increase across restarts (a new process starts
// above the old one's versions unless that published more than one snapshot
// per millisecond), and an ETag from before a restart cannot match new data.
func NewBuilder(cfg BuilderConfig, loader WindowLoader, m *Metrics, log *slog.Logger) *Builder {
	if cfg.LookBack <= 0 {
		cfg.LookBack = DefaultLookBack
	}
	if cfg.LookAhead <= 0 {
		cfg.LookAhead = DefaultLookAhead
	}
	if cfg.Every <= 0 {
		cfg.Every = DefaultRefreshEvery
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Builder{
		cfg: cfg, loader: loader, metrics: m, log: log,
		now:      cfg.Now,
		reload:   make(chan struct{}, 1),
		realtime: make(chan struct{}, 1),
		version:  uint64(time.Now().UnixMilli()),
	}
}

// Current returns the latest snapshot, or nil before the first one.
func (b *Builder) Current() *Snapshot {
	return b.current.Load()
}

// Reload asks Run to load the whole window again soon, e.g. after an import
// swapped the schedule. It never blocks.
func (b *Builder) Reload() {
	select {
	case b.reload <- struct{}{}:
	default: // one is pending already
	}
}

// RealtimeChanged asks Run to publish the current window again with the
// latest realtime data. It never blocks; the realtime store calls it after
// each applied feed.
func (b *Builder) RealtimeChanged() {
	select {
	case b.realtime <- struct{}{}:
	default: // one is pending already
	}
}

// Run refreshes now and then every cfg.Every, on Reload, and on
// RealtimeChanged, until ctx is done.
func (b *Builder) Run(ctx context.Context) {
	b.refreshLogged(ctx, false)
	t := time.NewTicker(b.cfg.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.refreshLogged(ctx, false)
		case <-b.reload:
			b.refreshLogged(ctx, true)
		case <-b.realtime:
			b.republishLogged()
		}
	}
}

// Refresh moves the window to now and publishes the result. If the window
// has not moved since the last refresh, it returns the current snapshot.
func (b *Builder) Refresh(ctx context.Context) (*Snapshot, error) {
	s, _, err := b.refresh(ctx, false)
	return s, err
}

// Republish publishes the current window again with the latest realtime
// data; nil before the first refresh.
func (b *Builder) Republish() *Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.win == nil {
		return nil
	}
	return b.publish(loadRealtime, b.now(), b.win, time.Now())
}

func (b *Builder) republishLogged() {
	if s := b.Republish(); s != nil {
		b.log.Debug("departures republished with realtime", "version", s.Version, "feed_time", s.RealtimeFeedTime)
	}
}

func (b *Builder) refreshLogged(ctx context.Context, full bool) {
	s, kind, err := b.refresh(ctx, full)
	switch {
	case ctx.Err() != nil:
		// shutting down
	case errors.Is(err, ErrNoSchedule):
		b.log.Info("no live schedule yet; no departures")
	case err != nil:
		b.log.Error("departures refresh failed; keeping the last snapshot", "error", err.Error())
	case kind == loadFull:
		b.log.Info("departures window loaded", "version", s.Version, "schedule_id", s.ScheduleID,
			"departures", s.Len(), "poles", s.Stops(), "from", s.From, "to", s.To)
	case kind == loadIncremental:
		b.log.Debug("departures window moved", "version", s.Version, "departures", s.Len(), "to", s.To)
	}
}

// refresh publishes a new snapshot unless the window has not moved and full
// is false. kind is "" when nothing was published.
func (b *Builder) refresh(ctx context.Context, full bool) (s *Snapshot, kind string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	generated := b.now()
	now := generated.Truncate(time.Second)
	from, to := now.Add(-b.cfg.LookBack), now.Add(b.cfg.LookAhead)
	if old := b.win; !full && old != nil && from.Equal(old.From) && to.Equal(old.To) {
		return b.current.Load(), "", nil
	}

	start := time.Now()
	w, kind, err := b.advance(ctx, from, to, full)
	if err != nil {
		b.metrics.failed()
		return nil, "", err
	}
	b.win = w
	return b.publish(kind, generated, w, start), kind, nil
}

// publish merges realtime into window w and publishes it as the next
// snapshot. b.mu must be held.
func (b *Builder) publish(kind string, generated time.Time, w *Window, start time.Time) *Snapshot {
	deps, feedTime := w.Departures, time.Time{}
	if b.cfg.Realtime != nil {
		deps, feedTime = b.cfg.Realtime(w.Departures, generated)
	}
	b.version++
	s := newSnapshot(b.version, generated, w, deps, feedTime)
	b.current.Store(s)
	b.metrics.published(kind, time.Since(start), s)
	return s
}

// advance returns the window [from, to). If it only moved forward and still
// overlaps the old one, the departures held are reused and only [old.To, to)
// is loaded; otherwise the whole window is.
func (b *Builder) advance(ctx context.Context, from, to time.Time, full bool) (*Window, string, error) {
	old := b.win
	if full || old == nil || from.Before(old.From) || !from.Before(old.To) || to.Before(old.To) {
		w, err := b.loader.Load(ctx, from, to)
		return w, loadFull, err
	}

	var added []Departure
	if to.After(old.To) {
		add, err := b.loader.Load(ctx, old.To, to)
		if err != nil {
			return nil, "", err
		}
		if add.ScheduleID != old.ScheduleID {
			// An import swapped the schedule; the departures held are from
			// the old one.
			w, err := b.loader.Load(ctx, from, to)
			return w, loadFull, err
		}
		added = add.Departures
	}

	// Departures are sorted by time and everything added is at or after
	// old.To, so keep + added is sorted too.
	i, _ := slices.BinarySearchFunc(old.Departures, from, func(d Departure, t time.Time) int {
		return d.Scheduled.Compare(t)
	})
	keep := old.Departures[i:]
	w := &Window{
		From:       from.In(old.From.Location()),
		To:         to.In(old.To.Location()),
		ScheduleID: old.ScheduleID,
		Departures: make([]Departure, 0, len(keep)+len(added)),
	}
	w.Departures = append(append(w.Departures, keep...), added...)
	return w, loadIncremental, nil
}
