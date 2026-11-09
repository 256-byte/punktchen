package static

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// fakeFeed is an upstream server. With etag/lastModified empty it behaves
// like VBB's GTFS URL (no validators, always 200).
type fakeFeed struct {
	mu           sync.Mutex
	body         string
	etag         string
	lastModified string
	honour304    bool // answer conditional requests with 304
	status       int  // force a status (0 = normal)
	hang         bool // send a few bytes, then stall until the client gives up
	requests     []http.Header
}

func (f *fakeFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Header.Clone())
	if f.hang {
		f.mu.Unlock() // do not block the test while stalling
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	defer f.mu.Unlock()
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	if f.etag != "" {
		w.Header().Set("ETag", f.etag)
	}
	if f.lastModified != "" {
		w.Header().Set("Last-Modified", f.lastModified)
	}
	if f.honour304 &&
		((f.etag != "" && r.Header.Get("If-None-Match") == f.etag) ||
			(f.lastModified != "" && r.Header.Get("If-Modified-Since") == f.lastModified)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write([]byte(f.body))
}

func (f *fakeFeed) set(fn func(f *fakeFeed)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeFeed) lastRequest() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	il   *ImportLog
	feed *fakeFeed
	dir  string
	d    *Downloader
}

// newEnv gives each subtest its own source name, server and download dir on
// the shared database.
func newEnv(t *testing.T, pool *pgxpool.Pool, feed *fakeFeed, maxBytes int64) *env {
	t.Helper()
	srv := httptest.NewServer(feed)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	il := NewImportLog(pool)
	d, err := NewDownloader(DownloaderConfig{
		Source: t.Name(), URL: srv.URL + "/gtfs.zip", UserAgent: "livestop-test", Dir: dir, MaxBytes: maxBytes,
	}, il, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, pool: pool, il: il, feed: feed, dir: dir, d: d}
}

func (e *env) fetch() *Download {
	e.t.Helper()
	dl, err := e.d.Fetch(context.Background())
	if err != nil {
		e.t.Fatalf("Fetch: %v", err)
	}
	return dl
}

type row struct {
	status                 string
	etag, lastMod, sha, er *string
	finished               bool
}

func (e *env) row(id int64) row {
	e.t.Helper()
	var r row
	err := e.pool.QueryRow(context.Background(),
		`SELECT status, etag, last_modified, sha256, error, finished_at IS NOT NULL FROM meta.feed_imports WHERE id = $1`, id).
		Scan(&r.status, &r.etag, &r.lastMod, &r.sha, &r.er, &r.finished)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// filesLeft lists files in the download dir (skips must not leave any).
func (e *env) filesLeft() []string {
	e.t.Helper()
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		e.t.Fatal(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	return names
}

func (e *env) succeed(dl *Download) {
	e.t.Helper()
	if err := e.il.Succeed(context.Background(), dl.ImportID, map[string]int64{"stops": 1}); err != nil {
		e.t.Fatal(err)
	}
	if err := dl.Remove(); err != nil {
		e.t.Fatal(err)
	}
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestDownloader(t *testing.T) {
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}

	// The VBB default URL: no ETag, no Last-Modified (DATA_MODEL §7 #15).
	t.Run("no validators: unchanged file is skipped by sha256", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: "zip v1"}, 0)

		first := e.fetch()
		if !first.Changed || first.Path == "" || first.Size != 6 {
			t.Fatalf("first fetch: %+v, want changed with a file", first)
		}
		if got, _ := os.ReadFile(first.Path); string(got) != "zip v1" {
			t.Fatalf("file content %q", got)
		}
		r := e.row(first.ImportID)
		if r.status != StatusRunning || str(r.sha) != first.SHA256 || r.etag != nil || r.lastMod != nil {
			t.Fatalf("row after first fetch: %+v sha=%s", r, str(r.sha))
		}
		e.succeed(first)

		second := e.fetch()
		if second.Changed || second.Path != "" {
			t.Fatalf("second fetch: %+v, want unchanged without file", second)
		}
		if r := e.row(second.ImportID); r.status != StatusSkipped || !r.finished || str(r.sha) != first.SHA256 {
			t.Fatalf("row after second fetch: %+v sha=%s", r, str(r.sha))
		}
		if left := e.filesLeft(); len(left) != 0 {
			t.Fatalf("skip left files: %v", left)
		}
		if h := e.feed.lastRequest(); h.Get("If-None-Match") != "" || h.Get("If-Modified-Since") != "" {
			t.Fatalf("sent validators the server never gave: %v", h)
		}
		if ua := e.feed.lastRequest().Get("User-Agent"); ua != "livestop-test" {
			t.Fatalf("User-Agent %q", ua)
		}

		e.feed.set(func(f *fakeFeed) { f.body = "zip v2" })
		third := e.fetch()
		if !third.Changed || third.SHA256 == first.SHA256 {
			t.Fatalf("changed upstream: %+v", third)
		}
	})

	t.Run("etag: 304 is skipped without download", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: "zip", etag: `"v1"`, honour304: true}, 0)
		e.succeed(e.fetch())

		dl := e.fetch()
		if got := e.feed.lastRequest().Get("If-None-Match"); got != `"v1"` {
			t.Fatalf("If-None-Match %q", got)
		}
		r := e.row(dl.ImportID)
		if dl.Changed || r.status != StatusSkipped || r.sha != nil || str(r.etag) != `"v1"` {
			t.Fatalf("304 path: %+v row=%+v", dl, r)
		}
	})

	t.Run("last-modified: 304 is skipped without download", func(t *testing.T) {
		lm := "Fri, 06 Nov 2026 10:00:00 GMT"
		e := newEnv(t, pool, &fakeFeed{body: "zip", lastModified: lm, honour304: true}, 0)
		e.succeed(e.fetch())

		dl := e.fetch()
		if got := e.feed.lastRequest().Get("If-Modified-Since"); got != lm {
			t.Fatalf("If-Modified-Since %q", got)
		}
		if dl.Changed || e.row(dl.ImportID).status != StatusSkipped {
			t.Fatalf("304 path: %+v", dl)
		}
	})

	t.Run("server ignores validators: same body still skipped by sha256", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: "zip", etag: `"v1"`}, 0)
		e.succeed(e.fetch())
		if dl := e.fetch(); dl.Changed {
			t.Fatalf("got changed for identical body: %+v", dl)
		}
	})

	t.Run("only succeeded imports are a reference", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: "zip"}, 0)
		first := e.fetch()
		if err := e.il.Fail(ctx, first.ImportID, errors.New("import broke")); err != nil {
			t.Fatal(err)
		}
		_ = first.Remove()
		if r := e.row(first.ImportID); r.status != StatusFailed || str(r.er) != "import broke" {
			t.Fatalf("failed row: %+v", r)
		}
		if dl := e.fetch(); !dl.Changed {
			t.Fatal("same file as a failed import must be imported again")
		}
	})

	t.Run("http error fails the attempt", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{status: http.StatusInternalServerError}, 0)
		_, err := e.d.Fetch(ctx)
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err = %v, want 500", err)
		}
		var id int64
		if err := pool.QueryRow(ctx, `SELECT max(id) FROM meta.feed_imports WHERE source = $1`, t.Name()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if r := e.row(id); r.status != StatusFailed || !strings.Contains(str(r.er), "500") {
			t.Fatalf("row: %+v error=%s", r, str(r.er))
		}
	})

	t.Run("too large is rejected and leaves no file", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: strings.Repeat("x", 100)}, 10)
		if _, err := e.d.Fetch(ctx); err == nil || !strings.Contains(err.Error(), "larger than 10 bytes") {
			t.Fatalf("err = %v", err)
		}
		if left := e.filesLeft(); len(left) != 0 {
			t.Fatalf("files left: %v", left)
		}
	})

	t.Run("cancelled download still closes the attempt", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{hang: true}, 0)
		cctx, cancel := context.WithCancel(ctx)
		go func() {
			// Cancel once the request is in flight.
			for {
				e.feed.mu.Lock()
				n := len(e.feed.requests)
				e.feed.mu.Unlock()
				if n > 0 {
					cancel()
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
		if _, err := e.d.Fetch(cctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM meta.feed_imports WHERE source = $1`, t.Name()).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != StatusFailed {
			t.Fatalf("status %q, want failed (no attempt may stay running)", status)
		}
		if left := e.filesLeft(); len(left) != 0 {
			t.Fatalf("files left: %v", left)
		}
	})

	t.Run("attempt cannot be closed twice", func(t *testing.T) {
		e := newEnv(t, pool, &fakeFeed{body: "zip"}, 0)
		dl := e.fetch()
		e.succeed(dl)
		if err := e.il.Fail(ctx, dl.ImportID, errors.New("late")); err == nil {
			t.Fatal("closing a finished attempt again must fail")
		}
	})
}

// TestDownloaderLiveVBB downloads the real feed twice (2 × ~85 MB). Run it on
// purpose only: LIVESTOP_LIVE_TESTS=1 go test -run LiveVBB ./internal/transit/static/
func TestDownloaderLiveVBB(t *testing.T) {
	if os.Getenv("LIVESTOP_LIVE_TESTS") != "1" {
		t.Skip("set LIVESTOP_LIVE_TESTS=1 to download from VBB")
	}
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	il := NewImportLog(pool)
	d, err := NewDownloader(DownloaderConfig{
		URL: config.DefaultGTFSURL, UserAgent: "livestop-test (+https://github.com/256-byte/punktchen)", Dir: t.TempDir(),
	}, il, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}

	first, err := d.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.Size < 50<<20 {
		t.Fatalf("first: %+v, want a changed zip > 50 MB", first)
	}
	if err := il.Succeed(ctx, first.ImportID, nil); err != nil {
		t.Fatal(err)
	}
	_ = first.Remove()

	start := time.Now()
	second, err := d.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("second fetch: changed=%v in %s (sha256 %s, ETag %q)", second.Changed, time.Since(start).Round(time.Millisecond), second.SHA256, second.ETag)
	if second.Changed {
		t.Fatal("unchanged upstream (seconds apart) must be skipped")
	}
}

func TestNewDownloaderNeedsURL(t *testing.T) {
	if _, err := NewDownloader(DownloaderConfig{}, nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("want error without URL")
	}
}
