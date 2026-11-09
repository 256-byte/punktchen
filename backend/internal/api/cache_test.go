package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

// testCache is a string cache with a hand-moved clock.
func testCache(t *testing.T, max int) (*cellCache[string], *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)
	return newCellCache[string](max, time.Minute, func() time.Time { return now }, prometheus.NewRegistry()), &now
}

// value builds v and stores it under key.
func value(v string, calls *int) func(context.Context) built[string] {
	return func(context.Context) built[string] { *calls++; return built[string]{val: v, key: v} }
}

func TestCellCacheHitTTLAndLRU(t *testing.T) {
	c, now := testCache(t, 2)
	ctx := context.Background()
	calls := 0
	get := func(key string) (string, cacheResult) {
		t.Helper()
		v, r, err := c.get(ctx, key, value(key, &calls))
		if err != nil || v != key {
			t.Fatalf("get %s: %q %v", key, v, err)
		}
		return v, r
	}

	if _, r := get("a"); r != cacheMiss {
		t.Errorf("first: %s", r)
	}
	if _, r := get("a"); r != cacheHit || calls != 1 {
		t.Errorf("second: %s, %d builds", r, calls)
	}

	*now = now.Add(time.Minute) // TTL over
	if _, r := get("a"); r != cacheMiss || calls != 2 {
		t.Errorf("after the TTL: %s, %d builds", r, calls)
	}

	get("b")
	get("a") // a is now the most recent
	get("c") // evicts b, the least recently used
	if _, r := get("a"); r != cacheHit {
		t.Errorf("a was used recently: %s", r)
	}
	if _, r := get("b"); r != cacheMiss {
		t.Errorf("b should have been evicted: %s", r)
	}
	if n := testutil.ToFloat64(c.entries); n != 2 {
		t.Errorf("entries %v", n)
	}
	if hits, misses := testutil.ToFloat64(c.hits.WithLabelValues("hit")), testutil.ToFloat64(c.hits.WithLabelValues("miss")); hits != 3 || misses != 5 {
		t.Errorf("hits %v, misses %v", hits, misses)
	}
}

func TestCellCacheStoreKey(t *testing.T) {
	c, _ := testCache(t, 10)
	ctx := context.Background()
	// Built from newer versions than looked up: stored under what it was built from.
	_, _, _ = c.get(ctx, "v1", func(context.Context) built[string] { return built[string]{val: "x", key: "v2"} })
	if _, r, _ := c.get(ctx, "v2", func(context.Context) built[string] { return built[string]{val: "y", key: "v2"} }); r != cacheHit {
		t.Errorf("v2: %s", r)
	}
	// Not stored: no key (a layer failed) or an error.
	for _, b := range []built[string]{{val: "z"}, {val: "z", key: "e", err: errors.New("boom")}} {
		key := fmt.Sprintf("n-%v", b.err)
		_, _, _ = c.get(ctx, key, func(context.Context) built[string] { return b })
		if _, r, _ := c.get(ctx, key, func(context.Context) built[string] { return b }); r != cacheMiss {
			t.Errorf("%+v was stored", b)
		}
	}
}

// TestCellCacheOneBuild: concurrent requests for one key wait for a single
// build; a waiter that gives up does not stop it.
func TestCellCacheOneBuild(t *testing.T) {
	c, _ := testCache(t, 10)
	release := make(chan struct{})
	var builds atomic.Int32
	build := func(ctx context.Context) built[string] {
		builds.Add(1)
		<-release
		if ctx.Err() != nil {
			return built[string]{err: ctx.Err()}
		}
		return built[string]{val: "v", key: "k"}
	}

	// The first request starts the build and then goes away.
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, _, err := c.get(ctx, "k", build); first <- err }()
	for builds.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()

	gaveUp, giveUp := context.WithCancel(context.Background())
	giveUp()
	if _, r, err := c.get(gaveUp, "k", build); r != cacheShared || !errors.Is(err, context.Canceled) {
		t.Errorf("waiter that gave up: %s %v", r, err)
	}

	var wg sync.WaitGroup
	results := make(chan string, 50)
	for range 50 {
		wg.Go(func() {
			v, _, err := c.get(context.Background(), "k", build)
			if err != nil {
				t.Error(err)
			}
			results <- v
		})
	}
	time.Sleep(20 * time.Millisecond) // let them queue up behind the build
	close(release)
	wg.Wait()
	close(results)
	for v := range results {
		if v != "v" {
			t.Errorf("got %q", v)
		}
	}
	if err := <-first; err != nil {
		t.Errorf("the build stopped with its request: %v", err)
	}
	if n := builds.Load(); n != 1 {
		t.Errorf("%d builds, want 1", n)
	}
}

func TestCellCacheBuildPanics(t *testing.T) {
	c, _ := testCache(t, 10)
	started, release := make(chan struct{}), make(chan struct{})
	waiter := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }()
		_, _, _ = c.get(context.Background(), "k", func(context.Context) built[string] {
			close(started)
			<-release
			panic("boom")
		})
	}()
	<-started
	go func() {
		_, _, err := c.get(context.Background(), "k", func(context.Context) built[string] { return built[string]{} })
		waiter <- err
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	if err := <-waiter; !errors.Is(err, errBuildFailed) {
		t.Errorf("waiter: %v, want errBuildFailed", err)
	}
}

// TestNearbyLoad is task 5.2's acceptance: 1000 concurrent requests from one
// cell cause one build; every one gets the same answer.
func TestNearbyLoad(t *testing.T) {
	fl := &fakeLayer{delay: 50 * time.Millisecond}
	reg := prometheus.NewRegistry()
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, fl); c.Metrics = reg })
	cell := geo.CellAt(52.5427, 13.366)

	// 100 clients with 10 requests each: Windows refuses 1000 simultaneous
	// connections to the test listener (its backlog is full).
	const clients = 100
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: clients}}
	bodies := make([]string, 1000)
	var wg sync.WaitGroup
	for c := range clients {
		wg.Go(func() {
			for i := c; i < len(bodies); i += clients {
				// Spread over the cell (±30 m north–south, ±20 m east–west).
				lat := cell.Lat + float64(i%61-30)*0.000009
				lng := cell.Lng + float64(i%41-20)*0.000015
				bodies[i] = fetch(t, client, fmt.Sprintf("%s/v1/nearby?lat=%f&lng=%f", srv.URL, lat, lng))
			}
		})
	}
	wg.Wait()

	if n := fl.calls(); n != 1 {
		t.Errorf("%d builds for one cell, want 1", n)
	}
	for i, b := range bodies {
		if b != bodies[0] {
			t.Fatalf("answer %d differs:\n%s\n%s", i, b, bodies[0])
		}
	}
	m := cacheHits(t, reg)
	if m["miss"] != 1 || m["hit"]+m["shared"] != 999 {
		t.Errorf("cache results %v", m)
	}
	t.Logf("1000 requests: %v", m)
}

// fetch returns the body of a 200 answer ("" after reporting an error).
func fetch(t *testing.T, client *http.Client, url string) string {
	resp, err := client.Get(url)
	if err != nil {
		t.Error(err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d: %s", resp.StatusCode, b)
	}
	return string(b)
}

// cacheHits reads livestop_cache_hits_total by result from reg.
func cacheHits(t *testing.T, reg *prometheus.Registry) map[string]float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() == "livestop_cache_hits_total" {
			for _, m := range mf.GetMetric() {
				out[m.GetLabel()[0].GetValue()] = m.GetCounter().GetValue()
			}
		}
	}
	return out
}

// TestNearbyCacheKeys: a new layer version builds again; a failing layer is
// not cached; generatedAt is the time of each response, not of the build.
func TestNearbyCacheKeys(t *testing.T) {
	fl := &fakeLayer{}
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, fl) })
	url := srv.URL + "/v1/nearby?lat=52.5427&lng=13.366"

	get(t, url)
	get(t, url+"&radius=450") // same bucket as 500
	if n := fl.calls(); n != 1 {
		t.Errorf("%d builds, want 1 (radius 450 → bucket 500)", n)
	}
	if q := fl.lastQuery(t); q.RadiusMeters != geo.CellAt(52.5427, 13.366).SearchRadius(500) {
		t.Errorf("radius %v, want the 500 bucket", q.RadiusMeters)
	}
	get(t, url+"&radius=550")
	if n := fl.calls(); n != 2 {
		t.Errorf("%d builds, want 2 (radius 550 → bucket 600)", n)
	}

	fl.setVersion(1794220476761) // new snapshot
	_, _, body := get(t, url)
	if n := fl.calls(); n != 3 {
		t.Errorf("%d builds after a new version, want 3", n)
	}
	lm := body["meta"].(map[string]any)["layers"].(map[string]any)["transit"].(map[string]any)
	if lm["version"] != float64(1794220476761) {
		t.Errorf("version %v", lm["version"])
	}

	down := &fakeLayer{err: errors.New("no snapshot")}
	srv2 := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, down) })
	get(t, srv2.URL+"/v1/nearby?lat=52.5427&lng=13.366")
	get(t, srv2.URL+"/v1/nearby?lat=52.5427&lng=13.366")
	if n := down.calls(); n != 2 {
		t.Errorf("failing layer asked %d times, want 2 (not cached)", n)
	}
}

func TestNearbyGeneratedAtFromCache(t *testing.T) {
	fl := &fakeLayer{}
	s := NewServer(Config{Version: "test", DB: fakePinger{}, Schedule: freshSchedule(), Layers: mustRegistry(t, fl),
		Transit: &fakeStations{}, Log: slog.New(slog.DiscardHandler)})
	var mu sync.Mutex
	now := testNow
	s.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	srv := httptest.NewServer(NewHandler(s))
	t.Cleanup(srv.Close)
	read := func() string {
		t.Helper()
		_, _, body := get(t, srv.URL+"/v1/nearby?lat=52.5427&lng=13.366")
		return body["meta"].(map[string]any)["generatedAt"].(string)
	}
	if got := read(); got != "2026-11-05T14:32:00+01:00" {
		t.Errorf("first: %s", got)
	}
	mu.Lock()
	now = now.Add(25 * time.Second)
	mu.Unlock()
	if got := read(); got != "2026-11-05T14:32:25+01:00" || fl.calls() != 1 {
		t.Errorf("from the cache: %s (%d builds); want the time of the response", got, fl.calls())
	}
}
