package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	dbtest "github.com/x2db736ac85h/finuance/backend/testutil"
)

type fakeDigests struct {
	mu   sync.Mutex
	due  []uuid.UUID // users the scan finds due
	pool interface {
		Begin(context.Context) (pgx.Tx, error)
	}
	writes []AssistantDigestArgs
	done   chan struct{}
	err    error
}

func (f *fakeDigests) Schedule(ctx context.Context, enqueue func(context.Context, pgx.Tx, uuid.UUID, time.Time) error) (int, error) {
	for _, u := range f.due {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			return 0, err
		}
		if err := enqueue(ctx, tx, u, time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)); err != nil {
			_ = tx.Rollback(ctx)
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
	}
	return len(f.due), nil
}

func (f *fakeDigests) Write(_ context.Context, u uuid.UUID, manual bool) (assistant.DigestOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, AssistantDigestArgs{UserID: u, Manual: manual})
	if f.done != nil && len(f.writes) == 2 {
		close(f.done)
	}
	return assistant.DigestOutcome{Proposals: 1}, f.err
}

// TestDigestJobs: the scan queues a digest per due user in the transaction it is given, the digest
// queue works them one at a time, and one asked for now waits at most once per user (ADR 0033).
func TestDigestJobs(t *testing.T) {
	tdb := dbtest.Require(t, testDB)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	user := platform.NewID()
	f := &fakeDigests{due: []uuid.UUID{user}, pool: tdb.App, done: make(chan struct{})}
	worker, err := river.NewClient(riverpgxv5.New(tdb.App), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}, DigestQueue: {MaxWorkers: 1}},
		Workers: Workers(Deps{Digests: f}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// asked for now twice before the worker runs: one job
	first, err := worker.Insert(ctx, AssistantDigestArgs{UserID: user, Manual: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := worker.Insert(ctx, AssistantDigestArgs{UserID: user, Manual: true}, nil)
	if err != nil || !again.UniqueSkippedAsDuplicate || again.Job.ID != first.Job.ID || first.Job.Queue != DigestQueue || first.Job.MaxAttempts != 1 {
		t.Fatalf("second request: %+v %v", again, err)
	}
	if _, err := worker.Insert(ctx, AssistantDigestScanArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.done:
	case <-ctx.Done():
		t.Fatalf("digests not written: %+v", f.writes)
	}
	if err := worker.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	manual, scheduled := 0, 0
	for _, w := range f.writes {
		if w.UserID != user {
			t.Errorf("write for %s", w.UserID)
		}
		if w.Manual {
			manual++
		} else {
			scheduled++
		}
	}
	if manual != 1 || scheduled != 1 {
		t.Errorf("writes = %+v", f.writes)
	}
	if (AssistantDigestArgs{}).Kind() != "assistant_digest" || (AssistantDigestScanArgs{}).Kind() != "assistant_digest_scan" ||
		(&AssistantDigestWorker{}).Timeout(nil) != DigestTimeout {
		t.Error("digest job kinds or timeout changed; a kind rename orphans queued jobs")
	}
}

func TestDigestWorkerErrors(t *testing.T) {
	ctx := context.Background()
	job := &river.Job[AssistantDigestArgs]{Args: AssistantDigestArgs{UserID: platform.NewID()}}
	if err := (&AssistantDigestWorker{}).Work(ctx, job); err == nil {
		t.Error("a worker without the assistant wrote a digest")
	}
	boom := errors.New("db down")
	if err := (&AssistantDigestWorker{Digests: &fakeDigests{err: boom}}).Work(ctx, job); !errors.Is(err, boom) {
		t.Errorf("error not returned: %v", err)
	}
	// the scan on a worker without the assistant does nothing
	if err := (&AssistantDigestScanWorker{}).Work(ctx, &river.Job[AssistantDigestScanArgs]{}); err != nil {
		t.Error(err)
	}
}
