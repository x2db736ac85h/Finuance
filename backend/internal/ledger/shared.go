package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/splits"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Shared copies (ADR 0028). A transaction in a personal group linked to a shared group has a copy
// in the shared group (date, amount, payee, category name, note, who paid), tied to it by the
// user's private link. syncShared keeps the copies of the given transactions in step with them, in
// the caller's database transaction:
//
//   - in a linked group and no copy yet (or a copy in another group): a new copy, split equally
//     between the active members, paid by the user;
//   - in a linked group with a copy there: the copy takes the transaction's values; a changed
//     amount is split again (equally, or by the same shares; exact splits go back to equal);
//   - in no linked group but with a copy: the copy is deleted (the transaction left the group).
//
// strict is false for the import pipeline (commit, re-materialisation): a transaction that can't be
// shared (another currency, an archived group, a group the user left) is skipped there instead of
// failing the import.

// ErrSharedCurrency means a transaction can't go into a shared group of another currency.
var ErrSharedCurrency = errors.New("ledger: shared group currency")

const (
	maxSharedPayee = 200
	maxSharedCat   = 80
	maxSharedNote  = 2000
)

func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// SharedCopyAmount is what a group spent for a transaction: money out is a positive expense,
// money in a negative one (a refund).
func SharedCopyAmount(txAmount int64) int64 { return -txAmount }

func syncShared(ctx context.Context, q *db.Queries, userID uuid.UUID, ids []uuid.UUID, now time.Time, strict bool) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.TransactionsForSharing(ctx, db.TransactionsForSharingParams{UserID: userID, Ids: ids})
	if err != nil {
		return err
	}
	groups := map[uuid.UUID]*sharedTarget{}
	for i := range rows {
		r := &rows[i]
		if r.SharedGroupID == nil {
			if r.LinkedExpense != nil {
				if _, err := q.DeleteSharedExpense(ctx, *r.LinkedExpense); err != nil {
					return err
				}
			}
			continue
		}
		tg, ok := groups[*r.SharedGroupID]
		if !ok {
			if tg, err = loadTarget(ctx, q, userID, *r.SharedGroupID); err != nil {
				return err
			}
			groups[*r.SharedGroupID] = tg
		}
		if problem := tg.refuses(r.Currency); problem != nil {
			if strict {
				return problem
			}
			continue
		}
		amount := SharedCopyAmount(r.AmountMinor)
		payee := cut(r.Payee, maxSharedPayee)
		if payee == "" {
			payee = "(no payee)"
		}
		fields := sharedFields{bookedOn: r.BookedOn, amount: amount, payee: payee,
			category: cut(r.CategoryName, maxSharedCat), note: cut(r.Note, maxSharedNote)}
		if r.LinkedExpense != nil && r.LinkedGroup != nil && *r.LinkedGroup == tg.group.ID {
			if err := updateCopy(ctx, q, tg, *r.LinkedExpense, fields, now); err != nil {
				return err
			}
			continue
		}
		if r.LinkedExpense != nil { // moved from another shared group
			if _, err := q.DeleteSharedExpense(ctx, *r.LinkedExpense); err != nil {
				return err
			}
		}
		id := platform.NewID()
		if err := q.InsertSharedExpense(ctx, db.InsertSharedExpenseParams{ID: id, GroupID: tg.group.ID, AddedBy: &userID,
			AddedMember: tg.me.ID, PaidBy: tg.me.ID, BookedOn: fields.bookedOn, AmountMinor: amount, Payee: fields.payee,
			CategoryName: fields.category, Note: fields.note, Origin: "transaction", SplitMode: "equal", Now: now}); err != nil {
			return err
		}
		shares, err := splits.Equal(amount, tg.active)
		if err != nil {
			return fmt.Errorf("%s has no active members: %w", tg.group.Name, ErrInvalid)
		}
		if err := writeShares(ctx, q, tg.group.ID, id, shares); err != nil {
			return err
		}
		if err := q.UpsertSharedLink(ctx, db.UpsertSharedLinkParams{UserID: userID, AccountID: r.AccountID, Fingerprint: r.Fingerprint,
			Occurrence: r.Occurrence, SplitScope: r.SplitScope, ExpenseID: id}); err != nil {
			return err
		}
	}
	return nil
}

type sharedTarget struct {
	group  db.SharedGroup
	me     db.SharedGroupMember
	active []uuid.UUID
	err    error // why nothing can be shared into this group, or nil
}

func loadTarget(ctx context.Context, q *db.Queries, userID, groupID uuid.UUID) (*sharedTarget, error) {
	g, err := q.GetSharedGroup(ctx, groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &sharedTarget{err: fmt.Errorf("the shared group is gone: %w", ErrInvalid)}, nil
	}
	if err != nil {
		return nil, err
	}
	tg := &sharedTarget{group: g}
	me, err := q.GetMyMembership(ctx, db.GetMyMembershipParams{GroupID: groupID, UserID: &userID})
	switch {
	case errors.Is(err, pgx.ErrNoRows) || (err == nil && me.State != "active"):
		tg.err = fmt.Errorf("you are not a member of %s any more: %w", g.Name, ErrInvalid)
		return tg, nil
	case err != nil:
		return nil, err
	}
	tg.me = me
	if g.ArchivedAt != nil {
		tg.err = fmt.Errorf("%s is archived: %w", g.Name, ErrInvalid)
		return tg, nil
	}
	if tg.active, err = q.ActiveSharedMemberIDs(ctx, groupID); err != nil {
		return nil, err
	}
	return tg, nil
}

func (t *sharedTarget) refuses(currency string) error {
	if t.err != nil {
		return t.err
	}
	if currency != t.group.Currency {
		return fmt.Errorf("%s is shared in %s; this transaction is in %s: %w: %w", t.group.Name, t.group.Currency, currency,
			ErrSharedCurrency, ErrInvalid)
	}
	return nil
}

type sharedFields struct {
	bookedOn              platform.Date
	amount                int64
	payee, category, note string
}

func writeShares(ctx context.Context, q *db.Queries, groupID, expenseID uuid.UUID, shares []splits.Share) error {
	if err := q.DeleteSharedSplits(ctx, expenseID); err != nil {
		return err
	}
	for _, s := range shares {
		var w *int32
		if s.Weight > 0 {
			v := int32(s.Weight) //nolint:gosec // weights are at most 1000 (splits.Weighted)
			w = &v
		}
		if err := q.InsertSharedSplit(ctx, db.InsertSharedSplitParams{ExpenseID: expenseID, GroupID: groupID, MemberID: s.Member,
			ShareMinor: s.Minor, Weight: w}); err != nil {
			return err
		}
	}
	return nil
}

// updateCopy gives an existing copy the transaction's values; a new amount is split again.
func updateCopy(ctx context.Context, q *db.Queries, tg *sharedTarget, expenseID uuid.UUID, f sharedFields, now time.Time) error {
	e, err := q.GetSharedExpense(ctx, expenseID)
	if err != nil {
		return err
	}
	mode := e.SplitMode
	if e.AmountMinor != f.amount {
		shares, err := resplit(ctx, q, e, f.amount, tg.active)
		if err != nil {
			return err
		}
		if len(shares) > 0 && shares[0].Weight == 0 {
			mode = "equal"
		}
		if _, err := q.UpdateSharedExpense(ctx, db.UpdateSharedExpenseParams{ID: e.ID, GroupID: e.GroupID, BookedOn: f.bookedOn,
			AmountMinor: f.amount, Payee: f.payee, CategoryName: f.category, Note: f.note, PaidBy: e.PaidBy, SplitMode: mode, Now: now}); err != nil {
			return err
		}
		return writeShares(ctx, q, e.GroupID, e.ID, shares)
	}
	_, err = q.UpdateSharedExpense(ctx, db.UpdateSharedExpenseParams{ID: e.ID, GroupID: e.GroupID, BookedOn: f.bookedOn,
		AmountMinor: f.amount, Payee: f.payee, CategoryName: f.category, Note: f.note, PaidBy: e.PaidBy, SplitMode: mode, Now: now})
	return err
}

// resplit splits a new amount the way the expense was split: by the same weights, else equally
// between the members it was split between (exact amounts can't follow a new total, so they go
// back to equal).
func resplit(ctx context.Context, q *db.Queries, e db.SharedExpense, amount int64, active []uuid.UUID) ([]splits.Share, error) {
	old, err := q.SharedSplitsOf(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	members := make([]uuid.UUID, 0, len(old))
	weights := make([]int, 0, len(old))
	for _, s := range old {
		members = append(members, s.MemberID)
		if s.Weight != nil {
			weights = append(weights, int(*s.Weight))
		}
	}
	if len(members) == 0 {
		members = active
	}
	if e.SplitMode == "shares" && len(weights) == len(members) {
		return splits.Weighted(amount, members, weights)
	}
	return splits.Equal(amount, members)
}

// SyncSharedGroup shares every transaction of a personal group (after the group was linked to a
// shared group: Share…, accepting an invitation) or removes their copies (after it was unlinked).
func (l *Ledger) SyncSharedGroup(ctx context.Context, userID, groupID uuid.UUID) error {
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		ids, err := q.TransactionsInGroup(ctx, db.TransactionsInGroupParams{UserID: userID, GroupID: &groupID})
		if err != nil {
			return err
		}
		return syncShared(ctx, q, userID, ids, l.clock.Now(), false)
	})
	return dbErr("share group", err)
}
