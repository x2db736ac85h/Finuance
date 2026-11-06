package aichanges_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/aichanges"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

type env struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	l   *ledger.Ledger
	r   *rules.Service
	s   *aichanges.Service
	u   uuid.UUID
	ids []uuid.UUID // the three transactions: Biedronka, Orlen, Kino
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.New(testutil.Require(t, testDB).App)
	clk := platform.FixedClock{T: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	l := ledger.New(st, clk)
	e := &env{t: t, ctx: context.Background(), st: st, l: l, r: rules.New(st, clk), s: aichanges.New(st, l, clk), u: platform.NewID()}
	if err := st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: e.u, Email: e.u.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	acc, err := l.CreateAccount(e.ctx, e.u, ledger.AccountInput{Name: "Konto"})
	if err != nil {
		t.Fatal(err)
	}
	csv := "Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n" +
		"2026-11-01;BIEDRONKA;zakupy;-50,00;Jedzenie\n2026-11-02;ORLEN;paliwo;-200,00;Transport\n2026-11-03;KINO;bilety;-40,00;\n"
	p, err := l.CreatePreview(e.ctx, e.u, ledger.Upload{Filename: "a.csv", Content: []byte(csv), AccountID: &acc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.StartCommit(e.ctx, e.u, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CommitImport(e.ctx, e.u, p.Import.ID); err != nil {
		t.Fatal(err)
	}
	e.ids = e.column(`SELECT id::text FROM transactions ORDER BY booked_on`)
	return e
}

// column reads one uuid column under the user's context.
func (e *env) column(sql string) []uuid.UUID {
	e.t.Helper()
	var out []uuid.UUID
	err := e.st.WithUser(e.ctx, e.u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, uuid.MustParse(s))
		}
		return rows.Err()
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// state is a transaction's category name, its source and its group name.
func (e *env) state(id uuid.UUID) (cat, source, group string) {
	e.t.Helper()
	err := e.st.WithUser(e.ctx, e.u, func(_ *db.Queries, tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT coalesce(c.name, ''), t.category_source, coalesce(g.name, '')
			FROM transactions t LEFT JOIN categories c ON c.id = t.category_id LEFT JOIN groups g ON g.id = t.group_id
			WHERE t.id = $1`, id).Scan(&cat, &source, &group)
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return
}

// ai is a context whose writes are the proposal key's (a fresh chat each time).
func (e *env) ai(action, key string) context.Context {
	return ledger.WithAIOrigin(e.ctx, ledger.AIOrigin{ChatID: platform.NewID(), ProposalKey: key, Action: action})
}

func (e *env) changes() []aichanges.Change {
	e.t.Helper()
	p, err := e.s.List(e.ctx, e.u, "", 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return p.Changes
}

func TestAIChangeRecordedWithTheChange(t *testing.T) {
	e := newEnv(t)
	ctx := e.ai("set_group", "00000000000000a1")
	g, err := e.l.CreateGroup(ctx, e.u, "Wakacje", -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.Annotate(ctx, e.u, e.ids[:2], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	cs := e.changes()
	if len(cs) != 1 || cs[0].Action != "set_group" || cs[0].Items != 3 || cs[0].RevertedAt != nil {
		t.Fatalf("changes = %+v (both requests of one Apply add to one change: the group and two rows)", cs)
	}
	// an ordinary edit records nothing
	if _, err := e.l.Annotate(e.ctx, e.u, e.ids[2:], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	// a write that fails leaves no record: another user's group is refused inside the transaction
	other := newEnv(t)
	og, err := other.l.CreateGroup(other.ctx, other.u, "Obca", -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.Annotate(e.ai("set_group", "00000000000000a2"), e.u, e.ids[:1], ledger.AnnotationPatch{SetGroup: true, GroupID: &og.ID}); err == nil {
		t.Fatal("another user's group was accepted")
	}
	if cs := e.changes(); len(cs) != 1 {
		t.Errorf("changes after a failed write = %d, want 1", len(cs))
	}
	// nobody else sees the change, nor can undo it
	if p, err := other.s.List(other.ctx, other.u, "", 0); err != nil || len(p.Changes) != 0 {
		t.Errorf("another user sees %+v, %v", p.Changes, err)
	}
	if _, err := other.s.Revert(other.ctx, other.u, cs[0].ID, nil); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another user's revert: %v", err)
	}
}

func TestRevertCategories(t *testing.T) {
	e := newEnv(t)
	jedzenie := e.column(`SELECT id::text FROM categories WHERE name = 'Jedzenie'`)[0]
	if _, err := e.l.Annotate(e.ctx, e.u, e.ids[2:], ledger.AnnotationPatch{SetCategory: true, CategoryID: &jedzenie}); err != nil {
		t.Fatal(err) // Kino: a category set by hand before the assistant came
	}
	ctx := e.ai("set_category", "00000000000000b1")
	fun, err := e.l.CreateCategory(ctx, e.u, "Rozrywka")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.Annotate(ctx, e.u, e.ids, ledger.AnnotationPatch{SetCategory: true, CategoryID: &fun.ID}); err != nil {
		t.Fatal(err)
	}
	for _, id := range e.ids {
		if c, src, _ := e.state(id); c != "Rozrywka" || src != "manual" {
			t.Fatalf("after the change: %s %s", c, src)
		}
	}
	res, err := e.s.Revert(e.ctx, e.u, e.changes()[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 3 || res.Deleted != 1 || res.Kept != 0 || !res.Reclassify || res.Change.RevertedAt == nil {
		t.Errorf("result = %+v", res)
	}
	// the bank's categories come back, the one chosen by hand too; the new category is gone
	for i, want := range [][2]string{{"Jedzenie", "bank"}, {"Transport", "bank"}, {"Jedzenie", "manual"}} {
		if c, src, _ := e.state(e.ids[i]); c != want[0] || src != want[1] {
			t.Errorf("row %d = %s %s, want %v", i, c, src, want)
		}
	}
	if n := e.column(`SELECT id::text FROM categories WHERE name = 'Rozrywka'`); len(n) != 0 {
		t.Error("the category the assistant created is still there")
	}
	if _, err := e.s.Revert(e.ctx, e.u, res.Change.ID, nil); !errors.Is(err, ledger.ErrState) {
		t.Errorf("second revert: %v", err)
	}
}

func TestRevertRefusedWhenChangedSince(t *testing.T) {
	e := newEnv(t)
	ctx := e.ai("set_group", "00000000000000c1")
	g, err := e.l.CreateGroup(ctx, e.u, "Wakacje", -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.Annotate(ctx, e.u, e.ids[:2], ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}
	// the user moves one row out again: undoing now would overwrite their decision
	if _, err := e.l.Annotate(e.ctx, e.u, e.ids[1:2], ledger.AnnotationPatch{SetGroup: true, GroupID: nil}); err != nil {
		t.Fatal(err)
	}
	id := e.changes()[0].ID
	if _, err := e.s.Revert(e.ctx, e.u, id, nil); !errors.Is(err, platform.ErrChangedSince) {
		t.Fatalf("revert after an edit: %v", err)
	}
	if _, _, grp := e.state(e.ids[0]); grp != "Wakacje" {
		t.Errorf("a refused revert changed row 0: %q", grp)
	}
	// put back as the assistant left it, and add another row by hand: the revert goes through,
	// the user's own row keeps the group, so the group stays
	for _, ids := range [][]uuid.UUID{e.ids[1:2], e.ids[2:]} {
		if _, err := e.l.Annotate(e.ctx, e.u, ids, ledger.AnnotationPatch{SetGroup: true, GroupID: &g.ID}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.s.Revert(e.ctx, e.u, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 || res.Deleted != 0 || res.Kept != 1 || res.Reclassify {
		t.Errorf("result = %+v", res)
	}
	for i, want := range []string{"", "", "Wakacje"} {
		if _, _, grp := e.state(e.ids[i]); grp != want {
			t.Errorf("row %d group = %q, want %q", i, grp, want)
		}
	}
}

func TestRevertRule(t *testing.T) {
	e := newEnv(t)
	cat := e.column(`SELECT id::text FROM categories WHERE name = 'Transport'`)[0]
	in := rules.Input{Name: "Kino", Enabled: true, Match: rules.Match{Search: "kino"}, SetCategoryID: &cat}
	noop := func(context.Context, pgx.Tx) error { return nil }
	r, err := e.r.Create(e.ai("create_rule", "00000000000000d1"), e.u, in, noop)
	if err != nil {
		t.Fatal(err)
	}
	queued := 0
	enqueue := func(context.Context, pgx.Tx) error { queued++; return nil }
	res, err := e.s.Revert(e.ctx, e.u, e.changes()[0].ID, enqueue)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || !res.Reclassify || queued != 1 {
		t.Errorf("result = %+v, reclassifications queued = %d", res, queued)
	}
	if left := e.column(`SELECT id::text FROM rules`); len(left) != 0 {
		t.Errorf("rules left: %v", left)
	}
	// a rule edited since is the user's now
	r, err = e.r.Create(e.ai("create_rule", "00000000000000d2"), e.u, in, noop)
	if err != nil {
		t.Fatal(err)
	}
	in.Name = "Kino (moje)"
	if _, err := e.r.Update(e.ctx, e.u, r.ID, in, noop); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Revert(e.ctx, e.u, e.changes()[0].ID, enqueue); !errors.Is(err, platform.ErrChangedSince) {
		t.Errorf("revert of an edited rule: %v", err)
	}
}
