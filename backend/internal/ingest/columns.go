package ingest

import (
	"math"
	"regexp"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// ColumnType is the inferred type of a column.
type ColumnType string

// Column types.
const (
	TypeText   ColumnType = "text"
	TypeNumber ColumnType = "number"
	TypeDate   ColumnType = "date"
)

// Column is one analysed column. Numbers and Dates hold the parsed value of every row (NaN /
// zero Date where a cell does not parse), whatever the column's type.
type Column struct {
	Index   int
	Name    string
	Type    ColumnType
	Numbers []float64
	Dates   []platform.Date
}

// AnalyzeColumn types a column (legacy `analyzeCol`): it is a date column when ≥ 90 % of its
// non-empty cells parse as dates, otherwise a number column when ≥ 90 % parse as numbers, otherwise
// text. Dates are tried first because "15.04.2024" also parses as a number (15.042024).
//
// order applies the month-first rule (see MDYColumns): pass DateOrderMDY only for the columns it
// selects.
func AnalyzeColumn(index int, name string, rows [][]string, order DateOrder) Column {
	c := Column{Index: index, Name: name, Numbers: make([]float64, len(rows)), Dates: make([]platform.Date, len(rows))}
	nonEmpty, numbers, dates := 0, 0, 0
	for i, r := range rows {
		c.Numbers[i] = math.NaN()
		v := r[index]
		if v == "" {
			continue
		}
		nonEmpty++
		if d, ok := ParseDateOrdered(v, order); ok {
			c.Dates[i] = d
			dates++
		}
		if x, ok := ParseNumber(v); ok {
			c.Numbers[i] = x
			numbers++
		}
	}
	c.Type = TypeText
	if nonEmpty > 0 {
		switch {
		case float64(dates)/float64(nonEmpty) >= 0.9:
			c.Type = TypeDate
		case float64(numbers)/float64(nonEmpty) >= 0.9:
			c.Type = TypeNumber
		}
	}
	return c
}

// AnalyzeTable types every column of t. With DateOrderMDY, month-first parsing applies to the
// columns MDYColumns selects plus the mapped date columns passed in mdyAlso.
func AnalyzeTable(t *Table, order DateOrder, mdyAlso ...int) []Column {
	mdy := map[int]bool{}
	if order == DateOrderMDY {
		for _, k := range MDYColumns(t) {
			mdy[k] = true
		}
		for _, k := range mdyAlso {
			mdy[k] = true
		}
	}
	cols := make([]Column, len(t.Headers))
	for k, h := range t.Headers {
		o := DateOrderDMY
		if mdy[k] {
			o = DateOrderMDY
		}
		cols[k] = AnalyzeColumn(k, h, t.Rows, o)
	}
	return cols
}

// MDYColumns returns the columns where ≥ 80 % of the first 200 non-empty cells look like
// dd/mm/yyyy (legacy applyMapping with dateOrder 'mdy'). Only these, and mapped date columns, are
// read month first; a column of amounts or ids is never reinterpreted.
func MDYColumns(t *Table) []int {
	var out []int
	for k := range t.Headers {
		n, m := 0, 0
		for i := 0; i < len(t.Rows) && i < 200; i++ {
			if v := t.Rows[i][k]; v != "" {
				n++
				if mdyCandidate.MatchString(v) {
					m++
				}
			}
		}
		if n > 0 && float64(m)/float64(n) >= 0.8 {
			out = append(out, k)
		}
	}
	return out
}

// Roles are the columns detected for each meaning, as indices into the table (-1 = none). The
// import preview proposes them as the mapping; the user can change every one.
type Roles struct {
	BookedOn            int
	Amount              int
	Category            int
	Payee               int
	Description         int
	Reference           int
	CurrencyColumn      int
	Balance             int
	ValueOn             int
	TxType              int
	CounterpartyAccount int
	OwnAccount          int
	// Currency is the import-level currency: the only distinct value of CurrencyColumn, if it has
	// exactly one ("PLN"), else "".
	Currency string
}

// The role regexes of the legacy detectRoles (case-insensitive), plus the new roles of PLAN §4.2.
var (
	reBookedOn    = regexp.MustCompile(`(?i)ksi[eę]gow|booking|posted|transakcji|operacji|transaction`)
	reAmount      = regexp.MustCompile(`(?i)kwota|amount|warto|value|suma|betrag|montant`)
	reBalance     = regexp.MustCompile(`(?i)saldo|balance`)
	reBalanceRole = regexp.MustCompile(`(?i)saldo|balance|stan konta`)
	reCategory    = regexp.MustCompile(`(?i)kategor|category|categor`)
	rePayee       = regexp.MustCompile(`(?i)nadawca|odbiorca|kontrahent|payee|merchant|counterpart|recipient|beneficiar`)
	rePayeeAlt    = regexp.MustCompile(`(?i)opis|description|tytu|title|name|nazwa`)
	reCurrency    = regexp.MustCompile(`(?i)^waluta$|currency`)
	reDescription = regexp.MustCompile(`(?i)tytu|title|opis|description|memo`)
	reReference   = regexp.MustCompile(`(?i)referenc|^ref\b|transaction id|id transakcji|numer transakcji`)
	reValueOn     = regexp.MustCompile(`(?i)walut|value date|valuta`)
	reTxType      = regexp.MustCompile(`(?i)typ operacji|typ transakcji|transaction type|^type$`)
	reCounterAcct = regexp.MustCompile(`(?i)rachunek docelowy|rachunek (nadawcy|odbiorcy)|counterparty account|iban`)
	reOwnAccount  = regexp.MustCompile(`(?i)rachunek [źz]r[oó]d[lł]owy|own account|account number|numer rachunku`)
)

// DetectRoles guesses the meaning of each column (legacy `detectRoles` + PLAN §4.2):
//
//   - exact canonical names win first ("Date" date column, "Amount" number column, "Category",
//     "Payee" text column), so files exported by Finuance itself re-import cleanly;
//   - BookedOn: a date column whose name matches the booking regex, else the first date column;
//   - Amount: a number column matching kwota/amount/… but not saldo/balance, else the first number
//     column that is not a balance;
//   - Payee: a text column matching nadawca/odbiorca/kontrahent/…, else one matching opis/tytuł/…;
//   - Description: tytuł/title/opis/… that is not the payee column; Reference: referenc/ref/…;
//   - CurrencyColumn: a text column named waluta/currency; one distinct value becomes Currency;
//   - Balance (number), ValueOn (a second date column), TxType, CounterpartyAccount, OwnAccount.
func DetectRoles(cols []Column, rows [][]string) Roles {
	find := func(match func(Column) bool) int {
		for _, c := range cols {
			if match(c) {
				return c.Index
			}
		}
		return -1
	}
	named := func(re *regexp.Regexp, typ ColumnType) func(Column) bool {
		return func(c Column) bool { return (typ == "" || c.Type == typ) && re.MatchString(c.Name) }
	}
	exact := func(name string, typ ColumnType) func(Column) bool {
		return func(c Column) bool { return c.Name == name && (typ == "" || c.Type == typ) }
	}
	first := func(fs ...func(Column) bool) int {
		for _, f := range fs {
			if i := find(f); i >= 0 {
				return i
			}
		}
		return -1
	}
	r := Roles{}
	r.BookedOn = first(exact("Date", TypeDate), named(reBookedOn, TypeDate), func(c Column) bool { return c.Type == TypeDate })
	r.Amount = first(exact("Amount", TypeNumber),
		func(c Column) bool {
			return c.Type == TypeNumber && reAmount.MatchString(c.Name) && !reBalance.MatchString(c.Name)
		},
		func(c Column) bool { return c.Type == TypeNumber && !reBalance.MatchString(c.Name) })
	r.Category = first(exact("Category", ""), named(reCategory, ""))
	r.Payee = first(exact("Payee", TypeText), named(rePayee, TypeText), named(rePayeeAlt, TypeText))
	r.Balance = find(named(reBalanceRole, TypeNumber))
	r.CurrencyColumn = find(named(reCurrency, TypeText))
	r.Description = find(func(c Column) bool { return c.Index != r.Payee && reDescription.MatchString(c.Name) })
	r.Reference = find(named(reReference, ""))
	r.ValueOn = find(func(c Column) bool {
		return c.Index != r.BookedOn && c.Type == TypeDate && reValueOn.MatchString(c.Name)
	})
	r.TxType = find(named(reTxType, ""))
	r.CounterpartyAccount = find(named(reCounterAcct, ""))
	r.OwnAccount = find(named(reOwnAccount, ""))
	if r.CurrencyColumn >= 0 {
		distinct := map[string]bool{}
		for _, row := range rows {
			if v := row[r.CurrencyColumn]; v != "" {
				distinct[v] = true
			}
		}
		if len(distinct) == 1 {
			for v := range distinct {
				r.Currency = v
			}
		}
	}
	return r
}
