package ingest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// NoColumn marks an unmapped role in a Mapping.
const NoColumn = -1

// Mapping says which table column holds which canonical field (PLAN §4.3). Columns are referenced
// by index into the shaped table, like the legacy e.cfg.map. Use NewMapping or MappingFromRoles:
// the zero value maps every role to column 0.
type Mapping struct {
	BookedOn            int
	Amount              int
	Payee               int
	Description         int
	Reference           int
	Category            int
	Currency            int
	ValueOn             int
	TxType              int
	CounterpartyAccount int
	OwnAccount          int
	Balance             int
	Debit               int // used with AmountDebitCredit
	Credit              int // used with AmountDebitCredit
	Ignore              []int
}

// NewMapping returns a mapping with every role unmapped.
func NewMapping() Mapping {
	return Mapping{BookedOn: NoColumn, Amount: NoColumn, Payee: NoColumn, Description: NoColumn, Reference: NoColumn,
		Category: NoColumn, Currency: NoColumn, ValueOn: NoColumn, TxType: NoColumn, CounterpartyAccount: NoColumn,
		OwnAccount: NoColumn, Balance: NoColumn, Debit: NoColumn, Credit: NoColumn}
}

// MappingFromRoles turns detected roles into the proposed mapping shown in the import preview.
func MappingFromRoles(r Roles) Mapping {
	m := NewMapping()
	m.BookedOn, m.Amount, m.Payee, m.Description, m.Reference = r.BookedOn, r.Amount, r.Payee, r.Description, r.Reference
	m.Category, m.Currency, m.ValueOn, m.TxType = r.Category, r.CurrencyColumn, r.ValueOn, r.TxType
	m.CounterpartyAccount, m.OwnAccount, m.Balance = r.CounterpartyAccount, r.OwnAccount, r.Balance
	return m
}

// AmountMode selects how the amount is read.
type AmountMode string

// Amount modes.
const (
	AmountSingle      AmountMode = "single"       // one signed amount column (default)
	AmountDebitCredit AmountMode = "debit_credit" // money out and money in in separate columns
)

// StageOptions are the per-import options that affect values.
type StageOptions struct {
	AmountMode AmountMode
	InvertSign bool // the bank exports expenses as positive numbers
	DateOrder  DateOrder
	// ImportCurrency is the file-level currency (Roles.Currency, or chosen in the preview).
	ImportCurrency string
	// AccountCurrency is the currency of the target account; the last fallback before PLN.
	AccountCurrency string
}

// RawField is one original cell, kept with its header in column order (stored as a JSONB array).
type RawField struct {
	Header string `json:"header"`
	Value  string `json:"value"`
}

// StagedRow is one data row turned into canonical fields. Rows with Error set are shown in the
// preview but never committed.
type StagedRow struct {
	RowIndex int `json:"rowIndex"` // 0-based position among the table's data rows

	BookedOn            platform.Date  `json:"bookedOn"`
	ValueOn             *platform.Date `json:"valueOn,omitempty"`
	AmountMinor         int64          `json:"amountMinor"`
	Currency            string         `json:"currency"`
	Payee               string         `json:"payee,omitempty"`
	Description         string         `json:"description,omitempty"`
	Reference           string         `json:"reference,omitempty"`
	TxType              string         `json:"txType,omitempty"`
	BankCategory        string         `json:"bankCategory,omitempty"`
	CounterpartyAccount string         `json:"counterpartyAccount,omitempty"`
	OwnAccount          string         `json:"ownAccount,omitempty"`
	BalanceMinor        *int64         `json:"balanceMinor,omitempty"`

	Raw        []RawField `json:"raw"`
	SearchText string     `json:"searchText"` // Norm(raw values joined by U+0001): the legacy rowNorm

	// Shadow columns: Collapse(Norm(x)), so search and filters use the one Go normalisation.
	PayeeN        string `json:"payeeN,omitempty"`
	DescriptionN  string `json:"descriptionN,omitempty"`
	ReferenceN    string `json:"referenceN,omitempty"`
	TxTypeN       string `json:"txTypeN,omitempty"`
	BankCategoryN string `json:"bankCategoryN,omitempty"`
	CounterpartyN string `json:"counterpartyN,omitempty"`
	PayeeGroup    string `json:"payeeGroup"`

	// De-duplication identity (fingerprint.go); zero for errored rows.
	Fingerprint [32]byte `json:"-"`
	Occurrence  int      `json:"occurrence"`

	Error string `json:"error,omitempty"`
}

// Stats summarises a staging run.
type Stats struct {
	Rows   int `json:"rows"`
	Valid  int `json:"valid"`
	Errors int `json:"errors"`
	// UnknownCurrencies were treated with money.DefaultExponent; the preview warns about them.
	UnknownCurrencies []string `json:"unknownCurrencies,omitempty"`
}

// Stage maps every data row of t to a StagedRow (PLAN §4.3) and assigns occurrences.
//
//   - Required: the booking date and the amount. A missing or unparseable value is a row error,
//     not a failed import: the preview shows the row and the error.
//   - Amount: money.ParseMinor of the mapped column, in the row's currency exponent. In debit/credit
//     mode the amount is |credit| − |debit| (both empty → error). If a signed Amount is mapped too,
//     the legacy rule applies: Amount is used and debit/credit stay ordinary columns. InvertSign
//     flips the result.
//   - Currency: the row's currency cell, else the import currency, else the account currency,
//     else PLN.
//   - Raw keeps every column except the ignored ones, in order; SearchText is built from it.
func Stage(ctx context.Context, t *Table, m Mapping, o StageOptions) ([]StagedRow, Stats, error) {
	ignored := map[int]bool{}
	for _, k := range m.Ignore {
		ignored[k] = true
	}
	mode := o.AmountMode
	if mode == "" || m.Amount != NoColumn {
		mode = AmountSingle
	}
	mdy := map[int]bool{}
	if o.DateOrder == DateOrderMDY {
		mdy[m.BookedOn], mdy[m.ValueOn] = true, true
	}
	unknown := map[string]bool{}
	rows := make([]StagedRow, len(t.Rows))
	stats := Stats{Rows: len(t.Rows)}
	for i, cells := range t.Rows {
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, stats, err
			}
		}
		cell := func(k int) string {
			if k < 0 || k >= len(cells) {
				return ""
			}
			return cells[k]
		}
		r := &rows[i]
		r.RowIndex = i
		r.Payee, r.Description, r.Reference = cell(m.Payee), cell(m.Description), cell(m.Reference)
		r.TxType, r.BankCategory = cell(m.TxType), cell(m.Category)
		r.CounterpartyAccount, r.OwnAccount = cell(m.CounterpartyAccount), cell(m.OwnAccount)

		r.Currency = firstNonEmpty(strings.ToUpper(jsTrim(cell(m.Currency))), strings.ToUpper(o.ImportCurrency), strings.ToUpper(o.AccountCurrency), "PLN")
		exp, known := money.Exponent(r.Currency)
		if !known {
			unknown[r.Currency] = true
		}

		var errs []string
		order := func(k int) DateOrder {
			if mdy[k] {
				return DateOrderMDY
			}
			return DateOrderDMY
		}
		switch v := cell(m.BookedOn); {
		case m.BookedOn == NoColumn:
			errs = append(errs, "no booking date column is mapped")
		case v == "":
			errs = append(errs, "booking date is empty")
		default:
			d, ok := ParseDateOrdered(v, order(m.BookedOn))
			if !ok {
				errs = append(errs, fmt.Sprintf("booking date %q is not a date", v))
			}
			r.BookedOn = d
		}
		if v := cell(m.ValueOn); m.ValueOn != NoColumn && v != "" {
			if d, ok := ParseDateOrdered(v, order(m.ValueOn)); ok {
				r.ValueOn = &d
			}
		}
		amount, err := stageAmount(cell, m, mode, exp)
		if err != "" {
			errs = append(errs, err)
		} else if o.InvertSign {
			amount = -amount
		}
		r.AmountMinor = amount
		if v := cell(m.Balance); m.Balance != NoColumn && v != "" {
			if b, err := money.ParseMinor(v, exp); err == nil {
				r.BalanceMinor = &b
			}
		}

		values := make([]string, 0, len(cells))
		for k, v := range cells {
			if ignored[k] {
				continue
			}
			r.Raw = append(r.Raw, RawField{Header: t.Headers[k], Value: v})
			values = append(values, v)
		}
		r.SearchText = Norm(strings.Join(values, "\u0001"))
		r.PayeeN, r.DescriptionN, r.ReferenceN = shadow(r.Payee), shadow(r.Description), shadow(r.Reference)
		r.TxTypeN, r.BankCategoryN, r.CounterpartyN = shadow(r.TxType), shadow(r.BankCategory), shadow(r.CounterpartyAccount)
		r.PayeeGroup = PayeeName(r.Payee)

		if len(errs) > 0 {
			r.Error = strings.Join(errs, "; ")
			stats.Errors++
			continue
		}
		r.Fingerprint = Fingerprint(FingerprintKey(r, exp))
		stats.Valid++
	}
	AssignOccurrences(rows)
	for c := range unknown {
		stats.UnknownCurrencies = append(stats.UnknownCurrencies, c)
	}
	sort.Strings(stats.UnknownCurrencies)
	return rows, stats, nil
}

// stageAmount reads the amount of one row in minor units, or returns an error message.
func stageAmount(cell func(int) string, m Mapping, mode AmountMode, exp int) (int64, string) {
	parse := func(what, v string) (int64, string) {
		x, err := money.ParseMinor(v, exp)
		if err != nil {
			return 0, fmt.Sprintf("%s %q is not an amount", what, v)
		}
		return x, ""
	}
	if mode == AmountDebitCredit {
		debit, credit := cell(m.Debit), cell(m.Credit)
		if m.Debit == NoColumn && m.Credit == NoColumn {
			return 0, "no debit or credit column is mapped"
		}
		if debit == "" && credit == "" {
			return 0, "debit and credit are both empty"
		}
		var d, c int64
		var e string
		if debit != "" {
			if d, e = parse("debit", debit); e != "" {
				return 0, e
			}
		}
		if credit != "" {
			if c, e = parse("credit", credit); e != "" {
				return 0, e
			}
		}
		return abs(c) - abs(d), ""
	}
	if m.Amount == NoColumn {
		return 0, "no amount column is mapped"
	}
	v := cell(m.Amount)
	if v == "" {
		return 0, "amount is empty"
	}
	return parse("amount", v)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func shadow(s string) string { return Collapse(Norm(s)) }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
