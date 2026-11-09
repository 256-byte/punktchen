package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/geo"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit"
)

// Product limits of /v1/nearby (DATA_MODEL §5.1, api/openapi.yaml).
const (
	minRadius     = 100
	maxRadius     = 1000
	defaultRadius = 500
)

// defaultLayers are served when a request names none.
var defaultLayers = []string{transit.LayerID}

// cacheControl: clients revalidate every time (DATA_MODEL §5.4); ETag/304
// follow with the cell cache (Phase 5).
const cacheControl = "no-cache"

// Layers composes the layers for /v1/nearby and lists them (*layer.Registry).
type Layers interface {
	Layers() []layer.Info
	Nearby(ctx context.Context, q layer.NearbyQuery, ids []string) (*layer.Composite, error)
}

// TransitStations serves the station sheet (*transit.Provider).
type TransitStations interface {
	Info() layer.Info
	Health() layer.Health
	Station(ctx context.Context, id string) (*openapi.Station, uint64, error)
}

// ListLayers lists the registered layers.
func (s *Server) ListLayers(context.Context, openapi.ListLayersRequestObject) (openapi.ListLayersResponseObject, error) {
	infos := s.cfg.Layers.Layers()
	out := openapi.ListLayers200JSONResponse{Layers: make([]openapi.LayerInfo, len(infos))}
	for i, info := range infos {
		out.Layers[i] = openapi.LayerInfo{Id: openapi.LayerId(info.ID), Name: info.Name, Attribution: nonNil(info.Attribution)}
	}
	return out, nil
}

// GetNearby returns the requested layers' features around a point. A layer
// that fails is reported as down in meta and left out of layers; the
// request still succeeds (ARCHITECTURE §3.5).
func (s *Server) GetNearby(ctx context.Context, req openapi.GetNearbyRequestObject) (openapi.GetNearbyResponseObject, error) {
	p := req.Params
	radius, ids, msg := nearbyParams(p)
	if msg != "" {
		return badNearby(msg), nil
	}
	// Snap to the cache cell (ADR-0006): everyone in the cell gets the
	// answer for its centre, with the radius grown so that nothing within
	// radius of the exact point is missed.
	cell := geo.CellAt(p.Lat, p.Lng)
	q := layer.NearbyQuery{Center: layer.Location{Lat: cell.Lat, Lng: cell.Lng}, RadiusMeters: cell.SearchRadius(float64(radius))}
	comp, err := s.cfg.Layers.Nearby(ctx, q, ids)
	switch {
	case errors.Is(err, layer.ErrUnknownLayer), errors.Is(err, layer.ErrInvalidQuery):
		return badNearby(err.Error()), nil
	case err != nil:
		return nil, err
	}

	body := openapi.NearbyResponse{Meta: s.meta(comp.Attribution)}
	body.Meta.Cell = &cell.Hash
	for _, l := range comp.Layers {
		lm := layerMeta(l.Version, l.Health)
		if l.Err != nil {
			s.log.Warn("layer failed", "layer", l.Info.ID, "error", l.Err.Error())
		} else {
			switch payload := l.Payload.(type) {
			case *openapi.TransitLayer:
				body.Layers.Transit = payload
			default:
				s.log.Error("layer payload of unknown type", "layer", l.Info.ID, "type", fmt.Sprintf("%T", l.Payload))
				lm.Status = openapi.StatusDown
			}
		}
		body.Meta.Layers[l.Info.ID] = lm
	}
	cc := cacheControl
	return openapi.GetNearby200JSONResponse{Body: body, Headers: openapi.GetNearby200ResponseHeaders{CacheControl: &cc}}, nil
}

// nearbyParams checks what the generated binding does not (ranges, layer
// list) and applies the defaults. msg is set for a bad request.
func nearbyParams(p openapi.GetNearbyParams) (radius int32, ids []string, msg string) {
	var problems []string
	if math.IsNaN(p.Lat) || p.Lat < -90 || p.Lat > 90 {
		problems = append(problems, "lat must be between -90 and 90")
	}
	if math.IsNaN(p.Lng) || p.Lng < -180 || p.Lng > 180 {
		problems = append(problems, "lng must be between -180 and 180")
	}
	radius = defaultRadius
	if p.Radius != nil {
		radius = *p.Radius
		if radius < minRadius || radius > maxRadius {
			problems = append(problems, fmt.Sprintf("radius must be between %d and %d", minRadius, maxRadius))
		}
	}
	ids = defaultLayers
	if p.Layers != nil {
		ids = nil
		for _, id := range *p.Layers {
			switch {
			case id == "":
				problems = append(problems, "layers must not contain empty ids")
			case slices.Contains(ids, string(id)):
				problems = append(problems, fmt.Sprintf("layer %q is listed twice", id))
			default:
				ids = append(ids, string(id))
			}
		}
		if len(*p.Layers) == 0 {
			problems = append(problems, "layers must name at least one layer")
		}
	}
	return radius, ids, strings.Join(problems, "; ")
}

func badNearby(msg string) openapi.GetNearbyResponseObject {
	return openapi.GetNearby400JSONResponse{BadRequestJSONResponse: openapi.BadRequestJSONResponse(errorBody(codeInvalidArgument, msg))}
}

// GetStation returns one transit station with all its poles (sheet).
func (s *Server) GetStation(ctx context.Context, req openapi.GetStationRequestObject) (openapi.GetStationResponseObject, error) {
	st, version, err := s.cfg.Transit.Station(ctx, req.StationId)
	switch {
	case errors.Is(err, transit.ErrUnknownStation):
		return openapi.GetStation404JSONResponse{NotFoundJSONResponse: openapi.NotFoundJSONResponse(
			errorBody(codeNotFound, fmt.Sprintf("no station %q with departures", req.StationId)))}, nil
	case errors.Is(err, transit.ErrNoDepartures):
		return openapi.GetStationdefaultJSONResponse{StatusCode: http.StatusServiceUnavailable,
			Body: errorBody(codeUnavailable, "no departures yet, try again later")}, nil
	case err != nil:
		return nil, err
	}
	info := s.cfg.Transit.Info()
	meta := s.meta(info.Attribution)
	meta.Layers[info.ID] = layerMeta(version, s.cfg.Transit.Health())
	cc := cacheControl
	return openapi.GetStation200JSONResponse{
		Body:    openapi.StationResponse{Meta: meta, Station: *st},
		Headers: openapi.GetStation200ResponseHeaders{CacheControl: &cc},
	}, nil
}

// meta starts a response's meta; the caller adds the layers.
func (s *Server) meta(attribution []string) openapi.ResponseMeta {
	return openapi.ResponseMeta{
		GeneratedAt: s.now().In(platform.Berlin).Truncate(time.Second),
		Attribution: nonNil(attribution),
		Layers:      map[string]openapi.LayerMeta{},
	}
}

func layerMeta(version uint64, h layer.Health) openapi.LayerMeta {
	m := openapi.LayerMeta{Version: int64(version), Status: openapi.Status(h.Status)} //nolint:gosec // versions stay far below 2^63 (Unix ms + count)
	if h.RealtimeAge != nil {
		age := int32(min(h.RealtimeAge.Seconds(), math.MaxInt32))
		m.RealtimeAgeSeconds = &age
	}
	return m
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
