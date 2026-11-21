package actions

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Labels: a category or a note on one row, a group or a category on many (a bulk selection).
func labels() []sim.Action {
	return []sim.Action{
		{Name: "labels.category", Area: "labels", Needs: sim.NeedData, Run: labelCategory},
		{Name: "labels.note", Area: "labels", Needs: sim.NeedData, Run: labelNote},
		{Name: "labels.bulk-category", Area: "labels", Needs: sim.NeedData, Run: labelBulkCategory},
		{Name: "labels.bulk-group", Area: "labels", Needs: sim.NeedData, Run: labelBulkGroup},
	}
}

// bulk is a bulk change of the rows ids: change is "category" or "set", with its fields (the
// generated body has anonymous structs for both, so it is built from JSON).
func bulk(ids []uuid.UUID, change string, fields map[string]any) (gen.BulkUpdateTransactionsJSONRequestBody, error) {
	var b gen.BulkUpdateTransactionsJSONRequestBody
	raw, err := json.Marshal(map[string]any{"ids": ids, change: fields})
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(raw, &b)
	return b, err
}

// rows reads a window of the persona's transactions, filtered as given.
func rows(ctx context.Context, a *sim.Actor, qc gen.QueryContext, limit int) ([]gen.TransactionRow, error) {
	r, err := a.API.QueryTransactionsWithResponse(ctx, gen.QueryTransactionsJSONRequestBody{Context: qc, Limit: limit, Sort: sort(a)})
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("transactions", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return r.JSON200.Rows, nil
}

// someRow is one of the persona's recent transactions, or nothing.
func someRow(ctx context.Context, a *sim.Actor) (*gen.TransactionRow, error) {
	list, err := rows(ctx, a, gen.QueryContext{Period: &gen.Period{Preset: ptr(gen.L3m)}}, 50)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sim.ErrNothingToDo
	}
	return &list[a.IntN(len(list))], nil
}

// someCategory is one of the user's categories, or nothing.
func someCategory(ctx context.Context, a *sim.Actor) (gen.Category, error) {
	r, err := a.API.ListCategoriesWithResponse(ctx, nil)
	if err != nil {
		return gen.Category{}, err
	}
	if err := sim.Expect("categories", r.HTTPResponse, http.StatusOK); err != nil {
		return gen.Category{}, err
	}
	if len(*r.JSON200) == 0 {
		return gen.Category{}, sim.ErrNothingToDo
	}
	return (*r.JSON200)[a.IntN(len(*r.JSON200))], nil
}

func labelCategory(ctx context.Context, a *sim.Actor) error {
	row, err := someRow(ctx, a)
	if err != nil {
		return err
	}
	cat, err := someCategory(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.UpdateTransactionWithResponse(ctx, row.Id, nil, gen.UpdateTransactionJSONRequestBody{CategoryId: &cat.Id})
	if err != nil {
		return err
	}
	return sim.Expect("set a category", r.HTTPResponse, http.StatusOK)
}

var notes = []string{"zwrot dla Ani", "do rozliczenia", "prezent", "służbowe", "na wakacje", "sprawdzić", ""}

func labelNote(ctx context.Context, a *sim.Actor) error {
	row, err := someRow(ctx, a)
	if err != nil {
		return err
	}
	r, err := a.API.UpdateTransactionWithResponse(ctx, row.Id, nil, gen.UpdateTransactionJSONRequestBody{Note: ptr(sim.Pick(a, notes...))})
	if err != nil {
		return err
	}
	return sim.Expect("write a note", r.HTTPResponse, http.StatusOK)
}

// selection is what a person ticks in the table: the rows of a search, at most 40.
func selection(ctx context.Context, a *sim.Actor) ([]uuid.UUID, error) {
	list, err := rows(ctx, a, gen.QueryContext{Search: ptr(sim.Pick(a, searches...)), Period: period(a)}, 40)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sim.ErrNothingToDo
	}
	ids := make([]uuid.UUID, len(list))
	for i := range list {
		ids[i] = list[i].Id
	}
	return ids, nil
}

func labelBulkCategory(ctx context.Context, a *sim.Actor) error {
	ids, err := selection(ctx, a)
	if err != nil {
		return err
	}
	cat, err := someCategory(ctx, a)
	if err != nil {
		return err
	}
	body, err := bulk(ids, "category", map[string]any{"id": cat.Id})
	if err != nil {
		return err
	}
	r, err := a.API.BulkUpdateTransactionsWithResponse(ctx, nil, body)
	if err != nil {
		return err
	}
	return sim.Expect("categorise a selection", r.HTTPResponse, http.StatusOK)
}

func labelBulkGroup(ctx context.Context, a *sim.Actor) error {
	group, err := someGroup(ctx, a)
	if err != nil {
		return err
	}
	ids, err := selection(ctx, a)
	if err != nil {
		return err
	}
	body, err := bulk(ids, "set", map[string]any{"groupId": group.Id})
	if err != nil {
		return err
	}
	r, err := a.API.BulkUpdateTransactionsWithResponse(ctx, nil, body)
	if err != nil {
		return err
	}
	return sim.Expect("group a selection", r.HTTPResponse, http.StatusOK)
}
