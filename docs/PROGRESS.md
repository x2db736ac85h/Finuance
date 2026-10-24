# Progress

**Current phase:** 15 — Product depth: categories, rules, transfers, assistant actions, group reports · **Branch:** phase/15-product-depth · **Status:** in progress

## Decisions from our Product Engineer
- Name: **Finuance** · GitHub owner: **x2db736ac85h** (module path `github.com/x2db736ac85h/finuance`) · License: **MIT**
- Git workflow: one branch per phase (`phase/NN-name`), merged into `main` with a merge commit so every phase stays visible in history. `main` may be rewritten once, to drop the unscrubbed legacy commit.
- Banks for fixtures (Phase 1): **Pekao**, **PKO BP**, **mBank**. Pekao worked best with the MVP. Samples sit in `testdata/private/` (gitignored, never committed).
- Demo hosting: TBD (ask in Phase 18)
- Homelab (ingress, cert-manager issuer, storage class, backup bucket): TBD (ask in Phase 16)
- Assistant LLM endpoint and model (Phase 13): **LM Studio** for dev and prod, but the assistant is **provider- and model-agnostic**: users pick from whatever connections the instance offers (LM Studio, Ollama, vLLM, OpenAI, Anthropic, any OpenAI- or Anthropic-compatible server, local or cloud). Thinking/reasoning is used when the model supports it.
- Test data policy (Phase 1): committed fixtures are synthetic and must cover the edge cases. Real exports are only ever used through the anonymiser; Claude runs it on the samples in `testdata/private/`, and our Product Engineer reviews the output before anything goes into `testdata/fixtures/real/`.
- Anonymiser keeps category, currency and operation-type columns verbatim. Amounts keep their original number of decimals.
- Legacy oracle pins Playwright 1.56.1 (matches the Chromium in cloud sessions; CI installs the same build).
- Phase 2: `/readyz` returns 503 when the DB is reachable but migrations are behind. `X-Request-ID` is accepted from any client when it is ≤ 64 chars of `[A-Za-z0-9_-]`, otherwise generated.
- Phase 2 tooling: oapi-codegen and govulncheck as Go `tool` deps; sqlc and golangci-lint as pinned, checksum-verified binaries in `backend/bin/`. OTel over OTLP/HTTP. No `worker` Compose service until Phase 6.
- Phase 3: Claude writes all of `ingest`/`money`, with explanatory comments so our Product Engineer can tweak it later (no hand-written parts). The ingest engine is a **strict port** of the legacy rules, including auto header detection; the mBank/PKO BP header fix moves to Phase 9 (import preview). The legacy demo generator is ported now (`internal/demo`) for the 100k-row benchmark.
- Phase 4: RLS is **enabled and forced** on every user-owned table (ADR 0007; forced also binds the owner role). The `pl_natural` collation is applied in queries (`ORDER BY … COLLATE pl_natural`), not on columns. Phase 4 writes the set-based commit core (§4.4 steps 1, 1b, 2) and the delete-import orphan cleanup as `ledger` methods; Phase 6 adds the River job, category upsert, stats, profiles and `Rematerialize`. Investments get schema, RLS and queries now; the repository comes with their API.
- Phase 5: `golang.org/x/crypto` (argon2id), `golang.org/x/time/rate` and `github.com/oapi-codegen/runtime` (the generator's support library, needed once the API has path parameters and uuid fields) approved as direct dependencies. `finuance admin set-password --email … --password-stdin` is added next to `create-user` (no email sending, so admins reset forgotten passwords; it revokes the user's sessions). The first preferences save must send `version: 0`; anything else is `409 stale_version`.
- Phase 6: `POST /imports` **pre-fills `accountId`** when the own-account column matches exactly one account by its last 4 digits (the user can change it; the fast path relies on it). Defaults: `seed-demo` refuses if the user already has a demo account; deleting an account's last import keeps the account; only `POST /imports` gets the `FINUANCE_MAX_UPLOAD_BYTES` body limit, everything else stays at 1 MiB.
- Phase 7: exact-match text filters (`=v`) compare whitespace-collapsed normalised values, so `=KINO POD GWIAZDAMI WARSZAWA` matches the padded bank value (legacy: no match; ALLOWED_DIFFS row 12). Amounts and dates read as text (the text fallback on number/date columns, the value checklist, `excluded`, export) use one fixed format: `-79,99` (comma decimals, no thousands separator) and `dd.mm.yyyy`.
- Phase 7 gate: **`textlike` is marked LEAKPROOF** in `bootstrap.sql` (option a), so search uses the trigram index under RLS; existing databases need the one-line `ALTER FUNCTION` once (docs/perf.md). **`includeTotals: false`** added to `POST /transactions/query` for scrolling windows.
- Phase 8: dev dependencies `@types/react`, `@types/react-dom` and `@eslint/js` approved (beyond §7.2). TypeScript is pinned to 5.9.3 (openapi-typescript 7 needs `typescript ^5`). `POST /demo-data` commits inline through `ledger.SeedDemo` (no job), so the account is ready when the request returns.
- Phase 9: the automatic header rule is fixed in the backend (the text-only row just before the first row with a date and a number; legacy rule otherwise), recorded in `ALLOWED_DIFFS.md` row 4. The import preview is a window over the app (legacy `.modal` look) with its own URL `/imports/:id`.
- Phase 10: one phase with one gate, pushed in two halves (table first, then groups, selection and duplicates). `POST /transactions/bulk` takes explicit ids only (the selection; ranges resolved through `/transactions/ids`).
- Public repository (2026-10-22): docs and code comments refer to "our Product Engineer"; test data uses a neutral sample user ("Alex", `alex@example.com`); git history is left as it is. README with screenshots, `SECURITY.md` (private vulnerability reporting), `CONTRIBUTING.md` (issues welcome, no pull requests yet), `docs/README.md` index, and a README in every placeholder folder.
- Phase 11: the aggregation SQL lives in `internal/query/insights.go` (reusing the compiled `base`/`scoped` CTEs; no change to the store guard), and `internal/insights` holds only the pure rules. Cash flow returns every month of `base`; the client cuts it to fit/6/12/24/36/all. The legacy oracle records each chart's table twin on the demo file, and Go tests compare the API against it. One gate, two pushes (backend + chart library + charts; then pane, modal and actions).
- Phase 12: one `POST /overview {context, month, months}` per screen with full lists (the browser cuts top-N for the small and large card sizes). The legacy oracle records every overview card's table twin and the highlight sentences on the demo year (several months and one filtered view), and Go tests compare against it. Uncategorised spending shows its default budget only (budgets are stored per category). One gate, two pushes (backend: overview, budgets, investments; then the page and cards).
- Phase 13: connections are **instance config** (a connections file with named local and cloud connections; `FINUANCE_AI_BASE_URL` stays as a one-connection shorthand); users pick a connection and model, never a URL; per-user API keys are in the backlog. Two wire adapters (OpenAI Chat Completions, Anthropic Messages) behind one internal chat model; per-model capabilities (tools, reasoning, context window) detected and learned. Cloud connections need an admin flag per connection, a one-time per-user consent and masking of long digit runs in tool results. Tools: the three legacy tools plus month overview and recurring payments (a compact set for small/plan-mode models; evals decide the default). Typed tools only, no model-written SQL. Chats are kept 48 h by default; each user picks 1–7 days or "forever" (with a warning); an hourly cleanup job enforces it; `FINUANCE_AI_MAX_CHATS` = 25. `FINUANCE_AI_RATE_PER_HOUR` (default 30). One gate, two pushes (backend, mock and eval harness; then the panel and E2E). The sandbox can't reach models: `finuance admin llm-probe` records real streams at the gate, and our Product Engineer runs the evals.
- Phase 14: dev dependency `@axe-core/playwright` approved. Release without new third-party GitHub Actions: buildx cross-compiles (no QEMU), cosign and syft run as pinned, checksum-verified binaries (like gitleaks), Trivy as the pinned container already used; images `ghcr.io/x2db736ac85h/finuance-api` and `finuance-web`. The gate tests `v0.1.0-rc.1` (tagged on the phase branch); `v0.1.0` is tagged on `main` after the merge. Migrated chats count as active from the moment of migration (they then follow the user's retention; the wizard shows it). `compose.prod.yaml` becomes a standalone file (only itself + `.env`, bootstrap inline as a Compose config) and replaces the overlay. One gate, three pushes (migration; E2E, parity harness, a11y, perf, PARITY; release).
- Phase 14 gate (2026-10-23): a HIGH CVE in the web image's base (brotli-libs, CVE-2026-111497, not reachable: nginx doesn't link it) is accepted in `.trivyignore` with an expiry of **one week** (`exp:2026-10-30`); after that the scans fail again. Every accepted vulnerability needs a reason and an expiry. **Before the public demo there must be a full security scan with nothing ignored** (Follow-ups: Phase 17 and the Phase 18 gate).
- Phase 15: the table **shows transfers** with a "Transfer" badge and a chip to hide them, while insights and overview leave them out (plan; ADR). A category set by a rule records the rule (`category_rule_id`); saving, reordering, disabling or deleting a rule re-applies all rules from scratch in the background (rule categories reset to the bank's, then rules in position order, last match wins), and manual categories are never touched. Assistant action proposals (15.7) are built now; applied actions are logged as a structured line until Phase 17's `audit_log` turns them into audit events. FX conversion (15.6) and assistant-named rule suggestions go to the backlog. The parity harness compares with transfers included (the legacy app has none). One gate, three pushes: (1) categories, rules, transfers; (2) bulk edit, cadence, group reports, rule suggestions; (3) assistant tools and action proposals.
- Known MVP failures to reproduce: mBank's header is not on line 1 (long preamble); PKO BP column names are not picked up correctly.

## Current phase tasks
- [x] 15.1 category management: Settings → Categories (counts incl. by hand / by rules, bank names it also gets, new, rename, merge, delete → uncategorised); `GET /categories/stats`, `POST /categories`, `PATCH`/`DELETE /categories/{id}`, `POST /categories/{id}/merge`; `category_aliases` + the `bank_category_map` view keep renames, merges and deletes across re-imports; colour slots keep the all-time rank (unchanged query)
- [x] 15.2 rules engine: migration `00008_rules_transfers.sql`, `internal/rules` (CRUD, reorder, preview, `Reclassify`), match compiled by `query` (`query/rules.go`), from scratch in position order (manual > rule > bank, last match wins), `reclassify` job after commits, migrations, deletes, duplicate decisions, demo data, category merges/deletes and every rule change; `GET`/`POST /rules`, `PUT`/`DELETE /rules/{id}`, `PUT /rules/order`, `POST /rules/preview`, `POST /rules/apply`; toolbar *Save filters as rule…*, Settings → Rules (drag and ↑/↓ reorder, enable, edit, delete, apply now), rule window with live preview, inspector "by the rule …"; ADR 0017
- [x] 15.3 transfer detection in the reclassify job (opposite amounts, same currency, another account of the same user, ≤ 3 days, smallest gap first, `seq` tie-break, greedy), `transactions.transfer_source` (pair/rule/manual), rules can mark transfers, inspector *Not a transfer* (unpairs both) / *Mark as transfer* / *Automatic* via `PATCH /transactions/{id}` `transfer`, decisions kept in `tx_annotations`; *Transfer* badge, Columns menu toggle + "transfers hidden" chip; insights, overview and the assistant's view scope without transfers; ADR 0018
- [x] 15.5 bulk edit: *Set category* and *Mark as transfer* in the quick bar (`POST /transactions/bulk`: exactly one of `set`, `category {id | reset}`, `transfer yes/no/auto`); Shift+↑/↓ (and Shift+J/K) range selection; `TestBulkClassifyHTTP`, Vitest quick bar + keys
- [x] 15.4 recurring cadence: weekly and yearly besides the unchanged monthly detection, `cadence` field (OpenAPI, overview card tag and twin column, assistant `recurring_payments`); one candidate scan for all cadences (docs/perf.md "Phase 15"); `TestCadences`, `TestCadenceFields`, `TestWithCadencesMerges`, `TestOverviewCadences`; parity unchanged
- [ ] 15.9 group reports: group window (totals, span, spend per day, categories donut, top payees, CSV export), optional group budget with a progress bar
- [ ] 15.10 rule suggestions: a dismissible toolbar card after ≥ 3 manual categorisations of one payee group
- [ ] 15.8 assistant insight tools: `get_budget_status`, `get_group_summary` (+ the existing month overview and recurring tools); evals extended
- [ ] 15.7 assistant action proposals: `propose_action` (never executed by the server), confirmation card with the affected rows, *Apply* through the normal endpoints, structured log line; evals with proposal cases
- [ ] Tests: rule compile/apply (precedence, manual protection, order, re-apply after changes), transfer pairing edge cases, insights exclusion default, E2E filters → rule → table and overview; parity harness with transfers included — push 1 done: `internal/rules` (8: precedence/order/disable/delete, re-import, validation + tenancy, preview, detection edge cases incl. 3 accounts and 4-day near miss, manual decisions through re-import and orphaned halves, rule transfers, tenancy), ledger categories (4: rename/merge/delete across re-imports, tenancy), HTTP (`TestCategoriesHTTP`, `TestRulesHTTP`, `TestTransfersHTTP` with the real job), reclassify worker, guard tests (RLS seeds, schema list, version 8, delete-me); 307 Vitest (+10: settings pages, rule window + preview, Save filters as rule, badge/hide/chip, inspector unpair, helpers); e2e `rules.spec.ts` (2), parity harness now with `includeTransfers: true` (30/30); 51 Playwright specs on a fresh stack

## Phase 14 tasks (done)
- [x] 14.1 legacy migration: `00007_legacy_imports.sql` (runs + files, forced RLS), `internal/legacyimport` (legacy table and key replay, cfg → options and mapping, labels, overrides, budgets, opening, investments, chats, report), `POST`/`GET /legacy-imports` + `GET /legacy-imports/{id}`, idempotent `LegacyImport` job with progress; Settings → Data wizard (`deser`, passphrase + WebCrypto, accounts per layout or number hint, opening, chats, appearance settings, progress, report); ADR 0016, `docs/migration.md`, THREAT-MODEL I9/D6. Tests: key replay vs the oracle (every ingest and cfg table with roles, 5 txKeys sets incl. merged, 5 dedupe sets × overrides; 99 subtests), `TestMigratePlainAndLockedBackups` (Go-side decryption, 917 + 8 transactions, 8/8 labels, 2/2 decisions, budgets, opening, investments, chats, second run creates nothing), failed files, validation, one run per user, tenancy; HTTP (`TestLegacyMigrationOverHTTP`, `TestLegacyMigrationRequests`: 413/422/409/401); 11 Vitest (backup reader on both backups, wrong passphrase, manifest without the AI key, wizard with MSW, plan defaults); e2e `legacy.spec.ts` (2: plain backup → accounts, group filter, budgets, chats; locked backup, wrong passphrase refused with nothing sent, repeat run reuses all files); 297 Vitest, 39 Playwright
- [x] 14.2 E2E completion: `quality.spec.ts` (5: settings — theme, interface size, profile, sessions, data export zip; keyboard map — ⌘F, `/`, ⌘O, ⌘J, `?`, Esc; 720 px window — rows, sidebar toggle, popovers inside the viewport; UI scale at DPR 2 and 2.5; every fixture family imported and counted against the oracle, overlap A+B = the oracle's dedupe count); migration plain + locked in `legacy.spec.ts`; `screenshots.spec.ts` (6 screens × 2 themes); CI keeps `web/screenshots/` and the parity report as the `screenshots-and-parity` artifact of every run; Playwright `github` reporter in CI (failures as annotations)
- [x] 14.3 parity harness `web/e2e/parity.spec.ts`: the legacy app (served from `legacy/finuance.html` inside the test) and the new app on the same data, direct and through migration (Σ, summaries for all time / last 3 months / three months, categories, cash flow, recurring, overview months, balance; on the migration path also groups, duplicate decisions, budgets, investments) → `docs/parity-report.md`: 30 of 30 equal
- [x] 14.4 accessibility: `a11y.spec.ts` (axe WCAG 2.1 A/AA on sign-in, sign-up, empty, table + pane, import preview, overview, four settings pages, assistant, both themes; serious/critical fail): zero violations after the fixes (contrast tokens raised to 4.5:1 with `--accent-solid` for white button text; scrollable lists focusable and labelled); `docs/a11y.md` with the keyboard-only checklist
- [x] 14.5 performance (`docs/perf.md` "Phase 14"): initial JS 200.5 KB gzip (budget 250), overview 12.3 KB and assistant 7.8 KB chunks; API p95 at 108k rows re-measured (query, insights, overview)
- [x] 14.6 `docs/PARITY.md`: every row `ported` or `retired: reason` (45 open rows settled)
- [x] 14.7 release: `release.yml` on `v*.*.*` tags (buildx linux/amd64 + linux/arm64 without QEMU — both Dockerfiles build on `$BUILDPLATFORM` and cross-compile, arm64 checked locally; Trivy CRITICAL on both platforms; syft SPDX SBOMs as cosign attestations; keyless cosign signatures; smoke test of the pushed images with `compose.prod.yaml`; GitHub release with notes from `CHANGELOG.md`, rc tags as pre-releases), standalone `compose.prod.yaml` + `.env.prod.example` (bootstrap inline, kept equal by `make check-compose-prod`; checked locally: bootstrap, sign-up, CSP), `CHANGELOG.md`, README v0.1; `v0.1.0-rc.1` is tagged by our Product Engineer (the cloud session can't push tags or create refs); a release created by hand in the GitHub web UI, which also creates the tag, is filled in by the workflow instead of failing. **`v0.1.0-rc.1` released on 2026-10-23** from `92ccfd1`: every step passed (amd64 + arm64 images pushed, Trivy, SBOM attestations, cosign signatures, smoke test with `compose.prod.yaml`, pre-release with notes and files). The env file is published as `finuance.env.example` (GitHub drops a leading dot from asset names)

## Phase 13 tasks (done)
- [x] 13.1 connections (file + shorthand, private-address check, cloud flag), capabilities, `GET /assistant/status`, `GET /meta.assistantEnabled`
- [x] 13.2 `internal/llm`: canonical chat model; OpenAI Chat Completions and Anthropic Messages adapters (streaming, reasoning fields and `<think>`, tool calls assembled by index, Qwen text fallback, non-streaming JSON, thinking levels, error classes, timeouts); recorded-stream tests; `finuance admin llm-probe`
- [x] 13.3 tools: `search_transactions`, `aggregate_transactions` (+ `group_by: account`), `find_values` (+ `field: group`), `month_overview`, `recurring_payments`; differential tests against the oracle (67 cases)
- [x] 13.4 prompt builder (port `sysPrompt`, `PLAN()`, RULES); golden test against the oracle prompt with a frozen clock
- [x] 13.5 run loop (tools, plan, auto fallback, 6 rounds, loop guard, token budget, `/no_think`), storage (migration + RLS), retention setting + hourly cleanup job, max 25 chats
- [x] 13.6 endpoints: chats CRUD, settings, streaming messages (SSE, heartbeat, disconnect = cancel), stop, one run per user, per-replica cap, rate limit; nginx unbuffered; ADR 0015
- [x] 13.8 mock server (both protocols) + `cmd/llmmock` + Compose `ai-mock` profile; HTTP + tenancy tests
- [x] 13.10 metrics, spans, one log line per run (no prompt text)
- [x] 13.9 eval harness (≥ 40 questions, Polish and English), `make eval-assistant`, mock self-test in CI
- [x] 13.7 frontend: panel, chat menu, connection/model picker, settings (thinking, retention with warning), cloud consent and badge, steps with Show in table, `md()` port, ⌘/Ctrl J, resize/maximise, stop, error states
- [x] E2E against the mock (legacy §29.6): `assistant.spec.ts` (4: a tool call streamed in pieces is assembled and run, its result goes back, the markdown table renders, Show in table filters to the 10 rows, the chat survives a reload, no request to another origin, no page errors; plan-mode fallback; stop; not configured); 37 Playwright specs pass on a fresh `compose.ci.yaml` stack; 286 Vitest (+19: SSE reader, `md()`, panel streaming and Show in table, view scope, error fixes, 409, stop, recent chats, not set up, retention warning, cloud consent, model/thinking, store events, prefs migration, chip); screenshots in both themes at 1440 px; 12 PARITY rows `ported`; the panel is an 8.6 KB gzip chunk (initial JS 192 KB)
- [x] Backend tests: `internal/llm` on 11 recorded streams (LM Studio, Ollama, gpt-oss, `<think>`, Qwen text calls, CRLF, non-streaming, Anthropic thinking + tool use, overloaded) + errors, timeouts, request bodies, caps, splitter; config (file, shorthand, every problem at once); `TestToolsParity` (all 67 oracle cases, results and summaries), `TestSystemPromptParity` (ALLOWED_DIFFS rows 7 and 14), run loop against `testutil/llmmock` (tools, plan fallback, text calls, Anthropic, stop, errors, rounds, retry without temperature, `/no_think`, limits, consent, retention); HTTP + tenancy (status, settings, chats, SSE, stop, 409/429/403/503, export, delete-me); `llm-probe`; eval harness self-test
- [x] Docs: ADR 0015, `docs/assistant.md`, THREAT-MODEL (B4, A7, I5, I7, I8, D5), ALLOWED_DIFFS rows 7 and 14, `evals/assistant/README.md`, `docs/evals.md` (filled by the gate runs)

## Phase 12 tasks (done)
- [x] 12.1 overview backend: month aggregates in `query` (month totals, month × category, income by payee, weekday window, largest expenses, recurring candidates, balance series) + rules in `internal/insights/overview.go` (M/P, partial, prevToDate, projection, KPIs and sparklines, default budgets and status, savings rate, weekday, balance, `findRecurring`, highlights)
- [x] 12.2 budgets: `GET /budgets`, `PUT /budgets/{categoryId}` (`null` = default)
- [x] 12.3 opening balance input on the balance card (single-account views; sum read-only in "All accounts")
- [x] 12.6 investments: `GET`/`PUT /investments` (versioned), calculators in `internal/investments`, oracle portfolios
- [x] Legacy oracle: overview card twins + highlight sentences on the demo year (11 views); `TestOverviewParity` matches every card and sentence (2 weekday half-cent ties, ALLOWED_DIFFS row 13); `TestInvestmentCardsParity` (5 portfolios × 2 months)
- [x] 12.4 `OverviewPage`: header, KPIs, the twelve cards, 12-column container grid, chart window with table and `ovMonths`, investment editors
- [x] 12.5 actions: Sankey category → filter + table, largest expense → row, cash-flow/savings bar → month
- [x] Tests: Go unit (rules, highlights at every threshold, recurring, investments), parity (11 overview views, 5 portfolios), HTTP + tenancy (overview, budgets, investments), ledger (investments, clear budget); 267 Vitest (+22: every card with fixed data, page, month picker, budget and opening inputs, investment editor with debounced save and 409, actions); E2E `overview.spec.ts` (4: every card's window table and the highlights = legacy oracle, budget change survives a reload, Sankey → filtered table, bar → month and largest expense → located row); 33 Playwright specs pass
- [x] Performance (docs/perf.md "Overview"): service 216 ms p95 unfiltered at 108k rows (recurring query rewritten); browser month step 428 ms p50, drawing 41 ms; overview chunk 12 KB gzip loaded at idle
- [x] Screenshots in both themes at 1440 / 1000 / 600 px containers; 16 PARITY rows `ported`

## Phase 11 tasks (done)
- [x] 11.1 backend: aggregation queries in `query/insights.go` (summary, categories, cashflow, pace, payees, daily) + pure rules in `internal/insights` (days clipping, prev-period delta, savings rate, pace arrays and comparisons, category shares and colour slots, top-N payees, heatmap ranges)
- [x] 11.2 OpenAPI + handlers `POST /insights/{summary,categories,cashflow,pace,payees,daily}`, HTTP + tenancy tests
- [x] 11.3 `web/src/charts/lib` ports (`barPath`, `arc`, `spark`, compact numbers, `roundNice`, heat quintiles, `<Svg w h>`, tooltip) with geometry tests
- [x] 11.4 chart registry (`ChartDef.use` → subtitle, legend, drawing, table twin) and the six charts: Summary, Categories, Cash flow, Pace, Payees, Daily heatmap
- [x] 11.5 `InsightsPane` (resize 170 px … 75 vh with double-click reset, collapse, Charts menu with drag/↑↓ reorder, scroll-snap cards) + `PeriodPicker` (months/years of the data, presets, stepping, custom range)
- [x] 11.6 `ChartModal` (chart/table switch, range controls saved and applied to the cards, ←/→, Esc, backdrop, redraw on resize)
- [x] 11.7 `useChartActions` (category `=Name` / `=` for uncategorised, month → period, payee contains, day → custom period, largest expense → located row; legacy toggle semantics)
- [x] Legacy oracle: chart table twins on the demo file (16 cases); `TestChartParity` matches all six charts, ALLOWED_DIFFS row 13 (half-cent ties in averages)
- [x] Performance (docs/perf.md "Insights"): every insight call ≤ 122 ms p95 at 108k rows; filter-to-charts 113–204 ms in the browser; first paint 275 ms
- [x] Tests: Go unit (rules), integration (parity, slots, empty user, validation), HTTP + tenancy; 245 Vitest (+48: geometry, every chart with fixed data, pane, period picker, menu, window, actions); E2E `insights.spec.ts` (4: legacy numbers on the cards, category → chip and count, month → period, window table = API); 29 Playwright specs pass on a fresh stack; screenshots in both themes at 90/100/130 %; 13 PARITY rows `ported`

## Phase 10 tasks (done)
- [x] 10.1 `useQueryContext(viewId)` (debounced search 120 ms / conditions 160 ms, `ctxKey`)
- [x] 10.2 `Grid` (§7.7: 200-row windows, skeletons, measured row height, sticky header + gutter, widths × uiScale, resize, sort cycle, badges, empty row); `includeTotals: false` after the first window
- [x] 10.3 `hl()` port (`table/highlight.tsx`)
- [x] 10.4 `ColumnMenu`, `ColumnsMenu`, legacy `HELP`, value checklist
- [x] 10.5 `Toolbar` + `Chips`, Clear all, Export
- [x] 10.6 `Inspector` (fields, raw, sources, category combobox, note, Copy as JSON, prev/next, overlay < 1100 px)
- [x] 10.7 `StatusBar` (shown/total, columns, period, Σ/avg/min/max, totals picker, duplicates link)
- [x] 10.8 keyboard (selection across windows, Esc chain, double-click copy)
- [x] 10.9 per-view state for `/t/all` and `/t/:accountId`; Account column only in `all`
- [x] 10.10 groups backend: CRUD + stats, `POST /transactions/bulk` (ids), group in exports
- [x] 10.11 multi-selection + `QuickBar`
- [x] 10.12 groups UI (menu, form, inspector pills, Group column, rename follows filters)
- [x] 10.13 duplicates: `GET /duplicates`, `PUT`/`DELETE /duplicates/{accountId}/{fingerprint}`, `DuplicatesDialog`
- [x] 10.14 performance with a 100k-row user (`tools/perf/grid.mjs`, Playwright trace, `docs/perf.md`): filter-to-paint 88–175 ms; scrolling frames match a plain HTML table in the sandbox (software rendering), 60 fps to be checked on real hardware at the gate
- [x] Tests: 196 Vitest (stores, chips, Grid/ColumnMenu/inspector/status/groups/selection/duplicates with MSW); E2E `table.spec.ts` (8, legacy §26 + oracle counts), `groups.spec.ts` (3), `duplicates.spec.ts` (1); Go HTTP tests for groups/bulk and duplicates; 30 PARITY rows `ported`

## Phase 9 tasks (done)
- [x] 9.0 header detection fix (`ingest.findHeader`): mBank → record 26, PKO BP → 1; also fixes `06-bank-preamble` and `21-blank-headers`. `ALLOWED_DIFFS` row 4 with a strict oracle check; every other fixture keeps the legacy header
- [x] 9.1 entry points: drag & drop, mini drop zone, ⌘/Ctrl O, Browse → `import/queue.ts` (non-CSV files refused with a toast)
- [x] 9.2 upload queue + "file 1 of N" in the window title, `409 duplicate_file` toast "… was already imported on <date>" with *Open*
- [x] 9.3 preview window `/imports/:id`: settings bar with account picker (suggested / existing / create), column meanings, options, stats, sample table with an "Imported as" column, rows-with-errors list, footer; 300 ms debounced `PATCH` (`import/editor.ts`)
- [x] 9.4 commit flow: poll every 500 ms, toast "Imported N transactions · M duplicates skipped", open the account; inline failure + *Try again* (a failed import can be committed again)
- [x] 9.5 sidebar import history folded out per account (status, `+new · dup`, source label) + menu (Download original, Re-map and reprocess, Delete with confirm); previews without an account under "Not imported yet"
- [x] 9.6 legacy v7 window parts: raw lines + header picking (scrolled to the header), footer/date-order, role menu with *auto* hints + *Use detected meanings* (`ImportPreview.detected`), first 50 + ⋯ + last 5 rows, required-fields footer
- [x] 9.7 fast path setting (settings popover → Import, saved in prefs), toast actions (*Review*, *Undo*), source labels
- [x] Tests: 152 Vitest (editor with fake timers, roles, queue, window parts with MSW, sidebar history, toasts); E2E `import.spec.ts` (8 cases: overlap A→B with the oracle's counts (16, 4 duplicates), delete A, duplicate file, Windows-1250, debit/credit signs via the API, headerless + skip rows, padded preamble, footer summary, month-first dates, fast path + Undo) — 13 specs pass against a fresh `compose.ci.yaml` stack

## Phase 8 tasks (done)
- [x] 8.1 scaffold `web/` (Vite, React, TS strict, ESLint, Prettier, Vitest, Playwright config, `/api` proxy, `make dev-web`)
- [x] 8.2 API client: `openapi-typescript` in `make gen`, `openapi-fetch` client (CSRF header, Problem errors, 401 → `/login?next=`), hooks
- [x] 8.3 styles: `tokens.css` (verbatim), `base.css` (rem), `scale.ts` (auto heuristic), `theme.ts`
- [x] 8.4 `Icon` (legacy SVG set), `Logo`, favicon
- [x] 8.5 UI primitives (Popover, Menu, Modal, Toast queue, Tooltip, Button, IconButton, inputs, Kbd) + dev-only `/__ui` (not in production builds)
- [x] 8.6 shell: TitleBar, Sidebar (accounts, auto-collapse), StatusBar, EmptyState, DropOverlay, keyboard manager
- [x] 8.7 auth pages, user menu, `RequireAuth` with the prefs gate
- [x] 8.8 `uiStore`, `viewStore`, preferences sync (hydrate, 500 ms debounce, 409: server wins + local changes re-applied, one retry)
- [x] 8.9 settings pages: appearance, profile, sessions, data
- [x] 8.10 `POST /demo-data` + "Load demo data"
- [x] 8.11 web image (unprivileged nginx 1.30.6, CSP + security headers, caching, gzip, `/api` proxy without buffering), Compose `web` on :8000, `compose.prod.yaml` (only web published), `make web-dist`
- [x] 8.12 CI `web` job (lint, typecheck, test, build, bundle size summary + 250 KB gzip budget); image job also builds, smoke-tests and scans the web image
- [x] 8.13 E2E: `compose.ci.yaml`, `make test-e2e`, CI `e2e` job; 5 specs (signup → demo data → sidebar; prefs survive a reload; sign out/in with `next`; `?` window; no CSP violations), passing against a fresh stack
- [x] 8.14 SettingsPopover, KeyboardShortcuts dialog
- [x] Gate: legacy vs new screenshots (dark/light), reviewed by our Product Engineer
- [x] Tests: 118 Vitest (stores, sync with fake timers, client, primitives, shell, auth, settings, keyboard), 5 Playwright

## Phase 7 tasks (done)
- [x] 7.1 `query/dsl`: condition lexer + parser → AST, round-trip printer, `FuzzParseCond`
- [x] 7.2 `query/columns.go` + `compile.go`: column registry (§4.5 whitelist), `Compile` → `{where, args}` (number/date/text semantics, `excluded`, search, accounts)
- [x] 7.3 `query/period.go`: anchor, presets, `prevRange`, labels
- [x] 7.4 `query/sql.go`: `base`/`scoped` builder, sorting (`pl_natural`, empties last, tie-break); store guard allowlist
- [x] 7.5 `POST /transactions/query` (window, total, stats, duplicatesSkipped, dataRange, period, `locateId`)
- [x] 7.6 `POST /transactions/distinct`
- [x] 7.7 `GET`/`PATCH /transactions/{id}` (sources; category upsert, manual source, `resetCategory`, note, annotations)
- [x] 7.8 `GET /categories?accountIds=` (`txCount`, `colorSlot`)
- [x] 7.9 `POST /transactions/export` (streamed, BOM, delimiter, injection guard); ADR 0014
- [x] 7.10 `GET /me/export` (zip: manifest, JSON per entity, preferences, transactions.csv, original files)
- [x] 7.11 performance: 108k-row seed, `EXPLAIN (ANALYZE, BUFFERS)`, migration `00004_query_indexes.sql`, `docs/perf.md` + `docs/perf-phase7-plans.md`; after the gate decisions 6 of 7 first-window shapes ≤ 150 ms p95 in the sandbox, scrolling windows 12–48 ms (payee contains and collation sort above; see perf.md)
- [x] 7.12 `group` column, `toolFilter` (port of `aiFilter`, oracle parity on every search/aggregate case), `POST /transactions/ids`
- [x] Tests: filter parity (84 oracle cases, 1 allowed diff), injection, sort, paging + `locateId`, stats, distinct, export, zip, tenancy; nightly workflow with `FuzzQueryDB`
- [x] `scripts/query-smoke.sh` (17 steps pass against `make up`); THREAT-MODEL T2/T4, ARCHITECTURE query section, ALLOWED_DIFFS row 12

## Phase 6 tasks (done)
- [x] 6.1 Accounts CRUD (+ txCount, firstOn, lastOn)
- [x] 6.2 `POST /imports`: multipart, NUL sniff, SHA-256 / 409, parse (+ profile), roles, account suggestion (pre-filled), stage, duplicate estimate, preview
- [x] 6.3 `GET /imports`, `/imports/{id}`, `/rows` (onlyErrors, duplicate flag), `/file`
- [x] 6.4 `PATCH /imports/{id}` (re-parse, re-stage, validation)
- [x] 6.5 `POST /imports/{id}/commit` → 202, `InsertTx`
- [x] 6.6 `CommitImport` job (categories, commit SQL, overrides, stats, profile, retries → failed, metrics); worker in Compose
- [x] 6.7 `DELETE /imports/{id}` (orphans + re-materialise)
- [x] 6.8 `POST /imports/{id}/reprocess`
- [x] 6.9 `finuance seed-demo --email` (+ `make seed-demo email=…`)
- [x] 6.10 v7 import settings end to end; `GET /imports/{id}/lines`; `headerAt`, `dropped`, `tailRows`
- [x] 6.11 fast path `autoCommit=true`
- [x] 6.12 source labels (`profileLabel`)
- [x] 6.13 duplicate review backend: `Rematerialize`, overrides, `RematerializeFingerprint` job, `duplicatesSkipped`, property test
- [x] Tests: overlap vs oracle (totals over HTTP, row multiset in the ledger), same-name, delete semantics, concurrent commits, reprocess keeps edits, profiles, errors, 409, job retry
- [x] `scripts/import-smoke.sh` against `make up` (18 steps pass: 17 transactions after A+B+C, 16 after deleting C, as the oracle; worker metrics visible); ADR 0013 (per-account writer lock)

## Phase 5 tasks (done)
- [x] 5.1 `auth/password.go`: argon2id PHC, `Verify`, `NeedsRehash`; benchmark + ADR 0010
- [x] 5.2 `auth/sessions.go`: create, resolve (absolute + idle expiry, last-seen throttle), revoke, revoke others, rotate
- [x] 5.3 middleware: `RequireSession`, `CSRF`, `RateLimit`, cookie helper; ADRs 0011 (sessions/CSRF), 0012 (rate limiting)
- [x] 5.4 OpenAPI + handlers: signup, login, logout, me, password, sessions, preferences
- [x] 5.5 `DELETE /me`; `GET /me/export` stub (501 until Phase 7)
- [x] 5.6 River: job registry with deps, hourly `CleanupExpiredSessions`, insert-only client in `serve`
- [x] 5.7 `finuance admin create-user` / `set-password` (`--password-stdin`)
- [x] 5.8 `docs/THREAT-MODEL.md` v1
- [x] 5.9 `scripts/auth-smoke.sh` against `make up` (15 steps pass; admin CLI also checked inside the container)
- [x] Tests: HTTP flows, cookies dev/prod, CSRF, 429 + Retry-After, expiry (fake clock), rotation, stale prefs, delete-me counts every table, log redaction

## Phase 4 tasks (done)
- [x] 4.1 migrations `00002_core.sql` (§5.2 + collation) and `00003_rls.sql` (§5.3, forced), with Down sections
- [x] 4.2 `store.Store`: `WithUser`, `WithUserReadOnly`; guard test against pool access outside `store`
- [x] 4.3 sqlc queries (users, sessions, preferences, accounts, categories, groups, imports, import_files, import_rows, transactions, budgets, profiles, annotations, overrides, investments)
- [x] 4.4 `InsertImportRows` via `unnest`, 5,000 rows per statement; 100k-row benchmark: 8.0–9.7 s in the sandbox, above the 3 s target; **accepted by our Product Engineer at the gate** (see perf.md)
- [x] 4.5 `ledger` repositories returning domain types
- [x] 4.6 tests: migrate up/down/up, every query, RLS suite, collation sort
- [x] 4.7 `docs/ARCHITECTURE.md` (components, ERD, RLS)
- [x] 4.8 groups + annotations (`ledger.Annotate`), commit core (steps 1, 1b, 2) + orphan cleanup; annotate → delete → re-import test
- [x] 4.9 overrides + investments schema, `split_scope`/`source_import_id`, RLS tests

## Phase 3 tasks (done)
- [x] 3.1 `ingest/normalize.go`
- [x] 3.2 `ingest/decode.go`
- [x] 3.3 `ingest/delimiter.go`
- [x] 3.4 `ingest/csv.go` (rune state machine + byte fast path for ASCII delimiters, fuzzed against each other)
- [x] 3.5 `ingest/table.go`
- [x] 3.6 `ingest/values.go` + `money/parse.go`
- [x] 3.7 `ingest/columns.go`
- [x] 3.8 `ingest/stage.go`
- [x] 3.9 `ingest/fingerprint.go`
- [x] 3.10 `ingest/parse.go`
- [x] 3.11 oracle differential suite (62 ingest fixtures, 5 corpora, 7 dedupe sets)
- [x] 3.12 fuzz targets (5 min each, no crash: Tokenize 2.9 M, ParseNumber 8.7 M, ParseMinor 9.7 M, ParseDate 10.3 M, Parse 0.97 M execs)
- [x] 3.13 golden files (61, `-update` to regenerate)
- [x] 3.14 benchmark + `docs/perf.md` (+ `internal/demo` port): median ≈ 563 ms on the 4-vCPU sandbox, above the 500 ms target; **accepted by our Product Engineer at the gate**, see perf.md
- [x] 3.15 v7 import-settings parity (27 recorded cfg variants)
- [x] CI: coverage gate (`make coverage`, ingest + money 96.7 %), no-float check (`make check-no-float`), fuzz smoke (`make fuzz FUZZTIME=10s`), benchmark in the job summary

## Phase 2 tasks (done)
- [x] 2.1 `cmd/finuance` subcommands + build info
- [x] 2.2 `internal/config`
- [x] 2.3 `internal/observability` (slog, Prometheus, OTel, admin server)
- [x] 2.4 `internal/httpapi` server, middleware chain, health, graceful shutdown
- [x] 2.5 `httpapi/problems.go`
- [x] 2.6 `api/openapi.yaml` v0 + oapi-codegen + validator + `make gen`/`gen-check`
- [x] 2.7 `internal/store` pool + goose + River migrations
- [x] 2.8 sqlc + `platform.Date`
- [x] 2.9 `testutil` testcontainers harness; `make test-integration`
- [x] 2.10 `Dockerfile.backend`
- [x] 2.11 Compose `migrate` + `api`; `make up/down/logs`, `dev-api`, `dev-worker`
- [x] 2.12 CI `backend` job

## Phase 1 tasks (done)
- [x] 1.1 `docs/PARITY.md` (≈ 140 rows; MVP failures and §27 limitations with decisions)
- [x] 1.2 58 synthetic fixtures (48 edge cases + Pekao/PKO BP/mBank look-alikes + overlap, same-name, duplicate-versions sets), generated by `make fixtures`
- [x] 1.3 Anonymiser `backend/cmd/anonymize` with tests; the three private samples anonymised, reviewed by our Product Engineer and committed to `testdata/fixtures/real/`
- [x] 1.4 `tools/legacy-oracle` and `make oracle`: deterministic (two runs, zero diff)
- [x] 1.5 `testdata/oracle/ALLOWED_DIFFS.md`
- [x] Makefile + CI: `make test` runs Go vet + tests, `make lint` checks gofmt, `fixtures-check` in `make check`; CI jobs `backend` and `oracle-fresh`

## Completed phases
- Phase 14 — Legacy migration and quality gate: gate approved 2026-10-23 with the hands-on checks deferred (see Follow-ups); `v0.1.0-rc.1` released; `phase/14-quality-gate` merged with `--no-ff`; `v0.1.0` is published from `main` by our Product Engineer.
- Phase 13 — AI assistant: gate approved 2026-10-23 with the real-model checks deferred (see Follow-ups); `phase/13-assistant` merged with `--no-ff`.
- Phase 12 — Overview: API and UI: gate approved 2026-10-23; `phase/12-overview` merged with `--no-ff`.
- Phase 11 — Insights: API, pane, modal: gate approved 2026-10-23; `phase/11-insights` merged with `--no-ff`.
- Phase 10 — Table view, groups and multi-selection: gate approved 2026-10-22; `phase/10-table` merged with `--no-ff`.
- Phase 9 — Import UX: gate approved 2026-10-22; `phase/09-import` merged with `--no-ff`.
- Phase 8 — Frontend foundation: gate approved 2026-10-22; `phase/08-frontend` merged with `--no-ff`.
- Phase 7 — Transactions query API: gate approved 2026-10-21 (option a: `textlike` LEAKPROOF; `includeTotals` added); `phase/07-query` merged with `--no-ff`.
- Phase 6 — Accounts and import pipeline: gate approved 2026-10-21 (import smoke matches the oracle; Pekao preview reviewed); `phase/06-imports` merged with `--no-ff`.
- Phase 5 — Auth, users, preferences: gate approved 2026-10-21; `phase/05-auth` merged with `--no-ff`.
- Phase 4 — Schema, store layer, RLS: gate approved 2026-10-20 (staging benchmark 8.0–9.7 s accepted against the 3 s target); `phase/04-schema-store-rls` merged with `--no-ff`.
- Phase 3 — Ingest engine: gate approved 2026-10-20 (benchmark ≈ 563 ms accepted against the 500 ms target); `phase/03-ingest-engine` merged with `--no-ff`.
- Phase 2 — Backend skeleton: gate approved 2026-10-20 (image size target amended); `phase/02-backend-skeleton` merged with `--no-ff`.
- Phase 1 — Legacy extraction: gate approved 2026-10-20 (anonymised samples reviewed); `phase/01-legacy-extraction` merged with `--no-ff`.
- Phase 0 — Repository bootstrap: gate approved 2026-10-20; `main` rewritten to the scrubbed root commit `0b05487`, then `phase/00-bootstrap` merged with `--no-ff`.

## Phase 0 tasks (done)
- [x] 0.1 Open questions asked; answers recorded here and in ADR 0001
- [x] 0.2 Repo layout (§3.3); legacy files in `legacy/`, `SAMPLE` scrubbed; `tools/check-private.sh`
- [x] 0.3 Root files: README stub, LICENSE (MIT), `.editorconfig`, `.gitignore`, `.gitattributes`
- [x] 0.4 Agent docs: `CLAUDE.md`, `backend/CLAUDE.md`, `web/CLAUDE.md`, `docs/PLAN.md`, this file, ADRs 0000–0009
- [x] 0.5 Claude Code commands: `.claude/commands/phase-start.md`, `phase-close.md`
- [x] 0.6 `Makefile` with every Appendix F target (stubs print `not yet implemented (Phase N)`)
- [x] 0.7 Local DB: `deploy/compose/compose.yaml`, `deploy/db/bootstrap.sql` + init wrapper, `.env.example`, `db-*` targets
- [x] 0.8 CI skeleton: `.github/workflows/ci.yml` (guardrails + placeholder jobs), `renovate.json`, PR template

## Deviations from the plan (with reason)
- 15.4: weekly = at least 6 charges, 75 % of the intervals 4–10 days with a median of 5–9, 75 % of the amounts within ±30 % of the median, active within 14 days, next = last + 7 days, yearly cost 52 × the amount; yearly = at least 2 charges, 75 % of the intervals 330–400 days, the same amount rule, active within 400 days, next = last + 12 months. Only payee groups the monthly rules didn't take are tried. The recurring highlight and the card's "≈ …/mo" now sum the yearly costs / 12 (the same as before for monthly payments). The card's table twin gets a *Cadence* column only when a weekly or yearly payment is listed, so a monthly-only list keeps the legacy columns.
- 15.1: categories get **aliases** (`category_aliases`, new table, and the `bank_category_map` view every bank-name lookup goes through): without them a rename, merge or delete was undone by the next import of the same bank category (ADR 0017). A typed category name that was merged resolves to the merge target; the name of a deleted category creates it again. Merging moves the budget only when the target has none.
- 15.2/15.3: one job, `reclassify`, instead of `ApplyRules` + `DetectTransfers` (rules can mark transfers, and detection must see their result), always over all of the user's transactions (no `import:<id>` / `uncategorised` scopes: a from-scratch run is a few set-based statements). It takes every account's writer lock (ADR 0013). The preview is `POST /rules/preview {match}` (also for unsaved rules) instead of `POST /rules/{id}/preview`; rule updates are `PUT` (full replace, position kept). A match needs a search or a filter. `transactions.transfer_source` (new column) records why a row is a transfer, so everything automatic can be recomputed. After a rule or category change the web app refetches the affected views at once and after 1.2 s and 4 s (the job's usual run time).
- 14.5 (found by the E2E on a fresh database): the table's window statement could run its `ORDER BY … LIMIT` subquery once per row when the statistics predated the user (2.5–5 s for a 917-row account until autoanalyze). The window is now a `MATERIALIZED` CTE (`query.windowFirst`, also the assistant's row lookup); `TestWindowWithStaleStatistics` recreates the stale statistics. docs/perf.md "Phase 14".
- 14.4: the legacy palette fails WCAG AA contrast in places (muted text, accent links, white text on the accent button, green in the light theme). The tokens were raised just enough for 4.5:1 (each change commented with its legacy value in `tokens.css`) and a new `--accent-solid` sits behind white button text; the look is otherwise unchanged. Scrollable chart and overview lists are focusable (`tabindex="0"`) and labelled "…, list".
- 14.2: the screenshot artifact also carries `docs/parity-report.md`; the Playwright `github` reporter turns E2E failures into annotations (job logs and artifacts can't be read from the cloud session).
- 14.7: cosign and syft are installed in `release.yml` with `go install …@version` (the Go checksum database verifies them) rather than downloaded release binaries, because GitHub release downloads can't be reached from the cloud session to record checksums. `compose.prod.yaml` has no plain-HTTP cookie switch: production cookies are always `Secure`; trying it locally works on `http://localhost` (browsers treat it as secure), otherwise behind TLS or an SSH tunnel. `make check` runs `tools/check-compose-prod.sh`, which keeps the inline bootstrap equal to `deploy/db/`.
- 14.1: labels and duplicate decisions are matched by **replaying the legacy keys** on each imported file (legacy header rule, `applyMapping` port, legacy typing and roles, per-file and merged-view `txKeys`), not by translating each key into a fingerprint (ADR 0016); the oracle checks the replay. A file whose header the legacy app found elsewhere can't have its labels matched (reported per file). New: `ingest.ShapeOptions.LegacyHeader`, `ledger.Row.Fingerprint/Occurrence`, `assistant.Chats.Limit`.
- 14.1: the uploaded files wait in `legacy_import_files` (not in the plan's schema) until the run ends; the manifest is cleared then too, so copied chats don't outlive the retention. Request limits: each file ≤ the upload limit, all files ≤ 4×, manifest ≤ 16 MiB. New problem code `migration_running` (409). A "new" account whose name exists is that account (idempotent re-runs); investments are written only when the user has none saved; a chat whose title exists is skipped; chats fill only the free room under the chat limit (oldest legacy chats skipped first).
- 14.1: the legacy appearance settings (theme, size, pane, cards, chart cfg, sidebar, assistant panel; never the AI address or key, nor per-file views) are applied by the browser through the preferences sync when the user ticks the box, not by the job (the server keeps preferences opaque). The wizard shows each file's date range and account-number hints from a text scan in the browser (no server preview), proposes one new account per file layout, and a `md` Modal size was added.
- 13.1: `GET /assistant/status` answers every connection (`{enabled, connections[{name, label, api, cloud, reachable, defaultModel, models[{id, tools, reasoning, contextTokens}], error}], maxRowsLimit}`) instead of one server's model list. A failed check is cached 5 s, a success 60 s; `?refresh=true` checks again. New settings beyond §3.5: `FINUANCE_AI_CONNECTIONS_FILE`, `FINUANCE_AI_API`, `FINUANCE_AI_CONTEXT_TOKENS` (8192), `FINUANCE_AI_RATE_PER_HOUR`.
- 13.3: results keep the legacy key names (`total_spent`, `total_income`, `title`, `kind`), which the oracle compares, rather than §4.8's sketch (`spent`, `description`); a row names its account only when the scope has several. A filter label names the group filter (ALLOWED_DIFFS row 7).
- 13.4: the prompt lists the canonical columns with a value (not the file's), says "self-hosted", and adds two rules after the legacy ones (ALLOWED_DIFFS row 14). "Today" uses the browser's time zone sent with each question (`timeZone`, default Europe/Warsaw), as the legacy used the browser's clock; there is no server-side time zone setting.
- 13.5: messages are stored one row per model message (§5.2 layout) with `replay` (Anthropic thinking blocks) and `error` columns, plus `assistant_settings` (retention, cloud consent). A turn that failed is stored with its error and left out of later requests (legacy dropped it). Auto mode's switch to plan mode is remembered only when plan mode then works: a server that answers 500 to everything doesn't switch the chat for good (the legacy did). A server that rejects the temperature or the reasoning setting is asked again without it, once.
- 13.6: the run settings travel with each question (`settings: {connection, model, mode, think, maxRows, temperature, toolset}`); the panel keeps them in the preferences' `ai` key as the legacy did. Stopping applies to the user's run whatever chat id is in the path (one run per user). New problem codes: `consent_required` (403), `run_in_progress` (409), `assistant_busy` (503), `assistant_unavailable` (503).
- 13.7: the panel's settings pick a connection and a model (the address is server config); thinking has five levels instead of the legacy checkbox (a stored `think: true` reads as *auto*, `false` as *off*); *Keep chats* is the server retention (1–7 days or *until I delete them*, with a warning) instead of the legacy on/off; temperature stays at the legacy 0.2 without a control (the legacy had none). Choosing a cloud connection, or a `403 consent_required`, opens a consent window; a cloud connection shows a *cloud* badge next to the model. *Show in table* sets a session-only `toolFilter` on the view (the "✦" chip, cleared by *Clear all*) in the view the lookup ran in. The panel is its own chunk, loaded when it first opens.
- 13.8: the mock server also speaks Anthropic Messages and has a rule-based answer mode for E2E and local work, besides scripted replies for Go tests. Its image is a separate Dockerfile target (`llmmock`), never in the production image.
- 12.1: `POST /overview` answers `{currency, data?}` (no `data` without dated rows) with every card's facts in one document (`OverviewData`), not the §4.6 field list one by one. Highlights are facts (`kind`, `tone`, amounts, percentages); the browser writes the sentences, so the wording lives next to the other UI text. The income sources reuse `PayeeSpend` (`spentMinor` is the money received). A `month` outside the data is clamped to the latest month (legacy).
- 12.2: `PUT /budgets/{categoryId}` answers `204`; `null` removes the budget and is idempotent, but a category the user doesn't own is `404` either way.
- 12.4: each overview card is a `ChartDef` whose `use()` reads the one cached `POST /overview` document, so `ChartModal` and the table twins work unchanged (it takes the overview's lookup as a prop). The page is a separate chunk loaded 1.5 s after start and rendered without Suspense (React holds resolved boundaries back ~300 ms); the request starts from the route's eager entry. The month selector lists every month between the first and last month with data.
- 12.4: removing an investment position happens at once with *Undo* in a toast (the legacy asked with `confirm()`); edits save 400 ms after the last change, one request at a time; a `409` shows the stored document with a toast. Names can't be emptied (the old one comes back).
- 12.5: a largest expense outside the list's period switches the list to all time (legacy `gotoRow`); the table scrolls to the row once its first window has arrived (`scrollToRow` waits for rows). The uncategorised Sankey band filters with `=` (as 11.4).
- 12.6: position ids are client-generated uuids (stable keys while editing). They are primary keys, so an id another user already has is `409 conflict` (nothing is revealed about the other document).
- 11.4: a `ChartDef` is `{key, title, cfgControls, use(q, env)}`; `use` reads the data and returns the card's parts (subtitle, legend, drawing, table twin). The plan's sketch split these into `useData`, `Component`, `Subtitle`, `Legend` and `toTable`; one hook keeps them together because the cash-flow subtitle and table depend on the measured width (the months that fit).
- 11.4: the "(uncategorized)" slice filters with `=` (an empty category) instead of the legacy `=(uncategorized)`, which matched nothing (planned at the Phase 11 plan). The largest expense on the summary card is a button that selects and scrolls to its row (PLAN §7.8; the legacy tile was plain text).
- 11.4: insight amounts are in the most common currency of the view's accounts; a mixed-currency view adds minor units as they are (like the status bar, 10.7). The API's payee `top` accepts 0/10/25/50/100; another stored value reads as 25.
- 11.5: no *Fields* button: roles are fixed by each import's mapping since Phase 6 (PARITY "Fields menu retired"). The period picker lists every month and year between the view's first and last booking dates (from the accounts), including months without rows; the legacy listed only months with rows.
- 11.5: the charts load once the list's first window has arrived (`ChartEnv.ready`): six parallel insight requests otherwise queued the list's own request behind them (HTTP/1.1 allows six per host) and first paint took 1.9 s instead of 0.3 s. Later filter changes send everything at once.
- 11.6: ←/→ step through the cards unless a select or input has focus (it would change its value); the Charts menu's arrow buttons are named per card ("Move Summary up") for screen readers.
- E2E: `groups.spec.ts` hides the insights pane in its setup: in the 1280×720 test window the pane halves the grid, and its drags then reach the auto-scroll zone at the bottom edge.
- 10.14 (found by the E2E): deleting a 917-row import took ~27 s because `transactions.transfer_peer_id` (self-reference, `ON DELETE SET NULL`) had no index. Migration `00005_fingerprint_index.sql` adds it plus `import_rows (user_id, fingerprint)`; delete now re-materialises only fingerprints other imports still contain; a store test requires an index for every foreign key (or a listed reason). docs/perf.md "Deleting an import".
- 10.13: `PUT` and `DELETE /duplicates/{accountId}/{fingerprint}` rebuild the fingerprint's transactions in the request (`ledger.SetOverride`/`ClearOverride` already did this in one transaction) and answer `200` with the group as it is now, instead of `202` and a `RematerializeFingerprint` job: one fingerprint is a handful of rows, and the window needs no polling. The job stays for later bulk use. A decision on a key the account has no versions of is `404` (no stray overrides). The window shows the canonical fields (Date, Amount, Payee, Description + every field that differs) with the app's column names, not the bank's headers; versions are listed per import. *Reset choice* undoes *Use this version*. With nothing skipped, the status bar keeps a "duplicates reviewed" link while decided groups exist (legacy), asked only for views with two or more imports.
- 10.11: `POST /transactions/ids` also returns each row's `groupId`, so a range selected across unloaded windows knows whether *Ungroup* applies. A range is capped at 5,000 rows (the bulk limit) with a toast. The selection lives in `stores/selectionStore` per view and is cleared when the result changes, on Esc (before the row selection), on a plain click and when the view closes.
- 10.12: group delete asks in a window (the legacy used `confirm()`); after editing a group from the menu the menu opens again (legacy). The Group column is visible by default once a group exists.
- 10.10: `POST /transactions/bulk` takes `set: {groupId}` with `groupId` required and nullable (`null` = ungroup), so the generated types can tell "ungroup" from "not sent"; category and transfer join in Phase 15. `PATCH /groups/{id}` keeps the colour when none is sent. Group stats count money out and in over all accounts with transfers included (legacy `groupStats`), in the most common currency of the group's transactions.
- 10.2: grid amounts are formatted with `fmtMoney` in the browser locale (the legacy showed the CSV text as is), because amounts are stored as minor units now. Dates use `dd.mm.yyyy`.
- 10.5/10.9: each view starts with the plan's default hidden columns (Account only in `all`); the "N hidden" chip counts only columns hidden beyond those defaults, and removing it restores them.
- 10.6: the inspector lives in `table/` next to the grid it reads from (the plan lists `shell/Inspector`). Its "N / total" position shows only while the row is in a loaded window; Balance after and Group appear only when the transaction has them. The category picker is a combobox: pick, create by name, or "Use the bank's category" (`resetCategory`) for a category set by hand.
- 10.7: totals are shown in the currency of the view's first row; the server sums minor units, so a mixed-currency "all" view adds them as they are (as the legacy summed numbers). A stored *Balance after* totals column falls back to Amount when the view has no balances (legacy).
- 9.2: an upload goes into the account in view (`/t/:accountId`) when there is one. Files without an own-account number (the overlap fixtures, most CSVs made by hand) could otherwise never use the fast path, which needs a resolved account; the server still pre-fills one from the file's account number on `/t/all`.
- 9.3: opening `/imports/:id` for a previewed import re-reads it with `PATCH {}` (the API has no read-only full preview); after an upload the window starts from the upload's answer. `ImportPreview.detected` was added for the role menu's *auto* hints. A committed import opens with its stored rows; changes apply with *Reprocess*.
- 9.4: `POST /imports/{id}/commit` accepts a failed import again (*Try again*).
- 9.6: the raw-lines panel loads up to 4 records past the header (at least the legacy 18, at most 200) and scrolls to it, so mBank's header on record 26 is visible. The window body scrolls on short screens instead of squeezing the preview table, because the account picker and column meanings make it taller than the legacy window.
- CI: gitleaks flagged one base64 chunk of the spec that oapi-codegen embeds in `httpapi/gen/gen.go` (a false positive after the `detected` change). `.gitleaks.toml` keeps the default rules and allowlists only that generated file; `api/openapi.yaml` is still scanned.
- 9.7: when a toast is waiting, the one on screen is cut to 1.2 s (the legacy replaced it at once); toasts with actions otherwise stay 8 s.
- 8.1: TypeScript 5.9.3 rather than the newest release: openapi-typescript 7.13 declares `typescript ^5` as a peer. The Icon parity test reads `legacy/finuance.html` with a Vite `?raw` import; `server.fs.allow: ['..']` is set only in test mode.
- 8.8: the preferences document keeps the legacy `collectPrefs` key names (`theme`, `ui`, `vizH`, `noViz`, `sbOpen`, `cards`, `cardOrder`, `cfg`, `ai`, `views`), plus `importFastPath`, so a legacy backup's prefs map onto it directly in Phase 14. Every stored document goes through `parsePrefs`, which drops unknown values; keys this build doesn't know are kept on save. The sync is a plain class (`stores/prefsSync.ts`) started by `RequireAuth`, not a React hook, so it can be unit-tested with fake timers.
- 8.6: until the import UX (Phase 9), ⌘O, Browse, the mini drop zone and drag & drop open the picker or the overlay and then show a toast that import isn't available yet. The assistant button toggles its (persisted) state; the panel is Phase 13. Table and overview routes show the toolbar and a placeholder.
- 8.6: the title bar has an account button (initial of the name) that opens Profile, Sessions and Sign out; the legacy app had no user menu. Its styles, the sign-in card, the settings pages and the gallery live in `styles/finuance.css`, built from the legacy tokens.
- 8.11: like `backend/vendor`, a host-built `web/dist` in the build context is used as is, so the image builds without network access; `make up` and `make test-e2e` rebuild it first (`make web-dist`), and CI image builds have no `dist/` and build from source. nginx runs read-only with tmpfs for its rendered config and temp files. The API trusts `X-Forwarded-For` from `172.16.0.0/12` (the Compose network; `FINUANCE_TRUSTED_PROXIES`) so rate limits see the client address behind nginx.
- 8.11: initial JS is 144 KB gzip (budget 250 KB).
- 8.10: `ledger.SeedDemo` returns a `DemoSeed` (account, import id, commit result) so `POST /demo-data` can answer with the ids and the transaction count.
- 7.2: SQL fragments live in `query/columns.go` (the registry) and `query/compile.go` as well as `query/sql.go`; the whole `internal/query` package is the SQL builder, and the store guard allows that package. The registry also gives each column a display text (`-79,99`, `dd.mm.yyyy`), used by the text fallback, the value checklist, `excluded` and exports (our Product Engineer's decision).
- 7.2: number conditions compare exact `numeric` values in major units (scaled per row by the currency's exponent), not `amount_minor` against a converted operand, so a mixed-currency view stays correct.
- 7.2/7.7: text with a NUL character (JSON allows `\u0000`) is `422`. PostgreSQL rejects NUL in text, so before this fix a NUL character reached the database and gave a `500`. `FuzzQueryDB` found it. The ledger maps SQLSTATE 22021 to `ErrInvalid`, and display names are checked too.
- 7.4: each statement compiles its own argument list (PostgreSQL/pgx reject unused arguments). Windows use a late row lookup: ids first, then the display columns.
- 7.5: `duplicatesSkipped` is computed as "linked rows − distinct linked transactions" (equal to the §4.4 definition; `TestDuplicatesSkippedFormula`) so an index answers it.
- 7.6: the value checklist is computed over the period-scoped rows of the context minus the column's own filter, ordered by count (plan); the legacy listed every value of the dataset in value order.
- 7.7: `PATCH /transactions/{id}` has `resetCategory: true` (back to the bank's category), because the generated types can't tell an absent `categoryId` from `null`.
- 7.8: the colour ranking includes uncategorised spending, as the legacy `catColorMap` did, so it can take a slot no category gets.
- 7.12: `POST /transactions/ids` has the operationId `transactionRange` (the linter rejects the generated `TransactionIds`).
- 7.11: `make fuzz` skips fuzz targets named `…DB`; `make fuzz-db` runs them (one Postgres container per fuzz worker), nightly in `.github/workflows/nightly.yml` together with 60 s of every pure target.
- 6.2/6.4: the mapping references columns by index **and** header name (`{"index": 3, "header": "Kwota"}`; `ledger.MappingDoc`, bound to the OpenAPI schema with `x-go-type`). A mapping whose header names no longer match the shaped table (e.g. after changing `headerRow`) is a `422`, instead of silently reading the wrong column.
- 6.2: the upload body limit is `FINUANCE_MAX_UPLOAD_BYTES` + 64 KiB for the multipart framing; the file itself is checked exactly. A request with a non-multipart `Content-Type` is `415`, and a body over the limit is `413`, also when the request validator is the one reading it.
- 6.3: the per-row `duplicate` flag means "the account already has this identity" before the commit, and "this row's transaction takes its values from another import" after it.
- 6.10: `GET /lines` is served by a new `ingest.Lines`, which decodes and classifies records (header, preamble, data, footer, blank, skipped) with the same rules as `Shape`. A test checks the two agree on every fixture and option set (241 pairs).
- 6.12: `profileLabel` is assigned per distinct header layout among the user's imports (first-seen order), shown once there are two or more layouts. Counting layouts rather than saved profiles labels a new layout's preview straight away, before any profile exists for it.
- 6.6: overlapping commits, deletes, reprocesses and override changes on one account are serialised by a row lock on the account (ADR 0013).
- 6.9: `SeedDemo` lives in `ledger` (the CLI and Phase 8's `POST /demo-data` share it) and commits inline, without a job. If the import fails (e.g. the user already imported the same demo bytes), the new demo account is removed again.
- Phase 6: ledger error kinds (`ErrNotFound`, `ErrConflict`, `ErrState`, `ErrInvalid`) now map to 404/409/409/422 in `problems.go`; before, they would have surfaced as `500`.
- 5.1: argon2id parameters are configurable through `FINUANCE_ARGON2_MEMORY_KIB`, `FINUANCE_ARGON2_TIME` and `FINUANCE_ARGON2_THREADS` (new config variables; defaults are the OWASP baseline). At most 4 hashes run at once per process.
- 5.2: the session queries changed: `expires_at` is now only the absolute expiry, and the idle expiry is checked against `last_seen_at` (an `idle_cutoff` parameter). `TouchSession` no longer slides `expires_at`. Revoking by public id and listing sessions are new queries.
- 5.3: `requireSession` and `CSRF` run as per-route middlewares of the generated router, so unknown paths still answer `404`, and an unauthenticated request with a bad body gets `401`, not `422`. The OpenAPI validator's security check is a no-op; the session layer authenticates.
- 5.3: the password-checking endpoints (`POST /me/password`, `DELETE /me`) have their own limit (10/min per user), in addition to the §6.1 login and signup limits.
- 5.4: `Set-Cookie` and `Retry-After` are declared response headers in OpenAPI, and `prefs` is `json.RawMessage` (`x-go-type`) so numbers and key order pass through untouched. New problem code `not_implemented` (501) for `GET /me/export` until Phase 7.
- 5.6: the api's insert-only River client is created and passed in `httpapi.Deps.Jobs`; nothing enqueues from the api yet (Phase 6).
- 4.3: queries beyond the plan's list: `groups.sql`, `annotations.sql`, `overrides.sql`, `investments.sql`, `import_rows.sql`. `timestamptz` maps to `time.Time` (sqlc override), not `pgtype.Timestamptz`. Times that matter to logic (session expiry, `updated_at`, `committed_at`) are parameters from `platform.Clock`, not `now()`.
- 4.3: `make gen-check` compares the working tree with the index (plus untracked files) instead of `git status`, so regenerated code that is staged for the next commit counts as clean. A fresh CI checkout still catches drift.
- 4.4: `InsertImportRows` is hand-written in `store/importrows.go`, not sqlc: the arrays carry NULL elements (errored rows), which sqlc can't type. It is the only hand-written statement and is tested against the generated model.
- 4.4: the 100k-row staging insert takes 8.0–9.7 s in the sandbox (target ≤ 3 s on a dev machine). About 75 % is server time. Half of that is parsing the `{"h","v"}` raw JSON, and a third is per-row FK triggers. Even a constraint-free server-side copy takes 2.1 s here. Options (both schema changes) are in `docs/perf.md`. **Our Product Engineer's decision at the gate:** accepted as is; no schema change.
- 4.5: foreign keys are checked without RLS, so a reference to another user's account, group or category would pass the FK. The ledger checks ownership in the same transaction (`ledger/owned.go`, tested). Composite FKs on `(user_id, id)` would move this into the schema; that is left for an ADR.
- 4.5/4.8: the ledger also has `CreateImport` (file + staged rows in one transaction), `ImportRowsPage`, `TransactionProvenance` and `EnsureCategories`, which Phase 6 needs and which the 4.8 test uses. `CommitImport` records the stats and marks the import committed.
- 4.7: `docs/ARCHITECTURE.md` uses Mermaid for the component diagram, the ERD and the request sequence (GitHub renders it).
- 3.3: an empty file, or one with no candidate delimiter, gets `;`, not the plan's "default ','". That is what the legacy does (every candidate scores 0 and `;` comes first), and the strict port keeps it.
- 3.7: the plan's amount-role regex reads `finuance`, a search-and-replace slip from the rename; the legacy (and the port) use `kwota`.
- 3.8: `Stage(ctx, table, mapping, opts) ([]StagedRow, Stats, error)`: the account currency is a field of `StageOptions`, and `ctx` + `error` were added so cancellation works on large files (PLAN §3.10). `Parse` takes `ctx` too.
- 3.8: in debit/credit mode a side that is not a number counts as 0, and the row is an error only when neither side is a number. Legacy rule; an earlier draft was stricter.
- 3.6: `ParseNumber` and column typing use `float64`, as in the legacy (typing and role heuristics only). Every stored amount goes through `money.ParseMinor`, and `check-no-float` covers `internal/money` and the fingerprint path.
- 3.14: `BenchmarkParse_100k` median ≈ 563 ms on the shared 4-vCPU sandbox (best 509 ms) vs the 500 ms target. **Our Product Engineer's decision at the gate:** accepted; further optimisation can go into a later phase that touches ingest (options in `docs/perf.md`).
- 3.6/3.11: dates with a time, years 0–99 and query dates with a time differ from the legacy on purpose: `ALLOWED_DIFFS.md` rows 9–11, each checked by a strict classifier in the tests.
- 0.2: the scrub also replaced digit runs of 6–7 digits in `SAMPLE` (card fragments `*********0229197`, reference parts), not only runs of 8 or more, plus the P24 reference and merchant addresses. 26-digit account numbers became the allowlisted all-zero form (`'00000000000000000000000001`), so `check-private.sh` stays strict. One example id in `legacy/DESIGN.md` §7.1 (`0229197`) became `0123456`.
- 0.2: `check-private.sh` also catches the spaced IBAN form (`PL61 1090 1014 …`), and scans untracked, non-ignored files too, so it fails before a commit, not after.
- 0.7: Docker isn't available in the cloud session that built Phase 0. `bootstrap.sql` and the init wrapper were run against a throwaway PostgreSQL 16 cluster: roles and extensions were created, a password containing a quote worked, `finuance_app` can't create tables but gets DML on tables the owner creates. The Compose file was checked with `docker compose config`. The CI job `db` runs the real `make db-up` and the role/extension checks on `postgres:18.6`.
- 0.7: the acceptance command is `make db-psql SQL='\du'`, not `make db-psql -c '\du'`, because make treats `-c` as its own flag.
- 0.1: the Go module path is `github.com/x2db736ac85h/finuance/backend`, not `…/finuance`, because `go.mod` lives in `backend/` (§3.3). A module path should match its directory so `go` tooling and imports resolve. Recorded in ADR 0001.
- 2.7: the baseline migration is `00001_baseline.sql`, not `00000_…`, because goose rejects version 0. Phase 4's migrations therefore become `00002_core.sql` and `00003_rls.sql`.
- 2.1: `worker` needs at least one River worker to start, so `internal/jobs` has a `noop` job kind (also used by the end-to-end test that proves a job inserted by another connection is processed). `seed-demo` and `admin` print "not yet implemented (Phase 9 / Phase 5)".
- 2.10: image size is 32.3 MB uncompressed (9.8 MiB compressed). The OTLP/HTTP trace exporter alone adds ~10 MiB (it links protobuf and gRPC even over HTTP). **Our Product Engineer's decision at the gate:** the target is now ≤ 35 MB uncompressed / ≤ 15 MB compressed, enforced by the CI `image` job.
- 2.10: the Dockerfile also builds from a `backend/vendor/` directory when one is in the build context (gitignored), for builds without network access to the module proxy; CI downloads modules normally.
- 2.12: images are scanned with Trivy in a separate CI job (`image`); CI also runs on pushes to `phase/**` so a phase can be verified before it is merged.
- 2.12: `govulncheck` can't run in the cloud session (`vuln.go.dev` is blocked there); it runs in CI. Its first run flagged `moby/go-archive` v0.2.0 (pinned by testcontainers v0.44.0, reached through `testutil`); it was bumped to v0.3.3 and CI is green.
- 1.2: fixtures are generated by `tools/legacy-oracle/make-fixtures.mjs`, not hand-written byte by byte. The script writes the exact bytes (and the README), and `make fixtures-check` catches drift. 58 fixtures instead of the minimum 35.
- 1.3: the anonymiser's header detection deliberately differs from legacy `buildTable`. Picking a data row as the header would copy it verbatim, which the legacy rule does for PKO BP and mBank. Fake cities are invented names, because a real city can coincide with the user's. Alphanumeric codes of 8+ characters (references, terminal ids) are replaced as a whole.
- 1.4: the oracle exposes 10 names beyond the plan's list (`CHARTS`, `ovCtx`, `drawChart`, `view`, `setActive`, `addFiles`, `setPassphrase`, `savePrivate`, `saveChatsNow`, `INV0`), which the investment cards and backups need. All 32 plan names exist unchanged.
- 1.4: the backup fixtures use the duplicate-versions files (same layout as the demo), so both a `dupPick` and a `dupKeep` override exist.
- 1.4: the locked backup was verified to decrypt with the test passphrase using WebCrypto (verifier `finuance`, private data, file bytes).
- 0.8: gitleaks runs as the pinned release binary (checksum verified) instead of `gitleaks-action`, which needs a license key for organisation accounts.

## Follow-ups (must be done in a later phase)
- **Phase 17 (security hardening) and the Phase 18 gate, required before the public demo (our Product Engineer, 2026-10-23):** a full security scan with no accepted findings. `.trivyignore` must be empty (today: CVE-2026-111497 until 2026-10-30, removed when Renovate bumps the nginx base); the release's Trivy gate is raised from CRITICAL to HIGH + CRITICAL; Trivy also scans the repository (Go and npm dependencies, secrets, misconfiguration, the Helm chart); `govulncheck` and `npm audit` clean; the OpenVAS / Nessus scan and an OWASP ZAP scan (17.6) of the deployed instance with every finding fixed or, if not exploitable, documented in `docs/security-scan.md`. The Phase 18 gate repeats the scans right before the demo goes public.
- Phase 14 (deferred gate checks, our Product Engineer): migrate the real legacy backup, read `docs/parity-report.md`, run `compose.prod.yaml` with the released images on a clean machine, 60 fps scrolling and the overview's negated-category filter (348 ms p95 in the sandbox) on the homelab.
- Phase 1: bank notes from the private samples, for the fixtures and the anonymiser:
  - **Pekao**: UTF-8 without BOM, LF, `;`, `dd.mm.yyyy`, comma decimals, `'`-prefixed account numbers and references, payee padded with spaces before the city. This is the same layout as the legacy `SAMPLE`.
  - **PKO BP**: Windows-1250, CRLF, `,`, ISO dates, dot decimals with no fixed scale (`-10`, `-43.52`). The header has 6 named columns plus 5 blank ones; extra description fields (`Lokalizacja: …`, `Numer karty: …`) spill into the unnamed columns. There are blank lines between rows.
  - **mBank**: UTF-8 with BOM, CRLF, `;`. A ~25-line preamble holds the client name, period and account summary. The header is `#Data operacji;#Opis operacji;…` with `#`-prefixed names. Amounts carry the currency suffix (`-800,00 PLN`), rows end in a trailing `;;`, and descriptions are quoted and padded with runs of spaces.

- Anonymisation key: `testdata/private/anon.key` (gitignored, lost with the container). A new key only changes the fake values of future runs.

- Phase 8: `POST /demo-data` calls `ledger.SeedDemo` (the same path as `finuance seed-demo`).
- Phase 8: the grid sends `includeTotals: false` for every window after the first.
- Phase 16: the RUNBOOK repeats the `textlike` LEAKPROOF step for restores into a new cluster (bootstrap.sql, docs/perf.md).
- Phase 13 (deferred gate checks, our Product Engineer): `finuance admin llm-probe` against LM Studio (recorded streams become `internal/llm` test cases), `make eval-assistant` on the chosen model(s) to fill `docs/evals.md` and decide the default toolset (full or compact), questions on the real data in the panel.
- Later (optional): re-measure the staging insert on a dev machine; the schema options in `docs/perf.md` stay open if it matters.

## Backlog (ideas, not scheduled)
- FX conversion with NBP table A rates (`fx_rates`, `prefs.baseCurrency`; PLAN 15.6, deferred at the Phase 15 plan).
- The assistant names the category of a suggested rule (PLAN 15.10 stretch).
- Restore a Finuance data export (`GET /me/export` zip) into a new instance.
- Per-user model API keys for the assistant (ADR 0015; connections are instance config for now).
- Per-month budgets with rollover.
- Balance reconciliation against the bank's `saldo` column.

## Session log
- 2026-10-23 — Phase 15 first push on `phase/15-product-depth`: category management with aliases, rules applied from scratch (ADR 0017), transfer detection with the user's decisions (ADR 0018), the `reclassify` job, Settings → Categories and Rules, rule window, Save filters as rule, transfer badge/toggle/inspector; Go, Vitest and E2E tests, parity 30/30 with transfers included. Next: push 2 (bulk edit, cadence, group reports, rule suggestions).
- 2026-10-23 — `v0.1.0` published from `main`. Phase 15 planned and approved (transfers shown in the table, rules re-applied from scratch with provenance, assistant proposals now with a log line until the audit log, FX and assistant-named suggestions to the backlog, three pushes). Branch `phase/15-product-depth`.
- 2026-10-23 — `v0.1.0-rc.1` released (every release step green). A new HIGH CVE in the nginx base turned CI red; accepted for one week in `.trivyignore` (unreachable), with a full security scan required before the public demo. Phase 14 gate approved with the hands-on checks deferred; merged. Next: plan Phase 15.
- 2026-10-23 — Phase 14 second and third pushes on `phase/14-quality-gate`: E2E completion, parity harness (30/30 equal), axe on every screen (zero violations after contrast and focus fixes), Phase 14 perf, PARITY settled; release workflow, standalone `compose.prod.yaml`, CHANGELOG, README v0.1. The fresh-database E2E found the window statement running once per row with stale statistics (fixed with a materialised CTE). Tag pushes are refused from the cloud session, so `v0.1.0-rc.1` waits for our Product Engineer. Next: the tag, the release run, then the gate review.
- 2026-10-23 — Phase 14 first push on `phase/14-quality-gate`: legacy migration (key replay checked against the oracle, job, API, wizard with browser-side decryption, report), ADR 0016. Next: E2E completion, parity harness, accessibility, performance, PARITY audit.
- 2026-10-23 — Phase 14 planned and approved (axe dev dependency, release without third-party actions, rc tag for the gate, migrated chats active from migration, standalone prod Compose, three pushes). Branch `phase/14-quality-gate`.
- 2026-10-23 — Phase 13 gate approved by our Product Engineer, who will test against real models later (follow-up recorded); merged. Next: plan Phase 14.
- 2026-10-23 — Phase 13 built on `phase/13-assistant` (second push): the assistant panel (streaming answers, lookups with Show in table, recent chats, settings with connection/model, thinking, retention and cloud consent), E2E against the mock, PARITY. Next: our Product Engineer's gate review (llm-probe and `make eval-assistant` on the homelab model, questions on the real data).
- 2026-10-23 — Phase 13 backend built on `phase/13-assistant` (first push): `internal/llm` adapters, connections config and status, oracle-equal tools and prompt, run loop, chat storage with retention, SSE endpoints, `testutil/llmmock`, `llm-probe`, eval harness (46 questions), ADR 0015, `docs/assistant.md`. Next: the panel and E2E.
- 2026-10-23 — Phase 13 planned and approved (provider-agnostic assistant: instance connections, OpenAI + Anthropic adapters, cloud consent, five tools, 48 h retention with 1–7 days or forever, 25 chats, rate limit setting, two pushes). Branch `phase/13-assistant`.
- 2026-10-23 — Phase 12 gate approved; merged. Next: plan Phase 13.
- 2026-10-23 — Phase 12 built on `phase/12-overview`: overview API (one document per screen), budgets, investments, legacy oracle overview twins with Go parity tests, the overview page with KPIs and twelve cards, budget/opening/investment editors, actions, perf at 100k rows; 267 Vitest + 33 Playwright specs, integration tests green. Next: our Product Engineer's hands-on gate review.
- 2026-10-23 — Phase 12 planned and approved (one overview request, oracle overview twins, uncategorised budget default-only, two pushes). Branch `phase/12-overview`.
- 2026-10-23 — Phase 11 gate approved; merged. Next: plan Phase 12.
- 2026-10-22 — Phase 11 built on `phase/11-insights`: insight endpoints (aggregations in `query`, rules in `insights`), oracle chart twins with a parity test over 16 views, chart library and the six charts, pane with period picker and Charts menu, chart window, chart → table actions, perf at 100k rows; 245 Vitest + 29 Playwright specs, integration tests green. Next: our Product Engineer's gate review (legacy vs new pane on the same demo data).
- 2026-10-22 — Phase 11 planned and approved (aggregation SQL in `query`, all cash-flow months, oracle chart twins, two pushes). Branch `phase/11-insights`.
- 2026-10-22 — Paused before Phase 11 for public-repository readiness (`chore/public-readiness`): README rewrite with screenshots, docs index, SECURITY/CONTRIBUTING, placeholder READMEs, naming clean-up; fixed the search box not emptying after *Clear all*. Next: plan Phase 11 (the plan proposed earlier still stands).
- 2026-10-22 — Phase 10 gate approved; merged. Next: plan Phase 11.
- 2026-10-22 — Phase 10 built on `phase/10-table`: grid (windows, menus, chips, export, keyboard), inspector (category picker, note, sources), status bar, groups (API, menu, quick bar, pills), multi-selection, duplicates window, delete-import perf fix (migration 00005), 100k-row measurements (`tools/perf/grid.mjs`); 196 Vitest + 25 Playwright specs, integration tests and CI green. Next: our Product Engineer's gate review (hands-on legacy vs new table on the same demo data; 60 fps on real hardware).
- 2026-10-22 — Phase 10 planned and approved (one phase, two halves; bulk by ids). Branch `phase/10-table`.
- 2026-10-22 — Phase 9 gate approved; merged. Next: plan Phase 10.
- 2026-10-22 — Phase 9 built on `phase/09-import`: header fix for mBank/PKO BP, upload queue, import window (legacy v7 parts, debounced PATCH, commit/reprocess/retry), sidebar import history, fast path with Undo, account-in-view uploads; 152 Vitest + 13 Playwright specs, integration tests and CI green. Next: our Product Engineer's gate review (import the anonymised real files through the UI).
- 2026-10-22 — Phase 9 planned and approved (backend header fix; preview as a window with its own URL). Branch `phase/09-import`.
- 2026-10-22 — Phase 8 gate approved; merged. Next: plan Phase 9.
- 2026-10-22 — Phase 8 built on `phase/08-frontend`: scaffold, typed client, legacy styles/icons, UI primitives, shell, auth, stores + preferences sync, settings, demo data, web image + Compose, CI `web`/`e2e` jobs, 5 E2E specs. Legacy vs new screenshots match closely. Next: our Product Engineer's gate review.
- 2026-10-21 — Phase 8 planned and approved (3 dev type/lint deps; inline demo-data commit). Branch `phase/08-frontend`.
- 2026-10-21 — Phase 7 gate approved with option (a) and `includeTotals`; both added (search p95 273 → 114 ms in the sandbox), merged. Next: plan Phase 8.
- 2026-10-21 — Phase 7 built on `phase/07-query`: filter language + compiler + periods + engine (all 84 legacy filter cases, assistant filter parity), query/distinct/ids/detail/patch/categories endpoints, CSV export (ADR 0014), zip data export, query indexes (perf.md: 4 of 7 shapes within target in the sandbox; trigram index unusable under RLS), nightly DB fuzz, `query-smoke.sh`. Next: our Product Engineer's gate review.
- 2026-10-21 — Phase 7 planned and approved (collapsed-whitespace equality as an allowed diff; fixed `-79,99` / `dd.mm.yyyy` text form). Branch `phase/07-query`.
- 2026-10-21 — Phase 6 gate approved; merged. Next: plan Phase 7.
- 2026-10-21 — Phase 6 built on `phase/06-imports`: accounts and the full import lifecycle over HTTP (upload, preview, lines, PATCH, commit job, reprocess, delete, fast path, labels), Rematerialize + overrides with a property test, per-account writer lock (ADR 0013), seed-demo, worker in Compose, `import-smoke.sh` (matches the oracle). Next: our Product Engineer's gate review.
- 2026-10-21 — Phase 6 planned and approved (pre-fill the suggested account). Branch `phase/06-imports`.
- 2026-10-21 — Phase 5 gate approved; merged. Next: plan Phase 6.
- 2026-10-21 — Phase 5 built on `phase/05-auth`: argon2id + ADR 0010, sessions (absolute + idle expiry, rotation, revoke), CSRF + ADR 0011, rate limits + ADR 0012, auth/me/sessions/preferences endpoints, `DELETE /me`, session cleanup job, insert-only River client, `admin create-user`/`set-password`, THREAT-MODEL v1, `auth-smoke.sh` (passes against `make up`). Next: our Product Engineer's gate review.
- 2026-10-21 — Phase 5 planned and approved (deps x/crypto + x/time, admin set-password, strict first preferences save). Branch `phase/05-auth`.
- 2026-10-20 — Phase 4 gate approved (benchmark accepted); merged. Next: plan Phase 5.
- 2026-10-20 — Phase 4 built on `phase/04-schema-store-rls`: core schema + forced RLS, `store.Store` (`WithUser`, `WithUserReadOnly`, `WithoutUser`), the sqlc query set, the `unnest` staging insert, `ledger` repositories, commit core, annotations, RLS suite, `ARCHITECTURE.md`. Staging benchmark above target in the sandbox. Next: our Product Engineer's gate review.
- 2026-10-20 — Phase 4 planned and approved (FORCE RLS, query-time collation, commit core in `ledger`, investments repo later). Branch `phase/04-schema-store-rls`.
- 2026-10-20 — Phase 3 gate approved (benchmark accepted); merged. Next: plan Phase 4.
- 2026-10-20 — Phase 3 built on `phase/03-ingest-engine`: decode, delimiter, tokenizer, shape, value parsers, exact money parsing, column typing and roles, staging, fingerprints, `Parse`. Oracle differential suite + cfg parity pass. 61 golden files, 5 fuzz targets (5 min each, clean), coverage 96.7 %, demo generator port. Benchmark ≈ 563 ms median (target 500 ms, not met in the sandbox). CI green. Next: our Product Engineer's gate review.
- 2026-10-20 — Phase 2 gate approved (image limits ≤ 35 MB / ≤ 15 MB compressed); merged. Next: plan Phase 3.
- 2026-10-20 — Phase 2 built on `phase/02-backend-skeleton`: binary + subcommands, config, observability, middleware, problems, OpenAPI v0 + codegen, store + migrations + sqlc, testcontainers harness, image, Compose, CI. `make up` verified locally. Next: our Product Engineer's gate review.
- 2026-10-20 — Phase 1 gate approved; real fixtures committed, oracle regenerated (62 ingest outputs), branch merged. Next: Phase 2 (backend skeleton).
- 2026-10-20 — Phase 1 built on `phase/01-legacy-extraction`: parity checklist, 58 synthetic fixtures, anonymiser, legacy oracle (deterministic), allowed diffs, CI. Next: our Product Engineer reviews the anonymised samples and the gate; then Phase 2.
- 2026-10-20 — Phase 0 gate approved; main rewritten and phase/00-bootstrap merged. Next: plan Phase 1.
- 2026-10-20 — Phase 0 built on `phase/00-bootstrap` (clean orphan root, legacy scrub, guardrails, docs, ADRs, Makefile, local DB, CI). Next: our Product Engineer reviews the gate; then Phase 1 (legacy extraction).
