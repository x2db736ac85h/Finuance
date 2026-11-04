package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

type fakeCleaner struct {
	n     int64
	err   error
	calls int
}

func (f *fakeCleaner) CleanupExpiredSessions(context.Context) (int64, error) {
	f.calls++
	return f.n, f.err
}

func TestCleanupExpiredSessionsWorker(t *testing.T) {
	job := &river.Job[CleanupExpiredSessionsArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	f := &fakeCleaner{n: 3}
	if err := (&CleanupExpiredSessionsWorker{Sessions: f}).Work(context.Background(), job); err != nil || f.calls != 1 {
		t.Errorf("Work = %v, calls %d", err, f.calls)
	}
	boom := errors.New("db down")
	if err := (&CleanupExpiredSessionsWorker{Sessions: &fakeCleaner{err: boom}}).Work(context.Background(), job); !errors.Is(err, boom) {
		t.Errorf("error not returned (River retries on error): %v", err)
	}
	if err := (&CleanupExpiredSessionsWorker{}).Work(context.Background(), job); err == nil {
		t.Error("missing dependency not reported")
	}
}

func TestRegistry(t *testing.T) {
	if (CleanupExpiredSessionsArgs{}).Kind() != "cleanup_expired_sessions" || (CleanupExpiredSessionsArgs{}).InsertOpts().MaxAttempts != 3 {
		t.Error("job kind or options changed; a kind rename orphans queued jobs")
	}
	if (CleanupAssistantChatsArgs{}).Kind() != "cleanup_assistant_chats" || (CleanupAssistantChatsArgs{}).InsertOpts().MaxAttempts != 3 {
		t.Error("assistant cleanup kind or options changed")
	}
	if (CleanupAuditLogArgs{}).Kind() != "cleanup_audit_log" || (CleanupAuditLogArgs{}).InsertOpts().MaxAttempts != 3 {
		t.Error("audit cleanup kind or options changed")
	}
	if (CleanupDemoUsersArgs{}).Kind() != "cleanup_demo_users" || (CleanupDemoUsersArgs{}).InsertOpts().MaxAttempts != 3 {
		t.Error("demo cleanup kind or options changed")
	}
	if len(PeriodicJobs()) != 6 { // sessions, assistant chats, demo users and friend codes hourly; the audit log and notifications daily
		t.Errorf("periodic jobs = %d", len(PeriodicJobs()))
	}
	if Workers(Deps{Sessions: &fakeCleaner{}}) == nil {
		t.Error("no workers")
	}
}

type fakeLedger struct {
	commitErr error
	failed    string
	remember  int
}

func (f *fakeLedger) CommitImport(context.Context, uuid.UUID, uuid.UUID) (ledger.CommitResult, error) {
	return ledger.CommitResult{New: 2, Duplicates: 1}, f.commitErr
}
func (f *fakeLedger) RememberMapping(context.Context, uuid.UUID, uuid.UUID) error {
	f.remember++
	return nil
}
func (f *fakeLedger) FailImport(_ context.Context, _, _ uuid.UUID, msg string) error {
	f.failed = msg
	return nil
}
func (f *fakeLedger) Rematerialize(context.Context, uuid.UUID, uuid.UUID, []byte) error { return nil }

func commitJob(attempt int, remember bool) *river.Job[CommitImportArgs] {
	return &river.Job[CommitImportArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: CommitAttempts},
		Args: CommitImportArgs{ImportID: uuid.New(), UserID: uuid.New(), RememberMapping: remember}}
}

func TestCommitImportWorker(t *testing.T) {
	m := NewImportMetrics(nil)
	ok := &fakeLedger{}
	w := &CommitImportWorker{Ledger: ok, Metrics: m}
	if err := w.Work(context.Background(), commitJob(1, true)); err != nil || ok.remember != 1 {
		t.Errorf("success: %v, remember %d", err, ok.remember)
	}
	// A transient error is retried (returned) without failing the import …
	flaky := &fakeLedger{commitErr: errors.New("connection reset")}
	w.Ledger = flaky
	if err := w.Work(context.Background(), commitJob(1, false)); err == nil || flaky.failed != "" {
		t.Errorf("attempt 1: %v, failed %q", err, flaky.failed)
	}
	// … until the last attempt, which marks it failed with a sanitised message.
	if err := w.Work(context.Background(), commitJob(CommitAttempts, false)); err == nil || flaky.failed != failedMessage {
		t.Errorf("last attempt: %v, failed %q", err, flaky.failed)
	}
	if strings.Contains(flaky.failed, "connection reset") {
		t.Error("internal error text reached the user")
	}
	// A commit that can't happen any more (deleted, wrong state) is not retried.
	gone := &fakeLedger{commitErr: fmt.Errorf("import: %w", ledger.ErrNotFound)}
	w.Ledger = gone
	if err := w.Work(context.Background(), commitJob(1, false)); err != nil || gone.failed != "" {
		t.Errorf("deleted import: %v, failed %q", err, gone.failed)
	}
	if testutil.ToFloat64(m.Imports.WithLabelValues("committed")) != 1 || testutil.ToFloat64(m.Imports.WithLabelValues("failed")) != 1 ||
		testutil.ToFloat64(m.Rows.WithLabelValues("new")) != 2 {
		t.Error("metrics not recorded")
	}
}
