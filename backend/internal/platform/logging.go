// Package platform holds process-level infrastructure shared by all
// components: logging, the database pool, metrics and HTTP server lifecycle.
// It contains no domain logic.
package platform

import (
	"io"
	"log/slog"
)

// NewLogger returns a JSON logger writing one object per line.
// Components derive their own logger with logger.With("component", "<name>").
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
