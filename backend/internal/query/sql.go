package query

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Engine runs compiled contexts against the database, always inside store.WithUser (RLS). It is
// the only code outside internal/store that runs SQL itself (the store guard allows it).
type Engine struct{ st *store.Store }

// New returns an engine on st.
func New(st *store.Store) *Engine { return &Engine{st: st} }

// Sort orders the rows by one column; nil is the default order (newest first, file order within a day).
type Sort struct {
	Column string
	Desc   bool
}

// Limits of the read endpoints (PLAN §6.2).
const (
	MaxPageRows     = 500
	MaxDistinct     = 600
	MaxIDs          = 5000
	DefaultDistinct = 600
)

// from is the full row source: transactions with their account, category and group.
const from = "transactions t JOIN accounts a ON a.id = t.account_id" +
	" LEFT JOIN categories c ON c.id = t.category_id LEFT JOIN groups g ON g.id = t.group_id"

// source is the row source with only the joins in need (the predicates' and the order's).
func source(need ...Join) string {
	has := func(j Join) bool { return slices.Contains(need, j) }
	s := "transactions t"
	if has(JoinAccounts) {
		s += " JOIN accounts a ON a.id = t.account_id"
	}
	if has(JoinCategories) {
		s += " LEFT JOIN categories c ON c.id = t.category_id"
	}
	if has(JoinGroups) {
		s += " LEFT JOIN groups g ON g.id = t.group_id"
	}
	return s
}

// joins lists the joins a compiled context needs, plus extra.
func (c *Compiled) joins(extra ...Join) []Join {
	out := slices.Collect(maps.Keys(c.Joins))
	return append(out, extra...)
}

// tieBreak is the stable end of every ordering (PLAN §4.5).
const tieBreak = "t.booked_on DESC, t.seq ASC"

// order is an ORDER BY list and the join its column needs.
type order struct {
	sql  string
	join Join
}

func orderBy(s *Sort) (order, error) {
	if s == nil {
		return order{sql: tieBreak}, nil
	}
	col, ok := columns[s.Column]
	if !ok {
		ve := &platform.ValidationError{}
		ve.Add("sort.column", fmt.Sprintf("unknown column %q", s.Column))
		return order{}, ve
	}
	return order{sql: col.sortExpr(s.Desc) + ", " + tieBreak, join: col.Join}, nil
}

func where(preds []string) string { return strings.Join(preds, " AND ") }

// resolved is a validated context with its period resolved against the data. Every statement
// compiles its own copy (fresh), so each gets exactly the arguments its placeholders use.
type resolved struct {
	userID uuid.UUID
	ctx    Context
	period *Resolved
}

// resolve validates c and finds the period anchor (the latest booking date in the context's
// accounts, ignoring filters).
func resolve(ctx context.Context, tx pgx.Tx, userID uuid.UUID, c Context) (*resolved, error) {
	comp, err := Compile(userID, c)
	if err != nil {
		return nil, err
	}
	out := &resolved{userID: userID, ctx: c}
	if c.Period != nil {
		var anchor *platform.Date
		if c.Period.Preset != "" {
			var d pgtype.Date
			if err := tx.QueryRow(ctx, "SELECT max(t.booked_on) FROM transactions t WHERE "+where(comp.Scope),
				comp.Args[:comp.ScopeN]...).Scan(&d); err != nil {
				return nil, err
			}
			anchor = dateOf(d)
		}
		if out.period, err = Resolve(c.Period, anchor); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// fresh compiles the context for one statement. With scoped, the period is part of the WHERE
// clause ("scoped" rows); without, it is "base" (filters and search only).
func (r *resolved) fresh(scoped bool) (*Compiled, string, error) {
	c, err := Compile(r.userID, r.ctx)
	if err != nil {
		return nil, "", err
	}
	preds := c.Where
	if p := r.period; scoped && p != nil {
		preds = append(append([]string{}, preds...),
			"t.booked_on >= "+c.Add(p.From.String())+"::date", "t.booked_on < "+c.Add(p.To.String())+"::date")
	}
	return c, where(preds), nil
}

func dateOf(d pgtype.Date) *platform.Date {
	if !d.Valid {
		return nil
	}
	v := platform.DateOf(d.Time, d.Time.Location())
	return &v
}

// Row is one transaction as the table shows it.
type Row struct {
	ID                  uuid.UUID
	AccountID           uuid.UUID
	AccountName         string
	BookedOn            platform.Date
	ValueOn             *platform.Date
	AmountMinor         int64
	Currency            string
	BalanceAfterMinor   *int64
	Payee               string
	Description         string
	CategoryID          *uuid.UUID
	CategoryName        *string
	CategorySource      string
	BankCategory        string
	TxType              string
	Reference           string
	CounterpartyAccount string
	Note                string
	IsTransfer          bool
	GroupID             *uuid.UUID
	GroupName           *string
}

const rowColumns = "t.id, t.account_id, a.name, t.booked_on, t.value_on, t.amount_minor, t.currency, t.balance_after_minor," +
	" t.payee, t.description, c.id, c.name, t.category_source, t.bank_category, t.tx_type, t.reference," +
	" t.counterparty_account, t.note, t.is_transfer, g.id, g.name"

func scanRow(rows pgx.CollectableRow) (Row, error) {
	var r Row
	var booked, value pgtype.Date
	err := rows.Scan(&r.ID, &r.AccountID, &r.AccountName, &booked, &value, &r.AmountMinor, &r.Currency, &r.BalanceAfterMinor,
		&r.Payee, &r.Description, &r.CategoryID, &r.CategoryName, &r.CategorySource, &r.BankCategory, &r.TxType, &r.Reference,
		&r.CounterpartyAccount, &r.Note, &r.IsTransfer, &r.GroupID, &r.GroupName)
	if err != nil {
		return r, err
	}
	r.BookedOn = *dateOf(booked)
	r.ValueOn = dateOf(value)
	return r, nil
}

// PageRequest asks for one window of rows.
type PageRequest struct {
	Context    Context
	Sort       *Sort
	Offset     int
	Limit      int
	StatColumn string     // amount (default) or balanceAfter
	LocateID   *uuid.UUID // return the window that contains this row
	SkipTotals bool       // only the window: no total, stats, data range or duplicatesSkipped
}

// Stats summarise the stat column over the period-scoped rows (minor units; Avg rounded).
type Stats struct {
	Column             string
	Sum, Avg, Min, Max *int64 // nil when no row has a value
}

// Page is one window plus the numbers the table's status bar and header need.
type Page struct {
	Rows              []Row
	Total             int // period-scoped rows
	Offset            int
	Period            *Resolved
	DataMin, DataMax  *platform.Date // booking-date range of the filtered rows ignoring the period ("base")
	Stats             Stats
	DuplicatesSkipped int64
	LocatedOffset     *int // the position of LocateID in the ordered result, when found
	TotalsIncluded    bool // false when the request skipped them
}

// pageSQL builds the totals and rows statements of a page request (shared by Page and Explain).
// The totals read only the tables the predicates need; the rows statement finds the window's ids
// first (cheap, often index-only) and joins the display columns for those rows only.
func pageSQL(r *resolved, stat string, o order, offset, limit int) (totals, rows string, totalsArgs, rowsArgs []any, err error) {
	c, base, err := r.fresh(false)
	if err != nil {
		return "", "", nil, nil, err
	}
	statExpr := map[string]string{"amount": "t.amount_minor", "balanceAfter": "t.balance_after_minor"}[stat]
	inPeriod := "true"
	if p := r.period; p != nil {
		inPeriod = "t.booked_on >= " + c.Add(p.From.String()) + "::date AND t.booked_on < " + c.Add(p.To.String()) + "::date"
	}
	totals = "SELECT count(*) FILTER (WHERE " + inPeriod + ")," +
		" sum(" + statExpr + ") FILTER (WHERE " + inPeriod + ")::bigint, round(avg(" + statExpr + ") FILTER (WHERE " + inPeriod + "))::bigint," +
		" min(" + statExpr + ") FILTER (WHERE " + inPeriod + "), max(" + statExpr + ") FILTER (WHERE " + inPeriod + ")," +
		" min(t.booked_on), max(t.booked_on) FROM " + source(c.joins()...) + " WHERE " + base
	rc, scoped, err := r.fresh(true)
	if err != nil {
		return "", "", nil, nil, err
	}
	rows = windowFirst(source(rc.joins(o.join)...)+" WHERE "+scoped+" ORDER BY "+o.sql+
		" OFFSET "+rc.Add(offset)+" LIMIT "+rc.Add(limit), rowColumns, o.sql)
	return totals, rows, c.Args, rc.Args, nil
}

// windowFirst is the late row lookup: the window's ids (window is "<source> WHERE … ORDER BY …
// LIMIT …") are found once, in a materialised CTE, and only those rows are joined to the display
// columns. Written as "t.id IN (SELECT … LIMIT …)", the planner may run the window once per row
// when its statistics predate the user (estimate: 1 row), which made a 917-row account's filtered
// window take seconds until autoanalyze caught up (found by the Phase 14 E2E on a fresh database).
func windowFirst(window, columns, orderBy string) string {
	return "WITH w AS MATERIALIZED (SELECT t.id FROM " + window + ") SELECT " + columns + " FROM " + from +
		" WHERE t.id IN (SELECT id FROM w) ORDER BY " + orderBy
}

// Explain runs EXPLAIN (ANALYZE, BUFFERS) on the statements of a page request, read-only, and
// returns the plans by name: totals, rows, duplicates (PLAN task 7.11; docs/perf.md, debugging).
func (e *Engine) Explain(ctx context.Context, userID uuid.UUID, req PageRequest) (map[string]string, error) {
	order, err := orderBy(req.Sort)
	if err != nil {
		return nil, err
	}
	plans := map[string]string{}
	stat := req.StatColumn
	if stat == "" {
		stat = "amount"
	}
	err = e.st.WithUserReadOnly(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, req.Context)
		if err != nil {
			return err
		}
		totals, rows, ta, ra, err := pageSQL(r, stat, order, req.Offset, req.Limit)
		if err != nil {
			return err
		}
		explain := func(sql string, args []any) (string, error) {
			rs, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+sql, args...)
			if err != nil {
				return "", err
			}
			lines, err := pgx.CollectRows(rs, pgx.RowTo[string])
			return strings.Join(lines, "\n"), err
		}
		dc, _, err := r.fresh(false)
		if err != nil {
			return err
		}
		ds, da := duplicatesSQL(dc)
		for name, q := range map[string]struct {
			sql  string
			args []any
		}{"totals": {totals, ta}, "rows": {rows, ra}, "duplicates": {ds, da}} {
			if plans[name], err = explain(q.sql, q.args); err != nil {
				return err
			}
		}
		return nil
	})
	return plans, err
}

// Page returns a window of rows with the totals (PLAN task 7.5).
func (e *Engine) Page(ctx context.Context, userID uuid.UUID, req PageRequest) (Page, error) {
	ve := &platform.ValidationError{}
	if req.Limit < 1 || req.Limit > MaxPageRows {
		ve.Add("limit", "must be 1–"+strconv.Itoa(MaxPageRows))
	}
	if req.Offset < 0 {
		ve.Add("offset", "must not be negative")
	}
	stat := req.StatColumn
	switch stat {
	case "":
		stat = "amount"
	case "amount", "balanceAfter":
	default:
		ve.Add("statColumn", "must be amount or balanceAfter")
	}
	if err := ve.Err(); err != nil {
		return Page{}, err
	}
	order, err := orderBy(req.Sort)
	if err != nil {
		return Page{}, err
	}
	out := Page{Offset: req.Offset, Stats: Stats{Column: stat}}
	err = e.st.WithUser(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, req.Context)
		if err != nil {
			return err
		}
		out.Period = r.period

		// Totals: one pass over "base" (filters, no period) computes the data range, and the
		// period-scoped count and stats with FILTER.
		totals, rowsSQL, ta, ra, err := pageSQL(r, stat, order, req.Offset, req.Limit)
		if err != nil {
			return err
		}
		if !req.SkipTotals {
			var dmin, dmax pgtype.Date
			var sum, avg, mn, mx pgtype.Int8
			if err := tx.QueryRow(ctx, totals, ta...).Scan(&out.Total, &sum, &avg, &mn, &mx, &dmin, &dmax); err != nil {
				return err
			}
			out.Stats.Sum, out.Stats.Avg, out.Stats.Min, out.Stats.Max = int8p(sum), int8p(avg), int8p(mn), int8p(mx)
			out.DataMin, out.DataMax = dateOf(dmin), dateOf(dmax)

			dc, _, err := r.fresh(false)
			if err != nil {
				return err
			}
			if out.DuplicatesSkipped, err = duplicatesSkipped(ctx, tx, dc); err != nil {
				return err
			}
			out.TotalsIncluded = true
		}

		if req.LocateID != nil {
			c, scoped, err := r.fresh(true)
			if err != nil {
				return err
			}
			var n int
			err = tx.QueryRow(ctx, "SELECT n FROM (SELECT t.id, row_number() OVER (ORDER BY "+order.sql+") - 1 AS n FROM "+
				source(c.joins(order.join)...)+" WHERE "+scoped+") x WHERE x.id = "+c.Add(*req.LocateID), c.Args...).Scan(&n)
			switch {
			case errors.Is(err, pgx.ErrNoRows): // not in the result: the requested window as is
			case err != nil:
				return err
			default:
				out.LocatedOffset = &n
				out.Offset = n / req.Limit * req.Limit
			}
		}

		if out.Offset != req.Offset { // moved to the located row's window
			if _, rowsSQL, _, ra, err = pageSQL(r, stat, order, out.Offset, req.Limit); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, rowsSQL, ra...)
		if err != nil {
			return err
		}
		out.Rows, err = pgx.CollectRows(rows, scanRow)
		return err
	})
	return out, err
}

func int8p(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// duplicatesSkipped counts, for the context's accounts and ignoring filters, the staged rows that
// were merged into an existing transaction (linked rows − import-origin transactions).
func duplicatesSkipped(ctx context.Context, tx pgx.Tx, c *Compiled) (int64, error) {
	sql, args := duplicatesSQL(c)
	var n int64
	err := tx.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// duplicatesSQL: every import-origin transaction has at least one linked staging row (orphans are
// deleted with their last import), so "linked rows − import-origin transactions" equals "linked
// rows − distinct linked transactions", which import_rows_linked_idx answers without the heap.
func duplicatesSQL(c *Compiled) (string, []any) {
	scope := strings.NewReplacer("t.user_id", "ir.user_id", "t.account_id", "i.account_id").Replace(where(c.Scope))
	return "SELECT count(*) - count(DISTINCT ir.transaction_id) FROM import_rows ir JOIN imports i ON i.id = ir.import_id WHERE " +
		scope + " AND ir.transaction_id IS NOT NULL", c.Args[:c.ScopeN]
}

// Value is one entry of a column's value checklist.
type Value struct {
	Value string
	Count int
}

// Distinct lists a column's display values with counts over the period-scoped rows of the context
// minus that column's own filter, so unticked values stay listed (PLAN task 7.6). search narrows
// the values (normalised substring). Ordered by count, then value.
func (e *Engine) Distinct(ctx context.Context, userID uuid.UUID, c Context, column, search string, limit int) ([]Value, error) {
	col, ok := columns[column]
	ve := &platform.ValidationError{}
	if !ok {
		ve.Add("column", fmt.Sprintf("unknown column %q", column))
	}
	if limit < 1 || limit > MaxDistinct {
		ve.Add("limit", "must be 1–"+strconv.Itoa(MaxDistinct))
	}
	checkText(ve, "search", search)
	if err := ve.Err(); err != nil {
		return nil, err
	}
	var out []Value
	err := e.st.WithUser(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, c.Without(column))
		if err != nil {
			return err
		}
		c, w, err := r.fresh(true)
		if err != nil {
			return err
		}
		if s := ingest.TrimJS(search); s != "" {
			w += " AND coalesce(" + col.Norm + ", '') LIKE " + c.Add(contains(shadow(s)))
		}
		v := "coalesce(" + col.Text + ", '')"
		rows, err := tx.Query(ctx, "SELECT "+v+" AS v, count(*) AS n FROM "+source(c.joins(col.Join)...)+" WHERE "+w+
			" GROUP BY 1 ORDER BY n DESC, "+v+" COLLATE pl_natural LIMIT "+c.Add(limit), c.Args...)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(rw pgx.CollectableRow) (Value, error) {
			var x Value
			return x, rw.Scan(&x.Value, &x.Count)
		})
		return err
	})
	return out, err
}

// IDAmount is a row reference for range selections.
type IDAmount struct {
	ID          uuid.UUID
	AmountMinor int64
	GroupID     *uuid.UUID // the selection's quick bar offers Ungroup when any row has one
}

// IDs returns ids and amounts of a range of the ordered result (range selection across windows
// that aren't loaded, PLAN task 7.12).
func (e *Engine) IDs(ctx context.Context, userID uuid.UUID, c Context, s *Sort, offset, limit int) ([]IDAmount, error) {
	ve := &platform.ValidationError{}
	if limit < 1 || limit > MaxIDs {
		ve.Add("limit", "must be 1–"+strconv.Itoa(MaxIDs))
	}
	if offset < 0 {
		ve.Add("offset", "must not be negative")
	}
	if err := ve.Err(); err != nil {
		return nil, err
	}
	order, err := orderBy(s)
	if err != nil {
		return nil, err
	}
	var out []IDAmount
	err = e.st.WithUser(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, c)
		if err != nil {
			return err
		}
		c, scoped, err := r.fresh(true)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT t.id, t.amount_minor, t.group_id FROM "+source(c.joins(order.join)...)+" WHERE "+scoped+" ORDER BY "+order.sql+
			" OFFSET "+c.Add(offset)+" LIMIT "+c.Add(limit), c.Args...)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(rw pgx.CollectableRow) (IDAmount, error) {
			var x IDAmount
			return x, rw.Scan(&x.ID, &x.AmountMinor, &x.GroupID)
		})
		return err
	})
	return out, err
}

// Validate checks a context and sort without running anything (the export validates before it
// starts streaming, when a problem response is still possible).
func Validate(userID uuid.UUID, c Context, s *Sort) error {
	if _, err := Compile(userID, c); err != nil {
		return err
	}
	if c.Period != nil {
		anchor := platform.MustDate(2000, 1, 1) // any date: only the shape is checked
		if _, err := Resolve(c.Period, &anchor); err != nil {
			return err
		}
	}
	_, err := orderBy(s)
	return err
}
