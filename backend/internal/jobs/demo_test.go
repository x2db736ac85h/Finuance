package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeDemo struct {
	err   error
	calls int
}

func (f *fakeDemo) CleanupDemoUsers(context.Context) (int64, error) {
	f.calls++
	return 2, f.err
}

func TestCleanupDemoUsersWorker(t *testing.T) {
	job := &river.Job[CleanupDemoUsersArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	f := &fakeDemo{}
	if err := (&CleanupDemoUsersWorker{Demo: f}).Work(context.Background(), job); err != nil || f.calls != 1 {
		t.Errorf("Work = %v, calls %d", err, f.calls)
	}
	boom := errors.New("db down")
	if err := (&CleanupDemoUsersWorker{Demo: &fakeDemo{err: boom}}).Work(context.Background(), job); !errors.Is(err, boom) {
		t.Errorf("error not returned (River retries on error): %v", err)
	}
	if err := (&CleanupDemoUsersWorker{}).Work(context.Background(), job); err == nil {
		t.Error("missing dependency not reported")
	}
}
