package ledger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// AIOrigin says that a change comes from an applied assistant proposal (ADR 0030): the chat, the
// proposal's key and action, and whether the user had it applied automatically. A handler puts it
// into the request's context after checking the proposal; every write path that sees it records
// what it changed, in the same transaction as the change.
type AIOrigin struct {
	ChatID      uuid.UUID
	ProposalKey string
	Action      string
	Auto        bool
}

type aiOriginKey struct{}

// WithAIOrigin marks ctx's writes as the assistant's.
func WithAIOrigin(ctx context.Context, o AIOrigin) context.Context {
	return context.WithValue(ctx, aiOriginKey{}, o)
}

// AIOriginFrom returns the origin WithAIOrigin set, if any.
func AIOriginFrom(ctx context.Context) (AIOrigin, bool) {
	o, ok := ctx.Value(aiOriginKey{}).(AIOrigin)
	return o, ok
}

// withoutAIOrigin hides the origin from a nested write that its caller records itself.
func withoutAIOrigin(ctx context.Context) context.Context {
	return context.WithValue(ctx, aiOriginKey{}, nil)
}

// Entities an AI change can touch (ai_change_items.entity).
const (
	AIEntityTransaction   = "transaction"
	AIEntityGroup         = "group"
	AIEntityCategory      = "category"
	AIEntityRule          = "rule"
	AIEntityBudget        = "budget"
	AIEntityGoal          = "goal"
	AIEntitySharedExpense = "shared_expense"
)

// AIItem is one entity a change touched, with the fields the action set before and after it.
// A nil Before means the change created the entity; a nil After that it deleted it.
type AIItem struct {
	Entity string
	ID     uuid.UUID
	Before any
	After  any
}

// RecordAIChange adds items to the open change of ctx's proposal inside the caller's transaction.
// Without an origin in ctx (an ordinary edit) it does nothing.
func RecordAIChange(ctx context.Context, q *db.Queries, clock platform.Clock, userID uuid.UUID, items []AIItem) error {
	o, ok := AIOriginFrom(ctx)
	if !ok || len(items) == 0 {
		return nil
	}
	changeID, err := q.OpenAIChange(ctx, db.OpenAIChangeParams{ID: platform.NewID(), UserID: userID, ChatID: o.ChatID,
		ProposalKey: o.ProposalKey, Action: o.Action, Auto: o.Auto, Now: clock.Now()})
	if err != nil {
		return fmt.Errorf("open ai change: %w", err)
	}
	for _, it := range items {
		before, err := jsonOrNil(it.Before)
		if err != nil {
			return err
		}
		after, err := jsonOrNil(it.After)
		if err != nil {
			return err
		}
		if err := q.UpsertAIChangeItem(ctx, db.UpsertAIChangeItemParams{ChangeID: changeID, UserID: userID,
			Entity: it.Entity, EntityID: it.ID, Before: before, After: after}); err != nil {
			return fmt.Errorf("ai change item %s %s: %w", it.Entity, it.ID, err)
		}
	}
	return nil
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ai change state: %w", err)
	}
	return b, nil
}

// AnnotationState is the part of a transaction an annotation change sets, as recorded in an AI
// change: the category and where it came from, and/or the group. Unset parts are left out, so a
// revert compares and restores only what the change touched.
type AnnotationState struct {
	CategoryID     *uuid.UUID `json:"categoryId,omitempty"`
	CategorySource string     `json:"categorySource,omitempty"`
	GroupID        *uuid.UUID `json:"groupId,omitempty"`
	HasCategory    bool       `json:"hasCategory,omitempty"`
	HasGroup       bool       `json:"hasGroup,omitempty"`
}

func annotationState(categoryID *uuid.UUID, source string, groupID *uuid.UUID, p AnnotationPatch) AnnotationState {
	var s AnnotationState
	if p.SetCategory {
		s.HasCategory, s.CategoryID, s.CategorySource = true, categoryID, source
	}
	if p.SetGroup {
		s.HasGroup, s.GroupID = true, groupID
	}
	return s
}

// CreatedState is the "after" of an entity a change created (a group, category or rule), so a
// revert can tell whether it is still the one the assistant made.
type CreatedState struct {
	Created bool   `json:"created"`
	Name    string `json:"name,omitempty"`
}

// AnnotateTx is Annotate inside the caller's transaction as an ordinary edit (an AI change's
// revert): it never records an AI change itself.
func (l *Ledger) AnnotateTx(ctx context.Context, q *db.Queries, userID uuid.UUID, ids []uuid.UUID, patch AnnotationPatch) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	return l.annotateTx(withoutAIOrigin(ctx), q, userID, ids, patch)
}
