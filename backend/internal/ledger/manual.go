package ledger

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Manual transactions (Phase 19, ADR 0029) are expenses and income a user types into a cash
// account, or (Phase 20, ADR 0032) into a bank account for a card payment the bank hasn't exported
// yet: when the import brings the bank's own row, a merge suggestion offers to keep that one with
// the typed row's annotations. They are ordinary transactions with origin = 'manual' and a random
// fingerprint, so no import matches them and nothing that rebuilds imported rows
// (re-materialisation, deleting an import, duplicate decisions) touches them. Rules and transfer
// detection treat them like any other row; a category or transfer decision set by hand still wins.

const (
	maxManualPayee       = 200
	maxManualDescription = 500
)

// ManualInput is a new manual transaction. Exactly one of CategoryID and CategoryName may be set
// (the name is created when missing).
type ManualInput struct {
	AccountID    uuid.UUID
	BookedOn     platform.Date
	AmountMinor  int64 // negative = money out
	Payee        string
	Description  string
	CategoryID   *uuid.UUID
	CategoryName *string
	GroupID      *uuid.UUID
	Note         string
}

// ManualEdit changes the typed fields of a manual transaction; nil keeps a field.
type ManualEdit struct {
	BookedOn    *platform.Date
	AmountMinor *int64
	Payee       *string
	Description *string
}

// Empty reports whether the edit changes nothing.
func (e ManualEdit) Empty() bool {
	return e.BookedOn == nil && e.AmountMinor == nil && e.Payee == nil && e.Description == nil
}

type manualFields struct {
	bookedOn    platform.Date
	amountMinor int64
	payee       string
	description string
}

func (f manualFields) clean() (manualFields, error) {
	f.payee, f.description = strings.TrimSpace(f.payee), strings.TrimSpace(f.description)
	switch {
	case f.payee == "":
		return f, invalidf("say who was paid (payee)")
	case utf8.RuneCountInString(f.payee) > maxManualPayee:
		return f, invalidf("payee longer than %d characters", maxManualPayee)
	case utf8.RuneCountInString(f.description) > maxManualDescription:
		return f, invalidf("description longer than %d characters", maxManualDescription)
	case strings.ContainsRune(f.payee+f.description, 0):
		return f, invalidf("text contains a NUL character")
	case f.amountMinor == 0:
		return f, invalidf("the amount can't be zero")
	case f.bookedOn.Y < 1990 || f.bookedOn.Y > 2100:
		return f, invalidf("the date must be between 1990 and 2100")
	}
	return f, nil
}

// searchText is what a search matches for a manual row: like an imported row's cells, the date
// and the amount in the app's text form (dd.mm.yyyy, -79,99) plus the payee and the description.
func (f manualFields) searchText(exp int) string {
	d := f.bookedOn
	date := fmt.Sprintf("%02d.%02d.%04d", d.D, int(d.M), d.Y)
	amount := strings.Replace(money.FormatMinor(f.amountMinor, exp), ".", ",", 1)
	return ingest.Norm(strings.Join([]string{date, amount, f.payee, f.description}, "\u0001"))
}

// AddManual adds a transaction typed by hand to one of the user's accounts and returns its id.
func (l *Ledger) AddManual(ctx context.Context, userID uuid.UUID, in ManualInput) (uuid.UUID, error) {
	f, err := manualFields{in.BookedOn, in.AmountMinor, in.Payee, in.Description}.clean()
	if err != nil {
		return uuid.UUID{}, err
	}
	if in.CategoryID != nil && in.CategoryName != nil {
		return uuid.UUID{}, invalidf("set only one of categoryId and categoryName")
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > maxNote {
		return uuid.UUID{}, invalidf("note longer than %d characters", maxNote)
	}
	category := in.CategoryID
	if in.CategoryName != nil {
		cid, err := l.categoryByName(ctx, userID, *in.CategoryName)
		if err != nil {
			return uuid.UUID{}, err
		}
		category = &cid
	}
	id := platform.NewID()
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		a, err := q.GetAccount(ctx, db.GetAccountParams{UserID: userID, ID: in.AccountID})
		if err != nil {
			return err
		}
		if err := lockAccount(ctx, q, userID, a.ID); err != nil {
			return err
		}
		exp, _ := money.Exponent(a.Currency)
		fp := make([]byte, 32)
		_, _ = rand.Read(fp) // crypto/rand.Read never fails on supported platforms
		now := l.clock.Now()
		if err := q.InsertManualTransaction(ctx, db.InsertManualTransactionParams{ID: id, UserID: userID, AccountID: a.ID,
			Fingerprint: fp, BookedOn: f.bookedOn, AmountMinor: f.amountMinor, Currency: a.Currency,
			Payee: f.payee, PayeeN: nameKey(f.payee), PayeeGroup: ingest.PayeeName(f.payee),
			Description: f.description, DescriptionN: nameKey(f.description), SearchText: f.searchText(exp), Now: now}); err != nil {
			return err
		}
		patch := AnnotationPatch{SetGroup: in.GroupID != nil, GroupID: in.GroupID,
			SetCategory: category != nil, CategoryID: category, SetNote: note != "", Note: note}
		if patch.SetGroup || patch.SetCategory || patch.SetNote {
			// part of the new row: an AI change records the row as created, not its annotation
			if _, err = l.annotateTx(withoutAIOrigin(ctx), q, userID, []uuid.UUID{id}, patch); err != nil {
				return err
			}
		}
		return RecordAIChange(ctx, q, l.clock, userID, []AIItem{{Entity: AIEntityTransaction, ID: id,
			After: ManualState{Created: true, BookedOn: f.bookedOn.String(), AmountMinor: f.amountMinor, Payee: f.payee}}})
	})
	return id, dbErr("add manual transaction", err)
}

// EditManual changes the typed fields of a manual transaction. An imported one is ErrInvalid: its
// values are the bank's.
func (l *Ledger) EditManual(ctx context.Context, userID, id uuid.UUID, e ManualEdit) error {
	if e.Empty() {
		return nil
	}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		t, err := q.GetTransaction(ctx, db.GetTransactionParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if t.Origin != "manual" {
			return invalidf("only transactions added by hand can be changed; this one comes from an imported file")
		}
		f := manualFields{t.BookedOn, t.AmountMinor, t.Payee, t.Description}
		if e.BookedOn != nil {
			f.bookedOn = *e.BookedOn
		}
		if e.AmountMinor != nil {
			f.amountMinor = *e.AmountMinor
		}
		if e.Payee != nil {
			f.payee = *e.Payee
		}
		if e.Description != nil {
			f.description = *e.Description
		}
		if f, err = f.clean(); err != nil {
			return err
		}
		if err := lockAccount(ctx, q, userID, t.AccountID); err != nil {
			return err
		}
		exp, _ := money.Exponent(t.Currency)
		_, err = q.UpdateManualTransaction(ctx, db.UpdateManualTransactionParams{UserID: userID, ID: id,
			BookedOn: f.bookedOn, AmountMinor: f.amountMinor, Payee: f.payee, PayeeN: nameKey(f.payee),
			PayeeGroup: ingest.PayeeName(f.payee), Description: f.description, DescriptionN: nameKey(f.description),
			SearchText: f.searchText(exp), Now: l.clock.Now()})
		if err != nil {
			return err
		}
		return syncShared(ctx, q, userID, []uuid.UUID{id}, l.clock.Now(), true)
	})
	return dbErr("edit manual transaction", err)
}

// ManualState is the "after" of a transaction an AI change added (add_expense): a revert deletes
// it while its typed fields are still these.
type ManualState struct {
	Created     bool   `json:"created"`
	BookedOn    string `json:"bookedOn"`
	AmountMinor int64  `json:"amountMinor"`
	Payee       string `json:"payee"`
}

// DeleteManual deletes a manual transaction (and its annotation) and returns its account. An
// imported one is ErrState: it goes with its import.
func (l *Ledger) DeleteManual(ctx context.Context, userID, id uuid.UUID) (uuid.UUID, error) {
	var account uuid.UUID
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		account, err = l.DeleteManualTx(ctx, q, userID, id)
		return err
	})
	return account, dbErr("delete manual transaction", err)
}

// DeleteManualTx is DeleteManual inside the caller's transaction (an AI change's revert).
func (l *Ledger) DeleteManualTx(ctx context.Context, q *db.Queries, userID, id uuid.UUID) (uuid.UUID, error) {
	var account uuid.UUID
	err := func() error {
		t, err := q.GetTransaction(ctx, db.GetTransactionParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if t.Origin != "manual" {
			return fmt.Errorf("this transaction comes from an imported file, delete the import instead: %w", ErrState)
		}
		if err := lockAccount(ctx, q, userID, t.AccountID); err != nil {
			return err
		}
		// a typed row deleted on purpose takes its shared copy along (ADR 0028)
		shared, err := q.TransactionsForSharing(ctx, db.TransactionsForSharingParams{UserID: userID, Ids: []uuid.UUID{id}})
		if err != nil {
			return err
		}
		for i := range shared {
			s := &shared[i]
			if s.LinkedExpense != nil {
				if _, err := q.DeleteSharedExpense(ctx, *s.LinkedExpense); err != nil {
					return err
				}
			}
		}
		r, err := q.DeleteManualTransaction(ctx, db.DeleteManualTransactionParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		account = r.AccountID
		return q.DeleteAnnotation(ctx, db.DeleteAnnotationParams{UserID: userID, AccountID: r.AccountID,
			Fingerprint: r.Fingerprint, Occurrence: r.Occurrence, SplitScope: r.SplitScope})
	}()
	return account, err
}
