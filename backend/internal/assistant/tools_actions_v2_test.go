package assistant

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/aichanges"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// TestProposalsV2: set_budget, add_expense and add_to_shared_group resolve their targets and what
// would change, read only, and refuse what can't be applied (Phase 20, ADR 0030).
func TestProposalsV2(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.importFile(u, "Konto", []byte("Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n"+
		"2026-10-01;BIEDRONKA;zakupy;-50,00;Jedzenie\n2026-10-03;KINO;bilety;-40,00;Rozrywka\n2026-10-05;KINO;popcorn;-15,00;Rozrywka\n"))
	var jedzenie uuid.UUID
	cats, err := e.q.CategoryRefs(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.Name == "Jedzenie" {
			jedzenie = c.ID
		}
	}
	if err := e.l.SetBudget(e.ctx, u, jedzenie, 50000); err != nil {
		t.Fatal(err)
	}
	wallet, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: "Portfel", Kind: ledger.KindCash})
	if err != nil {
		t.Fatal(err)
	}
	// a shared trip made from a personal group (ADR 0028)
	if err := e.st.WithoutUser(e.ctx, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `UPDATE users SET display_name = 'Alex' WHERE id = $1`, u)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	g, err := e.l.CreateGroup(e.ctx, u, "Praga", -1)
	if err != nil {
		t.Fatal(err)
	}
	notes := notify.New(e.st, platform.SystemClock{})
	sh := sharing.New(e.st, e.l, notes, platform.SystemClock{})
	sg, err := sh.Create(e.ctx, u, sharing.GroupInput{Name: "Wyjazd do Pragi", FromGroupID: &g.ID})
	if err != nil {
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

	// set_budget: an existing category, the budget it replaces, in the account's currency
	out, m := run(`{"action":"set_budget","to_category":"jedzenie","amount":800}`)
	p := out.Proposal
	if p == nil || p.CategoryID == nil || *p.CategoryID != jedzenie || p.BudgetMinor == nil || *p.BudgetMinor != 80000 ||
		p.BudgetBeforeMinor == nil || *p.BudgetBeforeMinor != 50000 || p.Currency != "PLN" || p.RemoveBudget {
		t.Fatalf("set_budget = %+v", p)
	}
	if m["budget"] != float64(800) || m["budget_before"] != float64(500) || m["to_category"] != "Jedzenie" || m["status"] != proposedStatus {
		t.Errorf("set_budget result = %s", out.JSON)
	}
	out, _ = run(`{"action":"set_budget","to_category":"Jedzenie","remove_budget":true}`)
	if p := out.Proposal; p == nil || !p.RemoveBudget || p.BudgetMinor != nil {
		t.Errorf("remove budget = %+v", p)
	}

	// add_expense: the only cash account by default, a negative amount, an existing or new category
	out, m = run(`{"action":"add_expense","date":"2026-11-05","amount":"25,50","payee":"Kawiarnia Ziarno","to_category":"jedzenie","note":"z Bo"}`)
	p = out.Proposal
	if p == nil || p.AccountID == nil || *p.AccountID != wallet.ID || p.AccountName != "Portfel" || p.AmountMinor != -2550 ||
		p.Date != "2026-11-05" || p.Payee != "Kawiarnia Ziarno" || p.CategoryID == nil || p.Note != "z Bo" || p.Matched != 1 {
		t.Fatalf("add_expense = %+v", p)
	}
	if m["amount"] != -25.5 || m["account"] != "Portfel" || m["creates_category"] != nil {
		t.Errorf("add_expense result = %s", out.JSON)
	}
	out, m = run(`{"action":"add_expense","account":"portfel","date":"2026-11-05","amount":12,"payee":"Bilety","to_category":"Komunikacja"}`)
	if p := out.Proposal; p == nil || p.CategoryID != nil || p.CategoryName != "Komunikacja" || m["creates_category"] != true {
		t.Errorf("add_expense with a new category = %+v %s", p, out.JSON)
	}

	// add_to_shared_group: the personal group linked to the shared one, the rows the filter selects
	out, m = run(`{"action":"add_to_shared_group","shared_group":"wyjazd do pragi","payee":"KINO"}`)
	p = out.Proposal
	if p == nil || p.GroupID == nil || *p.GroupID != g.ID || p.SharedGroupID == nil || p.SharedGroupName != "Wyjazd do Pragi" ||
		p.Matched != 2 || len(p.IDs) != 2 || m["shared_group"] != "Wyjazd do Pragi" {
		t.Fatalf("add_to_shared_group = %+v %s", p, out.JSON)
	}

	// a card payment the bank hasn't exported yet goes into the bank account named (ADR 0032)
	if out, _ := run(`{"action":"add_expense","account":"konto","date":"2026-11-05","amount":19.99,"payee":"Księgarnia"}`); out.Proposal == nil ||
		out.Proposal.AccountName != "Konto" || out.Proposal.AmountMinor != -1999 {
		t.Errorf("add_expense into a bank account = %s", out.JSON)
	}

	// refusals go back to the model; nothing changed
	for args, want := range map[string]string{
		`{"action":"set_budget","to_category":"Podróże","amount":100}`:                              "There is no category",
		`{"action":"set_budget","to_category":"Rozrywka","remove_budget":true}`:                     "has no budget of its own",
		`{"action":"set_budget","to_category":"Jedzenie","amount":-5,"remove_budget":false}`:        "",
		`{"action":"set_budget","to_category":"Jedzenie","amount":0}`:                               "amount must be the monthly budget",
		`{"action":"add_expense","date":"wczoraj","amount":5,"payee":"x"}`:                          "date must be",
		`{"action":"add_expense","date":"2026-11-05","amount":0,"payee":"x"}`:                       "amount must be",
		`{"action":"add_expense","date":"2026-11-05","amount":5}`:                                   "payee must",
		`{"action":"add_expense","account":"Skarbonka","date":"2026-11-05","amount":5,"payee":"x"}`: "There is no account",
		`{"action":"add_to_shared_group","shared_group":"Wakacje","payee":"KINO"}`:                  "shared_group must be one of: Wyjazd do Pragi",
		`{"action":"add_to_shared_group","shared_group":"Wyjazd do Pragi"}`:                         "Give a filter",
	} {
		out, _ := run(args)
		if want == "" {
			continue // a negative budget reads as its absolute value, like the amounts in filters
		}
		if out.Proposal != nil || !strings.Contains(out.JSON, want) {
			t.Errorf("%s: %s, want %q", args, out.JSON, want)
		}
	}
	budgets, err := e.q.Budgets(e.ctx, u)
	if err != nil || budgets[jedzenie] != 50000 || len(budgets) != 1 {
		t.Errorf("budgets after proposing = %v, %v (proposals change nothing)", budgets, err)
	}

	// split_expense: Bo joins the trip, the two cinema rows go in (the user's expenses there)
	bo := e.user()
	if err := e.st.WithoutUser(e.ctx, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `UPDATE users SET display_name = 'Bo' WHERE id = $1`, bo)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fr := friends.New(e.st, platform.SystemClock{}, notes)
	code, err := fr.NewCode(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fr.Redeem(e.ctx, bo, code.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := sh.Invite(e.ctx, u, sg.ID, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := sh.Accept(e.ctx, bo, sg.ID); err != nil {
		t.Fatal(err)
	}
	kino := p.IDs // the add_to_shared_group proposal's rows
	if _, err := e.l.Annotate(e.ctx, u, kino, ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	out, _ = run(`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino","date":"2026-10-03","members":[{"name":"me"},{"name":"bo"}]}`)
	p = out.Proposal
	if p == nil || p.ExpenseID == nil || p.SharedGroupID == nil || *p.SharedGroupID != sg.ID || p.Date != "2026-10-03" || p.AmountMinor != 4000 ||
		p.Split == nil || p.Split.Mode != "equal" || len(p.Split.Members) != 2 || p.Split.Members[1].Name != "Bo" || p.PaidBy == nil {
		t.Fatalf("split_expense = %+v %s", p, out.JSON)
	}
	out, _ = run(`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino","date":"2026-10-03","split_mode":"exact",` +
		`"members":[{"name":"me","amount":30},{"name":"Bo","amount":10}]}`)
	if p := out.Proposal; p == nil || p.Split.Mode != "exact" || p.Split.Members[0].AmountMinor != 3000 || p.Split.Members[1].AmountMinor != 1000 {
		t.Fatalf("exact split = %+v %s", out.Proposal, out.JSON)
	}
	for args, want := range map[string]string{
		`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino"}`:                                                                               "2 expenses match; give the date",
		`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"teatr"}`:                                                                              "theirs are:",
		`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino","date":"2026-10-03","members":[{"name":"Cy"}]}`:                                 `is not an active member of Wyjazd do Pragi; the members are: Alex, Bo`,
		`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino","date":"2026-10-03","split_mode":"exact","members":[{"name":"me","amount":5}]}`: "add up to 5, the expense is 40",
		`{"action":"split_expense","shared_group":"Wyjazd do Pragi","payee":"kino","date":"2026-10-03","split_mode":"halves"}`:                                     "split_mode must be",
	} {
		out, _ := run(args)
		if out.Proposal != nil || !strings.Contains(out.JSON, want) {
			t.Errorf("%s: %s, want %q", args, out.JSON, want)
		}
	}
	// applied (as the panel does) and undone: Bo joining split it equally already (ADR 0028), the
	// exact 30/10 goes back to 20/20
	members, _, err := e.q.SharedGroup(e.ctx, u, sg.ID)
	if err != nil {
		t.Fatal(err)
	}
	aiCtx := ledger.WithAIOrigin(e.ctx, ledger.AIOrigin{ChatID: platform.NewID(), ProposalKey: "00000000000000b1", Action: ActionSplitExpense})
	if err := sh.UpdateExpense(aiCtx, u, sg.ID, *p.ExpenseID, sharing.ExpenseInput{Split: sharing.Split{Mode: "exact",
		Exact: []splits.Share{{Member: members[0].ID, Minor: 3000}, {Member: members[1].ID, Minor: 1000}}}}); err != nil {
		t.Fatal(err)
	}
	ac := aichanges.New(e.st, e.l, platform.SystemClock{})
	page, err := ac.List(e.ctx, u, "", 0)
	if err != nil || len(page.Changes) != 1 || page.Changes[0].Action != ActionSplitExpense {
		t.Fatalf("changes = %+v, %v", page.Changes, err)
	}
	if _, err := ac.Revert(e.ctx, u, page.Changes[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	d, err := sh.Get(e.ctx, u, sg.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range d.Expenses {
		if x.ID == *p.ExpenseID && (x.SplitMode != "equal" || len(x.Shares) != 2 || x.Shares[0].Minor != 2000 || x.Shares[1].Minor != 2000) {
			t.Errorf("split after undo = %+v", x.Shares)
		}
	}

	// another user has none of it
	other := e.user()
	for args, want := range map[string]string{
		`{"action":"set_budget","to_category":"Jedzenie","amount":100}`:                    "There is no category",
		`{"action":"add_expense","date":"2026-11-05","amount":5,"payee":"x"}`:              "has no cash account",
		`{"action":"add_to_shared_group","shared_group":"Wyjazd do Pragi","payee":"KINO"}`: "in no shared group",
	} {
		out, err := tools.Run(e.ctx, other, Scope{MaxRows: 50}, ToolPropose, args)
		if err != nil || out.Proposal != nil || !strings.Contains(out.JSON, want) {
			t.Errorf("other user %s: %s, %v", args, out.JSON, err)
		}
	}
}
