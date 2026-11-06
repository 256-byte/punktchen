package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

// newTestServer serves the API with a fixed clock: 2026-11-05 13:32:00 UTC
// (winter time in Berlin, UTC+1).
func newTestServer(t *testing.T, db Pinger) *httptest.Server {
	t.Helper()
	s := NewServer("test-version", db, slog.New(slog.DiscardHandler))
	s.now = func() time.Time { return time.Date(2026, 11, 5, 13, 32, 0, 500, time.UTC) }
	srv := httptest.NewServer(NewHandler(s))
	t.Cleanup(srv.Close)
	return srv
}

// get returns status, Content-Type and the decoded JSON body.
func get(t *testing.T, url string) (int, string, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("GET %s: body is not JSON: %q", url, raw)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

func TestHealthOK(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	status, ctype, body := get(t, srv.URL+"/v1/health")

	if status != http.StatusOK || ctype != "application/json" {
		t.Fatalf("status %d, content-type %q", status, ctype)
	}
	want := map[string]any{
		"status":  "ok",
		"version": "test-version",
		// Absolute time with Berlin offset, whole seconds (ADR-0007).
		"time": "2026-11-05T14:32:00+01:00",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
}

func TestHealthDatabaseDown(t *testing.T) {
	srv := newTestServer(t, fakePinger{err: errors.New("connection refused")})
	status, _, body := get(t, srv.URL+"/v1/health")

	// Still 200: the process is alive; the body says the data is unusable.
	if status != http.StatusOK || body["status"] != "down" {
		t.Fatalf("status %d, body %v; want 200 with status down", status, body)
	}
}

func TestNotImplementedEndpoints(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	for _, path := range []string{
		"/v1/layers",
		"/v1/nearby?lat=52.5427&lng=13.366",
		"/v1/nearby?lat=52.5427&lng=13.366&radius=300&layers=transit",
		"/v1/stations/de:11000:900009104",
	} {
		t.Run(path, func(t *testing.T) {
			status, ctype, body := get(t, srv.URL+path)
			if status != http.StatusNotImplemented || ctype != "application/json" {
				t.Fatalf("status %d, content-type %q; want 501 JSON", status, ctype)
			}
			if code := errorCode(body); code != "not_implemented" {
				t.Fatalf("error.code = %q, want not_implemented", code)
			}
		})
	}
}

func TestBadParametersReturnErrorJSON(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	for _, path := range []string{
		"/v1/nearby",                            // lat and lng missing
		"/v1/nearby?lat=abc&lng=13.366",         // not a number
		"/v1/nearby?lat=52.5&lng=13.3&radius=x", // not an integer
	} {
		t.Run(path, func(t *testing.T) {
			status, _, body := get(t, srv.URL+path)
			if status != http.StatusBadRequest || errorCode(body) != "invalid_argument" {
				t.Fatalf("status %d, body %v; want 400 invalid_argument", status, body)
			}
		})
	}
}

func TestUnknownPathReturnsErrorJSON(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	status, _, body := get(t, srv.URL+"/v2/nope")
	if status != http.StatusNotFound || errorCode(body) != "not_found" {
		t.Fatalf("status %d, body %v; want 404 not_found", status, body)
	}
}

func errorCode(body map[string]any) string {
	e, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := e["code"].(string)
	if msg, _ := e["message"].(string); strings.TrimSpace(msg) == "" {
		return "" // message is required by the contract
	}
	return code
}
