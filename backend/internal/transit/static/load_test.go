package static

import (
	"archive/zip"
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/geo"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// miniFeed is a small but realistic GTFS feed: VBB-style quoting, a BOM in
// stops.txt, a single-digit hour, a trip after midnight, an optional column
// left out (routes has no route_sort_order), and empty optional values.
func miniFeed() map[string]string {
	return map[string]string{
		"agency.txt": `"agency_id","agency_name","agency_url","agency_timezone","agency_lang","agency_phone"
796,"Berliner Verkehrsbetriebe","https://www.bvg.de","Europe/Berlin","de",""
`,
		"routes.txt": `"route_id","agency_id","route_short_name","route_long_name","route_type","route_color","route_text_color","route_desc"
"17521_400",796,"U6","",400,"8C6DAB","FFFFFF",""
"17457_700",796,"N6","",700,"","",""
`,
		"trips.txt": `"route_id","service_id","trip_id","trip_headsign","trip_short_name","direction_id","block_id","shape_id","wheelchair_accessible","bikes_allowed"
"17521_400",1,1001,"Alt-Mariendorf (Berlin)","",0,,3,1,0
"17457_700",1,1002,"Alt-Tegel (Berlin)","",1,,,0,0
`,
		"stops.txt": string(rune(0xFEFF)) + `"stop_id","stop_code","stop_name","stop_desc","stop_lat","stop_lon","location_type","parent_station","wheelchair_boarding","platform_code","zone_id","level_id"
"de:11000:900009104","","S+U Wedding (Berlin)","","52.542730000000","13.366060000000",1,,0,"","",""
"de:11000:900009104::1","","S+U Wedding (Berlin)","","52.543030000000","13.364990000000",0,"de:11000:900009104",0,"1","",""
"de:11000:900009104::3","","S+U Wedding (Berlin)","","52.542920000000","13.365580000000",0,,0,"Pos. 5","",""
"000300158001","","S+U Wedding (Berlin)","","52.542170000000","13.366270000000",2,"de:11000:900009104",0,"","",""
"000000999001","","Numeric pole inside the bbox","","52.500000000000","13.400000000000",0,,0,"","",""
"de:12054:900230999","","Potsdam station (outside the bbox)","","52.390000000000","13.060000000000",1,,0,"","",""
"de:12054:900230999::1","","Potsdam station (outside the bbox)","","52.390000000000","13.060000000000",0,"de:12054:900230999",0,"","",""
`,
		"calendar.txt": `"service_id","monday","tuesday","wednesday","thursday","friday","saturday","sunday","start_date","end_date"
1,1,1,1,1,1,0,0,20261105,20271211
`,
		"calendar_dates.txt": `"service_id","date","exception_type"
1,20261224,2
1,20261227,1
`,
		"stop_times.txt": `trip_id,stop_id,stop_sequence,pickup_type,drop_off_type,stop_headsign,arrival_time,departure_time
1001,"de:11000:900009104::1",0,0,0,"",9:20:00,9:20:00
1001,"de:11000:900009104::3",1,0,1,"",,
1002,"de:11000:900009104::3",0,0,0,"",25:10:00,25:11:00
`,
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gtfs.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// Sorted, so the same files always give the same bytes (and SHA-256).
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// testScope is the default Berlin bbox from config.
func testScope(t *testing.T) geo.BBox {
	t.Helper()
	b, err := geo.ParseBBox(config.DefaultScopeBBox)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := newTestDBWithURL(t)
	return pool
}

// newTestDBWithURL starts a migrated database and also returns its URL, for
// tests that need a second pool.
func newTestDBWithURL(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	url := pgtest.Start(t)
	pool, err := platform.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return url, pool
}

func scalar[T any](t *testing.T, pool *pgxpool.Pool, query string, args ...any) T {
	t.Helper()
	var v T
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func TestLoad(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	loader := NewLoader(pool, testScope(t), slog.New(slog.DiscardHandler))

	t.Run("loads every table with converted values", func(t *testing.T) {
		res, err := loader.Load(ctx, writeZip(t, miniFeed()))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int64{"agency": 1, "routes": 2, "trips": 2, "stops": 7, "calendar": 1, "calendar_dates": 2, "stop_times": 3}
		for table, n := range want {
			if res.Rows[table] != n {
				t.Errorf("%s: %d rows, want %d", table, res.Rows[table], n)
			}
		}

		// Times: raw text kept, seconds computed, empty → NULL.
		if got := scalar[int32](t, pool, `SELECT departure_secs FROM gtfs_staging.stop_times WHERE trip_id='1001' AND stop_sequence=0`); got != 9*3600+20*60 {
			t.Errorf("9:20:00 → %d", got)
		}
		if got := scalar[string](t, pool, `SELECT arrival_time || '/' || arrival_secs FROM gtfs_staging.stop_times WHERE trip_id='1002'`); got != "25:10:00/90600" {
			t.Errorf("after midnight: %s", got)
		}
		if got := scalar[bool](t, pool, `SELECT arrival_time IS NULL AND arrival_secs IS NULL FROM gtfs_staging.stop_times WHERE trip_id='1001' AND stop_sequence=1`); !got {
			t.Error("empty time must be NULL")
		}

		// BOM header parsed; geometry generated from lat/lon; empty text → NULL.
		if got := scalar[string](t, pool, `SELECT round(ST_Y(geom::geometry)::numeric, 5) || ',' || round(ST_X(geom::geometry)::numeric, 5) FROM gtfs_staging.stops WHERE stop_id='de:11000:900009104::3'`); got != "52.54292,13.36558" {
			t.Errorf("geom: %s", got)
		}
		if got := scalar[bool](t, pool, `SELECT parent_station IS NULL AND stop_code IS NULL FROM gtfs_staging.stops WHERE stop_id='de:11000:900009104::3'`); !got {
			t.Error("empty optional text must be NULL")
		}
		if got := scalar[string](t, pool, `SELECT platform_code FROM gtfs_staging.stops WHERE stop_id='de:11000:900009104::3'`); got != "Pos. 5" {
			t.Errorf("platform_code %q", got)
		}

		// Missing optional column → NULL; ints, booleans and dates converted.
		if got := scalar[bool](t, pool, `SELECT route_sort_order IS NULL AND route_type = 400 FROM gtfs_staging.routes WHERE route_id='17521_400'`); !got {
			t.Error("routes: route_sort_order must be NULL, route_type 400")
		}
		if got := scalar[string](t, pool, `SELECT saturday || ' ' || start_date || ' ' || end_date FROM gtfs_staging.calendar`); got != "false 2026-11-05 2027-12-11" {
			t.Errorf("calendar: %s", got)
		}
	})

	t.Run("scope flags and station grouping", func(t *testing.T) {
		res, err := loader.Load(ctx, writeZip(t, miniFeed()))
		if err != nil {
			t.Fatal(err)
		}
		inScope := func(id string) bool {
			return scalar[bool](t, pool, `SELECT in_scope FROM gtfs_staging.stops WHERE stop_id = $1`, id)
		}
		for id, want := range map[string]bool{
			"de:11000:900009104::1": true,  // pole in the bbox
			"de:11000:900009104::3": true,  // pole in the bbox, no parent_station
			"de:11000:900009104":    true,  // station with a pole in scope
			"000000999001":          true,  // non-DHID pole in the bbox
			"000300158001":          false, // entrance: never in scope itself
			"de:12054:900230999::1": false, // Potsdam pole, west of 13.088
			"de:12054:900230999":    false, // station without a pole in scope
		} {
			if got := inScope(id); got != want {
				t.Errorf("in_scope(%s) = %v, want %v", id, got, want)
			}
		}
		// ::1 (parent) and ::3 (DHID prefix) are one station; the numeric pole is its own.
		if res.InScopePoles != 3 || res.InScopeStations != 2 {
			t.Errorf("in scope: %d poles, %d stations; want 3, 2", res.InScopePoles, res.InScopeStations)
		}
	})

	t.Run("keys and indexes exist", func(t *testing.T) {
		got := scalar[[]string](t, pool, `SELECT array_agg(indexname ORDER BY indexname) FROM pg_indexes WHERE schemaname = 'gtfs_staging'`)
		for _, want := range []string{
			"stop_times_pkey", "stop_times_stop_departure_idx", "stops_pkey", "stops_geom_idx", "stops_parent_idx",
			"trips_pkey", "trips_service_idx", "trips_route_idx", "routes_pkey", "calendar_pkey", "calendar_dates_pkey",
		} {
			if !slices.Contains(got, want) {
				t.Errorf("index %s missing; have %v", want, got)
			}
		}
		if scalar[int64](t, pool, `SELECT count(*) FROM pg_stats WHERE schemaname = 'gtfs_staging' AND tablename = 'stop_times'`) == 0 {
			t.Error("stop_times not analyzed")
		}
	})

	t.Run("duplicate stop time fails the load", func(t *testing.T) {
		feed := miniFeed()
		feed["stop_times.txt"] += "1001,\"de:11000:900009104::3\",1,0,0,\"\",9:25:00,9:25:00\n"
		if _, err := loader.Load(ctx, writeZip(t, feed)); err == nil || !strings.Contains(err.Error(), "stop_times_pkey") {
			t.Fatalf("err = %v, want a stop_times_pkey violation", err)
		}
	})

	t.Run("reload replaces the staging schema", func(t *testing.T) {
		feed := miniFeed()
		feed["calendar_dates.txt"] = "service_id,date,exception_type\n1,20261224,2\n"
		if _, err := loader.Load(ctx, writeZip(t, feed)); err != nil {
			t.Fatal(err)
		}
		if n := scalar[int64](t, pool, `SELECT count(*) FROM gtfs_staging.calendar_dates`); n != 1 {
			t.Fatalf("calendar_dates: %d rows after reload, want 1", n)
		}
	})

	t.Run("bad value fails with file and line, previous staging stays", func(t *testing.T) {
		feed := miniFeed()
		feed["stop_times.txt"] = strings.Replace(feed["stop_times.txt"], "25:10:00", "25:1O:00", 1)
		_, err := loader.Load(ctx, writeZip(t, feed))
		if err == nil || !strings.Contains(err.Error(), "stop_times.txt line 4") || !strings.Contains(err.Error(), "arrival_time") {
			t.Fatalf("err = %v, want stop_times.txt line 4 arrival_time", err)
		}
		if n := scalar[int64](t, pool, `SELECT count(*) FROM gtfs_staging.calendar_dates`); n != 1 {
			t.Fatalf("previous staging changed: %d calendar_dates", n)
		}
	})

	t.Run("missing required column", func(t *testing.T) {
		feed := miniFeed()
		feed["routes.txt"] = "route_id,route_short_name\nx,U6\n"
		if _, err := loader.Load(ctx, writeZip(t, feed)); err == nil || !strings.Contains(err.Error(), "required column route_type missing") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		feed := miniFeed()
		delete(feed, "calendar.txt")
		if _, err := loader.Load(ctx, writeZip(t, feed)); err == nil || !strings.Contains(err.Error(), "calendar.txt") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("not a zip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.zip")
		if err := os.WriteFile(path, []byte("<html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loader.Load(ctx, path); err == nil {
			t.Fatal("want error for a non-zip file")
		}
	})
}

// TestLoadRealFeed imports a real VBB zip and checks the PLAN 1.3 acceptance:
// import finishes, row counts logged, memory of this process < 500 MB.
//
//	$env:LIVESTOP_GTFS_ZIP='data\gtfs-www\GTFS.zip'; go -C backend test -run LoadRealFeed -v ./internal/transit/static/
func TestLoadRealFeed(t *testing.T) {
	zipPath := os.Getenv("LIVESTOP_GTFS_ZIP")
	if zipPath == "" {
		t.Skip("set LIVESTOP_GTFS_ZIP to a VBB GTFS zip")
	}
	if !filepath.IsAbs(zipPath) {
		zipPath = filepath.Join("..", "..", "..", zipPath) // relative to backend/
	}
	pool := newTestDB(t)
	loader := NewLoader(pool, testScope(t), slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var peak uint64
	done := make(chan struct{})
	go func() {
		var ms runtime.MemStats
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.Sys)
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()

	start := time.Now()
	res, err := loader.Load(context.Background(), zipPath)
	close(done)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded in %s, peak Go memory (runtime Sys) %d MB, rows %v", time.Since(start).Round(time.Second), peak>>20, res.Rows)
	t.Logf("in scope: %d poles, %d stations", res.InScopePoles, res.InScopeStations)
	if peak > 500<<20 {
		t.Errorf("peak memory %d MB, want < 500 MB", peak>>20)
	}
	if res.Rows["stop_times"] < 1_000_000 {
		t.Errorf("stop_times: %d rows, want > 1 M", res.Rows["stop_times"])
	}

	// Plausibility (PLAN 1.4): in-scope stations by municipality (AGS, the
	// second DHID part; 11000 = Berlin) and by mode.
	rows, err := pool.Query(context.Background(), `
		WITH poles AS (
			SELECT s.stop_id, s.station_id AS station
			FROM gtfs_staging.stops s
			WHERE s.in_scope AND coalesce(s.location_type, 0) = 0
		)
		SELECT CASE WHEN station LIKE 'de:%' THEN split_part(station, ':', 2) ELSE '(no DHID)' END AS ags,
		       count(DISTINCT station), count(*)
		FROM poles GROUP BY 1 ORDER BY 2 DESC LIMIT 8`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ags string
		var stations, poles int64
		if err := rows.Scan(&ags, &stations, &poles); err != nil {
			t.Fatal(err)
		}
		t.Logf("  AGS %-10s stations=%5d poles=%5d", ags, stations, poles)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("in-scope poles served by mode: %s", scalar[string](t, pool, `
		SELECT string_agg(rt || '=' || n, ', ' ORDER BY n DESC) FROM (
			SELECT r.route_type AS rt, count(DISTINCT st.stop_id) AS n
			FROM gtfs_staging.stop_times st
			JOIN gtfs_staging.stops s ON s.stop_id = st.stop_id AND s.in_scope
			JOIN gtfs_staging.trips t ON t.trip_id = st.trip_id
			JOIN gtfs_staging.routes r ON r.route_id = t.route_id
			GROUP BY 1) x`))
	t.Logf("pole_directions: %s", scalar[string](t, pool, `
		SELECT format('%s rows, terminal %s, origin %s, ring %s, display <> headsign %s, in-scope stations with directions %s',
			count(*), count(*) FILTER (WHERE is_terminal), count(*) FILTER (WHERE is_origin), count(*) FILTER (WHERE is_ring),
			count(*) FILTER (WHERE display_headsign <> headsign),
			(SELECT count(*) FROM gtfs_staging.stations WHERE in_scope AND modes <> '{}'))
		FROM gtfs_staging.pole_directions`))
	t.Logf("in-scope poles without any stop_times: %d", scalar[int64](t, pool, `
		SELECT count(*) FROM gtfs_staging.stops s
		WHERE s.in_scope AND coalesce(s.location_type, 0) = 0
		  AND NOT EXISTS (SELECT 1 FROM gtfs_staging.stop_times st WHERE st.stop_id = s.stop_id)`))
}
