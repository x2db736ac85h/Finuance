package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeChats struct {
	n     int64
	err   error
	calls int
}

func (f *fakeChats) Cleanup(context.Context) (int64, error) {
	f.calls++
	return f.n, f.err
}

func TestCleanupAssistantChatsWorker(t *testing.T) {
	job := &river.Job[CleanupAssistantChatsArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	f := &fakeChats{n: 2}
	if err := (&CleanupAssistantChatsWorker{Chats: f}).Work(context.Background(), job); err != nil || f.calls != 1 {
		t.Errorf("Work = %v, calls %d", err, f.calls)
	}
	boom := errors.New("db down")
	if err := (&CleanupAssistantChatsWorker{Chats: &fakeChats{err: boom}}).Work(context.Background(), job); !errors.Is(err, boom) {
		t.Errorf("error not returned: %v", err)
	}
	if err := (&CleanupAssistantChatsWorker{}).Work(context.Background(), job); err == nil {
		t.Error("missing dependency not reported")
	}
}
