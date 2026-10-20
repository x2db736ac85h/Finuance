package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Logging writes one access-log line per request and puts a request-scoped logger (with request_id)
// into the context. Bodies, cookies and query strings are never logged.
func Logging(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			start := time.Now()
			l := base.With(slog.String("request_id", info.RequestID))
			r = r.WithContext(observability.WithLogger(r.Context(), l))
			rec := record(w)
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			if rec.code() >= 500 {
				level = slog.LevelError
			}
			l.LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("route", info.Route),
				slog.Int("status", rec.code()),
				slog.Int64("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("client_ip", info.ClientIP),
				slog.String("trace_id", info.TraceID),
			)
		})
	}
}
