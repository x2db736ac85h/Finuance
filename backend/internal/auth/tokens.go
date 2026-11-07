package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Personal access tokens (ADR 0036): for the fin CLI and scripts, sent as "Authorization: Bearer
// fin_…". Only a token's SHA-256 is stored; the token itself is shown once. A read token may use
// what reads; a write token may also change data; neither reaches the admin, sign-in, password,
// session or token routes (httpapi's route policy).

// Token scopes.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// Token limits.
const (
	TokenPrefix     = "fin_"
	MaxTokens       = 25  // live tokens per user
	MaxTokenDays    = 366 // a token lives at most a year (and a day for time zones)
	tokenSecretSize = 32
	prefixLen       = 12 // fin_ + 8 characters shown in the list
	tokenTouchEvery = time.Minute
	tokenKeepDays   = 30 // revoked or expired tokens stay listed this long
)

// AccessToken is one token as its owner sees it: never the token.
type AccessToken struct {
	ID         uuid.UUID
	Name       string
	Prefix     string
	Scope      string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// TokenAccess is how a request authenticated with a token is allowed in.
type TokenAccess struct {
	ID    uuid.UUID
	Scope string
}

func tokenOf(r *db.AccessToken) AccessToken {
	return AccessToken{ID: r.ID, Name: r.Name, Prefix: r.Prefix, Scope: r.Scope, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt}
}

// IsAccessToken reports whether a bearer credential looks like a personal access token.
func IsAccessToken(s string) bool { return strings.HasPrefix(s, TokenPrefix) }

func hashAccessToken(s string) [32]byte { return sha256.Sum256([]byte(s)) }

// CreateToken makes a token for the user and returns it with the secret, which is never stored or
// shown again. days is its lifetime (1 to MaxTokenDays).
func (s *Service) CreateToken(ctx context.Context, userID uuid.UUID, name, scope string, days int) (AccessToken, string, error) {
	name = strings.TrimSpace(name)
	ve := &platform.ValidationError{}
	if name == "" || utf8.RuneCountInString(name) > 60 || strings.ContainsRune(name, 0) {
		ve.Add("name", "must be 1 to 60 characters")
	}
	if scope != ScopeRead && scope != ScopeWrite {
		ve.Add("scope", "must be read or write")
	}
	if days < 1 || days > MaxTokenDays {
		ve.Add("expiresInDays", fmt.Sprintf("must be 1 to %d days", MaxTokenDays))
	}
	if err := ve.Err(); err != nil {
		return AccessToken{}, "", err
	}
	raw := make([]byte, tokenSecretSize)
	if _, err := rand.Read(raw); err != nil {
		return AccessToken{}, "", err
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := hashAccessToken(secret)
	now := s.clock.Now()
	var out AccessToken
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.CountLiveAccessTokens(ctx, db.CountLiveAccessTokensParams{UserID: userID, Now: now})
		if err != nil {
			return err
		}
		if n >= MaxTokens {
			return &instance.LimitError{Limit: "accessTokens", Value: MaxTokens}
		}
		r, err := q.InsertAccessToken(ctx, db.InsertAccessTokenParams{ID: platform.NewID(), UserID: userID, Name: name, TokenHash: hash[:],
			Prefix: secret[:prefixLen], Scope: scope, Now: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour)})
		out = tokenOf(&r)
		return err
	})
	if err != nil {
		return AccessToken{}, "", err
	}
	return out, secret, nil
}

// Tokens lists the user's tokens, newest first (revoked and expired ones for 30 days).
func (s *Service) Tokens(ctx context.Context, userID uuid.UUID) ([]AccessToken, error) {
	var out []AccessToken
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListAccessTokens(ctx, userID)
		for i := range rows {
			out = append(out, tokenOf(&rows[i]))
		}
		return err
	})
	return out, err
}

// RevokeToken ends one of the user's tokens.
func (s *Service) RevokeToken(ctx context.Context, userID, id uuid.UUID) (AccessToken, error) {
	var out AccessToken
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.RevokeAccessToken(ctx, db.RevokeAccessTokenParams{UserID: userID, ID: id, Now: s.clock.Now()})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("token %s: %w", id, platform.ErrNotFound)
		}
		out = tokenOf(&r)
		return err
	})
	return out, err
}

// ResolveToken authenticates a bearer token: live, not revoked, of an account that isn't disabled.
func (s *Service) ResolveToken(ctx context.Context, secret string) (Identity, error) {
	if !IsAccessToken(secret) || len(secret) > 100 {
		return Identity{}, platform.ErrUnauthenticated
	}
	hash := hashAccessToken(secret)
	now := s.clock.Now()
	var id Identity
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.ResolveAccessToken(ctx, db.ResolveAccessTokenParams{TokenHash: hash[:], Now: now})
		if err != nil {
			return err
		}
		limits, _ := instance.ParseLimits(r.Limits)
		id = Identity{User: User{ID: r.UserID, Email: r.Email, DisplayName: r.DisplayName, IsDemo: r.IsDemo, CreatedAt: r.UserCreatedAt,
			IsAdmin: r.IsAdmin, MustChangePassword: r.MustChangePassword, Limits: limits},
			Token: &TokenAccess{ID: r.TokenID, Scope: r.Scope}}
		if r.LastUsedAt == nil || now.Sub(*r.LastUsedAt) >= tokenTouchEvery {
			return q.TouchAccessToken(ctx, db.TouchAccessTokenParams{ID: r.TokenID, Now: now})
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, platform.ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, fmt.Errorf("resolve token: %w", err)
	}
	return id, nil
}

// CleanupAccessTokens deletes tokens revoked or expired more than 30 days ago.
func (s *Service) CleanupAccessTokens(ctx context.Context) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteOldAccessTokens(ctx, s.clock.Now().Add(-tokenKeepDays*24*time.Hour))
		return err
	})
	return n, err
}
