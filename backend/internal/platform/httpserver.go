package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server is an HTTP server bound to its listener.
type Server struct {
	Name     string
	http     *http.Server
	listener net.Listener
}

// Listen binds addr right away, so a port that is already in use fails at
// startup, before anything is served.
func Listen(name, addr string, h http.Handler) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%s server: listen on %s: %w", name, addr, err)
	}
	return &Server{
		Name:     name,
		listener: ln,
		http: &http.Server{
			Handler:           h,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
	}, nil
}

// Addr returns the bound address (useful when listening on port 0).
func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Close releases the port of a server that was never passed to Serve,
// e.g. when a later startup step fails.
func (s *Server) Close() error { return s.listener.Close() }

// Serve runs all servers until ctx is cancelled or one of them fails.
// Then it shuts all of them down gracefully: in-flight requests get up to
// shutdownTimeout to finish, after which connections are closed.
// It returns nil after a clean shutdown triggered by ctx.
func Serve(ctx context.Context, log *slog.Logger, shutdownTimeout time.Duration, servers ...*Server) error {
	errc := make(chan error, len(servers))
	for _, s := range servers {
		log.Info("http server listening", "server", s.Name, "addr", s.Addr().String())
		go func() {
			err := s.http.Serve(s.listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			} else {
				err = fmt.Errorf("%s server: %w", s.Name, err)
			}
			errc <- err
		}()
	}

	var errs []error
	pending := len(servers)
	select {
	case <-ctx.Done():
		log.Info("shutting down", "reason", context.Cause(ctx).Error())
	case err := <-errc:
		// http.Server.Serve only returns before Shutdown on a real error.
		pending--
		log.Error("http server failed, shutting down", "error", err)
		errs = append(errs, err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range servers {
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("%s server: shutdown: %w", s.Name, err))
			_ = s.http.Close()
		}
	}
	for ; pending > 0; pending-- {
		if err := <-errc; err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
