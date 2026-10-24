package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/x2db736ac85h/finuance/backend/internal/rules"
)

type fakeReclassifier struct {
	users []uuid.UUID
	err   error
}

func (f *fakeReclassifier) Reclassify(_ context.Context, u uuid.UUID) (rules.Result, error) {
	f.users = append(f.users, u)
	return rules.Result{Categorised: 2, Pairs: 1}, f.err
}

func TestReclassifyWorker(t *testing.T) {
	u := uuid.New()
	job := &river.Job[ReclassifyArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: ReclassifyArgs{UserID: u}}
	f := &fakeReclassifier{}
	if err := (&ReclassifyWorker{Rules: f}).Work(context.Background(), job); err != nil || len(f.users) != 1 || f.users[0] != u {
		t.Errorf("Work = %v, users %v", err, f.users)
	}
	boom := errors.New("db down")
	if err := (&ReclassifyWorker{Rules: &fakeReclassifier{err: boom}}).Work(context.Background(), job); !errors.Is(err, boom) {
		t.Errorf("error not returned (River retries on error): %v", err)
	}
	if err := (&ReclassifyWorker{}).Work(context.Background(), job); err == nil {
		t.Error("missing dependency not reported")
	}
	if (ReclassifyArgs{}).Kind() != "reclassify" || (ReclassifyArgs{}).InsertOpts().MaxAttempts != 5 {
		t.Error("job kind or options changed; a kind rename orphans queued jobs")
	}
	// outside River (no client in the context) enqueueing from another job is a no-op
	if err := enqueueReclassify(context.Background(), u); err != nil {
		t.Errorf("enqueue without a client: %v", err)
	}
}
