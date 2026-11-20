package jobs

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	dbtest "github.com/x2db736ac85h/finuance/backend/testutil"
)

// countingReclassifier counts full runs per user; each run takes a moment, like a real one.
type countingReclassifier struct {
	mu   sync.Mutex
	runs map[uuid.UUID]int
	n    atomic.Int64
}

func (c *countingReclassifier) Reclassify(_ context.Context, u uuid.UUID) (rules.Result, error) {
	time.Sleep(20 * time.Millisecond)
	c.mu.Lock()
	c.runs[u]++
	c.mu.Unlock()
	c.n.Add(1)
	return rules.Result{}, nil
}

// TestReclassifyBurstCostsFewRuns: sixty reclassifications queued for one user (a burst of edits,
// or a script) cost a few full runs, because a job steps aside when a newer one waits; another
// user's single job runs, and the last change is always covered: a full run starts after the last
// job was queued (IMP-5).
func TestReclassifyBurstCostsFewRuns(t *testing.T) {
	tdb := dbtest.Require(t, testDB)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	busy, other := platform.NewID(), platform.NewID()
	rc := &countingReclassifier{runs: map[uuid.UUID]int{}}
	client, err := river.NewClient(riverpgxv5.New(tdb.App), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Workers: Workers(Deps{Rules: rc}), FetchCooldown: 20 * time.Millisecond, FetchPollInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	const burst = 60
	var lastID int64
	for range burst {
		res, err := client.Insert(ctx, ReclassifyArgs{UserID: busy}, nil)
		if err != nil {
			t.Fatal(err)
		}
		lastID = res.Job.ID
	}
	if _, err := client.Insert(ctx, ReclassifyArgs{UserID: other}, nil); err != nil {
		t.Fatal(err)
	}
	events, stop := client.Subscribe(river.EventKindJobCompleted)
	defer stop()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done, lastDone := 0, false
	for done < burst+1 {
		select {
		case ev := <-events:
			if ev.Job.Kind == (ReclassifyArgs{}).Kind() {
				done++
				lastDone = lastDone || ev.Job.ID == lastID
			}
		case <-ctx.Done():
			t.Fatalf("%d of %d jobs completed", done, burst+1)
		}
	}
	if err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if !lastDone {
		t.Error("the last job queued did not complete")
	}
	if n := rc.runs[busy]; n < 1 || n > 6 {
		t.Errorf("%d full runs for %d queued jobs of one user, want a few (at least 1)", n, burst)
	}
	if rc.runs[other] != 1 {
		t.Errorf("the other user's job ran %d times, want 1", rc.runs[other])
	}
}
