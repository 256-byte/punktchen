package amenities

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/importlog"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// fakeWFS serves whatever body/status the test sets and remembers the
// request headers.
type fakeWFS struct {
	mu        sync.Mutex
	body      []byte
	status    int
	userAgent string
	requests  int
}

func (f *fakeWFS) set(status int, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

func (f *fakeWFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	f.userAgent = r.Header.Get("User-Agent")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	_, _ = w.Write(f.body)
}

// firstN keeps the first n features of a recorded answer.
func firstN(t *testing.T, body []byte, n int) []byte {
	t.Helper()
	var fc map[string]any
	if err := json.Unmarshal(body, &fc); err != nil {
		t.Fatal(err)
	}
	fc["features"] = fc["features"].([]any)[:n]
	out, err := json.Marshal(fc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type importEnv struct {
	pool *pgxpool.Pool
	wfs  *fakeWFS
	im   *Importer[queries.InsertToiletsParams]
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker (PostGIS)")
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
	wfs := &fakeWFS{status: http.StatusOK}
	srv := httptest.NewServer(wfs)
	t.Cleanup(srv.Close)
	im, err := NewImporter(pool, Toilets(srv.URL), Config{UserAgent: "punktchen-test", Scope: berlin},
		importlog.New(pool), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return &importEnv{pool: pool, wfs: wfs, im: im}
}

func (e *importEnv) count(t *testing.T) int32 {
	t.Helper()
	n, err := queries.New(e.pool).CountToilets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *importEnv) attempt(t *testing.T, id int64) (status string, errMsg *string) {
	t.Helper()
	if err := e.pool.QueryRow(context.Background(),
		`SELECT status, error FROM meta.feed_imports WHERE id = $1`, id).Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	return status, errMsg
}

// TestImporter walks one dataset through the import lifecycle against a
// fake WFS: first import, unchanged (SHA-256), forced, changed, and the
// failures that must keep the stored rows.
func TestImporter(t *testing.T) {
	e := newImportEnv(t)
	ctx := context.Background()
	recorded := fixture(t, "toiletten-20261118.json")

	// First import: all 525 rows, attempt succeeded with its row count.
	e.wfs.set(http.StatusOK, recorded)
	res, err := e.im.Run(ctx, false)
	if err != nil || !res.Changed || res.Rows != 525 || e.count(t) != 525 {
		t.Fatalf("first import: %+v, %v, count %d", res, err, e.count(t))
	}
	if st, _ := e.attempt(t, res.ImportID); st != importlog.StatusSucceeded {
		t.Fatalf("first import status %s", st)
	}
	if e.wfs.userAgent != "punktchen-test" {
		t.Errorf("User-Agent = %q", e.wfs.userAgent)
	}

	// Same body again: skipped by SHA-256, rows untouched.
	res, err = e.im.Run(ctx, false)
	if err != nil || res.Changed {
		t.Fatalf("same body: %+v, %v", res, err)
	}
	if st, _ := e.attempt(t, res.ImportID); st != importlog.StatusSkipped {
		t.Fatalf("same body status %s", st)
	}

	// Same features, new "timeStamp" (every real answer has its own): skipped.
	restamped := strings.Replace(string(recorded), "\"timeStamp\":\"", "\"timeStamp\":\"1", 1)
	if restamped == string(recorded) {
		t.Fatal("fixture has no timeStamp")
	}
	e.wfs.set(http.StatusOK, []byte(restamped))
	if res, err = e.im.Run(ctx, false); err != nil || res.Changed {
		t.Fatalf("new timeStamp only: %+v, %v (want skipped)", res, err)
	}

	// Forced: replaced although the hash is the same.
	if res, err = e.im.Run(ctx, true); err != nil || !res.Changed || e.count(t) != 525 {
		t.Fatalf("forced: %+v, %v", res, err)
	}

	// A smaller answer within the allowed drop (−24 %) replaces the rows.
	e.wfs.set(http.StatusOK, firstN(t, recorded, 400))
	if res, err = e.im.Run(ctx, false); err != nil || res.Rows != 400 || e.count(t) != 400 {
		t.Fatalf("400 rows: %+v, %v, count %d", res, err, e.count(t))
	}

	failures := []struct {
		name    string
		status  int
		body    []byte
		wantErr string
	}{
		{"server error", http.StatusInternalServerError, []byte("boom"), "unexpected status 500"},
		{"html instead of json", http.StatusOK, []byte("<html>maintenance</html>"), "decode geojson"},
		{"drop over 30 %", http.StatusOK, firstN(t, recorded, 250), "dropped from 400 to 250"},
		{"too few rows", http.StatusOK, firstN(t, recorded, 10), "only 10 rows"},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			e.wfs.set(tc.status, tc.body)
			res, err := e.im.Run(ctx, false)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if n := e.count(t); n != 400 {
				t.Fatalf("rows = %d after a failed import, want the old 400", n)
			}
			st, msg := e.attempt(t, res.ImportID)
			if st != importlog.StatusFailed || msg == nil || !strings.Contains(*msg, tc.wantErr) {
				t.Fatalf("attempt = %s %v", st, msg)
			}
		})
	}

	// After the failures the last successful import is still the 400-row
	// one: its hash and row count are what the next run compares against.
	e.wfs.set(http.StatusOK, firstN(t, recorded, 400))
	if res, err = e.im.Run(ctx, false); err != nil || res.Changed {
		t.Fatalf("back to the 400-row answer: %+v, %v (want skipped)", res, err)
	}
}

func TestFountainsImport(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker (PostGIS)")
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
	wfs := &fakeWFS{status: http.StatusOK, body: fixture(t, "trinkwasserbrunnen-20261118.json")}
	srv := httptest.NewServer(wfs)
	t.Cleanup(srv.Close)
	im, err := NewImporter(pool, Fountains(srv.URL), Config{UserAgent: "punktchen-test", Scope: berlin},
		importlog.New(pool), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if res, err := im.Run(ctx, false); err != nil || res.Rows != 242 {
		t.Fatalf("%+v, %v", res, err)
	}
	q := queries.New(pool)
	// Alexanderplatz has no fountain within 590 m; the nearest (633 m) fills in.
	got, err := q.NearbyFountains(ctx, queries.NearbyFountainsParams{Lat: 52.5215, Lng: 13.4113, RadiusM: 590, MaxM: 2000, MinResults: 3})
	if err != nil || len(got) != 3 || got[0].DistanceM < 590 || got[0].DistanceM > 700 {
		t.Fatalf("nearby Alexanderplatz: %d rows, %v; first %+v", len(got), err, got)
	}
}

func TestNewImporterValidates(t *testing.T) {
	il := importlog.New(nil)
	log := slog.New(slog.DiscardHandler)
	if _, err := NewImporter(nil, Toilets(""), Config{UserAgent: "x", Scope: berlin}, il, log); err == nil {
		t.Error("empty URL accepted")
	}
	if _, err := NewImporter(nil, Toilets("http://x"), Config{Scope: berlin}, il, log); err == nil {
		t.Error("empty User-Agent accepted")
	}
	if _, err := NewImporter(nil, Toilets("http://x"), Config{UserAgent: "x"}, il, log); err == nil {
		t.Error("zero scope accepted")
	}
}
