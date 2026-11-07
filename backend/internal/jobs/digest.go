package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// DigestQueue runs the assistant's digests, one at a time per worker process (ADR 0033).
const DigestQueue = "digest"

// DigestTimeout is how long one digest may take: a few model requests on a small GPU.
const DigestTimeout = 30 * time.Minute

// DigestWriter schedules and writes digests (assistant.Digests).
type DigestWriter interface {
	Schedule(ctx context.Context, enqueue func(ctx context.Context, tx pgx.Tx, userID uuid.UUID, due time.Time) error) (int, error)
	Write(ctx context.Context, userID uuid.UUID, manual bool) (assistant.DigestOutcome, error)
}

// AssistantDigestScanArgs finds the users whose digest is due and queues one for each, every 15
// minutes and when a worker becomes leader.
type AssistantDigestScanArgs struct{}

// Kind identifies the job type in river_job.
func (AssistantDigestScanArgs) Kind() string { return "assistant_digest_scan" }

// InsertOpts allows a few retries; the next scan repeats a missed one.
func (AssistantDigestScanArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

// AssistantDigestScanWorker works AssistantDigestScanArgs.
type AssistantDigestScanWorker struct {
	river.WorkerDefaults[AssistantDigestScanArgs]
	Digests DigestWriter
}

// Work queues the due digests in the transaction that moves each user's due time.
func (w *AssistantDigestScanWorker) Work(ctx context.Context, _ *river.Job[AssistantDigestScanArgs]) error {
	if w.Digests == nil {
		return nil // the assistant isn't configured on this worker
	}
	client := river.ClientFromContext[pgx.Tx](ctx)
	n, err := w.Digests.Schedule(ctx, func(ctx context.Context, tx pgx.Tx, userID uuid.UUID, due time.Time) error {
		_, err := client.InsertTx(ctx, tx, AssistantDigestArgs{UserID: userID, Due: due}, nil)
		return err
	})
	if n > 0 {
		observability.Logger(ctx).InfoContext(ctx, "assistant digests queued", "count", n)
	}
	return err
}

// AssistantDigestArgs writes one user's digest: a scheduled one (Due) or one asked for now
// (Manual). Ids only, never user data.
type AssistantDigestArgs struct {
	UserID uuid.UUID `json:"user_id"`
	Due    time.Time `json:"due,omitzero"`
	Manual bool      `json:"manual,omitempty"`
}

// Kind identifies the job type in river_job.
func (AssistantDigestArgs) Kind() string { return "assistant_digest" }

// InsertOpts puts digests on their queue without retries (a digest that fails says so in its
// notification), at most one waiting per user and due time (or per user for one asked for now).
func (AssistantDigestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: DigestQueue, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true,
		ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled}}}
}

// AssistantDigestWorker works AssistantDigestArgs.
type AssistantDigestWorker struct {
	river.WorkerDefaults[AssistantDigestArgs]
	Digests DigestWriter
}

// Timeout gives a digest the time a few model requests take.
func (w *AssistantDigestWorker) Timeout(*river.Job[AssistantDigestArgs]) time.Duration {
	return DigestTimeout
}

// Work writes the digest and logs what happened (never what was written).
func (w *AssistantDigestWorker) Work(ctx context.Context, job *river.Job[AssistantDigestArgs]) error {
	if w.Digests == nil {
		return errors.New("assistant_digest: the assistant is not configured on this worker")
	}
	out, err := w.Digests.Write(ctx, job.Args.UserID, job.Args.Manual)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "assistant digest", "manual", job.Args.Manual, "skipped", out.Skipped,
		"failed", out.Failed, "proposals", out.Proposals)
	return nil
}
