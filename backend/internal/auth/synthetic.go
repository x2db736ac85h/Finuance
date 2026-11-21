package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Synthetic users (ADR 0041, PLAN 24.7). Who is synthetic is a fact about the user, kept in
// users.kind and decided by the server: a request header could be set by anyone, either to hide
// real failures from the numbers or to pass load off as real use. The traffic simulator's users
// are marked with MarkSynthetic (by `finuance admin synthetic-users`, Phase 25), and only on an
// instance started with FINUANCE_SIMULATION; elsewhere such a user can't be made and can't sign in.

// The values of users.kind.
const (
	KindPerson    = "person"
	KindSynthetic = "synthetic"
)

// MarkSynthetic turns the user into a synthetic one. It is refused outside a simulation instance
// and for an administrator, and it can't be undone: a synthetic user stays one until it is deleted.
func (s *Service) MarkSynthetic(ctx context.Context, userID uuid.UUID) error {
	if !s.cfg.Simulation {
		return fmt.Errorf("synthetic users need FINUANCE_SIMULATION=true: %w", platform.ErrForbidden)
	}
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.SetUserSynthetic(ctx, userID)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("no such user, or an administrator (a synthetic user is never one): %w", platform.ErrConflict)
		}
		return nil
	})
}
