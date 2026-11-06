package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// TestGoalTools: goal_status reads the goals with their progress, and create_goal / update_goal
// resolve what the panel would send, read only (Phase 20, ADR 0031).
func TestGoalTools(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.importFile(u, "Konto", []byte("Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n"+
		"2026-08-10;BIEDRONKA;zakupy;-600,00;Jedzenie\n2026-09-10;BIEDRONKA;zakupy;-400,00;Jedzenie\n"+
		"2026-10-10;BIEDRONKA;zakupy;-500,00;Jedzenie\n2026-11-01;PRACODAWCA;pensja;3000,00;Wynagrodzenie\n"+
		"2026-11-02;BIEDRONKA;zakupy;-100,00;Jedzenie\n"))
	gs := goals.New(e.st, platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)})
	tools := NewTools(e.q).WithGoals(gs)
	run := func(name, args string) (Outcome, map[string]any) {
		t.Helper()
		out, err := tools.Run(e.ctx, u, Scope{MaxRows: 50}, name, args)
		if err != nil {
			t.Fatal(err)
		}
		return out, decode(t, []byte(out.JSON)).(map[string]any)
	}

	if _, m := run(ToolGoals, `{}`); len(m["goals"].([]any)) != 0 || !strings.Contains(m["note"].(string), "no goals yet") {
		t.Errorf("no goals = %v", m)
	}
	start := platform.MustDate(2026, 11, 1)
	target := int64(1_000_000)
	holiday, err := gs.Create(e.ctx, u, goals.Input{Name: "Wakacje w Grecji", Kind: goals.KindSave, TargetMinor: &target, StartsOn: &start,
		Deadline: platform.MustDate(2027, 6, 30)})
	if err != nil {
		t.Fatal(err)
	}
	cats, err := e.q.CategoryRefs(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	var food = cats[0].ID
	for _, c := range cats {
		if c.Name == "Jedzenie" {
			food = c.ID
		}
	}
	pct := 20
	if _, err := gs.Create(e.ctx, u, goals.Input{Name: "Mniej na jedzenie", Kind: goals.KindReduce, CategoryID: &food, ReducePct: &pct,
		StartsOn: &start, Deadline: platform.MustDate(2027, 4, 30)}); err != nil {
		t.Fatal(err)
	}
	closed, err := gs.Create(e.ctx, u, goals.Input{Name: "Old", Kind: goals.KindSave, TargetMinor: &target, Deadline: platform.MustDate(2027, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Update(e.ctx, u, closed.ID, goals.Input{Name: "Old", TargetMinor: &target, Deadline: platform.MustDate(2027, 1, 1),
		Status: goals.StatusShelf, Version: 1}); err != nil {
		t.Fatal(err)
	}

	out, m := run(ToolGoals, `{}`)
	list := m["goals"].([]any)
	if len(list) != 2 || m["closed_goals_omitted"] != float64(1) || out.Summary != "goal status · 2 goals" {
		t.Fatalf("goal_status = %s", out.JSON)
	}
	cut, save := list[0].(map[string]any), list[1].(map[string]any)
	if cut["kind"] != "reduce" || cut["category"] != "Jedzenie" || cut["baseline_per_month"] != float64(500) || cut["monthly_limit"] != float64(400) ||
		cut["this_month"] != float64(100) || cut["allowed_so_far"] != float64(80) || cut["progress"] != "behind" || cut["reduce_pct"] != float64(20) {
		t.Errorf("reduce goal = %v", cut)
	}
	if save["kind"] != "save" || save["target"] != float64(10000) || save["saved"] != float64(2900) || save["progress"] != "on_track" ||
		save["deadline"] != "2027-06-30" || save["days_left"] != float64(236) {
		t.Errorf("save goal = %v", save)
	}
	if _, m := run(ToolGoals, `{"goal":"grecji"}`); len(m["goals"].([]any)) != 1 {
		t.Errorf("one goal by part of its name = %v", m)
	}
	if _, m := run(ToolGoals, `{"goal":"car"}`); m["error"] == nil || len(m["goals"].([]any)) != 3 {
		t.Errorf("unknown goal = %v", m)
	}

	// create_goal: save (target in the account's currency, optionally on one account) or reduce
	out, m = run(ToolPropose, `{"action":"create_goal","goal_name":"Car","amount":20000,"deadline":"2027-12-31","account":"konto"}`)
	if g := out.Proposal.Goal; g == nil || g.Kind != goals.KindSave || *g.TargetMinor != 2_000_000 || g.Currency != "PLN" || g.AccountName != "Konto" ||
		g.Deadline != "2027-12-31" || g.BaselineMonths != 3 || g.ID != nil {
		t.Fatalf("create_goal save = %+v", out.Proposal)
	}
	if goal := m["goal"].(map[string]any); goal["target"] != float64(20000) || goal["account"] != "Konto" || m["status"] != proposedStatus {
		t.Errorf("create_goal result = %s", out.JSON)
	}
	out, _ = run(ToolPropose, `{"action":"create_goal","goal_name":"Less food","to_category":"jedzenie","reduce_pct":"15%","deadline":"2027-04-30"}`)
	if g := out.Proposal.Goal; g == nil || g.Kind != goals.KindReduce || g.CategoryID == nil || *g.CategoryID != food || g.ReducePct != 15 || g.TargetMinor != nil {
		t.Fatalf("create_goal reduce = %+v", out.Proposal)
	}
	for args, want := range map[string]string{
		`{"action":"create_goal","goal_name":"x","amount":100,"deadline":"2026-11-06"}`:                            "after today",
		`{"action":"create_goal","goal_name":"x","deadline":"2027-01-01"}`:                                         "amount",
		`{"action":"create_goal","goal_name":"x","goal_kind":"spend","deadline":"2027-01-01"}`:                     "goal_kind",
		`{"action":"create_goal","goal_name":"x","to_category":"Kawa","reduce_pct":10,"deadline":"2027-01-01"}`:    "no category",
		`{"action":"create_goal","goal_name":"x","to_category":"Jedzenie","reduce_pct":0,"deadline":"2027-01-01"}`: "reduce_pct",
		`{"action":"create_goal","amount":100,"deadline":"2027-01-01"}`:                                            "goal_name",
		`{"action":"create_goal","goal_name":"x","amount":100,"deadline":"2027-01-01","account":"Skarbonka"}`:      "no account",
		`{"action":"update_goal","goal":"car","amount":100}`:                                                       "Wakacje w Grecji",
		`{"action":"update_goal","goal":"wakacje"}`:                                                                "Nothing to change",
		`{"action":"update_goal","goal":"wakacje","goal_status":"paused"}`:                                         "goal_status",
	} {
		if out, m := run(ToolPropose, args); out.Proposal != nil || !strings.Contains(m["error"].(string), want) {
			t.Errorf("%s: %s (want %q)", args, out.JSON, want)
		}
	}

	// update_goal: the whole goal after the change, with the version read, and as it was
	out, m = run(ToolPropose, `{"action":"update_goal","goal":"wakacje","amount":12000,"deadline":"2027-08-31"}`)
	p := out.Proposal
	if p == nil || p.Goal == nil || p.GoalBefore == nil || *p.Goal.ID != holiday.ID || *p.Goal.TargetMinor != 1_200_000 || p.Goal.Deadline != "2027-08-31" ||
		p.Goal.Version != 1 || p.Goal.Status != "active" || *p.GoalBefore.TargetMinor != 1_000_000 || p.GoalBefore.Deadline != "2027-06-30" {
		t.Fatalf("update_goal = %+v", p)
	}
	if m["goal_before"].(map[string]any)["target"] != float64(10000) || m["goal"].(map[string]any)["target"] != float64(12000) {
		t.Errorf("update_goal result = %s", out.JSON)
	}
	if out, _ := run(ToolPropose, `{"action":"update_goal","goal":"Old","goal_status":"active"}`); out.Proposal == nil || out.Proposal.Goal.Status != "active" {
		t.Errorf("reopen = %s", out.JSON)
	}
	if g, _ := gs.Get(e.ctx, u, holiday.ID); *g.TargetMinor != 1_000_000 || g.Version != 1 {
		t.Errorf("a proposal changed the goal: %+v", g)
	}

	// another user sees none of these goals; tools without the service say so
	if _, m := run(ToolGoals, `{}`); len(m["goals"].([]any)) != 2 {
		t.Fatal("setup")
	}
	if out, err := tools.Run(e.ctx, e.user(), Scope{MaxRows: 50}, ToolGoals, `{}`); err != nil || !strings.Contains(out.JSON, `"goals":[]`) {
		t.Errorf("other user: %v %s", err, out.JSON)
	}
	if out, err := NewTools(e.q).Run(e.ctx, u, Scope{}, ToolGoals, `{}`); err != nil || !strings.Contains(out.JSON, "not available") {
		t.Errorf("no service: %v %s", err, out.JSON)
	}
}
