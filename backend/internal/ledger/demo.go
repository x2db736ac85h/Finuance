package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	return l.SeedDemoDays(ctx, userID, 0)
}

// DemoRowsPerDay is how many rows the demo generator makes per day on average (917 in 442 days);
// the load test seeds a number of rows by asking for rows / DemoRowsPerDay days.
const DemoRowsPerDay = 917.0 / 442

// SeedDemoDays is SeedDemo with a history of the given number of days ending yesterday (0: the
// legacy demo span, 442 days) — the load test's large users (PLAN 17.5).
func (l *Ledger) SeedDemoDays(ctx context.Context, userID uuid.UUID, days int) (DemoSeed, error) {
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
	importID, res, err := l.seedImport(ctx, userID, acc.ID, days)
	if err != nil {
		return DemoSeed{}, errors.Join(err, l.DeleteAccount(ctx, userID, acc.ID))
	}
	acc, err = l.GetAccount(ctx, userID, acc.ID) // with the stats
	return DemoSeed{Account: acc, ImportID: importID, Result: res}, err
}

func (l *Ledger) seedImport(ctx context.Context, userID, accountID uuid.UUID, days int) (uuid.UUID, CommitResult, error) {
	content := demo.Recent(l.clock.Now())
	if days > 0 {
		now := l.clock.Now()
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		content = demo.CSV(end.AddDate(0, 0, -days), end)
	}
	p, err := l.CreatePreview(ctx, userID, Upload{Filename: DemoFilename, Content: []byte(content), AccountID: &accountID})
	if err != nil {
		return uuid.Nil, CommitResult{}, err
	}
	if _, err := l.StartCommit(ctx, userID, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return uuid.Nil, CommitResult{}, err
	}
	res, err := l.CommitImport(ctx, userID, p.Import.ID)
	return p.Import.ID, res, err
}
