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
		{"garbage XFF falls back", "10.1.2.3:80", "not-an-ip", "10.1.2.3"},
		{"ipv6 proxy", "[fd00::1]:80", "2001:db8::5", "2001:db8::5"},
		{"ipv4-mapped peer", "[::ffff:203.0.113.9]:1", "", "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := RealIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = FromContext(r.Context()).ClientIP }))
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
