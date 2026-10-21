package ledger

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The mapping and options documents of an import (PLAN §4.3, §6.3), stored as JSON on imports and
// import_profiles and exchanged with the API as is. Columns are referenced by index in the shaped
// table (as the legacy e.cfg.map); the header name is kept for display and for validation.

// ColRef points at one column of the shaped table.
type ColRef struct {
	Index  int    `json:"index"`
	Header string `json:"header"`
}

// MappingDoc assigns columns to canonical roles. Nil means "not mapped".
type MappingDoc struct {
	BookedOn            *ColRef  `json:"bookedOn,omitempty"`
	Amount              *ColRef  `json:"amount,omitempty"`
	Payee               *ColRef  `json:"payee,omitempty"`
	Description         *ColRef  `json:"description,omitempty"`
	Reference           *ColRef  `json:"reference,omitempty"`
	Category            *ColRef  `json:"category,omitempty"`
	Currency            *ColRef  `json:"currency,omitempty"`
	ValueOn             *ColRef  `json:"valueOn,omitempty"`
	TxType              *ColRef  `json:"txType,omitempty"`
	CounterpartyAccount *ColRef  `json:"counterpartyAccount,omitempty"`
	OwnAccount          *ColRef  `json:"ownAccount,omitempty"`
	Balance             *ColRef  `json:"balance,omitempty"`
	Ignore              []ColRef `json:"ignore"`
}

// HeaderRowOpt is "auto", "none" or a 1-based record number.
type HeaderRowOpt struct {
	Mode ingest.HeaderMode
	Row  int
}

// MarshalJSON writes "auto", "none" or the number.
func (h HeaderRowOpt) MarshalJSON() ([]byte, error) {
	switch h.Mode {
	case ingest.HeaderNone:
		return []byte(`"none"`), nil
	case ingest.HeaderRow:
		return []byte(strconv.Itoa(h.Row)), nil
	}
	return []byte(`"auto"`), nil
}

// UnmarshalJSON reads "auto", "none" or a positive number.
func (h *HeaderRowOpt) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		switch s {
		case "auto", "":
			*h = HeaderRowOpt{}
			return nil
		case "none":
			*h = HeaderRowOpt{Mode: ingest.HeaderNone}
			return nil
		}
		return fmt.Errorf(`headerRow must be "auto", "none" or a record number, got %q`, s)
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil || n < 1 {
		return fmt.Errorf(`headerRow must be "auto", "none" or a record number ≥ 1`)
	}
	*h = HeaderRowOpt{Mode: ingest.HeaderRow, Row: n}
	return nil
}

// OptionsDoc are the reading options. The zero value is not valid; start from DefaultOptions.
type OptionsDoc struct {
	Encoding         string       `json:"encoding"`  // auto | utf-8 | windows-1250 | iso-8859-2 | windows-1252
	Delimiter        string       `json:"delimiter"` // auto | ; | , | \t | |
	HeaderRow        HeaderRowOpt `json:"headerRow"`
	SkipRows         int          `json:"skipRows"`
	DropShort        bool         `json:"dropShort"`
	DateOrder        string       `json:"dateOrder"` // dmy | mdy
	StripQuote       bool         `json:"stripQuote"`
	InvertSign       bool         `json:"invertSign"`
	AmountMode       string       `json:"amountMode"` // single | debit_credit
	DebitColumn      *ColRef      `json:"debitColumn,omitempty"`
	CreditColumn     *ColRef      `json:"creditColumn,omitempty"`
	CurrencyFallback string       `json:"currencyFallback"`
}

// DefaultOptions are the automatic settings of a fresh upload (legacy defaults).
func DefaultOptions() OptionsDoc {
	return OptionsDoc{Encoding: "auto", Delimiter: "auto", DropShort: true, DateOrder: "dmy", StripQuote: true, AmountMode: string(ingest.AmountSingle)}
}

// ParseOptions converts the reading part of o for ingest.Parse.
func (o OptionsDoc) ParseOptions() (ingest.Options, error) {
	ve := &platform.ValidationError{}
	out := ingest.Options{Shape: ingest.ShapeOptions{Header: o.HeaderRow.Mode, HeaderRow: o.HeaderRow.Row, SkipRows: o.SkipRows,
		KeepShort: !o.DropShort, KeepQuote: !o.StripQuote}}
	switch o.Encoding {
	case "", "auto":
		out.Encoding = ingest.EncodingAuto
	default:
		out.Encoding = ingest.Encoding(o.Encoding)
		if _, _, err := ingest.Decode(nil, out.Encoding); err != nil {
			ve.Add("options.encoding", fmt.Sprintf("unknown encoding %q", o.Encoding))
		}
	}
	switch o.Delimiter {
	case "", "auto":
	case ";", ",", "|":
		out.Delimiter = rune(o.Delimiter[0])
	case "\t", `\t`, "tab":
		out.Delimiter = '\t'
	default:
		ve.Add("options.delimiter", fmt.Sprintf("unknown delimiter %q", o.Delimiter))
	}
	switch o.DateOrder {
	case "", "dmy":
	case "mdy":
		out.DateOrder = ingest.DateOrderMDY
	default:
		ve.Add("options.dateOrder", "must be dmy or mdy")
	}
	if o.SkipRows < 0 {
		ve.Add("options.skipRows", "must not be negative")
	}
	return out, ve.Err()
}

// DelimiterName is the options form of a detected delimiter.
func DelimiterName(d rune) string {
	if d == '\t' {
		return `\t`
	}
	return string(d)
}

// MappingFromRoles turns detected roles into a mapping document.
func MappingFromRoles(r ingest.Roles, headers []string) MappingDoc {
	ref := func(i int) *ColRef {
		if i < 0 || i >= len(headers) {
			return nil
		}
		return &ColRef{Index: i, Header: headers[i]}
	}
	return MappingDoc{BookedOn: ref(r.BookedOn), Amount: ref(r.Amount), Payee: ref(r.Payee), Description: ref(r.Description),
		Reference: ref(r.Reference), Category: ref(r.Category), Currency: ref(r.CurrencyColumn), ValueOn: ref(r.ValueOn),
		TxType: ref(r.TxType), CounterpartyAccount: ref(r.CounterpartyAccount), OwnAccount: ref(r.OwnAccount),
		Balance: ref(r.Balance), Ignore: []ColRef{}}
}

// Resolve checks the mapping and options against the shaped headers and returns the ingest forms.
// Every ColRef must point at an existing column with the same header (a mapping saved for another
// layout fails here instead of silently reading the wrong column). Required roles are not checked
// here: a preview may be incomplete; Required reports them.
func Resolve(m MappingDoc, o OptionsDoc, headers []string) (ingest.Mapping, ingest.StageOptions, error) {
	ve := &platform.ValidationError{}
	col := func(field string, c *ColRef) int {
		if c == nil {
			return ingest.NoColumn
		}
		if c.Index < 0 || c.Index >= len(headers) {
			ve.Add(field, fmt.Sprintf("column %d does not exist (the file has %d)", c.Index, len(headers)))
			return ingest.NoColumn
		}
		if c.Header != "" && c.Header != headers[c.Index] {
			ve.Add(field, fmt.Sprintf("column %d is %q, not %q", c.Index, headers[c.Index], c.Header))
		}
		return c.Index
	}
	out := ingest.NewMapping()
	out.BookedOn = col("mapping.bookedOn", m.BookedOn)
	out.Amount = col("mapping.amount", m.Amount)
	out.Payee = col("mapping.payee", m.Payee)
	out.Description = col("mapping.description", m.Description)
	out.Reference = col("mapping.reference", m.Reference)
	out.Category = col("mapping.category", m.Category)
	out.Currency = col("mapping.currency", m.Currency)
	out.ValueOn = col("mapping.valueOn", m.ValueOn)
	out.TxType = col("mapping.txType", m.TxType)
	out.CounterpartyAccount = col("mapping.counterpartyAccount", m.CounterpartyAccount)
	out.OwnAccount = col("mapping.ownAccount", m.OwnAccount)
	out.Balance = col("mapping.balance", m.Balance)
	for i := range m.Ignore {
		if k := col(fmt.Sprintf("mapping.ignore[%d]", i), &m.Ignore[i]); k >= 0 {
			out.Ignore = append(out.Ignore, k)
		}
	}
	so := ingest.StageOptions{InvertSign: o.InvertSign}
	if o.DateOrder == "mdy" {
		so.DateOrder = ingest.DateOrderMDY
	}
	switch ingest.AmountMode(o.AmountMode) {
	case ingest.AmountSingle, "":
		so.AmountMode = ingest.AmountSingle
	case ingest.AmountDebitCredit:
		so.AmountMode = ingest.AmountDebitCredit
		out.Debit = col("options.debitColumn", o.DebitColumn)
		out.Credit = col("options.creditColumn", o.CreditColumn)
	default:
		ve.Add("options.amountMode", "must be single or debit_credit")
	}
	if c := strings.ToUpper(strings.TrimSpace(o.CurrencyFallback)); c != "" {
		if _, ok := money.Exponent(c); !ok {
			ve.Add("options.currencyFallback", fmt.Sprintf("unknown currency %q", o.CurrencyFallback))
		}
		so.ImportCurrency = c
	}
	return out, so, ve.Err()
}

// Missing lists the required roles the mapping lacks: the booking date, and the amount — or, in
// debit/credit mode, both columns. An empty result means the import can be committed.
func Missing(m MappingDoc, o OptionsDoc) []string {
	var out []string
	if m.BookedOn == nil {
		out = append(out, "bookedOn")
	}
	if o.AmountMode == string(ingest.AmountDebitCredit) && m.Amount == nil {
		if o.DebitColumn == nil || o.CreditColumn == nil {
			out = append(out, "debitColumn/creditColumn")
		}
	} else if m.Amount == nil {
		out = append(out, "amount")
	}
	return out
}
