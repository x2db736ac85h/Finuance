package jobs

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

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
	if w.Rules == nil {
		return errors.New("reclassify: no rules service")
	}
	// A reclassification recomputes everything, so when a newer one already waits for the same
	// user, that one covers this job's change as well and this job steps aside. A burst of changes
	// (or a script) then costs one full run, not one per change, and the other users' jobs are
	// not queued behind them (IMP-5). River's own uniqueness can't do this: it would drop a job
	// asked for while another runs, and that change would never be reclassified.
	if newerReclassifyWaits(ctx, job.ID, job.Args.UserID) {
		observability.Logger(ctx).DebugContext(ctx, "reclassify skipped: a newer one waits")
		return nil
	}
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

// newerReclassifyWaits reports whether a reclassification of the same user that was queued after
// this job is ready to run. Outside River (a worker called directly in a test) and on any error
// the answer is no, and the job runs.
func newerReclassifyWaits(ctx context.Context, jobID int64, userID uuid.UUID) bool {
	c, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return false
	}
	res, err := c.JobList(ctx, river.NewJobListParams().Kinds(ReclassifyArgs{}.Kind()).
		States(rivertype.JobStateAvailable, rivertype.JobStatePending).
		Where("args->>'userId' = @user AND id > @id", river.NamedArgs{"user": userID.String(), "id": jobID}).First(1))
	return err == nil && len(res.Jobs) > 0
}
