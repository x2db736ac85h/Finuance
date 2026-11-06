// Package query compiles the table's query context — accounts, global search, per-column filters,
// period and the assistant's lookup filter — into SQL that every read endpoint shares (PLAN §4.5).
//
// The compiler only ever emits column expressions from the registry (columns.go) and $n
// placeholders: user text always travels as an argument, never inside the SQL string.
package query

import (
	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Context is the QueryContext of the API: what the user is looking at.
type Context struct {
	AccountIDs       []uuid.UUID // empty: every account of the user
	Search           string      // all tokens must appear in search_text
	Filters          []Filter    // AND across columns
	Period           *Period     // nil: all time
	IncludeTransfers bool        // Phase 15: transfers are hidden unless asked for
	GroupID          *uuid.UUID  // Phase 15: only this group's transactions (group report, export)
	ToolFilter       *ToolFilter // the assistant's lookup filter ("✦" chip), PLAN §4.8
	AIAssisted       bool        // Phase 20: only rows an assistant change set ("AI assisted" chip, ADR 0030)
}

// Filter is one column's condition and its unticked values.
type Filter struct {
	Column   string   // API column name (columns.go)
	Cond     string   // filter language (internal/query/dsl)
	Excluded []string // display values unticked in the value checklist
}

// Period selects a booking-date interval. Exactly one form is set: Preset (l30, l3m, l12m, ytd),
// Month ("2025-03"), Year, or From/To (custom, To exclusive).
type Period struct {
	Preset   string
	Month    string
	Year     int
	From, To *platform.Date
}

// ToolFilter is the typed filter of the assistant tools (legacy aiFilter, PLAN §4.8). Zero values
// mean "no constraint".
type ToolFilter struct {
	Query     string         // all tokens in search_text
	Payee     string         // contains, on the normalised payee
	Category  string         // contains, on the normalised category name
	Group     string         // contains, on the normalised group name
	DateFrom  *platform.Date // inclusive
	DateTo    *platform.Date // inclusive
	Type      string         // expense, income or any ("" = any)
	MinAmount *string        // absolute value, major units (decimal text)
	MaxAmount *string        // absolute value, major units (decimal text)
	Label     string         // the chip text; not used for filtering
}

// Without returns a copy of c without the filters on column (distinct values list a column's own
// unticked values, so its own filter must not hide them).
func (c Context) Without(column string) Context {
	out := c
	out.Filters = nil
	for _, f := range c.Filters {
		if f.Column != column {
			out.Filters = append(out.Filters, f)
		}
	}
	return out
}
