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
// Durations are written as text ("2m38s"), not as nanoseconds.
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindDuration {
				return slog.String(a.Key, a.Value.Duration().String())
			}
			return a
		},
	}))
}
