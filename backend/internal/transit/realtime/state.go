package realtime

import (
	"maps"
	"slices"
	"time"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// TripKey identifies a trip instance, the unit realtime data refers to
// (ARCHITECTURE §3.3): the static trip_id and the service day of the
// TripDescriptor's start_date. The same trip_id runs on many days; one feed
// carries today's and tomorrow's (DATA_MODEL §7 #29).
type TripKey struct {
	TripID      string
	ServiceDate departures.ServiceDay
}

// Event is an arrival or departure prediction of a stop time update.
type Event struct {
	// Time is the predicted time; zero if the feed gives none.
	Time time.Time
	// Delay in seconds against the schedule, valid if HasDelay. VBB fills
	// events without realtime data from the schedule (time, no delay), so
	// only an event with a delay is realtime (DATA_MODEL §7 #18).
	Delay    int32
	HasDelay bool
}

// StopUpdate is one stop time update, in the feed's order (increasing stop
// sequence).
type StopUpdate struct {
	// Sequence is the static stop_sequence; NoSequence if the feed has none.
	Sequence int32
	// StopID as the feed sends it: a pole id, or sometimes a bare station
	// number (DATA_MODEL §7 #18); "" if none.
	StopID string
	// Skipped: the vehicle does not stop here (SKIPPED).
	Skipped bool
	// NoData: no realtime for this stop (NO_DATA); the schedule applies.
	NoData    bool
	Arrival   Event
	Departure Event
}

// NoSequence marks a StopUpdate without stop_sequence (~1 % of VBB's).
const NoSequence int32 = -1

// TripUpdate is the realtime data of one trip instance.
type TripUpdate struct {
	Key      TripKey
	RouteID  string
	Canceled bool // the whole trip does not run (CANCELED)
	// Delay is TripUpdate.delay in seconds, valid if HasDelay.
	Delay    int32
	HasDelay bool
	// Updated is TripUpdate.timestamp; zero if the feed gives none.
	Updated time.Time
	Stops   []StopUpdate
}

// Realtime reports whether the trip carries any realtime data: a trip delay
// or an event with a delay (task 4.0 measured this per mode).
func (t *TripUpdate) Realtime() bool {
	if t.HasDelay {
		return true
	}
	for i := range t.Stops {
		if t.Stops[i].Arrival.HasDelay || t.Stops[i].Departure.HasDelay {
			return true
		}
	}
	return false
}

// State is the realtime data of one applied feed. Immutable once built:
// readers share it without locks (like departures.Snapshot, ADR-0005).
type State struct {
	// FeedTime is the feed's header.timestamp: how current the data is
	// (DATA_MODEL §7 #14), not when it was fetched.
	FeedTime time.Time
	trips    map[TripKey]*TripUpdate
}

// Trip returns the update of one trip instance, nil if there is none.
func (s *State) Trip(k TripKey) *TripUpdate {
	if s == nil {
		return nil
	}
	return s.trips[k]
}

// Len is the number of trip instances with an update.
func (s *State) Len() int {
	if s == nil {
		return 0
	}
	return len(s.trips)
}

// Keys returns the trip instances in a stable order (trip_id, then day).
func (s *State) Keys() []TripKey {
	if s == nil {
		return nil
	}
	return slices.SortedFunc(maps.Keys(s.trips), func(a, b TripKey) int {
		if a.TripID != b.TripID {
			if a.TripID < b.TripID {
				return -1
			}
			return 1
		}
		return compareDays(a.ServiceDate, b.ServiceDate)
	})
}

func compareDays(a, b departures.ServiceDay) int {
	switch {
	case a == b:
		return 0
	case a.Year != b.Year:
		return a.Year - b.Year
	case a.Month != b.Month:
		return int(a.Month) - int(b.Month)
	default:
		return a.Day - b.Day
	}
}
