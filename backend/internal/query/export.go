package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Export streams the ordered, period-scoped rows of c as display text, one call of row per
// transaction, in the order of cols (PLAN task 7.9, ADR 0014). It runs one query whose rows pgx
// reads from the server as they arrive, so memory stays flat however many rows there are; the
// read transaction stays open until the last row is written.
func (e *Engine) Export(ctx context.Context, userID uuid.UUID, c Context, s *Sort, cols []string, row func([]string) error) error {
	ve := &platform.ValidationError{}
	exprs := make([]string, len(cols))
	for i, name := range cols {
		col, ok := columns[name]
		if !ok {
			ve.Add(fmt.Sprintf("columns[%d]", i), fmt.Sprintf("unknown column %q", name))
			continue
		}
		exprs[i] = "coalesce(" + col.Text + ", '')"
	}
	if len(cols) == 0 {
		ve.Add("columns", "choose at least one column")
	}
	if err := ve.Err(); err != nil {
		return err
	}
	order, err := orderBy(s)
	if err != nil {
		return err
	}
	return e.st.WithUserReadOnly(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, c)
		if err != nil {
			return err
		}
		cc, scoped, err := r.fresh(true)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+strings.Join(exprs, ", ")+" FROM "+from+" WHERE "+scoped+" ORDER BY "+order.sql, cc.Args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			if err := row(vals); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// GuardCell defuses CSV/formula injection (PLAN §8.1): a cell that starts with =, +, -, @, tab or
// CR and is not a number gets a leading apostrophe, so spreadsheets show it as text. Amounts like
// "-79,99" stay numbers.
func GuardCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		if _, ok := ingest.ParseNumber(s); !ok {
			return "'" + s
		}
	}
	return s
}

// CSVLine joins cells with delim, quoting a cell that contains the delimiter, a quote or a line
// break (the legacy export's rule), after GuardCell. No line ending is added.
func CSVLine(cells []string, delim string) string {
	var b strings.Builder
	for i, c := range cells {
		if i > 0 {
			b.WriteString(delim)
		}
		c = GuardCell(c)
		if strings.Contains(c, delim) || strings.ContainsAny(c, "\"\r\n") {
			c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
		}
		b.WriteString(c)
	}
	return b.String()
}

// CategoryStat is a category with its transaction count and colour slot over a set of accounts.
type CategoryStat struct {
	ID        uuid.UUID
	Name      string
	TxCount   int
	ColorSlot *int // 1–7 for the top spenders, nil otherwise
}

// Slots is the number of category colours (PLAN §4.6).
const Slots = 7

// Categories lists the user's categories with counts over accountIDs (all when empty) and colour
// slots ranked by all-time spending over those accounts, ignoring filters and period (PLAN §4.6,
// legacy catColorMap). Uncategorised spending takes part in the ranking, as in the legacy, so it
// can hold a slot that no category gets.
func (e *Engine) Categories(ctx context.Context, userID uuid.UUID, accountIDs []uuid.UUID) ([]CategoryStat, error) {
	comp, err := Compile(userID, Context{AccountIDs: accountIDs})
	if err != nil {
		return nil, err
	}
	scope := where(comp.Scope)
	var out []CategoryStat
	err = e.st.WithUser(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH spend AS (
	SELECT t.category_id, sum(-t.amount_minor) AS s FROM transactions t WHERE `+scope+` AND t.amount_minor < 0 GROUP BY 1
), ranked AS (
	SELECT sp.category_id, row_number() OVER (ORDER BY sp.s DESC, c.name COLLATE pl_natural NULLS FIRST) AS r
	FROM spend sp LEFT JOIN categories c ON c.id = sp.category_id
), counts AS (
	SELECT t.category_id, count(*) AS n FROM transactions t WHERE `+scope+` AND t.category_id IS NOT NULL GROUP BY 1
)
SELECT c.id, c.name, coalesce(n.n, 0), CASE WHEN rk.r <= `+fmt.Sprint(Slots)+` THEN rk.r END
FROM categories c LEFT JOIN counts n ON n.category_id = c.id LEFT JOIN ranked rk ON rk.category_id = c.id
WHERE c.user_id = $1 ORDER BY c.name COLLATE pl_natural, c.id`, comp.Args[:comp.ScopeN]...)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (CategoryStat, error) {
			var c CategoryStat
			var slot *int64
			err := r.Scan(&c.ID, &c.Name, &c.TxCount, &slot)
			if slot != nil {
				s := int(*slot)
				c.ColorSlot = &s
			}
			return c, err
		})
		return err
	})
	return out, err
}
