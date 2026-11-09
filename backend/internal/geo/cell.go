package geo

import (
	"math"

	"github.com/mmcloughlin/geohash"
)

// CellPrecision is the geohash length of a cache cell (ADR-0006). In Berlin
// a cell is about 153 m north–south and 93 m east–west.
const CellPrecision = 7

// Cell is the geohash cell a /v1/nearby request is snapped to: everyone in
// the same cell gets the same answer (ADR-0006, task 5.1).
type Cell struct {
	Hash     string
	Lat, Lng float64 // centre
	// HalfDiagonalMeters is at least the distance from the centre to any
	// point of the cell (rounded up, plus a metre for the sphere-vs-spheroid
	// difference to PostGIS).
	HalfDiagonalMeters float64
}

// CellAt returns the cell that contains the point.
func CellAt(lat, lng float64) Cell {
	hash := geohash.EncodeWithPrecision(lat, lng, CellPrecision)
	box := geohash.BoundingBox(hash)
	cLat, cLng := box.Center()
	var half float64
	for _, corner := range [][2]float64{
		{box.MinLat, box.MinLng}, {box.MinLat, box.MaxLng}, {box.MaxLat, box.MinLng}, {box.MaxLat, box.MaxLng},
	} {
		half = math.Max(half, DistanceMeters(cLat, cLng, corner[0], corner[1]))
	}
	return Cell{Hash: hash, Lat: cLat, Lng: cLng, HalfDiagonalMeters: math.Ceil(half) + 1}
}

// SearchRadius is the radius to query around the cell centre so that
// everything within r of any point in the cell is included (triangle
// inequality); a user may get stops up to r + 2 × half diagonal away.
func (c Cell) SearchRadius(r float64) float64 {
	return r + c.HalfDiagonalMeters
}

// earthRadiusMeters is the mean Earth radius (IUGG).
const earthRadiusMeters = 6_371_008.8

// DistanceMeters is the great-circle (haversine) distance between two points.
func DistanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(a)))
}
