package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Logging writes one access-log line per request and puts a request-scoped logger (with request_id)
// into the context. Bodies, cookies and query strings are never logged. The line is written in a
// defer, so a request whose handler panics has one too, with status 500 (PLAN 24.1); the kubelet's
// probes are written at debug level (PLAN 24.3).
func Logging(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			start := time.Now()
			l := base.With(slog.String("request_id", info.RequestID))
			r = r.WithContext(observability.WithLogger(r.Context(), l))
			rec := record(w)
			panicked := true
			defer func() {
				status := rec.outcome(panicked)
				level := slog.LevelInfo
				switch {
				case status >= 500:
					level = slog.LevelError
				case IsProbe(r):
					level = slog.LevelDebug
				}
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", info.Route),
					slog.Int("status", status),
					slog.Int64("bytes", rec.bytes),
					slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
					slog.String("client_ip", info.ClientIP),
				}
				if info.TraceID != "" { // probes and requests refused before the tracing layer have none
					attrs = append(attrs, slog.String("trace_id", info.TraceID), slog.String("span_id", info.SpanID))
				}
				if info.UserHash != "" {
					attrs = append(attrs, slog.String("user", info.UserHash))
				}
				l.LogAttrs(r.Context(), level, "request", attrs...)
			}()
			next.ServeHTTP(rec, r)
			panicked = false
		})
	}
}
