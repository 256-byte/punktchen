// Package api implements the HTTP API defined in api/openapi.yaml.
// Handlers implement the strict server interface generated into
// backend/gen/openapi, so request parsing and response shapes come from the
// contract.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit/static"
)

const (
	// healthTimeout bounds the database checks of /v1/health.
	healthTimeout = time.Second
	// scheduleMaxAge: an older confirmation makes health "degraded"
	// (ARCHITECTURE §3.7). VBB publishes about twice a week.
	scheduleMaxAge = 72 * time.Hour
)

// Pinger reports whether a dependency is reachable. *pgxpool.Pool fits.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ScheduleSource tells whether schedule data exists and how fresh it is.
type ScheduleSource interface {
	ScheduleState(ctx context.Context) (static.ScheduleState, error)
}

// ImportTrigger starts an import in the background (static.Scheduler).
type ImportTrigger interface {
	Trigger() error
}

// Config holds the dependencies of the API.
type Config struct {
	Version  string
	DB       Pinger
	Schedule ScheduleSource
	Layers   Layers          // /v1/layers, /v1/nearby
	Transit  TransitStations // /v1/stations/{id}
	Imports  ImportTrigger   // used only when Dev
	Dev      bool            // LIVESTOP_DEV: enables /internal/*
	Log      *slog.Logger

	// Metrics gets the cell cache metrics (nil: not exported).
	Metrics prometheus.Registerer
	// CacheEntries and CacheTTL bound the /v1/nearby cell cache (0: the
	// defaults DefaultCacheEntries, DefaultCacheTTL).
	CacheEntries int
	CacheTTL     time.Duration
}

// Server implements openapi.StrictServerInterface.
type Server struct {
	cfg    Config
	log    *slog.Logger
	now    func() time.Time
	nearby *cellCache[*openapi.NearbyResponse]
}

var _ openapi.StrictServerInterface = (*Server)(nil)

// NewServer returns the API implementation.
func NewServer(cfg Config) *Server {
	if cfg.CacheEntries <= 0 {
		cfg.CacheEntries = DefaultCacheEntries
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}
	s := &Server{cfg: cfg, log: cfg.Log, now: time.Now}
	s.nearby = newCellCache[*openapi.NearbyResponse](cfg.CacheEntries, cfg.CacheTTL, func() time.Time { return s.now() }, cfg.Metrics)
	return s
}

// NewHandler wires the generated router to s. Parameter errors and unknown
// paths answer with the contract's Error JSON.
func NewHandler(s *Server) http.Handler {
	strict := openapi.NewStrictHandlerWithOptions(s, nil, openapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, codeInvalidArgument, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Error("request failed", "path", r.URL.Path, "error", err.Error())
			writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "no such endpoint")
	})
	return openapi.HandlerWithOptions(strict, openapi.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, codeInvalidArgument, err.Error())
		},
	})
}

// GetHealth reports the service status (ARCHITECTURE §3.7):
// down if the database is unreachable or there is no schedule; degraded if
// the schedule was not confirmed current in the last 3 days. Realtime
// freshness is added with the poller (Phase 4).
func (s *Server) GetHealth(ctx context.Context, _ openapi.GetHealthRequestObject) (openapi.GetHealthResponseObject, error) {
	now := s.now()
	resp := openapi.GetHealth200JSONResponse{
		Status:  openapi.StatusOk,
		Version: s.cfg.Version,
		Time:    now.In(platform.Berlin).Truncate(time.Second),
	}

	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := s.cfg.DB.Ping(ctx); err != nil {
		s.log.Warn("health: database unreachable", "error", err.Error())
		resp.Status = openapi.StatusDown
		return resp, nil
	}
	st, err := s.cfg.Schedule.ScheduleState(ctx)
	if err != nil {
		s.log.Warn("health: schedule state unknown", "error", err.Error())
		resp.Status = openapi.StatusDown
		return resp, nil
	}
	if st.CheckedAt != nil {
		at := st.CheckedAt.In(platform.Berlin).Truncate(time.Second)
		resp.ScheduleCheckedAt = &at
	}
	switch {
	case !st.HasData:
		resp.Status = openapi.StatusDown
	case st.CheckedAt == nil || now.Sub(*st.CheckedAt) > scheduleMaxAge:
		resp.Status = openapi.StatusDegraded
	case s.cfg.Transit != nil && s.cfg.Transit.Health().Status != layer.StatusOK:
		// Departures not refreshed, or realtime missing or older than 2 min
		// (task 4.4): the schedule is still served.
		resp.Status = openapi.StatusDegraded
	}
	return resp, nil
}

// TriggerImport starts a GTFS import in the background; dev only.
func (s *Server) TriggerImport(context.Context, openapi.TriggerImportRequestObject) (openapi.TriggerImportResponseObject, error) {
	if !s.cfg.Dev || s.cfg.Imports == nil {
		return openapi.TriggerImport404JSONResponse{NotFoundJSONResponse: openapi.NotFoundJSONResponse(errorBody(codeNotFound, "no such endpoint"))}, nil
	}
	switch err := s.cfg.Imports.Trigger(); {
	case errors.Is(err, static.ErrImportRunning):
		return openapi.TriggerImport409JSONResponse(errorBody(codeConflict, "an import is already running")), nil
	case err != nil:
		return nil, err
	}
	s.log.Info("gtfs import triggered via /internal/import")
	return openapi.TriggerImport202JSONResponse{Status: openapi.ImportStartedStatusStarted}, nil
}

// Error codes (see Error.code in api/openapi.yaml).
const (
	codeInvalidArgument = "invalid_argument"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codeInternal        = "internal"
	codeUnavailable     = "unavailable"
)

func errorBody(code, message string) openapi.Error {
	var e openapi.Error
	e.Error.Code = code
	e.Error.Message = message
	return e
}

// writeError is for paths outside the generated handlers (router errors).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(code, message))
}
