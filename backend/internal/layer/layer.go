// Package layer is the generic provider system behind /v1/nearby (ADR-0008).
//
// A Provider serves one kind of map content (a layer: transit, later toilets,
// alerts, ...). The Registry holds all providers, resolves the layers a
// request asks for and composes their results into one response. The package
// knows nothing about HTTP or the OpenAPI types: payloads are opaque here and
// the api package maps them to the typed contract fields.
package layer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// Status of a layer's data. The values match the contract's Status enum.
type Status string

const (
	StatusOK       Status = "ok"       // data is fresh
	StatusDegraded Status = "degraded" // data is served but stale
	StatusDown     Status = "down"     // no data can be served
)

// Info describes a layer for /v1/layers.
type Info struct {
	ID          string   // "transit"
	Name        string   // display name
	Attribution []string // texts that must be shown with the layer's data
}

// Location is a WGS84 point.
type Location struct {
	Lat, Lng float64
}

// NearbyQuery asks a provider for its features around a point.
type NearbyQuery struct {
	Center       Location
	RadiusMeters float64
}

// ErrInvalidQuery is returned for a query no provider can answer.
var ErrInvalidQuery = errors.New("invalid nearby query")

// Validate checks that the query is well-formed: coordinates in range and a
// positive radius. Product limits (100–1000 m) are the API's job.
func (q NearbyQuery) Validate() error {
	var errs []error
	if !finite(q.Center.Lat) || q.Center.Lat < -90 || q.Center.Lat > 90 {
		errs = append(errs, errors.New("lat must be within [-90, 90]"))
	}
	if !finite(q.Center.Lng) || q.Center.Lng < -180 || q.Center.Lng > 180 {
		errs = append(errs, errors.New("lng must be within [-180, 180]"))
	}
	if !finite(q.RadiusMeters) || q.RadiusMeters <= 0 {
		errs = append(errs, errors.New("radius must be positive"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidQuery, errors.Join(errs...))
	}
	return nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// Result is what a provider returns for one query.
type Result struct {
	// Version of the data the payload was built from. Taken from the same
	// data as the payload, so it can never belong to a newer or older state.
	Version uint64
	// Payload is the layer's own typed content (e.g. transit stations). The
	// api package knows each layer's type.
	Payload any
}

// Health is a provider's view of its own data. It must be cheap (no I/O).
type Health struct {
	Status Status
	// RealtimeAge is the age of the realtime data; nil for layers without
	// realtime data or before the first realtime update.
	RealtimeAge *time.Duration
	// LastError is the last data refresh error, empty if none.
	LastError string
}

// Provider serves one layer.
type Provider interface {
	// Info describes the layer. ID must be stable and unique.
	Info() Info
	// Nearby returns the layer's features around q.Center.
	Nearby(ctx context.Context, q NearbyQuery) (Result, error)
	// Version changes whenever the layer's data changes. Used for cache keys
	// before a response is built (ADR-0006).
	Version() uint64
	// Health reports freshness and the last error.
	Health() Health
}
