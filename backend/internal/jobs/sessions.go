package jobs

import (
	"context"
	"errors"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// SessionCleaner deletes expired sessions (auth.Service).
type SessionCleaner interface {
	CleanupExpiredSessions(ctx context.Context) (int64, error)
}

// CleanupExpiredSessionsArgs deletes sessions past their absolute or idle expiry. It runs hourly
// (PeriodicJobs) and once when a worker becomes leader. Expired sessions are already rejected when
// resolved; the job only keeps the table small.
type CleanupExpiredSessionsArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupExpiredSessionsArgs) Kind() string { return "cleanup_expired_sessions" }

// InsertOpts allows a few retries; a missed run is harmless because the next hour repeats it.
func (CleanupExpiredSessionsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3}
}

// CleanupExpiredSessionsWorker works CleanupExpiredSessionsArgs.
type CleanupExpiredSessionsWorker struct {
	river.WorkerDefaults[CleanupExpiredSessionsArgs]
	Sessions SessionCleaner
}

// Work deletes the expired sessions and logs how many.
func (w *CleanupExpiredSessionsWorker) Work(ctx context.Context, _ *river.Job[CleanupExpiredSessionsArgs]) error {
	if w.Sessions == nil {
		return errors.New("cleanup_expired_sessions: no session service configured")
	}
	n, err := w.Sessions.CleanupExpiredSessions(ctx)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "expired sessions deleted", "count", n)
	return nil
}
