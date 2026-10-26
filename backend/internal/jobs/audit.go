package jobs

import (
	"context"
	"errors"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// AuditCleaner deletes activity-log events past their retention (ledger.Ledger).
type AuditCleaner interface {
	CleanupAudit(ctx context.Context) (int64, error)
}

// CleanupAuditLogArgs deletes activity-log events older than ledger.AuditRetention (PLAN 17.6).
// It runs daily (PeriodicJobs) and once when a worker becomes leader.
type CleanupAuditLogArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupAuditLogArgs) Kind() string { return "cleanup_audit_log" }

// InsertOpts allows a few retries; a missed run is harmless because the next day repeats it.
func (CleanupAuditLogArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

// CleanupAuditLogWorker works CleanupAuditLogArgs.
type CleanupAuditLogWorker struct {
	river.WorkerDefaults[CleanupAuditLogArgs]
	Audit AuditCleaner
}

// Work deletes the expired events and logs how many.
func (w *CleanupAuditLogWorker) Work(ctx context.Context, _ *river.Job[CleanupAuditLogArgs]) error {
	if w.Audit == nil {
		return errors.New("cleanup_audit_log: no ledger configured")
	}
	n, err := w.Audit.CleanupAudit(ctx)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "expired audit events deleted", "count", n)
	return nil
}
