package middleware

import "net/http"

// SecurityHeaders sets the baseline response headers (docs/PLAN.md §8.1). HSTS is set at the
// ingress, CSP by nginx for the SPA; API responses are JSON and never cached.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// MaxBytes limits request bodies to n bytes (JSON default 1 MiB). routes raises the limit for
// specific "METHOD /path" keys — the upload endpoint gets FINUANCE_MAX_UPLOAD_BYTES plus room for
// the multipart framing. The path is matched exactly, before routing.
func MaxBytes(n int64, routes map[string]int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody {
				limit := n
				if l, ok := routes[r.Method+" "+r.URL.Path]; ok {
					limit = l
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
