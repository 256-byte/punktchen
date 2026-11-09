package realtime

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// ParseStats counts what Parse kept and left out of a feed.
type ParseStats struct {
	TripUpdates int // TripUpdate entities in the feed
	Kept        int // trip instances in the state
	Duplicates  int // older updates of a trip instance seen twice (latest wins)
	// Left out:
	Deleted     int // entities with is_deleted
	NoTripKey   int // without trip_id or with a bad start_date
	Unsupported int // ADDED, UNSCHEDULED, DUPLICATED, DELETED trips (not in VBB's feed)
}

// Parse errors: the feed cannot be used at all.
var (
	ErrNotFullDataset = errors.New("gtfs-rt: only FULL_DATASET feeds are supported")
	ErrNoFeedTime     = errors.New("gtfs-rt: header has no timestamp")
)

// Parse decodes a GTFS-RT FeedMessage into a State. Only TripUpdates are
// read (VBB sends nothing else, ARCHITECTURE §3.3). Within a feed the latest
// update of a trip instance wins (TripUpdate.timestamp, else the later
// entity).
func Parse(body []byte) (*State, ParseStats, error) {
	var stats ParseStats
	var msg gtfs.FeedMessage
	if err := proto.Unmarshal(body, &msg); err != nil {
		return nil, stats, fmt.Errorf("gtfs-rt: decode: %w", err)
	}
	h := msg.GetHeader()
	if h.GetIncrementality() != gtfs.FeedHeader_FULL_DATASET {
		return nil, stats, ErrNotFullDataset
	}
	if h.GetTimestamp() == 0 {
		return nil, stats, ErrNoFeedTime
	}
	st := &State{
		FeedTime: unix(h.GetTimestamp()),
		trips:    make(map[TripKey]*TripUpdate, len(msg.GetEntity())),
	}
	for _, e := range msg.GetEntity() {
		tu := e.GetTripUpdate()
		if tu == nil {
			continue
		}
		stats.TripUpdates++
		if e.GetIsDeleted() {
			stats.Deleted++
			continue
		}
		u, ok, supported := tripUpdate(tu)
		switch {
		case !supported:
			stats.Unsupported++
			continue
		case !ok:
			stats.NoTripKey++
			continue
		}
		if old := st.trips[u.Key]; old != nil {
			stats.Duplicates++
			if u.Updated.Before(old.Updated) {
				continue
			}
		}
		st.trips[u.Key] = u
	}
	stats.Kept = len(st.trips)
	return st, stats, nil
}

// tripUpdate converts one TripUpdate. ok is false without a usable trip
// key; supported is false for trip kinds the merge does not handle.
func tripUpdate(tu *gtfs.TripUpdate) (u *TripUpdate, ok, supported bool) {
	td := tu.GetTrip()
	switch td.GetScheduleRelationship() {
	case gtfs.TripDescriptor_SCHEDULED, gtfs.TripDescriptor_CANCELED:
	default:
		return nil, false, false
	}
	day, err := parseStartDate(td.GetStartDate())
	if td.GetTripId() == "" || err != nil {
		return nil, false, true
	}
	u = &TripUpdate{
		Key:      TripKey{TripID: td.GetTripId(), ServiceDate: day},
		RouteID:  td.GetRouteId(),
		Canceled: td.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED,
		Delay:    tu.GetDelay(),
		HasDelay: tu.Delay != nil,
		Stops:    make([]StopUpdate, 0, len(tu.GetStopTimeUpdate())),
	}
	if tu.Timestamp != nil {
		u.Updated = unix(tu.GetTimestamp())
	}
	for _, stu := range tu.GetStopTimeUpdate() {
		s := StopUpdate{
			Sequence:  NoSequence,
			StopID:    stu.GetStopId(),
			Skipped:   stu.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_SKIPPED,
			NoData:    stu.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_NO_DATA,
			Arrival:   event(stu.GetArrival()),
			Departure: event(stu.GetDeparture()),
		}
		if stu.StopSequence != nil && stu.GetStopSequence() <= 1<<31-1 {
			s.Sequence = int32(stu.GetStopSequence())
		}
		u.Stops = append(u.Stops, s)
	}
	return u, true, true
}

func event(ev *gtfs.TripUpdate_StopTimeEvent) Event {
	if ev == nil {
		return Event{}
	}
	e := Event{Delay: ev.GetDelay(), HasDelay: ev.Delay != nil}
	if ev.Time != nil {
		e.Time = time.Unix(ev.GetTime(), 0).UTC()
	}
	return e
}

// parseStartDate reads a GTFS date (YYYYMMDD).
func parseStartDate(s string) (departures.ServiceDay, error) {
	if len(s) != 8 {
		return departures.ServiceDay{}, fmt.Errorf("start_date %q: want YYYYMMDD", s)
	}
	y, err1 := strconv.Atoi(s[:4])
	m, err2 := strconv.Atoi(s[4:6])
	d, err3 := strconv.Atoi(s[6:])
	if err := errors.Join(err1, err2, err3); err != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return departures.ServiceDay{}, fmt.Errorf("start_date %q: want YYYYMMDD", s)
	}
	day := departures.ServiceDay{Year: y, Month: time.Month(m), Day: d}
	if day.AddDays(0) != day { // e.g. 20261131
		return departures.ServiceDay{}, fmt.Errorf("start_date %q: no such day", s)
	}
	return day, nil
}

func unix(secs uint64) time.Time {
	return time.Unix(int64(min(secs, 1<<62)), 0).UTC() //nolint:gosec // clamped
}
