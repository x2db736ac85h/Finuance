# 0004. Money as `int64` minor units

- Status: accepted
- Date: 2026-10-20

## Context

The legacy app stored amounts as JavaScript `number` (float64). Sums over thousands of rows pick up binary rounding errors (`0.1 + 0.2`), and equality checks on amounts (de-duplication, budgets) become fragile. Bank exports use exact decimal amounts with a currency, mostly PLN.

## Decision

- Every monetary amount is an **`int64` count of minor units** plus an **ISO 4217 currency code**: `-127,13 PLN` → `{amountMinor: -12713, currency: "PLN"}`.
- `internal/money` holds a currency → exponent map. PLN, EUR, USD, GBP, CHF, CZK, SEK, NOK, DKK and HUF use 2. An unknown currency defaults to 2 and logs a warning.
- Parsing a decimal string into minor units is exact (string arithmetic, no float step). An amount with more fraction digits than the exponent allows is rejected, not rounded.
- PostgreSQL stores `bigint amount_minor` + `text currency`. Aggregates use `sum(bigint)` → `numeric`, which is checked and converted back to `int64`.
- The API carries `amountMinor` as an integer. The frontend formats for display. The server pre-formats strings only where that helps (highlights).
- Sign convention: negative = money out, positive = money in.
- Exception: ratios and projections (savings rate, yield on cost, FI/RE) are not money amounts and may use `float64` (see the investments ADR, Phase 12).

## Consequences (positive, negative, follow-ups)

- Sums and comparisons are exact; de-duplication can compare amounts with `=`.
- `int64` minor units overflow only beyond ±9.2 × 10¹⁶ PLN. That's not a concern.
- Every boundary (CSV parse, JSON, SQL, display) must convert explicitly. That is the point, but it adds code.
- JSON integers above 2⁵³ would lose precision in JavaScript. Realistic amounts are far below that. The API documents the limit.
- Multi-currency totals are out of scope until the FX stretch goal. Aggregates are per currency.

## Alternatives considered

- **float64:** the legacy approach, which has the rounding problems above.
- **`shopspring/decimal` / `numeric` everywhere:** exact, but slower, allocation-heavy in Go, and awkward in JSON and TS. Minor units are the industry norm.
- **Strings in the API:** exact, but every client must parse them. Integers are simpler and type-safe.
