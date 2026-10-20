package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// legacyRoles maps the legacy MAP_ROLES vocabulary to Mapping fields (PLAN §4.2 table).
var legacyRoles = map[string]func(*Mapping, int){
	"date": func(m *Mapping, k int) { m.BookedOn = k }, "vdate": func(m *Mapping, k int) { m.ValueOn = k },
	"amount": func(m *Mapping, k int) { m.Amount = k }, "debit": func(m *Mapping, k int) { m.Debit = k },
	"credit": func(m *Mapping, k int) { m.Credit = k }, "payee": func(m *Mapping, k int) { m.Payee = k },
	"desc": func(m *Mapping, k int) { m.Description = k }, "category": func(m *Mapping, k int) { m.Category = k },
	"currency": func(m *Mapping, k int) { m.Currency = k }, "ref": func(m *Mapping, k int) { m.Reference = k },
	"type": func(m *Mapping, k int) { m.TxType = k }, "account": func(m *Mapping, k int) { m.OwnAccount = k },
	"balance": func(m *Mapping, k int) { m.Balance = k }, "ignore": func(m *Mapping, k int) { m.Ignore = append(m.Ignore, k) },
}

// optionsFromCfg translates a legacy e.cfg into Parse options.
func optionsFromCfg(cfg map[string]any) Options {
	o := Options{}
	if v, ok := cfg["enc"].(string); ok {
		o.Encoding = Encoding(v)
	}
	if v, ok := cfg["delim"].(string); ok {
		o.Delimiter, _ = utf8.DecodeRuneInString(v)
	}
	if v, ok := cfg["header"].(float64); ok {
		if v == 0 {
			o.Shape.Header = HeaderNone
		} else {
			o.Shape.Header, o.Shape.HeaderRow = HeaderRow, int(v)
		}
	}
	if v, ok := cfg["skip"].(float64); ok {
		o.Shape.SkipRows = int(v)
	}
	if v, ok := cfg["dropShort"].(bool); ok && !v {
		o.Shape.KeepShort = true
	}
	if v, ok := cfg["dateOrder"].(string); ok && v == "mdy" {
		o.DateOrder = DateOrderMDY
	}
	return o
}

// TestOracleImportSettings replays every recorded per-file setting (PLAN task 3.15).
//
// Without a mapping, invert or month-first dates, the legacy table is unchanged by applyMapping and
// must equal our shaped table exactly (header modes, footer drop, encodings, delimiters). With
// them, the legacy rewrote the table into canonical columns (Date, Amount, …); we keep the table
// and stage canonical fields instead, so each canonical legacy column is compared with the staged
// field (dates and amounts by value) and the remaining legacy columns with our raw columns.
func TestOracleImportSettings(t *testing.T) {
	cases := loadOracleIngest(t, "ingest-cfg")
	if len(cases) < 25 {
		t.Fatalf("only %d recorded settings", len(cases))
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, o.Fixture))
			if err != nil {
				t.Fatal(err)
			}
			opts := optionsFromCfg(o.Cfg)
			res, err := Parse(context.Background(), raw, opts)
			if err != nil && !errors.Is(err, ErrEmptyFile) {
				t.Fatal(err)
			}
			_, mapped := o.Cfg["map"]
			invert, _ := o.Cfg["invert"].(bool)
			if !mapped && !invert && opts.DateOrder != DateOrderMDY {
				ours := viewOf(res, err)
				if diff := cmp.Diff(legacyViewOf(t, o, ours), ours, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("table differs (-legacy +ours):\n%s", diff)
				}
				return
			}
			compareMapped(t, o, res, opts, invert)
		})
	}
}

func compareMapped(t *testing.T, o oracleIngest, res *Result, opts Options, invert bool) {
	t.Helper()
	if res.Table.HeaderAt != o.HeaderAt || res.Table.Dropped != o.Dropped {
		t.Errorf("headerAt/dropped = %d/%d, legacy %d/%d", res.Table.HeaderAt, res.Table.Dropped, o.HeaderAt, o.Dropped)
	}
	m := MappingFromRoles(res.Roles)
	so := StageOptions{InvertSign: invert, DateOrder: opts.DateOrder, ImportCurrency: res.Roles.Currency}
	if cm, ok := o.Cfg["map"].(map[string]any); ok {
		m = NewMapping()
		for k, role := range cm {
			idx, _ := strconv.Atoi(k)
			legacyRoles[role.(string)](&m, idx)
		}
		if m.Amount == NoColumn && (m.Debit != NoColumn || m.Credit != NoColumn) {
			so.AmountMode = AmountDebitCredit
		}
	}
	rows, _, err := Stage(context.Background(), res.Table, m, so)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(o.Rows) {
		t.Fatalf("%d staged rows, legacy %d", len(rows), len(o.Rows))
	}
	if _, mapped := o.Cfg["map"]; !mapped {
		compareRewritten(t, o, res, rows, m, opts)
		return
	}
	consumed := map[int]bool{m.BookedOn: true, m.ValueOn: true, m.Amount: true, m.Payee: true, m.Description: true,
		m.Category: true, m.Currency: true, m.Reference: true, m.TxType: true, m.OwnAccount: true, m.Balance: true}
	if so.AmountMode == AmountDebitCredit {
		consumed[m.Debit], consumed[m.Credit] = true, true
	}
	for i := range rows {
		r := &rows[i]
		if r.Error != "" {
			t.Errorf("row %d: %s", i, r.Error)
			continue
		}
		var plain []string
		for k, h := range o.Headers {
			v := o.Rows[i][k]
			switch h {
			case "Date":
				d, ok := ParseDate(v) // the legacy already rewrote month-first dates to ISO
				if !ok || d != r.BookedOn {
					t.Errorf("row %d: Date %q, ours %s", i, v, r.BookedOn)
				}
			case "Value date":
				if d, _ := ParseDate(v); r.ValueOn == nil || d != *r.ValueOn {
					t.Errorf("row %d: Value date %q, ours %v", i, v, r.ValueOn)
				}
			case "Amount":
				if x, err := money.ParseMinor(v, 2); err != nil || x != r.AmountMinor {
					t.Errorf("row %d: Amount %q, ours %d", i, v, r.AmountMinor)
				}
			case "Payee", "Description", "Category", "Currency", "Reference", "Type", "Account":
				ours := map[string]string{"Payee": r.Payee, "Description": r.Description, "Category": r.BankCategory,
					"Currency": r.Currency, "Reference": r.Reference, "Type": r.TxType, "Account": r.OwnAccount}[h]
				if h == "Currency" && v == "" {
					continue
				}
				if ours != v {
					t.Errorf("row %d: %s %q, ours %q", i, h, v, ours)
				}
			default:
				plain = append(plain, v)
			}
		}
		var ourPlain []string
		col := 0
		for k := range res.Table.Headers {
			if isIgnored(m, k) {
				continue
			}
			if !consumed[k] {
				ourPlain = append(ourPlain, r.Raw[col].Value)
			}
			col++
		}
		if diff := cmp.Diff(plain, ourPlain, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("row %d: unmapped columns differ (-legacy +ours):\n%s", i, diff)
		}
	}
}

func isIgnored(m Mapping, k int) bool {
	for _, i := range m.Ignore {
		if i == k {
			return true
		}
	}
	return false
}

// compareRewritten handles invert-only and month-first-only settings: the legacy kept the original
// column names and rewrote some cells in place (negated amounts, month-first dates as ISO). Every
// cell must equal ours, except those rewritten cells, which are compared by value.
func compareRewritten(t *testing.T, o oracleIngest, res *Result, rows []StagedRow, m Mapping, opts Options) {
	t.Helper()
	if diff := cmp.Diff(o.Headers, res.Table.Headers); diff != "" {
		t.Fatalf("headers differ (-legacy +ours):\n%s", diff)
	}
	mdy := map[int]bool{}
	if opts.DateOrder == DateOrderMDY {
		for _, k := range MDYColumns(res.Table) {
			mdy[k] = true
		}
	}
	for i := range rows {
		r := &rows[i]
		for k, ours := range res.Table.Rows[i] {
			legacy := o.Rows[i][k]
			switch {
			case k == m.Amount:
				if x, err := money.ParseMinor(legacy, 2); err != nil || x != r.AmountMinor {
					t.Errorf("row %d: amount %q, ours %d", i, legacy, r.AmountMinor)
				}
			case mdy[k]:
				want, ok1 := ParseDate(legacy)
				got, ok2 := ParseDateOrdered(ours, DateOrderMDY)
				if !ok1 || !ok2 || want != got {
					t.Errorf("row %d col %d: month-first %q → legacy %q", i, k, ours, legacy)
				}
			case legacy != ours:
				t.Errorf("row %d col %d: legacy %q, ours %q", i, k, legacy, ours)
			}
		}
	}
}
