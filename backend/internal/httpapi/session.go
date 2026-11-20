package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// SessionCookie is the name of the HttpOnly session cookie (docs/PLAN.md §6.1) where cookies are
// not Secure (development and tests over http). Over https it is SecureSessionCookie: the __Host-
// prefix makes the browser accept the cookie only with Secure, Path=/ and no Domain, so another
// host of the same site (or a page over http) can't set or overwrite it (SYS-4).
const (
	SessionCookie       = "finuance_session"
	SecureSessionCookie = "__Host-finuance_session"
)

// sessionCookieName is the cookie's name for an instance.
func sessionCookieName(secure bool) string {
	if secure {
		return SecureSessionCookie
	}
	return SessionCookie
}

// publicRoutes are the API routes that work without a session. Every other /api/v1 route requires
// one (RequireSession). Keys are ServeMux patterns, as the generated router registers them.
var publicRoutes = map[string]bool{
	"GET /api/v1/meta":         true,
	"POST /api/v1/auth/signup": true,
	"POST /api/v1/auth/login":  true,
	// the second step of signing in: the challenge it carries stands in for the session (ADR 0040)
	"POST /api/v1/auth/login/second-step": true,
	"POST /api/v1/auth/logout":            true,
	"POST /api/v1/auth/demo":              true, // answers 404 outside demo mode
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

// requireSession resolves the session cookie, or a personal access token sent as "Authorization:
// Bearer fin_…" (ADR 0036; cookies are then ignored). It runs per matched API route (r.Pattern is
// set), so unknown paths still answer 404. Public routes pass without a session; protected ones
// answer 401.
func requireSession(svc *auth.Service, adm *admin.Service, prod bool, tokens *mw.Limiter, cookie string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ra := &requestAuth{client: auth.Client{UserAgent: r.UserAgent()}}
			if ip, err := netip.ParseAddr(mw.FromContext(r.Context()).ClientIP); err == nil {
				ra.client.IP = ip
			}
			ctx := context.WithValue(r.Context(), authKey{}, ra)
			r = r.WithContext(ctx)
			if h := r.Header.Get("Authorization"); h != "" {
				if err := resolveBearer(ctx, svc, adm, r.Pattern, h, ra, tokens); err != nil {
					WriteProblem(w, r, err, prod)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if c, err := r.Cookie(cookie); err == nil {
				ra.token = c.Value
			}
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
			mw.FromContext(ctx).UserHash = platform.UserHash(id.User.ID)
			if err := gate(ctx, r.Pattern, id.User, adm); err != nil {
				WriteProblem(w, r, err, prod)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveBearer authenticates a personal access token for one route: a route no token reaches is
// 403, a changing route with a read token is 403, an unknown or expired token is 401.
func resolveBearer(ctx context.Context, svc *auth.Service, adm *admin.Service, pattern, header string, ra *requestAuth, tokens *mw.Limiter) error {
	secret, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || svc == nil || !auth.IsAccessToken(secret) {
		return platform.ErrUnauthenticated
	}
	class := tokenPolicy(pattern)
	if class == tokenNever {
		return fmt.Errorf("access tokens can't use %s: %w", pattern, platform.ErrForbidden)
	}
	if publicRoutes[pattern] {
		return nil // GET /meta: nothing to authenticate
	}
	id, err := svc.ResolveToken(ctx, secret)
	if err != nil {
		return err
	}
	if class == tokenWrite && id.Token.Scope != auth.ScopeWrite {
		return fmt.Errorf("a read token can't change data (%s): %w", pattern, platform.ErrForbidden)
	}
	if tokens != nil {
		if err := tokens.Allow(id.Token.ID.String()); err != nil {
			return err
		}
	}
	ra.identity = id
	mw.FromContext(ctx).UserHash = platform.UserHash(id.User.ID)
	return gate(ctx, pattern, id.User, adm)
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
	ck := http.Cookie{Name: sessionCookieName(c.secure), Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode} //nolint:gosec // G124: see above
	ck.Secure = c.secure
	s := ck.String()
	return &s
}

// passwordChangeRoutes are all a user with a one-time password may use until they replace it.
var passwordChangeRoutes = map[string]bool{
	"GET /api/v1/me":           true,
	"POST /api/v1/me/password": true,
	"GET /api/v1/preferences":  true, // the app loads it while showing the password screen
}

// featureRoutes are the routes of a feature an administrator can switch off (ADR 0025). A route
// matches when its pattern's path starts with the prefix.
var featureRoutes = []struct {
	prefix string
	on     func(instance.EffectiveFeatures) bool
}{
	{"/api/v1/overview", func(f instance.EffectiveFeatures) bool { return f.Overview }},
	{"/api/v1/assistant/", func(f instance.EffectiveFeatures) bool { return f.Assistant }},
	{"/api/v1/friends", func(f instance.EffectiveFeatures) bool { return f.Friends }},
	{"/api/v1/shared-groups", func(f instance.EffectiveFeatures) bool { return f.SharedGroups }},
	{"/api/v1/groups/timeline", func(f instance.EffectiveFeatures) bool { return f.SharedGroups }},
}

// gate applies what the account and the instance allow on a protected route: administrators
// only on /admin (anyone else gets 404, so the routes don't announce themselves), nothing but the
// password change while a one-time password is pending, and no route of a switched-off feature.
func gate(ctx context.Context, pattern string, u auth.User, adm *admin.Service) error {
	_, path, _ := strings.Cut(pattern, " ")
	if strings.HasPrefix(path, "/api/v1/admin/") && (!u.IsAdmin || adm == nil) {
		return fmt.Errorf("%s: %w", path, platform.ErrNotFound)
	}
	if u.MustChangePassword && !passwordChangeRoutes[pattern] {
		return fmt.Errorf("choose a new password first: %w", platform.ErrPasswordChangeRequired)
	}
	if adm == nil {
		return nil
	}
	// An instance may require two-step sign-in of its administrators: one who hasn't set it up
	// keeps their own data and Settings → Security, but administers nothing until they do (ADR 0040).
	if strings.HasPrefix(path, "/api/v1/admin/") && !u.SecondFactor {
		s, _, err := adm.Settings(ctx)
		if err != nil {
			return err
		}
		if s.AdminsNeedSecondFactor() {
			return fmt.Errorf("set up two-step sign-in first (Settings → Security): %w", platform.ErrSecondFactorRequired)
		}
	}
	for _, fr := range featureRoutes {
		if strings.HasPrefix(path, fr.prefix) {
			f, err := adm.Features(ctx)
			if err != nil {
				return err
			}
			if !fr.on(f) {
				return fmt.Errorf("%s is switched off on this instance: %w", path, platform.ErrNotFound)
			}
		}
	}
	return nil
}
