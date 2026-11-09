// Package geo holds geographic helpers: the scope bounding box and the
// geohash cells /v1/nearby snaps requests to (ADR-0006).
package geo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BBox is an axis-aligned bounding box in WGS84 degrees.
type BBox struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// ParseBBox parses "minLon,minLat,maxLon,maxLat".
func ParseBBox(s string) (BBox, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return BBox{}, fmt.Errorf("want 4 comma-separated numbers (minLon,minLat,maxLon,maxLat), got %d", len(parts))
	}
	var v [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return BBox{}, fmt.Errorf("value %d (%q) is not a number", i+1, p)
		}
		v[i] = f
	}
	b := BBox{MinLon: v[0], MinLat: v[1], MaxLon: v[2], MaxLat: v[3]}
	if err := b.Validate(); err != nil {
		return BBox{}, err
	}
	return b, nil
}

// Validate checks coordinate ranges and that min < max on both axes.
func (b BBox) Validate() error {
	var errs []error
	if b.MinLon < -180 || b.MaxLon > 180 {
		errs = append(errs, errors.New("longitude must be within [-180, 180]"))
	}
	if b.MinLat < -90 || b.MaxLat > 90 {
		errs = append(errs, errors.New("latitude must be within [-90, 90]"))
	}
	if b.MinLon >= b.MaxLon {
		errs = append(errs, errors.New("minLon must be less than maxLon"))
	}
	if b.MinLat >= b.MaxLat {
		errs = append(errs, errors.New("minLat must be less than maxLat"))
	}
	return errors.Join(errs...)
}

// String returns the same format ParseBBox accepts.
func (b BBox) String() string {
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
	return f(b.MinLon) + "," + f(b.MinLat) + "," + f(b.MaxLon) + "," + f(b.MaxLat)
}
