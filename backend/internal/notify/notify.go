// Package notify keeps in-app notifications (Phase 19, 19.8): short messages about what other
// people did that concern a user (a friend added or removed you, shared groups), and that the
// assistant's digest is ready (Phase 20). They are
// user-owned rows under forced RLS; another user's action writes one under the recipient's own
// context. The web app words them from the kind and the data, which hold ids and display names,
// never e-mail addresses. There is no e-mail delivery. Notifications older than 90 days are
// deleted by a daily job.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Kinds. Keep them equal to the API's NotificationKind (TestKindsMatchTheAPI).
const (
	FriendAdded   = "friend.added"
	FriendRemoved = "friend.removed"
	// Shared groups (ADR 0028): data holds the group's id and name and the other member's name.
	SharedInvited      = "shared.invited"
	SharedJoined       = "shared.joined"
	SharedLeft         = "shared.left"
	SharedExpenseAdded = "shared.expense_added"
	SharedSettled      = "shared.settled"
	// AssistantDigest: a digest is ready (ADR 0033); data holds the chat's id, the schedule, the
	// number of proposals waiting, and failed when it couldn't be written.
	AssistantDigest = "assistant.digest"
)

// Kinds lists every kind above.
var Kinds = []string{FriendAdded, FriendRemoved, SharedInvited, SharedJoined, SharedLeft, SharedExpenseAdded, SharedSettled,
	AssistantDigest}

// Retention is how long a notification is kept.
const Retention = 90 * 24 * time.Hour

// MaxList caps one listing.
const MaxList = 100

// Notification is one message.
type Notification struct {
	ID        uuid.UUID
	Kind      string
	Data      map[string]string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Service reads and writes notifications.
type Service struct {
	st    *store.Store
	clock platform.Clock
}

// New returns the service.
func New(st *store.Store, clock platform.Clock) *Service { return &Service{st: st, clock: clock} }

// Send writes a notification for userID, under that user's context.
func (s *Service) Send(ctx context.Context, userID uuid.UUID, kind string, data map[string]string) error {
	doc, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		return q.InsertNotification(ctx, db.InsertNotificationParams{ID: platform.NewID(), UserID: userID, Kind: kind, Data: doc, Now: s.clock.Now()})
	})
}

// List returns the newest notifications (at most limit, capped at MaxList) and the unread count.
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Notification, int64, error) {
	if limit <= 0 || limit > MaxList {
		limit = MaxList
	}
	var (
		out    []Notification
		unread int64
	)
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListNotifications(ctx, db.ListNotificationsParams{UserID: userID, PageSize: int32(limit)}) //nolint:gosec // capped above
		if err != nil {
			return err
		}
		out = make([]Notification, len(rows))
		for i, r := range rows {
			out[i] = Notification{ID: r.ID, Kind: r.Kind, CreatedAt: r.CreatedAt, ReadAt: r.ReadAt}
			if err := json.Unmarshal(r.Data, &out[i].Data); err != nil {
				return fmt.Errorf("notification %s: %w", r.ID, err)
			}
		}
		unread, err = q.CountUnreadNotifications(ctx, userID)
		return err
	})
	return out, unread, err
}

// MarkRead marks ids read, or every unread notification when ids is nil. Ids of other users are
// ignored (RLS).
func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (int64, error) {
	var n int64
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		now := s.clock.Now()
		n, err = q.MarkNotificationsRead(ctx, db.MarkNotificationsReadParams{UserID: userID, Ids: ids, Now: &now})
		return err
	})
	return n, err
}

// Cleanup deletes notifications older than Retention, user by user under each user's context
// (the cleanup jobs' pattern).
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	var total int64
	before := s.clock.Now().Add(-Retention)
	after := uuid.Nil
	for {
		var ids []uuid.UUID
		if err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
			ids, err = q.ListUserIDsAfter(ctx, db.ListUserIDsAfterParams{After: after, PageSize: 500})
			return err
		}); err != nil {
			return total, err
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			if err := s.st.WithUser(ctx, id, func(q *db.Queries, _ pgx.Tx) error {
				n, err := q.DeleteOldNotifications(ctx, db.DeleteOldNotificationsParams{UserID: id, Before: before})
				total += n
				return err
			}); err != nil {
				return total, err
			}
		}
		if len(ids) < 500 {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}
