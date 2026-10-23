package query

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Insight is a resolved context inside one read-only transaction, with the aggregation
// statements the insight charts read (PLAN §4.6). Every statement reuses the compiled context:
// "base" is filters and search, "scoped" adds the period. The rules on top of these numbers
// (days, deltas, cumulative arrays, ranges) live in internal/insights.
type Insight struct {
	ctx context.Context
	tx  pgx.Tx
	r   *resolved
	// Period is the context's period resolved against the data (nil: all time).
	Period *Resolved
}

// Insight resolves c and runs fn with the aggregation statements, in a read-only transaction.
func (e *Engine) Insight(ctx context.Context, userID uuid.UUID, c Context, fn func(*Insight) error) error {
	if _, err := Compile(userID, c); err != nil {
		return err
	}
	return e.st.WithUserReadOnly(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		r, err := resolve(ctx, tx, userID, c)
		if err != nil {
			return err
		}
		return fn(&Insight{ctx: ctx, tx: tx, r: r, Period: r.period})
	})
}

// DateRange is the first and last booking date of a set of rows (nil when there are none).
type DateRange struct{ Min, Max *platform.Date }

// DataRange is the booking-date range of the context's accounts, ignoring filters and period (the
// legacy dataRange of the date column: what "the data" covers).
func (in *Insight) DataRange() (DateRange, error) {
	c, err := Compile(in.r.userID, in.r.ctx)
	if err != nil {
		return DateRange{}, err
	}
	var lo, hi pgtype.Date
	err = in.tx.QueryRow(in.ctx, "SELECT min(t.booked_on), max(t.booked_on) FROM transactions t WHERE "+where(c.Scope),
		c.Args[:c.ScopeN]...).Scan(&lo, &hi)
	return DateRange{dateOf(lo), dateOf(hi)}, err
}

// Currency is the most common currency among the context's accounts (amounts are summed in minor
// units as they are; the legacy app had one currency per dataset). Empty when there are no accounts.
func (in *Insight) Currency() (string, error) {
	c, err := Compile(in.r.userID, in.r.ctx)
	if err != nil {
		return "", err
	}
	scope := strings.NewReplacer("t.user_id", "a.user_id", "t.account_id", "a.id").Replace(where(c.Scope))
	var cur string
	err = in.tx.QueryRow(in.ctx, "SELECT a.currency FROM accounts a WHERE "+scope+
		" GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 1", c.Args[:c.ScopeN]...).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(cur), err
}

// Totals are counts and sums of a set of rows. Spent is the sum of −amount over expenses.
type Totals struct {
	Count       int64
	SpentMinor  int64
	IncomeMinor int64
	Range       DateRange
}

// Totals sums the scoped rows (scoped) or the base rows.
func (in *Insight) Totals(scoped bool) (Totals, error) {
	c, w, err := in.r.fresh(scoped)
	if err != nil {
		return Totals{}, err
	}
	var t Totals
	var lo, hi pgtype.Date
	err = in.tx.QueryRow(in.ctx, "SELECT count(*), coalesce(sum(-t.amount_minor) FILTER (WHERE t.amount_minor < 0), 0)::bigint,"+
		" coalesce(sum(t.amount_minor) FILTER (WHERE t.amount_minor > 0), 0)::bigint, min(t.booked_on), max(t.booked_on) FROM "+
		source(c.joins()...)+" WHERE "+w, c.Args...).Scan(&t.Count, &t.SpentMinor, &t.IncomeMinor, &lo, &hi)
	t.Range = DateRange{dateOf(lo), dateOf(hi)}
	return t, err
}

// Expense is one transaction as the summary names it.
type Expense struct {
	ID          uuid.UUID
	BookedOn    platform.Date
	AmountMinor int64
	Payee       string // the payee group (legacy payeeName)
}

// Largest is the largest expense of the scoped rows; ties go to the first row in the table's
// default order (newest first). nil when there are no expenses.
func (in *Insight) Largest() (*Expense, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return nil, err
	}
	var x Expense
	var d pgtype.Date
	err = in.tx.QueryRow(in.ctx, "SELECT t.id, t.booked_on, t.amount_minor, t.payee_group FROM "+source(c.joins()...)+
		" WHERE "+w+" AND t.amount_minor < 0 ORDER BY t.amount_minor, "+tieBreak+" LIMIT 1", c.Args...).
		Scan(&x.ID, &d, &x.AmountMinor, &x.Payee)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	x.BookedOn = *dateOf(d)
	return &x, nil
}

// SpentBetween is the spending of the base rows booked in [from, to).
func (in *Insight) SpentBetween(from, to platform.Date) (int64, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return 0, err
	}
	var s int64
	err = in.tx.QueryRow(in.ctx, "SELECT coalesce(sum(-t.amount_minor), 0)::bigint FROM "+source(c.joins()...)+" WHERE "+w+
		" AND t.amount_minor < 0 AND t.booked_on >= "+c.Add(from.String())+"::date AND t.booked_on < "+c.Add(to.String())+"::date",
		c.Args...).Scan(&s)
	return s, err
}

// MonthFlow is one month's money in and out (minor units, both ≥ 0) and its number of rows.
type MonthFlow struct {
	Month        platform.Date // the first day of the month
	IncomeMinor  int64
	ExpenseMinor int64
	Count        int64
}

// Months aggregates the base rows per month, oldest first; months without rows are left out.
func (in *Insight) Months() ([]MonthFlow, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT date_trunc('month', t.booked_on::timestamp)::date AS m,"+
		" coalesce(sum(t.amount_minor) FILTER (WHERE t.amount_minor > 0), 0)::bigint,"+
		" coalesce(sum(-t.amount_minor) FILTER (WHERE t.amount_minor < 0), 0)::bigint, count(*) FROM "+
		source(c.joins()...)+" WHERE "+w+" GROUP BY 1 ORDER BY 1", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MonthFlow, error) {
		var m MonthFlow
		var d pgtype.Date
		err := r.Scan(&d, &m.IncomeMinor, &m.ExpenseMinor, &m.Count)
		if err == nil {
			m.Month = *dateOf(d)
		}
		return m, err
	})
}

// DaySpend is one day's spending (minor units, ≥ 0) and its number of expenses.
type DaySpend struct {
	Date       platform.Date
	SpentMinor int64
	Count      int64
}

// DailySpend aggregates the expenses of the scoped (or base) rows booked in [from, to) per day,
// oldest first; days without expenses are left out.
func (in *Insight) DailySpend(scoped bool, from, to platform.Date) ([]DaySpend, error) {
	c, w, err := in.r.fresh(scoped)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT t.booked_on, sum(-t.amount_minor)::bigint, count(*) FROM "+source(c.joins()...)+
		" WHERE "+w+" AND t.amount_minor < 0 AND t.booked_on >= "+c.Add(from.String())+"::date AND t.booked_on < "+
		c.Add(to.String())+"::date GROUP BY 1 ORDER BY 1", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DaySpend, error) {
		var x DaySpend
		var d pgtype.Date
		err := r.Scan(&d, &x.SpentMinor, &x.Count)
		if err == nil {
			x.Date = *dateOf(d)
		}
		return x, err
	})
}

// CategorySpend is one category's spending in the scoped rows. ID and Name are nil for
// uncategorised rows. ColorSlot is the category's colour (1–7) by all-time spending over the
// context's accounts, the same ranking as Engine.Categories, so colours never follow the period.
type CategorySpend struct {
	ID         *uuid.UUID
	Name       *string
	SpentMinor int64
	ColorSlot  *int
}

// CategorySpend lists the scoped rows' spending per category, largest first (ties by name).
func (in *Insight) CategorySpend() ([]CategorySpend, error) {
	c, w, err := in.r.fresh(true)
	if err != nil {
		return nil, err
	}
	scope := where(c.Scope)
	rows, err := in.tx.Query(in.ctx, `WITH ever AS (
	SELECT t.category_id, sum(-t.amount_minor) AS s FROM transactions t WHERE `+scope+` AND t.amount_minor < 0 GROUP BY 1
), ranked AS (
	SELECT al.category_id, row_number() OVER (ORDER BY al.s DESC, c.name COLLATE pl_natural NULLS FIRST) AS r
	FROM ever al LEFT JOIN categories c ON c.id = al.category_id
), spend AS (
	SELECT t.category_id, sum(-t.amount_minor)::bigint AS s FROM `+source(c.joins()...)+` WHERE `+w+` AND t.amount_minor < 0 GROUP BY 1
)
SELECT sp.category_id, c.name, sp.s, CASE WHEN rk.r <= `+strconv.Itoa(Slots)+` THEN rk.r END
FROM spend sp LEFT JOIN categories c ON c.id = sp.category_id
LEFT JOIN ranked rk ON rk.category_id IS NOT DISTINCT FROM sp.category_id
ORDER BY sp.s DESC, c.name COLLATE pl_natural NULLS FIRST`, c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CategorySpend, error) {
		var x CategorySpend
		var slot *int64
		err := r.Scan(&x.ID, &x.Name, &x.SpentMinor, &slot)
		if slot != nil {
			s := int(*slot)
			x.ColorSlot = &s
		}
		return x, err
	})
}

// PayeeSpend is one payee group's spending and number of expenses.
type PayeeSpend struct {
	Payee      string
	SpentMinor int64
	Count      int64
}

// PayeeSpend lists spending per payee group (legacy payeeName) over the scoped (or base) rows,
// largest first, at most top entries (0: all), and the number of payee groups in total.
func (in *Insight) PayeeSpend(scoped bool, top int) ([]PayeeSpend, int, error) {
	c, w, err := in.r.fresh(scoped)
	if err != nil {
		return nil, 0, err
	}
	limit := ""
	if top > 0 {
		limit = " LIMIT " + c.Add(top)
	}
	rows, err := in.tx.Query(in.ctx, "SELECT t.payee_group, sum(-t.amount_minor)::bigint, count(*), count(*) OVER () FROM "+
		source(c.joins()...)+" WHERE "+w+" AND t.amount_minor < 0 GROUP BY 1 ORDER BY 2 DESC, t.payee_group COLLATE pl_natural"+limit, c.Args...)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PayeeSpend, error) {
		var x PayeeSpend
		err := r.Scan(&x.Payee, &x.SpentMinor, &x.Count, &total)
		return x, err
	})
	return out, total, err
}
