# investments

Manual investment data (PLAN §4.9, legacy §32.4) and the three overview cards built on it.

- `Doc` is a user's document: PPK contributions and value, positions (invested, value, annual
  income), the safe withdrawal rate and the expected real return. It is versioned like the
  preferences (`GET`/`PUT /investments`, `409 stale_version` with the current document) and stored by
  `ledger.SaveInvestments`. `Validate` holds the limits.
- `Summarize` is the Investment card (legacy `o_inv`), `YieldOnCost` the Yield-on-cost card
  (`o_yoc`; without income entered by hand it uses the dividends and interest found in the
  transactions), `FireOf` the FI/RE card (`o_fire`).

Amounts are minor units and rates basis points; ratios and the years-to-FI projection use float64,
every amount is an exact integer sum rounded once. `insights.Overview` calls these, and
`TestInvestmentCardsParity` (`internal/insights`) checks all three against the legacy app on five
recorded portfolios.
