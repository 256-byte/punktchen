package departures

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/internal/transit/static/statictest"
)

const (
	alpha = "de:11000:900000001::1"
	beta  = "de:11000:900000002::1"
	gamma = "de:11000:900000003::1"
	far   = "de:12000:900000009::1" // outside the Berlin bbox
)

// windowFeed covers PLAN 2.2: times past 24:00, both DST switch days of 2026
// and the rows a departure board must leave out.
func windowFeed() map[string]string {
	var st strings.Builder
	st.WriteString("trip_id,stop_id,stop_sequence,arrival_time,departure_time,stop_headsign,pickup_type\n")
	// stop: "stop_id departure_time [stop_headsign] [pickup_type]"
	trip := func(id string, stops ...string) {
		for i, s := range stops {
			f := append(strings.Fields(s), "", "")
			fmt.Fprintf(&st, "%s,%s,%d,%s,%s,%s,%s\n", id, f[0], i+1, f[1], f[1], f[2], f[3])
		}
	}
	trip("night", alpha+" 24:30:00", beta+" 31:17:00", gamma+" 31:30:00")
	trip("day", alpha+" 00:30:00 Beta-Süd", beta+" 00:35:00", gamma+" 00:40:00")
	trip("filters", alpha+" 10:00:00 - 1", far+" 10:05:00", beta+" 10:10:00", gamma+" 10:15:00")
	trip("spring", alpha+" 01:30:00", beta+" 08:00:00", gamma+" 08:10:00")
	trip("springnight", alpha+" 26:30:00", beta+" 26:40:00", gamma+" 26:50:00")
	trip("fall", alpha+" 00:30:00", beta+" 08:00:00", gamma+" 08:10:00")
	trip("fallnight", alpha+" 26:30:00", beta+" 27:30:00", gamma+" 27:40:00")

	return map[string]string{
		"agency.txt": "agency_id,agency_name\na,Test\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon\n" +
			alpha + ",Alpha,52.50,13.40\n" + beta + ",Beta,52.51,13.41\n" +
			gamma + ",Gamma,52.52,13.42\n" + far + ",Far,52.80,13.40\n",
		"routes.txt": "route_id,agency_id,route_short_name,route_type\nM10,a,M10,900\n",
		"trips.txt": "route_id,service_id,trip_id,trip_headsign,direction_id\n" +
			"M10,MON,night,Gamma,0\nM10,TUE,day,Gamma,1\nM10,TUE,filters,,\n" +
			"M10,SPRING,spring,Gamma,0\nM10,SPRING,springnight,Gamma,0\n" +
			"M10,FALL,fall,Gamma,0\nM10,FALL,fallnight,Gamma,0\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n",
		"calendar_dates.txt": "service_id,date,exception_type\n" +
			"MON,20261109,1\nTUE,20261110,1\n" +
			"SPRING,20260328,1\nSPRING,20260329,1\nFALL,20261024,1\nFALL,20261025,1\n",
		"stop_times.txt": st.String(),
	}
}

// dep is the comparable part of a Departure.
type dep struct {
	trip, service, stop string
	at                  string // RFC 3339 in Berlin
}

func deps(w *Window) []dep {
	var out []dep
	for _, d := range w.Departures {
		out = append(out, dep{d.TripID, d.ServiceDate.String(), d.StopID, d.Scheduled.In(berlin).Format(time.RFC3339)})
	}
	return out
}

func load(t *testing.T, l *Loader, from, to string) *Window {
	t.Helper()
	w, err := l.Load(context.Background(), at(t, from), at(t, to))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func expect(t *testing.T, name string, w *Window, want []dep) {
	t.Helper()
	if got := deps(w); !slices.Equal(got, want) {
		t.Errorf("%s:\n got  %v\n want %v", name, got, want)
	}
}

func TestLoadWindow(t *testing.T) {
	pool := statictest.NewDB(t)
	l := NewLoader(pool)

	if _, err := l.Load(context.Background(), at(t, "2026-11-10T00:00:00+01:00"), at(t, "2026-11-10T01:00:00+01:00")); !errors.Is(err, ErrNoSchedule) {
		t.Fatalf("before the first import: err %v, want ErrNoSchedule", err)
	}
	statictest.LoadFiles(t, pool, windowFeed())

	t.Run("after midnight", func(t *testing.T) {
		// 24:30 of Monday's trip is Tuesday 00:30, next to Tuesday's own 00:30.
		w := load(t, l, "2026-11-10T00:20:00+01:00", "2026-11-10T00:40:00+01:00")
		expect(t, "00:20–00:40", w, []dep{
			{"day", "2026-11-10", alpha, "2026-11-10T00:30:00+01:00"},
			{"night", "2026-11-09", alpha, "2026-11-10T00:30:00+01:00"},
			{"day", "2026-11-10", beta, "2026-11-10T00:35:00+01:00"},
			// day's 00:40 at gamma: its last stop, and at the window's end.
		})
		expect(t, "31:17", load(t, l, "2026-11-10T07:00:00+01:00", "2026-11-10T08:00:00+01:00"), []dep{
			{"night", "2026-11-09", beta, "2026-11-10T07:17:00+01:00"},
		})
		// Monday's service does not run on Tuesday: nothing on Wednesday.
		expect(t, "next night", load(t, l, "2026-11-11T00:00:00+01:00", "2026-11-11T08:00:00+01:00"), nil)
	})

	t.Run("spring forward", func(t *testing.T) {
		w := load(t, l, "2026-03-29T00:00:00+01:00", "2026-03-29T09:00:00+02:00")
		expect(t, "2026-03-29", w, []dep{
			{"spring", "2026-03-29", alpha, "2026-03-29T00:30:00+01:00"}, // 01:30 since 23:00 CET
			{"springnight", "2026-03-28", alpha, "2026-03-29T03:30:00+02:00"},
			{"springnight", "2026-03-28", beta, "2026-03-29T03:40:00+02:00"},
			{"spring", "2026-03-29", beta, "2026-03-29T08:00:00+02:00"},
		})
	})

	t.Run("fall back", func(t *testing.T) {
		w := load(t, l, "2026-10-25T00:00:00+02:00", "2026-10-25T09:00:00+01:00")
		expect(t, "2026-10-25", w, []dep{
			{"fall", "2026-10-25", alpha, "2026-10-25T01:30:00+02:00"}, // 00:30 since 01:00 CEST
			{"fallnight", "2026-10-24", alpha, "2026-10-25T02:30:00+02:00"},
			{"fallnight", "2026-10-24", beta, "2026-10-25T02:30:00+01:00"}, // the second 02:30
			{"fall", "2026-10-25", beta, "2026-10-25T08:00:00+01:00"},
		})
	})

	t.Run("left out: no pickup, out of scope, last stop", func(t *testing.T) {
		w := load(t, l, "2026-11-10T09:00:00+01:00", "2026-11-10T11:00:00+01:00")
		expect(t, "filters", w, []dep{{"filters", "2026-11-10", beta, "2026-11-10T10:10:00+01:00"}})
		d := w.Departures[0]
		// No trip_headsign: the last stop's name; no direction_id: NoDirection.
		if d.Headsign != "Gamma" || d.StopHeadsign != "" || d.DirectionID != NoDirection || d.RouteID != "M10" || d.StopSequence != 3 {
			t.Errorf("filters departure %+v", d)
		}
	})

	t.Run("fields", func(t *testing.T) {
		w := load(t, l, "2026-11-10T00:30:00+01:00", "2026-11-10T00:31:00+01:00")
		d := w.Departures[0]
		if d.TripID != "day" || d.StopHeadsign != "Beta-Süd" || d.DirectionID != 1 || d.Headsign != "Gamma" || d.StopSequence != 1 {
			t.Errorf("day departure %+v", d)
		}
		if d.Scheduled.Location() != berlin {
			t.Errorf("scheduled in %v, want Europe/Berlin", d.Scheduled.Location())
		}
		if w.From.Location() != berlin || w.ScheduleID == 0 {
			t.Errorf("window %v / id %d", w.From, w.ScheduleID)
		}
	})

	t.Run("half-open windows add up", func(t *testing.T) {
		whole := deps(load(t, l, "2026-11-10T00:00:00+01:00", "2026-11-10T08:00:00+01:00"))
		var parts []dep
		for _, b := range [][2]string{
			{"2026-11-10T00:00:00+01:00", "2026-11-10T00:30:00+01:00"},
			{"2026-11-10T00:30:00+01:00", "2026-11-10T00:35:00+01:00"}, // starts on two departures
			{"2026-11-10T00:35:00+01:00", "2026-11-10T07:17:00+01:00"}, // ends on one
			{"2026-11-10T07:17:00+01:00", "2026-11-10T08:00:00+01:00"},
		} {
			parts = append(parts, deps(load(t, l, b[0], b[1]))...)
		}
		if len(whole) != 4 || !slices.Equal(parts, whole) {
			t.Errorf("slices %v\n whole %v", parts, whole)
		}
	})

	t.Run("bad window", func(t *testing.T) {
		if _, err := l.Load(context.Background(), at(t, "2026-11-10T01:00:00+01:00"), at(t, "2026-11-10T00:00:00+01:00")); err == nil {
			t.Error("to before from: want error")
		}
	})

	t.Run("new import, new schedule id", func(t *testing.T) {
		before := load(t, l, "2026-11-10T00:00:00+01:00", "2026-11-10T01:00:00+01:00").ScheduleID
		statictest.LoadFiles(t, pool, windowFeed())
		if after := load(t, l, "2026-11-10T00:00:00+01:00", "2026-11-10T01:00:00+01:00").ScheduleID; after == before {
			t.Errorf("schedule id %d unchanged after an import", after)
		}
	})
}

// TestLoadWindowWedding compares the loader with a plain SQL definition of
// "boardable departure" on real data (the Wedding subset).
func TestLoadWindowWedding(t *testing.T) {
	pool := statictest.NewDB(t)
	statictest.LoadZip(t, pool, filepath.Join("..", "..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip"))
	l := NewLoader(pool)

	for _, c := range []struct{ from, to string }{
		{"2026-11-10T17:00:00+01:00", "2026-11-10T18:32:00+01:00"}, // rush hour
		{"2026-11-10T00:15:00+01:00", "2026-11-10T01:45:00+01:00"}, // Monday's night trips + Tuesday's first
	} {
		w := load(t, l, c.from, c.to)
		if len(w.Departures) == 0 {
			t.Fatalf("%s: no departures", c.from)
		}
		if !slices.IsSortedFunc(w.Departures, func(a, b Departure) int { return a.Scheduled.Compare(b.Scheduled) }) {
			t.Errorf("%s: not sorted by time", c.from)
		}
		want := naiveWindow(t, pool, at(t, c.from), at(t, c.to))
		got := deps(w)
		slices.SortFunc(got, cmpDep)
		if !slices.Equal(got, want) {
			t.Errorf("%s: loader %d departures, plain SQL %d", c.from, len(got), len(want))
		}
		var nights int
		for _, d := range w.Departures {
			if DayOf(d.Scheduled, berlin) != d.ServiceDate {
				nights++
			}
		}
		t.Logf("%s: %d departures, %d of them from the previous service day", c.from, len(w.Departures), nights)
	}
}

func cmpDep(a, b dep) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) }

// naiveWindow: every stop time of a trip running on its service day, at an
// in-scope pole, with pickup, followed by another stop, at (service day noon
// − 12 h + seconds) in the window — computed in SQL only.
func naiveWindow(t *testing.T, pool *pgxpool.Pool, from, to time.Time) []dep {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT st.trip_id, sd.service_date::text, st.stop_id,
		       ((sd.service_date + time '12:00') AT TIME ZONE 'Europe/Berlin') - interval '12 hours'
		           + st.departure_secs * interval '1 second' AS at
		FROM gtfs.stop_times st
		JOIN gtfs.trips t ON t.trip_id = st.trip_id
		JOIN gtfs.service_dates sd ON sd.service_id = t.service_id
		JOIN gtfs.stops s ON s.stop_id = st.stop_id AND s.in_scope
		WHERE sd.service_date BETWEEN $1::date - 3 AND $2::date
		  AND coalesce(st.pickup_type, 0) <> 1
		  AND EXISTS (SELECT 1 FROM gtfs.stop_times n WHERE n.trip_id = st.trip_id AND n.stop_sequence > st.stop_sequence)
		  AND ((sd.service_date + time '12:00') AT TIME ZONE 'Europe/Berlin') - interval '12 hours'
		      + st.departure_secs * interval '1 second' >= $3
		  AND ((sd.service_date + time '12:00') AT TIME ZONE 'Europe/Berlin') - interval '12 hours'
		      + st.departure_secs * interval '1 second' < $4`,
		from.In(berlin).Format(time.DateOnly), to.In(berlin).Format(time.DateOnly), from, to)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []dep
	for rows.Next() {
		var d dep
		var ts time.Time
		if err := rows.Scan(&d.trip, &d.service, &d.stop, &ts); err != nil {
			t.Fatal(err)
		}
		d.at = ts.In(berlin).Format(time.RFC3339)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(out, cmpDep)
	return out
}
