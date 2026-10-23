package jobs

import (
	"context"
	"errors"

	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// ChatCleaner applies the assistant's chat retention (assistant.Chats).
type ChatCleaner interface {
	Cleanup(ctx context.Context) (int64, error)
}

// CleanupAssistantChatsArgs deletes chats older than their user's retention (48 h by default,
// 1–7 days or never by choice; PLAN §4.8). It runs hourly and when a worker becomes leader. Reads
// also skip expired chats, so a late run never shows an expired chat.
type CleanupAssistantChatsArgs struct{}

// Kind identifies the job type in river_job.
func (CleanupAssistantChatsArgs) Kind() string { return "cleanup_assistant_chats" }

// InsertOpts allows a few retries; the next hour repeats a missed run.
func (CleanupAssistantChatsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3}
}

// CleanupAssistantChatsWorker works CleanupAssistantChatsArgs.
type CleanupAssistantChatsWorker struct {
	river.WorkerDefaults[CleanupAssistantChatsArgs]
	Chats ChatCleaner
}

// Work deletes the expired chats and logs how many.
func (w *CleanupAssistantChatsWorker) Work(ctx context.Context, _ *river.Job[CleanupAssistantChatsArgs]) error {
	if w.Chats == nil {
		return errors.New("cleanup_assistant_chats: no chat store configured")
	}
	n, err := w.Chats.Cleanup(ctx)
	if err != nil {
		return err
	}
	observability.Logger(ctx).InfoContext(ctx, "expired assistant chats deleted", "count", n)
	return nil
}
