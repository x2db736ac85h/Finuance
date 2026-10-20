// Package jobs holds River job arguments, workers and periodic jobs (ADR 0006). Job args carry ids,
// never user data. Real jobs start in Phase 6; Noop proves the queue end to end.
package jobs

import (
	"context"

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

// Workers registers every worker. The worker process and the tests share it.
func Workers() *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &NoopWorker{})
	return w
}
