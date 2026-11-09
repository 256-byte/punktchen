package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestNewLoggerWritesJSONAndFiltersLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelInfo)
	log.Debug("hidden")
	log.With("component", "test").Info("hello", "n", 1)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line (debug filtered), got %d: %q", len(lines), buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if rec["msg"] != "hello" || rec["level"] != "INFO" || rec["component"] != "test" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestNewLoggerWritesDurationsAsText(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelInfo).Info("done", "duration", 158*time.Second)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["duration"] != "2m38s" {
		t.Fatalf("duration = %v, want \"2m38s\"", rec["duration"])
	}
}

func TestMetricsHandler(t *testing.T) {
	srv := httptest.NewServer(MetricsHandler(NewMetricsRegistry()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, m := range []string{"go_goroutines", "process_start_time_seconds"} {
		if !strings.Contains(string(body), m) {
			t.Errorf("metrics output misses %s", m)
		}
	}
}

func TestListenFailsWhenPortInUse(t *testing.T) {
	first, err := Listen("first", "127.0.0.1:0", http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	if _, err := Listen("second", first.Addr().String(), http.NotFoundHandler()); err == nil {
		t.Fatal("want error for port in use")
	}
}

// TestServeGracefulShutdown checks that a request in flight when the shutdown
// signal arrives still completes, and that Serve then returns nil.
func TestServeGracefulShutdown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	srv, err := Listen("api", "127.0.0.1:0", h)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Listen("metrics", "127.0.0.1:0", http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, discardLogger(), 5*time.Second, srv, other) }()

	type result struct {
		status int
		body   string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr().String())
		if err != nil {
			got <- result{err: err}
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got <- result{status: resp.StatusCode, body: string(b)}
	}()

	<-entered
	cancel()

	select {
	case err := <-served:
		t.Fatalf("Serve returned while a request was in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	r := <-got
	if r.err != nil || r.status != http.StatusOK || r.body != "done" {
		t.Fatalf("in-flight request: %+v", r)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}

func TestServeShutdownTimeout(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	})
	srv, err := Listen("api", "127.0.0.1:0", h)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, discardLogger(), 100*time.Millisecond, srv) }()
	go func() {
		if resp, err := http.Get("http://" + srv.Addr().String()); err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-entered
	cancel()
	select {
	case err := <-served:
		if err == nil || !strings.Contains(err.Error(), "shutdown") {
			t.Fatalf("want shutdown timeout error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not give up after the shutdown timeout")
	}
}
