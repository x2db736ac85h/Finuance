# insights

The insight charts of the table view (PLAN §4.6, Phase 11): summary, spending by category, cash flow by month, month pace, top payees and daily spending, served by `POST /insights/*`.

The aggregations run in `internal/query` (`insights.go`) on the same compiled context as the table, so every chart agrees with the rows shown. This package holds the rules on top of them as pure functions: the days a period has, the previous-period delta, cumulative pace arrays, the heatmap's date range. `TestChartParity` checks every chart's table twin against the legacy app on the demo year.

The overview (Phase 12) adds its month-centric cards here.
