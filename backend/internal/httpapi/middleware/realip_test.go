package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRealIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	for _, tc := range []struct {
		name, remote, xff, want string
	}{
		{"direct client", "203.0.113.5:4000", "", "203.0.113.5"},
		{"untrusted peer ignores XFF", "203.0.113.5:4000", "1.2.3.4", "203.0.113.5"},
		{"trusted proxy uses XFF", "10.1.2.3:80", "198.51.100.7", "198.51.100.7"},
		{"right-most untrusted hop", "10.1.2.3:80", "1.1.1.1, 198.51.100.7, 10.9.9.9", "198.51.100.7"},
		// a client's own header ends up left of what the proxies append and is never believed
		{"forged left-most hop", "10.1.2.3:80", "10.200.0.1, 127.0.0.1, 198.51.100.7, 10.9.9.9", "198.51.100.7"},
		// the proxy in front passed no client address: the nearest proxy is all there is
		{"proxies only", "10.1.2.3:80", "10.9.9.9", "10.9.9.9"},
		{"garbage XFF falls back", "10.1.2.3:80", "not-an-ip", "10.1.2.3"},
		{"ipv6 proxy", "[fd00::1]:80", "2001:db8::5", "2001:db8::5"},
		{"ipv4-mapped peer", "[::ffff:203.0.113.9]:1", "", "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := RealIP(trusted, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()).ClientIP }))
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got != tc.want {
				t.Errorf("client ip = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRealIPWarnsOnceWhenOnlyProxiesAreNamed: the first forwarded request that names only trusted
// proxies is reported, once; direct requests (a probe) and requests with a client address are not.
func TestRealIPWarnsOnceWhenOnlyProxiesAreNamed(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	warnings := 0
	h := RealIP(trusted, func() { warnings++ })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	send := func(remote, xff string) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	send("10.1.2.3:80", "")              // a probe from inside the trusted range
	send("10.1.2.3:80", "198.51.100.7")  // a client behind the proxy
	send("203.0.113.5:4000", "10.9.9.9") // an untrusted peer's header is not read at all
	if warnings != 0 {
		t.Fatalf("%d warnings before any request named only proxies", warnings)
	}
	send("10.1.2.3:80", "10.9.9.9")
	send("10.1.2.3:80", "10.9.9.9, 10.8.8.8")
	if warnings != 1 {
		t.Errorf("%d warnings, want exactly one", warnings)
	}
}
