package middleware

import (
	"fmt"
	"net/http"
	"net/url"
)

// ClientHeader must be present, with this value, on every unsafe request (docs/PLAN.md §6.1).
const (
	ClientHeader = "X-Finuance-Client"
	ClientValue  = "web"
)

// CSRF rejects unsafe requests (anything but GET, HEAD, OPTIONS) that lack the X-Finuance-Client
// header or carry an Origin other than the instance's public origin. A cross-site form can't set a
// custom header, and cross-origin JavaScript would need a CORS preflight, which is never granted;
// the Origin check covers browsers and proxies that strip nothing. reject writes the 403.
func CSRF(publicOrigin *url.URL, reject func(http.ResponseWriter, *http.Request, error)) Middleware {
	want := ""
	if publicOrigin != nil {
		want = publicOrigin.Scheme + "://" + publicOrigin.Host
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get(ClientHeader) != ClientValue {
				reject(w, r, fmt.Errorf("missing %s header", ClientHeader))
				return
			}
			if o := r.Header.Get("Origin"); o != "" && o != want {
				reject(w, r, fmt.Errorf("origin %q is not allowed", o))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
