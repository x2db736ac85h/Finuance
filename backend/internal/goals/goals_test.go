package goals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/aichanges"
	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

type env struct {
	t       *testing.T
	ctx     context.Context
	st      *store.Store
	l       *ledger.Ledger
	s       *goals.Service
	u       uuid.UUID
	account uuid.UUID
	food    uuid.UUID // Jedzenie
}

func d(y int, m time.Month, day int) platform.Date { return platform.MustDate(y, m, day) }

// newEnv is a user on 2026-11-06 with three months of groceries before November (600, 400, 500)
// and, in November, a salary of 3,000, groceries of 100 and fuel of 400.
func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.New(testutil.Require(t, testDB).App)
	clk := platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	e := &env{t: t, ctx: context.Background(), st: st, l: ledger.New(st, clk), s: goals.New(st, clk), u: newUser(t, st)}
	acc, err := e.l.CreateAccount(e.ctx, e.u, ledger.AccountInput{Name: "Konto"})
	if err != nil {
		t.Fatal(err)
	}
	e.account = acc.ID
	csv := "Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n" +
		"2026-08-10;BIEDRONKA;zakupy;-600,00;Jedzenie\n2026-09-10;BIEDRONKA;zakupy;-400,00;Jedzenie\n" +
		"2026-10-10;BIEDRONKA;zakupy;-500,00;Jedzenie\n2026-11-01;PRACODAWCA;pensja;3000,00;Wynagrodzenie\n" +
		"2026-11-02;BIEDRONKA;zakupy;-100,00;Jedzenie\n2026-11-03;ORLEN;paliwo;-400,00;Transport\n"
	p, err := e.l.CreatePreview(e.ctx, e.u, ledger.Upload{Filename: "a.csv", Content: []byte(csv), AccountID: &acc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.StartCommit(e.ctx, e.u, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, e.u, p.Import.ID); err != nil {
		t.Fatal(err)
	}
	e.food = e.category("Jedzenie")
	return e
}

func newUser(t *testing.T, st *store.Store) uuid.UUID {
	t.Helper()
	u := platform.NewID()
	if err := st.WithoutUser(context.Background(), func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(context.Background(), db.CreateUserParams{ID: u, Email: u.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return u
}

func (e *env) category(name string) uuid.UUID {
	e.t.Helper()
	cats, err := e.l.ListCategories(e.ctx, e.u)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, c := range cats {
		if c.Name == name {
			return c.ID
		}
	}
	e.t.Fatalf("no category %q", name)
	return uuid.Nil
}

func ptr[T any](v T) *T { return &v }

func (e *env) save(t *testing.T, in goals.Input) goals.Goal {
	t.Helper()
	g, err := e.s.Create(e.ctx, e.u, in)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGoalsProgress(t *testing.T) {
	e := newEnv(t)
	start := d(2026, 11, 1)
	// 10,000 by the end of June: 2,500 put aside in six days of 242 (the pace asks 247.93)
	all := e.save(t, goals.Input{Name: "  Wakacje   w Grecji ", Kind: goals.KindSave, TargetMinor: ptr(int64(1_000_000)), StartsOn: &start,
		Deadline: d(2027, 6, 30)})
	if all.Name != "Wakacje w Grecji" || all.Currency != "PLN" || all.Version != 1 || all.AI || all.Status != "active" {
		t.Errorf("created %+v", all)
	}
	if p := all.Progress; p.State != goals.StateOnTrack || p.SavedMinor != 250_000 || p.ExpectedMinor != 24_793 || p.Percent != 25 || p.DaysLeft != 236 {
		t.Errorf("save progress %+v", p)
	}
	// the same on the one account, by its name
	one := e.save(t, goals.Input{Name: "Konto", Kind: goals.KindSave, TargetMinor: ptr(int64(1_000_000)), AccountID: &e.account,
		StartsOn: &start, Deadline: d(2027, 6, 30)})
	if one.AccountName != "Konto" || one.Progress.SavedMinor != 250_000 {
		t.Errorf("account goal %+v", one)
	}
	// another currency saw nothing
	eur := e.save(t, goals.Input{Name: "Euro", Kind: goals.KindSave, TargetMinor: ptr(int64(100_000)), Currency: "EUR", StartsOn: &start,
		Deadline: d(2027, 6, 30)})
	if eur.Currency != "EUR" || eur.Progress.SavedMinor != 0 || eur.Progress.State != goals.StateBehind {
		t.Errorf("EUR goal %+v", eur)
	}
	// 20 % less on groceries: baseline 500, limit 400, 100 spent where 80 were allowed by the 6th
	cut := e.save(t, goals.Input{Name: "Mniej na jedzenie", Kind: goals.KindReduce, CategoryID: &e.food, ReducePct: ptr(20),
		StartsOn: &start, Deadline: d(2027, 4, 30)})
	if p := cut.Progress; cut.CategoryName != "Jedzenie" || p.State != goals.StateBehind || p.BaselineMinor != 50_000 ||
		p.MonthlyLimitMinor != 40_000 || p.ThisMonthMinor != 10_000 || p.AllowedSoFarMinor != 8_000 || p.Percent != 25 {
		t.Errorf("reduce goal %+v", cut)
	}
	list, err := e.s.List(e.ctx, e.u)
	if err != nil || len(list) != 4 || list[0].ID != cut.ID { // open goals by deadline
		t.Fatalf("list %v %+v", err, list)
	}

	// changing it needs the version read
	in := goals.Input{Name: "Mniej na jedzenie", CategoryID: &e.food, ReducePct: ptr(10), Deadline: d(2027, 4, 30), Version: 2}
	if _, err := e.s.Update(e.ctx, e.u, cut.ID, in); !errors.Is(err, platform.ErrStaleVersion) {
		t.Errorf("stale update: %v", err)
	}
	in.Version = 1
	cut, err = e.s.Update(e.ctx, e.u, cut.ID, in)
	if err != nil || cut.Version != 2 || *cut.ReducePct != 10 || cut.Progress.MonthlyLimitMinor != 45_000 || cut.Progress.State != goals.StateBehind {
		t.Fatalf("update %v %+v", err, cut)
	}

	// merging the category moves the goal along; deleting it leaves the goal needing attention
	other, err := e.l.CreateCategory(e.ctx, e.u, "Zakupy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.MergeCategory(e.ctx, e.u, e.food, other.ID); err != nil {
		t.Fatal(err)
	}
	if g, err := e.s.Get(e.ctx, e.u, cut.ID); err != nil || g.CategoryName != "Zakupy" || g.Progress.BaselineMinor != 50_000 {
		t.Errorf("after merge %v %+v", err, g)
	}
	if err := e.l.DeleteCategory(e.ctx, e.u, other.ID); err != nil {
		t.Fatal(err)
	}
	if g, err := e.s.Get(e.ctx, e.u, cut.ID); err != nil || g.CategoryID != nil || g.Progress.State != goals.StateNeedsAttention {
		t.Errorf("after delete %v %+v", err, g)
	}

	if err := e.s.Delete(e.ctx, e.u, eur.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Delete(e.ctx, e.u, eur.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestGoalsValidation(t *testing.T) {
	e := newEnv(t)
	dl := d(2027, 6, 30)
	cases := map[string]goals.Input{
		"no name":                {Name: "  ", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Deadline: dl},
		"unknown kind":           {Name: "x", Kind: "spend", TargetMinor: ptr(int64(1)), Deadline: dl},
		"save without a target":  {Name: "x", Kind: goals.KindSave, Deadline: dl},
		"save with a category":   {Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), CategoryID: &e.food, Deadline: dl},
		"reduce without one":     {Name: "x", Kind: goals.KindReduce, ReducePct: ptr(10), Deadline: dl},
		"reduce by 0 %":          {Name: "x", Kind: goals.KindReduce, CategoryID: &e.food, ReducePct: ptr(0), Deadline: dl},
		"reduce on an account":   {Name: "x", Kind: goals.KindReduce, CategoryID: &e.food, ReducePct: ptr(10), AccountID: &e.account, Deadline: dl},
		"deadline before start":  {Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Deadline: d(2026, 11, 6)},
		"deadline too far":       {Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Deadline: d(2037, 1, 1)},
		"baseline over 12":       {Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), BaselineMonths: 13, Deadline: dl},
		"an unknown status":      {Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Status: "paused", Deadline: dl},
		"name over 80 character": {Name: string(make([]rune, 81)), Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Deadline: dl},
	}
	for name, in := range cases {
		if _, err := e.s.Create(e.ctx, e.u, in); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for i := range goals.MaxGoals {
		e.save(t, goals.Input{Name: "g", Kind: goals.KindSave, TargetMinor: ptr(int64(i + 1)), Deadline: dl})
	}
	if _, err := e.s.Create(e.ctx, e.u, goals.Input{Name: "g", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), Deadline: dl}); !errors.Is(err, platform.ErrLimitReached) {
		t.Errorf("51st goal: %v", err)
	}
}

func TestGoalsTenancy(t *testing.T) {
	e := newEnv(t)
	g := e.save(t, goals.Input{Name: "Mine", Kind: goals.KindReduce, CategoryID: &e.food, ReducePct: ptr(20), Deadline: d(2027, 4, 30)})
	other := newUser(t, e.st)
	if _, err := e.s.Get(e.ctx, other, g.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, err := e.s.Update(e.ctx, other, g.ID, goals.Input{Name: "x", Deadline: d(2027, 4, 30), CategoryID: &e.food, ReducePct: ptr(1), Version: 1}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("update: %v", err)
	}
	if err := e.s.Delete(e.ctx, other, g.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("delete: %v", err)
	}
	if list, err := e.s.List(e.ctx, other); err != nil || len(list) != 0 {
		t.Errorf("list %v %v", err, list)
	}
	// another user's category or account can't be named
	if _, err := e.s.Create(e.ctx, other, goals.Input{Name: "x", Kind: goals.KindReduce, CategoryID: &e.food, ReducePct: ptr(20), Deadline: d(2027, 4, 30)}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("their category: %v", err)
	}
	if _, err := e.s.Create(e.ctx, other, goals.Input{Name: "x", Kind: goals.KindSave, TargetMinor: ptr(int64(1)), AccountID: &e.account, Deadline: d(2027, 4, 30)}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("their account: %v", err)
	}
}

// TestGoalAIChanges: a goal the assistant created or changed is recorded and can be undone while
// nobody changed it since (ADR 0030).
func TestGoalAIChanges(t *testing.T) {
	e := newEnv(t)
	undo := aichanges.New(e.st, e.l, platform.FixedClock{T: time.Date(2026, 11, 6, 13, 0, 0, 0, time.UTC)})
	chat := platform.NewID()
	ai := func(key, action string) context.Context {
		return ledger.WithAIOrigin(e.ctx, ledger.AIOrigin{ChatID: chat, ProposalKey: key, Action: action})
	}
	changeOf := func(key string) uuid.UUID {
		t.Helper()
		page, err := undo.List(e.ctx, e.u, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range page.Changes {
			if c.ProposalKey == key && c.RevertedAt == nil {
				return c.ID
			}
		}
		t.Fatalf("no open change for %s", key)
		return uuid.Nil
	}

	created, err := e.s.Create(ai("00000000000000a1", "create_goal"), e.u, goals.Input{Name: "Auto", Kind: goals.KindSave,
		TargetMinor: ptr(int64(500_000)), Deadline: d(2027, 6, 30)})
	if err != nil || !created.AI {
		t.Fatalf("created %v %+v", err, created)
	}
	if _, err := undo.Revert(e.ctx, e.u, changeOf("00000000000000a1"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Get(e.ctx, e.u, created.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("undone create left the goal: %v", err)
	}

	g := e.save(t, goals.Input{Name: "Mine", Kind: goals.KindSave, TargetMinor: ptr(int64(500_000)), Deadline: d(2027, 6, 30)})
	upd := goals.Input{Name: "Mine", TargetMinor: ptr(int64(800_000)), Deadline: d(2027, 12, 31), Version: 1}
	if g, err = e.s.Update(ai("00000000000000a2", "update_goal"), e.u, g.ID, upd); err != nil || !g.AI || *g.TargetMinor != 800_000 {
		t.Fatalf("ai update %v %+v", err, g)
	}
	if _, err := undo.Revert(e.ctx, e.u, changeOf("00000000000000a2"), nil); err != nil {
		t.Fatal(err)
	}
	if g, err = e.s.Get(e.ctx, e.u, g.ID); err != nil || *g.TargetMinor != 500_000 || g.Deadline != d(2027, 6, 30) || g.AI {
		t.Errorf("after undo %v %+v", err, g)
	}

	// changed by hand after the assistant: nothing is undone
	upd.Version = g.Version
	if g, err = e.s.Update(ai("00000000000000a3", "update_goal"), e.u, g.ID, upd); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Update(e.ctx, e.u, g.ID, goals.Input{Name: "Renamed", TargetMinor: ptr(int64(800_000)), Deadline: d(2027, 12, 31), Version: g.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := undo.Revert(e.ctx, e.u, changeOf("00000000000000a3"), nil); !errors.Is(err, platform.ErrChangedSince) {
		t.Errorf("revert after a manual change: %v", err)
	}
}
