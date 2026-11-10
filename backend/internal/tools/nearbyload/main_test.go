package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

func TestPercentile(t *testing.T) {
	ms := func(n ...int) []time.Duration {
		out := make([]time.Duration, len(n))
		for i, v := range n {
			out[i] = time.Duration(v) * time.Millisecond
		}
		return out
	}
	d := ms(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	for p, want := range map[float64]time.Duration{0.5: 5 * time.Millisecond, 0.95: 10 * time.Millisecond, 0.1: time.Millisecond, 1: 10 * time.Millisecond} {
		if got := percentile(d, p); got != want {
			t.Errorf("p%v: %s, want %s", p, got, want)
		}
	}
	if percentile(nil, 0.5) != 0 || percentile(ms(7), 0.95) != 7*time.Millisecond {
		t.Error("empty or single sample")
	}
}

func TestParseCacheCounters(t *testing.T) {
	got, err := parseCacheCounters(strings.NewReader(`# HELP livestop_cache_hits_total ...
# TYPE livestop_cache_hits_total counter
livestop_cache_entries 12
livestop_cache_hits_total{result="hit"} 900
livestop_cache_hits_total{result="miss"} 1
livestop_cache_hits_total{result="shared"} 9.9e+01
`))
	if err != nil || got["hit"] != 900 || got["miss"] != 1 || got["shared"] != 99 || len(got) != 3 {
		t.Errorf("%v, %v", got, err)
	}
	if _, err := parseCacheCounters(strings.NewReader(`livestop_cache_hits_total{result="hit"} x`)); err == nil {
		t.Error("a bad value was accepted")
	}
}

func TestPointInDisc(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 1))
	inner := 0
	for range 2000 {
		lat, lng := pointInDisc(rng, 52.5219, 13.4132, 300)
		d := geo.DistanceMeters(52.5219, 13.4132, lat, lng)
		if d > 301 {
			t.Fatalf("point %.1f m from the centre", d)
		}
		if d < 150 {
			inner++
		}
	}
	// Evenly over the area: a quarter lies within half the radius.
	if inner < 400 || inner > 600 {
		t.Errorf("%d of 2000 points within half the radius, want about 500", inner)
	}
}

func TestParseCenter(t *testing.T) {
	if lat, lng, err := parseCenter(" 52.5219, 13.4132"); err != nil || lat != 52.5219 || lng != 13.4132 {
		t.Errorf("%v %v %v", lat, lng, err)
	}
	for _, bad := range []string{"", "52.5", "a,b", "52.5,x"} {
		if _, _, err := parseCenter(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestRun: clients revalidate with their ETag, and the report counts
// statuses and the cache counters' difference.
func TestRun(t *testing.T) {
	var hits atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			_, _ = fmt.Fprintf(w, "livestop_cache_hits_total{result=\"hit\"} %d\nlivestop_cache_hits_total{result=\"miss\"} 5\n", 100+hits.Load())
			return
		}
		if r.Header.Get("Accept-Encoding") != "gzip" || r.URL.Query().Get("lat") == "" {
			t.Errorf("request %s, Accept-Encoding %q", r.URL, r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("ETag", `W/"v1"`)
		if r.Header.Get("If-None-Match") == `W/"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	var out bytes.Buffer
	err := run(context.Background(), options{
		url: backend.URL, metrics: backend.URL + "/metrics", users: 4,
		duration: 350 * time.Millisecond, interval: 100 * time.Millisecond,
		lat: 52.5219, lng: 13.4132, spread: 10, seed: 1,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	report := out.String()
	for _, want := range []string{
		"4 clients within 10 m of 52.5219,13.4132 (1 cells)",
		"  200 ", "  304 ", "body median 11 bytes",
		"4 hits, 0 shared, 0 builds → hit ratio 100.0 %",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	if hits.Load() != 4 {
		t.Errorf("%d full answers, want one per client", hits.Load())
	}

	if err := run(context.Background(), options{users: 0, duration: time.Second, interval: time.Second}, &out); err == nil {
		t.Error("0 users accepted")
	}
	if err := run(context.Background(), options{url: backend.URL, metrics: "http://127.0.0.1:1/metrics", users: 1, duration: time.Second, interval: time.Second}, &out); err == nil {
		t.Error("unreachable metrics accepted")
	}
}
