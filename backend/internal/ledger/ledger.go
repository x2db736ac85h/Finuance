// Package ledger owns the user's financial records: accounts, categories, groups, imports and their
// staged rows, transactions, annotations, budgets and import profiles (PLAN §4.3, §4.4, §4.7).
//
// Every method takes the user id and runs through store.WithUser, so the database's row-level
// security applies on top of the explicit user_id filters. Methods return domain types, never sqlc
// rows (backend/CLAUDE.md).
package ledger

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

var (
	// ErrNotFound means the record doesn't exist or belongs to another user (indistinguishable on purpose).
	ErrNotFound = errors.New("ledger: not found")
	// ErrConflict means a unique rule is violated, e.g. an account or group name already in use.
	ErrConflict = errors.New("ledger: conflict")
	// ErrInvalid means the request breaks a domain rule (empty name, too many ids, …).
	ErrInvalid = errors.New("ledger: invalid")
	// ErrState means the record is in the wrong state for the operation (e.g. committing a failed import).
	ErrState = errors.New("ledger: wrong state")
)

// Ledger is the entry point; it is safe for concurrent use.
type Ledger struct {
	st    *store.Store
	clock platform.Clock
}

// New returns a Ledger over st. Time comes from clock (platform.SystemClock in production).
func New(st *store.Store, clock platform.Clock) *Ledger { return &Ledger{st: st, clock: clock} }

// dbErr maps database errors to the package's sentinel errors and wraps everything else with what.
func dbErr(what string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%s: %w", what, ErrConflict)
		case "23503", "23514": // foreign_key_violation, check_violation
			return fmt.Errorf("%s: %w: %s", what, ErrInvalid, pgErr.ConstraintName)
		case "22021": // character_not_in_repertoire: a NUL character in text (JSON allows \u0000)
			return fmt.Errorf("%s: %w: text must not contain NUL characters", what, ErrInvalid)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// notFound turns a zero row count into ErrNotFound.
func notFound(what string, n int64, err error) error {
	if err != nil {
		return dbErr(what, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ErrInvalid)
}
