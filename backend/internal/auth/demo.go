package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// DemoLifetime is how long a demo visitor's user and data live (PLAN 19.1).
const DemoLifetime = 24 * time.Hour

// StartDemo creates an ephemeral demo user and signs it in. Its address is under the reserved
// .invalid domain and its password random and never shown, so nobody can sign in to it again; the
// session is the only way in. The caller seeds its data (ledger.SeedDemo) and gates the route on
// demo mode and the per-address limit.
func (s *Service) StartDemo(ctx context.Context, c Client) (User, string, error) {
	pw := make([]byte, 24)
	if _, err := rand.Read(pw); err != nil {
		return User{}, "", err
	}
	phc, err := s.hash(ctx, base64.RawStdEncoding.EncodeToString(pw))
	if err != nil {
		return User{}, "", err
	}
	id := platform.NewID()
	var (
		out   User
		token string
	)
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		u, err := q.CreateUser(ctx, db.CreateUserParams{ID: id, Email: "demo-" + id.String() + "@demo.invalid",
			PasswordHash: phc, DisplayName: "Demo visitor", IsDemo: true})
		if err != nil {
			return err
		}
		out = userOf(u)
		token, err = s.newSession(ctx, q, u.ID, c)
		return err
	})
	return out, token, err
}

// DiscardDemo deletes a demo user (when seeding it failed). A normal account is never deleted.
func (s *Service) DiscardDemo(ctx context.Context, id uuid.UUID) error {
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.DeleteDemoUser(ctx, id)
		return err
	})
}

// CleanupDemoUsers deletes demo users older than DemoLifetime with all of their data.
func (s *Service) CleanupDemoUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteDemoUsersCreatedBefore(ctx, s.clock.Now().Add(-DemoLifetime))
		return err
	})
	return n, err
}
