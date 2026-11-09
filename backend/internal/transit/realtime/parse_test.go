package realtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// fixtures are the recorded feeds of backend/testdata/gtfsrt (README there).
var fixtures = []string{
	"vbb-20261108T225938Z-wedding.pb",
	"vbb-20261108T225953Z-wedding.pb",
	"vbb-20261108T230009Z-wedding.pb",
	"vbb-20261108T230040Z-wedding.pb",
	"vbb-empty-stale.pb",
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gtfsrt", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestParseGolden parses each recorded feed and compares a summary plus the
// first and last trip instance in full with testdata/parse.golden.txt (task
// 4.2). Run with -update to rewrite it.
func TestParseGolden(t *testing.T) {
	var out strings.Builder
	for _, name := range fixtures {
		st, stats, err := Parse(readFixture(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		describe(&out, name, st, stats)
	}
	golden := filepath.Join("testdata", "parse.golden.txt")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if out.String() != string(want) {
		t.Errorf("parse output differs from %s; inspect with -update and git diff", golden)
	}
}

func describe(w *strings.Builder, name string, st *State, stats ParseStats) {
	trips, canceled, realtime, stops, withSeq, delays, skipped, noData := st.Len(), 0, 0, 0, 0, 0, 0, 0
	days := map[departures.ServiceDay]int{}
	for _, k := range st.Keys() {
		u := st.Trip(k)
		days[k.ServiceDate]++
		if u.Canceled {
			canceled++
		}
		if u.Realtime() {
			realtime++
		}
		for _, s := range u.Stops {
			stops++
			if s.Sequence != NoSequence {
				withSeq++
			}
			if s.Arrival.HasDelay || s.Departure.HasDelay {
				delays++
			}
			if s.Skipped {
				skipped++
			}
			if s.NoData {
				noData++
			}
		}
	}
	fmt.Fprintf(w, "== %s\n", name)
	fmt.Fprintf(w, "feed time %s, stats %+v\n", st.FeedTime.Format(time.RFC3339), stats)
	fmt.Fprintf(w, "trips %d (canceled %d, realtime %d), by service day:", trips, canceled, realtime)
	keys := st.Keys()
	seen := map[departures.ServiceDay]bool{}
	for _, k := range keys {
		if !seen[k.ServiceDate] {
			seen[k.ServiceDate] = true
			fmt.Fprintf(w, " %s=%d", k.ServiceDate, days[k.ServiceDate])
		}
	}
	fmt.Fprintf(w, "\nstop updates %d (stop_sequence %d, with delay %d, skipped %d, no data %d)\n", stops, withSeq, delays, skipped, noData)
	if len(keys) > 0 {
		for _, k := range []TripKey{keys[0], keys[len(keys)-1]} {
			dumpTrip(w, st.Trip(k))
		}
	}
}

func dumpTrip(w *strings.Builder, u *TripUpdate) {
	fmt.Fprintf(w, "trip %s %s route %s canceled=%v delay=%s updated %s\n",
		u.Key.TripID, u.Key.ServiceDate, u.RouteID, u.Canceled, delayText(u.Delay, u.HasDelay), u.Updated.Format(time.RFC3339))
	for _, s := range u.Stops {
		fmt.Fprintf(w, "  seq %3d %-26s arr %s dep %s%s%s\n", s.Sequence, s.StopID,
			eventText(s.Arrival), eventText(s.Departure), flagText(s.Skipped, " SKIPPED"), flagText(s.NoData, " NO_DATA"))
	}
}

func eventText(e Event) string {
	t := "-"
	if !e.Time.IsZero() {
		t = e.Time.Format("15:04:05")
	}
	return t + "/" + delayText(e.Delay, e.HasDelay)
}

func delayText(d int32, ok bool) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%+ds", d)
}

func flagText(on bool, s string) string {
	if on {
		return s
	}
	return ""
}

// feedMsg builds a FeedMessage for the synthetic cases.
func feedMsg(ts uint64, inc gtfs.FeedHeader_Incrementality, entities ...*gtfs.FeedEntity) []byte {
	msg := &gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Incrementality: inc.Enum()},
		Entity: entities,
	}
	if ts != 0 {
		msg.Header.Timestamp = proto.Uint64(ts)
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return b
}

type tripOpt func(*gtfs.TripUpdate, *gtfs.FeedEntity)

func trip(id, start string, opts ...tripOpt) *gtfs.FeedEntity {
	e := &gtfs.FeedEntity{Id: proto.String(id + start), TripUpdate: &gtfs.TripUpdate{
		Trip: &gtfs.TripDescriptor{TripId: proto.String(id), StartDate: proto.String(start), RouteId: proto.String("r1"),
			ScheduleRelationship: gtfs.TripDescriptor_SCHEDULED.Enum()},
	}}
	for _, o := range opts {
		o(e.TripUpdate, e)
	}
	return e
}

func at(ts uint64) tripOpt {
	return func(tu *gtfs.TripUpdate, _ *gtfs.FeedEntity) { tu.Timestamp = proto.Uint64(ts) }
}

func rel(r gtfs.TripDescriptor_ScheduleRelationship) tripOpt {
	return func(tu *gtfs.TripUpdate, _ *gtfs.FeedEntity) { tu.Trip.ScheduleRelationship = r.Enum() }
}

func stu(s *gtfs.TripUpdate_StopTimeUpdate) tripOpt {
	return func(tu *gtfs.TripUpdate, _ *gtfs.FeedEntity) { tu.StopTimeUpdate = append(tu.StopTimeUpdate, s) }
}

const feedTS = 1794178780 // 2026-11-08T22:59:40Z

func TestParse(t *testing.T) {
	full := gtfs.FeedHeader_FULL_DATASET
	day := departures.ServiceDay{Year: 2026, Month: time.November, Day: 8}

	st, stats, err := Parse(feedMsg(feedTS, full,
		trip("t1", "20261108", at(100), stu(&gtfs.TripUpdate_StopTimeUpdate{
			StopSequence: proto.Uint32(3), StopId: proto.String("de:11000:900009104::1"),
			Arrival:   &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(feedTS), Delay: proto.Int32(60)},
			Departure: &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(feedTS + 30)},
		}), stu(&gtfs.TripUpdate_StopTimeUpdate{
			StopId:               proto.String("900009104"), // bare station number, no sequence
			ScheduleRelationship: gtfs.TripUpdate_StopTimeUpdate_SKIPPED.Enum(),
		}), stu(&gtfs.TripUpdate_StopTimeUpdate{
			StopSequence:         proto.Uint32(9),
			ScheduleRelationship: gtfs.TripUpdate_StopTimeUpdate_NO_DATA.Enum(),
		})),
		trip("t1", "20261108", at(90)), // older duplicate: dropped
		trip("t1", "20261109", at(100)),
		trip("t2", "20261108", rel(gtfs.TripDescriptor_CANCELED), func(tu *gtfs.TripUpdate, _ *gtfs.FeedEntity) { tu.Delay = proto.Int32(0) }),
		trip("t3", "20261108", rel(gtfs.TripDescriptor_ADDED)),
		trip("t4", "2026-11-08"),
		trip("", "20261108"),
		trip("t5", "20261108", func(_ *gtfs.TripUpdate, e *gtfs.FeedEntity) { e.IsDeleted = proto.Bool(true) }),
		&gtfs.FeedEntity{Id: proto.String("alert"), Alert: &gtfs.Alert{}},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := ParseStats{TripUpdates: 8, Kept: 3, Duplicates: 1, Deleted: 1, NoTripKey: 2, Unsupported: 1}
	if stats != want {
		t.Errorf("stats %+v\n want %+v", stats, want)
	}
	if !st.FeedTime.Equal(time.Unix(feedTS, 0)) {
		t.Errorf("feed time %s", st.FeedTime)
	}

	t1 := st.Trip(TripKey{"t1", day})
	if t1 == nil || len(t1.Stops) != 3 || !t1.Updated.Equal(time.Unix(100, 0)) {
		t.Fatalf("t1 %+v: want the latest update with 3 stops", t1)
	}
	if s := t1.Stops[0]; s.Sequence != 3 || s.StopID != "de:11000:900009104::1" || !s.Arrival.HasDelay || s.Arrival.Delay != 60 ||
		s.Departure.HasDelay || !s.Departure.Time.Equal(time.Unix(feedTS+30, 0)) {
		t.Errorf("stop 0 %+v", s)
	}
	if s := t1.Stops[1]; s.Sequence != NoSequence || !s.Skipped || s.StopID != "900009104" || !s.Arrival.Time.IsZero() {
		t.Errorf("stop 1 %+v", s)
	}
	if s := t1.Stops[2]; s.Sequence != 9 || !s.NoData || s.StopID != "" {
		t.Errorf("stop 2 %+v", s)
	}
	if !t1.Realtime() {
		t.Error("t1 has an arrival delay: realtime")
	}
	if t2 := st.Trip(TripKey{"t2", day}); t2 == nil || !t2.Canceled || !t2.HasDelay || !t2.Realtime() {
		t.Errorf("t2 %+v: canceled with a trip delay", t2)
	}
	if other := st.Trip(TripKey{"t1", day.AddDays(1)}); other == nil || other.Realtime() {
		t.Errorf("t1 next day %+v: kept separately, no realtime", other)
	}
	if st.Trip(TripKey{"t3", day}) != nil || st.Trip(TripKey{"t5", day}) != nil {
		t.Error("added or deleted trip kept")
	}
	if keys := st.Keys(); len(keys) != 3 || keys[0] != (TripKey{"t1", day}) || keys[1] != (TripKey{"t1", day.AddDays(1)}) {
		t.Errorf("keys %v", keys)
	}
}

func TestParseErrors(t *testing.T) {
	for name, c := range map[string]struct {
		body []byte
		want error
	}{
		"differential": {feedMsg(feedTS, gtfs.FeedHeader_DIFFERENTIAL), ErrNotFullDataset},
		"no timestamp": {feedMsg(0, gtfs.FeedHeader_FULL_DATASET), ErrNoFeedTime},
	} {
		if _, _, err := Parse(c.body); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if _, _, err := Parse([]byte("<html>not protobuf</html>")); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("garbage: %v", err)
	}
}

func TestParseStartDate(t *testing.T) {
	if d, err := parseStartDate("20261108"); err != nil || d != (departures.ServiceDay{Year: 2026, Month: time.November, Day: 8}) {
		t.Errorf("20261108: %v %v", d, err)
	}
	for _, bad := range []string{"", "2026118", "2026-11-08", "20261131", "20261308", "2026110x", "20260100"} {
		if _, err := parseStartDate(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// TestStore is task 4.2's sequence on the recorded feeds: newer feeds
// replace the state, the empty stale feed keeps it (DATA_MODEL §7 #14), an
// older feed after a newer one is ignored, a broken one is an error.
func TestStore(t *testing.T) {
	s := NewStore(slog.New(slog.DiscardHandler))
	if s.Current() != nil || s.Current().Len() != 0 || s.Current().Trip(TripKey{}) != nil || s.Current().Keys() != nil {
		t.Fatal("new store has a state")
	}
	fetched := time.Date(2026, 11, 8, 23, 1, 0, 0, time.UTC)
	steps := []struct {
		file string
		want Outcome
	}{
		{"vbb-20261108T225938Z-wedding.pb", Applied},
		{"vbb-20261108T225953Z-wedding.pb", Applied},
		{"vbb-empty-stale.pb", NoData},
		{"vbb-20261108T230009Z-wedding.pb", Applied},
		{"vbb-20261108T225953Z-wedding.pb", Outdated},
		{"vbb-20261108T230009Z-wedding.pb", Outdated}, // same header time: same data
	}
	var lastApplied *State
	for i, step := range steps {
		got, err := s.apply(Feed{Body: readFixture(t, step.file), FetchedAt: fetched})
		if err != nil || got != step.want {
			t.Fatalf("step %d %s: %s, %v; want %s", i+1, step.file, got, err, step.want)
		}
		if got == Applied {
			lastApplied = s.Current()
		}
		if s.Current() != lastApplied {
			t.Fatalf("step %d %s: state changed without applying", i+1, step.file)
		}
	}
	if want := time.Date(2026, 11, 8, 23, 0, 7, 0, time.UTC); s.Current().FeedTime.Sub(want).Abs() > 10*time.Second {
		t.Errorf("state from %s; want the 23:00:09 sample", s.Current().FeedTime)
	}

	if err := s.Apply(context.Background(), Feed{Body: []byte("garbage")}); err == nil {
		t.Error("broken feed applied")
	}
	if s.Current() != lastApplied {
		t.Error("broken feed changed the state")
	}
}
