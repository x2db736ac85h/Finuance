package query

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The overview's aggregation statements (PLAN §4.6 "Overview", legacy ovCtx). The overview reads
// the "base" rows (filters and search, never the period); the balance reads every transaction of
// the context's accounts (legacy: the balance must equal the sum of transactions).

// MonthCategory is one month's spending in one category (ID and Name nil: uncategorised).
type MonthCategory struct {
	Month      platform.Date
	ID         *uuid.UUID
	Name       *string
	SpentMinor int64
}

// MonthCategorySpend is the base rows' spending per month and category for months in [from, to).
func (in *Insight) MonthCategorySpend(from, to platform.Date) ([]MonthCategory, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT x.m, x.category_id, c.name, x.s FROM (SELECT date_trunc('month', t.booked_on::timestamp)::date AS m, t.category_id,"+
		" sum(-t.amount_minor)::bigint AS s FROM "+source(c.joins()...)+" WHERE "+w+" AND t.amount_minor < 0 AND t.booked_on >= "+
		c.Add(from.String())+"::date AND t.booked_on < "+c.Add(to.String())+"::date GROUP BY 1, 2) x LEFT JOIN categories c ON c.id = x.category_id"+
		" ORDER BY 1, 4 DESC, c.name COLLATE pl_natural NULLS FIRST", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MonthCategory, error) {
		var x MonthCategory
		var d pgtype.Date
		err := r.Scan(&d, &x.ID, &x.Name, &x.SpentMinor)
		if err == nil {
			x.Month = *dateOf(d)
		}
		return x, err
	})
}

// CategorySlot is a category's colour slot (1–7) by all-time spending over the context's
// accounts, ignoring filters (the ranking of Engine.Categories, legacy catColorMap).
type CategorySlot struct {
	ID   *uuid.UUID
	Name *string
	Slot int
}

// CategorySlots lists the categories that own a colour.
func (in *Insight) CategorySlots() ([]CategorySlot, error) {
	c, err := Compile(in.r.userID, in.r.ctx)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, `WITH ever AS (
	SELECT t.category_id, sum(-t.amount_minor) AS s FROM transactions t WHERE `+where(c.Scope)+` AND t.amount_minor < 0 GROUP BY 1
), ranked AS (
	SELECT ev.category_id, c.name, row_number() OVER (ORDER BY ev.s DESC, c.name COLLATE pl_natural NULLS FIRST) AS r
	FROM ever ev LEFT JOIN categories c ON c.id = ev.category_id
)
SELECT category_id, name, r FROM ranked WHERE r <= `+strconv.Itoa(Slots)+` ORDER BY r`, c.Args[:c.ScopeN]...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CategorySlot, error) {
		var x CategorySlot
		var slot int64
		err := r.Scan(&x.ID, &x.Name, &slot)
		x.Slot = int(slot)
		return x, err
	})
}

// IncomeSources is the base rows' income per payee group booked in [from, to), largest first.
func (in *Insight) IncomeSources(from, to platform.Date) ([]PayeeSpend, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT t.payee_group, sum(t.amount_minor)::bigint, count(*) FROM "+source(c.joins()...)+" WHERE "+w+
		" AND t.amount_minor > 0 AND t.booked_on >= "+c.Add(from.String())+"::date AND t.booked_on < "+c.Add(to.String())+"::date"+
		" GROUP BY 1 ORDER BY 2 DESC, t.payee_group COLLATE pl_natural", c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PayeeSpend, error) {
		var x PayeeSpend
		err := r.Scan(&x.Payee, &x.SpentMinor, &x.Count)
		return x, err
	})
}

// ExpenseRow is one expense as the overview lists it.
type ExpenseRow struct {
	ID          uuid.UUID
	BookedOn    platform.Date
	Payee       string // as the bank wrote it
	PayeeGroup  string
	Category    *NamedRef
	AmountMinor int64 // negative
}

// NamedRef is an id with its display name.
type NamedRef struct {
	ID   uuid.UUID
	Name string
}

// LargestExpenses lists the base rows' largest expenses booked in [from, to), at most limit; ties
// go to the table's default order.
func (in *Insight) LargestExpenses(from, to platform.Date, limit int) ([]ExpenseRow, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT t.id, t.booked_on, t.payee, t.payee_group, c.id, c.name, t.amount_minor FROM "+
		source(c.joins(JoinCategories)...)+" WHERE "+w+" AND t.amount_minor < 0 AND t.booked_on >= "+c.Add(from.String())+
		"::date AND t.booked_on < "+c.Add(to.String())+"::date ORDER BY t.amount_minor, "+tieBreak+" LIMIT "+c.Add(limit), c.Args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExpenseRow, error) {
		var x ExpenseRow
		var d pgtype.Date
		var cid *uuid.UUID
		var cname *string
		err := r.Scan(&x.ID, &d, &x.Payee, &x.PayeeGroup, &cid, &cname, &x.AmountMinor)
		if err == nil {
			x.BookedOn = *dateOf(d)
			if cid != nil && cname != nil {
				x.Category = &NamedRef{ID: *cid, Name: *cname}
			}
		}
		return x, err
	})
}

// Charge is one expense of a payee group that may be recurring.
type Charge struct {
	PayeeGroup string
	BookedOn   platform.Date
	SpentMinor int64   // positive
	Category   *string // nil: uncategorised
}

// RecurringCandidates lists the base rows' expenses of the payee groups that may be recurring,
// oldest first per group, in one scan. monthly holds the groups that pass the cheap part of the
// legacy findRecurring rules (expenses in at least 3 distinct months, at most 1.5 per month);
// others the groups that may be weekly (at least 6 expenses, at most 1.5 per calendar week, in at
// least half the weeks between the first and the last) or yearly (at least 2 expenses in at least
// 2 distinct years, at most 1.5 per year; PLAN 15.4). A group can be in both. The interval and
// amount rules need every amount and run in Go.
func (in *Insight) RecurringCandidates() (monthly, others []Charge, err error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return nil, nil, err
	}
	src := source(c.joins(JoinCategories)...)
	// One hash aggregate per (group, month, week) feeds every count; date_trunc on a timestamp
	// (not timestamptz) skips the time-zone conversion. A week's year is that of its first expense.
	months := "count(DISTINCT m)"
	weeks := "count(DISTINCT wk)"
	years := "count(DISTINCT extract(year FROM d0))"
	q := "WITH g AS (SELECT p, " + months + " >= 3 AND sum(n) <= 1.5 * " + months + " AS mo," +
		" (sum(n) >= 6 AND sum(n) <= 1.5 * " + weeks + " AND " + weeks + " >= 0.5 * ((max(d1) - min(d0)) / 7 + 1))" +
		" OR (sum(n) >= 2 AND " + years + " >= 2 AND sum(n) <= 1.5 * " + years + ") AS ot" +
		" FROM (SELECT t.payee_group AS p, date_trunc('month', t.booked_on::timestamp) AS m, date_trunc('week', t.booked_on::timestamp) AS wk," +
		" count(*) AS n, min(t.booked_on) AS d0, max(t.booked_on) AS d1 FROM " + src + " WHERE " + w + " AND t.amount_minor < 0 GROUP BY 1, 2, 3) x" +
		" GROUP BY 1)" +
		" SELECT t.payee_group, t.booked_on, -t.amount_minor, c.name, g.mo, g.ot FROM " + src + " JOIN g ON g.p = t.payee_group WHERE " + w +
		" AND t.amount_minor < 0 AND (g.mo OR g.ot) ORDER BY t.payee_group, t.booked_on, t.seq"
	rows, err := in.tx.Query(in.ctx, q, c.Args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x Charge
		var d pgtype.Date
		var mo, ot bool
		if err := rows.Scan(&x.PayeeGroup, &d, &x.SpentMinor, &x.Category, &mo, &ot); err != nil {
			return nil, nil, err
		}
		x.BookedOn = *dateOf(d)
		if mo {
			monthly = append(monthly, x)
		}
		if ot {
			others = append(others, x)
		}
	}
	return monthly, others, rows.Err()
}

// DayTotal is the sum of one day's transactions.
type DayTotal struct {
	Date     platform.Date
	SumMinor int64
}

// Balance returns the opening balances of the context's accounts and every day's sum of their
// transactions, oldest first, ignoring filters and search (legacy o_balance).
func (in *Insight) Balance() (opening int64, days []DayTotal, err error) {
	c, err := Compile(in.r.userID, in.r.ctx)
	if err != nil {
		return 0, nil, err
	}
	scope := where(c.Scope)
	accScope := strings.NewReplacer("t.user_id", "a.user_id", "t.account_id", "a.id").Replace(scope)
	if err := in.tx.QueryRow(in.ctx, "SELECT coalesce(sum(a.opening_balance_minor), 0)::bigint FROM accounts a WHERE "+accScope,
		c.Args[:c.ScopeN]...).Scan(&opening); err != nil {
		return 0, nil, err
	}
	rows, err := in.tx.Query(in.ctx, "SELECT t.booked_on, sum(t.amount_minor)::bigint FROM transactions t WHERE "+scope+
		" GROUP BY 1 ORDER BY 1", c.Args[:c.ScopeN]...)
	if err != nil {
		return 0, nil, err
	}
	days, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DayTotal, error) {
		var x DayTotal
		var d pgtype.Date
		err := r.Scan(&d, &x.SumMinor)
		if err == nil {
			x.Date = *dateOf(d)
		}
		return x, err
	})
	return opening, days, err
}

// Budgets returns the user's budget overrides by category.
func (in *Insight) Budgets() (map[uuid.UUID]int64, error) {
	rows, err := in.tx.Query(in.ctx, "SELECT category_id, amount_minor FROM budgets WHERE user_id = $1", in.r.userID)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]int64{}
	for rows.Next() {
		var id uuid.UUID
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// ExpenseDays lists the base rows' expense days booked in [from, to) with their spending (for the
// previous month to date, no-spend days and the weekday averages).
func (in *Insight) ExpenseDays(from, to platform.Date) ([]DaySpend, error) {
	return in.DailySpend(false, from, to)
}

// dividendPattern finds dividends and interest in the normalised row text (legacy o_yoc).
const dividendPattern = "dywidend|dividend|odsetk|interest|kupon|coupon"

// DividendIncome is the base rows' income booked in [from, to) whose text mentions dividends,
// interest or coupons (the yield-on-cost fallback when no income is entered by hand).
func (in *Insight) DividendIncome(from, to platform.Date) (int64, error) {
	c, w, err := in.r.fresh(false)
	if err != nil {
		return 0, err
	}
	var s int64
	err = in.tx.QueryRow(in.ctx, "SELECT coalesce(sum(t.amount_minor), 0)::bigint FROM "+source(c.joins()...)+" WHERE "+w+
		" AND t.amount_minor > 0 AND t.booked_on >= "+c.Add(from.String())+"::date AND t.booked_on < "+c.Add(to.String())+"::date"+
		" AND t.search_text ~ "+c.Add(dividendPattern), c.Args...).Scan(&s)
	return s, err
}
