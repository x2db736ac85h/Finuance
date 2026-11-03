package admin

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Usage is what an administrator sees of an account's data: counts, never rows.
type Usage struct {
	Accounts, Transactions, Imports, StoredBytes, Chats int64
	LastActivity                                        *time.Time
}

// usage counts one user's data under that user's own row-level-security context, in a read-only
// transaction, the way the cleanup jobs reach every user (ledger.CleanupAudit).
func (s *Service) usage(ctx context.Context, userID uuid.UUID) (Usage, error) {
	var u Usage
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.UserUsage(ctx, userID)
		u = Usage{Accounts: r.Accounts, Transactions: r.Transactions, Imports: r.Imports, StoredBytes: r.StoredBytes, Chats: r.Chats}
		if r.LastActivity.Unix() > 0 {
			at := r.LastActivity
			u.LastActivity = &at
		}
		return err
	})
	return u, err
}

// DayUsage is the instance's activity on one day.
type DayUsage struct {
	Day                platform.Date
	Imports, Questions int64
}

// InstanceUsage is the instance as a whole.
type InstanceUsage struct {
	Totals                                       Totals
	Accounts, Transactions, Imports, StoredBytes int64
	Days                                         []DayUsage // the last `days` days, oldest first
}

// InstanceUsage adds up every user's counts and the last `days` days of imports and assistant
// questions, one user at a time.
func (s *Service) InstanceUsage(ctx context.Context, days int) (InstanceUsage, error) {
	if days < 1 || days > 90 {
		days = 30
	}
	now := s.clock.Now().UTC()
	until := platform.DateOf(now, time.UTC)
	since := platform.DateOf(now.AddDate(0, 0, 1-days), time.UTC)
	out := InstanceUsage{Days: make([]DayUsage, days)}
	for i := range out.Days {
		out.Days[i].Day = platform.DateOf(now.AddDate(0, 0, i+1-days), time.UTC)
	}
	after := uuid.Nil
	for {
		var ids []uuid.UUID
		err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
			if after == uuid.Nil {
				t, err := q.AdminCountUsers(ctx, db.AdminCountUsersParams{Active7: now.AddDate(0, 0, -7), Active30: now.AddDate(0, 0, -30)})
				if err != nil {
					return err
				}
				out.Totals = Totals{Users: t.Total, Admins: t.Admins, Disabled: t.Disabled, Demo: t.Demo, Active7: t.Active7, Active30: t.Active30}
			}
			ids, err = q.ListUserIDsAfter(ctx, db.ListUserIDsAfterParams{After: after, PageSize: 500})
			return err
		})
		if err != nil {
			return InstanceUsage{}, err
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return InstanceUsage{}, err
			}
			err := s.st.WithUserReadOnly(ctx, id, func(q *db.Queries, _ pgx.Tx) error {
				r, err := q.UserUsage(ctx, id)
				if err != nil {
					return err
				}
				out.Accounts += r.Accounts
				out.Transactions += r.Transactions
				out.Imports += r.Imports
				out.StoredBytes += r.StoredBytes
				rows, err := q.UserActivityByDay(ctx, db.UserActivityByDayParams{UserID: id, Since: since, Until: until})
				for i, d := range rows {
					if i < len(out.Days) {
						out.Days[i].Imports += d.Imports
						out.Days[i].Questions += d.Questions
					}
				}
				return err
			})
			if err != nil {
				return InstanceUsage{}, err
			}
		}
		if len(ids) < 500 {
			return out, nil
		}
		after = ids[len(ids)-1]
	}
}
