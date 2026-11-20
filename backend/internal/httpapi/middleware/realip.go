package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

// RealIP sets Info.ClientIP. X-Forwarded-For is honoured only when the direct peer is a trusted
// proxy; the client is then the right-most address that is not itself a trusted proxy.
//
// onlyProxies (may be nil) is called once, the first time a forwarded request names nothing but
// trusted proxies: the proxy in front passes no client address, so every visitor counts as one
// (limits per address, the addresses in the activity log). A request without the header (a
// probe from the node) is not such a case.
func RealIP(trusted []netip.Prefix, onlyProxies func()) Middleware {
	var warned sync.Once
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
				forwarded := r.Header.Values("X-Forwarded-For")
				hops := strings.Split(strings.Join(forwarded, ","), ",")
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
				if len(forwarded) > 0 && isTrusted(ip) && onlyProxies != nil {
					warned.Do(onlyProxies)
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
