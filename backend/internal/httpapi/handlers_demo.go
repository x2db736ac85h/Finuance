package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// LoadDemoData creates the demo account with the committed demo year (PLAN task 8.10).
func (a *api) LoadDemoData(ctx context.Context, _ gen.LoadDemoDataRequestObject) (gen.LoadDemoDataResponseObject, error) {
	u := identity(ctx).User.ID
	seed, err := a.ledger.SeedDemo(ctx, u)
	if err != nil {
		return nil, err
	}
	a.reclassifyAfter(ctx, u)
	return gen.LoadDemoData201JSONResponse{AccountId: seed.Account.ID, ImportId: seed.ImportID, Transactions: seed.Result.New}, nil
}
