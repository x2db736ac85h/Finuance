// Package observability sets up logging, metrics, tracing and the admin server (docs/PLAN.md §8.2).
package observability

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// NewLogger returns a JSON logger in prod and a human-readable text logger elsewhere. Errors logged
// as values go through platform.SafeError (PLAN 23.4), and every record logged with a context that
// carries a span gets its trace_id and span_id (PLAN 24.2).
func NewLogger(w io.Writer, env config.Env, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: safeErrors}
	if env == config.EnvProd {
		return slog.New(TraceHandler(slog.NewJSONHandler(w, opts)))
	}
	return slog.New(TraceHandler(slog.NewTextHandler(w, opts)))
}

// TraceHandler wraps h so that a record logged with a context inside a span carries that span's
// trace_id and span_id: any log line can then be joined to its trace, whichever code wrote it.
// It needs the call site to pass its context (InfoContext and friends; sloglint enforces it).
func TraceHandler(h slog.Handler) slog.Handler { return traceHandler{h} }

type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

type loggerKey struct{}

// WithLogger stores a request-scoped logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Logger returns the request- or job-scoped logger from ctx, or slog.Default().
func Logger(ctx context.Context) *slog.Logger { return LoggerOr(ctx, slog.Default()) }

// LoggerOr returns the request- or job-scoped logger from ctx, or fallback outside both.
func LoggerOr(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return fallback
}

// LogFields is the contract of the fields that mean the same on every log line
// (docs/observability.md lists them with their meaning; TestLogFieldContract keeps the two in step).
// A line may add fields of its own (count, import, error, …); it never reuses one of these names
// for something else.
var LogFields = []string{
	"time", "level", "msg", // slog's own
	"request_id", "trace_id", "span_id", // joining a line to a request and its trace
	"user",                                                           // platform.UserHash, never the id
	"route", "method", "status", "bytes", "duration_ms", "client_ip", // the access line
	"job_id", "kind", "attempt", // a job's lines
	"traffic", // real or synthetic (ADR 0041): on the access line and on a job's lines
}
