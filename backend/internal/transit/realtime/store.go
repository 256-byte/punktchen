package realtime

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
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
	cur atomic.Pointer[State]
	log *slog.Logger
}

// NewStore returns an empty store.
func NewStore(log *slog.Logger) *Store {
	return &Store{log: log}
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
	return Applied, nil
}
