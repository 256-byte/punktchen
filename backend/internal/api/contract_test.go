package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	"github.com/mmcloughlin/geohash"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/geo"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
	"github.com/256-byte/punktchen/backend/internal/transit/static/statictest"
)

// contract checks responses against api/openapi.yaml (PLAN 2.5).
type contract struct {
	router routers.Router
}

func loadContract(t *testing.T) *contract {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("openapi.yaml is not a valid document: %v", err)
	}
	doc.Servers = nil // match the test server's address
	router, err := legacyrouter.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &contract{router: router}
}

// get sends GET base+path and fails the test if the response (status,
// headers, body) does not match the contract. It returns status and body.
func (c *contract) get(t *testing.T, base, path string) (int, map[string]any) {
	t.Helper()
	status, _, body := c.getWith(t, base, path, nil)
	return status, body
}

// getWith is get with request headers; it also returns the response
// headers, and a nil body for an empty one (304).
func (c *contract) getWith(t *testing.T, base, path string, header http.Header) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	route, params, err := c.router.FindRoute(req)
	if err != nil {
		t.Fatalf("GET %s: not in the contract: %v", path, err)
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 resp.StatusCode,
		Header:                 resp.Header,
		Options:                &openapi3filter.Options{MultiError: true, IncludeResponseStatus: true},
	}
	in.SetBodyBytes(raw)
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Errorf("GET %s → %d breaks the contract: %v\nbody: %.500s", path, resp.StatusCode, err, raw)
	}

	if len(raw) == 0 {
		return resp.StatusCode, resp.Header, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("GET %s: body is not JSON: %.200s", path, raw)
	}
	return resp.StatusCode, resp.Header, body
}

type noSnapshot struct{}

func (noSnapshot) Current() *departures.Snapshot { return nil }

// TestContract runs every endpoint on real data (Wedding subset, Tuesday
// 2026-11-10 17:00) and on an empty departures state, and validates each
// response against api/openapi.yaml.
func TestContract(t *testing.T) {
	c := loadContract(t)
	pool := statictest.NewDB(t)
	statictest.LoadZip(t, pool, filepath.Join("..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip"))

	now := time.Date(2026, 11, 10, 17, 0, 0, 0, platform.Berlin)
	b := departures.NewBuilder(departures.BuilderConfig{Now: func() time.Time { return now }},
		departures.NewLoader(pool), nil, slog.New(slog.DiscardHandler))
	if _, err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	serve := func(snaps transit.Snapshots) string {
		p := transit.NewProvider(pool, snaps)
		reg, err := layer.NewRegistry(p)
		if err != nil {
			t.Fatal(err)
		}
		s := NewServer(Config{
			Version: "test", DB: pool, Schedule: freshSchedule(), Layers: reg, Transit: p,
			Log: slog.New(slog.DiscardHandler),
		})
		s.now = func() time.Time { return now }
		srv := httptest.NewServer(NewHandler(s))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	live := serve(b)
	empty := serve(noSnapshot{})

	for _, tc := range []struct {
		name, base, path string
		want             int
		check            func(t *testing.T, body map[string]any)
	}{
		{"health", live, "/v1/health", 200, nil},
		{"layers", live, "/v1/layers", 200, nil},
		{"nearby Wedding", live, "/v1/nearby?lat=52.5427&lng=13.366", 200, func(t *testing.T, body map[string]any) {
			features := body["layers"].(map[string]any)["transit"].(map[string]any)["features"].([]any)
			// The 6 stations within 500 m of the point (transit golden file)
			// plus 2 more: the search is 500 m + half diagonal around the
			// cell centre (ADR-0006).
			if len(features) != 8 {
				t.Errorf("%d stations, want 8", len(features))
			}
			lm := body["meta"].(map[string]any)["layers"].(map[string]any)["transit"].(map[string]any)
			if lm["version"] != float64(b.Current().Version) {
				t.Errorf("version %v, snapshot %d", lm["version"], b.Current().Version)
			}
		}},
		{"nearby max radius", live, "/v1/nearby?lat=52.5427&lng=13.366&radius=1000&layers=transit", 200, nil},
		{"nearby nothing around", live, "/v1/nearby?lat=52.40&lng=13.20&radius=100", 200, nil},
		{"station sheet", live, "/v1/stations/de:11000:900009104", 200, func(t *testing.T, body map[string]any) {
			if n := len(body["station"].(map[string]any)["poles"].([]any)); n != 6 {
				t.Errorf("%d poles, want 6", n)
			}
		}},
		{"unknown station", live, "/v1/stations/de:11000:000000000", 404, nil},
		{"bad radius", live, "/v1/nearby?lat=52.5&lng=13.4&radius=5000", 400, nil},
		{"unknown layer", live, "/v1/nearby?lat=52.5&lng=13.4&layers=toilets", 400, nil},
		{"missing lat", live, "/v1/nearby?lng=13.4", 400, nil},
		{"nearby without departures", empty, "/v1/nearby?lat=52.5427&lng=13.366", 200, func(t *testing.T, body map[string]any) {
			if _, ok := body["layers"].(map[string]any)["transit"]; ok {
				t.Error("transit payload without departures")
			}
		}},
		{"station without departures", empty, "/v1/stations/de:11000:900009104", 503, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := c.get(t, tc.base, tc.path)
			if status != tc.want {
				t.Fatalf("status %d, want %d: %v", status, tc.want, body)
			}
			if tc.check != nil {
				tc.check(t, body)
			}
		})
	}

	// Task 5.1's acceptance: two users 50 m apart in the same cell get the
	// identical response, and it holds every station within 500 m of each
	// of them; a user in the next cell gets another one.
	t.Run("same cell, same response", func(t *testing.T) {
		cell := geo.CellAt(52.5427, 13.366)
		users := [][2]float64{{cell.Lat - 0.0002, cell.Lng}, {cell.Lat + 0.00025, cell.Lng}}
		if d := geo.DistanceMeters(users[0][0], users[0][1], users[1][0], users[1][1]); d < 49.5 || d > 50.5 {
			t.Fatalf("users %.1f m apart", d)
		}
		raw := func(lat, lng float64) []byte {
			t.Helper()
			resp, err := http.Get(fmt.Sprintf("%s/v1/nearby?lat=%f&lng=%f", live, lat, lng))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			return body
		}
		first, second := raw(users[0][0], users[0][1]), raw(users[1][0], users[1][1])
		if !bytes.Equal(first, second) {
			t.Fatalf("different responses in one cell:\n%s\n%s", first, second)
		}
		var got openapi.NearbyResponse
		if err := json.Unmarshal(first, &got); err != nil || got.Meta.Cell == nil || *got.Meta.Cell != cell.Hash {
			t.Fatalf("cell %v, err %v", got.Meta.Cell, err)
		}
		served := map[string]bool{}
		for _, f := range got.Layers.Transit.Features {
			served[f.Id] = true
		}
		// What the exact points would get without snapping.
		p := transit.NewProvider(pool, b)
		for _, u := range users {
			res, err := p.Nearby(context.Background(), layer.NearbyQuery{Center: layer.Location{Lat: u[0], Lng: u[1]}, RadiusMeters: 500})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range res.Payload.(*openapi.TransitLayer).Features {
				if !served[f.Id] {
					t.Errorf("station %s within 500 m of %v is missing", f.Id, u)
				}
			}
		}

		box := geohash.BoundingBox(cell.Hash)
		next := raw(box.MaxLat+0.0002, cell.Lng)
		if err := json.Unmarshal(next, &got); err != nil || got.Meta.Cell == nil || *got.Meta.Cell == cell.Hash {
			t.Errorf("next cell north: %v, err %v", got.Meta.Cell, err)
		}
	})

	// Task 5.3's acceptance on real data: the ETag of a 200 brings a 304
	// with an empty body (both validated against the contract), for the map
	// and the station sheet.
	for _, path := range []string{"/v1/nearby?lat=52.5427&lng=13.366", "/v1/stations/de:11000:900009104"} {
		t.Run("revalidate "+path, func(t *testing.T) {
			status, h, _ := c.getWith(t, live, path, nil)
			etag := h.Get("ETag")
			if status != http.StatusOK || !strings.HasPrefix(etag, `W/"`) {
				t.Fatalf("status %d, ETag %q", status, etag)
			}
			status, h, body := c.getWith(t, live, path, http.Header{"If-None-Match": {etag}})
			if status != http.StatusNotModified || body != nil || h.Get("ETag") != etag {
				t.Errorf("revalidation: status %d, ETag %q, body %v", status, h.Get("ETag"), body)
			}
			if status, _, _ := c.getWith(t, live, path, http.Header{"If-None-Match": {`W/"other"`}}); status != http.StatusOK {
				t.Errorf("other ETag: status %d, want 200", status)
			}
		})
	}

	t.Run("the contract test catches violations", func(t *testing.T) {
		// A response missing a required field must fail validation.
		req, _ := http.NewRequest(http.MethodGet, live+"/v1/layers", nil)
		route, params, err := c.router.FindRoute(req)
		if err != nil {
			t.Fatal(err)
		}
		in := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
			Status:                 200,
			Header:                 http.Header{"Content-Type": {"application/json"}},
			Options:                &openapi3filter.Options{MultiError: true},
		}
		in.SetBodyBytes(bytes.TrimSpace([]byte(`{"layers":[{"id":"transit","name":"x"}]}`)))
		if err := openapi3filter.ValidateResponse(context.Background(), in); err == nil {
			t.Error("a layer without attribution passed validation")
		}
	})
}
