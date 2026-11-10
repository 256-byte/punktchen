// Command nearbyload measures /v1/nearby under simulated app clients (PLAN
// task 5.5). It is a developer tool, not part of the server binary. Each
// client stands at a random point around a centre and asks like the app
// does when standing still: every 30 s, with the ETag of its last answer
// and Accept-Encoding: gzip. It only talks to our own backend. Run it from
// backend/ against the Compose stack:
//
//	go run ./internal/tools/nearbyload -users 1000 -spread 300   # a crowd at one place
//	go run ./internal/tools/nearbyload -users 1000 -spread 3000  # the same users spread out
//
// The report has request counts by status, latency, transfer size and the
// cell cache's hit ratio (from the backend's /metrics before and after).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

type options struct {
	url, metrics string
	users        int
	duration     time.Duration
	interval     time.Duration
	lat, lng     float64
	spread       float64
	seed         uint64
}

func main() {
	var o options
	var center string
	flag.StringVar(&o.url, "url", "http://localhost:8080", "backend base URL")
	flag.StringVar(&o.metrics, "metrics", "http://127.0.0.1:9090/metrics", "backend metrics URL (empty: no cache numbers)")
	flag.IntVar(&o.users, "users", 1000, "simulated clients")
	flag.DurationVar(&o.duration, "duration", 2*time.Minute, "how long to run")
	flag.DurationVar(&o.interval, "interval", 30*time.Second, "time between a client's requests (the app's refresh when standing still)")
	flag.StringVar(&center, "center", "52.5219,13.4132", "lat,lng the clients stand around (default: Alexanderplatz)")
	flag.Float64Var(&o.spread, "spread", 300, "clients are spread evenly over a disc of this radius in metres")
	flag.Uint64Var(&o.seed, "seed", 1, "random seed (positions and start times)")
	flag.Parse()

	var err error
	if o.lat, o.lng, err = parseCenter(center); err == nil {
		err = run(context.Background(), o, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "nearbyload:", err)
		os.Exit(1)
	}
}

func parseCenter(s string) (lat, lng float64, err error) {
	a, b, ok := strings.Cut(s, ",")
	if ok {
		if lat, err = strconv.ParseFloat(strings.TrimSpace(a), 64); err == nil {
			lng, err = strconv.ParseFloat(strings.TrimSpace(b), 64)
		}
	}
	if !ok || err != nil {
		return 0, 0, fmt.Errorf("center %q: want lat,lng", s)
	}
	return lat, lng, nil
}

// sample is one finished request.
type sample struct {
	status  int // 0: transport error
	bytes   int // body as transferred (gzip)
	latency time.Duration
}

func run(ctx context.Context, o options, out io.Writer) error {
	if o.users <= 0 || o.duration <= 0 || o.interval <= 0 {
		return fmt.Errorf("users, duration and interval must be positive")
	}
	rng := rand.New(rand.NewPCG(o.seed, o.seed))
	client := &http.Client{
		Timeout: 20 * time.Second,
		// A bounded pool: the clients are spread over the interval, so they
		// need far fewer connections than there are clients.
		Transport: &http.Transport{MaxIdleConnsPerHost: 128, MaxConnsPerHost: 128},
	}

	before, err := cacheCounters(ctx, client, o.metrics)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, o.duration)
	defer cancel()

	cells := map[string]bool{}
	var mu sync.Mutex
	var samples []sample
	var wg sync.WaitGroup
	start := time.Now()
	for range o.users {
		lat, lng := pointInDisc(rng, o.lat, o.lng, o.spread)
		cells[geo.CellAt(lat, lng).Hash] = true
		delay := time.Duration(rng.Int64N(int64(o.interval)))
		url := fmt.Sprintf("%s/v1/nearby?lat=%.6f&lng=%.6f", strings.TrimSuffix(o.url, "/"), lat, lng)
		wg.Go(func() {
			own := simulate(ctx, client, url, delay, o.interval)
			mu.Lock()
			samples = append(samples, own...)
			mu.Unlock()
		})
	}
	wg.Wait()
	elapsed := time.Since(start)

	// The parent context: ctx has expired by now.
	after, err := cacheCounters(context.WithoutCancel(ctx), client, o.metrics)
	if err != nil {
		return err
	}
	report(out, o, samples, len(cells), elapsed, before, after)
	return nil
}

// simulate is one client: first request after delay, then every interval
// until ctx ends, revalidating with the last ETag.
func simulate(ctx context.Context, client *http.Client, url string, delay, interval time.Duration) []sample {
	var out []sample
	etag := ""
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return out
		case <-timer.C:
		}
		s, newETag := request(ctx, client, url, etag)
		if ctx.Err() != nil {
			return out // cut off by the end of the run: not a result
		}
		out = append(out, s)
		if newETag != "" {
			etag = newETag
		}
		timer.Reset(interval)
	}
}

func request(ctx context.Context, client *http.Client, url, etag string) (sample, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sample{}, ""
	}
	req.Header.Set("Accept-Encoding", "gzip") // set by hand: the body stays compressed, as transferred
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return sample{latency: time.Since(start)}, ""
	}
	defer func() { _ = resp.Body.Close() }()
	n, _ := io.Copy(io.Discard, resp.Body)
	return sample{status: resp.StatusCode, bytes: int(n), latency: time.Since(start)}, resp.Header.Get("ETag")
}

// pointInDisc returns a point spread evenly over the disc of radius metres
// around the centre.
func pointInDisc(rng *rand.Rand, lat, lng, radius float64) (float64, float64) {
	const metresPerDegree = 111_195
	r := radius * math.Sqrt(rng.Float64())
	angle := rng.Float64() * 2 * math.Pi
	return lat + r*math.Cos(angle)/metresPerDegree,
		lng + r*math.Sin(angle)/(metresPerDegree*math.Cos(lat*math.Pi/180))
}

// cacheCounters reads livestop_cache_hits_total by result; nil without a URL.
func cacheCounters(ctx context.Context, client *http.Client, url string) (map[string]float64, error) {
	if url == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w (use -metrics \"\" to skip the cache numbers)", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return parseCacheCounters(resp.Body)
}

func parseCacheCounters(r io.Reader) (map[string]float64, error) {
	const prefix = `livestop_cache_hits_total{result="`
	out := map[string]float64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), prefix)
		if !ok {
			continue
		}
		result, value, ok := strings.Cut(rest, `"} `)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: %q: %w", sc.Text(), err)
		}
		out[result] = v
	}
	return out, sc.Err()
}

// percentile returns the p-quantile (0 < p ≤ 1) of sorted durations by the
// nearest-rank method; 0 for none.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

func report(out io.Writer, o options, samples []sample, cells int, elapsed time.Duration, before, after map[string]float64) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format, args...) }
	byStatus := map[int][]sample{}
	total := 0
	for _, s := range samples {
		byStatus[s.status] = append(byStatus[s.status], s)
		total += s.bytes
	}
	p("%d clients within %.0f m of %.4f,%.4f (%d cells), one request every %s, for %s\n",
		o.users, o.spread, o.lat, o.lng, cells, o.interval, elapsed.Round(time.Second))
	if len(samples) == 0 {
		p("no requests finished\n")
		return
	}
	p("requests   %6d  (%.1f/s; %.1f per client and minute)\n", len(samples),
		float64(len(samples))/elapsed.Seconds(), float64(len(samples))/float64(o.users)/elapsed.Minutes())

	statuses := make([]int, 0, len(byStatus))
	for st := range byStatus {
		statuses = append(statuses, st)
	}
	slices.Sort(statuses)
	for _, st := range statuses {
		ss := byStatus[st]
		lat := make([]time.Duration, len(ss))
		sizes := make([]int, len(ss))
		for i, s := range ss {
			lat[i], sizes[i] = s.latency, s.bytes
		}
		slices.Sort(lat)
		slices.Sort(sizes)
		name := strconv.Itoa(st)
		if st == 0 {
			name = "failed"
		}
		p("  %-6s   %6d  %5.1f %%  latency p50 %s, p95 %s, max %s; body median %d bytes\n", name, len(ss),
			100*float64(len(ss))/float64(len(samples)),
			percentile(lat, 0.5).Round(100*time.Microsecond), percentile(lat, 0.95).Round(100*time.Microsecond),
			lat[len(lat)-1].Round(100*time.Microsecond), sizes[len(sizes)/2])
	}
	p("transferred %.1f MB (gzip)\n", float64(total)/1e6)

	if after == nil {
		return
	}
	hit, shared, miss := after["hit"]-before["hit"], after["shared"]-before["shared"], after["miss"]-before["miss"]
	if lookups := hit + shared + miss; lookups > 0 {
		p("cell cache %6.0f lookups: %.0f hits, %.0f shared, %.0f builds → hit ratio %.1f %%\n",
			lookups, hit, shared, miss, 100*(hit+shared)/lookups)
		p("answered without a build: %.1f %% of all requests (cache hits + 304s)\n",
			100*(float64(len(samples))-miss)/float64(len(samples)))
	}
}
