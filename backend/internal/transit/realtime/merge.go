package realtime

import (
	"time"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// MergeStats counts what Merge did, for logs.
type MergeStats struct {
	Departures int // scheduled departures in
	WithTrip   int // ... whose trip instance has a TripUpdate
	Realtime   int // ... that got a delay (own or propagated)
	Propagated int // ... of those, from an earlier stop
	Cancelled  int // ... of a cancelled trip
	Skipped    int // ... removed: SKIPPED stop
}

// MatchWindow: the match ratio counts departures scheduled at most this far
// from now. VBB sends TripUpdates only for trips running around now: ~95 %
// of departures within ±15 min have one, 14 % of those 60–90 min ahead
// (DATA_MODEL §7 #31), so a ratio over the whole window would mostly
// measure the window's shape.
const MatchWindow = 15 * time.Minute

// MatchRatio is the share of departures scheduled within ±window of now
// whose trip instance (trip_id, start_date) has a TripUpdate in st; 0 when
// there are none or st is nil. A sudden drop means static and realtime IDs
// no longer match (e.g. a new schedule that the feed does not use yet).
func MatchRatio(deps []departures.Departure, st *State, now time.Time, window time.Duration) float64 {
	var near, matched int
	for _, d := range deps {
		if off := d.Scheduled.Sub(now); off < -window || off > window {
			continue
		}
		near++
		if st.Trip(TripKey{TripID: d.TripID, ServiceDate: d.ServiceDate}) != nil {
			matched++
		}
	}
	if near == 0 {
		return 0
	}
	return float64(matched) / float64(near)
}

// Merge applies the realtime state to scheduled departures (ARCHITECTURE
// §3.4) and returns a new slice in the same order; deps is not changed. A nil
// state returns a copy.
//
// Per departure of a trip instance with a TripUpdate:
//   - trip CANCELED → Cancelled (no delay);
//   - its stop's update (matched by stop_sequence, else by stop_id):
//     SKIPPED → removed; with a delay (departure event, else arrival) →
//     Realtime with that delay; without a delay (VBB fills those from the
//     schedule, DATA_MODEL §7 #18) or NO_DATA → schedule;
//   - no update for its stop → the last update before it (by stop_sequence,
//     SKIPPED ones passed over) decides: with a delay → that delay
//     (propagated, GTFS-RT semantics); without one or NO_DATA → schedule;
//   - before the first update → TripUpdate.delay if set, else schedule.
func Merge(deps []departures.Departure, st *State) ([]departures.Departure, MergeStats) {
	stats := MergeStats{Departures: len(deps)}
	out := make([]departures.Departure, 0, len(deps))
	if st.Len() == 0 {
		return append(out, deps...), stats
	}
	trips := map[TripKey]*tripIndex{}
	for _, d := range deps {
		key := TripKey{TripID: d.TripID, ServiceDate: d.ServiceDate}
		idx, ok := trips[key]
		if !ok {
			idx = newTripIndex(st.Trip(key))
			trips[key] = idx
		}
		if idx == nil {
			out = append(out, d)
			continue
		}
		stats.WithTrip++
		if idx.trip.Canceled {
			d.Cancelled = true
			stats.Cancelled++
			out = append(out, d)
			continue
		}
		delay, realtime, propagated, skipped := idx.delayAt(d.StopSequence, d.StopID)
		if skipped {
			stats.Skipped++
			continue
		}
		if realtime {
			d.Delay, d.Realtime = delay, true
			stats.Realtime++
			if propagated {
				stats.Propagated++
			}
		}
		out = append(out, d)
	}
	return out, stats
}

// tripIndex finds a trip's stop updates by stop_sequence or stop_id.
type tripIndex struct {
	trip   *TripUpdate
	bySeq  map[int32]int  // stop_sequence → index in trip.Stops
	byStop map[string]int // stop_id → index, only for stop_ids that occur once
	// seqs holds the indexes of updates with a stop_sequence, in feed order
	// (increasing sequence, DATA_MODEL §7 #29).
	seqs []int
}

func newTripIndex(t *TripUpdate) *tripIndex {
	if t == nil {
		return nil
	}
	idx := &tripIndex{trip: t, bySeq: map[int32]int{}, byStop: map[string]int{}}
	seen := map[string]int{}
	for i, s := range t.Stops {
		if s.Sequence != NoSequence {
			idx.bySeq[s.Sequence] = i
			idx.seqs = append(idx.seqs, i)
		}
		if s.StopID != "" {
			seen[s.StopID]++
			idx.byStop[s.StopID] = i
		}
	}
	for id, n := range seen {
		if n > 1 {
			delete(idx.byStop, id) // a loop passes the stop twice: ambiguous
		}
	}
	return idx
}

// delayAt returns the realtime delay of the departure at sequence seq, pole
// stopID; realtime is false where the schedule applies.
func (x *tripIndex) delayAt(seq int32, stopID string) (delay int32, realtime, propagated, skipped bool) {
	if i, ok := x.own(seq, stopID); ok {
		s := x.trip.Stops[i]
		if s.Skipped {
			return 0, false, false, true
		}
		d, ok := stopDelay(s)
		return d, ok, false, false
	}
	// No update for this stop: the last update before it decides.
	for j := len(x.seqs) - 1; j >= 0; j-- {
		s := x.trip.Stops[x.seqs[j]]
		if s.Sequence >= seq || s.Skipped {
			continue
		}
		d, ok := stopDelay(s)
		return d, ok, ok, false
	}
	if x.trip.HasDelay {
		return x.trip.Delay, true, true, false
	}
	return 0, false, false, false
}

// own finds the update for this stop.
func (x *tripIndex) own(seq int32, stopID string) (int, bool) {
	if i, ok := x.bySeq[seq]; ok {
		return i, true
	}
	if i, ok := x.byStop[stopID]; ok && x.trip.Stops[i].Sequence == NoSequence {
		return i, true
	}
	return 0, false
}

// stopDelay is the realtime delay of a stop update: its departure event,
// else its arrival (a first stop can arrive after it departs, DATA_MODEL
// §7 #29). NO_DATA and events without a delay are no realtime.
func stopDelay(s StopUpdate) (int32, bool) {
	switch {
	case s.NoData:
		return 0, false
	case s.Departure.HasDelay:
		return s.Departure.Delay, true
	case s.Arrival.HasDelay:
		return s.Arrival.Delay, true
	default:
		return 0, false
	}
}
