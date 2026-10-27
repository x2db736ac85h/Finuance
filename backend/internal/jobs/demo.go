package jobs

import (
	"context"
	"errors"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// DemoCleaner deletes expired demo users (auth.Service).
type DemoCleaner interface {
	CleanupDemoUsers(ctx context.Context) (int64, error)
}

// CleanupDemoUsersArgs deletes demo visitors older than auth.DemoLifetime with all of their data
// (PLAN 19.1). It runs hourly (PeriodicJobs) and once when a worker becomes leader, on every
// instance: outside demo mode there are no demo users, or only ones left from an earlier demo.
type CleanupDemoUsersArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupDemoUsersArgs) Kind() string { return "cleanup_demo_users" }

// InsertOpts allows a few retries; a missed run is harmless because the next hour repeats it.
func (CleanupDemoUsersArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

// CleanupDemoUsersWorker works CleanupDemoUsersArgs.
type CleanupDemoUsersWorker struct {
	river.WorkerDefaults[CleanupDemoUsersArgs]
	Demo DemoCleaner
}

// Work deletes the expired demo users and logs how many.
func (w *CleanupDemoUsersWorker) Work(ctx context.Context, _ *river.Job[CleanupDemoUsersArgs]) error {
	if w.Demo == nil {
		return errors.New("cleanup_demo_users: no auth service configured")
	}
	n, err := w.Demo.CleanupDemoUsers(ctx)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "expired demo users deleted", "count", n)
	return nil
}
