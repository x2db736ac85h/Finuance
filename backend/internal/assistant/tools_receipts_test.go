package assistant

import (
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

func TestMatchReceipt(t *testing.T) {
	on := platform.Date{Y: 2026, M: 11, D: 3}
	row := func(payee string, d int, cur string) query.ReceiptRow {
		return query.ReceiptRow{Payee: payee, BookedOn: platform.Date{Y: 2026, M: 11, D: d}, Currency: cur}
	}
	rows := []query.ReceiptRow{row("ORLEN STACJA", 3, "PLN"), row("BIEDRONKA SKLEP 123 KRAKÓW", 4, "PLN"),
		row("Biedronka", 3, "PLN"), row("BIEDRONKA", 3, "EUR")}
	got := matchReceipt(rows, on, "Sklep Biedronka nr 123", "PLN")
	if len(got) != 3 || got[0].row.Payee != "Biedronka" || got[0].words != 1 || got[1].row.Payee != "ORLEN STACJA" ||
		got[2].gap != 1 || got[2].words != 2 {
		t.Errorf("ranking = %+v (the nearest day first, then words of the shop; EUR left out)", got)
	}
	if w := shopWords("The Copper Kettle Café, Ltd."); strings.Join(w, " ") != "copper kettle cafe" {
		t.Errorf("shop words = %v", w)
	}
}

// TestReceiptTools: find_receipt_match lists the payments a receipt may be, attach_receipt picks
// the one that fits and proposes its note, read only (ADR 0032).
func TestReceiptTools(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.importFile(u, "Konto", []byte("Data operacji;Kontrahent;Tytuł;Kwota\n"+
		"2026-11-03;BIEDRONKA SKLEP 123;zakupy;-54,30\n2026-11-04;ORLEN;paliwo;-54,30\n2026-10-20;KINO;bilety;-54,30\n"+
		"2026-11-03;ŻABKA;napoje;-12,00\n"))
	tools := NewTools(e.q)
	run := func(name, args string) (Outcome, map[string]any) {
		t.Helper()
		out, err := tools.Run(e.ctx, u, Scope{MaxRows: 50}, name, args)
		if err != nil {
			t.Fatal(err)
		}
		return out, decode(t, []byte(out.JSON)).(map[string]any)
	}

	out, m := run(ToolReceipt, `{"amount":54.30,"date":"2026-11-03","shop":"Biedronka","currency":"PLN"}`)
	cs := m["candidates"].([]any)
	if len(cs) != 2 || cs[0].(map[string]any)["payee"] != "BIEDRONKA SKLEP 123" || cs[0].(map[string]any)["shop_words_in_payee"] != float64(1) ||
		cs[1].(map[string]any)["days_apart"] != float64(1) || cs[0].(map[string]any)["amount"] != float64(54.3) || out.Summary != "receipt match · 2 candidates" {
		t.Fatalf("candidates = %s (KINO is 14 days away)", out.JSON)
	}
	if _, m := run(ToolReceipt, `{"amount":54.30,"date":"2026-11-03","shop":"Biedronka","currency":"EUR"}`); len(m["candidates"].([]any)) != 0 ||
		!strings.Contains(m["note"].(string), "add_expense") {
		t.Errorf("another currency = %v", m)
	}

	out, m = run(ToolPropose, `{"action":"attach_receipt","amount":"54,30","date":"2026-11-03","shop":"Biedronka","items":"chleb, masło"}`)
	p := out.Proposal
	if p == nil || len(p.IDs) != 1 || p.ReceiptLine != "Receipt: Biedronka · chleb, masło" || p.Note != p.ReceiptLine ||
		p.Payee != "BIEDRONKA SKLEP 123" || p.AccountName != "Konto" || p.AmountMinor != -5430 || len(p.Sample) != 1 || m["status"] != proposedStatus {
		t.Fatalf("attach_receipt = %+v %s", p, out.JSON)
	}
	// a note already there stays first
	if _, err := e.l.Annotate(e.ctx, u, p.IDs, ledger.AnnotationPatch{SetNote: true, Note: "zakupy na weekend"}); err != nil {
		t.Fatal(err)
	}
	if out, _ := run(ToolPropose, `{"action":"attach_receipt","amount":54.30,"date":"2026-11-03","shop":"Biedronka"}`); out.Proposal == nil ||
		out.Proposal.Note != "zakupy na weekend\nReceipt: Biedronka" {
		t.Errorf("with a note = %s", out.JSON)
	}
	if _, err := e.l.Annotate(e.ctx, u, p.IDs, ledger.AnnotationPatch{SetNote: true, Note: "Receipt: Biedronka"}); err != nil {
		t.Fatal(err)
	}

	// two payments that fit equally well need the account; another account's copy of the same
	e.importFile(u, "Karta", []byte("Data operacji;Kontrahent;Tytuł;Kwota\n2026-11-03;BIEDRONKA SKLEP 7;zakupy;-54,30\n"))
	for args, want := range map[string]string{
		`{"action":"attach_receipt","amount":54.30,"date":"2026-11-03","shop":"Biedronka"}`:                   "fit equally well",
		`{"action":"attach_receipt","amount":54.30,"date":"2026-11-03","shop":"Biedronka","account":"Konto"}`: "has this receipt",
		`{"action":"attach_receipt","amount":99,"date":"2026-11-03","shop":"Biedronka"}`:                      "propose add_expense",
		`{"action":"attach_receipt","amount":54.30,"date":"2026-11-03"}`:                                      "shop must be",
		`{"action":"attach_receipt","amount":54.30,"date":"wczoraj","shop":"Biedronka"}`:                      "date must be",
	} {
		if out, m := run(ToolPropose, args); out.Proposal != nil || !strings.Contains(m["error"].(string), want) {
			t.Errorf("%s: %s, want %q", args, out.JSON, want)
		}
	}
	if out, _ := run(ToolPropose, `{"action":"attach_receipt","amount":54.30,"date":"2026-11-03","shop":"Biedronka","account":"karta"}`); out.Proposal == nil ||
		out.Proposal.AccountName != "Karta" {
		t.Errorf("by account = %s", out.JSON)
	}

	// another user's payments are none of its business
	if out, err := tools.Run(e.ctx, e.user(), Scope{}, ToolReceipt, `{"amount":54.30,"date":"2026-11-03","shop":"Biedronka"}`); err != nil ||
		!strings.Contains(out.JSON, `"candidates":[]`) {
		t.Errorf("other user: %v %s", err, out.JSON)
	}
}
