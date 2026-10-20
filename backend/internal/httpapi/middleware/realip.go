package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// RealIP sets Info.ClientIP. X-Forwarded-For is honoured only when the direct peer is a trusted
// proxy; the client is then the right-most address that is not itself a trusted proxy.
func RealIP(trusted []netip.Prefix) Middleware {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, info := withInfo(r)
			ip := peer(r.RemoteAddr)
			if ip.IsValid() && isTrusted(ip) {
				hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					ip = a
					if !isTrusted(a) {
						break
					}
				}
			}
			if ip.IsValid() {
				info.ClientIP = ip.Unmap().String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

func peer(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, _ := netip.ParseAddr(host)
	return a
}
