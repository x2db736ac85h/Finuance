package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// DefaultRetentionDays is how long a chat is kept after its last message unless the user
// chooses otherwise (our Product Engineer's decision, Phase 13).
const DefaultRetentionDays = 2

// MaxRetentionDays is the longest choosable retention short of keeping chats forever.
const MaxRetentionDays = 7

// Settings are the user's assistant settings the server enforces.
type Settings struct {
	RetentionDays *int     // nil: keep chats until deleted
	CloudConsent  []string // cloud connections the user agreed to send data to
}

// Consented reports whether the user agreed to send data to a cloud connection.
func (s Settings) Consented(conn string) bool { return slices.Contains(s.CloudConsent, conn) }

// Chat is one conversation.
type Chat struct {
	ID         uuid.UUID
	Title      string
	Connection string
	Model      string
	NoTools    bool // auto mode switched this chat to plan mode
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Message is one stored model message. The last assistant message of a turn has Meta (and
// Steps); Error marks a turn that failed.
type Message struct {
	Seq        int
	Role       llm.Role
	Content    string
	Reasoning  string
	ToolCalls  []llm.ToolCall
	ToolCallID string
	Replay     json.RawMessage
	Steps      []Step
	Meta       *Meta
	Error      *TurnError
	CreatedAt  time.Time
}

// Step is one lookup as the panel shows it: exactly what went back to the model (PLAN §4.8).
type Step struct {
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Summary    string          `json:"summary"`
	Result     string          `json:"result"` // the JSON the model received, cut at MaxStepResult
	Truncated  bool            `json:"truncated,omitempty"`
	Matched    *int            `json:"matched,omitempty"`
	ToolFilter *StepFilter     `json:"toolFilter,omitempty"`
}

// StepFilter is a step's lookup filter in the API's ToolFilter form, for "Show in table".
type StepFilter struct {
	Query     string  `json:"query,omitempty"`
	Payee     string  `json:"payee,omitempty"`
	Category  string  `json:"category,omitempty"`
	Group     string  `json:"group,omitempty"`
	DateFrom  string  `json:"dateFrom,omitempty"`
	DateTo    string  `json:"dateTo,omitempty"`
	Type      string  `json:"type,omitempty"`
	MinAmount *string `json:"minAmount,omitempty"`
	MaxAmount *string `json:"maxAmount,omitempty"`
	Label     string  `json:"label"`
}

// Meta describes a finished turn.
type Meta struct {
	DurationMs       int64       `json:"durationMs"`
	ApproxTokensSent int         `json:"approxTokensSent"`
	Lookups          int         `json:"lookups"`
	Rounds           int         `json:"rounds"`
	Connection       string      `json:"connection"`
	Model            string      `json:"model"`
	Mode             string      `json:"mode"` // tools or plan
	Fallback         bool        `json:"fallback,omitempty"`
	Stopped          bool        `json:"stopped,omitempty"`
	Scope            string      `json:"scope"` // all or view
	AccountIDs       []uuid.UUID `json:"accountIds,omitempty"`
	InputTokens      int         `json:"inputTokens,omitempty"` // as the server reported them
	OutputTokens     int         `json:"outputTokens,omitempty"`
}

// TurnError is why a turn failed: an llm error kind (or "internal") and a message for the user.
type TurnError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Storage caps (PLAN §4.8).
const (
	MaxStepResult    = 6000
	MaxHistoryTool   = 4000
	MaxReasoning     = 4000
	maxTitle         = 120
	cleanupPageUsers = 500
)

// Chats stores conversations under the user's RLS context.
type Chats struct {
	st       *store.Store
	clock    platform.Clock
	maxChats int
}

// NewChats returns the chat store; maxChats is FINUANCE_AI_MAX_CHATS.
func NewChats(st *store.Store, clock platform.Clock, maxChats int) *Chats {
	return &Chats{st: st, clock: clock, maxChats: maxChats}
}

// Limit is the most chats a user keeps (FINUANCE_AI_MAX_CHATS); creating one more drops the oldest.
func (c *Chats) Limit() int { return c.maxChats }

// Settings returns the user's settings (the defaults before the first save).
func (c *Chats) Settings(ctx context.Context, userID uuid.UUID) (Settings, error) {
	var s Settings
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		s, err = settings(ctx, q, userID)
		return err
	})
	return s, err
}

func settings(ctx context.Context, q *db.Queries, userID uuid.UUID) (Settings, error) {
	row, err := q.GetAssistantSettings(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		d := DefaultRetentionDays
		return Settings{RetentionDays: &d, CloudConsent: []string{}}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := Settings{CloudConsent: row.CloudConsent}
	if row.RetentionDays != nil {
		d := int(*row.RetentionDays)
		s.RetentionDays = &d
	}
	if s.CloudConsent == nil {
		s.CloudConsent = []string{}
	}
	return s, nil
}

// SaveSettings replaces the settings and applies the retention at once.
func (c *Chats) SaveSettings(ctx context.Context, userID uuid.UUID, s Settings) (Settings, error) {
	ve := &platform.ValidationError{}
	var days *int16
	if s.RetentionDays != nil {
		if *s.RetentionDays < 1 || *s.RetentionDays > MaxRetentionDays {
			ve.Add("retentionDays", fmt.Sprintf("must be 1 to %d days, or null to keep chats", MaxRetentionDays))
		} else {
			d := int16(*s.RetentionDays) //nolint:gosec // 1..7
			days = &d
		}
	}
	if len(s.CloudConsent) > 32 {
		ve.Add("cloudConsent", "too many connections")
	}
	if err := ve.Err(); err != nil {
		return Settings{}, err
	}
	consent := slices.Compact(slices.Sorted(slices.Values(s.CloudConsent)))
	if consent == nil {
		consent = []string{}
	}
	var out Settings
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.UpsertAssistantSettings(ctx, db.UpsertAssistantSettingsParams{UserID: userID, RetentionDays: days,
			CloudConsent: consent, Now: c.clock.Now()}); err != nil {
			return err
		}
		var err error
		if out, err = settings(ctx, q, userID); err != nil {
			return err
		}
		return c.expire(ctx, q, userID, out)
	})
	return out, err
}

// expire deletes the chats past the retention.
func (c *Chats) expire(ctx context.Context, q *db.Queries, userID uuid.UUID, s Settings) error {
	if s.RetentionDays == nil {
		return nil
	}
	_, err := q.DeleteAssistantChatsBefore(ctx, db.DeleteAssistantChatsBeforeParams{UserID: userID,
		Cutoff: c.clock.Now().Add(-time.Duration(*s.RetentionDays) * 24 * time.Hour)})
	return err
}

// List returns the chats, newest first, after removing expired ones (the hourly job may not have
// run yet).
func (c *Chats) List(ctx context.Context, userID uuid.UUID) ([]Chat, error) {
	var out []Chat
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		s, err := settings(ctx, q, userID)
		if err != nil {
			return err
		}
		if err := c.expire(ctx, q, userID, s); err != nil {
			return err
		}
		rows, err := q.ListAssistantChats(ctx, userID)
		for i := range rows {
			out = append(out, chatOf(rows[i]))
		}
		return err
	})
	return out, err
}

func chatOf(r db.AssistantChat) Chat {
	return Chat{ID: r.ID, Title: r.Title, Connection: r.Connection, Model: r.Model, NoTools: r.NoTools, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// Create starts a chat and drops the oldest chats beyond the limit.
func (c *Chats) Create(ctx context.Context, userID uuid.UUID) (Chat, error) {
	var out Chat
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.CreateAssistantChat(ctx, db.CreateAssistantChatParams{ID: platform.NewID(), UserID: userID, Now: c.clock.Now()})
		if err != nil {
			return err
		}
		out = chatOf(r)
		_, err = q.TrimAssistantChats(ctx, db.TrimAssistantChatsParams{UserID: userID, Keep: int32(c.maxChats)}) //nolint:gosec // config-bounded
		return err
	})
	return out, err
}

// Get returns a chat with its messages; an expired chat is not found.
func (c *Chats) Get(ctx context.Context, userID, chatID uuid.UUID) (Chat, []Message, error) {
	var (
		chat Chat
		msgs []Message
	)
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		s, err := settings(ctx, q, userID)
		if err != nil {
			return err
		}
		if err := c.expire(ctx, q, userID, s); err != nil {
			return err
		}
		r, err := q.GetAssistantChat(ctx, db.GetAssistantChatParams{UserID: userID, ID: chatID})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chat %s: %w", chatID, platform.ErrNotFound)
		}
		if err != nil {
			return err
		}
		chat = chatOf(r)
		rows, err := q.ListAssistantMessages(ctx, db.ListAssistantMessagesParams{UserID: userID, ChatID: chatID})
		if err != nil {
			return err
		}
		for i := range rows {
			msg, err := messageOf(&rows[i])
			if err != nil {
				return err
			}
			msgs = append(msgs, msg)
		}
		return nil
	})
	return chat, msgs, err
}

func messageOf(m *db.AssistantMessage) (Message, error) {
	out := Message{Seq: int(m.Seq), Role: llm.Role(m.Role), Content: m.Content, Reasoning: m.Reasoning, ToolCallID: m.ToolCallID,
		Replay: m.Replay, CreatedAt: m.CreatedAt}
	for _, f := range []struct {
		raw []byte
		dst any
	}{{m.ToolCalls, &out.ToolCalls}, {m.Steps, &out.Steps}, {m.Meta, &out.Meta}, {m.Error, &out.Error}} {
		if len(f.raw) > 0 {
			if err := json.Unmarshal(f.raw, f.dst); err != nil {
				return Message{}, fmt.Errorf("assistant message %d: %w", m.ID, err)
			}
		}
	}
	return out, nil
}

// Delete removes a chat.
func (c *Chats) Delete(ctx context.Context, userID, chatID uuid.UUID) error {
	return c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteAssistantChat(ctx, db.DeleteAssistantChatParams{UserID: userID, ID: chatID})
		if err == nil && n == 0 {
			return fmt.Errorf("chat %s: %w", chatID, platform.ErrNotFound)
		}
		return err
	})
}

// ChatUpdate is what a turn changes on its chat.
type ChatUpdate struct {
	Title, Connection, Model string
	NoTools                  bool
}

// Append stores messages at the end of a chat and updates the chat, in one transaction. It
// returns the sequence number of the last message.
func (c *Chats) Append(ctx context.Context, userID, chatID uuid.UUID, msgs []Message, up ChatUpdate) (int, error) {
	last := 0
	err := c.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		now := c.clock.Now()
		for i := range msgs {
			p, err := paramsOf(userID, chatID, &msgs[i], now)
			if err != nil {
				return err
			}
			r, err := q.InsertAssistantMessage(ctx, p)
			if err != nil {
				return err
			}
			last = int(r.Seq)
		}
		return q.UpdateAssistantChat(ctx, db.UpdateAssistantChatParams{UserID: userID, ID: chatID, Now: now, Connection: up.Connection,
			Model: up.Model, NoTools: up.NoTools, Title: clip(up.Title, maxTitle)})
	})
	return last, err
}

func paramsOf(userID, chatID uuid.UUID, m *Message, now time.Time) (db.InsertAssistantMessageParams, error) {
	p := db.InsertAssistantMessageParams{ChatID: chatID, UserID: userID, Role: string(m.Role), Content: m.Content,
		Reasoning: clip(m.Reasoning, MaxReasoning), ToolCallID: m.ToolCallID, Replay: m.Replay, Now: now}
	if m.Role == llm.RoleTool {
		p.Content = clip(m.Content, MaxHistoryTool)
	}
	var err error
	marshal := func(v any, ok bool) []byte {
		if !ok || err != nil {
			return nil
		}
		var b []byte
		b, err = json.Marshal(v)
		return b
	}
	p.ToolCalls = marshal(m.ToolCalls, len(m.ToolCalls) > 0)
	p.Steps = marshal(m.Steps, m.Meta != nil)
	p.Meta = marshal(m.Meta, m.Meta != nil)
	p.Error = marshal(m.Error, m.Error != nil)
	return p, err
}

// Cleanup applies every user's retention (the hourly job). It returns the number of chats deleted.
func (c *Chats) Cleanup(ctx context.Context) (int64, error) {
	var total int64
	after := uuid.Nil
	for {
		var ids []uuid.UUID
		if err := c.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
			var err error
			ids, err = q.ListUserIDsAfter(ctx, db.ListUserIDsAfterParams{After: after, PageSize: cleanupPageUsers})
			return err
		}); err != nil {
			return total, err
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			err := c.st.WithUser(ctx, id, func(q *db.Queries, _ pgx.Tx) error {
				s, err := settings(ctx, q, id)
				if err != nil || s.RetentionDays == nil {
					return err
				}
				n, err := q.DeleteAssistantChatsBefore(ctx, db.DeleteAssistantChatsBeforeParams{UserID: id,
					Cutoff: c.clock.Now().Add(-time.Duration(*s.RetentionDays) * 24 * time.Hour)})
				total += n
				return err
			})
			if err != nil {
				return total, err
			}
		}
		if len(ids) < cleanupPageUsers {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}
