package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type countCleaner struct {
	n     int64
	err   error
	calls int
}

func (c *countCleaner) Cleanup(context.Context) (int64, error) {
	c.calls++
	return c.n, c.err
}

func TestFriendAndNotificationCleanup(t *testing.T) {
	ctx := context.Background()
	codes := &river.Job[CleanupFriendCodesArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	notes := &river.Job[CleanupNotificationsArgs]{JobRow: &rivertype.JobRow{ID: 2}}
	f, n := &countCleaner{n: 2}, &countCleaner{n: 5}
	if err := (&CleanupFriendCodesWorker{Friends: f}).Work(ctx, codes); err != nil || f.calls != 1 {
		t.Errorf("friend codes: %v, calls %d", err, f.calls)
	}
	if err := (&CleanupNotificationsWorker{Notifications: n}).Work(ctx, notes); err != nil || n.calls != 1 {
		t.Errorf("notifications: %v, calls %d", err, n.calls)
	}
	boom := errors.New("db down")
	if err := (&CleanupFriendCodesWorker{Friends: &countCleaner{err: boom}}).Work(ctx, codes); !errors.Is(err, boom) {
		t.Errorf("error not returned: %v", err)
	}
	if err := (&CleanupNotificationsWorker{}).Work(ctx, notes); err == nil {
		t.Error("missing dependency not reported")
	}
	if (CleanupFriendCodesArgs{}).Kind() != "cleanup_friend_codes" || (CleanupNotificationsArgs{}).Kind() != "cleanup_notifications" ||
		(CleanupFriendCodesArgs{}).InsertOpts().MaxAttempts != 3 || (CleanupNotificationsArgs{}).InsertOpts().MaxAttempts != 3 {
		t.Error("kinds or attempts changed")
	}
}
