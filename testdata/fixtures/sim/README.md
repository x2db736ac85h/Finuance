# The simulator's sample statements

Written by `backend/internal/sim/statements` (PLAN 25.2, ADR 0043); `TestSamples` keeps them equal
to the generator. **Do not edit by hand**: change the generator and run
`go test ./internal/sim/statements/ -run TestSamples -update` in `backend/`.

The traffic simulator writes statements like these on demand for each of its personas. A day's rows
are a pure function of the persona and the date, so overlapping statements de-duplicate like real
overlapping exports. Every value is invented, and the account numbers use the all-zero form that
`tools/check-private.sh` allows.

| File | Layout | What it shows |
|---|---|---|
| `otter-current-account-2026-01-01-2026-01-31.csv` | Pekao-like | a month of the current account: salary, rent, subscriptions, a bill, daily spending, the transfer to savings |
| `otter-savings-2026-01-01-2026-03-31.csv` | mBank-like | three months of the savings account: the transfers in and the interest |
| `owl-current-account-2026-01-01-2026-01-31.csv` | mBank-like | another persona's current account in the other layout |
