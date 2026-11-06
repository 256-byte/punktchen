// Package api implements the HTTP API defined in api/openapi.yaml.
// Handlers implement the strict server interface generated into
// backend/gen/openapi, so request parsing and response shapes come from the
// contract.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/platform"
)

// healthPingTimeout bounds the database check of /v1/health.
const healthPingTimeout = time.Second

// Pinger reports whether a dependency is reachable. *pgxpool.Pool fits.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server implements openapi.StrictServerInterface.
type Server struct {
	version string
	db      Pinger
	log     *slog.Logger
	now     func() time.Time
}

var _ openapi.StrictServerInterface = (*Server)(nil)

// NewServer returns the API implementation.
func NewServer(version string, db Pinger, log *slog.Logger) *Server {
	return &Server{version: version, db: db, log: log, now: time.Now}
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

// GetHealth reports the service status.
// Phase 0: "down" if the database is unreachable, else "ok". Schedule and
// realtime freshness (ARCHITECTURE §3.7) are added with those components.
func (s *Server) GetHealth(ctx context.Context, _ openapi.GetHealthRequestObject) (openapi.GetHealthResponseObject, error) {
	status := openapi.StatusOk

	pingCtx, cancel := context.WithTimeout(ctx, healthPingTimeout)
	defer cancel()
	if err := s.db.Ping(pingCtx); err != nil {
		s.log.Warn("health: database unreachable", "error", err.Error())
		status = openapi.StatusDown
	}

	return openapi.GetHealth200JSONResponse{
		Status:  status,
		Version: s.version,
		Time:    s.now().In(platform.Berlin).Truncate(time.Second),
	}, nil
}

// ListLayers is implemented in Phase 2 (task 2.5).
func (s *Server) ListLayers(context.Context, openapi.ListLayersRequestObject) (openapi.ListLayersResponseObject, error) {
	return openapi.ListLayersdefaultJSONResponse{StatusCode: http.StatusNotImplemented, Body: notImplemented()}, nil
}

// GetNearby is implemented in Phase 2 (task 2.5).
func (s *Server) GetNearby(context.Context, openapi.GetNearbyRequestObject) (openapi.GetNearbyResponseObject, error) {
	return openapi.GetNearbydefaultJSONResponse{StatusCode: http.StatusNotImplemented, Body: notImplemented()}, nil
}

// GetStation is implemented in Phase 2 (task 2.5).
func (s *Server) GetStation(context.Context, openapi.GetStationRequestObject) (openapi.GetStationResponseObject, error) {
	return openapi.GetStationdefaultJSONResponse{StatusCode: http.StatusNotImplemented, Body: notImplemented()}, nil
}

// Error codes (see Error.code in api/openapi.yaml).
const (
	codeInvalidArgument = "invalid_argument"
	codeNotFound        = "not_found"
	codeNotImplemented  = "not_implemented"
	codeInternal        = "internal"
)

func errorBody(code, message string) openapi.Error {
	var e openapi.Error
	e.Error.Code = code
	e.Error.Message = message
	return e
}

func notImplemented() openapi.Error {
	return errorBody(codeNotImplemented, "not implemented yet")
}

// writeError is for paths outside the generated handlers (router errors).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(code, message))
}
