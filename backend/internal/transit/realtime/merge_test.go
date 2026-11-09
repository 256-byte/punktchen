package realtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

var mergeDay = departures.ServiceDay{Year: 2026, Month: time.November, Day: 9}

// schedule is trip T1 on mergeDay: stops 1..5 at poles p1..p5, a minute
// apart from 19:00 Berlin.
func schedule() []departures.Departure {
	var deps []departures.Departure
	for seq := int32(1); seq <= 5; seq++ {
		deps = append(deps, departures.Departure{
			TripID: "T1", ServiceDate: mergeDay, StopID: fmt.Sprintf("p%d", seq), StopSequence: seq,
			Scheduled: time.Date(2026, 11, 9, 19, int(seq), 0, 0, time.UTC),
		})
	}
	return deps
}

// su is a stop update: sequence (or NoSequence), stop_id, departure delay
// (nil: no delay); arrival delay set via arr.
type su struct {
	seq      int32
	stop     string
	dep, arr *int32
	skipped  bool
	noData   bool
}

func sec(v int32) *int32 { return &v }

func (s su) update() StopUpdate {
	u := StopUpdate{Sequence: s.seq, StopID: s.stop, Skipped: s.skipped, NoData: s.noData}
	u.Departure.Time = time.Unix(1, 0) // VBB sets times; only delays count
	u.Arrival.Time = time.Unix(1, 0)
	if s.dep != nil {
		u.Departure.Delay, u.Departure.HasDelay = *s.dep, true
	}
	if s.arr != nil {
		u.Arrival.Delay, u.Arrival.HasDelay = *s.arr, true
	}
	return u
}

func state(trips ...*TripUpdate) *State {
	st := &State{FeedTime: time.Unix(1794249969, 0), trips: map[TripKey]*TripUpdate{}}
	for _, t := range trips {
		st.trips[t.Key] = t
	}
	return st
}

func t1(stops ...su) *TripUpdate {
	u := &TripUpdate{Key: TripKey{"T1", mergeDay}}
	for _, s := range stops {
		u.Stops = append(u.Stops, s.update())
	}
	return u
}

// result writes each departure as "seq:state", state = sched, +Ns
// (realtime), +Ns~ (propagated is not visible on a departure; see stats),
// cancelled; removed stops are missing.
func result(deps []departures.Departure) string {
	parts := make([]string, len(deps))
	for i, d := range deps {
		s := "sched"
		switch {
		case d.Cancelled:
			s = "cancelled"
		case d.Realtime:
			s = fmt.Sprintf("%+ds", d.Delay)
		}
		parts[i] = fmt.Sprintf("%d:%s", d.StopSequence, s)
	}
	return strings.Join(parts, " ")
}

// TestMerge has one case per rule of ARCHITECTURE §3.4 (task 4.3).
func TestMerge(t *testing.T) {
	for _, c := range []struct {
		name  string
		trip  *TripUpdate
		want  string
		stats MergeStats
	}{
		{
			name:  "no TripUpdate: schedule",
			trip:  nil,
			want:  "1:sched 2:sched 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5},
		},
		{
			name: "own delays by stop_sequence",
			trip: t1(su{seq: 1, stop: "p1", dep: sec(0)}, su{seq: 2, stop: "p2", dep: sec(60)}, su{seq: 3, stop: "p3", dep: sec(120)},
				su{seq: 4, stop: "p4", dep: sec(90)}, su{seq: 5, stop: "p5", dep: sec(-30)}),
			want:  "1:+0s 2:+60s 3:+120s 4:+90s 5:-30s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 5},
		},
		{
			name:  "departure event first, else arrival",
			trip:  t1(su{seq: 2, stop: "p2", dep: sec(30), arr: sec(90)}, su{seq: 3, stop: "p3", arr: sec(45)}),
			want:  "1:sched 2:+30s 3:+45s 4:+45s 5:+45s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 4, Propagated: 2},
		},
		{
			name:  "times without delay are schedule (DATA_MODEL §7 #18)",
			trip:  t1(su{seq: 1, stop: "p1"}, su{seq: 2, stop: "p2"}, su{seq: 3, stop: "p3"}, su{seq: 4, stop: "p4"}, su{seq: 5, stop: "p5"}),
			want:  "1:sched 2:sched 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5},
		},
		{
			name:  "delay propagates to later stops without an update",
			trip:  t1(su{seq: 2, stop: "p2", dep: sec(120)}),
			want:  "1:sched 2:+120s 3:+120s 4:+120s 5:+120s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 4, Propagated: 3},
		},
		{
			name:  "propagation stops at a stop filled from the schedule",
			trip:  t1(su{seq: 2, stop: "p2", dep: sec(120)}, su{seq: 4, stop: "p4"}),
			want:  "1:sched 2:+120s 3:+120s 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 2, Propagated: 1},
		},
		{
			name:  "NO_DATA: schedule, also after it",
			trip:  t1(su{seq: 2, stop: "p2", dep: sec(120)}, su{seq: 3, stop: "p3", dep: sec(300), noData: true}),
			want:  "1:sched 2:+120s 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 1},
		},
		{
			name:  "SKIPPED stop removed, propagation passes over it",
			trip:  t1(su{seq: 2, stop: "p2", dep: sec(60)}, su{seq: 3, stop: "p3", skipped: true}),
			want:  "1:sched 2:+60s 4:+60s 5:+60s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 3, Propagated: 2, Skipped: 1},
		},
		{
			name:  "cancelled trip: all departures cancelled",
			trip:  func() *TripUpdate { u := t1(su{seq: 2, stop: "p2", dep: sec(60)}); u.Canceled = true; return u }(),
			want:  "1:cancelled 2:cancelled 3:cancelled 4:cancelled 5:cancelled",
			stats: MergeStats{Departures: 5, WithTrip: 5, Cancelled: 5},
		},
		{
			name:  "stop_id when the update has no stop_sequence",
			trip:  t1(su{seq: NoSequence, stop: "p3", dep: sec(240)}),
			want:  "1:sched 2:sched 3:+240s 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 1},
		},
		{
			name:  "bare station number without stop_sequence matches nothing",
			trip:  t1(su{seq: NoSequence, stop: "900009104", dep: sec(240)}),
			want:  "1:sched 2:sched 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5},
		},
		{
			name:  "bare station number with stop_sequence matches by sequence (DATA_MODEL §7 #18)",
			trip:  t1(su{seq: 3, stop: "900009104", dep: sec(180)}),
			want:  "1:sched 2:sched 3:+180s 4:+180s 5:+180s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 3, Propagated: 2},
		},
		{
			name:  "a stop_id seen twice (loop) is not used to match",
			trip:  t1(su{seq: NoSequence, stop: "p2", dep: sec(60)}, su{seq: NoSequence, stop: "p2", dep: sec(600)}),
			want:  "1:sched 2:sched 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5},
		},
		{
			name:  "an update with another sequence for the same pole is another visit",
			trip:  t1(su{seq: 9, stop: "p2", dep: sec(60)}),
			want:  "1:sched 2:sched 3:sched 4:sched 5:sched",
			stats: MergeStats{Departures: 5, WithTrip: 5},
		},
		{
			name: "TripUpdate.delay before the first stop update",
			trip: func() *TripUpdate {
				u := t1(su{seq: 4, stop: "p4", dep: sec(30)})
				u.Delay, u.HasDelay = 90, true
				return u
			}(),
			want:  "1:+90s 2:+90s 3:+90s 4:+30s 5:+30s",
			stats: MergeStats{Departures: 5, WithTrip: 5, Realtime: 5, Propagated: 4},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var st *State
			if c.trip != nil {
				st = state(c.trip)
			} else {
				st = state(&TripUpdate{Key: TripKey{"OTHER", mergeDay}})
			}
			in := schedule()
			got, stats := Merge(in, st)
			if r := result(got); r != c.want {
				t.Errorf("got  %s\nwant %s", r, c.want)
			}
			if stats != c.stats {
				t.Errorf("stats %+v\n want %+v", stats, c.stats)
			}
			if result(in) != "1:sched 2:sched 3:sched 4:sched 5:sched" {
				t.Error("Merge changed its input")
			}
		})
	}
}

func TestMergeTripInstances(t *testing.T) {
	// Same trip_id, other service day: tomorrow's update must not touch today.
	tomorrow := t1(su{seq: 1, stop: "p1", dep: sec(300)})
	tomorrow.Key.ServiceDate = mergeDay.AddDays(1)
	got, stats := Merge(schedule(), state(tomorrow))
	if r := result(got); r != "1:sched 2:sched 3:sched 4:sched 5:sched" || stats.WithTrip != 0 {
		t.Errorf("%s %+v", r, stats)
	}
}

func TestMergeNoState(t *testing.T) {
	in := schedule()
	for _, st := range []*State{nil, state()} {
		got, stats := Merge(in, st)
		if len(got) != 5 || stats != (MergeStats{Departures: 5}) || &got[0] == &in[0] {
			t.Errorf("%v: %s %+v (want a copy of the schedule)", st, result(got), stats)
		}
	}
}

func TestPredicted(t *testing.T) {
	d := schedule()[0]
	if !d.Predicted().Equal(d.Scheduled) {
		t.Error("schedule only: Predicted = Scheduled")
	}
	d.Delay, d.Realtime = 150, true
	if want := d.Scheduled.Add(150 * time.Second); !d.Predicted().Equal(want) {
		t.Errorf("Predicted %s, want %s", d.Predicted(), want)
	}
	d.Delay = -60
	if want := d.Scheduled.Add(-time.Minute); !d.Predicted().Equal(want) {
		t.Errorf("early: %s, want %s", d.Predicted(), want)
	}
}
