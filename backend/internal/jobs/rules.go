package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
)

// Reclassifier is what the reclassify job needs from rules.Service.
type Reclassifier interface {
	Reclassify(ctx context.Context, userID uuid.UUID) (rules.Result, error)
}

// ReclassifyArgs recomputes a user's rule categories and transfers from scratch (PLAN tasks 15.2,
// 15.3: the plan's ApplyRules and DetectTransfers, run together because rules can mark transfers).
// It is enqueued after every change that can alter the result: a commit, a delete, a duplicate
// decision, a rule or category change, a legacy migration. Runs are idempotent and take every
// account's writer lock, so several queued runs simply repeat the same result.
type ReclassifyArgs struct {
	UserID uuid.UUID `json:"userId"`
}

// Kind identifies the job type in river_job.
func (ReclassifyArgs) Kind() string { return "reclassify" }

// InsertOpts allows a few retries; the job is idempotent.
func (ReclassifyArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 5} }

// ReclassifyWorker works ReclassifyArgs.
type ReclassifyWorker struct {
	river.WorkerDefaults[ReclassifyArgs]
	Rules Reclassifier
}

// Work reclassifies the user's transactions.
func (w *ReclassifyWorker) Work(ctx context.Context, job *river.Job[ReclassifyArgs]) error {
	res, err := w.Rules.Reclassify(ctx, job.Args.UserID)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "reclassified", "categorised", res.Categorised,
		"rule_transfers", res.RuleTransfers, "pairs", res.Pairs)
	return nil
}

// enqueueReclassify inserts a reclassification from inside another job (the worker's River client
// is in ctx); without one (a worker called directly in a test) it does nothing.
func enqueueReclassify(ctx context.Context, userID uuid.UUID) error {
	c, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return nil //nolint:nilerr // no client: called outside River
	}
	_, err = c.Insert(ctx, ReclassifyArgs{UserID: userID}, nil)
	return err
}
