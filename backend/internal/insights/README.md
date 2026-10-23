# insights

The insight charts of the table view (PLAN §4.6, Phase 11): summary, spending by category, cash flow by month, month pace, top payees and daily spending, served by `POST /insights/*`.

The aggregations run in `internal/query` (`insights.go`) on the same compiled context as the table, so every chart agrees with the rows shown. This package holds the rules on top of them as pure functions: the days a period has, the previous-period delta, cumulative pace arrays, the heatmap's date range. `TestChartParity` checks every chart's table twin against the legacy app on the demo year.

The overview (Phase 12, `POST /overview`) is one call per screen: `Overview` reads the context
without its period and returns every card's data for one month (KPIs, highlights, budgets,
recurring payments, income sources, Sankey, largest expenses, savings rate, weekdays, balance and,
through `internal/investments`, the investment cards). The SQL is in `query/overview.go`; the rules
(`monthView`, budget status, default budgets with `RoundNiceMinor`, `FindRecurring`, the highlight
rules in `highlights.go`) are ports of the legacy functions. Highlights are returned as facts
(kind, tone, numbers); the browser writes the sentence. `TestOverviewParity` compares every card's
table twin and the highlight sentences with the legacy app in 11 recorded views.
