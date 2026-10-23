package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/investments"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Investment data (PLAN §4.9): one versioned document per user, replaced as a whole.

// StaleInvestmentsError is returned when the client's version is not the stored one; it carries
// the current document so the client can merge and retry (409 stale_version).
type StaleInvestmentsError struct{ Current investments.Doc }

func (e *StaleInvestmentsError) Error() string {
	return fmt.Sprintf("investments are at version %d", e.Current.Version)
}

// Unwrap maps the error to 409 stale_version.
func (e *StaleInvestmentsError) Unwrap() error { return platform.ErrStaleVersion }

// Investments returns the user's investment document, or the empty one (version 0) before the
// first save.
func (l *Ledger) Investments(ctx context.Context, userID uuid.UUID) (investments.Doc, error) {
	var out investments.Doc
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		out, err = loadInvestments(ctx, q, userID)
		return err
	})
	return out, dbErr("investments", err)
}

func loadInvestments(ctx context.Context, q *db.Queries, userID uuid.UUID) (investments.Doc, error) {
	out := investments.Empty()
	p, err := q.GetInvestmentProfile(ctx, userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return out, err
	default:
		out = investments.Doc{Version: p.Version, SwrBp: int(p.SwrBp), RealReturnBp: int(p.RealReturnBp),
			PPK: investments.PPK{EmployeeMinor: p.PpkEmployeeMinor, EmployerMinor: p.PpkEmployerMinor, StateMinor: p.PpkStateMinor,
				ValueMinor: p.PpkValueMinor}}
	}
	rows, err := q.ListInvestmentPositions(ctx, userID)
	for i := range rows {
		r := &rows[i]
		out.Positions = append(out.Positions, investments.Position{ID: r.ID, Name: r.Name, Type: r.Type, InvestedMinor: r.InvestedMinor,
			ValueMinor: r.ValueMinor, AnnualIncomeMinor: r.AnnualIncomeMinor})
	}
	return out, err
}

// SaveInvestments replaces the document if doc.Version is the stored version (0 before the first
// save) and returns it with the new version. Position ids come from the client (stable keys
// while editing); an id another user already has is a conflict.
func (l *Ledger) SaveInvestments(ctx context.Context, userID uuid.UUID, doc investments.Doc) (investments.Doc, error) {
	if err := doc.Validate(); err != nil {
		return investments.Doc{}, err
	}
	var out investments.Doc
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := loadInvestments(ctx, q, userID)
		if err != nil {
			return err
		}
		if cur.Version != doc.Version {
			return &StaleInvestmentsError{Current: cur}
		}
		now := l.clock.Now()
		v, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: userID, PpkEmployeeMinor: doc.PPK.EmployeeMinor,
			PpkEmployerMinor: doc.PPK.EmployerMinor, PpkStateMinor: doc.PPK.StateMinor, PpkValueMinor: doc.PPK.ValueMinor,
			SwrBp: int32(doc.SwrBp), RealReturnBp: int32(doc.RealReturnBp), //nolint:gosec // bounded by Validate
			ExpectedVersion: doc.Version, Now: now})
		if errors.Is(err, pgx.ErrNoRows) { // changed between the read and the write
			return &StaleInvestmentsError{Current: cur}
		}
		if err != nil {
			return err
		}
		if err := q.DeleteInvestmentPositions(ctx, userID); err != nil {
			return err
		}
		for i, p := range doc.Positions {
			if _, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: p.ID, UserID: userID, Position: int32(i),
				Name: p.Name, Type: p.Type, InvestedMinor: p.InvestedMinor, ValueMinor: p.ValueMinor, AnnualIncomeMinor: p.AnnualIncomeMinor,
				Now: now}); err != nil {
				return err
			}
		}
		out = doc
		out.Version = v
		return nil
	})
	var stale *StaleInvestmentsError
	if errors.As(err, &stale) {
		return investments.Doc{}, err
	}
	return out, dbErr("save investments", err)
}
