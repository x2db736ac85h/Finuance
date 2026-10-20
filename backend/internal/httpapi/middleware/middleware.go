// Package middleware holds the HTTP middleware chain (docs/PLAN.md Phase 2 task 2.4):
// recover → request ID → real IP → logging → metrics → tracing → security headers → route recorder.
package middleware

import (
	"context"
	"net/http"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies mws so the first one is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Info is per-request state shared between the layers. The outermost middleware creates it; inner
// layers fill it in (the route pattern is only known after the mux has matched).
type Info struct {
	RequestID string
	ClientIP  string
	Route     string // ServeMux pattern, e.g. "GET /api/v1/meta"; "unmatched" for 404s
	TraceID   string
}

type infoKey struct{}

// FromContext returns the request's Info, or an empty one outside a request.
func FromContext(ctx context.Context) *Info {
	if i, ok := ctx.Value(infoKey{}).(*Info); ok {
		return i
	}
	return &Info{}
}

func withInfo(r *http.Request) (*http.Request, *Info) {
	if i, ok := r.Context().Value(infoKey{}).(*Info); ok {
		return r, i
	}
	i := &Info{}
	return r.WithContext(context.WithValue(r.Context(), infoKey{}, i)), i
}

// RequestID returns the request id stored in ctx ("" outside a request).
func RequestID(ctx context.Context) string { return FromContext(ctx).RequestID }

// recorder captures the status code and size while staying transparent to http.ResponseController.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func record(w http.ResponseWriter) *recorder {
	if rec, ok := w.(*recorder); ok {
		return rec
	}
	return &recorder{ResponseWriter: w}
}
