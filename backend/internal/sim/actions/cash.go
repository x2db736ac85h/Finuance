package actions

import (
	"context"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Cash: a cash account, expenses typed in by hand, edited and deleted. (A receipt by photo is an
// assistant action: the photo goes to the model.)
func cash() []sim.Action {
	return []sim.Action{
		{Name: "cash.add", Area: "cash", Run: cashAdd},
		{Name: "cash.edit", Area: "cash", Run: cashEdit},
		{Name: "cash.delete", Area: "cash", Run: cashDelete},
	}
}

var cashPayees = []string{"TARG NA PLACU", "KIOSK RUCH", "PIEKARNIA KŁOS", "BAZAR RÓŻYCKIEGO", "LODZIARNIA BAŁTYK", "TAKSÓWKA"}

// cashAccount is the persona's cash account, made the first time it is needed.
func cashAccount(ctx context.Context, a *sim.Actor) (openapi_types.UUID, error) {
	if id := a.CashAccountID(); id != (openapi_types.UUID{}) {
		return id, nil
	}
	kind := gen.AccountKindCash
	r, err := a.API.CreateAccountWithResponse(ctx, gen.CreateAccountJSONRequestBody{Name: sim.CashAccount, Kind: &kind})
	if err != nil {
		return openapi_types.UUID{}, err
	}
	if err := sim.Expect("make a cash account", r.HTTPResponse, http.StatusCreated); err != nil {
		return openapi_types.UUID{}, err
	}
	a.SetCashAccount(r.JSON201.Id)
	return r.JSON201.Id, nil
}

func cashAdd(ctx context.Context, a *sim.Actor) error {
	acc, err := cashAccount(ctx, a)
	if err != nil {
		return err
	}
	day := sim.AddDays(a.DataDay(), -a.IntN(3))
	r, err := a.API.AddTransactionWithResponse(ctx, nil, gen.AddTransactionJSONRequestBody{AccountId: acc, Payee: sim.Pick(a, cashPayees...),
		AmountMinor: -int64(300 + a.IntN(80)*100), BookedOn: openapi_types.Date{Time: day.Time()},
		CategoryName: ptr(sim.Pick(a, "Artykuły spożywcze", "Restauracje i kawiarnie", "Transport"))})
	if err != nil {
		return err
	}
	if err := sim.Expect("type in an expense", r.HTTPResponse, http.StatusCreated); err != nil {
		return err
	}
	a.SetHasData(true)
	return nil
}

// cashRow is one of the persona's hand-typed rows, or nothing.
func cashRow(ctx context.Context, a *sim.Actor) (*gen.TransactionRow, error) {
	acc := a.CashAccountID()
	if acc == (openapi_types.UUID{}) {
		return nil, sim.ErrNothingToDo
	}
	list, err := rows(ctx, a, gen.QueryContext{AccountIds: &[]openapi_types.UUID{acc}}, 50)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sim.ErrNothingToDo
	}
	return &list[a.IntN(len(list))], nil
}

func cashEdit(ctx context.Context, a *sim.Actor) error {
	row, err := cashRow(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.UpdateTransactionWithResponse(ctx, row.Id, nil, gen.UpdateTransactionJSONRequestBody{
		AmountMinor: ptr(-int64(300 + a.IntN(80)*100)), Payee: ptr(sim.Pick(a, cashPayees...))})
	if err != nil {
		return err
	}
	return sim.Expect("edit a typed expense", r.HTTPResponse, http.StatusOK)
}

func cashDelete(ctx context.Context, a *sim.Actor) error {
	row, err := cashRow(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.DeleteTransactionWithResponse(ctx, row.Id)
	if err != nil {
		return err
	}
	return sim.Expect("delete a typed expense", r.HTTPResponse, http.StatusNoContent)
}
