// Package jobs holds River job arguments, workers and periodic jobs (ADR 0006). Job args carry ids,
// never user data. Noop proves the queue end to end; CleanupExpiredSessions runs hourly (Phase 5);
// CommitImport and RematerializeFingerprint are the import pipeline's (Phase 6); LegacyImport
// migrates a legacy backup (Phase 14); Reclassify applies rules and finds transfers (Phase 15).
package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// NoopArgs is a job that does nothing. River needs at least one worker to start, and the
// integration tests use it to prove that an inserted job is worked.
type NoopArgs struct {
	Note string `json:"note"`
}

// Kind identifies the job type in river_job.
func (NoopArgs) Kind() string { return "noop" }

// InsertOpts keeps noop jobs cheap.
func (NoopArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

// NoopWorker works NoopArgs.
type NoopWorker struct {
	river.WorkerDefaults[NoopArgs]
}

// Work logs and returns.
func (w *NoopWorker) Work(ctx context.Context, job *river.Job[NoopArgs]) error {
	observability.Logger(ctx).DebugContext(ctx, "noop job", "id", job.ID)
	return nil
}

// Deps are the services the workers need. The worker process builds them from the configuration;
// tests pass fakes.
type Deps struct {
	Sessions SessionCleaner
	Chats    ChatCleaner
	Ledger   Ledger
	Migrator Migrator
	Rules    Reclassifier
	Metrics  *ImportMetrics // nil: unregistered metrics
}

// Workers registers every worker. The worker process, the api's insert-only client and the tests
// share it, so an inserted job kind always has a worker.
func Workers(d Deps) *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &NoopWorker{})
	river.AddWorker(w, &CleanupExpiredSessionsWorker{Sessions: d.Sessions})
	river.AddWorker(w, &CleanupAssistantChatsWorker{Chats: d.Chats})
	m := d.Metrics
	if m == nil {
		m = NewImportMetrics(nil)
	}
	river.AddWorker(w, &CommitImportWorker{Ledger: d.Ledger, Metrics: m})
	river.AddWorker(w, &RematerializeFingerprintWorker{Ledger: d.Ledger})
	river.AddWorker(w, &LegacyImportWorker{Migrator: d.Migrator})
	river.AddWorker(w, &ReclassifyWorker{Rules: d.Rules})
	return w
}

// PeriodicJobs are scheduled by the elected leader among the worker processes.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return CleanupExpiredSessionsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return CleanupAssistantChatsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}),
	}
}
