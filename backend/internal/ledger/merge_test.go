package ledger_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

type sharedCopy struct {
	id       uuid.UUID
	bookedOn string
	payee    string
	note     string
}

func (e *env) sharedCopies(u uuid.UUID) []sharedCopy {
	e.t.Helper()
	var out []sharedCopy
	if err := e.st.WithUser(e.ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		rows, err := tx.Query(e.ctx, `SELECT id, booked_on::text, payee, note FROM shared_expenses ORDER BY booked_on`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c sharedCopy
			if err := rows.Scan(&c.id, &c.bookedOn, &c.payee, &c.note); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// TestMergeTypedIntoImported: a card payment typed into a bank account meets the bank's row; the
// merge keeps the bank's row with the typed one's category, note, group and shared copy (ADR 0032).
func TestMergeTypedIntoImported(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	if err := e.st.WithoutUser(e.ctx, func(q *db.Queries, _ pgx.Tx) error {
		return q.SetUserDisplayName(e.ctx, db.SetUserDisplayNameParams{ID: u, DisplayName: "Alex"})
	}); err != nil {
		t.Fatal(err)
	}
	konto := e.account(u, "Konto")
	trip, err := e.l.CreateGroup(e.ctx, u, "Praga", -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sharing.New(e.st, e.l, notify.New(e.st, platform.SystemClock{}), platform.SystemClock{}).
		Create(e.ctx, u, sharing.GroupInput{Name: "Wyjazd do Pragi", FromGroupID: &trip.ID}); err != nil {
		t.Fatal(err)
	}
	food := "Jedzenie"
	day := platform.Date{Y: 2026, M: 11, D: 3}
	typed, err := e.l.AddManual(e.ctx, u, ledger.ManualInput{AccountID: konto.ID, BookedOn: day, AmountMinor: -5430, Payee: "Biedronka",
		CategoryName: &food, GroupID: &trip.ID, Note: "z Bo"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.l.AddManual(e.ctx, u, ledger.ManualInput{AccountID: konto.ID, BookedOn: day, AmountMinor: -1200, Payee: "Kiosk"})
	if err != nil {
		t.Fatal(err)
	}
	copies := e.sharedCopies(u)
	if len(copies) != 1 || copies[0].bookedOn != "2026-11-03" || copies[0].payee != "Biedronka" {
		t.Fatalf("copies before = %+v", copies)
	}
	e.commitCSV(u, konto.ID, "konto.csv", "Data operacji;Kontrahent;Tytuł;Kwota\n"+
		"2026-11-04;BIEDRONKA SKLEP 123;zakupy;-54,30\n2026-11-05;KIOSK RUCH;prasa;-12,00\n2026-11-20;BIEDRONKA;zakupy;-54,30\n")

	list, err := e.l.MergeSuggestions(e.ctx, u)
	if err != nil || len(list) != 2 {
		t.Fatalf("suggestions %v %+v", err, list)
	}
	byTyped := map[uuid.UUID]ledger.MergeSuggestion{}
	for _, s := range list {
		byTyped[s.TypedID] = s
	}
	s, ok := byTyped[typed]
	if !ok || s.ImportedOn != (platform.Date{Y: 2026, M: 11, D: 4}) || s.ImportedPayee == "" || s.AccountName != "Konto" || s.TypedNote != "z Bo" {
		t.Fatalf("suggestion = %+v (the 20th is more than 3 days away)", s)
	}

	// the wrong way round, or a pair that doesn't fit, is refused
	if _, err := e.l.Merge(e.ctx, u, s.ImportedID, typed); err == nil {
		t.Error("an imported row merged into a typed one")
	} else {
		isErr(t, err, ledger.ErrInvalid)
	}
	if _, err := e.l.Merge(e.ctx, u, other, s.ImportedID); err == nil {
		t.Error("different amounts merged")
	} else {
		isErr(t, err, ledger.ErrInvalid)
	}
	if _, err := e.l.Merge(e.ctx, e.user(), typed, s.ImportedID); err == nil {
		t.Error("another user merged")
	} else {
		isErr(t, err, ledger.ErrNotFound)
	}

	if _, err := e.l.Merge(e.ctx, u, typed, s.ImportedID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.GetTransaction(e.ctx, u, typed); err == nil {
		t.Error("the typed row is still there")
	}
	got, err := e.l.GetTransaction(e.ctx, u, s.ImportedID)
	if err != nil || got.Note != "z Bo" || got.GroupID == nil || *got.GroupID != trip.ID || got.CategorySource != "manual" ||
		e.categoryOf(u)["BIEDRONKA SKLEP 123"][0] != "Jedzenie" {
		t.Errorf("the bank's row = %+v %v", got, err)
	}
	// the same shared expense, now with the bank's values
	after := e.sharedCopies(u)
	if len(after) != 1 || after[0].id != copies[0].id || after[0].bookedOn != "2026-11-04" || after[0].payee != "BIEDRONKA SKLEP 123" || after[0].note != "z Bo" {
		t.Errorf("copies after = %+v, before %+v", after, copies)
	}

	// keep both: never suggested again
	k := byTyped[other]
	if err := e.l.KeepApart(e.ctx, u, other, k.ImportedID); err != nil {
		t.Fatal(err)
	}
	list, err = e.l.MergeSuggestions(e.ctx, u)
	if err != nil || slices.ContainsFunc(list, func(m ledger.MergeSuggestion) bool { return m.TypedID == other }) {
		t.Errorf("after keep both: %v %+v", err, list)
	}
	if n := e.countTransactions(u, konto.ID); n != 4 {
		t.Errorf("%d transactions (3 imported + the kiosk typed row)", n)
	}
}
