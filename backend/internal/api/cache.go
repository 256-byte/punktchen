package api

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Cell cache of /v1/nearby (ADR-0006, task 5.2): answers keyed by (layers,
// cell, radius bucket, provider versions). A new provider version makes a
// new key, so nothing is ever invalidated; old entries fall out by LRU or
// TTL.
const (
	// DefaultCacheEntries bounds the cache (a Wedding answer is ~30 KB as JSON).
	DefaultCacheEntries = 1024
	// DefaultCacheTTL is a safety net: the transit snapshot gets a new
	// version at least every minute (window move), so entries rarely reach it.
	DefaultCacheTTL = time.Minute
	// cacheBuildTimeout bounds a shared build, which does not stop when the
	// request that started it goes away (others may wait for it).
	cacheBuildTimeout = 10 * time.Second
)

// cacheResult labels livestop_cache_hits_total.
type cacheResult string

const (
	cacheHit    cacheResult = "hit"    // answered from the cache
	cacheShared cacheResult = "shared" // waited for a build another request started
	cacheMiss   cacheResult = "miss"   // built the answer
)

// errBuildFailed is what requests waiting for a build get when it panicked
// (the panic itself goes to the request that ran it).
var errBuildFailed = errors.New("cell cache: build failed")

// built is what a cache build returns: the value, the key to store it under
// (the versions it was really built from; empty: do not store) and an
// error (never stored).
type built[V any] struct {
	val V
	key string
	err error
}

// cellCache is an LRU + TTL cache with one build per key at a time. Values
// must not be changed after they are stored.
type cellCache[V any] struct {
	max int
	ttl time.Duration
	now func() time.Time

	mu       sync.Mutex
	order    *list.List // front: most recently used *cacheEntry[V]
	items    map[string]*list.Element
	inflight map[string]*cacheCall[V]

	hits    *prometheus.CounterVec
	entries prometheus.Gauge
}

type cacheEntry[V any] struct {
	key     string
	val     V
	expires time.Time
}

type cacheCall[V any] struct {
	done chan struct{}
	res  built[V]
}

// newCellCache returns a cache of up to max entries that live ttl; reg
// (nil: none) gets livestop_cache_hits_total{result} and
// livestop_cache_entries.
func newCellCache[V any](max int, ttl time.Duration, now func() time.Time, reg prometheus.Registerer) *cellCache[V] {
	c := &cellCache[V]{
		max: max, ttl: ttl, now: now,
		order: list.New(), items: map[string]*list.Element{}, inflight: map[string]*cacheCall[V]{},
		hits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "livestop_cache_hits_total",
			Help: "/v1/nearby cell cache lookups by result: hit, shared (waited for another request's build), miss (built).",
		}, []string{"result"}),
		entries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "livestop_cache_entries",
			Help: "Answers in the /v1/nearby cell cache.",
		}),
	}
	for _, r := range []cacheResult{cacheHit, cacheShared, cacheMiss} {
		c.hits.WithLabelValues(string(r)) // export zeros from the start
	}
	if reg != nil {
		reg.MustRegister(c.hits, c.entries)
	}
	return c
}

// get returns the value for key: from the cache, from a build another
// request started for the same key, or from build. Only one build per key
// runs at a time; it gets a context that outlives the caller's (bounded by
// cacheBuildTimeout), because other requests may wait for it.
func (c *cellCache[V]) get(ctx context.Context, key string, build func(context.Context) built[V]) (V, cacheResult, error) {
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*cacheEntry[V])
		if c.now().Before(e.expires) {
			c.order.MoveToFront(el)
			c.mu.Unlock()
			c.hits.WithLabelValues(string(cacheHit)).Inc()
			return e.val, cacheHit, nil
		}
		c.remove(el)
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		c.hits.WithLabelValues(string(cacheShared)).Inc()
		select {
		case <-call.done:
			return call.res.val, cacheShared, call.res.err
		case <-ctx.Done():
			var zero V
			return zero, cacheShared, ctx.Err()
		}
	}
	call := &cacheCall[V]{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()
	c.hits.WithLabelValues(string(cacheMiss)).Inc()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		if call.res.err == nil && call.res.key != "" {
			c.add(call.res.key, call.res.val)
		}
		c.mu.Unlock()
		close(call.done)
	}()
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheBuildTimeout)
	defer cancel()
	call.res.err = errBuildFailed // stays if build panics; waiters must not get a zero value
	call.res = build(bctx)
	return call.res.val, cacheMiss, call.res.err
}

// add stores val under key; c.mu is held.
func (c *cellCache[V]) add(key string, val V) {
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	c.items[key] = c.order.PushFront(&cacheEntry[V]{key: key, val: val, expires: c.now().Add(c.ttl)})
	for c.order.Len() > c.max {
		c.remove(c.order.Back())
	}
	c.entries.Set(float64(c.order.Len()))
}

// remove drops one entry; c.mu is held.
func (c *cellCache[V]) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.items, el.Value.(*cacheEntry[V]).key)
	c.entries.Set(float64(c.order.Len()))
}
