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
	DefaultLookBack     = 2 * time.Minute  // keep departures this long after their time
	DefaultLookAhead    = 90 * time.Minute // load departures this far ahead
	DefaultRefreshEvery = time.Minute
)

// How a refresh got its departures (metric label).
const (
	loadFull        = "full"        // whole window from the database
	loadIncremental = "incremental" // only the new part of the window
)

// BuilderConfig sets the window [now − LookBack, now + LookAhead) and how
// often it moves. Zero values take the defaults.
type BuilderConfig struct {
	LookBack, LookAhead, Every time.Duration
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
// clock jumped. A failed refresh keeps the last snapshot.
//
// Readers call Current at any time from any goroutine; it never blocks.
type Builder struct {
	cfg     BuilderConfig
	loader  WindowLoader
	metrics *Metrics
	log     *slog.Logger
	now     func() time.Time

	current atomic.Pointer[Snapshot]
	reload  chan struct{}

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
	return &Builder{
		cfg: cfg, loader: loader, metrics: m, log: log,
		now:     time.Now,
		reload:  make(chan struct{}, 1),
		version: uint64(time.Now().UnixMilli()),
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

// Run refreshes now and then every cfg.Every, and on Reload, until ctx is
// done.
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
		}
	}
}

// Refresh moves the window to now and publishes the result. If the window
// has not moved since the last refresh, it returns the current snapshot.
func (b *Builder) Refresh(ctx context.Context) (*Snapshot, error) {
	s, _, err := b.refresh(ctx, false)
	return s, err
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
	b.version++
	s = newSnapshot(b.version, generated, w)
	b.current.Store(s)
	b.metrics.published(kind, time.Since(start), s)
	return s, kind, nil
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
