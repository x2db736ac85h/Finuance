package actions

import (
	"context"
	"net/http"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Insights: the six charts of the charts pane for one period, and the monthly overview.
func insights() []sim.Action {
	return []sim.Action{
		{Name: "insights.charts", Area: "insights", Needs: sim.NeedData, Run: insightCharts},
		{Name: "insights.overview", Area: "insights", Needs: sim.NeedData, Run: insightOverview},
	}
}

// insightCharts loads the six charts as the charts pane does when a period is chosen.
func insightCharts(ctx context.Context, a *sim.Actor) error {
	qc := scope(a)
	summary, err := a.API.InsightSummaryWithResponse(ctx, gen.InsightSummaryJSONRequestBody{Context: qc})
	if err != nil {
		return err
	}
	if err := sim.Expect("summary", summary.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	cats, err := a.API.InsightCategoriesWithResponse(ctx, gen.InsightCategoriesJSONRequestBody{Context: qc})
	if err != nil {
		return err
	}
	if err := sim.Expect("categories", cats.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	flow, err := a.API.InsightCashflowWithResponse(ctx, gen.InsightCashflowJSONRequestBody{Context: qc})
	if err != nil {
		return err
	}
	if err := sim.Expect("cash flow", flow.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	pace, err := a.API.InsightPaceWithResponse(ctx, gen.InsightPaceJSONRequestBody{Context: qc,
		Compare: sim.Pick(a, gen.PaceRequestComparePrev, gen.PaceRequestCompareYoy, gen.PaceRequestCompareAvg3)})
	if err != nil {
		return err
	}
	if err := sim.Expect("pace", pace.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	payees, err := a.API.InsightPayeesWithResponse(ctx, gen.InsightPayeesJSONRequestBody{Context: qc, Top: gen.N10,
		Scope: gen.PayeesRequestScopePeriod})
	if err != nil {
		return err
	}
	if err := sim.Expect("payees", payees.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	daily, err := a.API.InsightDailyWithResponse(ctx, gen.InsightDailyJSONRequestBody{Context: qc, Range: gen.DailyRequestRangePeriod})
	if err != nil {
		return err
	}
	return sim.Expect("daily", daily.HTTPResponse, http.StatusOK)
}

func insightOverview(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.OverviewWithResponse(ctx, gen.OverviewJSONRequestBody{
		Months: sim.Pick(a, gen.OverviewRequestMonthsN6, gen.OverviewRequestMonthsN6, gen.OverviewRequestMonthsN12)})
	if err != nil {
		return err
	}
	return sim.Expect("overview", r.HTTPResponse, http.StatusOK)
}
