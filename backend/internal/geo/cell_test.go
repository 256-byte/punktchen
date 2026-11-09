package geo

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mmcloughlin/geohash"
)

const weddingLat, weddingLng = 52.5427, 13.366

func TestCellAt(t *testing.T) {
	c := CellAt(weddingLat, weddingLng)
	if c.Hash != "u33dbjh" || len(c.Hash) != CellPrecision {
		t.Errorf("hash %q", c.Hash)
	}
	box := geohash.BoundingBox(c.Hash)
	if !box.Contains(weddingLat, weddingLng) {
		t.Error("the cell does not contain the point")
	}
	// Size in Berlin: ~153 m north–south, ~93 m east–west.
	height := DistanceMeters(box.MinLat, c.Lng, box.MaxLat, c.Lng)
	width := DistanceMeters(c.Lat, box.MinLng, c.Lat, box.MaxLng)
	if math.Abs(height-153) > 2 || math.Abs(width-93) > 2 {
		t.Errorf("cell %.1f m × %.1f m", height, width)
	}
	if c.HalfDiagonalMeters < math.Hypot(height, width)/2 || c.HalfDiagonalMeters > math.Hypot(height, width)/2+2 {
		t.Errorf("half diagonal %v for %.1f × %.1f", c.HalfDiagonalMeters, height, width)
	}
	if got := c.SearchRadius(500); got != 500+c.HalfDiagonalMeters {
		t.Errorf("search radius %v", got)
	}
}

// TestCellSameAnswer: task 5.1's acceptance on the geo side — two users 50 m
// apart in one cell get the same cell (so the same query), users in the next
// cell another one.
func TestCellSameAnswer(t *testing.T) {
	c := CellAt(weddingLat, weddingLng)
	a := CellAt(c.Lat-0.0002, c.Lng)
	b := CellAt(c.Lat+0.00025, c.Lng)
	if d := DistanceMeters(c.Lat-0.0002, c.Lng, c.Lat+0.00025, c.Lng); math.Abs(d-50) > 0.5 {
		t.Fatalf("test points %.1f m apart, want 50", d)
	}
	if a != b || a != c {
		t.Errorf("same cell, different answers: %+v %+v", a, b)
	}
	box := geohash.BoundingBox(c.Hash)
	if next := CellAt(box.MaxLat+0.0001, c.Lng); next.Hash == c.Hash {
		t.Errorf("the cell north of %s is the same", c.Hash)
	}
}

// TestCellCoversRadius: whatever is within r of the exact point is within
// SearchRadius(r) of the cell centre (random points around Berlin).
func TestCellCoversRadius(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 10_000 {
		lat := 52.34 + rng.Float64()*0.34
		lng := 13.09 + rng.Float64()*0.67
		c := CellAt(lat, lng)
		if d := DistanceMeters(lat, lng, c.Lat, c.Lng); d > c.HalfDiagonalMeters-1 {
			t.Fatalf("point %v,%v is %.2f m from the centre of %s, half diagonal %v", lat, lng, d, c.Hash, c.HalfDiagonalMeters)
		}
		// A stop exactly r = 500 m away in a random direction.
		bearing := rng.Float64() * 2 * math.Pi
		sLat := lat + 500*math.Cos(bearing)/111_195
		sLng := lng + 500*math.Sin(bearing)/(111_195*math.Cos(lat*math.Pi/180))
		if d := DistanceMeters(c.Lat, c.Lng, sLat, sLng); d > c.SearchRadius(500) {
			t.Fatalf("stop %.1f m from the centre, search radius %v", d, c.SearchRadius(500))
		}
	}
}

func TestDistanceMeters(t *testing.T) {
	if d := DistanceMeters(weddingLat, weddingLng, weddingLat, weddingLng); d != 0 {
		t.Errorf("same point: %v", d)
	}
	// 0.001° of latitude ≈ 111.2 m.
	if d := DistanceMeters(weddingLat, weddingLng, weddingLat+0.001, weddingLng); math.Abs(d-111.2) > 0.1 {
		t.Errorf("0.001° north: %v", d)
	}
	// S+U Wedding → Alexanderplatz: 2.31 km south, 3.20 km east → 3.94 km.
	if d := DistanceMeters(weddingLat, weddingLng, 52.5219, 13.4132); math.Abs(d-3_942) > 5 {
		t.Errorf("Wedding → Alexanderplatz: %.0f m", d)
	}
}
