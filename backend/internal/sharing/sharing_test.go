package sharing_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
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
	f   *friends.Service
	n   *notify.Service
	s   *sharing.Service
	r   *rules.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.New(testutil.Require(t, testDB).App)
	clk := platform.FixedClock{T: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)}
	n := notify.New(st, clk)
	l := ledger.New(st, clk)
	return &env{t: t, ctx: context.Background(), st: st, l: l, f: friends.New(st, clk, n), n: n,
		s: sharing.New(st, l, n, clk), r: rules.New(st, clk)}
}

func (e *env) user(name string) uuid.UUID {
	e.t.Helper()
	id := platform.NewID()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(e.ctx, db.CreateUserParams{ID: id, Email: id.String() + "@example.test", PasswordHash: "x", DisplayName: name})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) befriend(a, b uuid.UUID) {
	e.t.Helper()
	c, err := e.f.NewCode(e.ctx, a)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.f.Redeem(e.ctx, b, c.Code); err != nil {
		e.t.Fatal(err)
	}
}

func noop(context.Context, pgx.Tx) error { return nil }

// importInto commits CSV lines into a new bank account and returns the account and the new
// transactions' ids in file order.
func (e *env) importInto(u uuid.UUID, account string, lines ...string) (uuid.UUID, []uuid.UUID) {
	e.t.Helper()
	acc, err := e.l.CreateAccount(e.ctx, u, ledger.AccountInput{Name: account})
	if err != nil {
		e.t.Fatal(err)
	}
	return acc.ID, e.importLines(u, acc.ID, account, lines...)
}

func (e *env) importLines(u, account uuid.UUID, name string, lines ...string) []uuid.UUID {
	e.t.Helper()
	content := "Data operacji;Kontrahent;Tytuł;Kwota;Kategoria\n" + strings.Join(lines, "\n") + "\n"
	p, err := e.l.CreatePreview(e.ctx, u, ledger.Upload{Filename: name + ".csv", Content: []byte(content), AccountID: &account})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.StartCommit(e.ctx, u, p.Import.ID, noop); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.l.CommitImport(e.ctx, u, p.Import.ID); err != nil {
		e.t.Fatal(err)
	}
	var ids []uuid.UUID
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id FROM transactions WHERE user_id = $1 AND account_id = $2 ORDER BY seq`, u, account)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func (e *env) group(u uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	g, err := e.l.CreateGroup(e.ctx, u, name, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return g.ID
}

func (e *env) put(u uuid.UUID, group *uuid.UUID, ids ...uuid.UUID) error {
	_, err := e.l.Annotate(e.ctx, u, ids, ledger.AnnotationPatch{SetGroup: true, GroupID: group})
	return err
}

func (e *env) detail(u, g uuid.UUID) sharing.Detail {
	e.t.Helper()
	d, err := e.s.Get(e.ctx, u, g)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func sum(b map[uuid.UUID]int64) int64 {
	var t int64
	for _, v := range b {
		t += v
	}
	return t
}

func isErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func memberOf(d sharing.Detail, name string) sharing.Member {
	for _, m := range d.Members {
		if m.DisplayName == name {
			return m
		}
	}
	return sharing.Member{}
}

// TestSharedTrip: Alex shares a personal group with Bo; both add expenses (Bo's from a typed cash
// row and from an imported transaction); balances sum to zero; settling up brings them to zero;
// Bo leaves only then.
func TestSharedTrip(t *testing.T) {
	e := newEnv(t)
	alex, bo, cy := e.user("Alex"), e.user("Bo"), e.user("Cy")
	_, ids := e.importInto(alex, "Konto A",
		"02.05.2026;HOTEL MARE;Nocleg;-600,00;Podróże",
		"03.05.2026;RESTAURACJA;Kolacja;-120,00;Jedzenie",
		"04.05.2026;APTEKA;Leki;-30,00;Zdrowie")
	trip := e.group(alex, "Włochy")
	if err := e.put(alex, &trip, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	g, err := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Włochy", Currency: "PLN", Kind: "trip", FromGroupID: &trip})
	if err != nil {
		t.Fatal(err)
	}
	d := e.detail(alex, g.ID)
	if len(d.Expenses) != 2 || d.Expenses[0].AmountMinor+d.Expenses[1].AmountMinor != 72000 || !d.Expenses[0].Mine {
		t.Fatalf("copies after sharing = %+v", d.Expenses)
	}
	for _, x := range d.Expenses {
		if x.Origin != "transaction" || x.Category == "" || strings.Contains(x.Payee, "Konto") {
			t.Errorf("copy = %+v", x)
		}
	}

	// friends only
	_, err = e.s.Invite(e.ctx, alex, g.ID, bo)
	isErr(t, err, ledger.ErrInvalid)
	e.befriend(alex, bo)
	e.befriend(alex, cy)
	if _, err := e.s.Invite(e.ctx, alex, g.ID, bo); err != nil {
		t.Fatal(err)
	}
	_, err = e.s.Invite(e.ctx, alex, g.ID, bo)
	isErr(t, err, ledger.ErrConflict)
	if _, err := e.s.Invite(e.ctx, alex, g.ID, cy); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Decline(e.ctx, cy, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Get(e.ctx, cy, g.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a declined user sees the group: %v", err)
	}
	// invited: the group and its members, not its expenses
	inv := e.detail(bo, g.ID)
	if inv.Group.Me.State != "invited" || len(inv.Expenses) != 0 || len(inv.Members) != 3 {
		t.Errorf("invited view = %+v", inv)
	}
	notes, _, _ := e.n.List(e.ctx, bo, 10)
	if len(notes) == 0 || notes[0].Kind != notify.SharedInvited || notes[0].Data["group"] != "Włochy" {
		t.Errorf("bo's notifications = %+v", notes)
	}
	if _, err := e.s.Accept(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	// Bo's personal group was created and linked: his own transaction goes in as a copy
	boGroups, err := e.l.ListGroups(e.ctx, bo)
	if err != nil || len(boGroups) != 1 || boGroups[0].SharedGroupID == nil || *boGroups[0].SharedGroupID != g.ID {
		t.Fatalf("bo's groups = %+v, %v", boGroups, err)
	}
	_, boIDs := e.importInto(bo, "Konto B", "03.05.2026;MUZEUM;Bilety;-80,00;Kultura")
	if err := e.put(bo, &boGroups[0].ID, boIDs[0]); err != nil {
		t.Fatal(err)
	}
	// and a typed cash expense, split equally
	if _, err := e.s.AddExpense(e.ctx, bo, g.ID, sharing.ExpenseInput{BookedOn: platform.Date{Y: 2026, M: 5, D: 4}, AmountMinor: 4000,
		Payee: "Lody"}); err != nil {
		t.Fatal(err)
	}
	d = e.detail(alex, g.ID)
	if len(d.Expenses) != 4 || sum(d.Balances) != 0 {
		t.Fatalf("expenses %d, balances %v", len(d.Expenses), d.Balances)
	}
	a, b := memberOf(d, "Alex").ID, memberOf(d, "Bo").ID
	// Alex's two copies were shared before Bo joined, with everyone (Alex): joining re-split them
	// between Alex and Bo. Alex paid 720 and owes 360 + 40 + 20; Bo paid 120 and owes the same 420.
	if d.Balances[a] != 30000 || d.Balances[b] != -30000 || len(d.Transfers) != 1 || d.Transfers[0].From != b || d.Transfers[0].Amount != 30000 {
		t.Errorf("balances %v transfers %+v", d.Balances, d.Transfers)
	}
	// Bo can't leave owing money; settle first (recorded by Alex, who received it)
	isErr(t, e.s.Leave(e.ctx, bo, g.ID), ledger.ErrState)
	if _, err := e.s.Settle(e.ctx, alex, g.ID, sharing.SettlementInput{From: b, To: a, AmountMinor: 30000, SettledOn: platform.Date{Y: 2026, M: 5, D: 10}}); err != nil {
		t.Fatal(err)
	}
	d = e.detail(bo, g.ID)
	if d.Balances[a] != 0 || d.Balances[b] != 0 || len(d.Transfers) != 0 {
		t.Errorf("after settling: %v %+v", d.Balances, d.Transfers)
	}
	// Bo can't change Alex's expenses, nor delete a copy through the group
	alexCopy := d.Expenses[len(d.Expenses)-1]
	if alexCopy.Mine {
		alexCopy = d.Expenses[0]
	}
	isErr(t, e.s.UpdateExpense(e.ctx, bo, g.ID, alexCopy.ID, sharing.ExpenseInput{Split: sharing.Split{Mode: "equal"}}), platform.ErrForbidden)
	if err := e.s.Leave(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Get(e.ctx, bo, g.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a member who left sees the group: %v", err)
	}
	if boGroups, _ = e.l.ListGroups(e.ctx, bo); boGroups[0].SharedGroupID != nil {
		t.Error("leaving kept Bo's group linked")
	}
	// what Bo shared stays in the group for the others
	if d = e.detail(alex, g.ID); len(d.Expenses) != 4 || memberOf(d, "Bo").State != "left" {
		t.Errorf("after Bo left: %d expenses, %+v", len(d.Expenses), d.Members)
	}
}

// TestCopiesFollowTheirTransactions: category, note and group changes reach the copy in the same
// transaction; deleting the import keeps the copy (its source is missing, for its owner only);
// re-importing finds it again; rules update its category; another currency is refused.
func TestCopiesFollowTheirTransactions(t *testing.T) {
	e := newEnv(t)
	alex := e.user("Alex")
	acc, ids := e.importInto(alex, "Konto", "02.05.2026;KAWIARNIA;Kawa;-15,00;", "02.05.2026;KINO;Bilet;-30,00;")
	trip := e.group(alex, "Trip")
	g, err := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Trip", Currency: "PLN", FromGroupID: &trip})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.put(alex, &trip, ids...); err != nil {
		t.Fatal(err)
	}
	cat := "Kawa i ciastka"
	if _, err := e.l.UpdateTransaction(e.ctx, alex, ids[0], ledger.TransactionPatch{CategoryName: &cat, Note: ptr("na dworcu")}); err != nil {
		t.Fatal(err)
	}
	d := e.detail(alex, g.ID)
	var coffee sharing.Expense
	for _, x := range d.Expenses {
		if x.Payee == "KAWIARNIA" {
			coffee = x
		}
	}
	if coffee.Category != cat || coffee.Note != "na dworcu" || coffee.AmountMinor != 1500 {
		t.Errorf("copy after edits = %+v", coffee)
	}
	// out of the group: the copy goes
	if err := e.put(alex, nil, ids[1]); err != nil {
		t.Fatal(err)
	}
	if d = e.detail(alex, g.ID); len(d.Expenses) != 1 {
		t.Fatalf("after removing one: %d copies", len(d.Expenses))
	}
	// delete the import: the copy stays, its source is missing
	imps, err := e.l.ListImports(e.ctx, alex, 10)
	if err != nil || len(imps) != 1 || imps[0].AccountID == nil || *imps[0].AccountID != acc {
		t.Fatal(imps, err)
	}
	if _, err := e.l.DeleteImport(e.ctx, alex, imps[0].ID); err != nil {
		t.Fatal(err)
	}
	if d = e.detail(alex, g.ID); len(d.Expenses) != 1 || !d.Expenses[0].SourceMissing {
		t.Fatalf("after deleting the import: %+v", d.Expenses)
	}
	// re-import: the same copy again (the group came back through the annotation)
	e.importLines(alex, acc, "again", "02.05.2026;KAWIARNIA;Kawa;-15,00;", "02.05.2026;KINO;Bilet;-30,00;")
	if d = e.detail(alex, g.ID); len(d.Expenses) != 1 || d.Expenses[0].ID != coffee.ID || d.Expenses[0].SourceMissing {
		t.Fatalf("after re-importing: %+v (was %s)", d.Expenses, coffee.ID)
	}
	// a rule's category reaches the copy
	tea := "Napoje"
	if _, err := e.l.UpdateTransaction(e.ctx, alex, e.idsOf(alex, acc)[0], ledger.TransactionPatch{ResetCategory: true}); err != nil {
		t.Fatal(err)
	}
	c, err := e.l.CreateCategory(e.ctx, alex, tea)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.Create(e.ctx, alex, rules.Input{Name: "kawa", Enabled: true,
		Match: rules.Match{Filters: []query.Filter{{Column: "payee", Cond: "kawiarnia"}}}, SetCategoryID: &c.ID}, noop); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.Reclassify(e.ctx, alex); err != nil {
		t.Fatal(err)
	}
	if d = e.detail(alex, g.ID); d.Expenses[0].Category != tea {
		t.Errorf("category after the rule = %q", d.Expenses[0].Category)
	}
	// another currency is refused
	eur, err := e.l.CreateAccount(e.ctx, alex, ledger.AccountInput{Name: "EUR", Currency: "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	eurIDs := e.importLines(alex, eur.ID, "eur", "05.05.2026;BAR;Kawa;-3,00;")
	err = e.put(alex, &trip, eurIDs[0])
	isErr(t, err, ledger.ErrSharedCurrency)
}

func (e *env) idsOf(u, account uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id FROM transactions WHERE user_id = $1 AND account_id = $2 ORDER BY seq`, u, account)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func ptr[T any](v T) *T { return &v }

// TestTypedCashCopiesResplit: a typed cash row in a linked group is shared, a new amount is split
// again by the same shares, and deleting the row deletes the copy.
func TestTypedCashCopiesResplit(t *testing.T) {
	e := newEnv(t)
	alex, bo := e.user("Alex"), e.user("Bo")
	e.befriend(alex, bo)
	g, err := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Dom", Currency: "PLN", Kind: "household"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Invite(e.ctx, alex, g.ID, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	wallet, err := e.l.CreateAccount(e.ctx, bo, ledger.AccountInput{Name: "Portfel", Kind: ledger.KindCash})
	if err != nil {
		t.Fatal(err)
	}
	boGroups, _ := e.l.ListGroups(e.ctx, bo)
	id, err := e.l.AddManual(e.ctx, bo, ledger.ManualInput{AccountID: wallet.ID, BookedOn: platform.Date{Y: 2026, M: 6, D: 1},
		AmountMinor: -9000, Payee: "Targ", GroupID: &boGroups[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	d := e.detail(bo, g.ID)
	if len(d.Expenses) != 1 || d.Expenses[0].AmountMinor != 9000 {
		t.Fatalf("copy = %+v", d.Expenses)
	}
	a, b := memberOf(d, "Alex").ID, memberOf(d, "Bo").ID
	if err := e.s.UpdateExpense(e.ctx, bo, g.ID, d.Expenses[0].ID, sharing.ExpenseInput{
		Split: sharing.Split{Mode: "shares", Members: []uuid.UUID{a, b}, Weights: []int{2, 1}}}); err != nil {
		t.Fatal(err)
	}
	amount := int64(-12000)
	if err := e.l.EditManual(e.ctx, bo, id, ledger.ManualEdit{AmountMinor: &amount}); err != nil {
		t.Fatal(err)
	}
	d = e.detail(bo, g.ID)
	x := d.Expenses[0]
	parts := map[uuid.UUID]int64{}
	for _, s := range x.Shares {
		parts[s.Member] = s.Minor
	}
	if x.AmountMinor != 12000 || x.SplitMode != "shares" || parts[a] != 8000 || parts[b] != 4000 {
		t.Errorf("after a new amount: %+v %v", x, parts)
	}
	if _, err := e.l.DeleteManual(e.ctx, bo, id); err != nil {
		t.Fatal(err)
	}
	if d = e.detail(bo, g.ID); len(d.Expenses) != 0 {
		t.Errorf("the typed row's copy outlived it: %+v", d.Expenses)
	}
}

// TestSplits: equal (default and a subset), shares and exact; exact must add up; members only.
func TestSplits(t *testing.T) {
	e := newEnv(t)
	alex, bo, cy := e.user("Alex"), e.user("Bo"), e.user("Cy")
	e.befriend(alex, bo)
	e.befriend(alex, cy)
	g, _ := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Event", Currency: "PLN", Kind: "event"})
	for _, u := range []uuid.UUID{bo, cy} {
		if _, err := e.s.Invite(e.ctx, alex, g.ID, u); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.Accept(e.ctx, u, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	d := e.detail(alex, g.ID)
	a, b, c := memberOf(d, "Alex").ID, memberOf(d, "Bo").ID, memberOf(d, "Cy").ID
	day := platform.Date{Y: 2026, M: 7, D: 1}
	add := func(in sharing.ExpenseInput) error {
		in.BookedOn, in.Payee = day, "x"
		_, err := e.s.AddExpense(e.ctx, alex, g.ID, in)
		return err
	}
	if err := add(sharing.ExpenseInput{AmountMinor: 100}); err != nil { // equal between 3: 34/33/33
		t.Fatal(err)
	}
	if err := add(sharing.ExpenseInput{AmountMinor: 1000, Split: sharing.Split{Members: []uuid.UUID{b, c}}}); err != nil {
		t.Fatal(err)
	}
	if err := add(sharing.ExpenseInput{AmountMinor: 600, Split: sharing.Split{Mode: "exact", Exact: []splits.Share{{Member: b, Minor: 600}}}}); err != nil {
		t.Fatal(err) // for someone else: Bo owes all of it
	}
	isErr(t, add(sharing.ExpenseInput{AmountMinor: 600, Split: sharing.Split{Mode: "exact", Exact: []splits.Share{{Member: b, Minor: 500}}}}), ledger.ErrInvalid)
	isErr(t, add(sharing.ExpenseInput{AmountMinor: 600, Split: sharing.Split{Members: []uuid.UUID{platform.NewID()}}}), ledger.ErrInvalid)
	isErr(t, add(sharing.ExpenseInput{AmountMinor: 0}), ledger.ErrInvalid)
	d = e.detail(cy, g.ID)
	if sum(d.Balances) != 0 || len(d.Transfers) > 2 {
		t.Errorf("balances %v transfers %+v", d.Balances, d.Transfers)
	}
	// Alex paid 1700; owes 34 → +1666; Bo owes 33 + 500 + 600 = −1133; Cy owes 33 + 500 = −533
	if d.Balances[a] != 1666 || d.Balances[b] != -1133 || d.Balances[c] != -533 {
		t.Errorf("balances %v", d.Balances)
	}
}

// TestConcurrentAdds: members adding expenses at once all land, and the balances still add up.
func TestConcurrentAdds(t *testing.T) {
	e := newEnv(t)
	alex, bo := e.user("Alex"), e.user("Bo")
	e.befriend(alex, bo)
	g, _ := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Dom", Currency: "PLN"})
	if _, err := e.s.Invite(e.ctx, alex, g.ID, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 40 {
		u := alex
		if i%2 == 1 {
			u = bo
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.s.AddExpense(e.ctx, u, g.ID, sharing.ExpenseInput{BookedOn: platform.Date{Y: 2026, M: 8, D: 1 + i%28},
				AmountMinor: int64(101 + i), Payee: "x"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	d := e.detail(bo, g.ID)
	if len(d.Expenses) != 40 || sum(d.Balances) != 0 {
		t.Errorf("%d expenses, balances %v", len(d.Expenses), d.Balances)
	}
}

// TestSharedRLS reads every shared table as the app role under each user: an outsider sees
// nothing, an invited user sees the group and its members only, an active member and one who left
// see what their state allows, and nobody sees another member's private links.
func TestSharedRLS(t *testing.T) {
	e := newEnv(t)
	alex, bo, cy, dee, out := e.user("Alex"), e.user("Bo"), e.user("Cy"), e.user("Dee"), e.user("Out")
	for _, u := range []uuid.UUID{bo, cy, dee} {
		e.befriend(alex, u)
	}
	_, ids := e.importInto(alex, "Konto", "02.05.2026;HOTEL;Nocleg;-300,00;")
	trip := e.group(alex, "Trip")
	if err := e.put(alex, &trip, ids...); err != nil {
		t.Fatal(err)
	}
	g, _ := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Trip", Currency: "PLN", FromGroupID: &trip})
	for _, u := range []uuid.UUID{bo, cy, dee} {
		if _, err := e.s.Invite(e.ctx, alex, g.ID, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []uuid.UUID{bo, dee} {
		if _, err := e.s.Accept(e.ctx, u, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	// joining split the hotel three ways: Dee pays their 100 back, then leaves
	dd := e.detail(dee, g.ID)
	if _, err := e.s.Settle(e.ctx, dee, g.ID, sharing.SettlementInput{From: memberOf(dd, "Dee").ID, To: memberOf(dd, "Alex").ID,
		AmountMinor: 10000, SettledOn: platform.Date{Y: 2026, M: 5, D: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Leave(e.ctx, dee, g.ID); err != nil {
		t.Fatal(err)
	}
	counts := func(u uuid.UUID) map[string]int {
		got := map[string]int{}
		if err := e.st.WithUserReadOnly(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
			for _, table := range []string{"shared_groups", "shared_group_members", "shared_expenses", "shared_expense_splits",
				"shared_settlements", "shared_expense_links"} {
				var n int
				if err := tx.QueryRow(e.ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
					return err
				}
				got[table] = n
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	want := map[uuid.UUID]map[string]int{
		alex: {"shared_groups": 1, "shared_group_members": 4, "shared_expenses": 1, "shared_expense_splits": 3, "shared_settlements": 1, "shared_expense_links": 1},
		bo:   {"shared_groups": 1, "shared_group_members": 4, "shared_expenses": 1, "shared_expense_splits": 3, "shared_settlements": 1},
		cy:   {"shared_groups": 1, "shared_group_members": 4}, // invited
		dee:  {"shared_group_members": 1},                     // left: their own row only
		out:  {},
	}
	names := map[uuid.UUID]string{alex: "owner", bo: "active", cy: "invited", dee: "left", out: "outsider"}
	for u, w := range want {
		got := counts(u)
		for table, n := range got {
			if n != w[table] {
				t.Errorf("%s sees %d rows of %s, want %d", names[u], n, table, w[table])
			}
		}
	}
	// writes: an outsider can't add to the group, an invited user can't either, nobody edits another's expense
	for _, u := range []uuid.UUID{out, cy, dee} {
		if _, err := e.s.AddExpense(e.ctx, u, g.ID, sharing.ExpenseInput{BookedOn: platform.Date{Y: 2026, M: 5, D: 1}, AmountMinor: 100}); err == nil {
			t.Errorf("%s added an expense", names[u])
		}
	}
	err := e.st.WithUser(e.ctx, bo, func(_ *db.Queries, tx pgx.Tx) error {
		tag, err := tx.Exec(e.ctx, `UPDATE shared_expenses SET payee = 'changed'`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("bo changed %d of Alex's expenses", tag.RowsAffected())
		}
		tag, err = tx.Exec(e.ctx, `UPDATE shared_group_members SET display_name = 'x' WHERE user_id <> $1`, bo)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("bo renamed %d members", tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestFormerMember: deleting an account keeps its member row (as "Former member") and its
// expenses, so the others' balances don't move.
func TestFormerMember(t *testing.T) {
	e := newEnv(t)
	alex, bo := e.user("Alex"), e.user("Bo")
	e.befriend(alex, bo)
	g, _ := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Dom", Currency: "PLN"})
	if _, err := e.s.Invite(e.ctx, alex, g.ID, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.AddExpense(e.ctx, bo, g.ID, sharing.ExpenseInput{BookedOn: platform.Date{Y: 2026, M: 8, D: 1}, AmountMinor: 1000, Payee: "x"}); err != nil {
		t.Fatal(err)
	}
	before := e.detail(alex, g.ID)
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error { _, err := q.DeleteUser(e.ctx, bo); return err }); err != nil {
		t.Fatal(err)
	}
	after := e.detail(alex, g.ID)
	m := memberOf(after, "Bo")
	if !m.Former || len(after.Expenses) != 1 || after.Balances[m.ID] != before.Balances[m.ID] || sum(after.Balances) != 0 {
		t.Errorf("after deleting Bo: member %+v, expenses %d, balances %v (before %v)", m, len(after.Expenses), after.Balances, before.Balances)
	}
}

// TestJoiningResplits: an equal split with everyone takes in a member who joins later; a split
// between some members, by shares or as exact amounts doesn't change.
func TestJoiningResplits(t *testing.T) {
	e := newEnv(t)
	alex, bo, cy := e.user("Alex"), e.user("Bo"), e.user("Cy")
	e.befriend(alex, bo)
	e.befriend(alex, cy)
	g, _ := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: "Dom", Currency: "PLN"})
	if _, err := e.s.Invite(e.ctx, alex, g.ID, bo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, bo, g.ID); err != nil {
		t.Fatal(err)
	}
	d := e.detail(alex, g.ID)
	a, b := memberOf(d, "Alex").ID, memberOf(d, "Bo").ID
	day := platform.Date{Y: 2026, M: 9, D: 1}
	everyone, _ := e.s.AddExpense(e.ctx, alex, g.ID, sharing.ExpenseInput{BookedOn: day, AmountMinor: 900, Payee: "all"})
	some, _ := e.s.AddExpense(e.ctx, alex, g.ID, sharing.ExpenseInput{BookedOn: day, AmountMinor: 900, Payee: "alex only",
		Split: sharing.Split{Members: []uuid.UUID{a}}})
	weighted, _ := e.s.AddExpense(e.ctx, bo, g.ID, sharing.ExpenseInput{BookedOn: day, AmountMinor: 900, Payee: "shares",
		Split: sharing.Split{Mode: "shares", Members: []uuid.UUID{a, b}, Weights: []int{2, 1}}})
	bosAll, _ := e.s.AddExpense(e.ctx, bo, g.ID, sharing.ExpenseInput{BookedOn: day, AmountMinor: 600, Payee: "bo all"})
	if _, err := e.s.Invite(e.ctx, alex, g.ID, cy); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, cy, g.ID); err != nil {
		t.Fatal(err)
	}
	d = e.detail(cy, g.ID)
	parts := func(id uuid.UUID) int {
		for _, x := range d.Expenses {
			if x.ID == id {
				return len(x.Shares)
			}
		}
		return -1
	}
	if parts(everyone) != 3 || parts(bosAll) != 3 || parts(some) != 1 || parts(weighted) != 2 || sum(d.Balances) != 0 {
		t.Errorf("after Cy joined: %d %d %d %d, balances %v", parts(everyone), parts(bosAll), parts(some), parts(weighted), d.Balances)
	}
}
