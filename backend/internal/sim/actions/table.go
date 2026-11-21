package actions

import (
	"context"
	"net/http"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// The table: a window with a sort, a filter expression, a search, the distinct values of a column,
// an id range (what a bulk selection reads).
func table() []sim.Action {
	return []sim.Action{
		{Name: "table.window", Area: "table", Needs: sim.NeedData, Run: tableWindow},
		{Name: "table.filter", Area: "table", Needs: sim.NeedData, Run: tableFilter},
		{Name: "table.search", Area: "table", Needs: sim.NeedData, Run: tableSearch},
		{Name: "table.distinct", Area: "table", Needs: sim.NeedData, Run: tableDistinct},
		{Name: "table.range", Area: "table", Needs: sim.NeedData, Run: tableRange},
	}
}

func sort(a *sim.Actor) *gen.Sort {
	return &gen.Sort{Column: sim.Pick(a, gen.FilterColumnBookedOn, gen.FilterColumnBookedOn, gen.FilterColumnAmount, gen.FilterColumnPayee),
		Dir: ptr(sim.Pick(a, gen.Desc, gen.Desc, gen.Asc))}
}

func query(ctx context.Context, a *sim.Actor, op string, qc gen.QueryContext) error {
	r, err := a.API.QueryTransactionsWithResponse(ctx, gen.QueryTransactionsJSONRequestBody{Context: qc, Limit: 100,
		Offset: ptr(sim.Pick(a, 0, 0, 0, 100, 200)), Sort: sort(a), IncludeTotals: ptr(true)})
	if err != nil {
		return err
	}
	return sim.Expect(op, r.HTTPResponse, http.StatusOK)
}

func tableWindow(ctx context.Context, a *sim.Actor) error {
	return query(ctx, a, "table window", scope(a))
}

// filters are expressions of the filter language a person types into a column's filter.
var filters = []gen.ColumnFilter{
	{Column: gen.FilterColumnAmount, Cond: ptr("<-100")},
	{Column: gen.FilterColumnAmount, Cond: ptr("-50..-10")},
	{Column: gen.FilterColumnAmount, Cond: ptr(">0")},
	{Column: gen.FilterColumnPayee, Cond: ptr("market")},
	{Column: gen.FilterColumnPayee, Cond: ptr("!przelew")},
	{Column: gen.FilterColumnCategory, Cond: ptr("=Transport")},
	{Column: gen.FilterColumnDescription, Cond: ptr("zakup")},
}

func tableFilter(ctx context.Context, a *sim.Actor) error {
	qc := scope(a)
	qc.Filters = &[]gen.ColumnFilter{sim.Pick(a, filters...)}
	return query(ctx, a, "table filter", qc)
}

var searches = []string{"market", "kawiarnia", "apteka", "przelew", "kino", "zakup", "czynsz", "bar mleczny"}

func tableSearch(ctx context.Context, a *sim.Actor) error {
	qc := scope(a)
	qc.Search = ptr(sim.Pick(a, searches...))
	return query(ctx, a, "table search", qc)
}

func tableDistinct(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.DistinctValuesWithResponse(ctx, gen.DistinctValuesJSONRequestBody{Context: scope(a), Limit: ptr(50),
		Column: sim.Pick(a, gen.FilterColumnPayee, gen.FilterColumnCategory, gen.FilterColumnBankCategory, gen.FilterColumnAccount)})
	if err != nil {
		return err
	}
	return sim.Expect("distinct values", r.HTTPResponse, http.StatusOK)
}

func tableRange(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.TransactionRangeWithResponse(ctx, gen.TransactionRangeJSONRequestBody{Context: scope(a), Limit: 500, Sort: sort(a)})
	if err != nil {
		return err
	}
	return sim.Expect("id range", r.HTTPResponse, http.StatusOK)
}
