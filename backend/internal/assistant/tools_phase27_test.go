package assistant

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
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

// The memory (ADR 0046): stored with the person's settings, added to the system prompt while it is
// on, shortened for a cloud model; and a line the assistant proposes to remember.
func TestMemory(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	system := func() string {
		t.Helper()
		reqs := re.mock.Requests()
		return string(reqs[len(reqs)-1])
	}
	// nothing kept: nothing added
	s, err := re.chats.Settings(re.ctx, re.user)
	if err != nil || s.Memory == nil || !s.Memory.Enabled || s.Memory.Auto || s.Memory.Text != "" {
		t.Fatalf("the default memory = %+v (%v)", s.Memory, err)
	}
	re.mock.Push(llmmock.Reply{Content: "Hello."})
	re.ask(chat, "Hi", nil)
	if strings.Contains(system(), "asked you to keep in mind") {
		t.Error("an empty memory is in the prompt")
	}
	// kept and on: in the prompt, after the rules
	note := "I am paid on the 10th. Answer in Polish. My account is PL61109010140000071219812874."
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Memory: &Memory{Enabled: true, Text: "  " + note + "\r\n"}}); err != nil {
		t.Fatal(err)
	}
	s, _ = re.chats.Settings(re.ctx, re.user)
	if s.Memory.Text != note {
		t.Errorf("stored memory = %q", s.Memory.Text)
	}
	re.mock.Push(llmmock.Reply{Content: "Hello."})
	re.ask(chat, "Hi again", nil)
	if got := system(); !strings.Contains(got, "asked you to keep in mind") || !strings.Contains(got, "I am paid on the 10th") {
		t.Errorf("the memory is not in the prompt: %s", got)
	}
	// a save without the memory keeps it (the panel's other settings don't send it)
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}}); err != nil {
		t.Fatal(err)
	}
	if s, _ = re.chats.Settings(re.ctx, re.user); s.Memory.Text != note {
		t.Errorf("a save without the memory changed it: %q", s.Memory.Text)
	}
	// switched off: kept, not sent
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Memory: &Memory{Enabled: false, Text: note}}); err != nil {
		t.Fatal(err)
	}
	re.mock.Push(llmmock.Reply{Content: "Hello."})
	re.ask(chat, "And now", nil)
	if strings.Contains(system(), "I am paid on the 10th") {
		t.Error("a memory that is switched off is in the prompt")
	}
	// too long is refused
	var ve *platform.ValidationError
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Memory: &Memory{Enabled: true,
		Text: strings.Repeat("ż", MaxMemoryChars+1)}}); !errors.As(err, &ve) {
		t.Errorf("a note over the limit: %v", err)
	}
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Memory: &Memory{Enabled: true,
		Text: strings.Repeat("ż", MaxMemoryChars)}}); err != nil {
		t.Errorf("a note at the limit: %v", err)
	}

	// for a cloud model long digit runs are shortened, as in everything else that leaves
	cloud := &Run{memory: note, conn: &Connection{Cloud: true}}
	if b := cloud.memoryBlock(); strings.Contains(b, "61109010140000071219812874") || !strings.Contains(b, "paid on the 10th") {
		t.Errorf("the memory for a cloud model: %s", b)
	}
	local := &Run{memory: note, conn: &Connection{}}
	if b := local.memoryBlock(); !strings.Contains(b, "61109010140000071219812874") {
		t.Errorf("the memory for a local model: %s", b)
	}

	// a line to remember is a proposal like any other: nothing is stored by the tool
	tools := NewTools(re.q)
	out, err := tools.Run(re.ctx, re.user, Scope{MaxRows: 50}, ToolPropose, `{"action":"remember","memory_line":"  Prefers answers   in Polish ","reason":"You asked me to"}`)
	if err != nil || out.Proposal == nil || out.Proposal.Action != ActionRemember || out.Proposal.MemoryLine != "Prefers answers in Polish" {
		t.Fatalf("remember = %+v (%v) %s", out.Proposal, err, out.JSON)
	}
	if err := MayAutoApply(out.Proposal, "chat", AutoApplyActions); err == nil {
		t.Error("a remember proposal passes the server's auto-apply check, which is for changes to data")
	}
	out, _ = tools.Run(re.ctx, re.user, Scope{MaxRows: 50}, ToolPropose, `{"action":"remember","memory_line":"`+strings.Repeat("x", 201)+`"}`)
	if out.Proposal != nil || !strings.Contains(out.JSON, "memory_line must be") {
		t.Errorf("a line too long = %s", out.JSON)
	}
}
