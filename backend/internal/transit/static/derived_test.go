package static

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
)

// feedBuilder writes a GTFS feed in code, so each derived-table rule gets a
// small, readable scenario.
type feedBuilder struct {
	stops, routes, trips, stopTimes strings.Builder
	trip                            int
}

func newFeedBuilder() *feedBuilder {
	b := &feedBuilder{}
	b.stops.WriteString("stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station\n")
	b.routes.WriteString("route_id,agency_id,route_short_name,route_type\n")
	b.trips.WriteString("route_id,service_id,trip_id,trip_headsign,direction_id\n")
	b.stopTimes.WriteString("trip_id,stop_id,stop_sequence,arrival_time,departure_time,stop_headsign\n")
	return b
}

func (b *feedBuilder) stop(id, name string, lat, lon float64, locType int, parent string) {
	fmt.Fprintf(&b.stops, "%s,%q,%f,%f,%d,%s\n", id, name, lat, lon, locType, parent)
}

func (b *feedBuilder) route(id, short string, routeType int) {
	fmt.Fprintf(&b.routes, "%s,a,%s,%d\n", id, short, routeType)
}

// tripVia adds a trip over stops; each stop is "stop_id" or "stop_id|stop_headsign".
func (b *feedBuilder) tripVia(route, service, headsign string, dir int, startHour int, stops ...string) string {
	b.trip++
	id := fmt.Sprintf("t%d", b.trip)
	fmt.Fprintf(&b.trips, "%s,%s,%s,%q,%d\n", route, service, id, headsign, dir)
	for i, s := range stops {
		stopID, stopHeadsign, _ := strings.Cut(s, "|")
		tm := fmt.Sprintf("%d:%02d:00", startHour, i*5)
		fmt.Fprintf(&b.stopTimes, "%s,%s,%d,%s,%s,%s\n", id, stopID, i, tm, tm, stopHeadsign)
	}
	return id
}

func (b *feedBuilder) files() map[string]string {
	return map[string]string{
		"agency.txt": "agency_id,agency_name\na,Test\n",
		// WK: Mon–Fri 2026-11-09 … 2026-11-15; EXTRA: only an added date.
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
			"WK,1,1,1,1,1,0,0,20261109,20261115\n",
		"calendar_dates.txt": "service_id,date,exception_type\n" +
			"WK,20261114,1\n" + // Saturday added
			"WK,20261111,2\n" + // Wednesday removed
			"EXTRA,20261120,1\n",
		"stops.txt":      b.stops.String(),
		"routes.txt":     b.routes.String(),
		"trips.txt":      b.trips.String(),
		"stop_times.txt": b.stopTimes.String(),
	}
}

// rulesFeed: Alpha has a station row (pole ::2 without parent), Beta exists
// only as a DHID prefix, Delta is never served.
func rulesFeed() map[string]string {
	b := newFeedBuilder()
	b.stop("de:11000:900000001", "Alpha (Berlin)", 52.500, 13.400, 1, "")
	b.stop("de:11000:900000001::1", "Alpha (Berlin)", 52.501, 13.401, 0, "de:11000:900000001")
	b.stop("de:11000:900000001::2", "Alpha (Berlin)", 52.502, 13.402, 0, "")
	b.stop("de:11000:900000002::1", "Beta", 52.510, 13.410, 0, "")
	b.stop("de:11000:900000002::2", "Beta", 52.510, 13.420, 0, "")
	b.stop("de:11000:900000002::3", "Beta/Nord", 52.520, 13.430, 0, "")
	b.stop("de:11000:900000003::1", "Gamma", 52.530, 13.440, 0, "")
	b.stop("de:11000:900000004::1", "Delta", 52.540, 13.450, 0, "")
	b.stop("de:11000:900000005::1", "Epsilon", 52.550, 13.460, 0, "")
	b.stop("de:11000:900000006::1", "Zeta", 52.560, 13.470, 0, "")

	for _, r := range []struct {
		id, short string
		typ       int
	}{
		{"R_U8", "U8", 400}, {"R_S41", "S41", 109}, {"R_100", "100", 700}, {"R_200", "200", 3},
		{"R_201", "201", 700}, {"R_M10", "M10", 900}, {"R_F10", "F10", 1000}, {"R_RE1", "RE1", 100},
		{"R_X", "X", 7},
	} {
		b.route(r.id, r.short, r.typ)
	}
	const alpha1, alpha2 = "de:11000:900000001::1", "de:11000:900000001::2"
	const beta1, beta2, beta3 = "de:11000:900000002::1", "de:11000:900000002::2", "de:11000:900000002::3"
	const gamma, epsilon, zeta = "de:11000:900000003::1", "de:11000:900000005::1", "de:11000:900000006::1"

	// U8 both ways; two of three trips show "Gamma Süd" at Alpha.
	b.tripVia("R_U8", "WK", "Gamma", 0, 8, alpha1+"|Gamma Süd", beta1, gamma)
	b.tripVia("R_U8", "WK", "Gamma", 0, 9, alpha1+"|Gamma Süd", beta1, gamma)
	b.tripVia("R_U8", "WK", "Gamma", 0, 10, alpha1, beta1, gamma)
	b.tripVia("R_U8", "WK", "Alpha", 1, 8, gamma, beta1, alpha1)
	// Loop bus: starts and ends at Alpha ::2.
	b.tripVia("R_100", "WK", "Alpha", 0, 8, alpha2, beta2, alpha2)
	// Short turns: 19 of 20 end at Epsilon (= 95 %) → terminal; 9 of 10 → not.
	for i := 0; i < 20; i++ {
		stops := []string{beta3, epsilon}
		if i == 0 {
			stops = append(stops, zeta)
		}
		b.tripVia("R_200", "WK", "Epsilon", 0, 6+i%10, stops...)
	}
	for i := 0; i < 10; i++ {
		stops := []string{beta3, epsilon}
		if i == 0 {
			stops = append(stops, zeta)
		}
		b.tripVia("R_201", "WK", "Ost", 0, 6+i, stops...)
	}
	// Ring S41 (a loop) and one trip per remaining mode; the ferry has no headsign.
	b.tripVia("R_S41", "WK", "Ringbahn S 41", 0, 8, gamma, beta1, gamma)
	b.tripVia("R_M10", "WK", "Gamma", 0, 24, beta2, gamma) // after midnight: 24:00, 24:05
	b.tripVia("R_F10", "EXTRA", "", 0, 8, beta2, gamma)
	b.tripVia("R_RE1", "WK", "Gamma", 0, 8, beta2, gamma)
	b.tripVia("R_X", "WK", "Gamma", 0, 8, beta2, gamma)
	return b.files()
}

type direction struct {
	Mode, Display, Station string
	Terminal, Origin, Ring bool
	TripCount              int64
}

func directionAt(t *testing.T, pool *pgxpool.Pool, stopID, line, headsign string) direction {
	t.Helper()
	var d direction
	err := pool.QueryRow(context.Background(), `
		SELECT mode, display_headsign, station_id, is_terminal, is_origin, is_ring, trip_count
		FROM gtfs_staging.pole_directions WHERE stop_id = $1 AND line = $2 AND headsign = $3`,
		stopID, line, headsign).Scan(&d.Mode, &d.Display, &d.Station, &d.Terminal, &d.Origin, &d.Ring, &d.TripCount)
	if err != nil {
		t.Fatalf("direction %s %s %q: %v", stopID, line, headsign, err)
	}
	return d
}

func TestDerivedTables(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	loader := NewLoader(pool, testScope(t), slog.New(slog.DiscardHandler))
	if _, err := loader.Load(ctx, writeZip(t, rulesFeed())); err != nil {
		t.Fatal(err)
	}

	t.Run("service dates: weekdays, added and removed dates", func(t *testing.T) {
		got := scalar[[]string](t, pool, `SELECT array_agg(service_id || ' ' || service_date ORDER BY service_id, service_date) FROM gtfs_staging.service_dates`)
		want := []string{"EXTRA 2026-11-20", "WK 2026-11-09", "WK 2026-11-10", "WK 2026-11-12", "WK 2026-11-13", "WK 2026-11-14"}
		if !slices.Equal(got, want) {
			t.Fatalf("service_dates\n got %v\nwant %v", got, want)
		}
	})

	t.Run("terminal and origin", func(t *testing.T) {
		const alpha1, beta1, gamma = "de:11000:900000001::1", "de:11000:900000002::1", "de:11000:900000003::1"
		if d := directionAt(t, pool, gamma, "U8", "Gamma"); !d.Terminal || d.Origin {
			t.Errorf("U8 → Gamma at Gamma: %+v, want terminal", d)
		}
		if d := directionAt(t, pool, gamma, "U8", "Alpha"); d.Terminal || !d.Origin {
			t.Errorf("U8 → Alpha at Gamma: %+v, want origin", d)
		}
		if d := directionAt(t, pool, alpha1, "U8", "Gamma"); d.Terminal || !d.Origin {
			t.Errorf("U8 → Gamma at Alpha: %+v, want origin", d)
		}
		if d := directionAt(t, pool, beta1, "U8", "Gamma"); d.Terminal || d.Origin {
			t.Errorf("U8 → Gamma at Beta: %+v, want neither", d)
		}
	})

	t.Run("95 percent rule for short turns", func(t *testing.T) {
		const epsilon = "de:11000:900000005::1"
		if d := directionAt(t, pool, epsilon, "200", "Epsilon"); !d.Terminal {
			t.Errorf("19 of 20 end here: %+v, want terminal", d)
		}
		if d := directionAt(t, pool, epsilon, "201", "Ost"); d.Terminal {
			t.Errorf("9 of 10 end here: %+v, want not terminal", d)
		}
	})

	t.Run("loop trip is neither terminal nor origin at its start", func(t *testing.T) {
		if d := directionAt(t, pool, "de:11000:900000001::2", "100", "Alpha"); d.Terminal || d.Origin {
			t.Errorf("loop bus at Alpha: %+v, want neither", d)
		}
		if d := directionAt(t, pool, "de:11000:900000003::1", "S41", "Ringbahn S 41"); !d.Ring || d.Terminal || d.Mode != "sbahn" {
			t.Errorf("S41 at Gamma: %+v, want ring sbahn, not terminal", d)
		}
		if d := directionAt(t, pool, "de:11000:900000001::2", "100", "Alpha"); d.Ring {
			t.Errorf("bus 100: %+v, want not ring", d)
		}
	})

	t.Run("modes, headsigns and trip counts", func(t *testing.T) {
		const alpha1, beta1, beta2 = "de:11000:900000001::1", "de:11000:900000002::1", "de:11000:900000002::2"
		for line, want := range map[string]string{"M10": "tram", "F10": "ferry", "RE1": "regional", "X": "other"} {
			var mode string
			if err := pool.QueryRow(ctx, `SELECT mode FROM gtfs_staging.pole_directions WHERE stop_id = $1 AND line = $2`, beta2, line).Scan(&mode); err != nil || mode != want {
				t.Errorf("%s: mode %q (%v), want %s", line, mode, err, want)
			}
		}
		if d := directionAt(t, pool, "de:11000:900000002::3", "200", "Epsilon"); d.Mode != "bus" {
			t.Errorf("route_type 3: mode %q, want bus", d.Mode)
		}
		if d := directionAt(t, pool, alpha1, "U8", "Gamma"); d.Display != "Gamma Süd" || d.TripCount != 15 {
			t.Errorf("U8 at Alpha: %+v, want display \"Gamma Süd\" (2 of 3), 3 trips × 5 days", d)
		}
		if d := directionAt(t, pool, beta1, "U8", "Gamma"); d.Display != "Gamma" {
			t.Errorf("U8 at Beta: display %q, want the trip headsign", d.Display)
		}
		// Empty trip_headsign → name of the last stop; EXTRA runs on one date.
		if d := directionAt(t, pool, beta2, "F10", "Gamma"); d.TripCount != 1 {
			t.Errorf("ferry without headsign: %+v, want headsign Gamma, 1 instance", d)
		}
		// Trip after midnight (24:00) is loaded and counted like any other.
		if d := directionAt(t, pool, beta2, "M10", "Gamma"); d.TripCount != 5 {
			t.Errorf("tram after midnight: %+v, want 5 instances", d)
		}
	})

	t.Run("stations: grouping, name, location, modes", func(t *testing.T) {
		type station struct {
			Name      string
			Lat, Lon  float64
			Modes     []string
			InScope   bool
			PoleCount int32
		}
		get := func(id string) station {
			var s station
			if err := pool.QueryRow(ctx, `SELECT name, round(ST_Y(geom::geometry)::numeric, 6)::float8, round(ST_X(geom::geometry)::numeric, 6)::float8, modes, in_scope, pole_count
				FROM gtfs_staging.stations WHERE station_id = $1`, id).Scan(&s.Name, &s.Lat, &s.Lon, &s.Modes, &s.InScope, &s.PoleCount); err != nil {
				t.Fatalf("station %s: %v", id, err)
			}
			return s
		}
		// Station row exists: its name and location; ::2 joins via DHID prefix.
		if s := get("de:11000:900000001"); s.Name != "Alpha (Berlin)" || s.Lat != 52.5 || s.Lon != 13.4 ||
			!slices.Equal(s.Modes, []string{"bus", "ubahn"}) || s.PoleCount != 2 || !s.InScope {
			t.Errorf("Alpha: %+v", s)
		}
		// Prefix-only station: most common pole name, centroid of its poles.
		if s := get("de:11000:900000002"); s.Name != "Beta" || s.Lat != 52.513333 || s.Lon != 13.42 ||
			!slices.Equal(s.Modes, []string{"bus", "ferry", "other", "regional", "sbahn", "tram", "ubahn"}) || s.PoleCount != 3 {
			t.Errorf("Beta: %+v", s)
		}
		// Never served: a station with no modes.
		if s := get("de:11000:900000004"); len(s.Modes) != 0 || s.PoleCount != 1 {
			t.Errorf("Delta: %+v", s)
		}
		if n := scalar[int64](t, pool, `SELECT count(*) FROM gtfs_staging.pole_directions WHERE stop_id = 'de:11000:900000004::1'`); n != 0 {
			t.Errorf("unserved pole has %d directions", n)
		}
	})
}

// TestWeddingSubset loads the real-data fixture around U Wedding (PLAN 1.5)
// and checks it against the trimmed GTFS-RT samples.
func TestWeddingSubset(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	loader := NewLoader(pool, testScope(t), slog.New(slog.DiscardHandler))
	if _, err := loader.Load(ctx, filepath.Join("..", "..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip")); err != nil {
		t.Fatal(err)
	}
	const wedding = "de:11000:900009104"

	t.Run("bus poles without parent_station belong to S+U Wedding", func(t *testing.T) {
		for _, n := range []string{"3", "4", "5", "6", "8"} {
			id := wedding + "::" + n
			if got := scalar[string](t, pool, `SELECT station_id FROM gtfs_staging.stops WHERE stop_id = $1`, id); got != wedding {
				t.Errorf("%s → station %s, want %s", id, got, wedding)
			}
		}
		modes := scalar[[]string](t, pool, `SELECT modes FROM gtfs_staging.stations WHERE station_id = $1`, wedding)
		for _, m := range []string{"bus", "sbahn", "ubahn"} {
			if !slices.Contains(modes, m) {
				t.Errorf("S+U Wedding modes %v, missing %s", modes, m)
			}
		}
	})

	t.Run("S Wedding/Martha-Ndumbe-Platz is a separate station", func(t *testing.T) {
		name := scalar[string](t, pool, `SELECT name FROM gtfs_staging.stations WHERE station_id = 'de:11000:900008103'`)
		if !strings.Contains(name, "Martha-Ndumbe-Platz") {
			t.Errorf("de:11000:900008103 name %q", name)
		}
	})

	t.Run("U6 passes through, ring lines are rings", func(t *testing.T) {
		if n := scalar[int64](t, pool, `SELECT count(*) FROM gtfs_staging.pole_directions
			WHERE station_id = $1 AND line = 'U6' AND mode = 'ubahn' AND NOT is_terminal AND NOT is_origin`, wedding); n < 2 {
			t.Errorf("U6 at Wedding: %d through directions, want both ways", n)
		}
		if n := scalar[int64](t, pool, `SELECT count(*) FROM gtfs_staging.pole_directions
			WHERE station_id = $1 AND line IN ('S41', 'S42') AND mode = 'sbahn' AND NOT is_ring`, wedding); n != 0 {
			t.Errorf("%d S41/S42 directions at Wedding not marked as ring", n)
		}
	})

	t.Run("every trip of the GTFS-RT fixtures is in the subset", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "gtfsrt", "*-wedding.pb"))
		if err != nil || len(files) == 0 {
			t.Fatalf("RT fixtures: %v (%d files)", err, len(files))
		}
		var ids []string
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var msg gtfs.FeedMessage
			if err := proto.Unmarshal(raw, &msg); err != nil {
				t.Fatal(err)
			}
			for _, e := range msg.GetEntity() {
				ids = append(ids, e.GetTripUpdate().GetTrip().GetTripId())
			}
		}
		missing := scalar[int64](t, pool, `SELECT count(*) FROM unnest($1::text[]) AS id
			WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.trips t WHERE t.trip_id = id)`, ids)
		if missing != 0 {
			t.Errorf("%d of %d RT trip ids are not in the static subset", missing, len(ids))
		}
	})
}
