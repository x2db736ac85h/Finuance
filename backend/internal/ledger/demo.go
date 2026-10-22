package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/demo"
)

// DemoAccountName and DemoFilename name what SeedDemo creates.
const (
	DemoAccountName = "Demo account"
	DemoFilename    = "demo-year (fictional).csv"
)

// DemoSeed is what SeedDemo created.
type DemoSeed struct {
	Account  Account // with its stats
	ImportID uuid.UUID
	Result   CommitResult
}

// SeedDemo gives a user the fictional demo year (PLAN task 6.9): a demo account and one committed
// import of demo.Recent, ending yesterday. It commits inline (no job), so the data is there when it
// returns (POST /demo-data relies on that). A user who already has a demo account gets ErrConflict;
// if the import fails, the new account is removed again.
func (l *Ledger) SeedDemo(ctx context.Context, userID uuid.UUID) (DemoSeed, error) {
	accounts, err := l.ListAccounts(ctx, userID)
	if err != nil {
		return DemoSeed{}, err
	}
	for i := range accounts {
		if accounts[i].IsDemo {
			return DemoSeed{}, fmt.Errorf("the user already has a demo account (%s): %w", accounts[i].ID, ErrConflict)
		}
	}
	acc, err := l.CreateAccount(ctx, userID, AccountInput{Name: DemoAccountName, Currency: "PLN", IsDemo: true})
	if err != nil {
		return DemoSeed{}, err
	}
	importID, res, err := l.seedImport(ctx, userID, acc.ID)
	if err != nil {
		return DemoSeed{}, errors.Join(err, l.DeleteAccount(ctx, userID, acc.ID))
	}
	acc, err = l.GetAccount(ctx, userID, acc.ID) // with the stats
	return DemoSeed{Account: acc, ImportID: importID, Result: res}, err
}

func (l *Ledger) seedImport(ctx context.Context, userID, accountID uuid.UUID) (uuid.UUID, CommitResult, error) {
	p, err := l.CreatePreview(ctx, userID, Upload{Filename: DemoFilename, Content: []byte(demo.Recent(l.clock.Now())), AccountID: &accountID})
	if err != nil {
		return uuid.Nil, CommitResult{}, err
	}
	if _, err := l.StartCommit(ctx, userID, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return uuid.Nil, CommitResult{}, err
	}
	res, err := l.CommitImport(ctx, userID, p.Import.ID)
	return p.Import.ID, res, err
}
