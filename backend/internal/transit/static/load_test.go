package static

import (
	"archive/zip"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
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
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
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

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return pool
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
	loader := NewLoader(pool, slog.New(slog.DiscardHandler))

	t.Run("loads every table with converted values", func(t *testing.T) {
		counts, err := loader.Load(ctx, writeZip(t, miniFeed()))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int64{"agency": 1, "routes": 2, "trips": 2, "stops": 3, "calendar": 1, "calendar_dates": 2, "stop_times": 3}
		for table, n := range want {
			if counts[table] != n {
				t.Errorf("%s: %d rows, want %d", table, counts[table], n)
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
	loader := NewLoader(pool, slog.New(slog.NewTextHandler(os.Stderr, nil)))

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
	counts, err := loader.Load(context.Background(), zipPath)
	close(done)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded in %s, peak Go memory (runtime Sys) %d MB, rows %v", time.Since(start).Round(time.Second), peak>>20, counts)
	if peak > 500<<20 {
		t.Errorf("peak memory %d MB, want < 500 MB", peak>>20)
	}
	if counts["stop_times"] < 1_000_000 {
		t.Errorf("stop_times: %d rows, want > 1 M", counts["stop_times"])
	}
}
