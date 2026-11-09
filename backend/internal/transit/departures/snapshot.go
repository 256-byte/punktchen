package departures

import (
	"slices"
	"time"
)

// Snapshot is the published state of the departures (ADR-0005). It is
// immutable: once published it is never changed, so any number of goroutines
// read it without locks. A newer state is a new Snapshot.
//
// It holds the schedule window with realtime merged in (task 4.4).
type Snapshot struct {
	// Version increases with every published snapshot, also across restarts
	// (see Builder). It goes into cache keys and ETags.
	Version     uint64
	GeneratedAt time.Time
	// ScheduleID is the schedule the departures come from (Window.ScheduleID).
	ScheduleID int64
	// From, To: the window covered, half-open like Window.
	From, To time.Time
	// RealtimeFeedTime is the header.timestamp of the realtime feed merged
	// in; zero without realtime data. Health and realtimeAgeSeconds use it.
	RealtimeFeedTime time.Time

	deps   []Departure            // grouped by StopID, each group sorted by predicted time
	byStop map[string][]Departure // StopID → its group in deps
}

// newSnapshot groups the departures of window w (deps: w.Departures with
// realtime merged in, see Builder) by pole and sorts each group by
// predicted time. It copies them, so the snapshot shares no memory with its
// input.
func newSnapshot(version uint64, generatedAt time.Time, w *Window, deps []Departure, feedTime time.Time) *Snapshot {
	// Counting sort by pole: stable, so each group keeps the time order of
	// the input. One backing array for all groups.
	count := map[string]int{}
	var order []string
	for _, d := range deps {
		if count[d.StopID] == 0 {
			order = append(order, d.StopID)
		}
		count[d.StopID]++
	}
	next := make(map[string]int, len(order)) // next free index per pole
	off := 0
	for _, stop := range order {
		next[stop] = off
		off += count[stop]
	}

	grouped := make([]Departure, len(deps))
	for _, d := range deps {
		grouped[next[d.StopID]] = d
		next[d.StopID]++
	}
	byStop := make(map[string][]Departure, len(order))
	for _, stop := range order {
		end := next[stop]
		start := end - count[stop]
		group := grouped[start:end:end] // full slice expr: append by a reader cannot reach the next group
		// A late vehicle can leave after the next one: order by when it
		// leaves (stable: equal times keep the schedule order).
		slices.SortStableFunc(group, func(a, b Departure) int { return a.Predicted().Compare(b.Predicted()) })
		byStop[stop] = group
	}

	return &Snapshot{
		Version:          version,
		GeneratedAt:      generatedAt,
		ScheduleID:       w.ScheduleID,
		From:             w.From,
		To:               w.To,
		RealtimeFeedTime: feedTime,
		deps:             grouped,
		byStop:           byStop,
	}
}

// Departures returns the departures at a pole, sorted by predicted time
// (scheduled time plus delay); nil if none.
// The slice belongs to the snapshot: read it, never modify it.
func (s *Snapshot) Departures(stopID string) []Departure {
	return s.byStop[stopID]
}

// Len is the number of departures in the snapshot.
func (s *Snapshot) Len() int { return len(s.deps) }

// Stops is the number of poles with at least one departure.
func (s *Snapshot) Stops() int { return len(s.byStop) }
