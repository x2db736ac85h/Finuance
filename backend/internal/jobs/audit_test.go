package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeAudit struct {
	err   error
	calls int
}

func (f *fakeAudit) CleanupAudit(context.Context) (int64, error) {
	f.calls++
	return 4, f.err
}

func TestCleanupAuditLogWorker(t *testing.T) {
	job := &river.Job[CleanupAuditLogArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	f := &fakeAudit{}
	if err := (&CleanupAuditLogWorker{Audit: f}).Work(context.Background(), job); err != nil || f.calls != 1 {
		t.Errorf("Work = %v, calls %d", err, f.calls)
	}
	boom := errors.New("db down")
	if err := (&CleanupAuditLogWorker{Audit: &fakeAudit{err: boom}}).Work(context.Background(), job); !errors.Is(err, boom) {
		t.Errorf("error not returned (River retries on error): %v", err)
	}
	if err := (&CleanupAuditLogWorker{}).Work(context.Background(), job); err == nil {
		t.Error("missing dependency not reported")
	}
}
