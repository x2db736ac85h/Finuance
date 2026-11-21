package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

type fakeMigrator struct {
	runErr error
	failed string
}

func (f *fakeMigrator) Run(context.Context, uuid.UUID, uuid.UUID) error { return f.runErr }
func (f *fakeMigrator) Fail(_ context.Context, _, _ uuid.UUID, message string) error {
	f.failed = message
	return nil
}

// TestLegacyImportFailure: after the last attempt the run is marked failed with a fixed sentence
// (the error's text stays in the log) and, for a run a request started, that request's reference.
func TestLegacyImportFailure(t *testing.T) {
	job := func(attempt int) *river.Job[LegacyImportArgs] {
		return &river.Job[LegacyImportArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: LegacyImportAttempts},
			Args: LegacyImportArgs{UserID: uuid.New(), ID: uuid.New()}}
	}
	m := &fakeMigrator{runErr: errors.New("chat 3: secret text")}
	w := &LegacyImportWorker{Migrator: m}
	if err := w.Work(context.Background(), job(1)); err == nil || m.failed != "" {
		t.Errorf("attempt 1: %v, failed %q", err, m.failed)
	}
	if err := w.Work(context.Background(), job(LegacyImportAttempts)); err == nil || m.failed != legacyFailedMessage {
		t.Errorf("last attempt: %v, failed %q", err, m.failed)
	}
	ctx := observability.WithRequestID(context.Background(), "0a1b2c")
	if err := w.Work(ctx, job(LegacyImportAttempts)); err == nil || m.failed != legacyFailedMessage+" (Reference: 0a1b2c)" {
		t.Errorf("last attempt of a request's run: %v, failed %q", err, m.failed)
	}
	if strings.Contains(m.failed, "secret") {
		t.Error("internal error text reached the user")
	}
}
