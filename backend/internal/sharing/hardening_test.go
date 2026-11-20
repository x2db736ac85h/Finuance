package sharing_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

var day = platform.Date{Y: 2026, M: 8, D: 1}

// trio is a group of Alex (owner), Bo and Cy, all active, in the order they joined.
func (e *env) trio(name string) (g uuid.UUID, alex, bo, cy uuid.UUID) {
	e.t.Helper()
	alex, bo, cy = e.user("Alex"), e.user("Bo"), e.user("Cy")
	e.befriend(alex, bo)
	e.befriend(alex, cy)
	created, err := e.s.Create(e.ctx, alex, sharing.GroupInput{Name: name, Currency: "PLN"})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, u := range []uuid.UUID{bo, cy} {
		if _, err := e.s.Invite(e.ctx, alex, created.ID, u); err != nil {
			e.t.Fatal(err)
		}
		if _, err := e.s.Accept(e.ctx, u, created.ID); err != nil {
			e.t.Fatal(err)
		}
	}
	return created.ID, alex, bo, cy
}

func (e *env) deleteUser(u uuid.UUID) {
	e.t.Helper()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error { _, err := q.DeleteUser(e.ctx, u); return err }); err != nil {
		e.t.Fatal(err)
	}
}

// TestOwnerHandover: when a group's owner deletes their account, the member who joined first owns
// the group from then on and can do what an owner does (API-7). The former owner's row stays, as
// a former member, so no balance moves.
func TestOwnerHandover(t *testing.T) {
	e := newEnv(t)
	g, alex, bo, cy := e.trio("Dom")
	if _, err := e.s.AddExpense(e.ctx, alex, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 3000, Payee: "Rent"}); err != nil {
		t.Fatal(err)
	}
	before := e.detail(bo, g)
	e.deleteUser(alex)

	after := e.detail(bo, g)
	if me := memberOf(after, "Bo"); me.Role != "owner" {
		t.Fatalf("Bo's role after the owner left: %q, want owner (members %+v)", me.Role, after.Members)
	}
	if former := memberOf(after, "Alex"); !former.Former || former.Role == "owner" {
		t.Errorf("the former owner's row: %+v", former)
	}
	if cyRow := memberOf(e.detail(cy, g), "Cy"); cyRow.Role != "member" {
		t.Errorf("Cy's role: %q", cyRow.Role)
	}
	for id, v := range before.Balances {
		if after.Balances[id] != v {
			t.Errorf("a balance moved in the handover: %d → %d", v, after.Balances[id])
		}
	}
	// the new owner does an owner's things; the other member still can't
	if _, err := e.s.Update(e.ctx, cy, g, sharing.GroupInput{Name: "Mine", Currency: "PLN"}); err == nil {
		t.Error("a member renamed the group")
	}
	if _, err := e.s.Update(e.ctx, bo, g, sharing.GroupInput{Name: "Dom 2", Currency: "PLN"}); err != nil {
		t.Errorf("the new owner can't rename the group: %v", err)
	}
	dee := e.user("Dee")
	e.befriend(bo, dee)
	if _, err := e.s.Invite(e.ctx, bo, g, dee); err != nil {
		t.Errorf("the new owner can't invite: %v", err)
	}
	// the expense of the account that is gone: the owner may change and delete it, a member not (DB-6)
	exp := after.Expenses[0].ID
	if !after.Expenses[0].Mine || e.detail(cy, g).Expenses[0].Mine {
		t.Error("the page should offer the change to the owner only")
	}
	in := sharing.ExpenseInput{BookedOn: day, AmountMinor: 3300, Payee: "Rent, corrected"}
	if err := e.s.UpdateExpense(e.ctx, cy, g, exp, in); !errors.Is(err, platform.ErrForbidden) {
		t.Errorf("a member changed the former owner's expense: %v", err)
	}
	if err := e.s.UpdateExpense(e.ctx, bo, g, exp, in); err != nil {
		t.Fatalf("the owner can't change the former member's expense: %v", err)
	}
	if d := e.detail(cy, g); d.Expenses[0].AmountMinor != 3300 || sum(d.Balances) != 0 {
		t.Errorf("after the correction: %+v, balances %v", d.Expenses[0], d.Balances)
	}
	if err := e.s.DeleteExpense(e.ctx, cy, g, exp); !errors.Is(err, platform.ErrForbidden) {
		t.Errorf("a member deleted the former owner's expense: %v", err)
	}
	if err := e.s.DeleteExpense(e.ctx, bo, g, exp); err != nil {
		t.Fatalf("the owner can't delete the former member's expense: %v", err)
	}
	// and the group can be archived again (a group other people have been in is never deleted)
	if _, err := e.s.Update(e.ctx, bo, g, sharing.GroupInput{Name: "Dom 2", Currency: "PLN", Archived: true}); err != nil {
		t.Errorf("the new owner can't archive the group: %v", err)
	}
}

// TestHandoverNeedsSomeone: a group whose only member deletes their account stays without an
// owner (nobody is left); deleting a member who is not the owner changes no role.
func TestHandoverNeedsSomeone(t *testing.T) {
	e := newEnv(t)
	solo := e.user("Solo")
	if _, err := e.s.Create(e.ctx, solo, sharing.GroupInput{Name: "Alone", Currency: "PLN"}); err != nil {
		t.Fatal(err)
	}
	e.deleteUser(solo) // must not fail

	g, alex, bo, cy := e.trio("Trip")
	e.deleteUser(cy)
	if d := e.detail(alex, g); memberOf(d, "Alex").Role != "owner" || memberOf(d, "Bo").Role != "member" {
		t.Errorf("roles after a member's account was deleted: %+v", d.Members)
	}
	_ = bo
}

// TestEitherSideRemovesASettlement: a payment can be taken back by whoever recorded it and by the
// other side, who is told either way; a third member can't (API-3).
func TestEitherSideRemovesASettlement(t *testing.T) {
	e := newEnv(t)
	g, alex, bo, cy := e.trio("Trip")
	if _, err := e.s.AddExpense(e.ctx, alex, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 3000, Payee: "Hotel"}); err != nil {
		t.Fatal(err)
	}
	d := e.detail(bo, g)
	// Bo records "I paid Alex 1000" without Alex's agreement
	st, err := e.s.Settle(e.ctx, bo, g, sharing.SettlementInput{From: memberOf(d, "Bo").ID, To: memberOf(d, "Alex").ID, AmountMinor: 1000, SettledOn: day})
	if err != nil {
		t.Fatal(err)
	}
	// the page offers the removal to both sides and not to the third member
	for who, want := range map[uuid.UUID]bool{alex: true, bo: true, cy: false} {
		if got := e.detail(who, g).Settlements[0].Mine; got != want {
			t.Errorf("the payment is offered for removal: %v, want %v", got, want)
		}
	}
	if err := e.s.DeleteSettlement(e.ctx, cy, g, st.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a third member removed the payment: %v", err)
	}
	if err := e.s.DeleteSettlement(e.ctx, alex, g, st.ID); err != nil {
		t.Fatalf("the receiving side can't remove the payment: %v", err)
	}
	if d := e.detail(alex, g); len(d.Settlements) != 0 || d.Balances[memberOf(d, "Bo").ID] != -1000 {
		t.Errorf("after removing it: %d payments, Bo's balance %d", len(d.Settlements), d.Balances[memberOf(d, "Bo").ID])
	}
	notes, _, err := e.n.List(e.ctx, bo, 50)
	if err != nil {
		t.Fatal(err)
	}
	told := false
	for _, n := range notes {
		told = told || n.Kind == notify.SharedSettlementRemoved
	}
	if !told {
		t.Errorf("Bo was not told that Alex removed the payment: %+v", notes)
	}
	// whoever recorded it can still remove their own
	st, _ = e.s.Settle(e.ctx, bo, g, sharing.SettlementInput{From: memberOf(d, "Bo").ID, To: memberOf(d, "Alex").ID, AmountMinor: 1000, SettledOn: day})
	if err := e.s.DeleteSettlement(e.ctx, bo, g, st.ID); err != nil {
		t.Errorf("the recording side can't remove its own payment: %v", err)
	}
}

// TestOnlyActiveMembersTakePart: the application refuses, and so does the database when asked
// directly (API-8, DB-2): a payer or a side of a payment who was only invited or has left, an
// invitation for someone who is not the owner's friend or with another role, renaming another
// member, joining an archived group, and changing a role.
func TestOnlyActiveMembersTakePart(t *testing.T) {
	e := newEnv(t)
	g, alex, bo, cy := e.trio("Trip")
	dee, stranger := e.user("Dee"), e.user("Stranger")
	e.befriend(alex, dee)
	invited, err := e.s.Invite(e.ctx, alex, g, dee) // Dee stays invited
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.Leave(e.ctx, cy, g); err != nil { // Cy has left
		t.Fatal(err)
	}
	d := e.detail(alex, g)
	alexM, boM, cyM := memberOf(d, "Alex").ID, memberOf(d, "Bo").ID, memberOf(d, "Cy").ID

	for name, payer := range map[string]uuid.UUID{"an invited": invited.ID, "a former": cyM} {
		if _, err := e.s.AddExpense(e.ctx, alex, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 100, Payee: "x", PaidBy: &payer}); err == nil {
			t.Errorf("%s member was accepted as the payer", name)
		}
		if _, err := e.s.Settle(e.ctx, alex, g, sharing.SettlementInput{From: alexM, To: payer, AmountMinor: 100, SettledOn: day}); err == nil {
			t.Errorf("a payment to %s member was accepted", name)
		}
	}
	exp, err := e.s.AddExpense(e.ctx, alex, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 100, Payee: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.UpdateExpense(e.ctx, alex, g, exp, sharing.ExpenseInput{BookedOn: day, AmountMinor: 100, Payee: "x", PaidBy: &cyM}); err == nil {
		t.Error("the payer was changed to a former member")
	}

	// the same asked of the database directly, as the owner
	mustFail := func(name, sql string, args ...any) {
		t.Helper()
		err := e.st.WithUser(e.ctx, alex, func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(e.ctx, sql, args...)
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || (pg.Code != "42501" && pg.Code != "23514") {
			t.Errorf("%s: %v, want a refusal by a policy (42501) or a check (23514)", name, err)
		}
	}
	mustFail("an expense paid by a former member",
		`INSERT INTO shared_expenses (id, group_id, added_by, added_member, paid_by, booked_on, amount_minor, payee, origin)
		 VALUES ($1, $2, $3, $4, $5, '2026-08-01', 100, 'x', 'typed')`, platform.NewID(), g, alex, alexM, cyM)
	mustFail("a payer changed to an invited member", `UPDATE shared_expenses SET paid_by = $1 WHERE id = $2`, invited.ID, exp)
	mustFail("a payment to a former member",
		`INSERT INTO shared_settlements (id, group_id, from_member, to_member, amount_minor, settled_on, created_by)
		 VALUES ($1, $2, $3, $4, 100, '2026-08-01', $5)`, platform.NewID(), g, alexM, cyM, alex)
	mustFail("an invitation for someone who is not a friend",
		`INSERT INTO shared_group_members (id, group_id, user_id, role, state, display_name) VALUES ($1, $2, $3, 'member', 'invited', 'S')`,
		platform.NewID(), g, stranger)
	fresh := e.user("Fresh")
	e.befriend(alex, fresh)
	mustFail("a friend added as active without being asked",
		`INSERT INTO shared_group_members (id, group_id, user_id, role, state, display_name) VALUES ($1, $2, $3, 'member', 'active', 'F')`,
		platform.NewID(), g, fresh)
	mustFail("a second owner",
		`INSERT INTO shared_group_members (id, group_id, user_id, role, state, display_name) VALUES ($1, $2, $3, 'owner', 'active', 'F')`,
		platform.NewID(), g, fresh)
	mustFail("another member renamed", `UPDATE shared_group_members SET display_name = 'Renamed' WHERE id = $1`, boM)
	mustFail("a role changed", `UPDATE shared_group_members SET role = 'owner' WHERE id = $1`, boM)

	// an archived group takes no new member
	if _, err := e.s.Update(e.ctx, alex, g, sharing.GroupInput{Name: "Trip", Currency: "PLN", Archived: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Accept(e.ctx, dee, g); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Errorf("joining an archived group: %v", err)
	}
	_ = bo
}

// TestGroupHoldsALimitedNumberOfExpenses: a full group takes no more, typed in the group or copied
// from a member's transactions (API-5). The limit is lowered for the test.
func TestGroupHoldsALimitedNumberOfExpenses(t *testing.T) {
	was := ledger.MaxSharedExpenses
	ledger.MaxSharedExpenses = 2
	t.Cleanup(func() { ledger.MaxSharedExpenses = was })
	e := newEnv(t)
	g, alex, bo, _ := e.trio("Small")
	for i := range 2 {
		if _, err := e.s.AddExpense(e.ctx, alex, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 100, Payee: "x"}); err != nil {
			t.Fatalf("expense %d: %v", i+1, err)
		}
	}
	if _, err := e.s.AddExpense(e.ctx, bo, g, sharing.ExpenseInput{BookedOn: day, AmountMinor: 100, Payee: "x"}); !errors.Is(err, platform.ErrLimitReached) {
		t.Errorf("a third expense in a group of two: %v", err)
	}
	// Bo's linked personal group: putting a transaction into it would share a copy
	_, ids := e.importInto(bo, "Konto", "02.08.2026;SKLEP;Zakupy;-50,00;")
	groups, err := e.l.ListGroups(e.ctx, bo)
	if err != nil || len(groups) == 0 {
		t.Fatal(err, groups)
	}
	linked := groups[0].ID
	if err := e.put(bo, &linked, ids...); !errors.Is(err, ledger.ErrInvalid) || !strings.Contains(err.Error(), "the most a shared group holds") {
		t.Errorf("a copy into a full group: %v", err)
	}
	if d := e.detail(alex, g); len(d.Expenses) != 2 {
		t.Errorf("%d expenses in a group that holds 2", len(d.Expenses))
	}
}
