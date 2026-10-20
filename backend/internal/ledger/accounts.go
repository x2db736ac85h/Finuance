package ledger

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Account is a bank account (or a cash wallet) that imports and transactions belong to.
type Account struct {
	ID                  uuid.UUID
	Name                string
	Currency            string
	NumberHint          string // last four digits of the account number, or ""
	OpeningBalanceMinor int64
	IsDemo              bool
	ArchivedAt          *time.Time
	CreatedAt           time.Time
}

// AccountInput is what a user sets on create and update.
type AccountInput struct {
	Name                string
	Currency            string // ISO 4217; "" means PLN
	NumberHint          string
	OpeningBalanceMinor int64
	Archived            bool // update only
	IsDemo              bool // create only
}

const maxAccountName = 80

func (in AccountInput) clean() (AccountInput, error) {
	var err error
	if in.Name, err = cleanName("account", in.Name, maxAccountName); err != nil {
		return in, err
	}
	if in.Currency == "" {
		in.Currency = "PLN"
	}
	if _, ok := money.Exponent(in.Currency); !ok {
		return in, invalidf("unknown currency %q", in.Currency)
	}
	if n := len(in.NumberHint); n != 0 && (n != 4 || !allDigits(in.NumberHint)) {
		return in, invalidf("number hint must be the last 4 digits")
	}
	return in, nil
}

func accountOf(a db.Account) Account {
	out := Account{ID: a.ID, Name: a.Name, Currency: a.Currency, OpeningBalanceMinor: a.OpeningBalanceMinor,
		IsDemo: a.IsDemo, ArchivedAt: a.ArchivedAt, CreatedAt: a.CreatedAt}
	if a.NumberHint != nil {
		out.NumberHint = *a.NumberHint
	}
	return out
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateAccount adds an account. A name already used by another of the user's accounts (after
// normalisation) is ErrConflict.
func (l *Ledger) CreateAccount(ctx context.Context, userID uuid.UUID, in AccountInput) (Account, error) {
	in, err := in.clean()
	if err != nil {
		return Account{}, err
	}
	var out Account
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		a, err := q.CreateAccount(ctx, db.CreateAccountParams{ID: platform.NewID(), UserID: userID, Name: in.Name,
			NameN: nameKey(in.Name), Currency: in.Currency, NumberHint: optional(in.NumberHint),
			OpeningBalanceMinor: in.OpeningBalanceMinor, IsDemo: in.IsDemo})
		out = accountOf(a)
		return err
	})
	return out, dbErr("create account", err)
}

// GetAccount returns one account.
func (l *Ledger) GetAccount(ctx context.Context, userID, id uuid.UUID) (Account, error) {
	var out Account
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		a, err := q.GetAccount(ctx, db.GetAccountParams{UserID: userID, ID: id})
		out = accountOf(a)
		return err
	})
	return out, dbErr("get account", err)
}

// ListAccounts returns the user's accounts in natural Polish order of their names.
func (l *Ledger) ListAccounts(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	var out []Account
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListAccounts(ctx, userID)
		for i := range rows {
			out = append(out, accountOf(rows[i]))
		}
		return err
	})
	return out, dbErr("list accounts", err)
}

// UpdateAccount replaces the editable fields. Archiving keeps an existing archive time.
func (l *Ledger) UpdateAccount(ctx context.Context, userID, id uuid.UUID, in AccountInput) (Account, error) {
	in, err := in.clean()
	if err != nil {
		return Account{}, err
	}
	var out Account
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetAccount(ctx, db.GetAccountParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		archived := cur.ArchivedAt
		switch {
		case !in.Archived:
			archived = nil
		case archived == nil:
			now := l.clock.Now()
			archived = &now
		}
		a, err := q.UpdateAccount(ctx, db.UpdateAccountParams{UserID: userID, ID: id, Name: in.Name, NameN: nameKey(in.Name),
			Currency: in.Currency, NumberHint: optional(in.NumberHint), OpeningBalanceMinor: in.OpeningBalanceMinor, ArchivedAt: archived})
		out = accountOf(a)
		return err
	})
	return out, dbErr("update account", err)
}

// DeleteAccount deletes the account with its imports, transactions, annotations and overrides.
func (l *Ledger) DeleteAccount(ctx context.Context, userID, id uuid.UUID) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteAccount(ctx, db.DeleteAccountParams{UserID: userID, ID: id})
		return err
	})
	return notFound("delete account", n, err)
}

// SuggestAccounts returns the active accounts whose number hint equals the last four digits of
// ownAccount (PLAN §4.4, account suggestion). Fewer than four digits match nothing.
func (l *Ledger) SuggestAccounts(ctx context.Context, userID uuid.UUID, ownAccount string) ([]Account, error) {
	digits := onlyDigits(ownAccount)
	if len(digits) < 4 {
		return nil, nil
	}
	var out []Account
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.FindAccountsByNumberHint(ctx, db.FindAccountsByNumberHintParams{UserID: userID, NumberHint: optional(digits[len(digits)-4:])})
		for i := range rows {
			out = append(out, accountOf(rows[i]))
		}
		return err
	})
	return out, dbErr("suggest accounts", err)
}

func allDigits(s string) bool { return onlyDigits(s) == s }

func onlyDigits(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}
