package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
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
	if len(PeriodicJobs()) != 1 {
		t.Errorf("periodic jobs = %d", len(PeriodicJobs()))
	}
	if Workers(Deps{Sessions: &fakeCleaner{}}) == nil {
		t.Error("no workers")
	}
}
