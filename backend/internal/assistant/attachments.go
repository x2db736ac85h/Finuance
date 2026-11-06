package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/images"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Photos attached to a question (Phase 20, receipts by photo). An upload is checked and
// re-encoded (internal/images) and waits in its chat until a question takes it; it then goes to
// the model with that question only (later questions don't send it again) and stays with the chat,
// so the chat's retention deletes it. Unsent uploads are deleted after a day.

// Limits.
const (
	MaxAttachments      = 3 // per question
	maxUnsent           = 6 // waiting in one chat
	unsentAttachmentTTL = 24 * time.Hour
)

// Attachment is a sent photo as the chat shows it.
type Attachment struct {
	ID            uuid.UUID
	Seq           int // the question it went with
	Width, Height int
}

// AddAttachment checks and stores a photo for the chat's next question.
func (c *Chats) AddAttachment(ctx context.Context, userID, chatID uuid.UUID, data []byte) (Attachment, error) {
	img, err := images.Normalize(data)
	if err != nil {
		return Attachment{}, fieldError("image", err.Error())
	}
	a := Attachment{ID: platform.NewID(), Width: img.Width, Height: img.Height}
	err = c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetAssistantChat(ctx, db.GetAssistantChatParams{UserID: userID, ID: chatID}); errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chat %s: %w", chatID, platform.ErrNotFound)
		} else if err != nil {
			return err
		}
		n, err := q.CountUnsentAssistantAttachments(ctx, db.CountUnsentAssistantAttachmentsParams{UserID: userID, ChatID: chatID})
		if err != nil {
			return err
		}
		if n >= maxUnsent {
			return fieldError("image", fmt.Sprintf("at most %d photos can wait for a question", maxUnsent))
		}
		return q.InsertAssistantAttachment(ctx, db.InsertAssistantAttachmentParams{ID: a.ID, UserID: userID, ChatID: chatID,
			Mime: "image/jpeg", Width: int32(img.Width), Height: int32(img.Height), Bytes: img.Data, Now: c.clock.Now()}) //nolint:gosec // ≤ 2048
	})
	return a, err
}

// AttachmentData returns a photo of the chat (the panel's thumbnail).
func (c *Chats) AttachmentData(ctx context.Context, userID, chatID, id uuid.UUID) ([]byte, error) {
	var out []byte
	err := c.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		a, err := q.GetAssistantAttachment(ctx, db.GetAssistantAttachmentParams{UserID: userID, ChatID: chatID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("attachment %s: %w", id, platform.ErrNotFound)
		}
		out = a.Bytes
		return err
	})
	return out, err
}

// unsentImages reads the photos a question names: each must be waiting in this chat.
func (c *Chats) unsentImages(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID) ([]llm.Image, error) {
	var out []llm.Image
	err := c.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.UnsentAssistantAttachments(ctx, db.UnsentAssistantAttachmentsParams{UserID: userID, ChatID: chatID, Ids: ids})
		if err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return fieldError("attachmentIds", "names a photo that isn't waiting in this chat")
		}
		for i := range rows {
			out = append(out, llm.Image{MIME: rows[i].Mime, Data: rows[i].Bytes})
		}
		return nil
	})
	return out, err
}

// markSent ties the photos to the question that took them.
func (c *Chats) markSent(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID, seq int) error {
	return c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		return q.MarkAssistantAttachmentsSent(ctx, db.MarkAssistantAttachmentsSentParams{UserID: userID, ChatID: chatID, Ids: ids,
			Seq: int32(seq)}) //nolint:gosec // a message number
	})
}

// attachmentRefs lists the chat's sent photos by question.
func attachmentRefs(ctx context.Context, q *db.Queries, userID, chatID uuid.UUID) (map[int][]Attachment, error) {
	rows, err := q.ListAssistantAttachmentRefs(ctx, db.ListAssistantAttachmentRefsParams{UserID: userID, ChatID: chatID})
	if err != nil {
		return nil, err
	}
	out := map[int][]Attachment{}
	for _, r := range rows {
		if r.MessageSeq == nil {
			continue
		}
		s := int(*r.MessageSeq)
		out[s] = append(out[s], Attachment{ID: r.ID, Seq: s, Width: int(r.Width), Height: int(r.Height)})
	}
	return out, nil
}

func fieldError(field, msg string) error {
	v := &platform.ValidationError{}
	v.Add(field, msg)
	return v
}

// uniqueIDs drops repeated ids, keeping the order.
func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ExportAttachments hands every sent photo of the user to fn (the data export).
func (c *Chats) ExportAttachments(ctx context.Context, userID uuid.UUID, fn func(chatID, id uuid.UUID, seq int, content []byte) error) error {
	return c.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ExportAssistantAttachments(ctx, userID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.MessageSeq == nil {
				continue
			}
			if err := fn(r.ChatID, r.ID, int(*r.MessageSeq), r.Bytes); err != nil {
				return err
			}
		}
		return nil
	})
}
