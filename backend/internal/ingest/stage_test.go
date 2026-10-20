package ingest

import (
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func table(headers []string, rows ...[]string) *Table { return &Table{Headers: headers, Rows: rows} }

func TestStageBasics(t *testing.T) {
	tb := table([]string{"Data", "Kontrahent", "Tytuł", "Kwota", "Waluta", "Kategoria", "Hash"},
		[]string{"02.01.2025", "KAWIARNIA  POZNAŃ", "Kawa  z  mlekiem", "-12,50", "", "Restauracje", "x1"},
		[]string{"03.01.2025", "SKLEP", "Zakupy", "-1 234,56", "EUR", "Zakupy", "x2"},
		[]string{"", "BRAK DATY", "", "-1,00", "", "", "x3"},
		[]string{"31.02.2025", "ZŁA DATA", "", "abc", "", "", "x4"},
	)
	m := NewMapping()
	m.BookedOn, m.Payee, m.Description, m.Amount, m.Currency, m.Category, m.Ignore = 0, 1, 2, 3, 4, 5, []int{6}
	rows, st, err := Stage(context.Background(), tb, m, StageOptions{ImportCurrency: "pln"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Rows != 4 || st.Valid != 2 || st.Errors != 2 {
		t.Errorf("stats = %+v", st)
	}
	r := rows[0]
	if r.BookedOn != platform.MustDate(2025, 1, 2) || r.AmountMinor != -1250 || r.Currency != "PLN" || r.BankCategory != "Restauracje" {
		t.Errorf("row 0 = %+v", r)
	}
	if r.PayeeN != "kawiarnia poznan" || r.DescriptionN != "kawa z mlekiem" || r.PayeeGroup != "KAWIARNIA" {
		t.Errorf("shadow fields: %q %q %q", r.PayeeN, r.DescriptionN, r.PayeeGroup)
	}
	if len(r.Raw) != 6 || r.Raw[5].Header != "Kategoria" || strings.Contains(r.SearchText, "x1") {
		t.Errorf("ignored column leaked into raw/search: %+v %q", r.Raw, r.SearchText)
	}
	if r.SearchText != Norm("02.01.2025\u0001KAWIARNIA  POZNAŃ\u0001Kawa  z  mlekiem\u0001-12,50\u0001\u0001Restauracje") {
		t.Errorf("search text = %q", r.SearchText)
	}
	if rows[1].Currency != "EUR" || rows[1].AmountMinor != -123456 {
		t.Errorf("row 1 = %+v", rows[1])
	}
	if rows[2].Error != "booking date is empty" || rows[3].Error != `booking date "31.02.2025" is not a date; amount "abc" is not an amount` {
		t.Errorf("errors: %q / %q", rows[2].Error, rows[3].Error)
	}
	if rows[2].Occurrence != 0 || rows[0].Occurrence != 1 {
		t.Error("errored rows get occurrence 0")
	}
}

func TestStageAmountModes(t *testing.T) {
	tb := table([]string{"Data", "Opis", "Wydatki", "Wpływy", "Kwota"},
		[]string{"02.01.2025", "a", "45,20", "", "-45,20"},
		[]string{"10.01.2025", "b", "", "8 500,00", "8 500,00"},
		[]string{"11.01.2025", "c", "", "", ""},
		[]string{"12.01.2025", "d", "-3,00", "1,00", "x"},
	)
	m := NewMapping()
	m.BookedOn, m.Description, m.Debit, m.Credit = 0, 1, 2, 3
	rows, _, _ := Stage(context.Background(), tb, m, StageOptions{AmountMode: AmountDebitCredit})
	want := []int64{-4520, 850000, 0, -200} // |credit| − |debit|: debit signs are ignored
	for i, w := range want {
		if rows[i].AmountMinor != w {
			t.Errorf("debit/credit row %d = %d, want %d", i, rows[i].AmountMinor, w)
		}
	}
	if rows[2].Error != "debit and credit are both empty" {
		t.Errorf("both empty: %q", rows[2].Error)
	}
	inv, _, _ := Stage(context.Background(), tb, m, StageOptions{AmountMode: AmountDebitCredit, InvertSign: true})
	if inv[0].AmountMinor != 4520 || inv[1].AmountMinor != -850000 {
		t.Error("invert after debit/credit")
	}
	// A signed Amount mapped as well wins; debit/credit stay ordinary raw columns (legacy rule).
	m.Amount = 4
	both, _, _ := Stage(context.Background(), tb, m, StageOptions{AmountMode: AmountDebitCredit})
	if both[0].AmountMinor != -4520 || both[1].AmountMinor != 850000 || both[3].Error != `amount "x" is not an amount` || len(both[0].Raw) != 5 {
		t.Errorf("amount + debit/credit: %+v", both[:2])
	}
}

func TestStageCurrencyAndOptionalFields(t *testing.T) {
	tb := table([]string{"Data", "Data waluty", "Kwota", "Saldo", "Waluta"},
		[]string{"2025-01-02", "2025-01-01", "-1,234", "100,00", "XYZ"},
		[]string{"2025-01-03", "garbage", "5", "n/a", ""},
	)
	m := NewMapping()
	m.BookedOn, m.ValueOn, m.Amount, m.Balance, m.Currency = 0, 1, 2, 3, 4
	rows, st, _ := Stage(context.Background(), tb, m, StageOptions{AccountCurrency: "EUR"})
	if rows[0].Currency != "XYZ" || rows[0].AmountMinor != -123 || *rows[0].BalanceMinor != 10000 || *rows[0].ValueOn != platform.MustDate(2025, 1, 1) {
		t.Errorf("row 0: %+v", rows[0])
	}
	if rows[1].Currency != "EUR" || rows[1].ValueOn != nil || rows[1].BalanceMinor != nil || rows[1].Error != "" {
		t.Errorf("row 1 (fallbacks, optional fields never error): %+v", rows[1])
	}
	if len(st.UnknownCurrencies) != 1 || st.UnknownCurrencies[0] != "XYZ" {
		t.Errorf("unknown currencies = %v", st.UnknownCurrencies)
	}
	none, _, _ := Stage(context.Background(), tb, m, StageOptions{})
	if none[1].Currency != "PLN" {
		t.Error("PLN is the last fallback")
	}
}

func TestStageMonthFirstAndCancel(t *testing.T) {
	tb := table([]string{"Date", "Amount"}, []string{"03/04/2025", "-1.00"}, []string{"10/31/2025", "2.00"})
	m := NewMapping()
	m.BookedOn, m.Amount = 0, 1
	rows, _, _ := Stage(context.Background(), tb, m, StageOptions{DateOrder: DateOrderMDY})
	if rows[0].BookedOn != platform.MustDate(2025, 3, 4) || rows[1].BookedOn != platform.MustDate(2025, 10, 31) {
		t.Errorf("mdy: %v %v", rows[0].BookedOn, rows[1].BookedOn)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Stage(ctx, tb, m, StageOptions{}); err == nil {
		t.Error("cancelled context ignored")
	}
}

// The fingerprint format is stored in the database: these vectors must never change. The hashes
// were computed independently: printf '%s' "$key" | sha256sum.
func TestFingerprintVectors(t *testing.T) {
	ref := StagedRow{Reference: "'C992506002418351", AmountMinor: -12713}
	if k := FingerprintKey(&ref, 2); k != "r|'c992506002418351|-127.13" {
		t.Errorf("reference key = %q", k)
	}
	plain := StagedRow{BookedOn: platform.MustDate(2025, 3, 1), AmountMinor: -4498, Payee: "ŻABKA  Z6373 K.1   RASZYN", Description: "Zakupy\tspożywcze"}
	key := FingerprintKey(&plain, 2)
	if key != "2025-03-01|-44.98|zabka z6373 k.1 raszyn|zakupy spozywcze" {
		t.Errorf("plain key = %q", key)
	}
	fp := Fingerprint(key)
	if got := hex.EncodeToString(fp[:]); got != "1d42b746b540675e589a7e35b7efea5c917e6e3412ad690d7ab0455c26731d14" {
		t.Errorf("fingerprint = %s", got)
	}
	sig := HeaderSignature([]string{"Data", "Kwota"}, ';')
	if got := hex.EncodeToString(sig[:]); got != "be5c4683717509790e5bf95ebd4e3562bd14b01e9e0fc217366c18cb58179b0d" {
		t.Errorf("header signature = %s", got)
	}
	if HeaderSignature([]string{"DATA", "Kwota"}, ';') != sig || HeaderSignature([]string{"Data", "Kwota"}, ',') == sig {
		t.Error("signature must ignore case but not the delimiter")
	}
}

func TestAssignOccurrences(t *testing.T) {
	a, b := Fingerprint("a"), Fingerprint("b")
	rows := []StagedRow{{Fingerprint: a}, {Fingerprint: b}, {Fingerprint: a}, {Error: "x"}, {Fingerprint: a}}
	AssignOccurrences(rows)
	got := []int{rows[0].Occurrence, rows[1].Occurrence, rows[2].Occurrence, rows[3].Occurrence, rows[4].Occurrence}
	if want := []int{1, 1, 2, 0, 3}; !slices.Equal(got, want) {
		t.Errorf("occurrences = %v, want %v", got, want)
	}
}

// SearchText is built per cell; it must equal Norm of the joined row (the legacy rowNorm).
func TestSearchTextEqualsNormOfJoin(t *testing.T) {
	tb := table([]string{"a", "b", "c"}, []string{"ZAŻÓŁĆ", "é x", "Σ ΟΔΟΣ"}, []string{"", "Ą", "plain"})
	m := NewMapping()
	m.BookedOn, m.Amount = 0, 1
	rows, _, _ := Stage(context.Background(), tb, m, StageOptions{})
	for i, r := range rows {
		if want := Norm(strings.Join(tb.Rows[i], "\u0001")); r.SearchText != want {
			t.Errorf("row %d: %q, want %q", i, r.SearchText, want)
		}
	}
}
