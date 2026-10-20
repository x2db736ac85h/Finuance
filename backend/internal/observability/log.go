// Package observability sets up logging, metrics, tracing and the admin server (docs/PLAN.md §8.2).
package observability

import (
	"context"
	"io"
	"log/slog"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// NewLogger returns a JSON logger in prod and a human-readable text logger elsewhere.
func NewLogger(w io.Writer, env config.Env, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if env == config.EnvProd {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

type loggerKey struct{}

// WithLogger stores a request-scoped logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Logger returns the request-scoped logger from ctx, or slog.Default().
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
