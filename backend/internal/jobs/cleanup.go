package jobs

import (
	"context"
	"errors"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Cleaner deletes what has expired and says how many rows went (friends.Service,
// notify.Service).
type Cleaner interface {
	Cleanup(ctx context.Context) (int64, error)
}

// CleanupFriendCodesArgs deletes expired friend codes (Phase 19, hourly). An expired code is
// already refused; the job only keeps the table small.
type CleanupFriendCodesArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupFriendCodesArgs) Kind() string { return "cleanup_friend_codes" }

// InsertOpts allows a few retries; the next hour repeats a missed run.
func (CleanupFriendCodesArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

// CleanupFriendCodesWorker works CleanupFriendCodesArgs.
type CleanupFriendCodesWorker struct {
	river.WorkerDefaults[CleanupFriendCodesArgs]
	Friends Cleaner
}

// Work deletes the expired codes.
func (w *CleanupFriendCodesWorker) Work(ctx context.Context, _ *river.Job[CleanupFriendCodesArgs]) error {
	return clean(ctx, "cleanup_friend_codes", "expired friend codes deleted", w.Friends)
}

// CleanupNotificationsArgs deletes notifications older than notify.Retention (daily).
type CleanupNotificationsArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupNotificationsArgs) Kind() string { return "cleanup_notifications" }

// InsertOpts allows a few retries; the next day repeats a missed run.
func (CleanupNotificationsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3}
}

// CleanupNotificationsWorker works CleanupNotificationsArgs.
type CleanupNotificationsWorker struct {
	river.WorkerDefaults[CleanupNotificationsArgs]
	Notifications Cleaner
}

// Work deletes the old notifications.
func (w *CleanupNotificationsWorker) Work(ctx context.Context, _ *river.Job[CleanupNotificationsArgs]) error {
	return clean(ctx, "cleanup_notifications", "old notifications deleted", w.Notifications)
}

func clean(ctx context.Context, kind, msg string, c Cleaner) error {
	if c == nil {
		return errors.New(kind + ": no service configured")
	}
	n, err := c.Cleanup(ctx)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, msg, "count", n)
	return nil
}
