package jobs

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Migrator is what the LegacyImport job needs from legacyimport.Migrator.
type Migrator interface {
	Run(ctx context.Context, userID, id uuid.UUID) error
	Fail(ctx context.Context, userID, id uuid.UUID, message string) error
}

// LegacyImportArgs runs a legacy migration (PLAN §4.10, task 14.1). Args carry ids only; the
// manifest and the files wait in legacy_imports.
type LegacyImportArgs struct {
	UserID uuid.UUID `json:"userId"`
	ID     uuid.UUID `json:"id"`
}

// Kind identifies the job type in river_job.
func (LegacyImportArgs) Kind() string { return "legacy_import" }

// LegacyImportAttempts is how often a run is tried before it is marked failed. A run is
// idempotent, so a retry continues where the last attempt stopped.
const LegacyImportAttempts = 3

// InsertOpts allows three attempts with River's default backoff.
func (LegacyImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: LegacyImportAttempts}
}

// LegacyImportWorker works LegacyImportArgs.
type LegacyImportWorker struct {
	river.WorkerDefaults[LegacyImportArgs]
	Migrator Migrator
}

const legacyFailedMessage = "The migration stopped with an error. Run it again; what was migrated is kept and nothing is created twice. If it keeps failing, check the server logs."

// Work runs the migration; after the last attempt the run is marked failed.
func (w *LegacyImportWorker) Work(ctx context.Context, job *river.Job[LegacyImportArgs]) error {
	a := job.Args
	log := observability.Logger(ctx)
	err := w.Migrator.Run(ctx, a.UserID, a.ID)
	switch {
	case err == nil:
		log.InfoContext(ctx, "legacy migration done", "run", a.ID)
		return nil
	case errors.Is(err, legacyimport.ErrNotFound):
		log.WarnContext(ctx, "legacy migration skipped", "run", a.ID, "error", err)
		return nil
	case job.Attempt >= job.MaxAttempts:
		log.ErrorContext(ctx, "legacy migration failed", "run", a.ID, "attempt", job.Attempt, "error", err)
		if ferr := w.Migrator.Fail(context.WithoutCancel(ctx), a.UserID, a.ID, legacyFailedMessage); ferr != nil {
			return errors.Join(err, ferr)
		}
		return err
	}
	return err
}
