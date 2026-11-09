package static

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/queries"
)

// wedding is the PLAN 1.8 test point (U Wedding).
var wedding = queries.NearbyPolesParams{Lat: 52.5427, Lng: 13.3660, RadiusM: 500}

// TestNearbyPolesWedding checks the nearby query (PLAN 1.8) on the real-data
// Wedding fixture, made live with a swap like a real import.
func TestNearbyPolesWedding(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	log := slog.New(slog.DiscardHandler)
	if _, err := NewLoader(pool, testScope(t), log).Load(ctx, filepath.Join("..", "..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip")); err != nil {
		t.Fatal(err)
	}
	if err := swap(ctx, pool, log); err != nil {
		t.Fatal(err)
	}
	rows, err := queries.New(pool).NearbyPoles(ctx, wedding)
	if err != nil {
		t.Fatal(err)
	}

	poles := map[string][]string{} // station → poles
	var order []string             // stations in result order
	for i, r := range rows {
		if r.DistanceM > wedding.RadiusM {
			t.Errorf("%s at %.0f m is outside the radius", r.StopID, r.DistanceM)
		}
		if i > 0 && r.StationDistanceM < rows[i-1].StationDistanceM {
			t.Errorf("row %d: stations not ordered by distance", i)
		}
		if len(poles[r.StationID]) == 0 {
			order = append(order, r.StationID)
		}
		poles[r.StationID] = append(poles[r.StationID], r.StopID)
	}
	// Rows of one station come together.
	for i, s := range order {
		if slices.Contains(order[:i], s) {
			t.Errorf("station %s appears in two groups", s)
		}
	}

	const sUWedding, marthaNdumbe = "de:11000:900009104", "de:11000:900008103"
	got := poles[sUWedding]
	sort.Strings(got)
	want := []string{sUWedding + "::1", sUWedding + "::2", sUWedding + "::3", sUWedding + "::4", sUWedding + ":1:50", sUWedding + ":1:51"}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("S+U Wedding poles %v, want %v (served poles only, bus poles included)", got, want)
	}
	if len(order) == 0 || order[0] != sUWedding {
		t.Errorf("nearest station %v, want S+U Wedding first", order)
	}
	if len(poles[marthaNdumbe]) == 0 {
		t.Errorf("S Wedding/Martha-Ndumbe-Platz missing; stations %v", order)
	}
	for _, r := range rows {
		if r.StationID == sUWedding && !slices.Equal(r.StationModes, []string{"bus", "sbahn", "ubahn"}) {
			t.Errorf("S+U Wedding modes %v", r.StationModes)
			break
		}
	}

	t.Run("nothing in an empty area", func(t *testing.T) {
		rows, err := queries.New(pool).NearbyPoles(ctx, queries.NearbyPolesParams{Lat: 52.45, Lng: 13.30, RadiusM: 300})
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows %d, err %v; want none", len(rows), err)
		}
	})
}

// medianNearby runs the query n times on one connection (prepared and
// cached, as in the API) and returns the median duration.
func medianNearby(t *testing.T, pool *pgxpool.Pool, p queries.NearbyPolesParams, n int) (time.Duration, int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	q := queries.New(conn)
	var times []time.Duration
	var count int
	for i := 0; i < n; i++ {
		start := time.Now()
		rows, err := q.NearbyPoles(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		times = append(times, time.Since(start))
		count = len(rows)
	}
	slices.Sort(times)
	return times[len(times)/2], count
}
