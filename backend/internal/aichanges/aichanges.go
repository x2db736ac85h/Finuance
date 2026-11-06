// Package aichanges lists the changes the assistant made and undoes them (Phase 20, ADR 0030).
// Changes are recorded by the write paths themselves (ledger.RecordAIChange, in the transaction
// of the change); a revert checks that nothing changed since and puts back what was there, through
// the same ledger functions as an edit by hand.
package aichanges

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Service is safe for concurrent use.
type Service struct {
	st    *store.Store
	led   *ledger.Ledger
	clock platform.Clock
}

// New returns the service.
func New(st *store.Store, led *ledger.Ledger, clock platform.Clock) *Service {
	return &Service{st: st, led: led, clock: clock}
}

// Change is one applied proposal.
type Change struct {
	ID          uuid.UUID  `json:"id"`
	ChatID      uuid.UUID  `json:"chatId"`
	ProposalKey string     `json:"proposalKey"`
	Action      string     `json:"action"`
	Auto        bool       `json:"auto"`
	AppliedAt   time.Time  `json:"appliedAt"`
	RevertedAt  *time.Time `json:"revertedAt,omitempty"`
	Items       int        `json:"items"`
}

// Page is one page of changes, newest first; Next continues it.
type Page struct {
	Changes []Change
	Next    string
}

// MaxPage bounds a page.
const MaxPage = 100

// List returns the user's changes, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, cursor string, limit int32) (Page, error) {
	if limit <= 0 || limit > MaxPage {
		limit = 50
	}
	var before *time.Time
	var beforeID uuid.UUID
	if cursor != "" {
		at, id, ok := strings.Cut(cursor, "_")
		t, err1 := time.Parse(time.RFC3339Nano, at)
		u, err2 := uuid.Parse(id)
		if !ok || err1 != nil || err2 != nil {
			return Page{}, fmt.Errorf("cursor is not a page cursor: %w", ledger.ErrInvalid)
		}
		before, beforeID = &t, u
	}
	var out Page
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListAIChanges(ctx, db.ListAIChangesParams{UserID: userID, BeforeApplied: before, BeforeID: beforeID, PageSize: limit + 1})
		if err != nil {
			return err
		}
		for i, r := range rows {
			if i == int(limit) {
				last := out.Changes[len(out.Changes)-1]
				out.Next = last.AppliedAt.UTC().Format(time.RFC3339Nano) + "_" + last.ID.String()
				break
			}
			out.Changes = append(out.Changes, Change{ID: r.ID, ChatID: r.ChatID, ProposalKey: r.ProposalKey, Action: r.Action,
				Auto: r.Auto, AppliedAt: r.AppliedAt, RevertedAt: r.RevertedAt, Items: int(r.Items)})
		}
		return nil
	})
	return out, err
}

// Result says what a revert did: rows put back, and the groups, categories and rules the change
// had created that were deleted (or kept because they are used now).
type Result struct {
	Change     Change
	Rows       int
	Deleted    int
	Kept       int
	Reclassify bool // a category went back to the bank's or a rule went: the rules run again
}

type item struct {
	entity string
	id     uuid.UUID
	before json.RawMessage
	after  json.RawMessage
}

// Revert undoes a change: platform.ErrChangedSince when anything it touched is different now
// (nothing is undone then), ledger.ErrState when it was undone already. enqueue queues the
// reclassification in the same transaction when categories or rules changed.
func (s *Service) Revert(ctx context.Context, userID, id uuid.UUID, enqueue rules.Enqueue) (Result, error) {
	var res Result
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		c, err := q.GetAIChangeForUpdate(ctx, db.GetAIChangeForUpdateParams{UserID: userID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ai change %s: %w", id, ledger.ErrNotFound)
		} else if err != nil {
			return err
		}
		if c.RevertedAt != nil {
			return fmt.Errorf("ai change %s was undone already: %w", id, ledger.ErrState)
		}
		rows, err := q.ListAIChangeItems(ctx, db.ListAIChangeItemsParams{UserID: userID, ChangeID: id})
		if err != nil {
			return err
		}
		items := make([]item, len(rows))
		for i, r := range rows {
			items[i] = item{entity: r.Entity, id: r.EntityID, before: r.Before, after: r.After}
		}
		if err := s.unchanged(ctx, q, userID, items); err != nil {
			return err
		}
		if res, err = s.undo(ctx, q, userID, items); err != nil {
			return err
		}
		now := s.clock.Now()
		if err := q.MarkAIChangeReverted(ctx, db.MarkAIChangeRevertedParams{UserID: userID, ID: id, Now: &now}); err != nil {
			return err
		}
		res.Change = Change{ID: c.ID, ChatID: c.ChatID, ProposalKey: c.ProposalKey, Action: c.Action, Auto: c.Auto,
			AppliedAt: c.AppliedAt, RevertedAt: &now, Items: len(items)}
		if res.Reclassify && enqueue != nil {
			return enqueue(ctx, tx)
		}
		return nil
	})
	return res, ledger.DBError("revert ai change", err)
}

// unchanged checks that every transaction, budget, split, goal and rule the change touched still is
// as it left it.
// Groups and categories it created never block: the revert deletes them only when unused.
func (s *Service) unchanged(ctx context.Context, q *db.Queries, userID uuid.UUID, items []item) error {
	var ids []uuid.UUID
	for _, it := range items {
		if it.entity == ledger.AIEntityTransaction {
			ids = append(ids, it.id)
		}
	}
	now := map[uuid.UUID]db.TransactionAnnotationStateRow{}
	if len(ids) > 0 {
		rows, err := q.TransactionAnnotationState(ctx, db.TransactionAnnotationStateParams{UserID: userID, Ids: ids})
		if err != nil {
			return err
		}
		for _, r := range rows {
			now[r.ID] = r
		}
	}
	changed := 0
	for _, it := range items {
		switch it.entity {
		case ledger.AIEntityTransaction:
			if created(it) {
				same, err := s.sameManual(ctx, q, userID, it)
				if err != nil {
					return err
				}
				if !same {
					changed++
				}
				continue
			}
			var after ledger.AnnotationState
			if err := json.Unmarshal(it.after, &after); err != nil {
				return fmt.Errorf("ai change item: %w", err)
			}
			cur, ok := now[it.id]
			if !ok || !sameAnnotation(after, cur) {
				changed++
			}
		case ledger.AIEntitySharedExpense:
			var after sharing.SplitState
			if err := json.Unmarshal(it.after, &after); err != nil {
				return fmt.Errorf("ai change item: %w", err)
			}
			cur, err := sharing.CurrentSplit(ctx, q, it.id)
			if err != nil {
				return err
			}
			if cur != nil && !cur.Same(after) {
				changed++
			}
		case ledger.AIEntityBudget:
			var after ledger.BudgetState
			if err := json.Unmarshal(it.after, &after); err != nil {
				return fmt.Errorf("ai change item: %w", err)
			}
			cur, err := q.BudgetAmountForUpdate(ctx, db.BudgetAmountForUpdateParams{UserID: userID, CategoryID: it.id})
			if errors.Is(err, pgx.ErrNoRows) {
				if after.AmountMinor != nil {
					changed++
				}
				continue
			} else if err != nil {
				return err
			}
			if after.AmountMinor == nil || *after.AmountMinor != cur {
				changed++
			}
		case ledger.AIEntityGoal:
			var after goals.State
			if err := json.Unmarshal(it.after, &after); err != nil {
				return fmt.Errorf("ai change item: %w", err)
			}
			cur, err := goals.CurrentState(ctx, q, userID, it.id)
			if err != nil {
				return err
			}
			if cur != nil && !cur.Same(after) { // a goal deleted since has nothing to undo
				changed++
			}
		case ledger.AIEntityRule:
			var after rules.RuleState
			if err := json.Unmarshal(it.after, &after); err != nil {
				return fmt.Errorf("ai change item: %w", err)
			}
			r, err := q.GetRule(ctx, db.GetRuleParams{UserID: userID, ID: it.id})
			if errors.Is(err, pgx.ErrNoRows) {
				continue // deleted since: nothing to undo
			} else if err != nil {
				return err
			}
			if !rules.StateOf(r).Same(after) {
				changed++
			}
		}
	}
	if changed > 0 {
		return fmt.Errorf("%d of the changed items are different now: %w", changed, platform.ErrChangedSince)
	}
	return nil
}

// created is true for a row the change added (add_expense): its "after" is a ManualState.
func created(it item) bool {
	var c struct {
		Created bool `json:"created"`
	}
	return it.before == nil && json.Unmarshal(it.after, &c) == nil && c.Created
}

// sameManual is true while a row the change added still exists with the typed values it got.
func (s *Service) sameManual(ctx context.Context, q *db.Queries, userID uuid.UUID, it item) (bool, error) {
	var after ledger.ManualState
	if err := json.Unmarshal(it.after, &after); err != nil {
		return false, fmt.Errorf("ai change item: %w", err)
	}
	t, err := q.GetTransaction(ctx, db.GetTransactionParams{UserID: userID, ID: it.id})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil // deleted since: nothing to undo
	} else if err != nil {
		return false, err
	}
	return t.Origin == "manual" && t.BookedOn.String() == after.BookedOn && t.AmountMinor == after.AmountMinor && t.Payee == after.Payee, nil
}

func sameAnnotation(after ledger.AnnotationState, cur db.TransactionAnnotationStateRow) bool {
	if after.HasCategory && (!sameID(after.CategoryID, cur.CategoryID) || after.CategorySource != cur.CategorySource) {
		return false
	}
	if after.HasGroup && !sameID(after.GroupID, cur.GroupID) {
		return false
	}
	return true
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// undo puts the "before" back: transactions first (by the value each goes back to), then the
// rules the change created, then its groups and categories when nothing uses them any more.
func (s *Service) undo(ctx context.Context, q *db.Queries, userID uuid.UUID, items []item) (Result, error) {
	var res Result
	categoryTo := map[string][]uuid.UUID{} // "" = back to the bank's (and rules)
	categoryOf := map[string]*uuid.UUID{}
	groupTo := map[string][]uuid.UUID{}
	groupOf := map[string]*uuid.UUID{}
	for _, it := range items {
		if it.entity != ledger.AIEntityTransaction || created(it) {
			continue
		}
		var before ledger.AnnotationState
		if err := json.Unmarshal(it.before, &before); err != nil {
			return res, fmt.Errorf("ai change item: %w", err)
		}
		if before.HasCategory {
			var to *uuid.UUID
			if before.CategorySource == "manual" {
				to = before.CategoryID
			}
			k := key(to)
			categoryTo[k], categoryOf[k] = append(categoryTo[k], it.id), to
		}
		if before.HasGroup {
			k := key(before.GroupID)
			groupTo[k], groupOf[k] = append(groupTo[k], it.id), before.GroupID
		}
		res.Rows++
	}
	for k, ids := range categoryTo {
		to := categoryOf[k]
		if to == nil {
			res.Reclassify = true
		} else if !s.exists(ctx, q, userID, ledger.AIEntityCategory, *to) {
			to, res.Reclassify = nil, true // the category it had is gone: back to the bank's
		}
		if _, err := s.led.AnnotateTx(ctx, q, userID, ids, ledger.AnnotationPatch{SetCategory: true, CategoryID: to}); err != nil {
			return res, err
		}
	}
	for k, ids := range groupTo {
		to := groupOf[k]
		if to != nil && !s.exists(ctx, q, userID, ledger.AIEntityGroup, *to) {
			to = nil
		}
		if _, err := s.led.AnnotateTx(ctx, q, userID, ids, ledger.AnnotationPatch{SetGroup: true, GroupID: to}); err != nil {
			return res, err
		}
	}
	for _, it := range items {
		switch {
		case it.entity == ledger.AIEntityTransaction && created(it):
			if _, err := s.led.DeleteManualTx(ctx, q, userID, it.id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return res, err
			}
			res.Rows++
			res.Reclassify = true // transfers pair again without it
		case it.entity == ledger.AIEntitySharedExpense:
			var before sharing.SplitState
			if err := json.Unmarshal(it.before, &before); err != nil {
				return res, fmt.Errorf("ai change item: %w", err)
			}
			cur, err := sharing.CurrentSplit(ctx, q, it.id)
			if err != nil {
				return res, err
			}
			if cur != nil { // the expense is still there
				if err := sharing.RestoreSplitTx(ctx, q, s.clock.Now(), userID, it.id, before); err != nil {
					return res, err
				}
			}
		case it.entity == ledger.AIEntityGoal:
			var before *goals.State
			if !created(it) {
				before = &goals.State{}
				if err := json.Unmarshal(it.before, before); err != nil {
					return res, fmt.Errorf("ai change item: %w", err)
				}
			}
			cur, err := goals.CurrentState(ctx, q, userID, it.id)
			if err != nil {
				return res, err
			}
			if cur != nil {
				if err := goals.RestoreTx(ctx, q, s.clock.Now(), userID, it.id, before); err != nil {
					return res, err
				}
				if before == nil {
					res.Deleted++
				}
			}
		case it.entity == ledger.AIEntityBudget:
			var before ledger.BudgetState
			if err := json.Unmarshal(it.before, &before); err != nil {
				return res, fmt.Errorf("ai change item: %w", err)
			}
			if err := s.led.BudgetTx(ctx, q, userID, it.id, before.AmountMinor); err != nil {
				return res, err
			}
		}
	}
	for _, it := range items {
		if it.entity != ledger.AIEntityRule {
			continue
		}
		n, err := q.DeleteRule(ctx, db.DeleteRuleParams{UserID: userID, ID: it.id})
		if err != nil {
			return res, err
		}
		if n > 0 {
			res.Deleted++
			res.Reclassify = true
		}
	}
	for _, it := range items {
		if it.entity != ledger.AIEntityGroup && it.entity != ledger.AIEntityCategory {
			continue
		}
		deleted, err := s.deleteIfUnused(ctx, q, userID, it.entity, it.id)
		if err != nil {
			return res, err
		}
		if deleted {
			res.Deleted++
		} else if s.exists(ctx, q, userID, it.entity, it.id) {
			res.Kept++
		}
	}
	if res.Deleted > 0 {
		if _, err := q.DeleteEmptyAnnotations(ctx, userID); err != nil {
			return res, err
		}
	}
	return res, nil
}

func key(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (s *Service) exists(ctx context.Context, q *db.Queries, userID uuid.UUID, entity string, id uuid.UUID) bool {
	var err error
	if entity == ledger.AIEntityGroup {
		_, err = q.GetGroup(ctx, db.GetGroupParams{UserID: userID, ID: id})
	} else {
		_, err = q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: id})
	}
	return err == nil
}

// deleteIfUnused deletes a group or category the change created once nothing holds it: what the
// user put into it since stays put, and so does it.
func (s *Service) deleteIfUnused(ctx context.Context, q *db.Queries, userID uuid.UUID, entity string, id uuid.UUID) (bool, error) {
	var n int64
	var err error
	if entity == ledger.AIEntityGroup {
		n, err = q.CountGroupUse(ctx, db.CountGroupUseParams{UserID: userID, ID: id})
	} else {
		n, err = q.CountCategoryUse(ctx, db.CountCategoryUseParams{UserID: userID, ID: id})
	}
	if err != nil || n > 0 {
		return false, err
	}
	if entity == ledger.AIEntityGroup {
		n, err = q.DeleteGroup(ctx, db.DeleteGroupParams{UserID: userID, ID: id})
	} else {
		n, err = q.DeleteCategory(ctx, db.DeleteCategoryParams{UserID: userID, ID: id})
	}
	return n > 0, err
}

// ExportChange is a change with what it touched, for the data export.
type ExportChange struct {
	Change
	Touched []ExportItem `json:"touched"`
}

// ExportItem is one entity of an exported change.
type ExportItem struct {
	Entity string          `json:"entity"`
	ID     uuid.UUID       `json:"id"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Export returns every change of the user with its items, oldest first.
func (s *Service) Export(ctx context.Context, userID uuid.UUID) ([]ExportChange, error) {
	out := []ExportChange{}
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ExportAIChanges(ctx, userID)
		if err != nil {
			return err
		}
		for i := range rows {
			r := &rows[i]
			if len(out) == 0 || out[len(out)-1].ID != r.ID {
				out = append(out, ExportChange{Change: Change{ID: r.ID, ChatID: r.ChatID, ProposalKey: r.ProposalKey, Action: r.Action,
					Auto: r.Auto, AppliedAt: r.AppliedAt, RevertedAt: r.RevertedAt}})
			}
			last := &out[len(out)-1]
			last.Touched = append(last.Touched, ExportItem{Entity: r.Entity, ID: r.EntityID, Before: r.Before, After: r.After})
			last.Items = len(last.Touched)
		}
		return nil
	})
	return out, err
}
