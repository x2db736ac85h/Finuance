package assistant

import (
	"fmt"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// Phase 27: the opposites of set_group and set_category, money in, and a payment that is there
// already.
func TestProposalsPhase27(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.importFile(u, "Konto", []byte("Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n"+
		"2026-10-01;BIEDRONKA;zakupy;-50,00;Jedzenie\n2026-10-03;KINO;bilety;-40,00;Rozrywka\n2026-10-05;KINO;popcorn;-15,00;Rozrywka\n"))
	if _, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: "Portfel", Kind: ledger.KindCash}); err != nil {
		t.Fatal(err)
	}
	tools := NewTools(e.q)
	run := func(args string) (Outcome, map[string]any) {
		t.Helper()
		out, err := tools.Run(e.ctx, u, Scope{MaxRows: 50}, ToolPropose, args)
		if err != nil {
			t.Fatal(err)
		}
		return out, decode(t, []byte(out.JSON)).(map[string]any)
	}

	// take transactions out of their group: the rows, no group named, said to the model as such
	out, m := run(`{"action":"set_group","payee":"KINO","remove_from_group":true,"reason":"Not part of the trip"}`)
	if p := out.Proposal; p == nil || p.Action != ActionSetGroup || !p.RemoveGroup || p.GroupName != "" || p.GroupID != nil || p.Matched != 2 || len(p.IDs) != 2 {
		t.Fatalf("remove_from_group = %+v (%s)", out.Proposal, out.JSON)
	}
	if m["removes_from_group"] != true || m["to_group"] != nil || m["creates_group"] != nil {
		t.Errorf("the model was told %s", out.JSON)
	}
	// take a hand-set category off
	out, m = run(`{"action":"set_category","payee":"BIEDRONKA","clear_category":true}`)
	if p := out.Proposal; p == nil || p.Action != ActionSetCategory || !p.ClearCategory || p.CategoryName != "" || p.Matched != 1 {
		t.Fatalf("clear_category = %+v (%s)", out.Proposal, out.JSON)
	}
	if m["clears_category"] != true || m["to_category"] != nil {
		t.Errorf("the model was told %s", out.JSON)
	}
	// neither applies without a click, whatever the person switched on
	for _, p := range []Proposal{{Action: ActionSetGroup, RemoveGroup: true, Matched: 1}, {Action: ActionSetCategory, ClearCategory: true, Matched: 1}} {
		if err := MayAutoApply(&p, "chat", []string{ActionSetGroup, ActionSetCategory}); err == nil {
			t.Errorf("%s (remove %v, clear %v) may apply by itself", p.Action, p.RemoveGroup, p.ClearCategory)
		}
	}
	// a proposal's key tells the two directions apart
	add, _ := run(`{"action":"set_group","payee":"KINO","to_group":"Wieczory"}`)
	rem, _ := run(`{"action":"set_group","payee":"KINO","remove_from_group":true}`)
	if add.Proposal.Key() == rem.Proposal.Key() {
		t.Error("adding to a group and removing from one have the same key")
	}

	// money in
	out, _ = run(`{"action":"add_expense","account":"Portfel","date":"2026-10-06","amount":120,"payee":"Zwrot od Ani","income":true}`)
	if p := out.Proposal; p == nil || p.AmountMinor != 12000 || p.AccountName != "Portfel" {
		t.Fatalf("income = %+v (%s)", out.Proposal, out.JSON)
	}
	// money out into a bank account by its name, for a payment that isn't there
	out, _ = run(`{"action":"add_expense","account":"Konto","date":"2026-10-08","amount":23.5,"payee":"Piekarnia"}`)
	if p := out.Proposal; p == nil || p.AmountMinor != -2350 || p.AccountName != "Konto" {
		t.Fatalf("a bank account = %+v (%s)", out.Proposal, out.JSON)
	}
	// a payment of that amount is there already (two days away): no proposal, and the model is told
	out, m = run(`{"action":"add_expense","account":"Konto","date":"2026-10-05","amount":40,"payee":"Kino Helios"}`)
	if out.Proposal != nil || !strings.Contains(fmt.Sprint(m["error"]), "already has a payment of this amount on 2026-10-03") {
		t.Errorf("a twin = %s", out.JSON)
	}
	// … unless the user said it is a second one
	out, _ = run(`{"action":"add_expense","account":"Konto","date":"2026-10-05","amount":40,"payee":"Kino Helios","add_anyway":true}`)
	if out.Proposal == nil || out.Proposal.AmountMinor != -4000 {
		t.Errorf("add_anyway = %s", out.JSON)
	}
	// the same amount in another account is no twin
	out, _ = run(`{"action":"add_expense","account":"Portfel","date":"2026-10-03","amount":40,"payee":"Kino"}`)
	if out.Proposal == nil {
		t.Errorf("another account = %s", out.JSON)
	}

	for args, want := range map[string]string{
		`{"action":"set_group","payee":"KINO","to_group":"X","remove_from_group":true}`:    "not both",
		`{"action":"set_category","payee":"KINO","to_category":"X","clear_category":true}`: "not both",
		`{"action":"set_group","remove_from_group":true}`:                                  "Give a filter",
		`{"action":"set_group","payee":"NO SUCH","remove_from_group":true}`:                "No transactions match",
	} {
		out, m := run(args)
		if out.Proposal != nil || !strings.Contains(fmt.Sprint(m["error"]), want) {
			t.Errorf("%s = %s", args, out.JSON)
		}
	}
}
