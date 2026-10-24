package rules

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/query/dsl"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// MaxSuggestions caps the rule suggestions (the toolbar shows one at a time).
const MaxSuggestions = 5

// Suggestion proposes a rule (PLAN 15.10): the user put at least 3 transactions of a payee group
// in one category by hand, so a rule "payee contains <group> → category" would do it for them,
// also on later imports.
type Suggestion struct {
	PayeeGroup   string
	CategoryID   uuid.UUID
	CategoryName string
	ManualCount  int64 // transactions of the group the user put in the category by hand
	TxCount      int64 // every transaction of the group
	Match        Match
}

// SuggestedMatch is the match of a suggestion: the payee contains the payee group.
func SuggestedMatch(payeeGroup string) Match {
	return Match{Filters: []query.Filter{{Column: "payee", Cond: payeeGroup}}}
}

// plainText reports whether a payee group reads as a plain "contains" condition in the filter
// language (not "!…", "=…", "<…", "a..b").
func plainText(s string) bool {
	c, ok := dsl.Parse(s)
	return ok && !c.Neg && c.Kind == dsl.Bare && c.Op == "" && c.Body == s
}

// Suggestions lists rule suggestions, most manual categorisations first. A payee group a rule
// already handles (a rule with the same match and category, enabled or not) isn't suggested;
// the client hides the ones the user dismissed.
func (s *Service) Suggestions(ctx context.Context, userID uuid.UUID) ([]Suggestion, error) {
	var out []Suggestion
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.RuleSuggestions(ctx, userID)
		if err != nil || len(rows) == 0 {
			return err
		}
		existing, err := q.ListRules(ctx, userID)
		if err != nil {
			return err
		}
		have := map[uuid.UUID][]Match{}
		for i := range existing {
			r := &existing[i]
			if r.SetCategoryID == nil {
				continue
			}
			m, err := unmarshalMatch(r.Match)
			if err != nil {
				return err
			}
			have[*r.SetCategoryID] = append(have[*r.SetCategoryID], m)
		}
		for i := range rows {
			r := &rows[i]
			m := SuggestedMatch(r.PayeeGroup)
			if !plainText(r.PayeeGroup) || slices.ContainsFunc(have[r.CategoryID], m.equal) {
				continue
			}
			out = append(out, Suggestion{PayeeGroup: r.PayeeGroup, CategoryID: r.CategoryID, CategoryName: r.CategoryName,
				ManualCount: r.ManualCount, TxCount: r.TxCount, Match: m})
			if len(out) == MaxSuggestions {
				break
			}
		}
		return nil
	})
	return out, ledger.DBError("rule suggestions", err)
}

// equal compares two matches as they are stored.
func (m Match) equal(o Match) bool {
	return m.Search == o.Search && slices.EqualFunc(m.Filters, o.Filters, func(a, b query.Filter) bool {
		return a.Column == b.Column && a.Cond == b.Cond && slices.Equal(a.Excluded, b.Excluded)
	})
}
