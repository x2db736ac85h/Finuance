package rules_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
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
	q   *query.Engine
	u   uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.New(testutil.Require(t, testDB).App)
	clk := platform.FixedClock{T: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	e := &env{t: t, ctx: context.Background(), st: st, l: ledger.New(st, clk), r: rules.New(st, clk), q: query.New(st), u: platform.NewID()}
	if err := st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: e.u, Email: e.u.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func noop(context.Context, pgx.Tx) error { return nil }

const header = "Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n"

// importInto commits a CSV (header added) into the account through the real import path.
func (e *env) importInto(acc uuid.UUID, name, lines string) uuid.UUID {
	e.t.Helper()
	p, err := e.l.CreatePreview(e.ctx, e.u, ledger.Upload{Filename: name + ".csv", Content: []byte(header + lines), AccountID: &acc})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.StartCommit(e.ctx, e.u, p.Import.ID, noop); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, e.u, p.Import.ID); err != nil {
		e.t.Fatal(err)
	}
	return p.Import.ID
}

func (e *env) account(name string) uuid.UUID {
	e.t.Helper()
	a, err := e.l.CreateAccount(e.ctx, e.u, ledger.AccountInput{Name: name})
	if err != nil {
		e.t.Fatal(err)
	}
	return a.ID
}

func (e *env) reclassify() rules.Result {
	e.t.Helper()
	res, err := e.r.Reclassify(e.ctx, e.u)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

// row is one transaction's classification, found by its payee.
type row struct {
	id                     uuid.UUID
	category, source, rule string
	transfer               bool
	transferSrc            string
	peer                   *uuid.UUID
}

func (e *env) rows() map[string]row {
	e.t.Helper()
	out := map[string]row{}
	err := e.st.WithUser(e.ctx, e.u, func(_ *db.Queries, tx pgx.Tx) error {
		rs, err := tx.Query(e.ctx, `SELECT t.id, t.payee || '/' || t.amount_minor, coalesce(c.name, ''), t.category_source, coalesce(r.name, ''),
			t.is_transfer, t.transfer_source, t.transfer_peer_id
			FROM transactions t LEFT JOIN categories c ON c.id = t.category_id LEFT JOIN rules r ON r.id = t.category_rule_id`)
		if err != nil {
			return err
		}
		for rs.Next() {
			var k string
			var x row
			if err := rs.Scan(&x.id, &k, &x.category, &x.source, &x.rule, &x.transfer, &x.transferSrc, &x.peer); err != nil {
				return err
			}
			out[k] = x
		}
		return rs.Err()
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) category(name string) uuid.UUID {
	e.t.Helper()
	c, err := e.l.CreateCategory(e.ctx, e.u, name)
	if err != nil {
		e.t.Fatal(err)
	}
	return c.ID
}

func (e *env) rule(name string, m rules.Match, cat *uuid.UUID, transfer bool) rules.Rule {
	e.t.Helper()
	r, err := e.r.Create(e.ctx, e.u, rules.Input{Name: name, Enabled: true, Match: m, SetCategoryID: cat, MarkTransfer: transfer}, noop)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func want(t *testing.T, got row, category, source string) {
	t.Helper()
	if got.category != category || got.source != source {
		t.Errorf("category = %q (%s), want %q (%s)", got.category, got.source, category, source)
	}
}

// TestRulePrecedence: manual > rule > bank; rules apply in position order and the last match
// wins; every change gives the same result as applying the current rules from scratch.
func TestRulePrecedence(t *testing.T) {
	e := newEnv(t)
	acc := e.account("Konto")
	e.importInto(acc, "jan", "02.01.2025;KAWIARNIA ZIARNO;Kawa;-12,00;Restauracje\n"+
		"03.01.2025;KAWIARNIA ZIARNO;Kawa z ciastkiem;-18,00;Restauracje\n"+
		"04.01.2025;ZABKA;Zakupy;-30,00;\n")
	coffee, snacks := e.category("Kawa"), e.category("Przekąski")
	rows := e.rows()
	manual := rows["KAWIARNIA ZIARNO/-1800"].id
	if _, err := e.l.Annotate(e.ctx, e.u, []uuid.UUID{manual}, ledger.AnnotationPatch{SetCategory: true, CategoryID: &snacks}); err != nil {
		t.Fatal(err)
	}
	r1 := e.rule("Kawiarnie", rules.Match{Filters: []query.Filter{{Column: "payee", Cond: "kawiarnia"}}}, &coffee, false)
	r2 := e.rule("Ziarno", rules.Match{Search: "ziarno"}, &snacks, false)
	if res := e.reclassify(); res.Categorised != 2 {
		t.Errorf("categorised = %d, want 2 (one row per rule, the manual one skipped)", res.Categorised)
	}
	rows = e.rows()
	want(t, rows["KAWIARNIA ZIARNO/-1200"], "Przekąski", "rule") // r2 comes later: it wins
	if rows["KAWIARNIA ZIARNO/-1200"].rule != "Ziarno" {
		t.Errorf("rule = %q, want Ziarno", rows["KAWIARNIA ZIARNO/-1200"].rule)
	}
	want(t, rows["KAWIARNIA ZIARNO/-1800"], "Przekąski", "manual")
	want(t, rows["ZABKA/-3000"], "", "none")

	if _, err := e.r.Reorder(e.ctx, e.u, []uuid.UUID{r2.ID, r1.ID}, noop); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	want(t, e.rows()["KAWIARNIA ZIARNO/-1200"], "Kawa", "rule")

	in := rules.Input{Name: r1.Name, Enabled: false, Match: r1.Match, SetCategoryID: r1.SetCategoryID}
	if _, err := e.r.Update(e.ctx, e.u, r1.ID, in, noop); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	want(t, e.rows()["KAWIARNIA ZIARNO/-1200"], "Przekąski", "rule")

	if err := e.r.Delete(e.ctx, e.u, r2.ID, noop); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	want(t, e.rows()["KAWIARNIA ZIARNO/-1200"], "Restauracje", "bank") // back to the bank's category
	want(t, e.rows()["KAWIARNIA ZIARNO/-1800"], "Przekąski", "manual")
}

// TestRulesSurviveReimport: rule categories are recomputed after a delete + re-import.
func TestRulesSurviveReimport(t *testing.T) {
	e := newEnv(t)
	acc := e.account("Konto")
	lines := "02.01.2025;ZABKA;Zakupy;-30,00;\n"
	imp := e.importInto(acc, "jan", lines)
	groceries := e.category("Spożywcze")
	e.rule("Żabka", rules.Match{Filters: []query.Filter{{Column: "payee", Cond: "=zabka"}}}, &groceries, false)
	e.reclassify()
	if _, err := e.l.DeleteImport(e.ctx, e.u, imp); err != nil {
		t.Fatal(err)
	}
	e.importInto(acc, "jan", lines)
	want(t, e.rows()["ZABKA/-3000"], "", "none") // before the job runs
	e.reclassify()
	want(t, e.rows()["ZABKA/-3000"], "Spożywcze", "rule")
}

func TestRuleValidation(t *testing.T) {
	e := newEnv(t)
	cat := e.category("A")
	other := newEnv(t)
	foreign := other.category("B")
	for name, in := range map[string]rules.Input{
		"no name":          {Match: rules.Match{Search: "x"}, SetCategoryID: &cat},
		"matches all":      {Name: "x", SetCategoryID: &cat},
		"empty filters":    {Name: "x", Match: rules.Match{Filters: []query.Filter{{Column: "payee"}}}, SetCategoryID: &cat},
		"no action":        {Name: "x", Match: rules.Match{Search: "x"}},
		"unknown column":   {Name: "x", Match: rules.Match{Filters: []query.Filter{{Column: "nope", Cond: "x"}}}, SetCategoryID: &cat},
		"another's target": {Name: "x", Match: rules.Match{Search: "x"}, SetCategoryID: &foreign},
	} {
		if _, err := e.r.Create(e.ctx, e.u, in, noop); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	r := e.rule("ok", rules.Match{Search: "x"}, &cat, false)
	for name, ids := range map[string][]uuid.UUID{"missing": {}, "twice": {r.ID, r.ID}, "foreign": {platform.NewID()}} {
		if _, err := e.r.Reorder(e.ctx, e.u, ids, noop); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("reorder %s: %v, want ErrInvalid", name, err)
		}
	}
	if _, err := other.r.Update(other.ctx, other.u, r.ID, rules.Input{Name: "x", Match: rules.Match{Search: "x"}, MarkTransfer: true}, noop); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("updating another user's rule: %v, want ErrNotFound", err)
	}
	if err := other.r.Delete(other.ctx, other.u, r.ID, noop); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("deleting another user's rule: %v, want ErrNotFound", err)
	}
	enqueued := 0
	count := func(context.Context, pgx.Tx) error { enqueued++; return nil }
	if _, err := e.r.Create(e.ctx, e.u, rules.Input{Name: "y", Match: rules.Match{Search: "y"}, MarkTransfer: true}, count); err != nil {
		t.Fatal(err)
	}
	if enqueued != 1 {
		t.Errorf("enqueued %d reclassifications, want 1", enqueued)
	}
}

func TestPreview(t *testing.T) {
	e := newEnv(t)
	acc := e.account("Konto")
	e.importInto(acc, "jan", "02.01.2025;ZABKA;Zakupy;-30,00;\n03.01.2025;ZABKA;Zakupy;-31,00;\n04.01.2025;LIDL;Zakupy;-40,00;\n")
	cat := e.category("A")
	id := e.rows()["ZABKA/-3100"].id
	if _, err := e.l.Annotate(e.ctx, e.u, []uuid.UUID{id}, ledger.AnnotationPatch{SetCategory: true, CategoryID: &cat}); err != nil {
		t.Fatal(err)
	}
	p, err := rules.Preview(e.ctx, e.q, e.u, rules.Match{Search: "zabka"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Matched != 2 || p.Manual != 1 || len(p.Sample) != 2 || p.Sample[0].AmountMinor != -3100 {
		t.Errorf("preview = %d matched, %d manual, %d rows, want 2, 1, 2 (newest first)", p.Matched, p.Manual, len(p.Sample))
	}
	if _, err := rules.Preview(e.ctx, e.q, e.u, rules.Match{}); err == nil {
		t.Error("an empty match previews everything")
	}
}

// TestTransferDetection covers the pairing rules: opposite amounts, another account of the same
// user, at most 3 days apart, same currency, closest dates first, each row in one pair at most.
func TestTransferDetection(t *testing.T) {
	e := newEnv(t)
	a, b, c := e.account("A"), e.account("B"), e.account("C")
	e.importInto(a, "a", strings.Join([]string{
		"02.01.2025;PRZELEW WLASNY;Na oszczędności;-500,00;",
		"10.01.2025;PRZELEW;Czynsz;-300,00;",
		"05.01.2025;PRZELEW TRZY;Trzy konta;-100,00;",
		"20.01.2025;PRZELEW SAM;To samo konto;-70,00;",
		"20.01.2025;ZWROT SAM;To samo konto;70,00;",
		"",
	}, "\n"))
	e.importInto(b, "b", strings.Join([]string{
		"04.01.2025;PRZELEW WLASNY;Z konta A;500,00;",
		"14.01.2025;PRZELEW;Za daleko;300,00;", // 4 days: no pair
		"05.01.2025;WPLYW B;Trzy konta;100,00;",
		"",
	}, "\n"))
	e.importInto(c, "c", "05.01.2025;WPLYW C;Trzy konta;100,00;\n")
	if res := e.reclassify(); res.Pairs != 2 {
		t.Errorf("pairs = %d, want 2", res.Pairs)
	}
	rows := e.rows()
	out, in := rows["PRZELEW WLASNY/-50000"], rows["PRZELEW WLASNY/50000"]
	if !out.transfer || !in.transfer || out.transferSrc != "pair" || out.peer == nil || *out.peer != in.id || *in.peer != out.id {
		t.Errorf("the 500 transfer isn't paired both ways: %+v %+v", out, in)
	}
	if rows["PRZELEW/-30000"].transfer || rows["PRZELEW/30000"].transfer {
		t.Error("4 days apart were paired")
	}
	if rows["PRZELEW SAM/-7000"].transfer {
		t.Error("two rows of one account were paired")
	}
	// three accounts, same day and amount: B's row is older (imported first), so it wins
	if !rows["WPLYW B/10000"].transfer || rows["WPLYW C/10000"].transfer {
		t.Errorf("the tie went to the wrong row: B %v, C %v", rows["WPLYW B/10000"].transfer, rows["WPLYW C/10000"].transfer)
	}
	// a second run changes nothing
	if res := e.reclassify(); res.Pairs != 0 {
		t.Errorf("a second run found %d new pairs", res.Pairs)
	}
	// transfers are left out unless asked for
	page := func(include bool) int {
		p, err := e.q.Page(e.ctx, e.u, query.PageRequest{Context: query.Context{IncludeTransfers: include}, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		return p.Total
	}
	if all, without := page(true), page(false); all != 9 || without != 5 {
		t.Errorf("rows with/without transfers = %d/%d, want 9/5", all, without)
	}
}

// TestManualTransfers: "not a transfer" unpairs both sides and outlives re-imports; "automatic"
// hands the rows back to the detection; deleting one side's import releases the other.
func TestManualTransfers(t *testing.T) {
	e := newEnv(t)
	a, b := e.account("A"), e.account("B")
	e.importInto(a, "a", "02.01.2025;PRZELEW;Out;-500,00;\n")
	impB := e.importInto(b, "b", "03.01.2025;PRZELEW;In;500,00;\n")
	e.reclassify()
	rows := e.rows()
	out := rows["PRZELEW/-50000"]
	no := false
	if n, err := e.l.SetTransfer(e.ctx, e.u, []uuid.UUID{out.id}, &no); err != nil || n != 2 {
		t.Fatalf("unpair: %d, %v; want both sides", n, err)
	}
	e.reclassify()
	for k, r := range e.rows() {
		if r.transfer || r.transferSrc != "manual" || r.peer != nil {
			t.Errorf("%s after unpairing: %+v", k, r)
		}
	}
	// the decision survives deleting and re-importing B
	if _, err := e.l.DeleteImport(e.ctx, e.u, impB); err != nil {
		t.Fatal(err)
	}
	impB = e.importInto(b, "b", "03.01.2025;PRZELEW;In;500,00;\n")
	e.reclassify()
	if r := e.rows()["PRZELEW/50000"]; r.transfer || r.transferSrc != "manual" {
		t.Errorf("after re-import: %+v, want still not a transfer", r)
	}
	// automatic again: paired on the next run
	ids := []uuid.UUID{e.rows()["PRZELEW/-50000"].id, e.rows()["PRZELEW/50000"].id}
	if _, err := e.l.SetTransfer(e.ctx, e.u, ids, nil); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	if r := e.rows()["PRZELEW/50000"]; !r.transfer || r.transferSrc != "pair" {
		t.Errorf("automatic again: %+v, want paired", r)
	}
	// deleting B's import leaves A's half without a peer: it is released on the next run
	if _, err := e.l.DeleteImport(e.ctx, e.u, impB); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	if r := e.rows()["PRZELEW/-50000"]; r.transfer || r.transferSrc != "none" {
		t.Errorf("orphaned half: %+v, want unmarked", r)
	}
	// "yes" marks a row by hand; a rule's transfer action marks unmarked rows only
	yes := true
	if _, err := e.l.SetTransfer(e.ctx, e.u, []uuid.UUID{e.rows()["PRZELEW/-50000"].id}, &yes); err != nil {
		t.Fatal(err)
	}
	if r := e.rows()["PRZELEW/-50000"]; !r.transfer || r.transferSrc != "manual" {
		t.Errorf("marked by hand: %+v", r)
	}
}

func TestRuleMarksTransfers(t *testing.T) {
	e := newEnv(t)
	acc := e.account("Konto")
	e.importInto(acc, "jan", "02.01.2025;MAKLER;Wpłata na rachunek maklerski;-1000,00;\n03.01.2025;ZABKA;Zakupy;-30,00;\n")
	r := e.rule("Makler", rules.Match{Filters: []query.Filter{{Column: "payee", Cond: "makler"}}}, nil, true)
	if res := e.reclassify(); res.RuleTransfers != 1 {
		t.Errorf("rule transfers = %d, want 1", res.RuleTransfers)
	}
	if m := e.rows()["MAKLER/-100000"]; !m.transfer || m.transferSrc != "rule" {
		t.Errorf("rule transfer: %+v", m)
	}
	if err := e.r.Delete(e.ctx, e.u, r.ID, noop); err != nil {
		t.Fatal(err)
	}
	e.reclassify()
	if m := e.rows()["MAKLER/-100000"]; m.transfer {
		t.Error("the deleted rule's transfer mark stayed")
	}
}

func TestReclassifyTenancy(t *testing.T) {
	e, other := newEnv(t), newEnv(t)
	acc := other.account("Konto")
	other.importInto(acc, "jan", "02.01.2025;ZABKA;Zakupy;-30,00;\n")
	cat := e.category("A")
	e.rule("All Żabka", rules.Match{Search: "zabka"}, &cat, true)
	e.reclassify()
	if r := other.rows()["ZABKA/-3000"]; r.source != "none" || r.transfer {
		t.Errorf("another user's rule changed this user's row: %+v", r)
	}
	if rs, err := other.r.List(other.ctx, other.u); err != nil || len(rs) != 0 {
		t.Errorf("another user sees %d rules (%v)", len(rs), err)
	}
}
