package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// LoadDemoData creates the demo account with the committed demo year (PLAN task 8.10).
func (a *api) LoadDemoData(ctx context.Context, _ gen.LoadDemoDataRequestObject) (gen.LoadDemoDataResponseObject, error) {
	seed, err := a.ledger.SeedDemo(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.LoadDemoData201JSONResponse{AccountId: seed.Account.ID, ImportId: seed.ImportID, Transactions: seed.Result.New}, nil
}
