package query

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The assistant's lookups (PLAN §4.8, legacy execTool). Each runs on an Insight, inside the
// read-only transaction of one tool call; the context carries the chat's scope plus the tool's
// ToolFilter, and the period when the scope is a table view. The legacy's rules on top of these
// rows (sorting groups, top-N, number formatting) live in internal/assistant.

// fileOrder is the order the legacy walked its rows in (the file's order: newest first, file order
// within a day). Ties in counts and sorts keep this order, as the legacy's stable sorts did.
const fileOrder = "t.booked_on DESC, t.seq ASC"

// ToolRow is one transaction as the assistant's search returns it.
type ToolRow struct {
	ID          uuid.UUID
	BookedOn    platform.Date
	AmountMinor int64
	Currency    string
	Payee       string
	Description string
	Category    string // "" when uncategorised
	TxType      string
	Group       string // "" when in no group
	Account     string
}

// Tool search sorts (legacy execTool): newest first by default; amounts by absolute value.
var toolSorts = map[string]string{
	"date_desc":   fileOrder,
	"date_asc":    "t.booked_on ASC, t.seq ASC",
	"amount_desc": "abs(t.amount_minor) DESC, " + fileOrder,
	"amount_asc":  "abs(t.amount_minor) ASC, " + fileOrder,
}

// ToolRows returns the first limit scoped rows in a tool sort order (an unknown sort is date_desc).
func (in *Insight) ToolRows(sort string, limit int) ([]ToolRow, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return nil, err
	}
	o, ok := toolSorts[sort]
	if !ok {
		o = toolSorts["date_desc"]
	}
	rows, err := in.tx.Query(in.ctx, windowFirst(source(c.joins()...)+" WHERE "+w+" ORDER BY "+o+" LIMIT "+c.Add(limit),
		"t.id, t.booked_on, t.amount_minor, t.currency, t.payee, t.description, coalesce(c.name, ''), t.tx_type, coalesce(g.name, ''), a.name", o),
		c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ToolRow, error) {
		var x ToolRow
		var d pgtype.Date
		err := r.Scan(&x.ID, &d, &x.AmountMinor, &x.Currency, &x.Payee, &x.Description, &x.Category, &x.TxType, &x.Group, &x.Account)
		x.BookedOn = *dateOf(d)
		return x, err
	})
}

// ToolGroup is one group of aggregate_transactions. First is the position of the group's first
// row in file order (the legacy's insertion order, its tie-break).
type ToolGroup struct {
	Key         string
	Count       int64
	SpentMinor  int64
	IncomeMinor int64
	First       int64
}

// toolKeys are the group_by dimensions: the key expression and the join it needs. Week keys are
// the Monday; weekday keys are ISO day numbers (1 = Monday); the caller names them.
var toolKeys = map[string]struct {
	expr string
	join Join
}{
	"category": {"coalesce(c.name, '')", JoinCategories},
	"payee":    {"t.payee_group", ""},
	"month":    {"to_char(t.booked_on, 'YYYY-MM')", ""},
	"week":     {"to_char(date_trunc('week', t.booked_on), 'YYYY-MM-DD')", ""},
	"day":      {"to_char(t.booked_on, 'YYYY-MM-DD')", ""},
	"weekday":  {"extract(isodow FROM t.booked_on)::text", ""},
	"type":     {"CASE WHEN t.amount_minor < 0 THEN 'expense' WHEN t.amount_minor > 0 THEN 'income' ELSE 'zero' END", ""},
	"group":    {"coalesce(g.name, '')", JoinGroups},
	"account":  {"a.name", JoinAccounts},
}

// ToolGroups counts and sums the scoped rows per group_by key; any other dimension is one group
// "all" (legacy). Groups come in file order of their first row.
func (in *Insight) ToolGroups(by string) ([]ToolGroup, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return nil, err
	}
	key, ok := toolKeys[by]
	if !ok {
		key.expr = "'all'"
	}
	rows, err := in.tx.Query(in.ctx, "SELECT k, count(*), coalesce(sum(-m) FILTER (WHERE m < 0), 0)::bigint,"+
		" coalesce(sum(m) FILTER (WHERE m > 0), 0)::bigint, min(o) FROM (SELECT "+key.expr+" AS k, t.amount_minor AS m,"+
		" row_number() OVER (ORDER BY "+fileOrder+") AS o FROM "+source(c.joins(key.join)...)+" WHERE "+w+") s GROUP BY k ORDER BY 5", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ToolGroup, error) {
		var g ToolGroup
		err := r.Scan(&g.Key, &g.Count, &g.SpentMinor, &g.IncomeMinor, &g.First)
		return g, err
	})
}

// Counted is a distinct value with its number of rows.
type Counted struct {
	Value string
	Count int64
}

// valueFields are the find_values fields: the payee group (legacy payeeName), the category name
// ("" when uncategorised) and the group name (rows in a group only).
var valueFields = map[string]struct {
	expr, where string
	join        Join
}{
	"payee":    {"t.payee_group", "", ""},
	"category": {"coalesce(c.name, '')", "", JoinCategories},
	"group":    {"g.name", " AND t.group_id IS NOT NULL", JoinGroups},
}

// ValueCounts counts the scoped rows per value of field, most rows first; ties keep file order.
func (in *Insight) ValueCounts(field string) ([]Counted, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return nil, err
	}
	f, ok := valueFields[field]
	if !ok {
		f = valueFields["payee"]
	}
	rows, err := in.tx.Query(in.ctx, "SELECT v, count(*) FROM (SELECT "+f.expr+" AS v, row_number() OVER (ORDER BY "+fileOrder+") AS o FROM "+
		source(c.joins(f.join)...)+" WHERE "+w+f.where+") s GROUP BY v ORDER BY 2 DESC, min(o)", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Counted, error) {
		var x Counted
		err := r.Scan(&x.Value, &x.Count)
		return x, err
	})
}

// ColumnSample is a column of the scoped rows with its first non-empty display text in file order
// ("" when every row is empty).
type ColumnSample struct {
	Column Column
	Sample string
}

// ColumnSamples returns every column of GridOrder with a sample value, for the system prompt.
func (in *Insight) ColumnSamples() ([]ColumnSample, error) {
	out := make([]ColumnSample, 0, len(GridOrder))
	for _, name := range GridOrder {
		col := columns[name]
		c, w, err := in.r.fresh(true)
		if err != nil {
			return nil, err
		}
		var s pgtype.Text
		err = in.tx.QueryRow(in.ctx, "SELECT "+col.Text+" FROM "+source(c.joins(col.Join)...)+" WHERE "+w+" AND coalesce("+col.Text+
			", '') <> '' ORDER BY "+fileOrder+" LIMIT 1", c.Args...).Scan(&s)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		out = append(out, ColumnSample{Column: col, Sample: s.String})
	}
	return out, nil
}

// GroupNames lists the user's groups in creation order (the legacy listed S.groups).
func (in *Insight) GroupNames() ([]string, error) {
	rows, err := in.tx.Query(in.ctx, "SELECT name FROM groups WHERE user_id = $1 ORDER BY created_at, name", in.r.userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AccountCount is the number of accounts with scoped rows (search results name the account when
// there are several).
func (in *Insight) AccountCount() (int, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return 0, err
	}
	var n int
	err = in.tx.QueryRow(in.ctx, "SELECT count(DISTINCT t.account_id) FROM "+source(c.joins()...)+" WHERE "+w, c.Args...).Scan(&n)
	return n, err
}
