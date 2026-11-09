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

	"github.com/256-byte/punktchen/backend/internal/transit/static"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

type fakeSchedule struct {
	state static.ScheduleState
	err   error
}

func (f fakeSchedule) ScheduleState(context.Context) (static.ScheduleState, error) {
	return f.state, f.err
}

type fakeTrigger struct {
	err   error
	calls int
}

func (f *fakeTrigger) Trigger() error { f.calls++; return f.err }

// testNow is 2026-11-05 13:32:00 UTC (winter time in Berlin, UTC+1).
var testNow = time.Date(2026, 11, 5, 13, 32, 0, 500, time.UTC)

// freshSchedule was confirmed one hour ago.
func freshSchedule() fakeSchedule {
	at := testNow.Add(-time.Hour)
	return fakeSchedule{state: static.ScheduleState{HasData: true, CheckedAt: &at}}
}

// newTestServer serves the API with a fixed clock, a fresh schedule and dev
// mode off; opts change the config.
func newTestServer(t *testing.T, db Pinger, opts ...func(*Config)) *httptest.Server {
	t.Helper()
	cfg := Config{
		Version: "test-version", DB: db, Schedule: freshSchedule(), Log: slog.New(slog.DiscardHandler),
		Layers: mustRegistry(t, &fakeLayer{}), Transit: &fakeStations{},
	}
	for _, o := range opts {
		o(&cfg)
	}
	s := NewServer(cfg)
	s.now = func() time.Time { return testNow }
	srv := httptest.NewServer(NewHandler(s))
	t.Cleanup(srv.Close)
	return srv
}

func withSchedule(f fakeSchedule) func(*Config) { return func(c *Config) { c.Schedule = f } }

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

func TestHealthFollowsTheSchedule(t *testing.T) {
	old := testNow.Add(-73 * time.Hour)
	cases := []struct {
		name  string
		sched fakeSchedule
		want  string
	}{
		{"no schedule data yet", fakeSchedule{}, "down"},
		{"schedule state unknown", fakeSchedule{err: errors.New("db gone")}, "down"},
		{"data but never confirmed", fakeSchedule{state: static.ScheduleState{HasData: true}}, "degraded"},
		{"confirmed more than 3 days ago", fakeSchedule{state: static.ScheduleState{HasData: true, CheckedAt: &old}}, "degraded"},
		{"confirmed recently", freshSchedule(), "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, fakePinger{}, withSchedule(tc.sched))
			status, _, body := get(t, srv.URL+"/v1/health")
			if status != http.StatusOK || body["status"] != tc.want {
				t.Fatalf("status %d, body %v; want 200 with %s", status, body, tc.want)
			}
		})
	}

	t.Run("scheduleCheckedAt in Berlin time", func(t *testing.T) {
		srv := newTestServer(t, fakePinger{})
		_, _, body := get(t, srv.URL+"/v1/health")
		if body["scheduleCheckedAt"] != "2026-11-05T13:32:00+01:00" {
			t.Fatalf("scheduleCheckedAt = %v", body["scheduleCheckedAt"])
		}
	})
}

func post(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestTriggerImport(t *testing.T) {
	t.Run("disabled without dev mode", func(t *testing.T) {
		trig := &fakeTrigger{}
		srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Imports = trig })
		if status, body := post(t, srv.URL+"/internal/import"); status != http.StatusNotFound || errorCode(body) != "not_found" || trig.calls != 0 {
			t.Fatalf("status %d body %v calls %d; want 404 and no trigger", status, body, trig.calls)
		}
	})
	t.Run("starts an import in dev mode", func(t *testing.T) {
		trig := &fakeTrigger{}
		srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Imports, c.Dev = trig, true })
		if status, body := post(t, srv.URL+"/internal/import"); status != http.StatusAccepted || body["status"] != "started" || trig.calls != 1 {
			t.Fatalf("status %d body %v calls %d; want 202 started", status, body, trig.calls)
		}
	})
	t.Run("conflict while an import runs", func(t *testing.T) {
		trig := &fakeTrigger{err: static.ErrImportRunning}
		srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Imports, c.Dev = trig, true })
		if status, body := post(t, srv.URL+"/internal/import"); status != http.StatusConflict || errorCode(body) != "conflict" {
			t.Fatalf("status %d body %v; want 409 conflict", status, body)
		}
	})
}
