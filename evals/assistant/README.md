# Assistant evaluations

70 golden questions (`questions.json`, Polish and English) on the demo year with today frozen at Thursday 2025-03-20: relative dates, merchant spelling, categories, groups, rankings and trends, comparisons, periods outside the data, items vs transactions, income and savings, the month overview and recurring payments, budgets and group summaries (Phase 15), proposals, goals and splits (Phase 20), and the advice library and digests (Phase 20 PR 3; the setup publishes the starter articles).

Each question names the tools a lookup may use, the arguments it must have (date ranges especially), and reference lookups whose results the answer must contain. The harness runs the references itself through the oracle-checked tools, so no expected number is written by hand.

```sh
# a model server configured like the API (FINUANCE_AI_BASE_URL or FINUANCE_AI_CONNECTIONS_FILE); Docker for the database
FINUANCE_AI_BASE_URL=http://localhost:1234/v1 make eval-assistant MODEL=qwen3-14b
FINUANCE_AI_CONNECTIONS_FILE=connections.json make eval-assistant CONNECTION=lmstudio MODEL=qwen3-8b THINK=off
```

Options: `THINK` (off, auto, low, medium, high), `MODE` (auto, tools, plan), `TOOLSET` (full, compact). Each run writes `results/<connection>__<model>__<think>.json` and rebuilds `docs/evals.md` from every result. The harness itself is tested in CI against the mock server (`internal/assistant/eval`).
