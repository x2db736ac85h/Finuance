package ledger_test

import (
	"encoding/json"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

func TestCategories(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	c, err := e.l.CreateCategory(e.ctx, u, "Żywność")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.l.CreateCategory(e.ctx, u, "zywnosc")
	isErr(t, err, ledger.ErrConflict)
	ids, err := e.l.EnsureCategories(e.ctx, u, []string{"ŻYWNOŚĆ", "Transport", " transport ", ""})
	if err != nil || len(ids) != 2 || ids["zywnosc"] != c.ID {
		t.Fatalf("ensure = %v, %v", ids, err)
	}
	list, _ := e.l.ListCategories(e.ctx, u)
	if len(list) != 2 || list[0].Name != "Transport" || list[1].Name != "Żywność" { // first spelling kept; T < Ż in Polish order
		t.Errorf("list = %+v", list)
	}
	r, err := e.l.RenameCategory(e.ctx, u, c.ID, "Jedzenie")
	if err != nil || r.Name != "Jedzenie" {
		t.Errorf("rename = %+v, %v", r, err)
	}
	if err := e.l.SetBudget(e.ctx, u, c.ID, 120000); err != nil {
		t.Fatal(err)
	}
	if err := e.l.DeleteCategory(e.ctx, u, c.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := e.l.ListBudgets(e.ctx, u); len(b) != 0 {
		t.Errorf("budget outlived its category: %+v", b)
	}
	isErr(t, e.l.DeleteCategory(e.ctx, u, c.ID), ledger.ErrNotFound)
}

func TestGroups(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	var colors []int
	for _, n := range []string{"a", "b", "c"} {
		g, err := e.l.CreateGroup(e.ctx, u, n, -1)
		if err != nil {
			t.Fatal(err)
		}
		colors = append(colors, g.Color)
	}
	if colors[0] != 0 || colors[1] != 1 || colors[2] != 2 {
		t.Errorf("colours = %v", colors)
	}
	_, err := e.l.CreateGroup(e.ctx, u, "A", -1)
	isErr(t, err, ledger.ErrConflict)
	_, err = e.l.CreateGroup(e.ctx, u, string(make([]rune, 41)), -1)
	isErr(t, err, ledger.ErrInvalid)
	_, err = e.l.CreateGroup(e.ctx, u, "x", 8)
	isErr(t, err, ledger.ErrInvalid)
	gs, _ := e.l.ListGroups(e.ctx, u)
	up, err := e.l.UpdateGroup(e.ctx, u, gs[0].ID, "Italy", 7)
	if err != nil || up.Name != "Italy" || up.Color != 7 {
		t.Errorf("update = %+v, %v", up, err)
	}
}

func TestNextGroupColor(t *testing.T) {
	g := func(colors ...int) []ledger.Group {
		out := make([]ledger.Group, len(colors))
		for i, c := range colors {
			out[i].Color = c
		}
		return out
	}
	for _, c := range []struct {
		in   []ledger.Group
		want int
	}{
		{nil, 0}, {g(0, 1, 3), 2}, {g(1), 0}, {g(0, 1, 2, 3, 4, 5, 6, 7), 0}, {g(0, 1, 2, 3, 4, 5, 6, 7, 0), 1},
	} {
		if got := ledger.NextGroupColor(c.in); got != c.want {
			t.Errorf("NextGroupColor(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBudgets(t *testing.T) {
	e := newEnv(t)
	u, other := e.user(), e.user()
	c, _ := e.l.CreateCategory(e.ctx, u, "Food")
	isErr(t, e.l.SetBudget(e.ctx, u, c.ID, -1), ledger.ErrInvalid)
	if err := e.l.SetBudget(e.ctx, u, c.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := e.l.SetBudget(e.ctx, u, c.ID, 250); err != nil {
		t.Fatal(err)
	}
	b, _ := e.l.ListBudgets(e.ctx, u)
	if len(b) != 1 || b[0].AmountMinor != 250 || b[0].CategoryName != "Food" {
		t.Errorf("budgets = %+v", b)
	}
	// A budget on another user's category is rejected (the FK alone would accept it).
	isErr(t, e.l.SetBudget(e.ctx, other, c.ID, 1), ledger.ErrNotFound)
	if err := e.l.DeleteBudget(e.ctx, u, c.ID); err != nil {
		t.Fatal(err)
	}
	isErr(t, e.l.DeleteBudget(e.ctx, u, c.ID), ledger.ErrNotFound)
}

func TestProfiles(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	sig := [32]byte{1, 2, 3}
	_, err := e.l.GetProfile(e.ctx, u, sig)
	isErr(t, err, ledger.ErrNotFound)
	p := ledger.Profile{HeaderSignature: sig, Name: "Pekao", Mapping: json.RawMessage(`{"bookedOn":{"index":0}}`), Options: json.RawMessage(`{"dateOrder":"dmy"}`)}
	if err := e.l.SaveProfile(e.ctx, u, p); err != nil {
		t.Fatal(err)
	}
	p.Options = json.RawMessage(`{"dateOrder":"mdy"}`)
	if err := e.l.SaveProfile(e.ctx, u, p); err != nil {
		t.Fatal(err)
	}
	got, err := e.l.GetProfile(e.ctx, u, sig)
	if err != nil || got.Name != "Pekao" || string(got.Options) != `{"dateOrder": "mdy"}` || got.HeaderSignature != sig {
		t.Errorf("profile = %+v (%s), %v", got, got.Options, err)
	}
	isErr(t, e.l.SaveProfile(e.ctx, u, ledger.Profile{HeaderSignature: sig, Mapping: json.RawMessage(`{`), Options: json.RawMessage(`{}`)}), ledger.ErrInvalid)
	list, _ := e.l.ListProfiles(e.ctx, u)
	if len(list) != 1 {
		t.Errorf("profiles = %d", len(list))
	}
	if err := e.l.DeleteProfile(e.ctx, u, sig); err != nil {
		t.Fatal(err)
	}
}
