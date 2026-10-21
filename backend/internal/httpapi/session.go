package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// SessionCookie is the name of the HttpOnly session cookie (docs/PLAN.md §6.1).
const SessionCookie = "finuance_session"

// publicRoutes are the API routes that work without a session. Every other /api/v1 route requires
// one (RequireSession). Keys are ServeMux patterns, as the generated router registers them.
var publicRoutes = map[string]bool{
	"GET /api/v1/meta":         true,
	"POST /api/v1/auth/signup": true,
	"POST /api/v1/auth/login":  true,
	"POST /api/v1/auth/logout": true,
}

// requestAuth is what the session layer knows about a request: the raw cookie (login rotates it,
// logout deletes it), the client (stored with a new session) and, on protected routes, the user.
type requestAuth struct {
	token    string
	client   auth.Client
	identity auth.Identity
}

type authKey struct{}

func authFrom(ctx context.Context) *requestAuth {
	if a, ok := ctx.Value(authKey{}).(*requestAuth); ok {
		return a
	}
	return &requestAuth{}
}

// identity returns the signed-in user of a protected route.
func identity(ctx context.Context) auth.Identity { return authFrom(ctx).identity }

// requireSession resolves the session cookie. It runs per matched API route (r.Pattern is set), so
// unknown paths still answer 404. Public routes pass without a session; protected ones answer 401.
func requireSession(svc *auth.Service, prod bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ra := &requestAuth{client: auth.Client{UserAgent: r.UserAgent()}}
			if ip, err := netip.ParseAddr(mw.FromContext(r.Context()).ClientIP); err == nil {
				ra.client.IP = ip
			}
			if c, err := r.Cookie(SessionCookie); err == nil {
				ra.token = c.Value
			}
			ctx := context.WithValue(r.Context(), authKey{}, ra)
			r = r.WithContext(ctx)
			if publicRoutes[r.Pattern] {
				next.ServeHTTP(w, r)
				return
			}
			if svc == nil || ra.token == "" {
				WriteProblem(w, r, platform.ErrUnauthenticated, prod)
				return
			}
			id, err := svc.Resolve(ctx, ra.token)
			if err != nil {
				WriteProblem(w, r, err, prod)
				return
			}
			ra.identity = id
			sum := sha256.Sum256(id.User.ID[:])
			mw.FromContext(ctx).UserHash = hex.EncodeToString(sum[:8])
			next.ServeHTTP(w, r)
		})
	}
}

// cookies builds Set-Cookie values. Secure is on unless FINUANCE_COOKIE_SECURE=false, which the
// config only allows in dev and test.
type cookies struct {
	secure bool
	ttl    time.Duration
}

func newCookies(cfg config.Config) cookies {
	return cookies{secure: cfg.CookieSecure || cfg.Env == config.EnvProd, ttl: cfg.SessionTTL}
}

func (c cookies) session(token string) *string {
	return c.cookie(token, int(c.ttl.Seconds()))
}

// clear expires the cookie (Max-Age=0).
func (c cookies) clear() *string { return c.cookie("", -1) }

func (c cookies) cookie(value string, maxAge int) *string {
	// Secure is off only when FINUANCE_COOKIE_SECURE=false, which config allows in dev and test.
	ck := http.Cookie{Name: SessionCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode} //nolint:gosec // G124: see above
	ck.Secure = c.secure
	s := ck.String()
	return &s
}
