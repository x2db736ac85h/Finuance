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

// SeedDemo gives a user the fictional demo year (PLAN task 6.9): a demo account and one committed
// import of demo.Recent, ending yesterday. It commits inline (no job), so the data is there when it
// returns. A user who already has a demo account gets ErrConflict; if the import fails, the new
// account is removed again.
func (l *Ledger) SeedDemo(ctx context.Context, userID uuid.UUID) (Account, CommitResult, error) {
	accounts, err := l.ListAccounts(ctx, userID)
	if err != nil {
		return Account{}, CommitResult{}, err
	}
	for i := range accounts {
		if accounts[i].IsDemo {
			return Account{}, CommitResult{}, fmt.Errorf("the user already has a demo account (%s): %w", accounts[i].ID, ErrConflict)
		}
	}
	acc, err := l.CreateAccount(ctx, userID, AccountInput{Name: DemoAccountName, Currency: "PLN", IsDemo: true})
	if err != nil {
		return Account{}, CommitResult{}, err
	}
	res, err := l.seedImport(ctx, userID, acc.ID)
	if err != nil {
		return Account{}, CommitResult{}, errors.Join(err, l.DeleteAccount(ctx, userID, acc.ID))
	}
	acc, err = l.GetAccount(ctx, userID, acc.ID) // with the stats
	return acc, res, err
}

func (l *Ledger) seedImport(ctx context.Context, userID, accountID uuid.UUID) (CommitResult, error) {
	p, err := l.CreatePreview(ctx, userID, Upload{Filename: DemoFilename, Content: []byte(demo.Recent(l.clock.Now())), AccountID: &accountID})
	if err != nil {
		return CommitResult{}, err
	}
	if _, err := l.StartCommit(ctx, userID, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return CommitResult{}, err
	}
	return l.CommitImport(ctx, userID, p.Import.ID)
}
