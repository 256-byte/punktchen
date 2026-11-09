package layer

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fake is a configurable Provider.
type fake struct {
	info    Info
	version uint64
	health  Health
	nearby  func(ctx context.Context, q NearbyQuery) (Result, error)
	calls   atomic.Int32
}

func (f *fake) Info() Info      { return f.info }
func (f *fake) Version() uint64 { return f.version }
func (f *fake) Health() Health  { return f.health }
func (f *fake) Nearby(ctx context.Context, q NearbyQuery) (Result, error) {
	f.calls.Add(1)
	if f.nearby != nil {
		return f.nearby(ctx, q)
	}
	return Result{Version: f.version, Payload: f.info.ID + " payload"}, nil
}

func newFake(id string, attribution ...string) *fake {
	return &fake{
		info:    Info{ID: id, Name: strings.ToUpper(id), Attribution: attribution},
		version: 7,
		health:  Health{Status: StatusOK},
	}
}

var wedding = NearbyQuery{Center: Location{Lat: 52.5427, Lng: 13.3660}, RadiusMeters: 500}

func mustRegistry(t *testing.T, ps ...Provider) *Registry {
	t.Helper()
	r, err := NewRegistry(ps...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewRegistry(t *testing.T) {
	if _, err := NewRegistry(newFake("transit"), newFake("transit")); err == nil {
		t.Error("duplicate id: want error")
	}
	if _, err := NewRegistry(newFake("")); err == nil {
		t.Error("empty id: want error")
	}

	r := mustRegistry(t, newFake("transit", "Data: VBB"), newFake("toilets"))
	var ids []string
	for _, l := range r.Layers() {
		ids = append(ids, l.ID)
	}
	if !slices.Equal(ids, []string{"transit", "toilets"}) {
		t.Errorf("Layers() ids %v, want registration order", ids)
	}
	if p, ok := r.Get("toilets"); !ok || p.Info().ID != "toilets" {
		t.Error("Get(toilets) failed")
	}
	if _, ok := r.Get("alerts"); ok {
		t.Error("Get(alerts) found an unregistered layer")
	}
}

func TestResolve(t *testing.T) {
	r := mustRegistry(t, newFake("transit"), newFake("toilets"), newFake("alerts"))

	ps, err := r.Resolve([]string{"alerts", "transit", "alerts"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ps {
		got = append(got, p.Info().ID)
	}
	if !slices.Equal(got, []string{"transit", "alerts"}) {
		t.Errorf("Resolve = %v, want deduplicated, in registration order", got)
	}

	_, err = r.Resolve([]string{"transit", "elevators", "taxi", "elevators"})
	if !errors.Is(err, ErrUnknownLayer) || !strings.Contains(err.Error(), "elevators, taxi") {
		t.Errorf("unknown layers: err %v, want ErrUnknownLayer naming both once", err)
	}
	if _, err := r.Resolve(nil); err == nil {
		t.Error("no layers: want error")
	}
}

func TestNearbyQueryValidate(t *testing.T) {
	bad := []NearbyQuery{
		{Center: Location{Lat: 91, Lng: 13}, RadiusMeters: 500},
		{Center: Location{Lat: 52, Lng: -181}, RadiusMeters: 500},
		{Center: Location{Lat: math.NaN(), Lng: 13}, RadiusMeters: 500},
		{Center: Location{Lat: 52, Lng: 13}, RadiusMeters: 0},
		{Center: Location{Lat: 52, Lng: 13}, RadiusMeters: math.Inf(1)},
	}
	for _, q := range bad {
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("%+v: err %v, want ErrInvalidQuery", q, err)
		}
	}
	if err := wedding.Validate(); err != nil {
		t.Errorf("valid query: %v", err)
	}
}

func TestNearbyComposesLayers(t *testing.T) {
	transit := newFake("transit", "Data: VBB (CC BY 4.0)")
	transit.version = 18342
	age := 11 * time.Second
	transit.health = Health{Status: StatusDegraded, RealtimeAge: &age}
	var seen NearbyQuery
	transit.nearby = func(_ context.Context, q NearbyQuery) (Result, error) {
		seen = q
		// Built from newer data than Version() said before: the result wins.
		return Result{Version: 18343, Payload: []string{"U Wedding"}}, nil
	}
	toilets := newFake("toilets", "Data: Berlin Open Data", "Data: VBB (CC BY 4.0)")
	unused := newFake("alerts")

	r := mustRegistry(t, transit, toilets, unused)
	c, err := r.Nearby(context.Background(), wedding, []string{"toilets", "transit"})
	if err != nil {
		t.Fatal(err)
	}

	if seen != wedding {
		t.Errorf("provider got query %+v, want %+v", seen, wedding)
	}
	if unused.calls.Load() != 0 {
		t.Error("a layer that was not requested was asked")
	}
	if len(c.Layers) != 2 || c.Layers[0].Info.ID != "transit" || c.Layers[1].Info.ID != "toilets" {
		t.Fatalf("layers %+v, want transit then toilets", c.Layers)
	}
	tr := c.Layers[0]
	if tr.Err != nil || tr.Version != 18343 || tr.Health.Status != StatusDegraded || *tr.Health.RealtimeAge != age {
		t.Errorf("transit response %+v", tr)
	}
	if p, ok := tr.Payload.([]string); !ok || !slices.Equal(p, []string{"U Wedding"}) {
		t.Errorf("transit payload %#v", tr.Payload)
	}
	if c.Layers[1].Payload != "toilets payload" {
		t.Errorf("toilets payload %#v", c.Layers[1].Payload)
	}
	want := []string{"Data: VBB (CC BY 4.0)", "Data: Berlin Open Data"}
	if !slices.Equal(c.Attribution, want) {
		t.Errorf("attribution %v, want %v (deduplicated, layer order)", c.Attribution, want)
	}
}

func TestNearbyFailingLayerIsDown(t *testing.T) {
	transit := newFake("transit", "Data: VBB")
	broken := newFake("toilets", "Data: Toilets")
	broken.nearby = func(context.Context, NearbyQuery) (Result, error) {
		return Result{Version: 3, Payload: "partial"}, errors.New("db unreachable")
	}
	panics := newFake("alerts", "Data: Alerts")
	panics.nearby = func(context.Context, NearbyQuery) (Result, error) { panic("boom") }

	r := mustRegistry(t, transit, broken, panics)
	c, err := r.Nearby(context.Background(), wedding, []string{"transit", "toilets", "alerts"})
	if err != nil {
		t.Fatalf("one failing layer must not fail the request: %v", err)
	}
	if c.Layers[0].Err != nil || c.Layers[0].Payload == nil {
		t.Errorf("healthy layer affected: %+v", c.Layers[0])
	}
	for _, l := range c.Layers[1:] {
		if l.Err == nil || l.Payload != nil || l.Health.Status != StatusDown || l.Health.LastError == "" {
			t.Errorf("%s: %+v, want down without payload", l.Info.ID, l)
		}
	}
	if !strings.Contains(c.Layers[1].Err.Error(), "db unreachable") || !strings.Contains(c.Layers[2].Err.Error(), "boom") {
		t.Errorf("errors %v / %v", c.Layers[1].Err, c.Layers[2].Err)
	}
	if !slices.Equal(c.Attribution, []string{"Data: VBB"}) {
		t.Errorf("attribution %v, want only layers with data", c.Attribution)
	}
}

func TestNearbyAsksLayersConcurrently(t *testing.T) {
	// Each provider waits until both have started: this only finishes if
	// they run at the same time.
	var started atomic.Int32
	both := make(chan struct{})
	wait := func(ctx context.Context, _ NearbyQuery) (Result, error) {
		if started.Add(1) == 2 {
			close(both)
		}
		select {
		case <-both:
			return Result{Payload: "ok"}, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	a, b := newFake("transit"), newFake("toilets")
	a.nearby, b.nearby = wait, wait

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mustRegistry(t, a, b).Nearby(ctx, wedding, []string{"transit", "toilets"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range c.Layers {
		if l.Err != nil {
			t.Errorf("%s: %v (layers ran one after another?)", l.Info.ID, l.Err)
		}
	}
}

func TestNearbyRequestErrors(t *testing.T) {
	transit := newFake("transit")
	r := mustRegistry(t, transit)

	if _, err := r.Nearby(context.Background(), NearbyQuery{RadiusMeters: -1}, []string{"transit"}); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("invalid query: %v", err)
	}
	if _, err := r.Nearby(context.Background(), wedding, []string{"toilets"}); !errors.Is(err, ErrUnknownLayer) {
		t.Errorf("unknown layer: %v", err)
	}
	if transit.calls.Load() != 0 {
		t.Error("provider asked for a rejected request")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transit.nearby = func(ctx context.Context, _ NearbyQuery) (Result, error) { return Result{}, ctx.Err() }
	if _, err := r.Nearby(ctx, wedding, []string{"transit"}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled request: err %v, want context.Canceled", err)
	}
}
