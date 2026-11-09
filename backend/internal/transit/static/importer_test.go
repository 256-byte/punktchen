package static

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// rulesFeed's services run 2026-11-09 … 2026-11-20.
var testToday = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

var testLimits = SanityLimits{MinInScopePoles: 1, MinStopTimes: 1, MaxDrop: 0.5}

func zipBytes(t *testing.T, files map[string]string) string {
	t.Helper()
	b, err := os.ReadFile(writeZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type importEnv struct {
	t    *testing.T
	pool *pgxpool.Pool
	feed *fakeFeed
	im   *Importer
}

func newImportEnv(t *testing.T, pool *pgxpool.Pool, files map[string]string) *importEnv {
	t.Helper()
	feed := &fakeFeed{body: zipBytes(t, files)}
	srv := httptest.NewServer(feed)
	t.Cleanup(srv.Close)
	log := slog.New(slog.DiscardHandler)
	il := NewImportLog(pool)
	dl, err := NewDownloader(DownloaderConfig{URL: srv.URL, UserAgent: "livestop-test", Dir: t.TempDir()}, il, log)
	if err != nil {
		t.Fatal(err)
	}
	im := NewImporter(pool, dl, NewLoader(pool, testScope(t), log), il, testLimits, log)
	im.now = func() time.Time { return testToday }
	return &importEnv{t: t, pool: pool, feed: feed, im: im}
}

func (e *importEnv) serve(files map[string]string) {
	e.t.Helper()
	body := zipBytes(e.t, files)
	e.feed.set(func(f *fakeFeed) { f.body = body })
}

func (e *importEnv) liveStops() int64 {
	e.t.Helper()
	return scalar[int64](e.t, e.pool, `SELECT count(*) FROM gtfs.stops`)
}

func (e *importEnv) schemas() string {
	e.t.Helper()
	return scalar[string](e.t, e.pool, `SELECT coalesce(string_agg(nspname, ',' ORDER BY nspname), '') FROM pg_namespace WHERE nspname LIKE 'gtfs%'`)
}

func (e *importEnv) lastAttempt() (status, errMsg string) {
	e.t.Helper()
	var msg *string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT status, error FROM meta.feed_imports ORDER BY id DESC LIMIT 1`).Scan(&status, &msg); err != nil {
		e.t.Fatal(err)
	}
	if msg != nil {
		errMsg = *msg
	}
	return status, errMsg
}

// withExtraStop is rulesFeed plus one unserved stop, so its hash differs.
func withExtraStop(name string) map[string]string {
	f := rulesFeed()
	f["stops.txt"] += "de:11000:900000099::1," + name + ",52.5,13.4,0,\n"
	return f
}

func TestImporter(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	status := NewStatus(pool, SourceGTFS)

	t.Run("first import creates the live schema", func(t *testing.T) {
		if st, err := status.ScheduleState(ctx); err != nil || st.HasData || st.CheckedAt != nil {
			t.Fatalf("before any import: %+v, %v; want no data, never checked", st, err)
		}
		e := newImportEnv(t, pool, rulesFeed())
		out, err := e.im.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !out.Changed || out.Rows["stops"] != 10 || out.Rows[keyInScopePoles] == 0 {
			t.Fatalf("outcome %+v", out)
		}
		if s := e.schemas(); s != "gtfs" {
			t.Fatalf("schemas %q, want only gtfs (staging renamed, no old)", s)
		}
		if st, _ := e.lastAttempt(); st != StatusSucceeded {
			t.Fatalf("attempt %s", st)
		}
		counts := scalar[string](t, pool, `SELECT row_counts->>'in_scope_stations' FROM meta.feed_imports ORDER BY id DESC LIMIT 1`)
		if counts == "" || counts == "0" {
			t.Fatalf("row_counts.in_scope_stations = %q", counts)
		}
		if st, err := status.ScheduleState(ctx); err != nil || !st.HasData || st.CheckedAt == nil {
			t.Fatalf("after import: %+v, %v; want data and a check time", st, err)
		}
	})

	t.Run("unchanged feed is skipped and nothing swapped", func(t *testing.T) {
		e := newImportEnv(t, pool, rulesFeed())
		before := scalar[int64](t, pool, `SELECT oid::bigint FROM pg_namespace WHERE nspname = 'gtfs'`)
		out, err := e.im.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		after := scalar[int64](t, pool, `SELECT oid::bigint FROM pg_namespace WHERE nspname = 'gtfs'`)
		if out.Changed || before != after {
			t.Fatalf("changed=%v, schema oid %d → %d; want skipped, same schema", out.Changed, before, after)
		}
		if st, _ := e.lastAttempt(); st != StatusSkipped {
			t.Fatalf("attempt %s, want skipped", st)
		}
	})

	t.Run("changed feed replaces the live data", func(t *testing.T) {
		e := newImportEnv(t, pool, withExtraStop("Neu"))
		if _, err := e.im.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if n := e.liveStops(); n != 11 {
			t.Fatalf("live stops %d, want 11", n)
		}
		if s := e.schemas(); s != "gtfs" {
			t.Fatalf("schemas %q, want only gtfs", s)
		}
	})

	// From here on the live schema has 11 stops; every failure must keep it.
	failures := []struct {
		name    string
		files   func() map[string]string
		today   time.Time
		limits  *SanityLimits
		wantErr string
	}{
		{"sanity check fails", func() map[string]string {
			f := withExtraStop("Kaputt")
			f["stop_times.txt"] += "t1,de:11000:999999999::1,9,8:50:00,8:50:00,\n"
			return f
		}, testToday, nil, "stop_times with unknown stop_id"},
		{"not a zip", func() map[string]string { return nil }, testToday, nil, "open "},
		{"feed expired", func() map[string]string { return withExtraStop("Alt") }, time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC), nil, "no service on 2027-01-01"},
		{"feed shrinks too much", func() map[string]string {
			f := withExtraStop("Klein")
			lines := strings.SplitAfter(f["stop_times.txt"], "\n")
			f["stop_times.txt"] = strings.Join(lines[:8], "") // header + 7 of ~80 rows
			return f
		}, testToday, &SanityLimits{MinInScopePoles: 1, MinStopTimes: 1, MaxDrop: 0.5}, "stop_times dropped from"},
	}
	for _, tc := range failures {
		t.Run("failure keeps old data: "+tc.name, func(t *testing.T) {
			e := newImportEnv(t, pool, rulesFeed())
			if files := tc.files(); files != nil {
				e.serve(files)
			} else {
				e.feed.set(func(f *fakeFeed) { f.body = "<html>not a zip</html>" })
			}
			e.im.now = func() time.Time { return tc.today }
			if tc.limits != nil {
				e.im.limits = *tc.limits
			}
			_, err := e.im.Run(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if n := e.liveStops(); n != 11 {
				t.Fatalf("live stops %d after a failed import, want the old 11", n)
			}
			st, msg := e.lastAttempt()
			if st != StatusFailed || !strings.Contains(msg, tc.wantErr) {
				t.Fatalf("attempt %s %q", st, msg)
			}
		})
	}

	t.Run("second import while one runs", func(t *testing.T) {
		e := newImportEnv(t, pool, rulesFeed())
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, importLockKey); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, importLockKey) }()
		if _, err := e.im.Run(ctx); !errors.Is(err, ErrImportRunning) {
			t.Fatalf("err = %v, want ErrImportRunning", err)
		}
	})
}

// TestSanityChecks loads a broken feed into staging and runs check directly.
func TestSanityChecks(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	loader := NewLoader(pool, testScope(t), slog.New(slog.DiscardHandler))

	cases := []struct {
		name, want string
		mutate     func(f map[string]string)
	}{
		{"unknown trip", "stop_times with unknown trip_id", func(f map[string]string) {
			f["stop_times.txt"] += "nope,de:11000:900000003::1,0,8:00:00,8:00:00,\n"
		}},
		{"unknown route", "trips with unknown route_id", func(f map[string]string) {
			f["trips.txt"] += "R_NOPE,WK,tz,\"x\",0\n"
			f["stop_times.txt"] += "tz,de:11000:900000003::1,0,8:00:00,8:00:00,\n"
		}},
		{"service never runs", "trips whose service never runs", func(f map[string]string) {
			f["trips.txt"] += "R_U8,NEVER,tz,\"x\",0\n"
			f["stop_times.txt"] += "tz,de:11000:900000003::1,0,8:00:00,8:00:00,\n"
		}},
		{"trip without stop times", "trips without stop_times", func(f map[string]string) {
			f["trips.txt"] += "R_U8,WK,tz,\"x\",0\n"
		}},
		{"served pole without coordinates", "served poles without coordinates", func(f map[string]string) {
			f["stops.txt"] += "de:11000:900000098::1,Nowhere,,,0,\n"
			f["stop_times.txt"] += "t1,de:11000:900000098::1,9,8:50:00,8:50:00,\n"
		}},
		{"missing departure time", "stop_times without departure_time", func(f map[string]string) {
			f["stop_times.txt"] += "t1,de:11000:900000003::1,9,,,\n"
		}},
		{"too small", "poles in scope, want at least", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := rulesFeed()
			limits := testLimits
			if tc.mutate != nil {
				tc.mutate(f)
			} else {
				limits.MinInScopePoles = 1_000
			}
			res, err := loader.Load(ctx, writeZip(t, f))
			if err != nil {
				t.Fatal(err)
			}
			err = check(ctx, pool, res, nil, testToday, limits)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("check: %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("clean feed passes", func(t *testing.T) {
		res, err := loader.Load(ctx, writeZip(t, rulesFeed()))
		if err != nil {
			t.Fatal(err)
		}
		if err := check(ctx, pool, res, map[string]int64{"stop_times": res.Rows["stop_times"]}, testToday, testLimits); err != nil {
			t.Fatal(err)
		}
	})
}

// TestSwapWithoutFailedReads is the PLAN 1.6 acceptance test: readers query
// the live schema in a loop while imports swap it; no read may fail.
func TestSwapWithoutFailedReads(t *testing.T) {
	ctx := context.Background()
	url, pool := newTestDBWithURL(t)
	e := newImportEnv(t, pool, rulesFeed())
	if _, err := e.im.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// Readers use their own pool: prepared statements (pgx caches them per
	// connection) and plain queries, as the API will.
	readers, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(readers.Close)

	var reads, failed atomic.Int64
	var mu sync.Mutex
	seen := map[int64]bool{}
	var firstErr error
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				var stops, dirs int64
				err := readers.QueryRow(ctx, `SELECT (SELECT count(*) FROM gtfs.stops),
					(SELECT count(*) FROM gtfs.pole_directions pd JOIN gtfs.stations s ON s.station_id = pd.station_id)`).Scan(&stops, &dirs)
				reads.Add(1)
				mu.Lock()
				if err != nil {
					failed.Add(1)
					if firstErr == nil {
						firstErr = err
					}
				} else {
					seen[stops] = true
				}
				mu.Unlock()
			}
		}()
	}

	// Alternate two feeds (10 and 11 stops): every run is a full import + swap.
	feeds := []map[string]string{withExtraStop("A"), rulesFeed()}
	for i := 0; i < 6; i++ {
		e.serve(feeds[i%2])
		if _, err := e.im.Run(ctx); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("import %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	t.Logf("%d reads during 6 swaps, %d failed, stop counts seen %v", reads.Load(), failed.Load(), seen)
	if failed.Load() != 0 {
		t.Fatalf("%d of %d reads failed during swaps; first: %v", failed.Load(), reads.Load(), firstErr)
	}
	if !seen[10] || !seen[11] {
		t.Fatalf("readers saw %v, want both the old (10) and new (11) data", seen)
	}
	if s := e.schemas(); s != "gtfs" {
		t.Fatalf("schemas after swaps %q, want only gtfs", s)
	}
}

// TestImportRealFeed runs the whole pipeline on a real VBB zip with the
// production sanity limits: the checks must pass, a second run must skip.
//
//	$env:LIVESTOP_GTFS_ZIP='data/gtfs/GTFS.zip'; go -C backend test -run ImportRealFeed -v ./internal/transit/static/
func TestImportRealFeed(t *testing.T) {
	zipPath := os.Getenv("LIVESTOP_GTFS_ZIP")
	if zipPath == "" {
		t.Skip("set LIVESTOP_GTFS_ZIP to a VBB GTFS zip")
	}
	if !filepath.IsAbs(zipPath) {
		zipPath = filepath.Join("..", "..", "..", zipPath)
	}
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := newTestDB(t)
	feed := &fakeFeed{body: string(body)}
	srv := httptest.NewServer(feed)
	t.Cleanup(srv.Close)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	il := NewImportLog(pool)
	dl, err := NewDownloader(DownloaderConfig{URL: srv.URL, UserAgent: "livestop-test", Dir: t.TempDir()}, il, log)
	if err != nil {
		t.Fatal(err)
	}
	im := NewImporter(pool, dl, NewLoader(pool, testScope(t), log), il, DefaultSanityLimits, log)
	if day := os.Getenv("LIVESTOP_TODAY"); day != "" { // e.g. an old feed: LIVESTOP_TODAY=2026-11-01
		d, err := time.Parse(time.DateOnly, day)
		if err != nil {
			t.Fatal(err)
		}
		im.now = func() time.Time { return d.Add(12 * time.Hour) }
	}

	out, err := im.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("import %d: %v", out.ImportID, out.Rows)
	if s := scalar[string](t, pool, `SELECT string_agg(nspname, ',') FROM pg_namespace WHERE nspname LIKE 'gtfs%'`); s != "gtfs" {
		t.Errorf("schemas %q, want gtfs", s)
	}
	again, err := im.Run(context.Background())
	if err != nil || again.Changed {
		t.Fatalf("second run: %+v, %v; want skipped", again, err)
	}

	// PLAN 1.8: "Wedding 500 m" in < 20 ms on the full feed (warm, prepared).
	d, n := medianNearby(t, pool, wedding, 25)
	t.Logf("nearby Wedding 500 m: %d rows, median %s", n, d)
	if d > 20*time.Millisecond {
		t.Errorf("nearby query median %s, want < 20 ms", d)
	}
	alex := queries.NearbyPolesParams{Lat: 52.5219, Lng: 13.4132, RadiusM: 1000}
	d, n = medianNearby(t, pool, alex, 25)
	t.Logf("nearby Alexanderplatz 1000 m (max radius, densest area): %d rows, median %s", n, d)

	// PLAN 2.2: schedule window at 17:00 (whole window and a 1-minute slice,
	// the per-minute refresh) and just after midnight.
	y, m, dd := im.now().In(platform.Berlin).Date()
	evening := time.Date(y, m, dd, 17, 0, 0, 0, platform.Berlin)
	night := time.Date(y, m, dd, 0, 15, 0, 0, platform.Berlin)
	loader := departures.NewLoader(pool)
	for _, c := range []struct {
		name string
		from time.Time
		span time.Duration
	}{
		{"window 17:00 + 92 min", evening, 92 * time.Minute},
		{"slice 18:32 + 1 min", evening.Add(92 * time.Minute), time.Minute},
		{"window 00:15 + 92 min", night, 92 * time.Minute},
	} {
		var times []time.Duration
		var count int
		for range 5 {
			start := time.Now()
			w, err := loader.Load(context.Background(), c.from, c.from.Add(c.span))
			if err != nil {
				t.Fatal(err)
			}
			times = append(times, time.Since(start))
			count = len(w.Departures)
		}
		slices.Sort(times)
		t.Logf("%s: %d departures, median %s", c.name, count, times[len(times)/2])
	}
}
