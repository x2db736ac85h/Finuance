package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// Personal access tokens (ADR 0036): which routes a token may use, and the Settings → Access
// tokens endpoints. A route's class comes from rules, and TestTokenRoutePolicy lists every route's
// class in a golden file, so a new route is reviewed before tokens can use it.

type tokenClass string

// Route classes for tokens.
const (
	tokenNever tokenClass = "never" // sign-in, password, sessions, tokens, account deletion, admin
	tokenRead  tokenClass = "read"  // reads: GET and the read-only POSTs below
	tokenWrite tokenClass = "write" // changes data: a write token only
)

// tokenNeverPrefixes are paths no token reaches, whatever its scope.
var tokenNeverPrefixes = []string{"/api/v1/auth/", "/api/v1/admin/", "/api/v1/me/password", "/api/v1/me/sessions", "/api/v1/me/tokens"}

// tokenReadPosts are the POST routes that only read (they take a query in the body).
var tokenReadPosts = map[string]bool{
	"POST /api/v1/transactions/query":    true,
	"POST /api/v1/transactions/distinct": true,
	"POST /api/v1/transactions/ids":      true,
	"POST /api/v1/transactions/export":   true,
	"POST /api/v1/rules/preview":         true,
	"POST /api/v1/insights/summary":      true,
	"POST /api/v1/insights/categories":   true,
	"POST /api/v1/insights/cashflow":     true,
	"POST /api/v1/insights/pace":         true,
	"POST /api/v1/insights/payees":       true,
	"POST /api/v1/insights/daily":        true,
	"POST /api/v1/overview":              true,
}

// tokenPolicy is a route's class for tokens (pattern as the router registers it).
func tokenPolicy(pattern string) tokenClass {
	method, path, _ := strings.Cut(pattern, " ")
	for _, p := range tokenNeverPrefixes {
		if strings.HasPrefix(path, p) {
			return tokenNever
		}
	}
	switch {
	case pattern == "DELETE /api/v1/me":
		return tokenNever
	case method == "GET", tokenReadPosts[pattern]:
		return tokenRead
	}
	return tokenWrite
}

// TokenRate is how many requests one token may make per minute.
var TokenRate = mw.Policy{Events: 600, Per: time.Minute}

func accessTokenDTO(t *auth.AccessToken) gen.AccessToken {
	return gen.AccessToken{Id: t.ID, Name: t.Name, Prefix: t.Prefix, Scope: gen.AccessTokenScope(t.Scope), CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

// ListAccessTokens lists the user's tokens (never a token itself).
func (a *api) ListAccessTokens(ctx context.Context, _ gen.ListAccessTokensRequestObject) (gen.ListAccessTokensResponseObject, error) {
	list, err := a.auth.Tokens(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListAccessTokens200JSONResponse, 0, len(list))
	for i := range list {
		out = append(out, accessTokenDTO(&list[i]))
	}
	return out, nil
}

// CreateAccessToken makes a token and returns it once.
func (a *api) CreateAccessToken(ctx context.Context, req gen.CreateAccessTokenRequestObject) (gen.CreateAccessTokenResponseObject, error) {
	b := req.Body
	t, secret, err := a.auth.CreateToken(ctx, identity(ctx).User.ID, b.Name, string(b.Scope), b.ExpiresInDays)
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditTokenCreated, t.ID.String(), map[string]any{"name": t.Name, "scope": t.Scope, "prefix": t.Prefix,
		"expiresAt": t.ExpiresAt.Format(time.RFC3339)})
	return gen.CreateAccessToken201JSONResponse{Token: accessTokenDTO(&t), Secret: secret}, nil
}

// RevokeAccessToken ends one of the user's tokens.
func (a *api) RevokeAccessToken(ctx context.Context, req gen.RevokeAccessTokenRequestObject) (gen.RevokeAccessTokenResponseObject, error) {
	t, err := a.auth.RevokeToken(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditTokenRevoked, t.ID.String(), map[string]any{"name": t.Name, "prefix": t.Prefix})
	return gen.RevokeAccessToken204Response{}, nil
}
