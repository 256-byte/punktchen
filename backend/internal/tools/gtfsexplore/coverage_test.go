package main

import (
	"testing"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func update(trip string, rel gtfs.TripDescriptor_ScheduleRelationship, delays ...*int32) *gtfs.FeedEntity {
	tu := &gtfs.TripUpdate{Trip: &gtfs.TripDescriptor{TripId: proto.String(trip), ScheduleRelationship: rel.Enum()}}
	for i, d := range delays {
		ev := &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(int64(1000 + i))}
		ev.Delay = d
		tu.StopTimeUpdate = append(tu.StopTimeUpdate, &gtfs.TripUpdate_StopTimeUpdate{Arrival: ev})
	}
	return &gtfs.FeedEntity{Id: proto.String(trip), TripUpdate: tu}
}

func TestCountCoverage(t *testing.T) {
	sched := gtfs.TripDescriptor_SCHEDULED
	modes := map[string]string{
		"u1": "ubahn", "u2": "ubahn", "u3": "ubahn",
		"b1": "bus", "b2": "bus",
		"x1": "", // route without an in-scope pole
	}
	running := map[string]bool{"u1": true, "u2": true, "u3": true, "b1": true, "x1": true}
	msg := &gtfs.FeedMessage{Entity: []*gtfs.FeedEntity{
		update("u1", sched, proto.Int32(0), proto.Int32(60)), // realtime, moved
		update("u1", sched, proto.Int32(0)),                  // second update of u1: ignored
		update("u2", sched, nil, nil),                        // schedule-filled: times only
		update("b1", gtfs.TripDescriptor_CANCELED, proto.Int32(0)),
		update("b2", sched, proto.Int32(120)), // not running now
		update("x1", sched, proto.Int32(30)),  // out of scope
		update("zz", sched, proto.Int32(30)),  // not in the schedule
		{Id: proto.String("v"), Vehicle: &gtfs.VehiclePosition{}},
	}}
	c := countCoverage(msg, modes, running)

	if u := c["ubahn"]; *u != (modeCount{running: 3, updated: 2, realtime: 1, stus: 4, stuDelay: 2, stuNonZero: 1, nonZero: 1}) {
		t.Errorf("ubahn %+v", *u)
	}
	if b := c["bus"]; *b != (modeCount{running: 1, updated: 1, realtime: 1, cancelled: 1, extraTrips: 1, stus: 1, stuDelay: 1}) {
		t.Errorf("bus %+v", *b)
	}
	if _, ok := c[""]; ok {
		t.Error("out-of-scope trips counted")
	}
	if m := c[matchKey]; m.running != 7 || m.updated != 6 {
		t.Errorf("schedule match %d of %d, want 6 of 7", m.updated, m.running)
	}
}

func TestHasDelay(t *testing.T) {
	sched := gtfs.TripDescriptor_SCHEDULED
	if hasDelay(update("a", sched, nil).TripUpdate) {
		t.Error("times only counted as realtime")
	}
	if !hasDelay(update("a", sched, nil, proto.Int32(0)).TripUpdate) {
		t.Error("an explicit delay of 0 is realtime")
	}
	tu := update("a", sched).TripUpdate
	tu.Delay = proto.Int32(30)
	if !hasDelay(tu) {
		t.Error("TripUpdate.delay ignored")
	}
	dep := update("a", sched).TripUpdate
	dep.StopTimeUpdate = []*gtfs.TripUpdate_StopTimeUpdate{{Departure: &gtfs.TripUpdate_StopTimeEvent{Delay: proto.Int32(0)}}}
	if !hasDelay(dep) {
		t.Error("departure delay ignored")
	}
}
