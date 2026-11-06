package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Limits on rules (PLAN §6.1 "every list is bounded").
const (
	MaxRules      = 200
	MaxFilters    = 20
	maxName       = 80
	maxSearch     = 200
	PreviewSample = 10
)

// Match selects transactions: all of the search's tokens and every column filter, as in the table.
type Match struct {
	Search  string         `json:"search,omitempty"`
	Filters []query.Filter `json:"filters,omitempty"`
}

// matchDoc is the stored JSON of a Match (query.Filter has no JSON tags).
type matchDoc struct {
	Search  string      `json:"search,omitempty"`
	Filters []filterDoc `json:"filters,omitempty"`
}

type filterDoc struct {
	Column   string   `json:"column"`
	Cond     string   `json:"cond,omitempty"`
	Excluded []string `json:"excluded,omitempty"`
}

func (m Match) marshal() ([]byte, error) {
	d := matchDoc{Search: m.Search}
	for _, f := range m.Filters {
		d.Filters = append(d.Filters, filterDoc(f))
	}
	return json.Marshal(d)
}

func unmarshalMatch(b []byte) (Match, error) {
	var d matchDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return Match{}, err
	}
	m := Match{Search: d.Search}
	for _, f := range d.Filters {
		m.Filters = append(m.Filters, query.Filter(f))
	}
	return m, nil
}

// Rule is a stored rule. CategoryName is the current name of the category it sets.
type Rule struct {
	ID            uuid.UUID
	Name          string
	Enabled       bool
	Position      int
	Match         Match
	SetCategoryID *uuid.UUID
	CategoryName  *string
	MarkTransfer  bool
	UpdatedAt     time.Time
}

// RuleState is the "after" of a rule an AI change created: a revert deletes it only while it is
// still this rule (nobody edited its name, match, category or switches since).
type RuleState struct {
	Created       bool            `json:"created"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	Match         json.RawMessage `json:"match"`
	SetCategoryID *uuid.UUID      `json:"setCategoryId,omitempty"`
	MarkTransfer  bool            `json:"markTransfer,omitempty"`
}

// StateOf is a stored rule's RuleState.
func StateOf(r db.GetRuleRow) RuleState {
	return RuleState{Created: true, Name: r.Name, Enabled: r.Enabled, Match: r.Match, SetCategoryID: r.SetCategoryID, MarkTransfer: r.MarkTransfer}
}

// Same reports whether two states describe the same rule.
func (s RuleState) Same(o RuleState) bool {
	same := func(a, b *uuid.UUID) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	return s.Name == o.Name && s.Enabled == o.Enabled && bytes.Equal(s.Match, o.Match) && same(s.SetCategoryID, o.SetCategoryID) && s.MarkTransfer == o.MarkTransfer
}

// Input is a rule as the user edits it.
type Input struct {
	Name          string
	Enabled       bool
	Match         Match
	SetCategoryID *uuid.UUID
	MarkTransfer  bool
}

// Enqueue inserts a job in the caller's database transaction (River's InsertTx): every change to
// the rules enqueues a reclassification with it.
type Enqueue func(ctx context.Context, tx pgx.Tx) error

// Service stores rules and reclassifies transactions; it is safe for concurrent use.
type Service struct {
	st    *store.Store
	clock platform.Clock
}

// New returns a Service over st.
func New(st *store.Store, clock platform.Clock) *Service { return &Service{st: st, clock: clock} }

func invalidf(format string, args ...any) error {
	return fmt.Errorf("rule: %s: %w", fmt.Sprintf(format, args...), ledger.ErrInvalid)
}

// validate trims in and checks the domain rules: a name, something to match, something to do, and a
// match the compiler accepts. The category must be the user's (checked in the transaction).
func validate(userID uuid.UUID, in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Match.Search = strings.TrimSpace(in.Match.Search)
	ve := &platform.ValidationError{}
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > maxName {
		ve.Add("name", fmt.Sprintf("must be 1–%d characters", maxName))
	}
	if utf8.RuneCountInString(in.Match.Search) > maxSearch {
		ve.Add("match.search", fmt.Sprintf("must be at most %d characters", maxSearch))
	}
	if len(in.Match.Filters) > MaxFilters {
		ve.Add("match.filters", fmt.Sprintf("at most %d filters", MaxFilters))
	}
	if !matchesSomething(in.Match) {
		ve.Add("match", "a rule needs a search or at least one column filter (it would match every transaction)")
	}
	if in.SetCategoryID == nil && !in.MarkTransfer {
		ve.Add("actions", "set a category, mark as transfer, or both")
	}
	if err := ve.Err(); err != nil {
		return err
	}
	return query.ValidateRuleMatch(userID, in.Match.Search, in.Match.Filters)
}

func matchesSomething(m Match) bool {
	if m.Search != "" {
		return true
	}
	for _, f := range m.Filters {
		if strings.TrimSpace(f.Cond) != "" || len(f.Excluded) > 0 {
			return true
		}
	}
	return false
}

func ruleOf(id uuid.UUID, name string, enabled bool, position int32, match []byte, cat *uuid.UUID, catName *string,
	transfer bool, updated time.Time) (Rule, error) {
	m, err := unmarshalMatch(match)
	return Rule{ID: id, Name: name, Enabled: enabled, Position: int(position), Match: m, SetCategoryID: cat,
		CategoryName: catName, MarkTransfer: transfer, UpdatedAt: updated}, err
}

// List returns the user's rules in position order.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Rule, error) {
	var out []Rule
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListRules(ctx, userID)
		if err != nil {
			return err
		}
		for i := range rows {
			r := &rows[i]
			rule, err := ruleOf(r.ID, r.Name, r.Enabled, r.Position, r.Match, r.SetCategoryID, r.CategoryName, r.MarkTransfer, r.UpdatedAt)
			if err != nil {
				return err
			}
			out = append(out, rule)
		}
		return nil
	})
	return out, ledger.DBError("list rules", err)
}

func get(ctx context.Context, q *db.Queries, userID, id uuid.UUID) (Rule, error) {
	r, err := q.GetRule(ctx, db.GetRuleParams{UserID: userID, ID: id})
	if err != nil {
		return Rule{}, err
	}
	return ruleOf(r.ID, r.Name, r.Enabled, r.Position, r.Match, r.SetCategoryID, r.CategoryName, r.MarkTransfer, r.UpdatedAt)
}

func ownCategory(ctx context.Context, q *db.Queries, userID uuid.UUID, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	if _, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: *id}); err != nil {
		return fmt.Errorf("category %s: %w", *id, err)
	}
	return nil
}

// Create adds a rule at the end and enqueues a reclassification in the same transaction.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input, enqueue Enqueue) (Rule, error) {
	if err := validate(userID, &in); err != nil {
		return Rule{}, err
	}
	match, err := in.Match.marshal()
	if err != nil {
		return Rule{}, err
	}
	var out Rule
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		if n, err := q.CountRules(ctx, userID); err != nil || n >= MaxRules {
			if err == nil {
				err = invalidf("at most %d rules", MaxRules)
			}
			return err
		}
		if err := ownCategory(ctx, q, userID, in.SetCategoryID); err != nil {
			return err
		}
		id := platform.NewID()
		if err := q.CreateRule(ctx, db.CreateRuleParams{ID: id, UserID: userID, Name: in.Name, Enabled: in.Enabled, Match: match,
			SetCategoryID: in.SetCategoryID, MarkTransfer: in.MarkTransfer, Now: s.clock.Now()}); err != nil {
			return err
		}
		if out, err = get(ctx, q, userID, id); err != nil {
			return err
		}
		// a rule from an assistant proposal can be undone while nobody edited it (ADR 0030)
		stored, err := q.GetRule(ctx, db.GetRuleParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if err := ledger.RecordAIChange(ctx, q, s.clock, userID, []ledger.AIItem{{Entity: ledger.AIEntityRule, ID: id,
			After: StateOf(stored)}}); err != nil {
			return err
		}
		return enqueue(ctx, tx)
	})
	return out, ledger.DBError("create rule", err)
}

// Update replaces a rule's fields (not its position) and enqueues a reclassification.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input, enqueue Enqueue) (Rule, error) {
	if err := validate(userID, &in); err != nil {
		return Rule{}, err
	}
	match, err := in.Match.marshal()
	if err != nil {
		return Rule{}, err
	}
	var out Rule
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		if err := ownCategory(ctx, q, userID, in.SetCategoryID); err != nil {
			return err
		}
		n, err := q.UpdateRule(ctx, db.UpdateRuleParams{UserID: userID, ID: id, Name: in.Name, Enabled: in.Enabled, Match: match,
			SetCategoryID: in.SetCategoryID, MarkTransfer: in.MarkTransfer, Now: s.clock.Now()})
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		if out, err = get(ctx, q, userID, id); err != nil {
			return err
		}
		return enqueue(ctx, tx)
	})
	return out, ledger.DBError("update rule", err)
}

// Delete removes a rule and enqueues a reclassification (its categories go back to the bank's,
// or to the next matching rule's).
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID, enqueue Enqueue) error {
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		n, err := q.DeleteRule(ctx, db.DeleteRuleParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		return enqueue(ctx, tx)
	})
	return ledger.DBError("delete rule", err)
}

// Reorder sets the rules' order: ids must be exactly the user's rules, each once.
func (s *Service) Reorder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, enqueue Enqueue) ([]Rule, error) {
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, invalidf("rule %s is listed twice", id)
		}
		seen[id] = true
	}
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		count, err := q.CountRules(ctx, userID)
		if err != nil {
			return err
		}
		n, err := q.SetRulePositions(ctx, db.SetRulePositionsParams{UserID: userID, Ids: ids, Now: s.clock.Now()})
		if err != nil {
			return err
		}
		if n != count || int64(len(ids)) != count {
			return invalidf("list every rule exactly once (%d rules, %d ids)", count, len(ids))
		}
		return enqueue(ctx, tx)
	})
	if err != nil {
		return nil, ledger.DBError("reorder rules", err)
	}
	return s.List(ctx, userID)
}
