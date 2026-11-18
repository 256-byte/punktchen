package amenities

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/layer"
)

// Layer metadata (/v1/layers, ADR-0010).
const (
	ToiletsLayerID     = "toilets"
	ToiletsLayerName   = "Public toilets"
	FountainsLayerID   = "fountains"
	FountainsLayerName = "Drinking fountains"
	// Attribution is not required by dl-de/zero-2-0; we name the source anyway.
	Attribution = "Data: Geoportal Berlin (dl-de/zero-2-0)"
)

// Sparse-layer rule (ADR-0010): every feature within the radius, filled up
// to MinResults with the nearest within MaxDistanceMeters.
const (
	MinResults        = 3
	MaxDistanceMeters = 2000
)

// DefaultStaleAfter: health turns degraded when a dataset was not confirmed
// current (import succeeded or answer unchanged) for this long.
const DefaultStaleAfter = 72 * time.Hour

// state is what a provider knows about its dataset without I/O; Refresh
// reads it from meta.feed_imports.
type state struct {
	version   uint64     // id of the last successful import; 0 = no data yet
	checkedAt *time.Time // last import that succeeded or found the data unchanged
	lastError string
}

// provider holds what the toilet and fountain layers share.
type provider struct {
	pool       *pgxpool.Pool
	source     string
	info       layer.Info
	now        func() time.Time
	staleAfter time.Duration

	mu sync.RWMutex
	st state
}

func newProvider(pool *pgxpool.Pool, source, id, name string) provider {
	return provider{
		pool: pool, source: source, now: time.Now, staleAfter: DefaultStaleAfter,
		info: layer.Info{ID: id, Name: name, Attribution: []string{Attribution}},
	}
}

// Info describes the layer.
func (p *provider) Info() layer.Info { return p.info }

// Version is the id of the last successful import: it changes only when
// the stored rows do (cache keys, ADR-0006).
func (p *provider) Version() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.st.version
}

// Health: down without data, degraded when the data was not confirmed
// current within staleAfter or the last refresh failed.
func (p *provider) Health() layer.Health {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h := layer.Health{Status: layer.StatusOK, LastError: p.st.lastError}
	switch {
	case p.st.version == 0:
		h.Status = layer.StatusDown
	case p.st.lastError != "", p.st.checkedAt == nil, p.now().Sub(*p.st.checkedAt) > p.staleAfter:
		h.Status = layer.StatusDegraded
	}
	return h
}

// Refresh reads version and freshness from meta.feed_imports. The scheduler
// calls it after every run; main calls it once at startup.
func (p *provider) Refresh(ctx context.Context) error {
	q := queries.New(p.pool)
	var st state
	last, err := q.LastSuccessfulFeedImport(ctx, p.source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return p.failed(fmt.Errorf("refresh %s: %w", p.source, err))
	default:
		st.version = uint64(last.ID) //nolint:gosec // identity column, positive
	}
	checked, err := q.LastScheduleCheck(ctx, p.source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return p.failed(fmt.Errorf("refresh %s: %w", p.source, err))
	default:
		st.checkedAt = checked
	}
	p.mu.Lock()
	p.st = st
	p.mu.Unlock()
	return nil
}

func (p *provider) failed(err error) error {
	p.mu.Lock()
	p.st.lastError = err.Error()
	p.mu.Unlock()
	return err
}

// read runs fn in a read-only REPEATABLE READ transaction together with the
// version, so the payload and its version always describe the same rows
// even while an import replaces them.
func (p *provider) read(ctx context.Context, fn func(q *queries.Queries) error) (uint64, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := queries.New(tx)
	var version uint64
	last, err := q.LastSuccessfulFeedImport(ctx, p.source)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, fmt.Errorf("%s: no data yet", p.info.ID)
	case err != nil:
		return 0, err
	default:
		version = uint64(last.ID) //nolint:gosec // identity column, positive
	}
	if err := fn(q); err != nil {
		return 0, err
	}
	return version, tx.Commit(ctx)
}

func meters(m float64) int32 { return int32(math.Round(m)) }

// ToiletsProvider is the "toilets" layer.
type ToiletsProvider struct{ provider }

var _ layer.Provider = (*ToiletsProvider)(nil)

// NewToiletsProvider returns the toilet layer on pool; call Refresh before serving.
func NewToiletsProvider(pool *pgxpool.Pool) *ToiletsProvider {
	return &ToiletsProvider{newProvider(pool, SourceToilets, ToiletsLayerID, ToiletsLayerName)}
}

// Nearby returns the toilets around q (sparse-layer rule), nearest first.
func (p *ToiletsProvider) Nearby(ctx context.Context, q layer.NearbyQuery) (layer.Result, error) {
	var rows []queries.NearbyToiletsRow
	version, err := p.read(ctx, func(qs *queries.Queries) (err error) {
		rows, err = qs.NearbyToilets(ctx, queries.NearbyToiletsParams{
			Lat: q.Center.Lat, Lng: q.Center.Lng, RadiusM: q.RadiusMeters, MaxM: MaxDistanceMeters, MinResults: MinResults,
		})
		return err
	})
	if err != nil {
		return layer.Result{}, err
	}
	out := &openapi.ToiletsLayer{Layer: openapi.ToiletsLayerLayerToilets, Features: make([]openapi.Toilet, len(rows))}
	for i, r := range rows {
		out.Features[i] = openapi.Toilet{
			Id: r.ID, Kind: openapi.ToiletKind(r.Kind), Location: openapi.LatLng{Lat: r.Lat, Lng: r.Lng},
			DistanceMeters: meters(r.DistanceM), Address: r.Address, District: r.District, Operator: r.Operator,
			EventOnly: r.EventOnly, OpeningHours: r.OpeningHours, Open24h: r.Open24h, FeeEuro: r.FeeEur,
			Payment: r.Payment, Accessible: r.Accessible, MostlyAccessible: r.MostlyAccessible, ChangingTable: r.ChangingTable,
		}
	}
	return layer.Result{Version: version, Payload: out}, nil
}

// FountainsProvider is the "fountains" layer.
type FountainsProvider struct{ provider }

var _ layer.Provider = (*FountainsProvider)(nil)

// NewFountainsProvider returns the fountain layer on pool; call Refresh before serving.
func NewFountainsProvider(pool *pgxpool.Pool) *FountainsProvider {
	return &FountainsProvider{newProvider(pool, SourceFountains, FountainsLayerID, FountainsLayerName)}
}

// Nearby returns the drinking fountains around q (sparse-layer rule), nearest first.
func (p *FountainsProvider) Nearby(ctx context.Context, q layer.NearbyQuery) (layer.Result, error) {
	var rows []queries.NearbyFountainsRow
	version, err := p.read(ctx, func(qs *queries.Queries) (err error) {
		rows, err = qs.NearbyFountains(ctx, queries.NearbyFountainsParams{
			Lat: q.Center.Lat, Lng: q.Center.Lng, RadiusM: q.RadiusMeters, MaxM: MaxDistanceMeters, MinResults: MinResults,
		})
		return err
	})
	if err != nil {
		return layer.Result{}, err
	}
	out := &openapi.FountainsLayer{Layer: openapi.FountainsLayerLayerFountains, Features: make([]openapi.Fountain, len(rows))}
	for i, r := range rows {
		var built *int32
		if r.BuiltYear != nil {
			y := int32(*r.BuiltYear)
			built = &y
		}
		out.Features[i] = openapi.Fountain{
			Id: r.ID, Location: openapi.LatLng{Lat: r.Lat, Lng: r.Lng}, DistanceMeters: meters(r.DistanceM),
			Place: r.Location, District: r.District, Model: r.Model, BuiltYear: built,
			Season:       openapi.Season{FromMonth: int32(r.SeasonFromMonth), ToMonth: int32(r.SeasonToMonth)},
			OutOfService: r.OutOfService,
		}
	}
	return layer.Result{Version: version, Payload: out}, nil
}
