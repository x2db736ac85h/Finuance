package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Ledger is what the import jobs need from ledger.Ledger.
type Ledger interface {
	CommitImport(ctx context.Context, userID, importID uuid.UUID) (ledger.CommitResult, error)
	RememberMapping(ctx context.Context, userID, importID uuid.UUID) error
	FailImport(ctx context.Context, userID, importID uuid.UUID, message string) error
	Rematerialize(ctx context.Context, userID, accountID uuid.UUID, fingerprint []byte) error
}

// ImportMetrics are the import pipeline's Prometheus metrics (PLAN task 6.6).
type ImportMetrics struct {
	Imports  *prometheus.CounterVec // finuance_imports_total{result}
	Rows     *prometheus.CounterVec // finuance_import_rows_total{kind}
	Duration prometheus.Histogram   // finuance_import_commit_duration_seconds
}

// NewImportMetrics creates the metrics and registers them on reg (nil: unregistered, for tests).
func NewImportMetrics(reg prometheus.Registerer) *ImportMetrics {
	m := &ImportMetrics{
		Imports: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_imports_total",
			Help: "Import commits by result (committed, failed, retried)."}, []string{"result"}),
		Rows: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "finuance_import_rows_total",
			Help: "Committed import rows by kind (new, duplicate, error)."}, []string{"kind"}),
		Duration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "finuance_import_commit_duration_seconds",
			Help: "Duration of the CommitImport job.", Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120}}),
	}
	if reg != nil {
		reg.MustRegister(m.Imports, m.Rows, m.Duration)
	}
	return m
}

// CommitImportArgs commits a staged import (PLAN §4.4 commit algorithm). Args carry ids only.
type CommitImportArgs struct {
	ImportID        uuid.UUID `json:"importId"`
	UserID          uuid.UUID `json:"userId"`
	RememberMapping bool      `json:"rememberMapping"`
}

// Kind identifies the job type in river_job.
func (CommitImportArgs) Kind() string { return "commit_import" }

// CommitAttempts is how often a commit is tried before the import is marked failed.
const CommitAttempts = 3

// InsertOpts allows three attempts with River's default backoff.
func (CommitImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: CommitAttempts}
}

// CommitImportWorker works CommitImportArgs.
type CommitImportWorker struct {
	river.WorkerDefaults[CommitImportArgs]
	Ledger  Ledger
	Metrics *ImportMetrics
}

// failedMessage is what the user sees for a failed commit. Internal error text stays in the log.
const failedMessage = "The import could not be committed. Try again; if it keeps failing, check the server logs."

// Work commits the import. The whole commit is one database transaction, so a failed attempt
// leaves nothing behind and the retry starts clean. After the last attempt the import is marked
// failed with a sanitised message.
func (w *CommitImportWorker) Work(ctx context.Context, job *river.Job[CommitImportArgs]) error {
	start := time.Now()
	a := job.Args
	res, err := w.Ledger.CommitImport(ctx, a.UserID, a.ImportID)
	if err == nil && a.RememberMapping {
		err = w.Ledger.RememberMapping(ctx, a.UserID, a.ImportID)
	}
	w.Metrics.Duration.Observe(time.Since(start).Seconds())
	log := observability.Logger(ctx)
	if err != nil {
		if errors.Is(err, ledger.ErrState) || errors.Is(err, ledger.ErrNotFound) {
			// Deleted, or no longer committing: nothing left to do, and retrying won't change it.
			log.WarnContext(ctx, "commit_import skipped", "import", a.ImportID, "error", err)
			return nil
		}
		if job.Attempt >= job.MaxAttempts {
			w.Metrics.Imports.WithLabelValues("failed").Inc()
			log.ErrorContext(ctx, "commit_import failed", "import", a.ImportID, "attempt", job.Attempt, "error", err)
			if ferr := w.Ledger.FailImport(context.WithoutCancel(ctx), a.UserID, a.ImportID, failedMessage); ferr != nil {
				return errors.Join(err, ferr)
			}
			return err
		}
		w.Metrics.Imports.WithLabelValues("retried").Inc()
		return err
	}
	w.Metrics.Imports.WithLabelValues("committed").Inc()
	w.Metrics.Rows.WithLabelValues("new").Add(float64(res.New))
	w.Metrics.Rows.WithLabelValues("duplicate").Add(float64(res.Duplicates))
	w.Metrics.Rows.WithLabelValues("error").Add(float64(res.Errors))
	log.InfoContext(ctx, "import committed", "import", a.ImportID, "new", res.New, "duplicates", res.Duplicates, "errors", res.Errors)
	return nil
}

// RematerializeFingerprintArgs rebuilds the transactions of one (account, fingerprint) after a
// duplicate-review override is set or cleared (PLAN §4.4; the endpoints arrive in Phase 10).
type RematerializeFingerprintArgs struct {
	UserID      uuid.UUID `json:"userId"`
	AccountID   uuid.UUID `json:"accountId"`
	Fingerprint []byte    `json:"fingerprint"`
}

// Kind identifies the job type in river_job.
func (RematerializeFingerprintArgs) Kind() string { return "rematerialize_fingerprint" }

// InsertOpts allows a few retries; the operation is idempotent.
func (RematerializeFingerprintArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5}
}

// RematerializeFingerprintWorker works RematerializeFingerprintArgs.
type RematerializeFingerprintWorker struct {
	river.WorkerDefaults[RematerializeFingerprintArgs]
	Ledger Ledger
}

// Work re-materialises the fingerprint.
func (w *RematerializeFingerprintWorker) Work(ctx context.Context, job *river.Job[RematerializeFingerprintArgs]) error {
	return w.Ledger.Rematerialize(ctx, job.Args.UserID, job.Args.AccountID, job.Args.Fingerprint)
}
