package realtime

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// Outcome of applying a feed to the Store.
type Outcome string

const (
	// Applied: the feed is the new state.
	Applied Outcome = "applied"
	// NoData: the feed has no trip updates. VBB sometimes serves an empty
	// feed with a days-old header (DATA_MODEL §7 #14); that is "no data",
	// not "no delays", so the last state stays.
	NoData Outcome = "no_data"
	// Outdated: the feed is not newer than the state (header.timestamp).
	Outdated Outcome = "outdated"
)

// Store holds the current realtime State. Apply is the poller's Handler;
// readers call Current without locks. Only one goroutine (the poller)
// applies feeds.
type Store struct {
	cur     atomic.Pointer[State]
	log     *slog.Logger
	onApply func()
}

// NewStore returns an empty store.
func NewStore(log *slog.Logger) *Store {
	return &Store{log: log}
}

// OnApply sets a function called after each applied feed (the snapshot
// builder's RealtimeChanged). Set it before the poller starts.
func (s *Store) OnApply(f func()) {
	s.onApply = f
}

// Current returns the state of the last applied feed, nil before the first.
func (s *Store) Current() *State {
	return s.cur.Load()
}

// Apply parses a feed and makes it the current state unless it is empty or
// not newer. A feed that cannot be parsed is an error (the poller counts it
// as rejected and fetches it again); the state stays.
func (s *Store) Apply(_ context.Context, f Feed) error {
	_, err := s.apply(f)
	return err
}

func (s *Store) apply(f Feed) (Outcome, error) {
	st, stats, err := Parse(f.Body)
	if err != nil {
		return "", err
	}
	old := s.cur.Load()
	attrs := []any{"feed_time", st.FeedTime, "age", f.FetchedAt.Sub(st.FeedTime).Round(time.Second).String()}
	switch {
	case st.Len() == 0:
		s.log.Warn("gtfs-rt feed without trip updates: no data, keeping the last state", attrs...)
		return NoData, nil
	case old != nil && !st.FeedTime.After(old.FeedTime):
		s.log.Debug("gtfs-rt feed not newer than the state", append(attrs, "state_time", old.FeedTime)...)
		return Outdated, nil
	}
	s.cur.Store(st)
	s.log.Debug("gtfs-rt feed applied", append(attrs,
		"trips", stats.Kept, "duplicates", stats.Duplicates, "deleted", stats.Deleted,
		"no_trip_key", stats.NoTripKey, "unsupported", stats.Unsupported)...)
	if s.onApply != nil {
		s.onApply()
	}
	return Applied, nil
}

// DefaultMaxAge: realtime data older than this (by header.timestamp) is not
// merged any more. Old predictions shown as live mislead more than the
// schedule; health turns degraded earlier, at 2 min (task 4.4).
const DefaultMaxAge = 5 * time.Minute

// Merger returns the snapshot builder's RealtimeFunc: the current state
// merged into the window's departures, unless there is none or it is older
// than maxAge at now. It always reports the state's feed time, so health
// shows how old it is. Each call sets the match ratio (MatchRatio within
// MatchWindow) on m (nil: none); without a merged state it is 0.
func (s *Store) Merger(maxAge time.Duration, m *Metrics) departures.RealtimeFunc {
	return func(deps []departures.Departure, now time.Time) ([]departures.Departure, time.Time) {
		st := s.Current()
		if st == nil || now.Sub(st.FeedTime) > maxAge {
			m.matched(0)
			if st == nil {
				return deps, time.Time{}
			}
			return deps, st.FeedTime
		}
		merged, stats := Merge(deps, st)
		ratio := MatchRatio(deps, st, now, MatchWindow)
		m.matched(ratio)
		s.log.Debug("realtime merged", "departures", stats.Departures, "with_trip", stats.WithTrip,
			"realtime", stats.Realtime, "propagated", stats.Propagated, "cancelled", stats.Cancelled, "skipped", stats.Skipped,
			"match_ratio", ratio)
		return merged, st.FeedTime
	}
}
