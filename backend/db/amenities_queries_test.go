package db_test

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/db"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

// Test points north of a centre near Alexanderplatz, at known distances.
const (
	centerLat = 52.52
	centerLng = 13.40
	// Metres per degree of latitude (mean); the test allows 1 % slack.
	metresPerDegLat = 111195.0
)

func north(m float64) float64 { return centerLat + m/metresPerDegLat }

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := platform.NewPool(ctx, pgtest.Start(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platform.Migrate(ctx, pool, db.Migrations, db.MigrationsDir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func ptr[T any](v T) *T { return &v }

// TestNearbyToilets checks the sparse-layer rule of ADR-0010: everything in
// the radius, filled up to min_results with the nearest within max_m.
func TestNearbyToilets(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	q := queries.New(pool)

	toilet := func(id string, metres float64) queries.InsertToiletsParams {
		return queries.InsertToiletsParams{
			ID: id, Lat: north(metres), Lng: centerLng, Address: id, Kind: "toilet",
			Open24h: true, FeeEur: ptr(0.0), Accessible: ptr(true),
		}
	}
	rows := []queries.InsertToiletsParams{
		toilet("a100", 100), toilet("b300", 300), toilet("c1000", 1000),
		toilet("d1500", 1500), toilet("e2500", 2500),
	}
	// Replace in one transaction, as the importer does.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	qtx := q.WithTx(tx)
	if err := qtx.DeleteToilets(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := qtx.InsertToilets(ctx, rows); err != nil || n != int64(len(rows)) {
		t.Fatalf("InsertToilets = %d, %v", n, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := q.CountToilets(ctx); err != nil || n != 5 {
		t.Fatalf("CountToilets = %d, %v; want 5", n, err)
	}

	tests := []struct {
		name     string
		atMetres float64 // query point north of the centre
		radius   float64
		maxM     float64
		want     []string
	}{
		{"radius has 2, filled with the third nearest", 0, 500, 2000, []string{"a100", "b300", "c1000"}},
		{"radius has 1, filled to 3", 0, 200, 2000, []string{"a100", "b300", "c1000"}},
		{"radius has 4, all of them", 0, 1600, 2000, []string{"a100", "b300", "c1000", "d1500"}},
		{"fill stops at max distance", 2500, 500, 1200, []string{"e2500", "d1500"}},
		{"nothing within max distance", -5000, 500, 2000, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := q.NearbyToilets(ctx, queries.NearbyToiletsParams{
				Lat: north(tc.atMetres), Lng: centerLng, RadiusM: tc.radius, MaxM: tc.maxM, MinResults: 3,
			})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for i, r := range got {
				ids = append(ids, r.ID)
				if i > 0 && r.DistanceM < got[i-1].DistanceM {
					t.Errorf("not nearest first: %s %.0f m after %.0f m", r.ID, r.DistanceM, got[i-1].DistanceM)
				}
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
		})
	}

	// Types and distance of one row.
	got, err := q.NearbyToilets(ctx, queries.NearbyToiletsParams{Lat: centerLat, Lng: centerLng, RadiusM: 150, MaxM: 150, MinResults: 3})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	r := got[0]
	if math.Abs(r.DistanceM-100) > 1 || r.Kind != "toilet" || !r.Open24h || r.FeeEur == nil || *r.FeeEur != 0 ||
		r.Accessible == nil || !*r.Accessible || r.ChangingTable != nil || r.OpeningHours != nil {
		t.Errorf("row = %+v", r)
	}
}

func TestNearbyFountains(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	q := queries.New(pool)

	if _, err := q.InsertFountains(ctx, []queries.InsertFountainsParams{
		{ID: "f1", Lat: north(50), Lng: centerLng, Location: "Alexanderplatz", Model: ptr("Kaiser"),
			BuiltYear: ptr(int16(2019)), SeasonFromMonth: 5, SeasonToMonth: 10},
		{ID: "f2", Lat: north(800), Lng: centerLng, Location: "Rosa-Luxemburg-Platz",
			SeasonFromMonth: 5, SeasonToMonth: 10, OutOfService: ptr("zur Zeit wegen Reparatur außer Betrieb")},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := q.NearbyFountains(ctx, queries.NearbyFountainsParams{Lat: centerLat, Lng: centerLng, RadiusM: 500, MaxM: 2000, MinResults: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "f1" || got[1].ID != "f2" {
		t.Fatalf("got %+v", got)
	}
	if f := got[0]; *f.Model != "Kaiser" || *f.BuiltYear != 2019 || f.SeasonFromMonth != 5 || f.SeasonToMonth != 10 || f.OutOfService != nil {
		t.Errorf("f1 = %+v", f)
	}
	if f := got[1]; f.OutOfService == nil || f.Model != nil || math.Abs(f.DistanceM-800) > 8 {
		t.Errorf("f2 = %+v", f)
	}

	if err := q.DeleteFountains(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := q.CountFountains(ctx); err != nil || n != 0 {
		t.Fatalf("after delete: %d, %v", n, err)
	}
}
