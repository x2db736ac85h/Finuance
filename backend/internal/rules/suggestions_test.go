package rules

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

func TestPlainText(t *testing.T) {
	for s, want := range map[string]bool{
		"ZABKA Z1001 K.1":    true,
		"OPERATOR INTERNETU": true,
		"=ZABKA":             false, // exact match
		"!ZABKA":             false,
		">100":               false,
		"A..B":               false,
		"  padded ":          false, // the condition would be trimmed
		"":                   false,
	} {
		if got := plainText(s); got != want {
			t.Errorf("plainText(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestMatchEqual(t *testing.T) {
	m := SuggestedMatch("ZABKA")
	if !m.equal(Match{Filters: []query.Filter{{Column: "payee", Cond: "ZABKA"}}}) {
		t.Error("the same match")
	}
	for _, o := range []Match{
		{Search: "x", Filters: m.Filters},
		{Filters: []query.Filter{{Column: "description", Cond: "ZABKA"}}},
		{Filters: []query.Filter{{Column: "payee", Cond: "ZABKA", Excluded: []string{"a"}}}},
		{Filters: append(append([]query.Filter{}, m.Filters...), query.Filter{Column: "amount", Cond: "<0"})},
	} {
		if m.equal(o) {
			t.Errorf("%+v equals %+v", m, o)
		}
	}
}
