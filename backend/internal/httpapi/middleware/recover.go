package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns a panic into a 500 (written by onPanic when nothing was sent yet) and logs the stack
// with base: it is the outermost layer, so the request-scoped logger does not exist yet. It also
// creates the request Info that the inner layers fill in, so the log line can carry the request id.
// http.ErrAbortHandler is re-raised: it is the stdlib's way to abort a response on purpose.
func Recover(base *slog.Logger, onPanic func(http.ResponseWriter, *http.Request, error)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			rec := record(w)
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				base.LogAttrs(r.Context(), slog.LevelError, "panic", slog.String("request_id", info.RequestID),
					slog.String("panic", fmt.Sprint(v)), slog.String("stack", string(debug.Stack())))
				if rec.status == 0 {
					onPanic(rec, r, fmt.Errorf("panic: %v", v))
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
