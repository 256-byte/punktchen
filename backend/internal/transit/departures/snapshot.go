package departures

import "time"

// Snapshot is the published state of the departures (ADR-0005). It is
// immutable: once published it is never changed, so any number of goroutines
// read it without locks. A newer state is a new Snapshot.
//
// For now it holds the schedule only; realtime is merged in Phase 4.
type Snapshot struct {
	// Version increases with every published snapshot, also across restarts
	// (see Builder). It goes into cache keys and ETags.
	Version     uint64
	GeneratedAt time.Time
	// ScheduleID is the schedule the departures come from (Window.ScheduleID).
	ScheduleID int64
	// From, To: the window covered, half-open like Window.
	From, To time.Time

	deps   []Departure            // grouped by StopID, each group sorted by time
	byStop map[string][]Departure // StopID → its group in deps
}

// newSnapshot groups w's departures by pole. It copies them, so the
// snapshot shares no memory with w.
func newSnapshot(version uint64, generatedAt time.Time, w *Window) *Snapshot {
	// Counting sort by pole: stable, so each group keeps the time order of
	// w.Departures. One backing array for all groups.
	count := map[string]int{}
	var order []string
	for _, d := range w.Departures {
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

	deps := make([]Departure, len(w.Departures))
	for _, d := range w.Departures {
		deps[next[d.StopID]] = d
		next[d.StopID]++
	}
	byStop := make(map[string][]Departure, len(order))
	for _, stop := range order {
		end := next[stop]
		start := end - count[stop]
		byStop[stop] = deps[start:end:end] // full slice expr: append by a reader cannot reach the next group
	}

	return &Snapshot{
		Version:     version,
		GeneratedAt: generatedAt,
		ScheduleID:  w.ScheduleID,
		From:        w.From,
		To:          w.To,
		deps:        deps,
		byStop:      byStop,
	}
}

// Departures returns the departures at a pole, sorted by time; nil if none.
// The slice belongs to the snapshot: read it, never modify it.
func (s *Snapshot) Departures(stopID string) []Departure {
	return s.byStop[stopID]
}

// Len is the number of departures in the snapshot.
func (s *Snapshot) Len() int { return len(s.deps) }

// Stops is the number of poles with at least one departure.
func (s *Snapshot) Stops() int { return len(s.byStop) }
