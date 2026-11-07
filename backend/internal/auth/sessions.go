package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Session tokens (PLAN §8.1): 32 random bytes, sent to the browser base64url-encoded in the
// finuance_session cookie. The database stores only SHA-256(token), so a database leak can't be
// replayed as a login. The public session id (for the "your sessions" list) is the first 8 bytes of
// that hash in hex: enough to address one session, useless as a credential.
const (
	tokenBytes = 32
	// touchEvery throttles last_seen_at writes: a session used every few seconds would otherwise
	// write on every request.
	touchEvery = 5 * time.Minute
)

// Client describes where a request came from; it is stored with the session for the sessions list.
type Client struct {
	UserAgent string
	IP        netip.Addr // zero if unknown
}

// Identity is the authenticated user of a request.
type Identity struct {
	User      User
	TokenHash [32]byte // the current session; needed to keep it when revoking the others
	// Token is set when the request came with a personal access token instead of a session.
	Token *TokenAccess
}

// SessionInfo is one entry of the user's session list.
type SessionInfo struct {
	ID         string // hex of the first 8 bytes of the token hash
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IP         string
	Current    bool
}

// hashToken returns SHA-256 of the raw token bytes, or false if token isn't a well-formed cookie
// value (so garbage never reaches the database).
func hashToken(token string) ([32]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return [32]byte{}, false
	}
	return sha256.Sum256(raw), true
}

func sessionID(hash []byte) string { return hex.EncodeToString(hash[:8]) }

// newSession creates a session for userID inside an open transaction and returns the cookie value.
func (s *Service) newSession(ctx context.Context, q *db.Queries, userID uuid.UUID, c Client) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	now := s.clock.Now()
	var ip *netip.Addr
	if c.IP.IsValid() {
		ip = &c.IP
	}
	err := q.CreateSession(ctx, db.CreateSessionParams{TokenHash: hash[:], UserID: userID, Now: now,
		ExpiresAt: now.Add(s.cfg.SessionTTL), UserAgent: truncate(c.UserAgent, 300), Ip: ip})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Resolve turns a cookie value into the signed-in user. It enforces both expiries (absolute:
// FINUANCE_SESSION_TTL from login; idle: FINUANCE_SESSION_IDLE_TTL since the last request) and
// records activity at most every five minutes. Any failure is platform.ErrUnauthenticated.
func (s *Service) Resolve(ctx context.Context, token string) (Identity, error) {
	hash, ok := hashToken(token)
	if !ok {
		return Identity{}, platform.ErrUnauthenticated
	}
	now := s.clock.Now()
	var id Identity
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		row, err := q.GetSession(ctx, db.GetSessionParams{TokenHash: hash[:], Now: now, IdleCutoff: now.Add(-s.cfg.SessionIdleTTL)})
		if err != nil {
			return err
		}
		limits, _ := instance.ParseLimits(row.Limits)
		id = Identity{TokenHash: hash, User: User{ID: row.UserID, Email: row.Email, DisplayName: row.DisplayName,
			IsDemo: row.IsDemo, CreatedAt: row.UserCreatedAt, IsAdmin: row.IsAdmin,
			MustChangePassword: row.MustChangePassword, Limits: limits}}
		if now.Sub(row.LastSeenAt) >= touchEvery {
			return q.TouchSession(ctx, db.TouchSessionParams{TokenHash: hash[:], Now: now})
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, platform.ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, fmt.Errorf("resolve session: %w", err)
	}
	return id, nil
}

// Logout deletes the session behind token. An unknown or malformed token is not an error: the
// cookie is cleared either way.
func (s *Service) Logout(ctx context.Context, token string) error {
	hash, ok := hashToken(token)
	if !ok {
		return nil
	}
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error { return q.DeleteSession(ctx, hash[:]) })
}

// Sessions lists the user's live sessions, most recently used first, marking the current one.
func (s *Service) Sessions(ctx context.Context, id Identity) ([]SessionInfo, error) {
	now := s.clock.Now()
	var out []SessionInfo
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{UserID: id.User.ID, Now: now, IdleCutoff: now.Add(-s.cfg.SessionIdleTTL)})
		for _, r := range rows {
			info := SessionInfo{ID: sessionID(r.TokenHash), CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
				ExpiresAt: r.ExpiresAt, UserAgent: r.UserAgent, Current: bytes.Equal(r.TokenHash, id.TokenHash[:])}
			if r.Ip != nil {
				info.IP = r.Ip.String()
			}
			out = append(out, info)
		}
		return err
	})
	return out, err
}

// RevokeSession deletes one of the user's sessions by its public id. Unknown ids, and ids of other
// users' sessions, are platform.ErrNotFound.
func (s *Service) RevokeSession(ctx context.Context, id Identity, sessionHexID string) error {
	prefix, err := hex.DecodeString(sessionHexID)
	if err != nil || len(prefix) != 8 {
		return fmt.Errorf("session %q: %w", sessionHexID, platform.ErrNotFound)
	}
	var n int64
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteUserSessionByPrefix(ctx, db.DeleteUserSessionByPrefixParams{UserID: id.User.ID, Prefix: prefix})
		return err
	})
	if err == nil && n == 0 {
		err = fmt.Errorf("session %q: %w", sessionHexID, platform.ErrNotFound)
	}
	return err
}

// RevokeOtherSessions logs the user out everywhere except the current session.
func (s *Service) RevokeOtherSessions(ctx context.Context, id Identity) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: id.User.ID, KeepTokenHash: id.TokenHash[:]})
		return err
	})
	return n, err
}

// CleanupExpiredSessions deletes sessions past either expiry (the hourly job).
func (s *Service) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	now := s.clock.Now()
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{Now: now, IdleCutoff: now.Add(-s.cfg.SessionIdleTTL)})
		if err != nil {
			return err
		}
		_, err = q.DeleteOldAccessTokens(ctx, now.Add(-tokenKeepDays*24*time.Hour))
		return err
	})
	return n, err
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
