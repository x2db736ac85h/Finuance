package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Limit names, as in instance.Effective's JSON.
const (
	LimitUpload        = "maxUploadBytes"
	LimitImportsPerDay = "importsPerDay"
	LimitAccounts      = "maxAccounts"
	LimitGroups        = "maxGroups"
)

// CheckUpload refuses a file larger than the user's upload limit.
func (s *Service) CheckUpload(ctx context.Context, u auth.User, size int64) error {
	l, err := s.Limits(ctx, u)
	if err != nil {
		return err
	}
	if l.MaxUploadBytes > 0 && size > l.MaxUploadBytes {
		return &instance.LimitError{Limit: LimitUpload, Value: l.MaxUploadBytes}
	}
	return nil
}

// CheckCount refuses one more import today, account or group when the user is at the limit. The
// count runs under the user's own context and returns a number.
func (s *Service) CheckCount(ctx context.Context, u auth.User, limit string) error {
	l, err := s.Limits(ctx, u)
	if err != nil {
		return err
	}
	max := map[string]int64{LimitImportsPerDay: l.ImportsPerDay, LimitAccounts: l.MaxAccounts, LimitGroups: l.MaxGroups}[limit]
	if max == 0 {
		return nil
	}
	var n int64
	err = s.st.WithUserReadOnly(ctx, u.ID, func(q *db.Queries, _ pgx.Tx) (err error) {
		switch limit {
		case LimitImportsPerDay:
			n, err = q.CountImportsSince(ctx, db.CountImportsSinceParams{UserID: u.ID, Since: s.clock.Now().Add(-24 * time.Hour)})
		case LimitAccounts:
			n, err = q.CountAccounts(ctx, u.ID)
		case LimitGroups:
			n, err = q.CountGroups(ctx, u.ID)
		}
		return err
	})
	if err != nil {
		return err
	}
	if n >= max {
		return &instance.LimitError{Limit: limit, Value: max}
	}
	return nil
}
