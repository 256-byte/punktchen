// Package transit is the public transport layer (ADR-0008): it builds the
// transit features — stations with their poles, directions and next
// departures — from the static schedule in Postgres and the departures
// snapshot.
package transit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// Layer metadata (/v1/layers).
const (
	LayerID     = "transit"
	LayerName   = "Public transport"
	Attribution = "Data: VBB (CC BY 4.0)"
)

// Departures per direction (DATA_MODEL §5.2).
const (
	NearbyDepartures  = 3
	StationDepartures = 10
)

// DefaultStaleAfter: health turns degraded when the snapshot is older, i.e.
// a few window refreshes in a row failed.
const DefaultStaleAfter = 3 * departures.DefaultRefreshEvery

// DefaultRealtimeStaleAfter: with realtime, health turns degraded when the
// feed merged into the snapshot is older (by header.timestamp; task 4.4).
const DefaultRealtimeStaleAfter = 2 * time.Minute

var (
	// ErrNoDepartures: no departures snapshot yet (before the first import
	// or while the database is unreachable at startup).
	ErrNoDepartures = errors.New("transit: no departures yet")
	// ErrUnknownStation: no station with this id has a served pole in scope.
	ErrUnknownStation = errors.New("transit: unknown station")
)

// Snapshots gives the latest departures snapshot (*departures.Builder).
type Snapshots interface {
	Current() *departures.Snapshot
}

// Provider is the transit layer.
type Provider struct {
	q          *queries.Queries
	snaps      Snapshots
	now        func() time.Time
	staleAfter time.Duration
	// rtStaleAfter > 0: realtime is expected; its age counts for health.
	rtStaleAfter time.Duration
}

// Option configures a Provider.
type Option func(*Provider)

// WithClock replaces the clock health uses (tests).
func WithClock(now func() time.Time) Option {
	return func(p *Provider) { p.now = now }
}

// WithRealtime makes realtime part of health: degraded without realtime data
// or when the merged feed is older than staleAfter.
func WithRealtime(staleAfter time.Duration) Option {
	return func(p *Provider) { p.rtStaleAfter = staleAfter }
}

var _ layer.Provider = (*Provider)(nil)

// NewProvider returns the transit layer over the live schedule and snapshots.
func NewProvider(pool *pgxpool.Pool, snaps Snapshots, opts ...Option) *Provider {
	p := &Provider{q: queries.New(pool), snaps: snaps, now: time.Now, staleAfter: DefaultStaleAfter}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Info describes the layer.
func (p *Provider) Info() layer.Info {
	return layer.Info{ID: LayerID, Name: LayerName, Attribution: []string{Attribution}}
}

// Version is the version of the current snapshot, 0 before the first one.
func (p *Provider) Version() uint64 {
	if s := p.snaps.Current(); s != nil {
		return s.Version
	}
	return 0
}

// Health is down without a snapshot and degraded when the snapshot is
// older than staleAfter. With realtime it is also degraded without realtime
// data or when the merged feed is older than rtStaleAfter; RealtimeAge is
// set whenever the snapshot has realtime data. The schedule is served in
// all those cases.
func (p *Provider) Health() layer.Health {
	s := p.snaps.Current()
	if s == nil {
		return layer.Health{Status: layer.StatusDown, LastError: ErrNoDepartures.Error()}
	}
	now := p.now()
	h := layer.Health{Status: layer.StatusOK}
	if !s.RealtimeFeedTime.IsZero() {
		age := max(now.Sub(s.RealtimeFeedTime), 0)
		h.RealtimeAge = &age
	}
	switch age := now.Sub(s.GeneratedAt); {
	case age > p.staleAfter:
		h.Status, h.LastError = layer.StatusDegraded, fmt.Sprintf("departures not refreshed for %s", age.Round(time.Second))
	case p.rtStaleAfter <= 0:
	case h.RealtimeAge == nil:
		h.Status, h.LastError = layer.StatusDegraded, "no realtime data yet; schedule only"
	case *h.RealtimeAge > p.rtStaleAfter:
		h.Status, h.LastError = layer.StatusDegraded, fmt.Sprintf("realtime feed %s old", h.RealtimeAge.Round(time.Second))
	}
	return h
}

// Nearby returns the stations with served poles within the radius, nearest
// first, each with only the poles inside the circle. Payload:
// *openapi.TransitLayer.
func (p *Provider) Nearby(ctx context.Context, q layer.NearbyQuery) (layer.Result, error) {
	s := p.snaps.Current()
	if s == nil {
		return layer.Result{}, ErrNoDepartures
	}
	rows, err := p.q.NearbyPoles(ctx, queries.NearbyPolesParams{
		Lat: q.Center.Lat, Lng: q.Center.Lng, RadiusM: q.RadiusMeters,
	})
	if err != nil {
		return layer.Result{}, fmt.Errorf("nearby poles: %w", err)
	}
	poles := make([]poleRow, len(rows))
	for i, r := range rows {
		dist := int32(r.StationDistanceM + 0.5)
		poles[i] = poleRow{
			station: stationRow{
				id: r.StationID, name: deref(r.StationName), lat: r.StationLat, lng: r.StationLng,
				modes: r.StationModes, distance: &dist,
			},
			id: r.StopID, platform: r.PlatformCode, description: nonEmpty(r.Description), lat: r.Lat, lng: r.Lng,
		}
	}
	features, err := p.build(ctx, s, poles, NearbyDepartures)
	if err != nil {
		return layer.Result{}, err
	}
	return layer.Result{
		Version: s.Version,
		Payload: &openapi.TransitLayer{Layer: openapi.TransitLayerLayerTransit, Features: features},
	}, nil
}

// Station returns one station with all its served poles in scope and up to
// StationDepartures departures per direction, plus the snapshot version it
// was built from.
func (p *Provider) Station(ctx context.Context, id string) (*openapi.Station, uint64, error) {
	s := p.snaps.Current()
	if s == nil {
		return nil, 0, ErrNoDepartures
	}
	rows, err := p.q.StationPoles(ctx, id)
	if err != nil {
		return nil, 0, fmt.Errorf("station poles: %w", err)
	}
	if len(rows) == 0 {
		return nil, 0, fmt.Errorf("%w: %q", ErrUnknownStation, id)
	}
	poles := make([]poleRow, len(rows))
	for i, r := range rows {
		poles[i] = poleRow{
			station: stationRow{id: r.StationID, name: r.StationName, lat: r.StationLat, lng: r.StationLng, modes: r.StationModes},
			id:      r.StopID, platform: r.PlatformCode, description: nonEmpty(r.Description), lat: r.Lat, lng: r.Lng,
		}
	}
	features, err := p.build(ctx, s, poles, StationDepartures)
	if err != nil {
		return nil, 0, err
	}
	if len(features) == 0 { // every pole lost its directions (terminal rule)
		return nil, 0, fmt.Errorf("%w: %q", ErrUnknownStation, id)
	}
	st := &features[0]
	sortSheetPoles(st.Poles)
	return st, s.Version, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
