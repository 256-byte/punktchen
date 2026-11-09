package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/transit"
)

// fakeLayer is a transit layer with a fixed payload; it records queries.
type fakeLayer struct {
	mu      sync.Mutex
	queries []layer.NearbyQuery
	err     error
	age     *time.Duration
}

func (f *fakeLayer) Info() layer.Info {
	return layer.Info{ID: "transit", Name: "Public transport", Attribution: []string{"Data: VBB (CC BY 4.0)"}}
}

func (f *fakeLayer) Nearby(_ context.Context, q layer.NearbyQuery) (layer.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if f.err != nil {
		return layer.Result{}, f.err
	}
	return layer.Result{Version: 1794220416761, Payload: &openapi.TransitLayer{
		Layer: openapi.TransitLayerLayerTransit, Features: []openapi.Station{},
	}}, nil
}

func (f *fakeLayer) Version() uint64 { return 1794220416761 }

func (f *fakeLayer) Health() layer.Health {
	if f.err != nil {
		return layer.Health{Status: layer.StatusDown, LastError: f.err.Error()}
	}
	return layer.Health{Status: layer.StatusOK, RealtimeAge: f.age}
}

func (f *fakeLayer) lastQuery(t *testing.T) layer.NearbyQuery {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		t.Fatal("layer was not asked")
	}
	return f.queries[len(f.queries)-1]
}

func mustRegistry(t *testing.T, providers ...layer.Provider) *layer.Registry {
	t.Helper()
	r, err := layer.NewRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// fakeStations knows one station; its health is status (default ok).
type fakeStations struct {
	err    error
	status layer.Status
}

func (f *fakeStations) Info() layer.Info { return (&fakeLayer{}).Info() }
func (f *fakeStations) Health() layer.Health {
	if f.status == "" {
		return layer.Health{Status: layer.StatusOK}
	}
	return layer.Health{Status: f.status}
}

func (f *fakeStations) Station(_ context.Context, id string) (*openapi.Station, uint64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	if id != "de:11000:900009104" {
		return nil, 0, transit.ErrUnknownStation
	}
	return &openapi.Station{Id: id, Kind: openapi.StationKindStation, Name: "S+U Wedding",
		Modes: []openapi.Mode{}, Poles: []openapi.Pole{}}, 42, nil
}

func TestListLayers(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	status, _, body := get(t, srv.URL+"/v1/layers")
	layers, _ := body["layers"].([]any)
	if status != http.StatusOK || len(layers) != 1 {
		t.Fatalf("status %d body %v", status, body)
	}
	l := layers[0].(map[string]any)
	if l["id"] != "transit" || l["name"] != "Public transport" || l["attribution"].([]any)[0] != "Data: VBB (CC BY 4.0)" {
		t.Errorf("layer %v", l)
	}
}

func TestNearbyParameters(t *testing.T) {
	fl := &fakeLayer{}
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, fl) })

	t.Run("defaults", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/nearby?lat=52.5427&lng=13.366")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("status %d, Cache-Control %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
		q := fl.lastQuery(t)
		if q.Center != (layer.Location{Lat: 52.5427, Lng: 13.366}) || q.RadiusMeters != 500 {
			t.Errorf("query %+v, want the point with radius 500", q)
		}
	})

	t.Run("radius and layers", func(t *testing.T) {
		if status, _, _ := get(t, srv.URL+"/v1/nearby?lat=52.5&lng=13.4&radius=1000&layers=transit"); status != http.StatusOK {
			t.Fatalf("status %d", status)
		}
		if q := fl.lastQuery(t); q.RadiusMeters != 1000 {
			t.Errorf("radius %v", q.RadiusMeters)
		}
	})

	for _, c := range []struct{ query, want string }{
		{"lat=91&lng=13.4", "lat must be between -90 and 90"},
		{"lat=52.5&lng=-181", "lng must be between -180 and 180"},
		{"lat=52.5&lng=13.4&radius=99", "radius must be between 100 and 1000"},
		{"lat=52.5&lng=13.4&radius=1001", "radius must be between 100 and 1000"},
		{"lat=52.5&lng=13.4&layers=toilets", "unknown layer: toilets"},
		{"lat=52.5&lng=13.4&layers=transit,transit", `layer "transit" is listed twice`},
		{"lat=52.5&lng=13.4&layers=", "layers must"},
		{"lat=99&lng=13.4&radius=5", "lat must be between -90 and 90; radius must be"},
	} {
		t.Run(c.query, func(t *testing.T) {
			status, _, body := get(t, srv.URL+"/v1/nearby?"+c.query)
			msg, _ := body["error"].(map[string]any)["message"].(string)
			if status != http.StatusBadRequest || errorCode(body) != "invalid_argument" || !strings.Contains(msg, c.want) {
				t.Errorf("status %d body %v; want 400 invalid_argument with %q", status, body, c.want)
			}
		})
	}
}

func TestNearbyResponse(t *testing.T) {
	age := 11 * time.Second
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, &fakeLayer{age: &age}) })
	status, _, body := get(t, srv.URL+"/v1/nearby?lat=52.5427&lng=13.366")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	meta := body["meta"].(map[string]any)
	if meta["generatedAt"] != "2026-11-05T14:32:00+01:00" || meta["attribution"].([]any)[0] != "Data: VBB (CC BY 4.0)" {
		t.Errorf("meta %v", meta)
	}
	if _, ok := meta["cell"]; ok {
		t.Error("cell before the cell cache (Phase 5)")
	}
	lm := meta["layers"].(map[string]any)["transit"].(map[string]any)
	if lm["version"] != float64(1794220416761) || lm["status"] != "ok" || lm["realtimeAgeSeconds"] != float64(11) {
		t.Errorf("layer meta %v", lm)
	}
	tl := body["layers"].(map[string]any)["transit"].(map[string]any)
	if tl["layer"] != "transit" || tl["features"] == nil {
		t.Errorf("transit payload %v", tl)
	}
}

func TestNearbyLayerDown(t *testing.T) {
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, &fakeLayer{err: transit.ErrNoDepartures}) })
	status, _, body := get(t, srv.URL+"/v1/nearby?lat=52.5427&lng=13.366")
	if status != http.StatusOK {
		t.Fatalf("status %d; a down layer must not fail the request", status)
	}
	meta := body["meta"].(map[string]any)
	if lm := meta["layers"].(map[string]any)["transit"].(map[string]any); lm["status"] != "down" {
		t.Errorf("layer meta %v, want down", lm)
	}
	if attr := meta["attribution"].([]any); len(attr) != 0 {
		t.Errorf("attribution %v without data", attr)
	}
	if _, ok := body["layers"].(map[string]any)["transit"]; ok {
		t.Error("payload of a down layer")
	}
}

func TestGetStation(t *testing.T) {
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Transit = &fakeStations{status: layer.StatusDegraded} })
	status, _, body := get(t, srv.URL+"/v1/stations/de:11000:900009104")
	if status != http.StatusOK {
		t.Fatalf("status %d body %v", status, body)
	}
	if body["station"].(map[string]any)["name"] != "S+U Wedding" {
		t.Errorf("station %v", body["station"])
	}
	lm := body["meta"].(map[string]any)["layers"].(map[string]any)["transit"].(map[string]any)
	if lm["version"] != float64(42) || lm["status"] != "degraded" {
		t.Errorf("layer meta %v", lm)
	}

	status, _, body = get(t, srv.URL+"/v1/stations/de:11000:999")
	if status != http.StatusNotFound || errorCode(body) != "not_found" {
		t.Errorf("unknown station: %d %v", status, body)
	}

	down := newTestServer(t, fakePinger{}, func(c *Config) { c.Transit = &fakeStations{err: transit.ErrNoDepartures} })
	status, _, body = get(t, down.URL+"/v1/stations/de:11000:900009104")
	if status != http.StatusServiceUnavailable || errorCode(body) != "unavailable" {
		t.Errorf("no departures: %d %v, want 503 unavailable", status, body)
	}

	broken := newTestServer(t, fakePinger{}, func(c *Config) { c.Transit = &fakeStations{err: errors.New("db gone")} })
	status, _, body = get(t, broken.URL+"/v1/stations/de:11000:900009104")
	if status != http.StatusInternalServerError || errorCode(body) != "internal" {
		t.Errorf("db error: %d %v, want 500 internal", status, body)
	}
}
