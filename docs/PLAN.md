# Finuance — rebuild plan: single-file app → Go + PostgreSQL + React

> **Audience:** Claude Code (primary executor) and our Product Engineer (owner, reviewer).
> **Purpose:** rebuild the single-file `finuance.html` MVP (formerly CSV Inspector) as a separated frontend/backend, multi-user, self-hostable personal-finance application that is production-grade and portfolio-worthy.
> **Scope:** web app only. **No desktop app** in this plan (see §1.4).
> **Name:** **Finuance** (formerly CSV Inspector; purple book-with-$ logo). Code identifiers, env vars and DB roles use `finuance` / `FINUANCE_`.
> **Revision 3 (2026-10-20):** synced with the Finuance release of the legacy app (rename, duplicates window, Investment / Yield on cost / FI/RE cards, settings clean-up), plus a full consistency review. See [Revision history](#revision-history).

---

## Table of contents

- [Revision history](#revision-history)
0. [How to use this document](#0-how-to-use-this-document)
1. [Product brief](#1-product-brief)
2. [Locked decisions](#2-locked-decisions)
3. [Architecture](#3-architecture)
4. [Domain specification](#4-domain-specification)
5. [Data model](#5-data-model)
6. [API specification](#6-api-specification)
7. [Frontend specification](#7-frontend-specification)
8. [Cross-cutting concerns](#8-cross-cutting-concerns)
9. [Phased implementation plan](#9-phased-implementation-plan)
10. [Appendices](#10-appendices)

---

## Revision history

### Rev 4 — 2026-11-03 · regrouping after Phase 18

Phase 18 (homelab rollout) is complete. Before the public release, the maintainer wants more product
work, so two phases are added and the public release moves to the end:

| Phase | Content |
|---|---|
| **19** (new) | Admin panel (users, limits, instance defaults, feature toggles, usage) without access to anyone's financial data; colour themes with an admin theme editor; friends through hourly friend codes; manual cash expenses; **shared groups** (expenses shared with friends, splits, balances, settling up) and a Shared tab with a timeline of trips |
| **20** (new) | Agentic assistant (proposals applied on a click or automatically by opt-in, every change marked as AI-made and revertible; receipts by photo; goals in plain language; daily/weekly digests), an advice library, the admin's assistant section (connections, tuning, token budgets), personal access tokens and a CLI for agents |
| **21** (was 19, before that 18) | Public release: privacy clean-up of the tree and history, security scanning in CI, signed images only, the public demo host, documentation and write-ups, a new public repository, `v1.0.0` |

The domain, schema and API sections (§4–§7) describe Phases 0–18. Phases 19 and 20 record their
designs in ADRs and in the docs they add (`docs/admin.md`, `docs/sharing.md`, …); the task lists live
in `docs/PROGRESS.md`. Items in Appendix J that these phases schedule are marked there.

### Rev 3 — 2026-10-20 · Finuance release + consistency review

**What changed in the legacy app** (design doc §32; the file is now `finuance.html`, ~4,100 lines):

1. **Renamed to Finuance.** It now has a purple book-with-$ logo and favicon. IndexedDB and backup names were migrated from `csv-inspector`.
2. **Duplicates window.** The status-bar text "N duplicates skipped" is now a link that opens a dialog. For each duplicate group, it shows every version of the transaction side by side, with the differing fields highlighted. Two actions:
   * **Use this version** (`dupPick`): that file's version becomes the one shown.
   * **Keep both / Merge again** (`dupKeep`): stop merging the group, or merge it again.
3. **Three new overview cards:** Investment, Yield on cost and FI/RE number. They use manual data: PPK contributions and value, investment positions, the safe withdrawal rate (SWR) and the expected real return.
4. **UI clean-up:**
   * Theme and interface size moved to Settings → Appearance.
   * The title-bar open, theme and size buttons are gone.
   * A keyboard shortcuts window, opened with `?` or from Settings.
   * The Columns menu has a single *Hide all / Show all* toggle.
   * The status-bar totals picker appears only when there are 2+ number columns.
   * Insights cards can be reordered (drag, or ↑/↓).

**What changed in this plan:**

| Area | Change |
|---|---|
| Naming | The project is **Finuance**:<br>• module `github.com/<owner>/finuance`, binary `finuance`;<br>• env `FINUANCE_*`, DB roles `finuance_owner` / `finuance_app`;<br>• cookie `finuance_session`, header `X-Finuance-Client`, metrics `finuance_*`;<br>• legacy reference file `legacy/finuance.html`. |
| Domain | §4.4 duplicate review: prefer a version or keep both, via `split_scope`, value source rows and **re-materialisation** (with a property test).<br>New §4.9 investments; new §4.10 legacy migration.<br>§4.6 overview gains three cards; new `balanceAfter` column. |
| Schema | New tables: `dedupe_overrides`, `investment_profile`, `investment_positions`, `legacy_imports`.<br>New columns: `split_scope`, `source_import_id`.<br>Tables listed in FK order. |
| API | `/meta` added to the endpoint table.<br>Duplicates, investments and legacy-migration endpoints.<br>`locateId`/`statColumn` documented; one file per upload request.<br>New `FINUANCE_PUBLIC_ORIGIN` for the CSRF check. |
| Frontend | Settings popover with Appearance; keyboard shortcuts window; Duplicates dialog.<br>Insights card ordering; totals picker rule; logo/favicon.<br>No title-bar import button. |
| Phases | Phase 14 becomes **Legacy migration and quality gate** (size L): your real data moves over through a backup import, and the parity harness also runs through it.<br>Duplicates land in Phases 6 and 10; investments in Phase 12. |
| Consistency fixes | Non-goals no longer exclude the (manual) investment features.<br>`Period` has no `all` preset (null = all time).<br>The preferences comment points to §7.4.<br>Import UI required roles include the debit/credit alternative.<br>Dev compose ports documented.<br>`float64` rule clarified.<br>`llmmock` is its own binary (`cmd/llmmock`), never shipped in the production image.<br>Tables whose rows broke out of their list indentation (grid columns, overview cards) were repaired. |

**Caveat:** this revision was written from the Finuance design doc only; the latest `finuance.html` wasn't attached. Function names for the new features come from the doc: `dupChanged`, `cardOrder`, `exportBackup`, `ser`/`deser`, `o_inv`, `o_yoc`, `o_fire`, `KEYS`. Phase 1 verifies them against the file.

**If work has already started:**
* Follow the same catch-up procedure as Rev 2.
* Rename identifiers **before Phase 2 creates the Go module**. After that, a rename needs its own refactor PR.

### Rev 2 — 2026-10-20 · synced with legacy app v7

**What changed in the legacy app** (v3 → v7; `legacy/DESIGN.md` §29–§31; the HTML grew from ~2,600 to ~3,800 lines):

1. **AI assistant (legacy §29).** A chat panel (⌘J) talks to a local OpenAI-compatible server (LM Studio, Qwen models).
   * The model gets a compact prompt (schema, vocabularies, date rules) plus three tools that run locally: `search_transactions`, `aggregate_transactions`, `find_values`. The dataset never leaves the app in bulk.
   * Native tool calling, with a JSON "plan mode" fallback for models without it.
   * Streaming answers, a reasoning view, per-lookup transparency, "Show in table".
   * The last 10 chats are stored, encrypted when the passphrase lock is on.
   * The CSP now allows exactly one user-approved origin, through a runtime narrowing meta tag.
2. **Transaction groups and multi-selection (legacy §30).**
   * Colour-coded user labels, one per transaction, keyed by a content hash that survives re-imports.
   * A virtual "Grouping" column that works with search, filters, sort, export and charts.
   * Drag, Shift and ⌘ selection; a floating quick bar with Σ and group assignment; a Groups menu.
3. **Per-file import settings (legacy §31).** An import window with:
   * a raw-lines header picker (explicit header row, or no header + skip rows);
   * footer-row dropping, month-first dates, sign inversion;
   * a column-role map (`date, vdate, amount, debit, credit, payee, desc, category, currency, ref, type, account, balance, ignore`). It renames columns to canonical names so different banks line up.

   Other import changes:
   * Auto header detection now skips padded preamble lines (fill ratio ≥ 80 %).
   * The sidebar is hidden by default.
   * Source labels `b1`, `b2`, … mark files with different import profiles.

**What changed in this plan:**

| Area | Change |
|---|---|
| Domain | §4.2 shape rules (header modes, fill ratio, footer drop, month-first dates); §4.3 mapping by column index, `ignore`, debit/credit is now legacy parity; §4.4 user annotations keyed by dedupe identity; new §4.7 groups; new §4.8 assistant |
| Schema | `groups`, `tx_annotations`, `transactions.group_id`, `imports.header_signature`; assistant tables (Phase 13) |
| API | Groups CRUD + stats, `POST /transactions/ids`, bulk with `groupId`, `GET /imports/{id}/lines`, `autoCommit`, assistant endpoints with SSE streaming |
| Frontend | Port of the import window, Groups menu, quick bar, multi-selection, chat panel, ⌘J, sidebar hidden by default |
| Phases | New **Phase 13 — AI assistant**; old Phases 13–17 are now **14–18**. Groups and multi-selection land in Phase 10; import settings in Phases 3, 6 and 9; the oracle is extended in Phase 1 |
| New ideas | Assistant runs on the server under RLS (strict browser CSP); LLM eval suite; import fast path for known layouts; labels that survive delete + re-import; assistant action proposals with confirmation, insight tools, group reports, rule suggestions (Phase 15) |

**If work on rev 1 has already started:**
1. Finish the current task.
2. Diff `docs/PLAN.md` against this file and list the completed tasks it affects. The likely ones are the Phase 1 oracle and fixtures, the Phase 3 shape rules, and the Phase 4 schema.
3. Add catch-up tasks to `docs/PROGRESS.md` before continuing.
4. Copy the new legacy files into `legacy/` and **scrub them again**: the `SAMPLE` constant in v7 still contains the real-looking account numbers.

---

## 0. How to use this document

### 0.1 For Claude Code — working agreement

1. **One phase at a time.** Phases are in §9 and are strictly ordered. Do not start phase N+1 until phase N's acceptance criteria are met and our Product Engineer has approved the phase gate.
2. **Start every phase in plan mode.** Read the phase section, every "Read first" reference it lists, and `docs/PROGRESS.md`. Then propose a task breakdown (files to touch, tests to write, open questions) and wait for approval before editing code.
3. **Branch per phase:** `phase/NN-short-name`. Small commits using Conventional Commits (`feat(ingest): …`, `test(api): …`, `chore(ci): …`). Open a PR per phase (or per large task) with a summary, checklist and screenshots where UI changed.
4. **Tests are part of the task, not a follow-up.** A task is done only when its tests exist and pass. Never delete, skip or weaken a failing test to make CI green. If a test reveals that the spec is wrong, stop and ask.
5. **Run the full local check before every commit:** `make check` (lint + typecheck + unit tests + codegen drift check). Run `make test-integration` before closing a phase.
6. **Keep `docs/PROGRESS.md` current:** after each task, tick it, note deviations from this plan and why, and list follow-ups. This file is the hand-off between sessions.
7. **Record decisions.** Any decision not already in §2 that changes architecture, schema, API contract or dependencies gets an ADR in `docs/adr/` (template in Appendix C). Small choices go in the PR description.
8. **The legacy app is the behavioural spec.** When this plan and the legacy code disagree on a behaviour that §4 does not explicitly change, the legacy behaviour wins. Read `legacy/finuance.html` and `legacy/DESIGN.md` for details. Section references like *(legacy §8)* point into `legacy/DESIGN.md`.
9. **Ask instead of guessing** when:
   * a requirement is ambiguous;
   * a dependency must be added that §2 does not list;
   * a schema or API change breaks a previously merged contract;
   * the work would take noticeably longer than the phase's size estimate.
10. **Privacy rule:** never print, commit or paste the contents of `testdata/private/` (our Product Engineer's real bank exports). Only anonymised fixtures in `testdata/fixtures/` may be committed or inspected.
11. **No scope creep.** Ideas that are not in the current phase go to the "Backlog" list in `docs/PROGRESS.md`.

### 0.2 For our Product Engineer — review gates

Each phase ends with a **Gate** block listing what to check (a demo command, a URL, test output, screenshots). Approve in the PR, or reply with changes, before Claude Code continues. Phase sizes (S ≈ 1–2 sessions, M ≈ 3–5, L ≈ 6+) are rough.

### 0.3 Files that must exist after Phase 0

| File | Purpose |
|---|---|
| `CLAUDE.md` | Always-loaded agent instructions (template in Appendix A). Short. Points here. |
| `backend/CLAUDE.md`, `web/CLAUDE.md` | Language-specific conventions, loaded when working in those dirs (Appendix B). |
| `docs/PLAN.md` | This document, committed verbatim. Changes go through PRs. |
| `docs/PROGRESS.md` | Living status (template in Appendix D). |
| `docs/adr/NNNN-*.md` | Architecture decision records. |
| `docs/PARITY.md` | Legacy feature checklist (created in Phase 1). |
| `legacy/finuance.html`, `legacy/DESIGN.md` | Behavioural reference, **scrubbed** (see Phase 0, task 0.2). |

---

## 1. Product brief

### 1.1 What it is

A **self-hostable, privacy-first personal-finance app for European (initially Polish) bank CSV exports.** Users import their statements, the app de-duplicates overlapping exports, normalises transactions across accounts, and provides:

* a fast, keyboard-friendly transaction explorer;
* interactive insights (charts);
* a month-centric finance overview (budgets, recurring payments, money flow, balance).
* user-defined transaction groups (e.g. "Italy trip 2025") with multi-select labelling;
* a privacy-preserving **AI assistant** that answers questions about the user's own data with a self-hosted LLM. It retrieves through read-only tool calls instead of uploading the data.
* duplicate review across overlapping exports: choose the version to keep, or keep both;
* manual investment tracking for the overview: PPK, positions, yield on cost and an FI/RE number.

It is in the same category as Firefly III or Actual Budget. Its differentiators are:

* best-in-class ingest of messy bank CSVs (encodings, preambles, Polish number/date formats);
* correct idempotent de-duplication;
* a dense, IDE-like UI (VS Code / Obsidian / LM Studio look).

### 1.2 Who it is for

* **Primary:** one technical person (or household) running it on their own server or homelab.
* **Secondary:** a public read-mostly demo instance with synthetic data, for recruiters.

### 1.3 What changes from the legacy MVP

| Legacy (single HTML file) | New |
|---|---|
| Files are the source of truth; views are computed over parsed files | **Transactions** in **accounts** are the source of truth; an **import** is an event that adds transactions. Raw file bytes are kept for re-processing. |
| One browser profile, IndexedDB, optional passphrase lock | Multi-user server, PostgreSQL, sessions, row-level security |
| Parse options are global (encoding, delimiter, strip `'`) | Parse options and column mapping are per import, with a preview step and remembered "import profiles" |
| "All data" = union of files with in-memory de-dupe | "All accounts" view; de-dupe enforced by DB constraints per account |
| Filtering/aggregation in the browser over all rows | Filtering, search, sorting and aggregation on the server (SQL), with the same filter language |
| Float64 amounts | Integer minor units (grosze) |
| Charts rendered as HTML strings | Charts as React SVG components (enables a strict CSP) |
| Per-file import settings stored with each file (`e.cfg`); the import window opens only when detection fails | Per-import options and mapping with a preview step, remembered per header layout as **import profiles**; known layouts can import without review (fast path) |
| Groups keyed by a content hash (`txKeys`) in IndexedDB | `groups` table. User annotations (group, manual category, note) are keyed by the dedupe identity `(account, fingerprint, occurrence, split scope)`, so they survive delete and re-import |
| AI assistant: the browser calls LM Studio directly; the CSP is narrowed at runtime to one approved origin; tools run in the browser | The **server** orchestrates the model at an admin-configured OpenAI-compatible endpoint. Tools run as SQL under RLS. Answers stream to the browser over SSE. The browser CSP stays `connect-src 'self'`, and egress is restricted by config + NetworkPolicy |
| *All data* merges every file; duplicate overrides (`dupPick`, `dupKeep`) keyed by content | De-duplication **per account**. Overrides (prefer a version / keep both) live in `dedupe_overrides` and are applied by re-materialising the affected fingerprint. **[CHANGE]** Files in different accounts are never merged. |
| Manual investment data (`S.inv`) in IndexedDB | `investment_profile` + `investment_positions` tables; the calculations are part of the overview payload |
| Backup = JSON dump of IndexedDB | Data export endpoint and database backups. A **one-time migration** imports a legacy backup into the new app (Phase 14). Encrypted backups are decrypted in the browser. |

### 1.4 Non-goals (for this plan)

* Desktop app (Wails etc.), mobile app, offline mode.
* Open-banking / PSD2 connections. CSV import is the only ingest.
* Automated investment tracking (broker imports, market prices, net worth across asset classes) and loans. Manual investment data for the overview cards (PPK, positions; legacy §32.4) **is** in scope.
* UI internationalisation. The UI stays English; data stays Polish. Keep strings centralised so i18n stays possible.
* Microservices, message brokers, Redis. One modular monolith plus a worker, with Postgres as the only stateful dependency.
* Cloud LLM providers as a default. The assistant targets self-hosted OpenAI-compatible servers (Ollama, LM Studio, llama.cpp, vLLM). A remote endpoint requires an explicit admin flag.

### 1.5 Definition of "done" for the whole project

* Every item in `docs/PARITY.md` is ported or explicitly retired with a reason.
* Deployed on our Product Engineer's k3s homelab via GitOps, with backups, a tested restore, dashboards and alerts.
* A public demo instance with synthetic data.
* A README a recruiter can understand in 2 minutes. ADRs explain the key decisions.
* CI green: lint, unit, integration, E2E, security scans, signed images.

---

## 2. Locked decisions

Do not relitigate these without an ADR and our Product Engineer's approval.

| Area | Decision | Rationale (one line) |
|---|---|---|
| Backend language | **Go** (latest stable; pin `go` + `toolchain` in `go.mod`) | Owner wants to learn it; explicit, fast compile, good for AI-assisted coding |
| HTTP | **stdlib `net/http`** with method+pattern `ServeMux` | No framework lock-in; idiomatic |
| API contract | **OpenAPI 3.0.3**, spec-first, file `api/openapi.yaml` | Single source of truth for Go server + TS client. 3.0.3 (not 3.1) for best `oapi-codegen` support |
| Go server codegen | **`oapi-codegen` v2**, `std-http-server` + `strict-server` + models | Typed handlers; compile errors when the contract changes |
| TS client codegen | **`openapi-typescript`** types + **`openapi-fetch`** | Tiny runtime, fully typed |
| Request validation | `oapi-codegen` OpenAPI validator middleware (kin-openapi) + domain validation | Reject bad input at the edge |
| Database | **PostgreSQL** (latest stable major, ≥ 17), official image | Integrity constraints, RLS, SQL analytics |
| DB driver | **`pgx` v5** (`pgxpool`) | De facto standard; fast; COPY support |
| Queries | **`sqlc`** for static queries; small hand-written builder for dynamic filter SQL | Explicit SQL, generated types; dynamic WHERE is sqlc's weak spot |
| Migrations | **`goose` v3**, SQL files embedded with `embed.FS` | Simple, plain SQL, runs from the binary |
| Background jobs | **River** (Postgres-backed queue) | No extra infrastructure; transactional enqueue |
| Logging | `log/slog`, JSON in prod, text in dev | Stdlib |
| IDs | **UUIDv7** generated in Go (`google/uuid`) | Time-ordered, index-friendly |
| Money | `int64` minor units + ISO currency code | No float rounding errors |
| Auth | Email + password (**argon2id**), server-side sessions in Postgres, HttpOnly cookie | Simple, secure, self-host friendly. OIDC later (backlog). |
| Multi-tenancy | `user_id` on every owned row + **Postgres RLS** as defence in depth | Talking point; prevents cross-tenant bugs |
| Frontend | **React + TypeScript (strict) + Vite** | Market-standard; best AI-assist coverage |
| Server state | **TanStack Query** | Caching, retries, invalidation |
| Client UI state | **Zustand** | Tiny; mirrors legacy `S.views` |
| Routing | **React Router** (SPA/library mode) | Mainstream |
| Table virtualisation | **TanStack Virtual** (custom grid, no data-grid library) | Keep legacy look and behaviour |
| Charts | **Port legacy SVG chart code** to React components; no chart library | Already validated; distinctive |
| Positioning (popovers/tooltips) | **`@floating-ui/react`** | Replaces hand-rolled `placePop` |
| Styling | Plain CSS: legacy design tokens as CSS variables + **CSS Modules** per component; rem-based scaling | Preserves the legacy design exactly; no Tailwind |
| Go testing | stdlib `testing`, `go-cmp`, **testcontainers-go** (Postgres), native fuzzing, golden files | Minimal deps; real DB in tests |
| Web testing | **Vitest** + Testing Library + MSW; **Playwright** E2E | Standard |
| Lint/format | `golangci-lint` (v2 config), `gofmt`/`goimports`; ESLint (flat) + typescript-eslint + Prettier | Standard |
| Task runner | **Makefile** at repo root | Universal |
| CI | **GitHub Actions** (public repo is primary); Gitea pull-mirror for the homelab | Recruiters see CI |
| Images | Backend: distroless static (`:nonroot`). Web: unprivileged nginx serving static files + proxying `/api` in Compose | Small, secure |
| Deploy | Docker Compose (dev / single host); **Helm chart** on **k3s**; **Argo CD** GitOps; **CloudNativePG** | Showcases DevOps skills on existing homelab |
| Secrets (k8s) | **Sealed Secrets** | Simple with GitOps |
| Observability | OpenTelemetry traces (OTLP), Prometheus metrics (`/metrics`), JSON logs → Grafana stack (Tempo / Prometheus / Loki via Alloy) | Industry standard |
| LLM integration | Server-side orchestration over the **OpenAI-compatible Chat Completions API** (streaming), with a small hand-written client in `internal/llm` (no SDK) | Local servers deviate from the spec (reasoning fields, Qwen `<tool_call>` text, missing tool support). The legacy `complete()` already handles this and gets ported. |
| Assistant transport | **Server-Sent Events** from `POST /assistant/chats/{id}/messages` | One-way token stream; works through nginx and the ingress with buffering off; simpler than WebSockets |
| Markdown in answers | Port the legacy `md()` to a React-node renderer; no markdown library | Keeps the no-`innerHTML` rule; small surface |

### 2.1 Open questions to ask our Product Engineer in Phase 0

1. GitHub owner (the name **Finuance** is decided). It determines the Go module path `github.com/<owner>/finuance`.
2. License (suggest MIT or AGPL-3.0 for a self-hosted app).
3. Which banks' exports they can provide as private fixtures. These get anonymised by the tool from Phase 1.
4. Public demo hosting preference: Tailscale Funnel from the homelab, Cloudflare Tunnel, or a small VPS.
5. Homelab specifics needed in Phase 16 (asked again in Phase 18, the homelab rollout, after the 2026-10-24 regrouping):
   * k3s ingress controller (Traefik default?);
   * cert-manager issuer name;
   * storage class;
   * whether an S3-compatible bucket exists for backups.
6. Assistant LLM endpoint (Phase 13):
   * Where it runs: the homelab Ollama (already used with n8n), LM Studio on the Mac over the LAN/tailnet, or both (dev vs prod).
   * Which model. A Qwen 2.5/3 instruct with tool calling is the legacy default.
   * Confirm that the assistant must never use a non-private endpoint.

---

## 3. Architecture

### 3.1 Components

```
                 ┌───────────────────────── Browser ─────────────────────────┐
                 │  React SPA (web/)  ── openapi-fetch ──►  /api/v1/*         │
                 └───────────────▲───────────────────────────────┬────────────┘
                                 │ static assets                  │ JSON over HTTPS, session cookie
                    ┌────────────┴───────────┐        ┌───────────▼────────────┐
                    │ web  (nginx, static)   │        │ api   (finuance serve)    │  stateless, N replicas
                    └────────────────────────┘        │  net/http + strict     │
                    In k8s: Ingress routes             │  OpenAPI handlers      │
                    /api → api, / → web                └───────────┬────────────┘
                    In Compose: nginx proxies /api                 │ pgx (tx + SET LOCAL app.user_id)
                                                       ┌───────────▼────────────┐
                    ┌────────────────────────┐         │  PostgreSQL            │
                    │ worker (finuance worker)  │◄───────►│  app tables + RLS      │
                    │ River jobs             │  pgx    │  River job tables      │
                    └────────────────────────┘         └────────────────────────┘
                    One Go binary, one image; subcommands: serve | worker | migrate (DB schema) | seed-demo | admin | healthcheck | version
```

* **api**: HTTP only. It does not run jobs. Long or async work (import commit, rules, recurring detection) is enqueued transactionally via River.
* **worker**: runs River workers and periodic jobs. It uses the same domain packages as api.
* **web**: static files only. No Node at runtime.
* **Same origin everywhere** (ingress or nginx path routing). This means no CORS, and cookies stay `SameSite=Lax`.
* **llm** (optional, external): an OpenAI-compatible server (Ollama, LM Studio, vLLM). Only the api process talks to it, for the assistant. Its address comes from config and is the only extra egress allowed.

### 3.2 Key flows

**Import (two-phase, idempotent):**

```
Browser                    api                                   Postgres                worker
  │ POST /imports (multipart: file, accountId?)                    │                        │
  │──────────────────────────►│ sha256(file); reject exact dup ────►│ imports, import_files  │
  │                           │ ingest.Parse(bytes, opts=auto)      │                        │
  │                           │ detect mapping (profile or roles)   │                        │
  │                           │ stage rows (fingerprint, occurrence)►│ import_rows (staged)   │
  │◄──────── 201 ImportPreview (mapping, sample rows, stats, dup estimate)                  │
  │ PATCH /imports/{id} (mapping/options changes) → re-parse from stored bytes, re-stage    │
  │ POST /imports/{id}/commit ─►│ tx: status=committing; river.InsertTx(CommitImport) ─────►│
  │◄──────── 202 {status: committing}                                │◄────── job picked ─────│
  │                           │                                     │ INSERT … ON CONFLICT   │
  │                           │                                     │ DO NOTHING; link rows; │
  │                           │                                     │ status=committed;      │
  │                           │                                     │ enqueue follow-ups     │
  │ GET /imports/{id} (poll every 500 ms until committed/failed) → stats {new, duplicates}  │
```

**Query (table, insights, overview):** the browser sends a `QueryContext` (accounts, search, column filters, period). The server compiles it into one parameterised SQL fragment (`internal/query`). Every endpoint reuses that fragment, which guarantees that the table, the charts and the status bar always agree.

**Assistant (tool calling, server-side; §4.8):**

```
Browser (ChatPanel)               api (internal/assistant)                      LLM server (Ollama / LM Studio)
  │ POST /assistant/chats/{id}/messages {text, scope}                               │
  │──────────────────────────────►│ build prompt (schema, vocabularies, date rules) │
  │                               │──── chat/completions (stream, tools) ─────────►│
  │◄── SSE reasoning / content ───│◄─── deltas + tool_calls ───────────────────────│
  │                               │ run tools in WithUserReadOnly (RLS) → SQL       │
  │◄── SSE step {tool, args, summary, what is sent back}                            │
  │                               │──── tool results (capped) ────────────────────►│
  │◄── SSE content … done {meta} ─│◄─── final answer ──────────────────────────────│
  │                               │ persist the turn (assistant_messages)           │
```

### 3.3 Repository layout

```
finuance/
├─ CLAUDE.md                     # agent instructions (Appendix A)
├─ Makefile                      # all dev/CI entry points (Appendix F)
├─ README.md
├─ LICENSE
├─ .editorconfig  .gitignore  .gitattributes
├─ api/
│  └─ openapi.yaml               # THE contract
├─ backend/
│  ├─ CLAUDE.md
│  ├─ go.mod  go.sum
│  ├─ .golangci.yml
│  ├─ sqlc.yaml
│  ├─ oapi-codegen.yaml
│  ├─ cmd/finuance/main.go       # subcommand dispatch
│  ├─ cmd/anonymize/             # fixture anonymiser CLI (Phase 1)
│  ├─ cmd/llmmock/               # mock OpenAI-compatible server for CI/local UI work (Phase 13; never in the prod image)
│  ├─ internal/
│  │  ├─ app/                    # wiring: config → deps → servers (composition root)
│  │  ├─ config/                 # env parsing + validation
│  │  ├─ httpapi/                # generated server (gen.go), handlers, middleware, problem errors
│  │  │  ├─ gen/                 # oapi-codegen output (do not edit)
│  │  │  ├─ middleware/
│  │  │  └─ handlers_*.go
│  │  ├─ auth/                   # password hashing, sessions, CSRF, rate limit
│  │  ├─ ingest/                 # PURE: decode, delimiter, csv, table shaping, value parsers, roles, normalise
│  │  ├─ ledger/                 # accounts, imports, fingerprints, dedupe, commit orchestration
│  │  ├─ query/                  # QueryContext → SQL (filter DSL parser, periods, sort)
│  │  ├─ insights/               # aggregations, overview context, highlights, recurring, budgets, balance
│  │  ├─ investments/            # manual investment data (PPK, positions) + yield-on-cost / FI/RE calculators
│  │  ├─ legacyimport/           # (Phase 14) legacy backup import
│  │  ├─ rules/                  # (Phase 15) categorisation rules engine
│  │  ├─ assistant/              # (Phase 13) tools, prompt builder, tool loop, plan mode, chat persistence
│  │  ├─ llm/                    # (Phase 13) OpenAI-compatible streaming client, tolerant parsing
│  │  ├─ jobs/                   # River job args + workers + periodic jobs
│  │  ├─ store/                  # pgx pool, WithUser tx helper, sqlc output in store/db/
│  │  │  ├─ db/                  # sqlc generated (do not edit)
│  │  │  ├─ queries/*.sql        # sqlc input
│  │  │  └─ migrations/*.sql     # goose migrations (embedded)
│  │  ├─ demo/                   # demo-year generator (port of legacy demoCSV)
│  │  ├─ money/                  # minor-unit parsing/formatting, currency exponents
│  │  ├─ observability/          # otel setup, metrics registry, request logging
│  │  └─ platform/               # small shared helpers (clock, ids, errors)
│  └─ testutil/                  # testcontainers harness, fixtures loader, golden helpers
├─ web/
│  ├─ CLAUDE.md
│  ├─ package.json  vite.config.ts  tsconfig.json  eslint.config.js
│  ├─ index.html
│  ├─ src/
│  │  ├─ main.tsx  App.tsx  routes.tsx
│  │  ├─ api/                    # generated schema.d.ts + client.ts + query hooks
│  │  ├─ styles/                 # tokens.css (ported), base.css, scale.ts
│  │  ├─ stores/                 # zustand: viewStore, uiStore
│  │  ├─ shell/                  # TitleBar, Sidebar, Toolbar, StatusBar, Inspector, EmptyState, DropOverlay
│  │  ├─ table/                  # Grid, HeaderCell, ColumnMenu, ColumnsMenu, Chips, highlight
│  │  ├─ import/                 # ImportPreview, MappingEditor, ImportHistory
│  │  ├─ insights/               # InsightsPane, PeriodPicker, ChartCard, ChartModal
│  │  ├─ overview/               # OverviewPage, Kpis, cards
│  │  ├─ assistant/              # ChatPanel, ChatMenu, StepView, Markdown (md() port), SSE reader
│  │  ├─ groups/                 # GroupsMenu, GroupForm, QuickBar, group pills, selectionStore
│  │  ├─ duplicates/             # DuplicatesDialog
│  │  ├─ charts/                 # lib (barPath, arc, spark, cpt, money, colors), components per chart
│  │  ├─ ui/                     # Popover, Toast, Tooltip, Icon, Button, Select, Modal primitives
│  │  ├─ auth/                   # Login, Signup, RequireAuth
│  │  ├─ settings/               # settings pages incl. Data → legacy backup import wizard (§4.10)
│  │  └─ lib/                    # format, dates (UTC/date-only helpers), keyboard, debounce
│  └─ e2e/                       # Playwright specs + fixtures
├─ deploy/
│  ├─ docker/                    # Dockerfile.backend, Dockerfile.web, nginx.conf
│  ├─ compose/                   # compose.yaml (dev), compose.prod.yaml, compose.ci.yaml
│  ├─ db/bootstrap.sql           # roles + extensions (not a migration; needs superuser)
│  ├─ helm/finuance/             # chart
│  ├─ k8s/                       # CNPG cluster, sealed secrets, argocd apps, observability values
│  └─ environments/              # homelab/, demo/ values files (GitOps promotion target)
├─ tools/
│  ├─ legacy-oracle/             # Node + Playwright harness exposing legacy internals (Phase 1)
│  └─ check-private.sh           # fails on IBAN-like numbers / private files in git (Phase 0)
├─ testdata/
│  ├─ fixtures/                  # committed, anonymised/synthetic CSVs
│  ├─ oracle/                    # committed JSON produced by legacy-oracle (expected outputs)
│  └─ private/                   # GITIGNORED real exports
├─ loadtest/                     # k6 scripts (Phase 17)
├─ evals/assistant/              # golden questions + runner for real-model evaluation (Phase 13)
├─ legacy/                       # scrubbed legacy app + design doc (reference only)
└─ docs/
   ├─ PLAN.md  PROGRESS.md  PARITY.md  ARCHITECTURE.md  THREAT-MODEL.md  RUNBOOK.md  perf.md
   └─ adr/
```

### 3.4 Backend dependency rules (enforce in review; optionally with `depguard`)

```
cmd → app → {httpapi, jobs} → {ledger, query, insights, rules, auth} → {store, ingest, money, platform}
ingest, money, query/dsl     : PURE — no imports of store, httpapi, net/http, pgx
store                        : no imports of httpapi
httpapi                      : maps HTTP ⇄ domain; no SQL strings
```

* Domain services take interfaces or `store.Querier`. They return domain types, not sqlc rows.
* Time comes from an injected `platform.Clock`. No direct `time.Now()` in domain code, so tests stay deterministic.
* Every exported function in `ingest`, `query`, `insights` has table-driven tests.

### 3.5 Configuration (env vars, 12-factor)

| Variable | Default | Notes |
|---|---|---|
| `FINUANCE_ENV` | `dev` | `dev` / `test` / `prod`; controls log format and cookie `Secure` |
| `FINUANCE_HTTP_ADDR` | `:8080` | API listener |
| `FINUANCE_ADMIN_ADDR` | `:9090` | `/metrics`, `/debug/pprof` (never exposed via ingress) |
| `FINUANCE_PUBLIC_ORIGIN` | — | Required. Public origin of the web app, e.g. `https://finuance.<tailnet>.ts.net`; dev `http://localhost:8000`. Used for the CSRF `Origin` check and absolute links |
| `FINUANCE_DATABASE_URL` | — | Required. App role (`finuance_app`) DSN |
| `FINUANCE_MIGRATE_DATABASE_URL` | — | Owner role (`finuance_owner`) DSN, used only by `finuance migrate` |
| `FINUANCE_DB_MAX_CONNS` | `10` | pgxpool size |
| `FINUANCE_SESSION_TTL` | `720h` | Absolute session lifetime |
| `FINUANCE_SESSION_IDLE_TTL` | `168h` | Idle timeout |
| `FINUANCE_COOKIE_SECURE` | `true` (prod) | `false` allowed only in dev |
| `FINUANCE_ALLOW_SIGNUP` | `true` | Set `false` on personal and demo instances |
| `FINUANCE_DEMO_MODE` | `false` | Demo login button, read-only guard, nightly reset |
| `FINUANCE_MAX_UPLOAD_BYTES` | `20971520` | 20 MiB per file |
| `FINUANCE_MAX_LEGACY_IMPORT_BYTES` | `209715200` | 200 MiB per legacy import upload (all files + manifest) |
| `FINUANCE_OTEL_EXPORTER_OTLP_ENDPOINT` | empty | Tracing disabled when empty |
| `FINUANCE_LOG_LEVEL` | `info` | |
| `FINUANCE_TRUSTED_PROXIES` | empty | CIDRs allowed to set `X-Forwarded-For` |
| `FINUANCE_AI_BASE_URL` | empty | OpenAI-compatible base URL, e.g. `http://ollama.ollama.svc:11434/v1`. Empty = assistant disabled |
| `FINUANCE_AI_API_KEY` | empty | Optional bearer token (secret) |
| `FINUANCE_AI_DEFAULT_MODEL` | empty | Used when the user hasn't picked a model. Empty = the first chat model from `/models` (embedding models skipped) |
| `FINUANCE_AI_ALLOW_REMOTE` | `false` | When false, startup fails if the base URL host doesn't resolve to a loopback/private address (port of legacy `isPrivateHost`) |
| `FINUANCE_AI_TIMEOUT` | `120s` | Per model request |
| `FINUANCE_AI_MAX_CONCURRENT` | `2` | Concurrent assistant runs per api replica |
| `FINUANCE_AI_MAX_CHATS` | `10` | Chats kept per user (legacy `MAX_CHATS`) |

Tool-only variables, not read by the app: `FINUANCE_ANON_KEY` (anonymiser, Phase 1), `E2E_BASE_URL` (Playwright), `LOADTEST_BASE_URL` (k6).

`config.Load()` validates everything at startup. Missing or invalid values make the binary exit non-zero, naming the variable.

---

## 4. Domain specification

This section is the behavioural contract. It is ported from the legacy code; deviations are marked **[CHANGE]** with a reason. Every rule here must have a test. Where it says "port exactly", copy the legacy algorithm and constants and add a differential test against the legacy oracle (Phase 1).

### 4.1 Conventions

* **Sign convention:** negative amount = money out (expense), positive = money in (income). Everything financial depends on this. Import option `invertSign` handles banks that export expenses as positive numbers.
* **Money:** `int64` minor units plus a currency code. The `money` package holds a currency → exponent map:
  * PLN, EUR, USD, GBP, CHF, CZK, SEK, NOK, DKK, HUF = 2;
  * unknown currencies = 2, with a warning.
  * Formatting for display happens in the frontend. The API returns `{amountMinor, currency}` plus a pre-formatted string only where useful (highlights).
* **Dates:** bank booking dates are calendar dates, not instants. **[CHANGE]** They are stored as SQL `date` (`booked_on`, `value_on`). The legacy app used UTC epoch ms, which is equivalent. No time zones are involved anywhere in domain logic; Go uses a `civil.Date`-like type (`platform.Date`, `{Y, M, D}`) or `time.Time` at UTC midnight. Pick one, write an ADR, and use it consistently.
* **Period intervals are half-open** `[from, to)` everywhere.
* **Normalisation `norm(s)`** (search, filters, fingerprints). Port exactly:
  1. lowercase;
  2. Unicode NFD;
  3. remove combining marks (U+0300–U+036F);
  4. replace `ł` with `l`.

  In Go use `golang.org/x/text/unicode/norm` and `golang.org/x/text/runes`. Note that `Ł` lowercases to `ł` first, then gets replaced.
* **`collapse(s)`**: replace runs of whitespace with a single space.
* **`payeeName(s)`** (grouping key for payees and recurring). Port exactly:
  1. trim;
  2. split on runs of 2+ whitespace and take the first part;
  3. collapse whitespace;
  4. empty → `(unknown)`.

  Example: `ZABKA Z6373 K.1        RASZYN` → `ZABKA Z6373 K.1`.

### 4.2 Ingest pipeline (`internal/ingest`, pure)

`Parse(raw []byte, opts Options) (*Table, error)`.

| Step | Rule (port exactly unless marked) |
|---|---|
| Decode | `opts.Encoding == auto`: strip UTF-8 BOM; if `utf8.Valid` → UTF-8, else **Windows-1250**. Manual options: `utf-8`, `windows-1250`, `iso-8859-2`, `windows-1252` (`golang.org/x/text/encoding/charmap`). Report the encoding used. |
| Delimiter | Candidates in order `;`, `,`, `\t`, `\|`. Use the first 30 non-empty lines. For each candidate, count occurrences outside double quotes per line (a quote toggles in/out). Take the modal count among lines with count > 0, and its frequency `mf`. Score = `mf*1000 + mode` (0 if no mode). The highest score wins; **ties keep the earlier candidate**. Default `,`. |
| Tokenise | RFC-4180-ish state machine: a quote opens only at field start; `""` inside quotes = literal quote; CRLF / CR / LF end a record; a trailing partial record is kept. |
| Shape | Port `buildTable(raw, o)` (legacy v7).<br>**Header mode** (`opts.HeaderRow`): `auto` (default), `none`, or an explicit 1-based record number.<br>**Auto:**<br>1. Drop blank rows and find the modal field count (ties → larger count).<br>2. Among the first 50 non-blank rows, the header is the first row with the modal count **whose non-empty cell count ≥ ceil(0.8 × the max non-empty count of modal-length rows)**. This skips padded preamble lines such as `Bank export;;;`.<br>3. Fall back to row 0.<br>**Explicit N:** record N is the header (out of range → `ErrHeaderRowOutOfRange`); data = the later non-blank records.<br>**None:** data starts at record `opts.SkipRows` (1-based, default 1); headers become `Column N`.<br>Report `headerAt` (1-based record; 0 for none).<br>**Footer drop** (`opts.DropShort`, default on): ref = the header length, or the modal row length when there is no header. If ref ≥ 4, drop data rows with ≤ ref/2 fields (e.g. `Saldo końcowe;1234,56`). Report `dropped`.<br>**Then, in order:**<br>1. width = max(header length, max row length);<br>2. pad ragged rows with `''` and trim cells;<br>3. with `opts.StripQuote` (default true), remove one leading `'` from data cells (not headers);<br>4. drop columns whose header is empty **and** whose data cells are all empty;<br>5. name blank headers `Column N` (1-based index after dropping);<br>6. de-duplicate names (`X (2)`, `X (3)`, …). |
| Empty | No rows after step 1 → `ErrEmptyFile`. |

**Value parsers:**

* `ParseNumber(s) (float64, ok)` — used for **type detection only**. Port `parseNum` exactly:
  1. Strip whitespace, NBSP (U+00A0), narrow NBSP (U+202F) and `'`.
  2. Strip one leading or trailing currency token: `PLN|EUR|USD|GBP|CHF|zł|$|€|£`, case-insensitive.
  3. The result must match `^[-+]?[\d.,]+$` and contain a digit.
  4. Reject more than 15 digits (account and reference numbers).
  5. Reject leading-zero integers like `0229197` (pattern `^[-+]?0\d`).
  6. Decimal separator rules:
     * both `,` and `.` present → the last one is the decimal separator and the other is the thousands separator;
     * only `,` → a single occurrence is decimal, multiple occurrences are thousands;
     * the same for `.`.
* `ParseMinor(s, exp) (int64, error)` — **[CHANGE]** exact decimal parsing for **mapped amount columns**. It applies the same normalisation as above but works on the digit string and never uses a float. Round half away from zero if there are more fraction digits than `exp`. Errors become row-level parse errors (§4.3).
* `ParseDate(s) (Date, ok)`:
  * `yyyy[-./]m[m][-./]d[d]` with an optional `[ T]hh:mm[:ss]` (time ignored for dates);
  * **day-first** `d[d][./-]m[m][./-]yyyy` with an optional time.
  * **[CHANGE]** Reject impossible dates such as 31.02.2025. The legacy `Date.UTC` silently rolled them over. The oracle comparison must skip these cases, which are listed in a test.
* **Month-first dates** (`opts.DateOrder = mdy`; port from `applyMapping`). Values matching `^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(.*)$` are read as month/day in two places:
  * mapped date columns;
  * unmapped columns where ≥ 80 % of the first 200 non-empty values match.

  The default is `dmy`.
* `LooseDate(s) ([lo, hi), ok)` (filters only):
  * a full date → `[d, d+1)`;
  * `yyyy` → that year;
  * `yyyy-mm` or `yyyy.mm` or `yyyy/mm` → that month;
  * `mm.yyyy`, `mm-yyyy`, `mm/yyyy` → that month.

**Column typing (`AnalyzeColumn`):**

* A column is `date` if ≥ 90 % of non-empty values parse as dates.
* Otherwise it is `number` if ≥ 90 % parse as numbers.
* Otherwise it is `text`.
* Dates are checked first.

**Role detection (`DetectRoles`):** used to propose a mapping in the import preview. Port the legacy regexes (case-insensitive) and add the new roles:

| Role | Rule |
|---|---|
| `bookedOn` (req.) | date column matching `ksi[eę]gow\|booking\|posted\|transakcji\|operacji\|transaction`, else the first date column |
| `amount` (req.) | number column matching `finuance\|amount\|warto\|value\|suma\|betrag\|montant`, excluding `saldo\|balance`; else the first number column whose name doesn't match `saldo\|balance` |
| `category` | any column matching `kategor\|category\|categor` |
| `payee` | text column matching `nadawca\|odbiorca\|kontrahent\|payee\|merchant\|counterpart\|recipient\|beneficiar`, else a text column matching `opis\|description\|tytu\|title\|name\|nazwa` |
| `description` | column matching `tytu\|title\|opis\|description\|memo` that is not the payee column |
| `reference` | column matching `referenc\|^ref\b\|transaction id\|id transakcji\|numer transakcji` |
| `currency` | text column matching `^waluta$\|currency`. If it has exactly one distinct value, that value becomes the import currency. |
| `balance` | number column matching `saldo\|balance\|stan konta` (stored only; for future reconciliation) |
| `valueOn` **[new]** | a date column other than `bookedOn`, matching `walut\|value date\|valuta` |
| `txType` **[new]** | column matching `typ operacji\|typ transakcji\|transaction type\|^type$` |
| `counterpartyAccount` **[new]** | column matching `rachunek docelowy\|rachunek (nadawcy\|odbiorcy)\|counterparty account\|iban` |
| `ownAccount` **[new]** | column matching `rachunek [źz]r[oó]d[lł]owy\|own account\|account number\|numer rachunku` |

New heuristics must be validated against the fixtures. Add a fixture per heuristic.

**Legacy role vocabulary (v7).** The legacy import window uses `MAP_ROLES` and renames mapped columns to canonical names (`Date`, `Value date`, `Amount`, …). The legacy roles map to this plan's roles as follows:

| Legacy role | Role here |
|---|---|
| `date` | `bookedOn` |
| `vdate` | `valueOn` |
| `amount` | `amount` |
| `debit`, `credit` | `amountMode=debit_credit` |
| `payee` | `payee` |
| `desc` | `description` |
| `category` | `category` |
| `currency` | `currency` |
| `ref` | `reference` |
| `type` | `txType` |
| `account` | `ownAccount` |
| `balance` | `balance` |
| `ignore` | `ignore` (§4.3) |

`counterpartyAccount` has no legacy role. The legacy `detectRoles` checks **exact canonical names first** (`Date`/date, `Amount`/number, `Category`, `Payee`/text). Keep that rule, so files exported from Finuance re-import cleanly. The auto-detection regexes for `valueOn`, `txType`, `counterpartyAccount` and `ownAccount` remain new.

### 4.3 Mapping → canonical rows (`internal/ledger`)

* An **import mapping** is `{bookedOn, amount?, payee?, description?, reference?, category?, currency?, valueOn?, txType?, counterpartyAccount?, ownAccount?, balance?, ignore: []}`.
  * Columns are referenced by **column index** in the shaped table (`ColRef = {index, header}`), as the legacy `e.cfg.map` does. The header name is kept for display and for profile matching.
  * Options are `{encoding, delimiter, headerRow: auto|none|N, skipRows, dropShort, dateOrder: dmy|mdy, stripQuote, invertSign, amountMode, debitColumn?, creditColumn?, currencyFallback}`.
* `amountMode`:
  * `single` (default);
  * `debit_credit` (legacy v7 parity):
  * amount = |credit| − |debit|;
  * both cells empty → row error;
  * `invertSign` applies afterwards;
  * if a signed `amount` is also mapped, the debit/credit columns stay ordinary raw columns (legacy rule).
* `ignore` columns are dropped from `raw` and from `searchText`, just as the legacy removes them from the table.
* Each data row produces a `StagedRow` with:
  * canonical fields;
  * `raw` = ordered list of `{header, value}` (stored as JSONB array to keep order);
  * `parseError` if a required field fails to parse. Errored rows are shown in the preview and **never committed**.
* **Currency resolution:**
  1. the mapped currency column value;
  2. else the import-level single currency;
  3. else the account currency;
  4. else `PLN`.
* **Search text:** `searchText = norm(join(raw values in column order, "\u0001"))`. This is the legacy `rowNorm`.
* **Shadow normalised columns** for filtering: `payee_n`, `description_n`, `reference_n`, `tx_type_n`, `bank_category_n`, `counterparty_n`, each `collapse(norm(x))`. They are computed in Go so search and filters use exactly one normalisation implementation.
* **`payeeGroup`** = `payeeName(payee)` (§4.1).

### 4.4 Fingerprint and de-duplication (`internal/ledger`)

Fingerprint key (port of legacy `keyOf`):

```
amountStr = decimal string of amountMinor with exponent digits, '.' separator, e.g. "-127.13"
if reference != "":  key = "r|" + norm(reference) + "|" + amountStr
else:                key = bookedOn.ISO() + "|" + amountStr + "|" + collapse(norm(payee)) + "|" + collapse(norm(description))
fingerprint = SHA-256(key)   (32 bytes, stored as bytea)
```

The key deliberately excludes volatile fields such as category, which banks re-label between exports.

**Occurrence:** within one import, in file order, `occurrence` = 1-based running count of rows with the same fingerprint.

**Invariant:** `UNIQUE (account_id, fingerprint, occurrence, split_scope)` on `transactions`. `split_scope` is the nil UUID, except for fingerprints with a *keep both* override (see *Duplicate review* below).

As a result, a transaction is kept `max(count in any single import)` times:

* two genuine identical purchases inside one export stay as two;
* the same purchase appearing in two overlapping exports is stored once.

**[CHANGE]** The legacy app bucketed by file name, so two files with the same name collided. Here buckets are per import, which fixes that bug.

**Commit algorithm** (single DB transaction, run by the `CommitImport` job):

```sql
-- 1. insert new transactions from staged rows (skip errored rows and fingerprints that have a dedupe override;
--    those are handled by step 2b). source_import_id = this import.
INSERT INTO transactions (id, user_id, account_id, fingerprint, occurrence, split_scope, source_import_id, booked_on, value_on,
                          amount_minor, currency, payee, payee_n, payee_group, description, description_n,
                          reference, reference_n, tx_type, tx_type_n, bank_category, bank_category_n,
                          counterparty_account, counterparty_n, category_id, category_source,
                          raw, search_text, origin)
SELECT …                                   -- category_id resolved from bank_category (upsert categories first)
FROM import_rows WHERE import_id = $1 AND parse_error IS NULL
  AND fingerprint NOT IN (SELECT fingerprint FROM dedupe_overrides WHERE account_id = $2)
ORDER BY row_index
ON CONFLICT (account_id, fingerprint, occurrence, split_scope) DO NOTHING
RETURNING id;                                                     -- returned rows = "new"

-- 1b. re-apply user annotations to the returned rows (see *User annotations* below)

-- 2. link every staged row to its transaction (new or pre-existing): provenance
UPDATE import_rows ir SET transaction_id = t.id
FROM transactions t
WHERE ir.import_id = $1 AND ir.parse_error IS NULL
  AND t.account_id = $2 AND t.fingerprint = ir.fingerprint AND t.occurrence = ir.occurrence
  AND t.split_scope = ir.split_scope;

-- 2b. fingerprints of this import that have a dedupe override → Rematerialize (see *Duplicate review* below)
-- 3. stats: new = rows returned by (1) + rows inserted by (2b); duplicates = linked rows − new; errors = rows with parse_error
-- 4. imports.status = 'committed'; enqueue follow-up jobs (rules, transfers) with river InsertTx
```

**Delete import:**

1. Delete the `import_rows` of that import.
2. Delete transactions where `origin = 'import'` and no `import_rows` reference them any more.
3. Re-materialise the fingerprints whose `source_import_id` was this import and that still have rows, so their values come from the remaining sources (*Duplicate review* below).

Effect: deleting an overlapping export never deletes transactions that another import also contains. This is a test case, not an assumption.

**Reprocess import** (mapping or options changed after commit):

1. Re-parse the stored bytes.
2. Restage the rows.
3. Run the same delete-orphans procedure, then the commit.

Manual edits on transactions (category, note) survive whenever the fingerprint is unchanged.

**User annotations keyed by identity (rev 2).** The legacy app keys group labels by a content key (`txKeys`: the dedupe recipe plus a `#n` twin suffix), so labels survive re-imports. The equivalent here is `tx_annotations`, keyed by `(account_id, fingerprint, occurrence, split_scope)`; `occurrence` plays the role of the `#n` suffix.

* Every user edit is written to **both** the transaction row and its annotation, in one DB transaction. User edits are: group, manual category, note, and the manual transfer flag (Phase 15).
* **Commit step 1b** re-applies annotations to the rows inserted in step 1:

  ```sql
  UPDATE transactions t
  SET group_id = a.group_id,
      category_id = coalesce(a.category_id, t.category_id),
      category_source = CASE WHEN a.category_id IS NOT NULL THEN 'manual' ELSE t.category_source END,
      note = a.note,
      note_n = a.note_n
  FROM tx_annotations a
  WHERE t.id = ANY($inserted_ids)
    AND (a.account_id, a.fingerprint, a.occurrence, a.split_scope) = (t.account_id, t.fingerprint, t.occurrence, t.split_scope);
  ```
* Annotations are deleted only by:
  * deleting the **account** (cascade);
  * an explicit "clear" (label removed, category reset, note emptied). Fully empty annotations are deleted.

  Deleting or reprocessing an import never deletes them.
* Test: label a transaction, delete its only import, re-import the same file → the label is back.

**Duplicate review and overrides (rev 3, legacy §32.3).**

* **Value source.** A transaction's field values come from one staged row, its **source** (`source_import_id`). By default the source is the row of the earliest-committed import that contains that `(fingerprint, occurrence)`.
  * Fingerprint fields are equal across sources by definition.
  * Other fields can differ between exports: bank category, operation type, balance, raw values.
* **Duplicate group:** an `(account_id, fingerprint)` with rows from ≥ 2 committed imports, where at least one row was linked to an existing transaction (i.e. skipped), or which has an override.
* **"N duplicates skipped"** for a set of accounts = linked import rows − import-origin transactions, ignoring filters.
* **Overrides** (`dedupe_overrides`, at most one per `(account_id, fingerprint)`):

  | Override | Legacy name | Effect | Count |
  |---|---|---|---|
  | `prefer` + `preferred_import_id` | *Use this version* / `dupPick` | That import is the source for every occurrence it contains | Unchanged (max rule) |
  | `keep_both` | *Keep both* / `dupKeep` | Never merged: each import's rows get their own `split_scope`, so they become separate transactions | Sum over imports |
  | no override | *Merge again* | The default rule | Max rule |

  For `keep_both`, `split_scope` = a UUID built from the first 16 bytes of that import file's SHA-256. It stays the same when the identical file is re-imported, so annotations survive.
* **`Rematerialize(accountID, fingerprint)`** is the reference implementation. It runs in one DB transaction:
  1. Load the override and all non-errored rows of that fingerprint in the account (committed imports only).
  2. Compute the desired set `{(occurrence, split_scope) → source row}` with the rules above.
  3. Delete transactions of that fingerprint that are no longer desired. Annotations stay.
  4. Insert the missing ones. For kept ones whose source changed, update the field values and `source_import_id`.
  5. Relink `import_rows.transaction_id` (setting their `split_scope`), then re-apply annotations (step 1b).
* **Callers:**
  * The commit job uses the set-based SQL above for fingerprints without an override, and `Rematerialize` for the rest (step 2b).
  * Delete-import calls it for fingerprints whose source was the deleted import and that still have rows.
  * Setting or clearing an override enqueues `RematerializeFingerprint`.
  * Deleting the preferred import removes its override (FK cascade). The delete path then re-materialises that fingerprint.
* **Property test (required):** for random sequences of import / delete / reprocess / override set and clear over generated overlapping files, the incrementally maintained `transactions` must always equal what `Rematerialize` produces from scratch for every fingerprint.
* **[CHANGE] Duplicates are detected per account.** The legacy merges duplicates across **all** files in *All data*. Here, the same purchase in exports of two different accounts stays as two transactions, which is usually correct (e.g. a transfer between your own accounts). Exports of one bank account belong in one account.

**Exact duplicate file:** if the same user uploads a file with an identical SHA-256 to a non-failed import, the server returns `409` with the existing import id.

**Import profiles:**

* Signature: `headerSignature = SHA-256(join(norm(headers), "\u0001") + "|" + delimiter)`.
* On commit with "remember mapping" (default on), upsert `import_profiles(user_id, header_signature) → mapping, options`.
* On preview, a matching profile is applied automatically, and the preview reports `mappingSource: "profile"` instead of `"detected"`.

**Account suggestion:**

* If `ownAccount` is mapped, take the most common digit string in that column.
* Match its last 4 digits against `accounts.number_hint`.
* Suggest the matching account, or suggest creating "Account ••1234".

### 4.5 Query context and filter language (`internal/query`)

`QueryContext` (shared by the table, insights, overview, export, distinct values):

```jsonc
{
  "accountIds": ["…"],            // empty = all accounts of the user
  "search": "platnosc blik",      // all tokens must appear in search_text (AND)
  "filters": [                    // AND across columns
    {"column": "amount", "cond": "<= -100", "excluded": []},
    {"column": "category", "cond": "", "excluded": ["Bez kategorii"]}
  ],
  "period": {"preset": "l3m"} | {"month": "2025-03"} | {"year": 2025} | {"from": "2025-01-01", "to": "2025-02-01"} | null,
  "includeTransfers": false,      // Phase 15; default false once transfer detection exists
  "toolFilter": null              // assistant lookup filter (§4.8), shown as a removable "✦ …" chip
}
```

**Filterable columns** (stable API names → SQL expression, type):

| Column | SQL (value / normalised) | Type |
|---|---|---|
| `bookedOn` | `t.booked_on` | date |
| `valueOn` | `t.value_on` | date |
| `amount` | `t.amount_minor` | number |
| `balanceAfter` | `t.balance_after_minor` | number |
| `currency` | `t.currency` / `lower(t.currency)` | text |
| `payee` | `t.payee` / `t.payee_n` | text |
| `description` | `t.description` / `t.description_n` | text |
| `category` | `c.name` / `c.name_n` (LEFT JOIN categories) | text |
| `bankCategory` | `t.bank_category` / `t.bank_category_n` | text |
| `txType` | `t.tx_type` / `t.tx_type_n` | text |
| `reference` | `t.reference` / `t.reference_n` | text |
| `counterpartyAccount` | `t.counterparty_account` / `t.counterparty_n` | text |
| `account` | `a.name` / `a.name_n` | text |
| `group` | `g.name` / `g.name_n` (LEFT JOIN groups) | text |
| `note` | `t.note` / `t.note_n` | text |

**Condition grammar** (port of legacy `compileCond`):

```ebnf
cond      = [ "!" ] body ;                       (* "!" negates the whole condition *)
body      = range | op_expr | bare ;
range     = value ".." value ;                   (* whitespace allowed around ".." *)
op_expr   = ( ">=" | "<=" | ">" | "<" | "=" ) [ value ] ;
bare      = value ;
```

**Semantics by column type:**

* **number:**
  * `a..b` → `min ≤ x ≤ max` (inclusive);
  * `op v` with a non-empty v → comparison.
  * Operands use the `ParseNumber` rules and are converted to minor units.
* **date:**
  * operands use `LooseDate` intervals `[lo, hi)`;
  * `>` → `x ≥ hi`; `>=` → `x ≥ lo`; `<` → `x < lo`; `<=` → `x < hi`; `=` or bare → `lo ≤ x < hi`;
  * `a..b` → `min(lo) ≤ x < max(hi)`.
* **Text fallback**, also used when a number/date operand does not parse:
  * `=v` → normalised equality;
  * `=` alone → empty value;
  * `!=` → not empty (that is, `!` + `=`);
  * anything else → normalised `LIKE '%v%'`.
  * Escape `%`, `_` and `\` in user input.
* **`excluded`:** raw display values unticked in the value checklist → `coalesce(expr, '') NOT IN (…)`.
* **Search:** `tokens = split(norm(search))`. Each token adds `t.search_text LIKE '%' || $n || '%'`, served by a trigram GIN index.

**Security rule:**

* The compiler outputs `(sqlFragment string, args []any)`.
* The fragment may only contain column expressions from the whitelist above and `$n` placeholders.
* User input is **never** concatenated into SQL.
* Fuzz-test the parser, and test that hostile inputs (`'; DROP TABLE …`, `%`, `\`) only ever become arguments.

**Period resolution** (port of `resolvePeriod` / `prevRange`):

* The anchor is `max(booked_on)` over the context's accounts, **ignoring filters**. Statements are historical, so presets anchor to the latest data rather than to today.
* With `end = max + 1 day` and `ms = first day of max's month`:

| Preset | Interval |
|---|---|
| `l30` | `[end − 30 d, end)` |
| `l3m` | `[ms − 2 mo, ms + 1 mo)` |
| `l12m` | `[ms − 11 mo, ms + 1 mo)` |
| `ytd` | `[Jan 1 of max's year, end)` |
| `month` | that calendar month |
| `year` | that calendar year |
| `custom` | `[from, to)` |

* The period applies on `booked_on` **after** filters. The server always computes two row sets:
  * `base` = filters and search, no period;
  * `scoped` = base ∩ period.

  Comparisons (previous period, cash-flow context, pace) use `base`. This is the legacy `S.base` vs `S.order`.
* `prevRange`:
  * month → the previous month;
  * year → the previous year;
  * range → an equal-length range immediately before.

**Sorting:**

* Any filterable column, `asc`/`desc`.
* Text uses the ICU collation `pl_natural` (`CREATE COLLATION pl_natural (provider = icu, locale = 'pl-u-kn-true')`), which gives Polish order with numeric-aware comparison and replaces the legacy `Intl.Collator(numeric)`.
* Empty values are always last (`NULLIF(expr,'') … NULLS LAST`).
* Tie-break with `t.booked_on DESC, t.seq ASC`.
* Default sort (none selected): `booked_on DESC, seq ASC`. `seq` is a `bigint identity` and preserves file order within a day.

### 4.6 Insights (`internal/insights`)

All functions take `(ctx, userID, QueryContext, params)` and use the compiled `base`/`scoped` CTEs. "Expenses" means `amount < 0` and the spend value is `−amount`.

| Key | Output | Rules (port from legacy `CHARTS.*`; see legacy §14) |
|---|---|---|
| `summary` | spent, income, net, savingsRate, days, avgPerDay, largestExpense {id, payee, amount, date}, spentDelta vs prevRange | `days` = period length clipped to the data range of `base`; delta uses `base` within `prevRange` |
| `categories` | `[{category, spentMinor, share, colorSlot}]` | Expenses in `scoped`. Missing category → `(none)`. The client shows top 7 + Other (donut) or all (bars). |
| `cashflow` | `[{month, incomeMinor, expenseMinor, inPeriod}]` | Over `base`; months range from the `flowMonths` param (`fit`=client-decided count, 6, 12, 24, 36, all) |
| `pace` | target month, comparison label, `current[31]` cumulative, `compare[31]`, lastDataDay | Target = the period's month, else the latest month. Comparison = `prev` / `yoy` / `avg3` / `avg6` / `none`. Uses `base`. |
| `payees` | `[{payee, spentMinor, count}]` | Grouped by `payee_group`; `top` = 10/25/50/100/all; `scope` = period (`scoped`) or all-time (`base`) |
| `daily` | `[{date, spentMinor}]` | Range = follow period / last 3, 6, 12 months / all. The client computes quintile bins and the calendar layout. |

**Overview** (`GET`-like composite, one request per screen). It respects filters and search but **not** the period. Selected month `M` defaults to the latest month with data. Port legacy `ovCtx`, `renderOverview` and `CHARTS.o_*`:

* `months[]`: per month `{month, incomeMinor, expenseMinor, count}` over `base`.
* `M`, `P` (previous), `cur`, `prev`, `prevToDate` (P's spend up to the same day-of-month), `partial` (M is the latest data month and the month is not complete), `elapsed`, `daysInMonth`.
* `categoryStats`: per category `{curMinor, prevMinor, avg3Minor}`, where `avg3` = average over up to 3 months before M.
* `incomeSources`: income by `payee_group` for M.
* `kpis`: income, spending, net + rate, avgPerDay or **projected** (partial: `spent / elapsed × daysInMonth`), each with Δ vs the previous month and a 12-point sparkline (length = `ovMonths`).
* `highlights[]`: `{kind, tone: good|warn|crit|info, params}`. Port every sentence rule from legacy `o_high`:
  * projection;
  * vs last month (same-day comparison when partial);
  * vs 3-month average;
  * biggest category share;
  * largest category increase/decrease vs usual (keep the noise thresholds);
  * savings rate vs N-month average or overspend;
  * no-spend days;
  * recurring total;
  * largest expense.

  The **client** formats the text, so wording lives in one place.
* `budgets[]`: per category `{spentMinor, budgetMinor, isDefault, paceRatio, status}`.
  * Default budget = `roundNice(avg3)`, where `roundNice(v) = v ≤ 0 ? 0 : v < 100 ? ceil(v/5)*5 : round(v/10)*10` in **major** units.
  * Overrides come from the `budgets` table.
  * `status`: `ok` (on track), `ahead` (spent > budget × elapsed/dim), `over` (spent > budget), `none` (no budget).
* `recurring[]`: port `findRecurring` exactly, computed on request over `base` expenses (cheap at personal scale, and it respects filters as the legacy did):
  * group by `payeeName`;
  * require ≥ 3 distinct months;
  * require ≤ 1.5 charges/month;
  * require ≥ 75 % of amounts within ±30 % of the median;
  * require coverage (distinct months / month span) ≥ 0.6;
  * `active` if the last charge is within 45 days of the latest data date;
  * `next` = same day next month, clamped to the month end;
  * `yearly = median × 12`;
  * category = the most frequent;
  * sort: active first, then yearly desc.
* `moneyFlow`: nodes/links for the Sankey diagram:
  * income sources (top 6 + Other) → `Cash` → categories (top 8 + Other);
  * plus `Saved` (if net > 0) or a `From savings` source (if net < 0).
* `largestExpenses`: top 12 for M (40 when `big=true`).
* `savingsRate[]`: per month over the last `ovMonths`, `(inc − exp)/inc` clamped to ±100 %, plus the average.
* `weekday[]`: average spend per weekday over the 3 months ending at M. Port the legacy denominator exactly.
* `balance[]`: end-of-day running balance over the last `ovMonths`.
  * Computed as sum of `opening_balance_minor` of the context's accounts + cumulative sum of **all** their transactions.
  * **Ignores filters and search** (legacy rule: balance must equal the sum of transactions).
* `colors`: `{categoryName: slot 1..7 | null}`. Rank categories by **all-time** expense across the context's accounts, ignoring filters and period. Ranks 1–7 get slots and the rest get `null` (grey "Other"). Colour follows the entity, not the current rank. **The same mapping is returned by `GET /categories?accountIds=` for the table/insights views.**
* `investment` (legacy `o_inv`), from the manual data (§4.9):
  * PPK: paid in (employee + employer + state), gain (value − paid in), return on your own money;
  * positions: invested, value, annual income;
  * allocation shares by value.
* `yieldOnCost` (legacy `o_yoc`): per position `annualIncome / invested`, plus the highlighted total. PPK is excluded because it doesn't pay out. If no position has annual income, use the dividends/interest found in `base` income transactions of the 12 months up to M (matching `dywidend|dividend|odsetk|interest|kupon|coupon` in `search_text`), and flag that source.
* `fire` (legacy `o_fire`):
  * annual expenses = the `base` expenses of the 12 months up to M, annualised when fewer months exist;
  * FI number = annual expenses × 100 / SWR % (25× at the default 4 %);
  * progress = invested value ÷ FI number;
  * years to FI by yearly compounding at the expected real return, plus the current yearly savings (income − expenses). `null` means not reachable within 100 years.

  **Port all three cards' formulas exactly from the legacy cards**, including which values count as "invested value". Each card also sets a table twin.

### 4.7 Transaction groups and multi-selection (`internal/ledger`, legacy §30)

**Groups:**
* A **group** is a user-defined, colour-coded label, e.g. "Italy trip 2025" or "Wedding".
  * The name is unique per user (normalised) and ≤ 40 characters.
  * Colour slot 0–7 = `--s1…--s7` + `--g8` (port `GCOLS`). A new group takes the first unused slot (port `nextColor`).
* **One group per transaction** (legacy parity: it behaves like a column). It is stored in `transactions.group_id` plus the annotation (§4.4). A many-to-many "tags" model is in the backlog.
* It is exposed as the filterable text column `group` (`=Italy trip 2025`, contains, the value checklist). As in the legacy, chips, charts, the overview, the status-bar Σ and export all respect it with no special code.
* **Group stats** per group, over the user's accounts and ignoring filters: `{id, name, color, count, spentMinor, incomeMinor, firstOn, lastOn}`.
* **Rename:** the client renames any view filters that reference the old name (port `renameGroupFilters`).
* **Delete:** removes the group and its labels only, never transactions.

**Selection:** a client concept (§7.7), namely a set of transaction ids.
* Bulk operations send ids (max 5,000 per request).
* A range selection across unloaded grid windows resolves ids and amounts via `POST /transactions/ids`.

### 4.8 AI assistant (`internal/assistant`, `internal/llm`, legacy §29)

**Principle (port as-is): retrieval by tools, not by context dump.** The model gets a compact system prompt and three **read-only** tools. The tools run **on the server under `WithUserReadOnly` (RLS)** over the requesting user's data, and the model receives only what it asked for. A typical request stays at 1.5–4k tokens regardless of the dataset size.

**System prompt** (port `sysPrompt`; generated by the server per request):
* today's date and weekday in the user's time zone (prefs; default `Europe/Warsaw`);
* the relative-date rules: weeks start on Monday; "last week" = the previous Mon–Sun; "this month" = the 1st to today; relative dates are always converted to explicit ISO ranges before a tool call;
* scope (all accounts, or the current view's QueryContext), row count, date range and latest date, currency;
* the canonical column list with types and one masked example value each (digit runs ≥ 6 → first 2 digits + `…`);
* category vocabulary with counts (≤ 60), the most frequent payee groups (≤ 40), group names;
* the legacy RULES block verbatim:
  * sign convention;
  * rows are transactions, not items;
  * which tool to use when;
  * **every number must come from a tool result**;
  * say so when the period is outside the data;
  * answer in the user's language and state the date range used.
* the user data is framed as data, never as instructions.

**Tools** (JSON-schema definitions ported from `AI_TOOLS`/`FILTERS`):

| Tool | Arguments | Result |
|---|---|---|
| `search_transactions` | filters + `sort` (`date_desc`, `date_asc`, `amount_desc`, `amount_asc`; amounts compared by absolute value) + `limit` (default 25, capped by the user's `maxRows`) | `{currency, matched, returned, truncated, spent, income, net, rows[]}`, rows = `{id, date, amount, payee, description, category, group, account}` |
| `aggregate_transactions` | `group_by` ∈ `none`, `category`, `payee`, `month`, `week` (Mon), `day`, `weekday`, `type`, `group`, **`account`** [new] + filters + `top` | `{currency, group_by, matched, spent, income, net, groups[{key, count, spent, income, net}], omitted_groups}`. Time keys are sorted ascending and the last `top` kept (default 60); other keys are sorted by volume and the first `top` kept (default 20, max 120). |
| `find_values` | `field` ∈ `payee`, `category`, **`group`** [new]; `query` | `{field, query, values[{value, count}]}` (≤ 40) |

**Filters** (`ToolFilter`, typed). They are compiled by `internal/query` next to the normal QueryContext, never via strings:
* `query`: all tokens in `search_text`;
* `payee`: contains, normalised, on `payee_n`;
* `category`, `group`: contains;
* `date_from`/`date_to`: inclusive ISO dates;
* `type`: expense, income or any;
* `min_amount`/`max_amount`: **absolute** values in major units, converted to minor.

Port `aiFilter` exactly, with differential tests against the oracle (Phase 1).

**Run loop** (port `runTools`, `runPlan`, `complete`, `trimHistory`):

* **Tools mode:**
  1. Send the request with `tools`.
  2. Execute every tool call.
  3. Append the `tool` messages.
  4. Repeat until the model answers; after max 6 rounds, it answers with what it has.
* **Plan mode**, for models without tool calling:
  1. Ask for `{"calls":[…]}` JSON only (port `PLAN()`).
  2. Execute the calls.
  3. Make a second request with the results to get the answer.
* **Auto mode:** start in tools mode. If the server rejects `tools` (HTTP 400/422/500 before any step), switch this chat to plan mode (`noTools`) and emit an SSE `mode` event.
* **Tolerant parsing:**
  * assemble streamed `tool_calls` fragments by index;
  * accept `reasoning_content` and `reasoning`;
  * split `<think>…</think>` into reasoning;
  * parse raw Qwen `<tool_call>{…}</tool_call>` text as a fallback;
  * also accept non-streaming JSON responses.
* **Thinking off:** append `/no_think` to the last user message (Qwen 3; harmless for other models).
* **History:**
  * keep the last ≤ 30 messages, starting at a user turn;
  * truncate older tool results to 2.5k chars;
  * plan mode strips tool messages.
* **Caps:** tool JSON stored for the UI ≤ 6k chars; in history ≤ 4k; reasoning ≤ 4k.

**Transparency:**
* Every lookup is persisted as a **step** `{tool, args, summary, resultPreview, matched, toolFilter}`. The UI shows exactly what was sent back to the model.
* Every answer has meta `{durationMs, approxTokensSent, lookups, model, mode}`.

**"Show in table" [CHANGE]:** the legacy app applied the step's matched row-id set. Here the step stores its `toolFilter`, and "Show in table" applies it as a removable "✦ <summary>" chip (`QueryContext.toolFilter`). This works because a query survives reloads and new imports, so restored chats keep working; the legacy dropped those links.

**Persistence:**
* Chats are stored in Postgres (§5.2). Each user keeps the last `FINUANCE_AI_MAX_CHATS` (default 10) unless "keep chats" is off. In that case nothing is stored and existing chats are deleted.
* Chats are included in `GET /me/export` and deleted with the user.

**Safety:**
* **No write access.** Tools are read-only, so the model never changes data. Write actions are a Phase 15 stretch that requires explicit user confirmation.
* **Prompt injection.** Transaction text is untrusted input (e.g. a payee named "ignore previous instructions"). The blast radius is limited by:
  * read-only tools scoped by RLS to the requesting user;
  * tools that cannot reach other users or the network;
  * answer numbers that can be traced to visible steps.
* **SSRF.** The endpoint is instance config only, never user input. Startup checks for a private address unless `FINUANCE_AI_ALLOW_REMOTE` is set. A NetworkPolicy limits egress to that host.
* **Limits:**
  * a per-user rate limit (default 30 messages/hour);
  * one in-flight run per user;
  * `FINUANCE_AI_MAX_CONCURRENT` per replica;
  * request timeouts;
  * stop = cancel the upstream request.

### 4.9 Investments (manual data; `internal/investments`, legacy §32.4)

* **One document per user:**
  * PPK `{employee, employer, state, value}`;
  * ordered positions `[{id, name, type, invested, value, annualIncome}]`;
  * `swr` (default 4 %) and `realReturn` (default 5 %).
* **Units:** amounts are PLN minor units (the base currency); percentages are stored as basis points.
* **PPK** = *Pracownicze Plany Kapitałowe*, the Polish employee capital plan. It doesn't pay out income, so it is excluded from yield on cost.
* **Validation:**
  * amounts ≥ 0;
  * name 1–80 characters;
  * type ≤ 30 characters (free text with suggestions: ETF, stock, bond, deposit, crypto, other);
  * SWR 0.5–10 %;
  * real return −10 % to 20 %.
* **API:** `GET /investments` returns the document with a `version`. `PUT /investments` replaces it with optimistic concurrency, like preferences.
* **Editing** happens only in the large (modal) view of the cards. The small cards are read-only.
* **Calculations** (`investment`, `yieldOnCost`, `fire`) are part of the overview payload (§4.6). `float64` is allowed for these ratios and projections (§8.4).

### 4.10 Legacy import — migrating a legacy backup (`internal/legacyimport`; legacy §17.5, §32.1)

A one-time import of a legacy backup into the current user. Supported files:
* `finuance-backup-*.json` (key `finuanceBackup`);
* old `csvInspectorBackup` files.

The goal: our Product Engineer's real data, labels and settings move over without re-doing any work.

**In the browser:**

1. Parse the JSON (port `deser`: base64 `ArrayBuffer`s and typed arrays).
2. If `kv.lock` exists, ask for the passphrase. Derive the key exactly as the legacy does: PBKDF2-SHA-256, 310,000 iterations, the stored salt → AES-256-GCM.
   * Verify the passphrase by decrypting `check`; accept both constants, `'finuance'` and `'csv-inspector'`.
   * Decrypt every `files[].data` and `kv.private` (and `kv.chats` if encrypted).
   * **The passphrase and the ciphertext never leave the browser.**
3. Show a wizard:
   * each legacy file with its name, size, date range and suggested account (by own-account number hint, else by import profile `b1`, `b2`, …);
   * the user creates or picks the accounts;
   * choose whether to include chats, and which account receives the legacy opening balance.
4. Upload with `POST /legacy-imports` as multipart:
   * a `manifest.json` with the decrypted private data, a prefs subset, and per file `{legacyId, name, cfg, accountId | newAccountName}`;
   * one part per file (raw bytes).

**On the server** (job `LegacyImport`; status via `GET /legacy-imports/{id}`):

1. **Accounts and files.** Create the accounts. Run every file through the normal import pipeline and commit it.
   * Options and mapping are translated from the legacy `e.cfg` and the global parse options: the role map by column index (§4.3 role table), `header`, `skip`, `dropShort`, `dateOrder`, `invert`, `enc`, `delim`, `strip`.
   * Already-imported identical files are reused, so re-running the migration is idempotent.
2. **Groups.** Create the groups (name, colour). Translate each tag key (`txKeys` recipe) to the new identity:
   * `r|…` keys map directly;
   * date keys: convert epoch ms → ISO date;
   * the `#n` suffix → `occurrence`;
   * then compute the fingerprint, find the transaction in the migrated accounts and write the annotation.

   Keys from the "normalised row" branch (files without date/amount) can't be mapped and are reported.
3. **Everything else:**
   * budgets (by category name);
   * the opening balance (to the chosen account);
   * `dupPick` / `dupKeep` → `dedupe_overrides` (key → fingerprint as above; legacy file id → import id);
   * `S.inv` → investments;
   * chats → assistant chats (optional).
4. **Preferences:** theme, interface size, pane height, cards, `cardOrder`, chart cfg, sidebar state and AI panel settings. Never the AI address or key.
5. **Report:**
   * files imported, reused or failed;
   * transactions created and duplicates skipped;
   * labels and overrides matched/unmatched, with reasons;
   * unknown backup keys (ignored, never fatal).

Limitation: legacy keys were computed with the legacy **auto-detected** roles (`ds.roles`), while fingerprints use the mapped roles. Where these differ, a label can't be matched; the report lists those cases.

---

## 5. Data model

### 5.1 Roles and bootstrap (`deploy/db/bootstrap.sql`, run once as superuser; **not** a goose migration)

```sql
-- passwords injected by the runner (psql -v) or by CloudNativePG managed roles
CREATE ROLE finuance_owner LOGIN PASSWORD :'owner_password';   -- owns schema, runs migrations
CREATE ROLE finuance_app   LOGIN PASSWORD :'app_password';     -- used by api + worker; subject to RLS
CREATE DATABASE finuance OWNER finuance_owner;
\connect finuance
ALTER SCHEMA public OWNER TO finuance_owner;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
GRANT USAGE ON SCHEMA public TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO finuance_app;
```

* `finuance_app` is never the table owner, so RLS always applies to it.
* `finuance migrate up` connects as `finuance_owner`. It runs goose migrations, then River's migrations (`rivermigrate`), in that order, inside its own process.

### 5.2 Core schema (Phase 4 migration `00001_core.sql`, unless noted)

```sql
CREATE COLLATION IF NOT EXISTS pl_natural (provider = icu, locale = 'pl-u-kn-true');

CREATE TABLE users (
  id             uuid PRIMARY KEY,
  email          citext NOT NULL UNIQUE,
  password_hash  text NOT NULL,                       -- PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$hash
  display_name   text NOT NULL DEFAULT '',
  is_demo        boolean NOT NULL DEFAULT false,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_login_at  timestamptz
);

CREATE TABLE sessions (
  token_hash    bytea PRIMARY KEY,                     -- SHA-256 of the random cookie token; raw token never stored
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  user_agent    text NOT NULL DEFAULT '',
  ip            inet
);
CREATE INDEX sessions_user_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE user_preferences (
  user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  prefs       jsonb NOT NULL DEFAULT '{}'::jsonb,       -- theme, uiScale, paneHeight, cards, chart cfg, cardOrder, AI panel, per-view state (§7.4)
  version     integer NOT NULL DEFAULT 1,               -- optimistic concurrency
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
  id                     uuid PRIMARY KEY,
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                   text NOT NULL,
  name_n                 text NOT NULL,
  currency               char(3) NOT NULL DEFAULT 'PLN',
  number_hint            text,                          -- last 4 digits only; never the full account number
  opening_balance_minor  bigint NOT NULL DEFAULT 0,
  is_demo                boolean NOT NULL DEFAULT false,
  archived_at            timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE categories (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text NOT NULL,
  name_n      text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE groups (                                   -- user-defined transaction groups (§4.7)
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
  name_n      text NOT NULL,
  color       smallint NOT NULL CHECK (color BETWEEN 0 AND 7),
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, name_n)
);

CREATE TABLE imports (
  id              uuid PRIMARY KEY,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id      uuid REFERENCES accounts(id) ON DELETE CASCADE,   -- NULL until chosen in preview
  filename        text NOT NULL,
  size_bytes      integer NOT NULL,
  sha256          bytea NOT NULL,
  status          text NOT NULL CHECK (status IN ('previewed','committing','committed','failed')),
  encoding        text NOT NULL,
  delimiter       text NOT NULL,
  headers         jsonb NOT NULL,                       -- ["Data księgowania", …]
  column_types    jsonb NOT NULL,                       -- ["date","date","text",…]
  mapping         jsonb NOT NULL,                       -- §4.3
  options         jsonb NOT NULL,                       -- §4.3
  mapping_source  text NOT NULL CHECK (mapping_source IN ('detected','profile','user')),
  header_signature bytea NOT NULL,                     -- links to import_profiles; drives the sidebar source labels b1, b2, …
  stats           jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {rows, errors, new, duplicates, headerAt, dropped}
  error           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  committed_at    timestamptz
);
CREATE UNIQUE INDEX imports_user_sha_uniq ON imports (user_id, sha256) WHERE status <> 'failed';
CREATE INDEX imports_user_created_idx ON imports (user_id, created_at DESC);
CREATE INDEX imports_account_idx      ON imports (account_id);

CREATE TABLE import_files (                             -- raw bytes kept for lossless re-processing
  import_id  uuid PRIMARY KEY REFERENCES imports(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content    bytea NOT NULL
);

CREATE TABLE transactions (
  id                    uuid PRIMARY KEY,
  seq                   bigint GENERATED ALWAYS AS IDENTITY,
  user_id               uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id            uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  origin                text NOT NULL DEFAULT 'import' CHECK (origin IN ('import','manual')),
  fingerprint           bytea NOT NULL,
  occurrence            integer NOT NULL CHECK (occurrence >= 1),
  split_scope           uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',  -- keep-both overrides (§4.4)
  source_import_id      uuid REFERENCES imports(id) ON DELETE SET NULL,            -- where the field values come from
  booked_on             date NOT NULL,
  value_on              date,
  amount_minor          bigint NOT NULL,
  currency              char(3) NOT NULL,
  payee                 text NOT NULL DEFAULT '',
  payee_n               text NOT NULL DEFAULT '',
  payee_group           text NOT NULL DEFAULT '(unknown)',
  description           text NOT NULL DEFAULT '',
  description_n         text NOT NULL DEFAULT '',
  reference             text NOT NULL DEFAULT '',
  reference_n           text NOT NULL DEFAULT '',
  tx_type               text NOT NULL DEFAULT '',
  tx_type_n             text NOT NULL DEFAULT '',
  bank_category         text NOT NULL DEFAULT '',
  bank_category_n       text NOT NULL DEFAULT '',
  counterparty_account  text NOT NULL DEFAULT '',
  counterparty_n        text NOT NULL DEFAULT '',
  balance_after_minor   bigint,
  category_id           uuid REFERENCES categories(id) ON DELETE SET NULL,
  category_source       text NOT NULL DEFAULT 'none' CHECK (category_source IN ('none','bank','rule','manual')),
  group_id              uuid REFERENCES groups(id) ON DELETE SET NULL,  -- §4.7; mirrored in tx_annotations
  is_transfer           boolean NOT NULL DEFAULT false,               -- Phase 15
  transfer_peer_id      uuid REFERENCES transactions(id) ON DELETE SET NULL,
  note                  text NOT NULL DEFAULT '',
  note_n                text NOT NULL DEFAULT '',
  raw                   jsonb NOT NULL DEFAULT '[]'::jsonb,           -- [{"h":"Finuance operacji","v":"-44,98"}, …]
  search_text           text NOT NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT transactions_dedupe UNIQUE (account_id, fingerprint, occurrence, split_scope)
);
CREATE INDEX tx_user_booked_idx   ON transactions (user_id, booked_on DESC, seq);
CREATE INDEX tx_account_booked_idx ON transactions (account_id, booked_on);
CREATE INDEX tx_user_category_idx ON transactions (user_id, category_id);
CREATE INDEX tx_user_payee_idx    ON transactions (user_id, payee_group);
CREATE INDEX tx_user_group_idx    ON transactions (user_id, group_id);
CREATE INDEX tx_source_import_idx ON transactions (source_import_id);
CREATE INDEX tx_search_trgm_idx   ON transactions USING gin (search_text gin_trgm_ops);

CREATE TABLE import_rows (                              -- staging AND provenance
  import_id       uuid NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
  row_index       integer NOT NULL,                     -- 0-based data row index in the file
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  -- same canonical columns as transactions, all NULLable (errored rows may lack them):
  fingerprint bytea, occurrence integer, split_scope uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000', booked_on date, value_on date, amount_minor bigint, currency char(3),
  payee text, payee_n text, payee_group text, description text, description_n text,
  reference text, reference_n text, tx_type text, tx_type_n text, bank_category text, bank_category_n text,
  counterparty_account text, counterparty_n text, balance_after_minor bigint,
  raw             jsonb NOT NULL,
  search_text     text,
  parse_error     text,
  transaction_id  uuid REFERENCES transactions(id) ON DELETE SET NULL,
  PRIMARY KEY (import_id, row_index)
);
CREATE INDEX import_rows_tx_idx ON import_rows (transaction_id);

CREATE TABLE import_profiles (
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  header_signature  bytea NOT NULL,
  name              text NOT NULL DEFAULT '',
  mapping           jsonb NOT NULL,
  options           jsonb NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, header_signature)
);

CREATE TABLE budgets (                                  -- global per category (legacy behaviour)
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id   uuid NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, category_id)
);


CREATE TABLE tx_annotations (                           -- user edits keyed by dedupe identity (§4.4)
  account_id         uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  fingerprint        bytea NOT NULL,
  occurrence         integer NOT NULL,
  split_scope        uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id           uuid REFERENCES groups(id) ON DELETE SET NULL,
  category_id        uuid REFERENCES categories(id) ON DELETE SET NULL,   -- manual categories only
  note               text NOT NULL DEFAULT '',
  note_n             text NOT NULL DEFAULT '',
  transfer_override  boolean,                                             -- Phase 15: manual pair/unpair
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, fingerprint, occurrence, split_scope)
);

CREATE TABLE dedupe_overrides (                         -- duplicate review (§4.4, legacy §32.3)
  account_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  fingerprint          bytea NOT NULL,
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode                 text NOT NULL CHECK (mode IN ('prefer','keep_both')),
  preferred_import_id  uuid REFERENCES imports(id) ON DELETE CASCADE,   -- deleting it removes the override
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, fingerprint),
  CHECK ((mode = 'prefer') = (preferred_import_id IS NOT NULL))
);

CREATE TABLE investment_profile (                       -- manual investment data (§4.9, legacy §32.4)
  user_id             uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  ppk_employee_minor  bigint NOT NULL DEFAULT 0 CHECK (ppk_employee_minor >= 0),
  ppk_employer_minor  bigint NOT NULL DEFAULT 0 CHECK (ppk_employer_minor >= 0),
  ppk_state_minor     bigint NOT NULL DEFAULT 0 CHECK (ppk_state_minor >= 0),
  ppk_value_minor     bigint NOT NULL DEFAULT 0 CHECK (ppk_value_minor >= 0),
  swr_bp              integer NOT NULL DEFAULT 400 CHECK (swr_bp BETWEEN 50 AND 1000),
  real_return_bp      integer NOT NULL DEFAULT 500 CHECK (real_return_bp BETWEEN -1000 AND 2000),
  version             integer NOT NULL DEFAULT 1,
  updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE investment_positions (
  id                   uuid PRIMARY KEY,
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  position             integer NOT NULL,
  name                 text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
  type                 text NOT NULL DEFAULT '' CHECK (char_length(type) <= 30),
  invested_minor       bigint NOT NULL DEFAULT 0 CHECK (invested_minor >= 0),
  value_minor          bigint NOT NULL DEFAULT 0 CHECK (value_minor >= 0),
  annual_income_minor  bigint NOT NULL DEFAULT 0 CHECK (annual_income_minor >= 0),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX investment_positions_user_idx ON investment_positions (user_id, position);
```

**Later migrations:**

| Phase | Migration | Tables / changes |
|---|---|---|
| 13 | `000NN_assistant.sql` | `assistant_chats(id, user_id, title, mode, model, created_at, updated_at)`, `assistant_messages(id bigint identity, chat_id → CASCADE, user_id, seq, role user\|assistant\|tool, content text, reasoning text, tool_calls jsonb, tool_call_id text, steps jsonb, meta jsonb, created_at)`, index `(chat_id, seq)` |
| 14 | `000NN_legacy_imports.sql` | `legacy_imports(id, user_id, status queued\|running\|done\|failed, progress jsonb, report jsonb, created_at, finished_at)` |
| 15 | `000NN_rules.sql` | `rules(id, user_id, position, name, enabled, conditions jsonb, actions jsonb, created_at, updated_at)`, unique `(user_id, position)` deferrable |
| 15 | `000NN_fx.sql` (stretch) | `fx_rates(currency char(3), on_date date, rate_pln numeric(14,6), source text, PRIMARY KEY (currency, on_date))`, not user-scoped |
| 16 | `000NN_audit.sql` | `audit_log(id bigint identity, user_id, action, entity, entity_id, at, meta jsonb)` |

### 5.3 Row-level security (Phase 4 migration `00002_rls.sql`)

For every user-owned table (`user_preferences`, `accounts`, `categories`, `groups`, `imports`, `import_files`, `import_rows`, `transactions`, `import_profiles`, `budgets`, `tx_annotations`, `dedupe_overrides`, `investment_profile`, `investment_positions`; later `assistant_chats`, `assistant_messages`, `legacy_imports`, `rules`, `audit_log`):

```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
CREATE POLICY <t>_tenant ON <t>
  USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
  WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid);
```

* `users` and `sessions` are **not** under RLS. Only the `auth` package touches them, because sessions must be resolved before a user is known.
* River tables and `fx_rates` are not under RLS either.

**`store.WithUser(ctx, userID, fn)`** is the only way handlers and jobs touch user data:

```go
func (s *Store) WithUser(ctx context.Context, userID uuid.UUID, fn func(q *db.Queries, tx pgx.Tx) error) error {
    return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
        if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, userID.String()); err != nil {
            return err
        }
        return fn(db.New(tx), tx)
    })
}
```

* Queries **also** filter by `user_id` explicitly. RLS is defence in depth, not the primary filter.
* An integration test proves that user A cannot read, update or delete user B's rows even when the query "forgets" the `user_id` predicate. Use a deliberately unsafe test-only query to prove it.

---

## 6. API specification

The contract lives in `api/openapi.yaml` (OpenAPI 3.0.3). This section is the design brief for writing it. Any change to the file regenerates both the Go server interfaces and the TS types (`make gen`). CI fails if generated code is stale (`make gen-check`).

### 6.1 Conventions

* **Base path:** `/api/v1`. Ops endpoints live outside it: `/healthz`, `/readyz` on the API port; `/metrics` and `/debug/pprof/*` on the admin port only.
* **JSON:**
  * camelCase field names; IDs are UUID strings; dates are `YYYY-MM-DD`; timestamps are RFC 3339 UTC.
  * **Money** is `{"amountMinor": -4498, "currency": "PLN"}`. `amountMinor` is a JSON integer (safe below 2^53, far beyond personal finance).
* **Errors:** RFC 9457 `application/problem+json`:
  `type` is a relative URI reference (`/problems/<code>`); no public domain is assumed.
  ```json
  {"type":"/problems/validation","title":"Validation failed","status":422,
   "code":"validation_failed","detail":"…","instance":"/api/v1/imports/…","requestId":"…",
   "errors":[{"field":"mapping.amount","message":"column not found"}]}
  ```
  Stable `code` values: `validation_failed`, `unauthenticated`, `forbidden`, `not_found`, `conflict`, `duplicate_file`, `import_not_committable`, `stale_version`, `rate_limited`, `payload_too_large`, `unsupported_file`, `internal`.
  * Never leak internal error text or SQL in `detail` when `FINUANCE_ENV=prod`.
* **Auth:**
  * HttpOnly cookie `finuance_session` (`Secure` in prod, `SameSite=Lax`, `Path=/`).
  * **CSRF:** every non-GET/HEAD request must carry `X-Finuance-Client: web` **and** an `Origin` that matches `FINUANCE_PUBLIC_ORIGIN` (if `Origin` is present). Cross-site forms cannot set custom headers, and cross-origin JS would need a CORS preflight, which is never granted.
* **Request ID:** accept or generate `X-Request-ID`, echo it in the response and in logs and traces.
* **Rate limits:** login and signup at 10/min per (IP, email) and 30/min per IP; uploads at 30/min per user. Exceeding returns `429` with `Retry-After`.
  * This is an in-memory token bucket (`golang.org/x/time/rate`). Document in an ADR that it is per replica.
* **Body limits:**
  * JSON at 1 MiB;
  * multipart: one file per request, ≤ `FINUANCE_MAX_UPLOAD_BYTES`. The legacy import endpoint has its own limit, `FINUANCE_MAX_LEGACY_IMPORT_BYTES`;
  * reject anything that is not text-like CSV/TSV (sniff the first 8 KiB for NUL bytes).
* **Large filter payloads:** query-style reads use `POST …/query`. QueryContext does not fit in a query string. Write an ADR for this.
* **Paging (transactions):** `offset` + `limit` (max 500), returning `total`.
  * ADR: a virtualised grid needs random access when the scrollbar is dragged;
  * per-user datasets are bounded (≤ ~200k rows);
  * the sort is index-assisted.
  * Export streams with a server-side cursor instead.
* **Optimistic concurrency:** `PUT /preferences` takes `version`; a mismatch returns `409 stale_version` with the current document.

### 6.2 Endpoints

| Method & path | Body → Response | Phase |
|---|---|---|
| **Meta** | | |
| `GET /meta` | → `{version, signupEnabled, demoMode, assistantEnabled}`; public, no auth | 2 |
| **Auth & account** | | |
| `POST /auth/signup` | `{email, password, displayName}` → `201 User` + cookie. Disabled → `403`. | 5 |
| `POST /auth/login` | `{email, password}` → `200 User` + cookie. Constant-time failure path. | 5 |
| `POST /auth/logout` | → `204` and clears the cookie; deletes the session | 5 |
| `POST /auth/demo` | → `200 User` (only when `FINUANCE_DEMO_MODE`) | 18 |
| `GET /me` | → `User` | 5 |
| `PATCH /me` | `{displayName}` → `User` | 5 |
| `POST /me/password` | `{currentPassword, newPassword}` → `204`; revokes other sessions | 5 |
| `GET /me/sessions` · `DELETE /me/sessions/{id}` | list / revoke (id = first 8 bytes of token hash, hex) | 5 |
| `DELETE /me` | `{password}` → `204`; deletes the user and all data (cascade) | 5 |
| `GET /me/export` | → `application/zip` stream: JSON per entity (accounts, categories, budgets, groups, annotations, dedupe overrides, investments, preferences, assistant chats), `transactions.csv` (all accounts) and the original import files. Each later phase adds its entity to the export. | 7 |
| **Preferences** | | |
| `GET /preferences` · `PUT /preferences` | `{version, prefs}` | 5 |
| **Accounts** | | |
| `GET /accounts` | → `[{id, name, currency, numberHint, openingBalance: Money, txCount, firstOn, lastOn, isDemo}]` | 6 |
| `POST /accounts` · `PATCH /accounts/{id}` · `DELETE /accounts/{id}` | CRUD; delete cascades transactions and imports | 6 |
| **Imports** | | |
| `POST /imports` | multipart `file`, optional `accountId` → `201 ImportPreview` · `409 duplicate_file {existingImportId}` | 6 |
| `POST /imports?autoCommit=true` | Fast path: if the mapping came from a profile, there are no row errors and the account is resolved → commit immediately and return `202 ImportPreview` with `status: committing` | 6 |
| `GET /imports?accountId=` | → `[ImportSummary]` newest first | 6 |
| `GET /imports/{id}` | → `ImportDetail` (status, stats, mapping, options, error) | 6 |
| `GET /imports/{id}/rows?offset&limit&onlyErrors` | → staged rows (canonical + raw + parseError + duplicate flag) | 6 |
| `GET /imports/{id}/lines?limit=18` | → raw decoded records `[{record, fields[], status: header\|preamble\|data\|footer_dropped\|blank\|skipped}]` for the header-row picker | 6 |
| `PATCH /imports/{id}` | `{accountId?, mapping?, options?}` → `ImportPreview` (re-parse from stored bytes; allowed when `previewed`) | 6 |
| `POST /imports/{id}/commit` | `{rememberMapping: true}` → `202 {status:"committing"}`; requires `accountId` and no blocking errors | 6 |
| `POST /imports/{id}/reprocess` | `{mapping?, options?}` → `202` (committed imports) | 6 |
| `DELETE /imports/{id}` | → `204` (orphan-safe, §4.4) | 6 |
| `GET /imports/{id}/file` | → original bytes, `Content-Disposition: attachment` | 6 |
| **Duplicates** | | |
| `GET /duplicates?accountIds&q&offset&limit≤60` | → `{total, groups: [DuplicateGroup]}`, newest first | 10 |
| `PUT /duplicates/{accountId}/{fingerprint}` | `{mode: "prefer", importId}` \| `{mode: "keep_both"}` → `202` (job `RematerializeFingerprint`); `fingerprint` is hex | 10 |
| `DELETE /duplicates/{accountId}/{fingerprint}` | → `202`; back to the default merge ("Merge again" / undo "Use this version") | 10 |
| **Transactions** | | |
| `POST /transactions/query` | `{context, sort?, offset, limit≤500, statColumn?: amount\|balanceAfter, locateId?}` → `TransactionPage` | 7 |
| `POST /transactions/distinct` | `{context, column, search?, limit≤600}` → `[{value, count}]`. Computed with this column's own filter removed, so unticked values stay listed. | 7 |
| `GET /transactions/{id}` | → `TransactionDetail` (all fields, `raw`, sources: imports that contain it) | 7 |
| `PATCH /transactions/{id}` | `{categoryId? \| categoryName?, note?}` → `TransactionDetail` (`category_source='manual'`) | 7 |
| `POST /transactions/export` | `{context, sort?, columns[], delimiter?}` → `text/csv` stream (UTF-8 BOM, `;` default, CSV-injection guard §8.1) | 7 |
| `POST /transactions/bulk` | `{ids[≤5000], set: {groupId? (null = ungroup), categoryId?, isTransfer?}}` → `{updated}`; also writes annotations | 10 (group), 15 (category, transfer) |
| `POST /transactions/ids` | `{context, sort?, offset, limit≤5000}` → `[{id, amountMinor}]` (range selection across unloaded windows; selection Σ) | 10 |
| **Categories & budgets** | | |
| `GET /categories?accountIds=` | → `[{id, name, txCount, colorSlot}]` (§4.6 colours) | 7 |
| `POST/PATCH/DELETE /categories…` | management (rename, merge into another category) | 15 |
| `GET /budgets` · `PUT /budgets/{categoryId}` | `{amountMinor}` or `{amountMinor: null}` to reset to default | 12 |
| **Investments** | | |
| `GET /investments` · `PUT /investments` | versioned document (§4.9; `Investments` schema); `409 stale_version` on conflict | 12 |
| **Insights** | | |
| `POST /insights/summary` | `{context}` → `Summary` | 11 |
| `POST /insights/categories` | `{context}` → `[CategorySpend]` | 11 |
| `POST /insights/cashflow` | `{context, months: "fit:N"\|6\|12\|24\|36\|"all"}` → `[MonthFlow]` | 11 |
| `POST /insights/pace` | `{context, compare: prev\|yoy\|avg3\|avg6\|none}` → `Pace` | 11 |
| `POST /insights/payees` | `{context, top, scope: period\|all}` → `[PayeeSpend]` | 11 |
| `POST /insights/daily` | `{context, range: period\|3m\|6m\|12m\|all}` → `[DaySpend]` | 11 |
| `POST /overview` | `{context (period ignored), month?, months: 6\|12\|24, big?}` → `Overview` | 12 |
| **Rules** (Phase 15) | | |
| `GET/POST /rules` · `PATCH/DELETE /rules/{id}` · `POST /rules/reorder` | CRUD and ordering | 15 |
| `POST /rules/{id}/preview` | → `{matches, sample[]}` | 15 |
| `POST /rules/apply` | `{scope: all\|uncategorised}` → `202` (job) | 15 |
| **Groups** | | |
| `GET /groups` | → `[Group]` with stats (§4.7) | 10 |
| `POST /groups` · `PATCH /groups/{id}` · `DELETE /groups/{id}` | `{name, color}`; delete removes labels only | 10 |
| **Assistant** (Phase 13) | | |
| `GET /assistant/status` | → `{enabled, reachable, models[], defaultModel, maxRowsLimit, error?}` (upstream `GET /models`, cached 60 s) | 13 |
| `GET /assistant/chats` · `POST /assistant/chats` | list (newest first: `{id, title, created, updated}`) · create | 13 |
| `GET /assistant/chats/{id}` · `DELETE /assistant/chats/{id}` | full chat (turns with steps, reasoning, meta) · delete | 13 |
| `POST /assistant/chats/{id}/messages` | `{text, scope: all\|view, context?, settings: {model?, mode: auto\|tools\|plan, think, maxRows, temperature}}` → `text/event-stream` of `AssistantEvent` (§6.3) | 13 |
| `POST /assistant/chats/{id}/stop` | → `204`; cancels the in-flight run (a client disconnect also cancels it) | 13 |
| **Legacy import** (Phase 14) | | |
| `POST /legacy-imports` | multipart `manifest` + one part per file (≤ `FINUANCE_MAX_LEGACY_IMPORT_BYTES` in total) → `202 {id}`; one legacy import per user at a time | 14 |
| `GET /legacy-imports/{id}` | → `{status, progress, report: LegacyImportReport}` | 14 |
| **Demo data** | | |
| `POST /demo-data` | → `201 {accountId, importId}`: creates a "Demo account" with the generated demo year (port of legacy `demoCSV`, seeded RNG, dates shifted so the last day is "yesterday") | 8 |

### 6.3 Key schemas (sketch; write them fully in OpenAPI)

```yaml
QueryContext: { accountIds: [uuid], search: string, filters: [ColumnFilter], period: Period|null, includeTransfers: boolean }
ColumnFilter: { column: FilterColumn(enum §4.5), cond: string, excluded: [string] }
Period:       oneOf { preset: l30|l3m|l12m|ytd } | { month: "YYYY-MM" } | { year: int } | { from: date, to: date }
              # period: null = all time (there is no "all" preset)
ResolvedPeriod: { kind: range|month|year, from: date, to: date, label: string }
TransactionRow: { id, accountId, bookedOn, valueOn, amount: Money, payee, description, category: {id,name}|null,
                  bankCategory, txType, reference, counterpartyAccount, note, isTransfer, categorySource }
TransactionPage: { rows: [TransactionRow], total: int, offset: int, period: ResolvedPeriod|null,
                   dataRange: {min: date, max: date}, stats: {column, sum, avg, min, max},
                   duplicatesSkipped: int, locatedOffset?: int }
ImportPreview: { id, status, filename, sizeBytes, encoding, delimiter, headers: [string], columnTypes: [ColumnType],
                 mapping: Mapping, options: ImportOptions, mappingSource, suggestedAccount: {id?|createName?},
                 stats: {rows, errors, duplicatesEstimate, newEstimate, dateRange, headerAt, dropped},
                 sampleRows: [StagedRow] (first 50), tailRows: [StagedRow] (last 5), profileLabel?,
                 warnings: [{code, message}] }
Mapping:       { bookedOn: ColRef, amount?: ColRef, payee?, description?, reference?, category?, currency?, valueOn?,
                 txType?, counterpartyAccount?, ownAccount?, balance?, ignore: [ColRef] }   # ColRef = {index, header}
ImportOptions: { encoding: auto|utf-8|windows-1250|iso-8859-2|windows-1252, delimiter: auto|";"|","|"\t"|"|",
                 headerRow: "auto"|"none"|int, skipRows: int, dropShort: bool, dateOrder: dmy|mdy,
                 stripQuote: bool, invertSign: bool, amountMode: single|debit_credit, debitColumn?: ColRef, creditColumn?: ColRef,
                 currencyFallback: string }
Overview:      §4.6 fields
ToolFilter:    { query?, payee?, category?, group?, dateFrom?, dateTo?, type?: expense|income|any, minAmount?, maxAmount?, label }
Group:         { id, name, color: 0..7, count, spent: Money, income: Money, firstOn, lastOn }
DuplicateGroup: { accountId, fingerprint (hex), override: null|{mode, importId?}, keyFields: {bookedOn, amount, payee, description, reference},
                 differingFields: [string], versions: [{importId, filename, committedAt, rows: [{occurrence, fields, raw, inData: bool}]}] }
Investments:   { version, ppk: {employeeMinor, employerMinor, stateMinor, valueMinor}, swrBp, realReturnBp,
                 positions: [{id, name, type, investedMinor, valueMinor, annualIncomeMinor}] }   # PLN
LegacyImportReport: { files: [{legacyId, name, importId?, status: imported|reused|failed, error?}], transactions, duplicatesSkipped,
                 labels: {matched, unmatched, reasons[]}, overrides: {matched, unmatched}, budgets, investments: bool, chats, ignoredKeys: [string] }
AssistantEvent (SSE event names):
               step {id, tool, args, summary, resultPreview, matched, toolFilter}
               reasoning {delta} · content {delta} · mode {mode: "plan", reason}
               done {messageId, meta: {durationMs, approxTokensSent, lookups, model, mode}} · error {code, message}
Problem:       §6.1
```

---

## 7. Frontend specification

### 7.1 Principles

* **Visual parity first.** The new UI must look and feel like the legacy app. The legacy rules all carry over:
  * VS Code / Obsidian / LM Studio look;
  * dark default following the OS, plus a light theme;
  * hairline borders, muted chrome, one violet accent;
  * system fonts only (`--sans`, `--mono`; no web fonts, no icon fonts). Port the legacy inline SVG icons into an `<Icon name>` component.

  Don't redesign. Improvements go to the backlog.
* **Brand:** the Finuance logo (a purple gradient tile with a white book-with-$, legacy `ICONS.book`) appears in the title bar and as the favicon.
* **No HTML-string rendering.** No `innerHTML` and no `dangerouslySetInnerHTML`. Charts are JSX SVG. This keeps XSS impossible by construction and allows a strict CSP (`script-src 'self'; style-src 'self'`). React's `style={{…}}` uses CSSOM, which CSP does not block.
* **Server is the source of truth.** The client holds UI/view state only.
* **Model output is untrusted.** Assistant answers are rendered by the `md()` port into React nodes. No raw HTML passes through; links render only for `http(s)` URLs, with `rel="noreferrer noopener"`.
* **Quality floor:**
  * visible keyboard focus everywhere;
  * every popover and menu keyboard-operable;
  * `prefers-reduced-motion` respected;
  * every chart has a table twin (modal);
  * tooltips enhance and never gate information.
* **Copy:** sentence case and plain verbs.
  * An action keeps its name through the flow: the "Import" button produces an "Imported 312 transactions" toast.
  * Errors say what happened and how to fix it, e.g. "This file has no date column. Choose the booking date column in the mapping."

### 7.2 Dependencies (pin exact versions; add nothing else without asking)

`react`, `react-dom`, `react-router`, `@tanstack/react-query`, `@tanstack/react-virtual`, `zustand`, `@floating-ui/react`, `openapi-fetch`.

Dev: `typescript`, `vite`, `@vitejs/plugin-react`, `openapi-typescript`, `vitest`, `@testing-library/react`, `@testing-library/user-event`, `jsdom`, `msw`, `@playwright/test`, `eslint`, `typescript-eslint`, `eslint-plugin-react-hooks`, `prettier`.

`tsconfig`: `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`.

### 7.3 Routes

| Path | Screen |
|---|---|
| `/login`, `/signup` | Auth forms (signup hidden when disabled; demo button when demo mode) |
| `/` | Redirect to `/t/all` (or empty state when the user has no transactions) |
| `/t/:viewId` | Table mode (`viewId` = `all` or an account id): insights pane + toolbar + grid + inspector |
| `/o/:viewId` | Overview mode |
| `/imports/:importId` | Import preview and mapping |
| `/settings/:section` | `profile`, `appearance`, `accounts`, `categories`, `rules` (Phase 15), `data` (export, legacy backup import, delete account), `sessions` |

`<RequireAuth>` loads `GET /me`. A `401` redirects to `/login?next=…`. Before first render it also loads `GET /preferences`, the equivalent of the legacy `S.booted` gate, so defaults never overwrite saved prefs.

### 7.4 State

**Server state (TanStack Query) keys:**

| Key | Contents |
|---|---|
| `['me']` | Current user |
| `['prefs']` | Preferences document |
| `['accounts']` | Account list |
| `['imports', accountId?]` | Import list |
| `['import', id]` | Import detail; polls while `committing` |
| `['categories', accountIds]` | Categories + colour slots |
| `['tx', ctxKey, sortKey, windowIndex]` | One window of transactions |
| `['distinct', ctxKey, column, q]` | Value checklist |
| `['insight', kind, ctxKey, paramsKey]` | Insight chart data |
| `['overview', ctxKey, month, months]` | Overview payload |
| `['budgets']` | Budget overrides |

* `ctxKey` is a stable hash of the QueryContext.
* Use `placeholderData: keepPreviousData` on `tx` and insights so filtering doesn't flash.
* **Mutations invalidate precisely:**
  * import commit/delete → `accounts`, `imports`, `categories`, and everything `tx`/`insight`/`overview`;
  * transaction patch → that row's window, insights, overview, categories;
  * budget change → `overview`.

**View state (Zustand `viewStore`)**, one entry per `viewId`, mirroring the legacy `ViewState`:

```ts
type ViewState = {
  sort: { column: Column; dir: 'asc' | 'desc' } | null;
  filters: Partial<Record<Column, { cond: string; excluded: string[] }>>;
  search: string;
  hidden: Column[];
  widths: Partial<Record<Column, number>>;
  selectedId: string | null;
  statColumn: 'amount' | 'balanceAfter';
  period: PeriodPref;               // 'all' | 'l30' | 'l3m' | 'l12m' | 'ytd' | `m:${string}` | `y:${number}` | 'custom'
  custom: { from: string; to: string } | null;
  ovMonth: string | null;
};
```

**UI state (`uiStore`):**

* `mode`;
* `sidebarOpen` (**default false**: legacy v7 hides the sidebar by default) and `sidebarAutoCollapsed`;
* `vizOpen`, `paneHeight`, `cards`, `cardOrder` (port `cardOrder()`: unknown keys dropped, new cards appended), `catMode`;
* `cfg: {flowMonths, paceCmp, payeeTop, payeeScope, heatRange, ovMonths}`;
* `ai: {open, width, max, model, mode, think, maxRows, temperature, scope, keepChats}` (legacy `S.ai`);
* `importFastPath: boolean` (default true).

**Multi-selection** lives in a non-persisted `selectionStore` per view: a `Set` of ids plus an anchor. It is cleared when the QueryContext or sort changes (legacy `refresh()` rule), on Esc, or by a plain click.
* `themePref`, `uiScalePref`.

**Persistence rules:**

* Persist exactly what the legacy persisted (`collectPrefs`): UI state, plus per view `{hidden, widths, statColumn, sort, period, custom}`. v7 adds `sbOpen` and the AI panel settings `S.ai`; Finuance adds `cardOrder`.
* **Not** search or filters; those are session-only, as in the legacy.
* Writes go through `PUT /preferences`, debounced 500 ms, with version handling. On `409` merge "server wins, then re-apply local change" and retry once.

**Debounce** (legacy values): search 120 ms, filter condition 160 ms.

### 7.5 Design tokens and scaling

* Copy the legacy CSS custom properties **verbatim** into `web/src/styles/tokens.css`:
  * `:root`, `[data-theme=dark]` and `[data-theme=light]` blocks (legacy lines ~10–45);
  * status tokens `--st-*` (legacy line ~323);
  * the chart palette `--s1…--s7`, `--g8` (v7: 8th group colour), `--c-other`, `--c-prev`, `--inc`, `--exp`, `--gridl`, `--axis`, `--h0…--h5`.
* Theme: set `data-theme` on `<html>` from `themePref` (`system`/`dark`/`light`). While `system`, follow `prefers-color-scheme` changes.
* **rem system:** all component CSS uses `rem` except 1–2 px hairlines and SVG text. `html { font-size: calc(16px * var(--ui-scale)); }`.
  * `uiScale` is either manual (90–130 %) or `auto`. Auto ports `browserZoom()` and `calcUI()`: `clamp(1.12 / zoom, 0.85, 1.25)`, re-evaluated on resize.
  * JS-side pixel sizes (gutter width, default column widths, min column width, popover widths) multiply by `uiScale`.
  * The grid row height is **measured** after first render, not assumed.
* **Charts:** draw in logical units; `<Svg w h>` sets physical size and `viewBox` so everything scales with `uiScale`. SVG text uses **px** (logical). Never use rem inside SVG, because it double-scales.

### 7.6 Legacy → React component map

| Legacy (id / function) | React | Notes |
|---|---|---|
| `header.titlebar` | `shell/TitleBar` | Sidebar toggle, charts-pane toggle, overview toggle, brand (logo), search (⌘F, `/`), assistant (⌘J), settings (`SettingsPopover`), user menu. **No** import, theme or interface-size buttons (legacy §32.2); files open via ⌘O, the drop zones, or drag & drop anywhere |
| `#setBtn` settings popover | `shell/SettingsPopover` | Scrollable. Appearance inline (theme System/Dark/Light, interface size incl. Auto), *Keyboard shortcuts*, links to `/settings/*`, Sign out |
| `#keysDlg` + `KEYS` | `shell/KeyboardShortcuts` | Dialog listing all shortcuts; ⌘ on macOS, Ctrl elsewhere; opened from settings or with `?` |
| `#dupDlg` | `duplicates/DuplicatesDialog` | §7.14 |
| `aside#sidebar` + `renderSidebar` | `shell/Sidebar` | **Explorer:** All accounts (count) → accounts (count, `demo` tag, context menu) → collapsible import history per account (status, new/dup counts, delete, reprocess). Mini drop zone. Auto-collapse below 900 px and restore only if it was auto-collapsed (port). Parsing options move to the import preview. **Hidden by default** (v7). Source labels `b1`, `b2`, … next to imports whose import profile differs (port), so files from different banks are easy to tell apart. |
| `.toolbar` + `renderToolbar` + `chip` | `shell/Toolbar`, `table/Chips` | Title + meta, removable chips (period, search, sort, each condition, excluded count, hidden columns), Clear all, Columns menu, Export |
| `#gridWrap table#grid` | `table/Grid` | §7.7 |
| `openColMenu` / `openColsMenu` | `table/ColumnMenu`, `table/ColumnsMenu` | Condition help text ported from legacy `HELP`. ColumnsMenu has a single toggle: *Hide all* when every column is visible, otherwise *Show all* |
| `aside#inspector` | `shell/Inspector` | Canonical fields, original `raw` fields with original headers, sources (imports), category combobox (pick or create), note, Copy as JSON, prev/next |
| `footer.statusbar` | `shell/StatusBar` | Rows shown/total (accent when filtered), visible columns, period label, Σ / avg / min / max from server `stats`. The totals column picker ("totals of …") appears only when 2+ number columns are available: Amount, plus Balance after when the view's accounts have balance values (legacy rule); otherwise the totals carry a tooltip naming the column. When rows were skipped as duplicates, "N duplicates skipped" links to the Duplicates dialog |
| `#viz` + `renderVizHead` | `insights/InsightsPane`, `insights/PeriodPicker` | Resizable (170 px … 75 vh, double-click reset). Charts menu: show/hide **and reorder** (draggable rows + ↑/↓ buttons, persisted `cardOrder`). Collapse. Period picker options come from `dataRange`. Prev/next stepping; custom range inputs. |
| `CHARTS.*` | `charts/*` + `insights/ChartCard` | §7.8 |
| `#modal` + `renderModal` | `insights/ChartModal` | Chart/Table switch, range controls, prev/next, Esc / ← / →, backdrop click closes |
| `section#overview` + `renderOverview` | `overview/OverviewPage` | §7.9 |
| `#pop` + `openPop/placePop` | `ui/Popover` (Floating UI: `flip`, `shift`, `size`, `autoUpdate`) | Re-anchors on resize and scroll; outside-click and Esc close |
| `#toast` | `ui/Toast` | Queue; success/error variants |
| `#vtip` | `ui/Tooltip` + `charts/useTip` | One portal tooltip; follows the pointer; hidden on scroll |
| `.empty #bigDrop` | `shell/EmptyState` | Drop zone, Browse, **Load demo data** (`POST /demo-data`) |
| `.overlay` (drag & drop) | `shell/DropOverlay` | Depth counter to avoid flicker (port). Reacts only to drags that carry files (`dataTransfer.types` includes `Files`), so reordering chart cards never triggers it |
| `#grpBtn` + `openGroupsMenu`, `openGroupForm` | `groups/GroupsMenu`, `groups/GroupForm` | Toolbar button with a count badge.<br>**List:** colour, name, count · spent. Clicking an entry toggles the filter `group =Name`.<br>**Actions:** ✎ rename/recolour (active filters follow the new name), × delete (labels only, with confirm), New group, Show all.<br>**Form:** unique name ≤ 40, an 8-colour palette, Enter submits. |
| `#qbar` + `renderQbar` | `groups/QuickBar` | Floats at the bottom of the content area while ≥ 1 row is selected: count, Σ amount, group colour dots (assign), **+** (new group, then assign), Ungroup (if any selected row is grouped), close. Toasts move up while it is visible. |
| Inspector "Group" row | `shell/Inspector` | A pill per group (active outlined); clicking the active pill removes the label; + New |
| `aside#chat` + `renderChat`, `#aiMenu` | `assistant/ChatPanel`, `assistant/ChatMenu` | §7.13 |
| `#imp` import settings window | `import/ImportPreview` | §7.10 (mirrors the legacy window) |
| `.lock` + passphrase | — | **Retired.** Replaced by server auth. |
| `openFieldsMenu` (role overrides) | — | **Retired.** Roles are mapped per import in the preview (§7.10). |
| `openSettings` / backup / restore | `settings/*` | Data export via `GET /me/export`. Legacy backups are imported once through the migration (§4.10). Restoring a new-app export is in the backlog. |

### 7.7 Grid

* `<table>` with `table-layout: fixed`, a `<colgroup>`, a sticky header and a sticky row-number gutter. The gutter shows the 1-based position in the current result.
* **Columns**, with a fixed type per column:

  | Column | Type | Visible by default |
  |---|---|---|
  | Date | date | yes |
  | Value date | date | |
  | Payee | text | yes |
  | Description | text | yes |
  | Amount | number | yes |
  | Balance after | number | hidden; available only when the view's accounts have balance values |
  | Currency | text | |
  | Category | text | yes |
  | Bank category | text | |
  | Type | text | |
  | Reference | text | |
  | Counterparty account | text | |
  | Account | text | yes, in the `all` view |
  | Note | text | |
  | Group | text | once the user has a group |

  Type badges as in the legacy: `#` number, calendar date, `Aa` text, coloured with `--c-num/--c-date/--c-text`.
* **Virtualisation:** TanStack Virtual over `total` rows, with 10 rows of overscan.
  * Data is fetched in **windows of 200 rows** (`offset = windowIndex*200`) for the windows intersecting the viewport ±1. Unloaded rows render as skeleton rows.
  * The scroll handler is rAF-throttled.
* **Header:**
  * Click cycles sort asc → desc → none.
  * The funnel button (visible on hover or when active) or a right-click opens the ColumnMenu.
  * Drag the right edge to resize (stored in `widths`); double-click resets.
* **ColumnMenu:**
  * sort entries with type-specific labels;
  * a condition input with type-specific help;
  * the distinct-value checklist: search box, All/None acting on the shown values, counts, capped at 600;
  * Hide column; Clear filter.
* **Cells:**
  * numbers right-aligned in mono with tabular figures; negatives use `.neg` (red);
  * dates in mono;
  * search hits wrapped in `<mark>` by a port of `hl()` (maps normalised matches back to original characters; skips highlighting when normalisation changes length).
* **Interaction:**
  * row click → select and open the Inspector;
  * double-click a cell → copy its value (toast);
  * ↑/↓, j/k, PgUp/PgDn move the selection and keep it in view, fetching windows as needed.
* **Empty result:** a single row "No rows match … Clear all".
* **Multi-selection** (legacy §30):
  * gestures: drag across rows (auto-scroll near the edges), Shift-click selects a range from the anchor, ⌘/Ctrl-click toggles a row;
  * selected rows get `.msel`; a plain click selects one row (inspector);
  * ranges that cross unloaded windows resolve ids and amounts via `POST /transactions/ids`, and the selection Σ comes from those amounts;
  * port the click-suppression detail: after a drag, ignore clicks for a short window (`suppressUntil`). The click handler ignores modifier clicks instead of using a flag.
* **Group column:** text, tag-icon badge, rendered as coloured pills; hidden until the user has at least one group.
* **Locate row:** `POST /transactions/query` accepts `locateId`. The response includes `locatedOffset`, so "go to row" from charts (largest expense) can scroll to it, then select it. This ports legacy `gotoRow`.

### 7.8 Charts

* `charts/lib/`: port `barPath`, `arc`, `spark`, `cpt` (compact numbers), `money`, `tipHtml` → `<TipContent>`, `roundNice`, colour-slot helpers. Add unit tests for the geometry helpers.
* **Chart definition registry:**

  ```ts
  type ChartDef<D> = {
    key: ChartKey; title: string;
    useData(ctx: QueryContext, cfg: ChartCfg): UseQueryResult<D>;
    Component: (p: { data: D; w: number; h: number; big: boolean; cfg: ChartCfg;
                     actions: ChartActions; colors: ColorMap }) => JSX.Element;
    Subtitle?: (p: { data: D }) => JSX.Element;      // legacy c.sub
    Legend?: (p: { data: D }) => JSX.Element;        // legacy c.cx
    cfgControls?: CfgControl[];                      // legacy cfgCtl, rendered only in the modal
    toTable(data: D): { cols: string[]; num: boolean[]; rows: (string | number)[][] };  // table twin
  };
  ```
* `ChartCard` measures its body with a ResizeObserver. It passes logical `w/h` (= px / uiScale) and skips rendering below 40 px (legacy guard).
* **Pane charts** (legacy §14): `summary`, `categories` (donut/bars toggle), `cashflow`, `pace` (crosshair overlay with its own pointer handlers), `payees`, `daily` (GitHub-style calendar, quintile bins, Mon-first, fits height and width).
* **Chart → table actions** (port `toggleFilter` semantics: applying an identical filter again toggles it off):
  * category slice/bar → filter `category` `=Name`;
  * month bar → period `m:YYYY-MM`;
  * payee bar → filter `payee` contains the name;
  * heatmap day → custom period of that day;
  * largest expense → locate row.
* **Data-viz rules** (legacy §19):
  * one y-axis;
  * hairline solid grid;
  * 2 px lines;
  * small round markers with a surface-coloured ring;
  * 2 px surface gaps between segments;
  * rounded data-ends;
  * selective direct labels;
  * colour follows the entity (server `colorSlot`), never generated hues;
  * status colours always paired with icon + label.

### 7.9 Overview

* Header:
  * month selector with prev/next;
  * the note "N filters from the list applied · Clear" when filters or search are active;
  * a partial-month flag.
* KPI row: Income, Spending, Net saved (+ rate), Avg/day or **Projected spend**. Each has Δ vs the previous month (colour by whether up is good) and a sparkline.
* **Cards** (port `OV_DEFS` order and spans):

  | Card | Notes |
  |---|---|
  | Highlights | |
  | Cash flow | |
  | Budgets | Inline editable inputs → `PUT /budgets/{categoryId}`; pace marker; status icon + label |
  | Recurring payments | |
  | Money flow (Sankey) | Click a category → filter + switch to table |
  | Largest expenses | Click → locate row |
  | Savings rate | |
  | Spending by weekday | Emphasis form |
  | Account balance | Opening balance input **only in a single-account view**. In "All accounts" it shows the sum, read-only. **[CHANGE]**, documented. |
  | Investment | Allocation bar, PPK (paid in, gain, return on your own money), positions table + totals |
  | Yield on cost (PLN) | Per position + highlighted total; transaction-derived fallback flagged |
  | FI/RE number | FI number, progress meter, years to FI; SWR and real return shown |

* Investment, Yield on cost and FI/RE share one row: 3 + 3 + 6 columns, or 6 + 6 + 12 at ≤ 1100 px.
* Their data is edited only in the large view (`ChartModal`): PPK fields, the positions table (add/remove), SWR and expected real return. Enter blurs; changes save with a debounced `PUT /investments`.

* Layout:
  * 12-column grid inside a `container-type: inline-size` wrapper;
  * `@container` breakpoints at 1100 px (6/12) and 640 px (full width);
  * **card bodies have explicit heights** (legacy gotcha: grid + flex collapse).

### 7.10 Import UX

1. Drop files anywhere, use the sidebar or empty-state drop zone, or press ⌘/Ctrl O → `POST /imports` per file. There is no title-bar import button (legacy §32.2).
   * Multiple files are previewed one after another, with a "1 of 3" stepper.
   * A `409 duplicate_file` shows a toast "Already imported on <date>" with a link to that import.
2. **Preview** (`/imports/:id`):
   * file facts: name, size, detected encoding and delimiter, each with an override select;
   * account: the suggestion, an existing account, or "Create account" with an inline name;
   * **raw lines panel** (port of the legacy v7 import window): the first 18 decoded records with record numbers (`GET /imports/{id}/lines`).
     * Click a line to make it the header row.
     * Buttons: *File has no header row* (reveals a skip-rows input) and *Automatic*.
     * Preamble, footer-dropped and skipped lines are struck through.
   * footer-drop toggle and date order (day-first / month-first);
   * **mapping editor**: one row per role. Each row has a header dropdown (with the detected column type badge), plus `detected`/`profile` badges and a required marker for `bookedOn` and `amount` (or debit + credit);
   * second entry point, as in the legacy window:
     * click a column name in the sample grid → role menu (each role usable once; an "auto" hint shows what detection would choose);
     * *Use detected meanings* fills every role from the hints;
     * a footer lists the required fields (Date, Amount, Name, Description) with ✓/✗, plus the detected encoding, delimiter, header row, rows and skipped count.
   * popovers inside the dialog stay inside the viewport and above the dialog.
   * options: strip leading `'`, invert sign, amount mode (single / debit + credit);
   * stats: rows, rows with errors, new (estimate), duplicates (estimate), date range;
   * sample grid: the first 50 and the last 5 staged rows (a ⋯ row marks the gap), in canonical columns. Errored rows are highlighted with the error message, and a link shows all errors (`onlyErrors=true`);
   * buttons: **Cancel** (`DELETE /imports/{id}`) and **Import N transactions** (disabled until an account and the required roles are set). There is also a "Remember this mapping for files like this" checkbox, on by default.
   * Every change calls `PATCH /imports/{id}` (debounced 300 ms) and re-renders from the response.

**Fast path (rev 2).** The legacy opens its import window only when detection fails. Equivalent here:
* When the user preference *Import known layouts without review* is on (default), uploads use `autoCommit=true`.
* A profile-matched, error-free file with a resolved account commits directly.
* The toast offers *Review* (open the import) and *Undo* (`DELETE /imports/{id}`).
* Everything else opens the preview.

3. Commit → `202` → poll `GET /imports/{id}` every 500 ms (max 2 min).
   * Success: toast "Imported 312 transactions · 45 duplicates skipped", then navigate to `/t/:accountId`.
   * Failure: inline error with the server message.

### 7.11 Keyboard map (port legacy §22)

| Keys | Action |
|---|---|
| ⌘/Ctrl F, `/` | Focus search |
| ⌘/Ctrl O | Import file dialog (preventDefault) |
| ⌘/Ctrl J | Toggle the assistant panel |
| `?` | Keyboard shortcuts window (outside inputs) |
| Esc | Close import window / modal → close popover → clear/blur search → clear multi-selection → deselect row |
| ↑/↓, j/k, PgUp/PgDn | Move row selection (outside inputs) |
| ←/→ | Previous/next chart in the modal |

### 7.12 Performance budgets

* Initial JS ≤ 250 KB gzip. The route-split overview and import preview don't count against it.
* Grid scrolling at 60 fps with 100k rows.
* Filter-to-paint ≤ 300 ms p95 on the homelab with a 100k-row user.

### 7.13 Assistant panel (legacy §29)

* **Panel:** a right-side `ChatPanel`, toggled by the title-bar button or ⌘/Ctrl J.
  * Resizable from its left edge (logical px × uiScale; double-click resets; max 85 % of the main area).
  * **Maximise** covers the whole main area, with content centred at 56 rem.
  * Width and maximise state are persisted (`uiStore.ai`).
* **Status:**
  * Without an endpoint (`/assistant/status.enabled=false`), the panel explains that the admin must set `FINUANCE_AI_BASE_URL`, with a link to the docs.
  * When the endpoint is unreachable, the status dot turns red and the server's error is shown with the usual fixes.
* **Header:**
  * the title; hovering or clicking it opens `ChatMenu`: recent chats `Interaction DD.MM HH:MM` + first question, delete ×, *New chat*;
  * the status dot;
  * the model select (from `/assistant/status`);
  * a settings popover: mode auto/tools/plan, thinking on/off, max rows per lookup, temperature, scope all/current view, keep chats.
* **Empty state:** suggestion chips ported from the legacy, e.g. "How much did I spend on groceries last month?".
* **Messages:**
  * assistant answers are rendered by the `md()` port (escape first; paragraphs, lists, headings, code, tables → React nodes);
  * a collapsible **Thinking** section;
  * one collapsible **step** per lookup, showing the tool, the arguments and exactly what was sent back to the model, with **Show in table**;
  * per-answer meta (time, ~tokens sent, lookups).
* **Streaming:** `fetch` POST + `ReadableStream` with a small SSE parser (port legacy `sse()`; `EventSource` can't POST).
  * **Stop** → `POST …/stop` and abort the fetch.
  * Switching or deleting chats first stops the run and waits for it (port `stopAndWait`).
* **Show in table** applies the step's `toolFilter` as a "✦" chip in the right view (`all` or the account it came from) and clears the period/search, as the legacy does.
* **Scope "current view"** sends the active QueryContext. The server intersects the tool filters with it.

### 7.14 Duplicates dialog (legacy §32.3)

* Opened from the status-bar link "N duplicates skipped" in any view whose accounts have skipped rows.
* Groups are listed newest first, 60 at a time (*Show more*), with a text filter. The header notes that duplicates are detected **per account**.
* Each group is a small table of all versions:
  * the import file name, the key fields and every field that differs;
  * differing cells highlighted;
  * ✓ marks the version shown in the data.
* Actions per group: **Use this version** (prefer that import) and **Keep both** / **Merge again**.
  * Each action calls the API and shows progress until the re-materialisation job finishes.
  * It then refreshes the table, the insights, the status bar and the dialog.

---

## 8. Cross-cutting concerns

### 8.1 Security

Write `docs/THREAT-MODEL.md` in Phase 5 and update it in Phase 16. Baseline controls:

| Threat | Control |
|---|---|
| Cross-tenant data access | `user_id` predicates + RLS (§5.3) + integration tests per endpoint ("user B gets 404 for user A's ids") |
| Credential stuffing / brute force | argon2id (OWASP params: m=19 MiB, t=2, p=1; tune with a benchmark and record in an ADR), rate limits, generic error message, constant-time compare, dummy hash on unknown email |
| Session theft | Random 32-byte tokens, only SHA-256 stored, HttpOnly + Secure + SameSite=Lax, idle + absolute expiry, rotate on login and password change, revoke list in UI |
| CSRF | Custom header + Origin check (§6.1) |
| XSS | React escaping, no `dangerouslySetInnerHTML`, strict CSP from nginx/ingress: `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'` |
| SQL injection | sqlc + parameterised builder; whitelisted columns; fuzz tests (§4.5) |
| Malicious uploads | Size limits, NUL-byte sniff, row and column caps (200k rows, 200 columns per file), parse timeout via context, no files written to disk |
| **CSV/formula injection on export** | For text cells starting with `=`, `+`, `-`, `@`, tab or CR **that do not parse as a number**, prefix `'`. Unit tests cover this. (The legacy export lacked it.) |
| Sensitive data exposure | Full account numbers are never stored outside `raw`/`counterparty`; `number_hint` = last 4 digits; logs never contain request bodies, transaction fields or cookies (log redaction test) |
| Security headers | `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy` (minimal), HSTS at the ingress |
| Supply chain | Pinned deps; `govulncheck`, `npm audit --omit=dev` (or osv-scanner), Trivy image scan, gitleaks, Renovate, SBOM (syft), cosign keyless signing |
| Data at rest | Disk/volume encryption documented in RUNBOOK; backups encrypted at the destination |
| Privacy | No third-party analytics or CDNs; `GET /me/export`; `DELETE /me` cascades everything |
| Prompt injection via transaction text | Read-only tools scoped by RLS to the requesting user; no tool reaches the network or other users; numbers traceable to visible steps; data framed as data in the prompt |
| SSRF / data egress via the assistant | Endpoint is instance config only, never user input; private-address check unless `FINUANCE_AI_ALLOW_REMOTE`; NetworkPolicy egress only to that host; browser CSP stays `connect-src 'self'` (the legacy needed a runtime CSP-narrowing trick) |
| Assistant abuse / resource exhaustion | Per-user rate limit, one in-flight run per user, per-replica concurrency cap, timeouts, history and result caps |
| Logging of private data via the assistant | Never log prompts, model output or tool results; spans carry only model, mode, rounds, tool names, durations |
| Legacy backup passphrase | Encrypted legacy backups are decrypted **in the browser** with WebCrypto (legacy §17.4 parameters). The passphrase and ciphertext never reach the server. |

### 8.2 Observability

* **Logs:** `slog` JSON with fields `time, level, msg, request_id, trace_id, user_id (hashed), route, status, duration_ms`. One access-log line per request. Never log bodies.
* **Metrics** (Prometheus, admin port):

  | Metric | Labels |
  |---|---|
  | `http_server_requests_total` | `route`, `method`, `status_class` |
  | `http_server_request_duration_seconds` (histogram) | `route` |
  | `finuance_imports_total` | `result` = committed\|failed\|duplicate_file |
  | `finuance_import_rows_total` | `outcome` = new\|duplicate\|error |
  | `finuance_import_commit_duration_seconds` | |
  | `finuance_jobs_total` | `kind`, `result` |
  | `finuance_job_duration_seconds` | `kind` |
  | pgxpool stats | (collector) |
  | Go runtime | default |

* **Assistant metrics:**

  | Metric | Labels |
  |---|---|
  | `finuance_assistant_runs_total` | `mode`, `result` = ok\|error\|stopped\|fallback_plan |
  | `finuance_assistant_run_duration_seconds` | `mode` |
  | `finuance_assistant_tool_calls_total` | `tool`, `result` |
  | `finuance_assistant_rounds` (histogram) | |
  | `finuance_assistant_tokens_sent_approx` (histogram) | |
  | `finuance_llm_request_duration_seconds` | `model` |

* **Traces:** OpenTelemetry SDK, `otelhttp` on the router, a pgx tracer (e.g. `otelpgx`), River job spans. OTLP exporter only when the endpoint is set. Propagate `traceparent`.
* **Health:**
  * `/healthz` = process alive;
  * `/readyz` = DB ping + migration version ≥ expected + River client started (worker).
* **pprof** on the admin port only.

### 8.3 Testing strategy

| Layer | Tooling | What |
|---|---|---|
| Pure domain (Go) | `testing`, table-driven, `go-cmp` | `ingest`, `money`, `query` DSL, `insights` calculators, fingerprinting |
| **Differential vs legacy** | `testdata/oracle/*.json` from `tools/legacy-oracle` | Go output must equal legacy output for: decode/delimiter/shape, parseNum/parseDate/looseDate corpora, column types, roles, norm, payeeName, fingerprint keys, merge/dedupe results, filter results on demo data, findRecurring on demo data. Allowed differences are listed explicitly in `testdata/oracle/ALLOWED_DIFFS.md`. |
| Fuzz | `go test -fuzz` | `ParseCSV`, `ParseNumber`, `ParseMinor`, `ParseDate`, filter DSL parser, full `ingest.Parse`. Seed corpora from the fixtures. No panics; invariants hold (e.g. `ParseMinor` round-trips its own formatting). Run 60 s per target in nightly CI. |
| Golden | `testdata/golden/*.json` with `-update` flag | `ingest.Parse` of every fixture; OpenAPI response snapshots for key endpoints |
| Integration (DB) | testcontainers-go Postgres (same major as prod), migrations applied, `finuance_app` role | Store queries, RLS isolation, commit/delete/reprocess semantics, query compiler against real SQL, insights against seeded demo data |
| HTTP | `httptest` + real DB | Auth flows, CSRF, rate limits, problem+json shapes, per-endpoint tenancy |
| Frontend unit | Vitest + Testing Library + MSW | Stores, chips logic, ColumnMenu, PeriodPicker, chart helpers, `hl()`, formatting |
| E2E | Playwright against `compose.ci.yaml` | Port legacy scenarios (legacy §26) + import flows + auth. Screenshots in dark/light; viewport 720 px; UI scale 100 % vs 125 % |
| **Parity harness** | Playwright drives the legacy file (demo year) and the new app (same demo data) | Compare summary numbers, category totals, cash-flow months, recurring list, overview KPIs |
| Load | k6 | §9 Phase 17 |
| Assistant (integration) | Go `httptest` mock OpenAI-compatible server | Scripted streaming tool calls split mid-JSON; a model that rejects `tools` (auto → plan fallback); Qwen `<tool_call>` text; `<think>` blocks; slow streams and mid-stream cancel. Ports the legacy §29.6 scenarios. |
| Assistant tool parity | Oracle (`execTool` on demo data, with groups) | Every tool/filter/`group_by` combination equals the legacy result |
| **Dedupe model property test** | Seeded random generator (`math/rand/v2`) | Random sequences of import / delete / reprocess / override set and clear over generated overlapping files. The incremental paths must always equal a full `Rematerialize` per fingerprint, and the multiset rule must hold. |
| Legacy migration | Oracle-generated backups (plain + encrypted) | Migration report counts; labels, budgets, overrides and investments land where expected; the parity harness numbers also match after migration |
| **Assistant evals** | `evals/assistant` runner against a real local model | ≥ 40 golden questions on demo data with a frozen "today", each with expected tool calls / argument constraints and expected numbers. Scores: tool choice, argument accuracy (date ranges!), numeric correctness of the answer, latency, tokens. Run manually or nightly on a homelab runner; results in `docs/evals.md`. |

**Coverage goals** (guidance, not a gate): `ingest`, `query`, `insights`, `ledger` ≥ 85 % lines; handlers ≥ 70 %.

### 8.4 Coding standards

**Go:**

* `float64` is allowed **only** for ratios and projections in `insights`/`investments` (savings rate, YOC, years to FI). Stored, summed and compared amounts are always `int64` minor units.

* `gofmt`/`goimports`; golangci-lint enabled set: `errcheck`, `govet`, `staticcheck`, `revive`, `gosec`, `bodyclose`, `sqlclosecheck`, `rowserrcheck`, `errorlint`, `contextcheck`, `noctx`, `misspell`, `unparam`, `prealloc`, `gocritic`.
* Errors: wrap with `%w`; sentinel errors per package (`ingest.ErrEmptyFile`); map them to problem codes in one place (`httpapi/problems.go`).
* Every exported identifier has a doc comment. Pass `context.Context` first. No package-level mutable state. No `init()` except codegen.
* Don't panic in request paths. A recovery middleware converts panics to `500` and logs the stack.

**TypeScript:**

* Strict mode, no `any` (ESLint `@typescript-eslint/no-explicit-any: error`). Prefer function components and hooks. One component per file. Co-locate the CSS module.
* Format money and dates only through `lib/format.ts` (`Intl.NumberFormat('pl-PL')` for amounts in data; UI copy in English). All date math uses date-only helpers in `lib/dates.ts`, never local-time getters (legacy gotcha #5).

**SQL:**

* Lowercase keywords allowed; explicit column lists, no `SELECT *` in sqlc queries.
* Every query filters by `user_id`. Each migration file is reversible (`-- +goose Down`) where practical.

### 8.5 Git and CI workflow

* `main` is protected: PR required, CI green, linear history (squash merge). Tags `vX.Y.Z` trigger release images.
* `.github/workflows/ci.yml` (PR + push):
  * `backend`: lint, `make gen-check`, `go test -race ./...`, integration tests with Docker, `govulncheck`.
  * `web`: `npm ci`, lint, typecheck, `vitest`, `vite build`.
  * `e2e`: compose up, Playwright, upload the report as an artifact.
  * `helm`: `helm lint` + `kubeconform` (from Phase 16).
  * `security`: gitleaks, Trivy fs scan.
* `.github/workflows/release.yml` (tags):
  * build multi-arch (amd64/arm64) images with buildx and cache;
  * push to GHCR;
  * Trivy image scan (fail on CRITICAL);
  * SBOM (syft) attached;
  * cosign keyless sign;
  * open a PR bumping `deploy/environments/homelab/values.yaml` image tags (GitOps promotion).
* `.github/workflows/nightly.yml`: fuzz targets 60 s each, full E2E, dependency audit.
* Renovate config for Go modules, npm, GitHub Actions, Docker base images (pinned by digest) and the Helm chart deps.

---

## 9. Phased implementation plan

### Overview

| Milestone | Phases | Outcome |
|---|---|---|
| **M0 Foundations** | 0 Bootstrap · 1 Legacy extraction | Repo, guardrails, fixtures, legacy oracle |
| **M1 Backend core** | 2 Skeleton · 3 Ingest · 4 Schema · 5 Auth · 6 Imports · 7 Query API | A working API you can import into and query with curl |
| **M2 Frontend parity** | 8 FE foundation · 9 Import UX · 10 Table view + groups · 11 Insights · 12 Overview + investments · 13 AI assistant | Feature parity with legacy v7, multi-user |
| **M3 Quality gate** | 14 Legacy migration + E2E + parity harness | `v0.1.0` tagged |
| **M4 Product depth** | 15 Categories, rules, transfers, assistant actions, group reports (+ FX stretch) | Features the legacy app could not have |
| **M5 Production** | 16 Deploy (Helm, CNPG, Argo CD, LLM endpoint) · 17 Observability, load, security · 18 Homelab rollout | Running on the homelab with GitOps, backups, monitoring and alerts (`v0.2.0`) |
| **M6 Sharing and agents** | 19 Admin, themes, friends, shared groups · 20 Agentic assistant, advice library, CLI | Several people share expenses; the assistant can act (with consent and undo); agents use the app |
| **M7 Public release** | 21 Privacy clean-up, security CI, signed images, public demo, docs | `v1.0.0` in a public repository + public demo |

Each phase below lists: **Goal · Size · Read first · Tasks · Acceptance criteria · Gate**. Tasks are ordered. Each numbered task should be its own commit, or a few commits.

---

### Phase 0 — Repository bootstrap and guardrails · Size S

**Goal:** an empty but well-structured repo with agent instructions, docs, local Postgres and a CI skeleton. No application code yet.

**Read first:** this whole document once; `legacy/DESIGN.md` §1–§4.

**Tasks**

0.1 **Ask our Product Engineer the open questions** in §2.1. Record the answers in `docs/PROGRESS.md`, and the name and license in `docs/adr/0001-project-foundations.md`.

0.2 **Create the repo layout** from §3.3 (directories with `.gitkeep` where empty).
* Copy `finuance.html` → `legacy/finuance.html` and `Finuance-DESIGN.md` → `legacy/DESIGN.md`. Always copy the latest versions.
* **Scrub the legacy file before the first commit.** The `SAMPLE` constant contains what look like real account numbers (`'3212…`), card fragments and transaction references. Replace every digit run of length ≥ 8 with synthetic digits of the same length, and the merchant names with neutral ones.
* Verify with `tools/check-private.sh`. This script:
  * fails on any 26-digit IBAN-like number (`\b\d{26}\b`, optionally with `PL` prefix) in tracked files, except allowlisted all-zero patterns;
  * fails on any file under `testdata/private/`.

  Add it to `make check` and CI.

0.3 **Root files:**
* `README.md` (stub: one-paragraph pitch + "under construction"), `LICENSE`, `.editorconfig`.
* `.gitignore`: Go, Node, `dist/`, coverage, `.env*`, `testdata/private/`, Playwright reports.
* `.gitattributes`: **`testdata/** -text`** and **`*.csv -text`**, so git never rewrites line endings or encodings in fixtures; `*.sql text eol=lf`; `*.go text eol=lf`.

0.4 **Agent docs:**
* `CLAUDE.md` (Appendix A), `backend/CLAUDE.md` and `web/CLAUDE.md` (Appendix B).
* `docs/PLAN.md` (this file, verbatim), `docs/PROGRESS.md` (Appendix D).
* `docs/adr/0000-template.md` and ADRs 0001–0009 (list in Appendix C). Write them in full now: context, decision, consequences. They are short.

0.5 **Optional Claude Code project commands** in `.claude/commands/`:
* `phase-start.md`: read the PLAN phase + PROGRESS, enter plan mode, propose tasks;
* `phase-close.md`: run `make check` + `make test-integration`, update PROGRESS and PARITY, draft the PR description with the Gate checklist.

0.6 **`Makefile`** with the targets from Appendix F. Targets whose tooling doesn't exist yet print `not yet implemented (Phase N)` and exit 0.

0.7 **Local database:**
* `deploy/compose/compose.yaml` with a `db` service: official Postgres image pinned by version, a named volume, a healthcheck with `pg_isready`, and `deploy/db/bootstrap.sql` mounted into `/docker-entrypoint-initdb.d/` via a small wrapper that supplies passwords from env.
* `.env.example` with dev-only passwords.
* `make db-up`, `make db-down`, `make db-reset`, `make db-psql`.

0.8 **CI skeleton:** `.github/workflows/ci.yml` with a `guardrails` job (gitleaks + `tools/check-private.sh`) and placeholder `backend`/`web` jobs that succeed. `renovate.json` with conservative grouping.

**Acceptance criteria**
- [ ] `make db-up && make db-psql -c '\du'` shows `finuance_owner` and `finuance_app`; extensions `pg_trgm` and `citext` exist in `finuance`.
- [ ] `make check` passes. CI is green on the PR.
- [ ] `tools/check-private.sh` passes, and fails when a fake 26-digit number is added (prove it in the PR description).
- [ ] The ADRs read well on their own.

**Gate (our Product Engineer):** review the layout, `CLAUDE.md` and ADRs; confirm the GitHub owner and license; confirm the legacy scrub.

---

### Phase 1 — Legacy extraction: parity checklist, fixtures, oracle · Size M

**Goal:**
* turn the legacy app into **executable expectations**;
* get a safe, rich set of CSV fixtures.

**Read first:**
* `legacy/DESIGN.md` §6–§9, §14, §16, §26, §27;
* in the legacy source: `decode`, `detectDelim`, `parseCSV`, `buildTable`, `parseNum`, `parseDate`, `looseDate`, `analyzeCol`, `detectRoles`, `getDataset`, `compileCond`, `findRecurring`, `demoCSV`.

**Tasks**

1.1 **`docs/PARITY.md`:** a table of every legacy capability with columns:

| Feature | Legacy ref | New phase | Status (`todo` / `ported` / `retired: reason`) |
|---|---|---|---|

Cover:
* search and highlight;
* filters: every operator, the value checklist;
* sort, resize, hide;
* inspector actions, export, status-bar stats;
* each of the 6 pane charts and each of the 12 overview cards, plus every range setting (`flowMonths`, `paceCmp`, `payeeTop`, `payeeScope`, `heatRange`, `ovMonths`);
* the modal, the period presets, the keyboard shortcuts;
* theme, UI scale incl. auto, sidebar auto-collapse, drag & drop;
* demo/sample, persistence, lock, backup;
* **v7 features:**
  * the import settings window (every control);
  * groups: menu, form, inspector pills, quick bar, every multi-selection gesture;
  * the AI assistant: connection flow, settings, chat history, steps, Show in table, plan fallback, ⌘J;
  * the sidebar hidden by default; source labels;
* **Finuance-release features:**
  * the duplicates window (filter, paging, Use this version, Keep both / Merge again);
  * the Investment, Yield on cost and FI/RE cards with their editors;
  * Settings → Appearance; the keyboard shortcuts window (`?`);
  * the Columns *Hide all / Show all* toggle; the status-bar totals picker rule;
  * insights card ordering; the logo and favicon;
  * the backup format (for the migration);
* the known limitations from legacy §27 (each marked "fix in phase N" or "keep").

1.2 **Synthetic edge-case fixtures** in `testdata/fixtures/synthetic/`. Write each with exact bytes and record intent in `testdata/fixtures/README.md`. One fixture per case:
* UTF-8 with BOM; UTF-8 without BOM;
* Windows-1250 with Polish letters; ISO-8859-2;
* bank preamble (5 lines of account info before the header);
* trailing delimiters → empty unnamed columns;
* quoted fields containing delimiters, `""` and newlines;
* CRLF / LF / CR line endings;
* tab-delimited and pipe-delimited files;
* a comma-delimited file with quoted decimal commas;
* amounts `-1 234,56`, `1.234,56`, `1,234.56`, `12,00 PLN`, `€12`;
* `'`-prefixed account numbers;
* leading-zero ids and 16+ digit references (must stay text);
* duplicate header names; blank headers; ragged rows;
* ISO dates, `dd.mm.yyyy`, `dd/mm/yyyy`, dates with times, the invalid date `31.02.2025`;
* a debit/credit two-column file; a positive-expenses file (needs `invertSign`);
* an empty file; a header-only file;
* **v7 import-settings cases:**
  * padded preamble lines (`Bank export;;;`) that the fill-ratio rule must skip;
  * a footer summary line (`Saldo końcowe;1234,56`);
  * a header-less file;
  * a file whose header is on record 7;
  * month-first dates (`10/31/2025`);
  * a junk column to `ignore`;
  * debit/credit columns alongside a signed amount;
  * **two banks with different layouts** that map to the same canonical columns (merged-view parity);
* **duplicate-versions set** (Finuance): two overlapping exports of one account, where the same transactions carry different non-key fields (re-labelled category, changed operation type). Used for the duplicates window and the override rules;
* **overlap set** `overlap/A_jan-mar.csv`, `B_mar-may.csv` (overlapping March), `C_q1_again.csv` (same rows as A plus one genuine identical second purchase);
* **same-name set**: two different files both named `export.csv` in subfolders (legacy bug).

1.3 **Anonymiser** (Go CLI in the backend module: `backend/cmd/anonymize`). It anonymises our Product Engineer's real exports from `testdata/private/` into `testdata/fixtures/real/<bank>/`. Requirements:
* Preserve bytes structurally: encoding, delimiter, quoting style, line endings, preamble line count, column count, and the "NAME    CITY" multi-space padding pattern.
* **Deterministic and consistent across files in one run:** HMAC-SHA256 with a key from `--key` or `FINUANCE_ANON_KEY`. The same original value maps to the same fake value in every file, so overlap/dedupe behaviour is preserved.
* Rules:
  * digit runs of length ≥ 6 → same-length pseudo-random digits;
  * names and payees → fake merchant names from an embedded list, keeping casing style;
  * free text → words outside an allowlist of banking terms (`BLIK`, `PRZELEW`, `TRANSAKCJA`, `KARTĄ`, `PŁATNOŚĆ`, `REF`, `PLN`, …) replaced by fake words of similar length;
  * dates shifted by a constant `--shift-days`;
  * amounts multiplied by a constant factor in [0.7, 1.3] chosen by key, rounded to 2 dp, preserving the sign and number format;
  * category values kept (they are not personal).
* Tests run on synthetic "realistic" inputs and assert: consistent mapping across two files, round-trip encoding, no original token survives (search output for every original token), the same delimiter/line-ending/quote layout.
* **Claude Code builds and tests the tool on synthetic data only. Our Product Engineer runs it on real data** and reviews the output before committing.

1.4 **`tools/legacy-oracle`** (Node + Playwright; its own `package.json`):
* Copy `legacy/finuance.html` to a temp file. Inject `window.__legacy = { decode, detectDelim, parseCSV, buildTable, fileOpts, applyMapping, parseEntry, parseNum, parseDate, looseDate, analyzeCol, makeDataset, detectRoles, norm, payeeName, compileCond, getDataset, dupChanged, txKeys, applyGroupsAll, cardOrder, exportBackup, ser, deser, findRecurring, resolvePeriod, roundNice, demoCSV, aiData, aiFilter, execTool, sysPrompt, S };` **immediately before** the final `updateToggles();` statement of the IIFE. Assert the anchor exists exactly once and fail loudly otherwise.
* Launch headless Chromium with `bypassCSP: true`. Use a fresh context per fixture.
* For every fixture, pass the bytes into the page and run the legacy pipeline with default options (`enc=auto`, `delim=auto`, `strip=true`). Emit `testdata/oracle/ingest/<fixture>.json` containing:
  * `encoding`, `delimiter`, `headers`, `rows`;
  * per column: `type`, parsed numbers, parsed dates as ISO strings;
  * `roles`; `rowNorm`.
* **Value corpora** (`testdata/oracle/corpus/*.json`): for `parseNum`, `parseDate`, `looseDate`, `norm`, `payeeName`, at least 150 hand-written inputs each covering every branch, mapped to outputs. Output dates as ISO; `NaN` → `null`.
* **Dedupe:** for the overlap and same-name sets, build `S.files` via `parseEntry`, clear `S.cache`, call `getDataset('merged')`, and emit the kept rows (sorted canonical JSON) and `dupes`.
* **Filters:** on the demo dataset (`demoCSV()` output), evaluate a table of ≥ 60 `(legacyColumn, cond)` cases through `compileCond`. Emit matching row counts and the first 20 row indices. Include `tools/legacy-oracle/column-map.json`, which maps legacy demo headers to the new canonical columns.
* **Recurring and periods:** `findRecurring` and `resolvePeriod` for each preset on the demo dataset.
* **Per-file settings (v7):** for the v7 fixtures, run `parseEntry` with explicit `e.cfg` variants:
  * header row N;
  * none + skip;
  * `dropShort` on/off;
  * `dateOrder: 'mdy'`;
  * invert;
  * a full `map` including debit/credit and ignore.

  Emit the same ingest JSON plus `headerAt` and `dropped`. Record the exact cfg used in each output, so the Go tests can replay it.
* **Assistant tools:**
  * Assign a fixed set of groups on the demo dataset via `S.groups`/`S.tags` + `applyGroupsAll()`.
  * Run `execTool` for ≥ 60 argument sets covering every filter, every sort, every `group_by` and both `find_values` fields.
  * Emit the `result` objects, with row ids mapped to stable row indices.
  * Also emit `sysPrompt(aiData())` with today's date frozen through Playwright's clock API. It is the reference for the Go prompt builder.
* **Group keys:** `txKeys` for the overlap fixtures, to confirm that the `#n` twin rule matches `occurrence`.
* **Duplicates and overrides (Finuance):** for the overlap and duplicate-versions sets:
  * emit `ds.dup` (groups, versions, kept flags) for the default rule;
  * repeat with `S.dupPick` (prefer a file) and with `S.dupKeep` (keep both), rebuilding via `dupChanged` each time.
* **Investments:**
  * set `S.inv` to 5 fixture portfolios: all-zero, typical, no annual income (→ transaction fallback), short history (< 12 months), and FI not reachable;
  * render the overview for 2 months;
  * emit the table twins (`c.table`) of `o_inv`, `o_yoc` and `o_fire`.
* **Legacy backups:**
  * load the demo data plus groups, budgets, an opening balance, `dupPick`/`dupKeep`, `S.inv` and two chats;
  * run `exportBackup()` twice: plain, and with the known test passphrase `oracle-test-passphrase` set as a lock;
  * capture both downloads into `testdata/fixtures/legacy-backup/`. They drive the migration tests (Phase 14).
* **Verify the names.** The function names for the Finuance features come from the design doc. Check each against the current `legacy/finuance.html`, and record renamed or missing ones in `ALLOWED_DIFFS.md`.
* Save the legacy demo CSV text to `testdata/fixtures/demo/demo-legacy.csv`.
* Write deterministic JSON (sorted keys, 2-space indent). `make oracle` regenerates everything. CI job `oracle-fresh` runs it and fails on `git diff`.

1.5 Write `testdata/oracle/ALLOWED_DIFFS.md`. It starts with the known **[CHANGE]** items from §4: invalid dates, per-import dedupe buckets, ISO dates instead of epoch ms. Every future allowed difference needs a line here with a reason.

**Acceptance criteria**
- [ ] `docs/PARITY.md` covers all legacy capabilities (our Product Engineer spot-checks against the legacy UI).
- [ ] ≥ 35 synthetic fixtures; the overlap and same-name sets exist.
- [ ] `make oracle` runs headless, is deterministic (two runs, zero diff) and produces all the outputs above.
- [ ] The anonymiser's tests pass; the tool documents its usage in `backend/cmd/anonymize/README.md`.

**Gate (our Product Engineer):**
1. Run the anonymiser on real exports from each bank.
2. Manually check that no real names or numbers remain.
3. Commit the `real/` fixtures.
4. Re-run `make oracle`.

---

### Phase 2 — Backend skeleton · Size M

**Goal:** a production-shaped Go service with config, logging, metrics, health, the OpenAPI pipeline, migrations, sqlc, a test harness and a container image. No business features yet.

**Read first:** §2, §3.3–§3.5, §6.1, §8.2, §8.4; `backend/CLAUDE.md`.

**Tasks**

2.1 `go mod init github.com/<owner>/finuance/backend`. Pin `go` and `toolchain`.
* `cmd/finuance/main.go` dispatches the subcommands `serve`, `worker`, `migrate {up|down|status|version}`, `seed-demo`, `admin`, `healthcheck`, `version`, using stdlib `flag` with one FlagSet per subcommand.
* Build info via `-ldflags` (`version`, `commit`, `date`).

2.2 `internal/config`: typed config with defaults from §3.5. Validation returns all errors at once. Tests cover the defaults, missing required values, and invalid durations or URLs.

2.3 `internal/observability`:
* slog setup (JSON in prod, text in dev; level from config);
* the Prometheus registry and HTTP metrics middleware (route pattern label, not raw path);
* the OTel tracer provider (noop when no endpoint);
* an admin server on `FINUANCE_ADMIN_ADDR` with `/metrics` and `/debug/pprof/*`.

2.4 `internal/httpapi`:
* server with timeouts (ReadHeader 5 s, Read 30 s, Write 60 s, Idle 120 s), `MaxHeaderBytes` of 64 KiB, and `http.MaxBytesReader` defaults;
* middleware chain: recover → request ID → real IP (trusted proxies) → logging → metrics → tracing → security headers → (auth, CSRF later);
* `/healthz` and `/readyz`;
* graceful shutdown on SIGINT/SIGTERM: stop accepting, drain for 20 s, close the pool.

2.5 `httpapi/problems.go`: the problem+json writer, the domain error → code/status mapping table, and validation-error aggregation. Unit tests for each mapping.

2.6 `api/openapi.yaml` v0: `info`, `servers`, components (`Problem`, `Money`, `Meta`), and `GET /api/v1/meta` → `{version, signupEnabled, demoMode}`.
* `backend/oapi-codegen.yaml`: `std-http-server`, `strict-server`, `models`, `embedded-spec`, output `internal/httpapi/gen/gen.go`.
* Wire the OpenAPI request-validation middleware.
* Add `make gen` and `make gen-check`, which regenerates and fails on `git diff`.

2.7 `internal/store`:
* `pgxpool` with config, the OTel tracer hook, a ping at startup, pool metrics.
* `migrations/` embedded via `embed.FS`; `finuance migrate` uses `FINUANCE_MIGRATE_DATABASE_URL`, runs goose, then River migrations (`rivermigrate`).
* Add `00000_baseline.sql` (empty, with a comment) so the pipeline is exercised.

2.8 `backend/sqlc.yaml`:
* engine `postgresql`, `sql_package: pgx/v5`, `emit_interface: true`, `emit_pointers_for_null_types: true`;
* overrides: `uuid` → `github.com/google/uuid.UUID`, `date` → the date type chosen in ADR-0005.
* Add one trivial query (`SELECT 1`) to prove the pipeline. `make gen` includes sqlc.

2.9 `testutil`: a testcontainers Postgres helper, started once per package in `TestMain`. It runs `bootstrap.sql` as superuser, migrations as owner, and returns an app-role pool, an owner pool and `t.Cleanup` truncation helpers.
* `make test` = unit tests only (`-short`); `make test-integration` = all tests.

2.10 `deploy/docker/Dockerfile.backend`:
* multi-stage: `golang` builder with `--mount=type=cache` for modules and build cache, `CGO_ENABLED=0`, `-trimpath`;
* final stage: distroless static (current Debian variant, `:nonroot` tag), pinned by digest;
* `USER nonroot`;
* `EXPOSE 8080 9090`;
* entrypoint `finuance`.

  Distroless has no shell, so the compose healthcheck uses `finuance healthcheck`, which GETs `/healthz`.

2.11 Compose:
* services `migrate` (one-shot, `depends_on: db healthy`) and `api` (`depends_on: migrate completed_successfully`);
* `make up` / `make down` / `make logs`.

2.12 Fill the CI `backend` job:
* `golangci-lint` (pinned version);
* `make gen-check`;
* `go test -race -short ./...`;
* `go test -race ./...` with Docker (integration);
* `govulncheck ./...`;
* `docker build` (no push).

**Acceptance criteria**
- [ ] `make up` → `curl -s localhost:8080/api/v1/meta` returns JSON; `/readyz` is 200; `curl localhost:9090/metrics` shows the HTTP metrics.
- [ ] An invalid env var stops startup with a clear message naming the variable.
- [ ] SIGTERM during a slow test request finishes the request (test with a test-only handler).
- [ ] The image is ≤ 30 MB, runs as non-root, and the Trivy scan shows no HIGH/CRITICAL.
- [ ] `make check` and `make test-integration` are green locally and in CI.

**Gate (our Product Engineer):** run `make up`, hit the endpoints, read the middleware chain and `Makefile`.

---

### Phase 3 — Ingest engine (pure Go) · Size L

**Goal:** a faithful, tested, fuzzed Go port of the legacy parsing and normalisation logic. This is the part interviewers will ask about, so make it excellent.

**Read first:** §4.1–§4.4; legacy functions listed in Phase 1; `testdata/oracle/`.

**Tasks** (each with table tests + oracle differential tests where applicable)

3.1 `ingest/normalize.go`: `Norm`, `Collapse`, `PayeeName`. Oracle corpus tests.

3.2 `ingest/decode.go`: auto + manual encodings, BOM handling, reporting the encoding name.

3.3 `ingest/delimiter.go`: `DetectDelimiter` with quote-aware counting, tie-break order and default.

3.4 `ingest/csv.go`: a tokenizer that is a state machine over runes, faithful to the legacy one. It is **not** `encoding/csv`, which differs on edge cases. Return `[][]string`.

3.5 `ingest/table.go`: `Shape(raw [][]string, opts) (*Table, error)` with header detection, padding, trimming, stripping `'`, empty-column dropping, naming and de-duplication.

3.6 `ingest/values.go`: `ParseNumber`, `ParseDate`, `LooseDate`.
* `money/parse.go`: `ParseMinor(s, exp)`, exact and float-free, plus `FormatMinor` (for fingerprint `amountStr`). Property test: `ParseMinor(FormatMinor(x)) == x`.

3.7 `ingest/columns.go`: `AnalyzeColumn`, `DetectRoles` (legacy regexes + the new roles in §4.2). Tests per heuristic, plus fixtures for the new heuristics.

3.8 `ingest/stage.go`: `Stage(table, mapping, opts, accountCurrency) ([]StagedRow, Stats)`:
* canonical fields;
* `raw` ordered pairs;
* errors per row (missing or unparseable date/amount);
* `invertSign`;
* `debit_credit` mode;
* currency resolution;
* `searchText`;
* the `_n` shadow fields;
* `payeeGroup`.

3.9 `ingest/fingerprint.go`: `FingerprintKey(row)`, `Fingerprint(key) [32]byte`, `AssignOccurrences(rows)`, and `HeaderSignature(headers, delim)`.

3.10 `ingest/parse.go`: the top-level `Parse(raw, opts) (*Result, error)` composing the steps. It enforces limits (200k rows, 200 columns) and checks `ctx` for cancellation every 10k rows.

3.11 **Differential suite** `ingest/oracle_test.go`:
* loads every `testdata/oracle/ingest/*.json` and compares with `Parse` output;
* loads corpora for the value parsers;
* for overlap and same-name sets, runs `Stage` + `AssignOccurrences` per file and simulates the unique constraint in memory. The kept multiset must equal the oracle output, apart from documented allowed differences;
* produces a readable diff (go-cmp) on failure.

3.12 **Fuzz targets:** `FuzzTokenize`, `FuzzParseNumber`, `FuzzParseMinor`, `FuzzParseDate`, `FuzzParse` (whole pipeline). Seed from the fixtures. Invariants:
* no panic;
* row widths are consistent after `Shape`;
* `ParseMinor` never returns a value whose formatting re-parses differently.

3.13 **Golden tests:** `testdata/golden/ingest/<fixture>.json` (the full `Result`), with `-update` to regenerate.

3.14 **Benchmarks:** `BenchmarkParse_100k` (generate 100k rows from the demo generator). Target ≤ 500 ms and allocations reported. Record results in `docs/perf.md`.

3.15 **v7 import-settings parity:**
* header modes: auto with the fill-ratio rule, explicit row, none + skip;
* footer drop with `headerAt`/`dropped` reporting;
* month-first dates;
* the `ignore` role;
* debit/credit, incl. the "signed amount also mapped" rule;
* mapping by column index;
* the exact-canonical-name preference in `DetectRoles`.

Each is differential-tested against the Phase 1 per-file-settings oracle outputs, replaying the recorded cfg.

**Acceptance criteria**
- [ ] All oracle comparisons pass, except entries in `ALLOWED_DIFFS.md`.
- [ ] Each fuzz target runs 5 minutes locally without a crash. Any crasher found is added to the seed corpus with a fix.
- [ ] `ingest` + `money` line coverage ≥ 90 %.
- [ ] No `float64` in `money` or in the fingerprint path (`grep` check in CI).
- [ ] Benchmark numbers are in `docs/perf.md`.

**Gate (our Product Engineer):** review `ALLOWED_DIFFS.md`, the fingerprint code, and a sample golden file for a real bank fixture.

---

### Phase 4 — Schema, store layer, RLS · Size M

**Goal:** the database schema from §5, the `WithUser` pattern, sqlc queries, and proven tenant isolation.

**Read first:** §5 in full; §4.4.

**Tasks**

4.1 Migrations `00001_core.sql` (tables, indexes and the collation exactly as in §5.2) and `00002_rls.sql` (§5.3). Include `-- +goose Down` sections.

4.2 `store.WithUser` (§5.3) and `store.WithUserReadOnly` (`BEGIN READ ONLY`). Add a lint-like test that greps `internal/` for `pool.Query`/`pool.Exec` outside `store` and fails if any are found.

4.3 sqlc queries in `store/queries/`: `users.sql`, `sessions.sql`, `preferences.sql`, `accounts.sql`, `categories.sql` (upsert by `name_n` returning id), `imports.sql`, `import_files.sql`, `transactions.sql` (detail, patch, delete orphans), `budgets.sql`, `profiles.sql`.

4.4 **Staging insert:** `InsertImportRows` uses `INSERT … SELECT * FROM unnest($1::int[], $2::bytea[], …)`.
* **Do not use `COPY FROM`.** PostgreSQL does not support `COPY FROM` into tables with row-level security enabled.
* Batch 5,000 rows per statement.
* Benchmark 100k rows.

4.5 Domain repositories in `internal/ledger` that wrap sqlc and return domain types.

4.6 Tests:
* migrate up → down → up;
* every query against a real DB;
* **RLS suite**: create users A and B with data; with `app.user_id = A`, a test-only query **without** a `user_id` predicate returns only A's rows; update/delete of B's rows affects 0 rows; insert with `user_id = B` fails `WITH CHECK`; with no `app.user_id` set, user tables return 0 rows.
* collation sort test (Polish letters and numeric-aware ordering).

4.7 `docs/ARCHITECTURE.md`: start it with the component diagram (§3.1), a Mermaid ERD of §5.2, and the RLS explanation.

4.8 **Groups and annotations (§4.4, §4.7):**
* `groups` and `tx_annotations` tables and the RLS policies;
* `ledger.Annotate(ctx, txIDs, patch)` writes `transactions` + `tx_annotations` in one DB transaction;
* commit step 1b re-applies annotations.

Integration test: annotate → delete the import (orphan cleanup) → re-import → the annotation is restored. Also test that a fully cleared annotation row is deleted.

4.9 **Overrides and investments schema:**
* `dedupe_overrides`, `investment_profile`, `investment_positions`, with RLS and tests;
* `split_scope` and `source_import_id` on `transactions`, `import_rows` and `tx_annotations`.

**Acceptance criteria**
- [ ] `make test-integration` green, including the RLS suite.
- [ ] `make gen-check` clean.
- [ ] Inserting 100k staged rows takes ≤ 3 s on a dev machine (recorded in `docs/perf.md`).

**Gate (our Product Engineer):** read the migrations and the RLS tests.

---

### Phase 5 — Auth, users, preferences · Size M

**Goal:** secure multi-user accounts with sessions, CSRF protection, rate limiting and the preferences document.

**Read first:** §6.1, §6.2 (Auth & Preferences), §8.1.

**Tasks**

5.1 `auth/password.go`: argon2id hashing in PHC string format; parameters from config with defaults (§8.1); `Verify` with constant-time compare; `NeedsRehash`. Benchmark the chosen parameters and record them in ADR-0010 (password hashing).

5.2 `auth/sessions.go`:
* create: 32 random bytes, base64url cookie, SHA-256 stored;
* resolve: cookie → hash → join users; check absolute expiry and idle expiry; update `last_seen_at` at most every 5 min;
* revoke; revoke-all-except-current;
* rotate on login and on password change.

5.3 Middleware:
* `RequireSession` puts `UserID` into the context;
* `CSRF` (§6.1) on unsafe methods;
* `RateLimit` (per-route policies);
* the cookie helper sets attributes according to `FINUANCE_ENV`/`FINUANCE_COOKIE_SECURE`.

5.4 OpenAPI: add the auth, `me` and preferences paths and schemas. Regenerate and implement the handlers.
* Signup validation: email format, password ≥ 12 characters (no composition rules), display name ≤ 80.
* An unknown email still runs a dummy argon2 verify, so timing stays uniform.

5.5 `DELETE /me` (password confirmation, cascade). Wire the `GET /me/export` stub; it is implemented in Phase 7.

5.6 **River setup** (worker process):
* the River client in `finuance worker`;
* the job registry in `internal/jobs`;
* periodic job `CleanupExpiredSessions` (hourly);
* graceful shutdown of the River client.

  `finuance serve` gets an insert-only River client for transactional enqueue.

5.7 `finuance admin create-user --email … --password-stdin`, for instances with signup disabled.

5.8 `docs/THREAT-MODEL.md` v1: assets, actors, trust boundaries, STRIDE table referencing the §8.1 controls.

5.9 `scripts/auth-smoke.sh`: a curl script for signup → me → logout → login → change password → old session invalid.

**Tests:**
* full HTTP flows;
* cookie attributes in dev vs prod;
* CSRF rejections (missing header, wrong `Origin`);
* `429` after the limit, with `Retry-After`;
* session expiry and idle expiry (fake clock);
* rotate-on-login;
* preferences `409` on a stale version;
* delete-me removes all rows (count every table);
* the access log never contains the password or cookie (capture logs in a test).

**Acceptance criteria**
- [ ] All tests green; `auth-smoke.sh` passes against `make up`.
- [ ] THREAT-MODEL v1 committed.

**Gate (our Product Engineer):** run the smoke script; skim the threat model.

---

### Phase 6 — Accounts and import pipeline · Size L

**Goal:** the full import lifecycle (§3.2, §4.3, §4.4) over HTTP, with the commit running as a River job.

**Read first:** §3.2, §4.3, §4.4, §6.2 (Accounts, Imports), §7.10 (to understand the UI needs).

**Tasks**

6.1 Accounts CRUD (OpenAPI + handlers + `ledger.Accounts`). `txCount`, `firstOn` and `lastOn` come from an aggregate query. Delete cascades.

6.2 `POST /imports`:
1. multipart streaming with a per-file limit;
2. NUL sniff;
3. SHA-256;
4. duplicate check (`409 duplicate_file`);
5. `ingest.Parse` with auto options, or with the options of a matching profile (signature computed after a first parse with auto options; if a profile matches, re-parse with its options);
6. `DetectRoles` → mapping (`mappingSource`);
7. account suggestion (§4.4);
8. `Stage`;
9. in one transaction: insert `imports`, `import_files` and staged `import_rows`;
10. duplicate estimate: count of staged `(fingerprint, occurrence)` already in `transactions` for the suggested account, if any;
11. respond `ImportPreview` with the first 50 staged rows.

6.3 `GET /imports`, `GET /imports/{id}`, `GET /imports/{id}/rows` (with `onlyErrors`, and a `duplicate` flag computed per row against the chosen account), `GET /imports/{id}/file`.

6.4 `PATCH /imports/{id}` (only while `previewed`): apply the changes to account, mapping and options. Re-parse from the stored bytes if encoding, delimiter or stripQuote changed. Re-stage in one transaction (delete staged rows, insert new ones). Return the new preview. Validation: mapped headers exist, required roles are present, and debit/credit columns are set when `amountMode=debit_credit`.

6.5 `POST /imports/{id}/commit`:
* validates: `previewed`, account set, required roles present, at least one non-errored row;
* in one transaction: status → `committing` + `river.InsertTx(CommitImportArgs{ImportID, UserID, RememberMapping})`;
* returns `202`;
* a repeated call while `committing` or `committed` → `409 import_not_committable`.

6.6 `jobs/commit_import.go`, inside `WithUser`:
1. Upsert categories for the distinct non-empty `bank_category` values → map name to id.
2. Run the §4.4 commit SQL.
3. Link the rows.
4. Compute the stats.
5. Update the import (status, stats, `committed_at`).
6. Upsert the import profile if requested.
7. Enqueue follow-up jobs. None exist yet; keep the hook.

Retries: max 3 attempts with backoff. On the final failure, set status `failed` and store a sanitised error. Metrics: `finuance_imports_total`, `finuance_import_rows_total`, commit duration.

6.7 `DELETE /imports/{id}`: delete the import rows, then orphaned transactions (§4.4), in one transaction. Return `204`.

6.8 `POST /imports/{id}/reprocess` (committed imports): re-parse with the new mapping/options → re-stage → delete orphans → commit via the same job.

6.9 `finuance seed-demo --email …`: port of legacy `demoCSV` into `internal/demo`:
* seeded RNG (Park–Miller `seed = seed*16807 % 2147483647`, seed 7);
* the same categories, merchants, probabilities and amount ranges;
* the date range shifted so the latest date = yesterday.

It is used by `POST /demo-data` (wired in Phase 8) and by load tests. A unit test confirms it reproduces `testdata/fixtures/demo/demo-legacy.csv` byte-for-byte when given the legacy date range.

6.10 **v7 import settings end to end:**
* `headerRow`, `skipRows`, `dropShort`, `dateOrder`, the `ignore` role and column-index mapping all flow through POST, PATCH (re-parse) and profiles.
* `GET /imports/{id}/lines` returns the first N decoded records with their status.
* The preview adds `headerAt`, `dropped` and `tailRows`.

6.11 **Fast path:** `POST /imports?autoCommit=true`. It commits immediately when:
* the mapping came from a profile;
* there are no row errors;
* the account is resolved (explicit, or a unique number-hint match).

Otherwise it behaves like a normal preview. Tests cover both paths.

6.12 **Source labels:** store `header_signature` on each import. `GET /imports` returns `profileLabel` (`b1`, `b2`, … by first-seen order per user) when the user has more than one profile.

6.13 **Duplicate review backend (§4.4):**
* `source_import_id` on every insert;
* `Rematerialize(account, fingerprint)`;
* override handling in commit (step 2b) and in delete-import;
* job `RematerializeFingerprint`;
* the `duplicatesSkipped` count.

The HTTP endpoints and the dialog come in Phase 10. **Property test (§8.3).** Unit and integration tests:
* *prefer* on a re-labelled category changes the transaction's fields;
* *keep_both* yields one transaction per import, and clearing it merges them again;
* deleting the preferred import falls back to the default source;
* annotations survive every one of these.

**Tests:**
* **Overlap scenario:** import A, B, C into one account. Totals and per-row multiplicity equal the oracle's merged result. Two genuine identical purchases inside C are both kept.
* **Same-name files** are imported as distinct imports, with no collision.
* **Delete semantics:** delete A → transactions only in A disappear; those also in B or C remain. Delete everything → 0 transactions.
* **Concurrency:** commit two overlapping imports concurrently (two goroutines, two jobs) → no duplicate transactions; stats add up.
* **Manual edits survive reprocess:** set a category manually, reprocess with the same mapping → the category is kept.
* **Profiles:** the second upload of the same layout gets `mappingSource=profile`.
* Errored rows are excluded and reported. A `409` on an identical file. Validation errors on bad mapping.
* Job retry → failed status after the final attempt (inject a failure).

**Acceptance criteria**
- [ ] `scripts/import-smoke.sh` uploads the overlap fixtures, commits, prints the stats, deletes one import and prints the remaining count. The numbers match the oracle.
- [ ] All tests green; metrics visible.

**Gate (our Product Engineer):** run the smoke script; inspect a preview JSON for a real-bank fixture.

---

### Phase 7 — Transactions query API · Size L

**Goal:** server-side search, filter, period, sort, paging, stats, distinct values, detail and edit, export. All of it shares one compiler.

**Read first:** §4.5, §4.6 (colours), §6.2 (Transactions, Categories), §7.7 (UI needs: windows, `locateId`, distinct).

**Tasks**

7.1 `query/dsl`: lexer + parser → AST `{Neg bool; Kind: range|op|bare; Op; A, B string}`. Pure, fuzzed (`FuzzParseCond`), round-trip printable.

7.2 `query/compile.go`:
* `Compile(userID, ctx QueryContext, cols ColumnRegistry) (*Compiled, error)` produces `{baseWhere string, args []any}`.
* `ColumnRegistry` holds the §4.5 whitelist with the type, value expression and normalised expression.
* Semantics as in §4.5 (number → minor units; date → LooseDate intervals; text fallback; `excluded`; search tokens).
* Escape `LIKE` patterns.
* Return validation errors for unknown columns.

7.3 `query/period.go`: anchor query (`max(booked_on)` over the accounts, ignoring filters), the presets, `prevRange`, labels (English; `Mar 2025`, `Last 3 months`, `Year to date 2025`).

7.4 `query/sql.go`: the CTE builder used by **every** read endpoint:

```sql
WITH base AS (
  SELECT t.*, c.name AS category_name, c.name_n AS category_n, a.name AS account_name, a.name_n AS account_n
  FROM transactions t
  JOIN accounts a ON a.id = t.account_id
  LEFT JOIN categories c ON c.id = t.category_id
  WHERE t.user_id = $1 AND <accounts> AND <filters> AND <search> [AND NOT t.is_transfer]
), scoped AS (
  SELECT * FROM base WHERE booked_on >= $p_from AND booked_on < $p_to     -- or base when no period
)
```

Sorting uses `COLLATE pl_natural`, `NULLIF(…,'') NULLS LAST`, and the tie-breakers (§4.5).

7.5 `POST /transactions/query`: rows window (`OFFSET/LIMIT`), `total` (`count(*)` over scoped), `stats` (sum/avg/min/max of `statColumn`, i.e. amount or balance after, over scoped), `duplicatesSkipped` (the context's accounts, ignoring filters), `dataRange` (min/max over base), resolved period.
* With `locateId`: compute `row_number() OVER (<sort>)` in a subquery to find the offset, return `locatedOffset`, and return the window containing it.

7.6 `POST /transactions/distinct`: the same context **minus that column's own filter**, `GROUP BY` display value, counts, ordered by count desc then value (collation), limit 600, optional search inside the values.

7.7 `GET /transactions/{id}`: detail + `raw` + sources (imports with filename/date). `PATCH /transactions/{id}`:
* `categoryName` → upsert the category;
* `categorySource='manual'`;
* note + `note_n`;
* `updated_at`.

7.8 `GET /categories?accountIds=` with `txCount` and `colorSlot` (rank by all-time expense over those accounts; top 7 get slots 1–7).

7.9 `POST /transactions/export`: streams CSV from a server-side cursor in batches.
* UTF-8 BOM, configurable delimiter (default `;`), the visible columns in the given order, amounts formatted with comma decimals for Polish Excel compatibility (ADR), dates as `dd.mm.yyyy` (matching the legacy export).
* CSV-injection guard (§8.1).
* Filename `finuance-<view>-<yyyy-mm-dd>.csv`.

7.10 `GET /me/export`: a zip stream with JSON, CSV and original import files.

**Tests:**
* **Filter parity:** load the demo CSV into an account via the real import path. For every oracle filter case, the result count and the first 20 rows (mapped) match.
* **Injection:** hostile conditions and search strings only ever appear as args (assert the compiled SQL has no user text); fuzz the compile path against a real DB for 60 s (nightly).
* **Sort:** Polish collation order, numeric awareness, empties last for each column type, a stable tie-break.
* **Paging:** concatenating windows equals the full ordered result; `locateId` lands on the right row.
* **Stats** equal a Go recomputation; **distinct** ignores its own column filter.
* **Export:** BOM, delimiter, injection guard, column order.
* **Tenancy:** every endpoint returns 404 for another user's ids.

7.11 **Performance:** seed a user with 100k rows (demo generator × N accounts).
* `EXPLAIN (ANALYZE, BUFFERS)` the main query shapes: default sort, payee filter, search, category filter + period.
* Add or adjust indexes if needed.
* Record the plans and timings in `docs/perf.md`. Target p95 ≤ 150 ms on a dev machine.

7.12 **Rev 2 additions:**
* the `group` filter column (LEFT JOIN groups);
* `QueryContext.toolFilter` compiled from the typed `ToolFilter` (§4.8), shared by the assistant tools and the table chip;
* `POST /transactions/ids`.

Tests for each, including oracle parity for `aiFilter` semantics.

**Acceptance criteria**
- [ ] Filter parity suite green.
- [ ] `docs/perf.md` updated with the plans.
- [ ] `make check` and integration tests green.

**Gate (our Product Engineer):**
* run `scripts/query-smoke.sh`: a few curl queries with filters and search;
* look at one `EXPLAIN` plan;
* open an exported CSV in Excel or LibreOffice.

---

### Phase 8 — Frontend foundation · Size L

**Goal:** the React app shell, which looks like the legacy app, with auth, preferences sync, settings, demo data, and the production web image.

**Read first:** §7.1–§7.6, §7.11; `legacy/DESIGN.md` §3, §11, §12, §19–§22; legacy `<style>` block; `web/CLAUDE.md`.

**Tasks**

8.1 **Scaffold** `web/` with Vite + React + TS:
* configs per §7.2 (tsconfig strict flags, ESLint flat config, Prettier, Vitest with jsdom, Playwright config pointing at `E2E_BASE_URL`);
* Vite dev server proxies `/api` → `http://localhost:8080`;
* `make dev-web`.

8.2 **API client:**
* `make gen` also runs `openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts`;
* `src/api/client.ts`: `openapi-fetch` with base `/api/v1`, `credentials: 'same-origin'`, default header `X-Finuance-Client: web`, and a response middleware that normalises errors into a `Problem` type and routes `401` to `/login?next=`;
* typed hooks per resource in `src/api/hooks/`.

8.3 **Styles:**
* `tokens.css`: verbatim port of the legacy custom properties;
* `base.css`: port the legacy base element styles (body, scrollbars, focus ring, inputs, buttons, `.ib`, `.on`, kbd, mark) converted to rem;
* `scale.ts`: `uiScale` + auto heuristic port with unit tests;
* `theme.ts`: system/dark/light + OS listener.

8.4 **Icons and brand:** port the legacy `ico()` inline SVG set to `ui/Icon.tsx` (same paths, `currentColor`). Port the logo (`ICONS.book`: a purple gradient tile with a white book-with-$) as `ui/Logo.tsx` and `web/public/favicon.svg`; `<title>Finuance</title>`.

8.5 **UI primitives:** `Popover`, `Menu`, `Toast` (+ queue store), `Tooltip`, `Modal`, `Button`, `IconButton`, `Select`, `Checkbox`, `TextInput`, `Kbd`. Match the legacy look; screenshot each in a dev-only `/__ui` route (excluded from prod builds).

8.6 **Shell:**
* `App` grid (titlebar / main / statusbar, using `minmax(0,1fr)` so wide content never overflows the page);
* `TitleBar`, `Sidebar` (accounts from `GET /accounts`, "All accounts", auto-collapse logic), `StatusBar` (placeholder content), `EmptyState`, `DropOverlay` (visual only for now);
* a global keyboard manager with input-aware guards.

8.7 **Auth:**
* `Login` and `Signup` pages (signup only when `meta.signupEnabled`);
* user menu with Logout;
* `RequireAuth` with the prefs-load gate.

8.8 **Stores:**
* `uiStore`, `viewStore`;
* `usePreferencesSync()`: hydrate from `GET /preferences`; subscribe to the persisted slices; debounced `PUT`; `409` handling (§7.4).

  Unit tests for hydration, debouncing and conflict merging.

8.9 **Settings pages:**
* appearance (theme System/Dark/Light, interface size incl. Auto); also inline in the `SettingsPopover` (8.14);
* profile (display name, change password);
* sessions (list, revoke);
* data (download export, delete account with password confirmation; the legacy backup import is added in Phase 14).

8.10 **Demo data:**
* backend `POST /demo-data`: creates a "Demo account" (`is_demo`) and imports the generated demo year through the normal pipeline (preview → commit job), returning ids;
* frontend `EmptyState` "Load demo data" button → toast → navigate to the demo account.
* A demo account can be deleted like any other account.

8.11 **Web image** `deploy/docker/Dockerfile.web`: Node builder → `nginxinc/nginx-unprivileged` (pinned). `deploy/docker/nginx.conf`:
* SPA fallback to `index.html`;
* `index.html` `no-cache`; hashed assets `immutable, max-age=31536000`;
* gzip;
* security headers incl. the CSP from §8.1;
* `location /api/` proxy to `api:8080` (Compose variant via env-substituted template).

Compose service `web` on `localhost:8000`. `make up` now serves the full app. The dev compose file keeps the api (`:8080`) and admin (`:9090`) ports published on localhost for debugging; `compose.prod.yaml` publishes only web.

8.12 CI `web` job: `npm ci`, lint, typecheck, `vitest run`, `vite build`, and the bundle size report as a job summary.

8.13 **E2E plumbing:**
* `deploy/compose/compose.ci.yaml`: builds the images, uses a fresh DB, `FINUANCE_ENV=test`, and exposes web on `:8000`;
* the CI `e2e` job (compose up → wait for `/readyz` → `npx playwright test` → upload the report);
* first spec: signup → load demo data → the sidebar shows the Demo account.

8.14 **Settings popover and keyboard window:**
* `SettingsPopover` (scrollable): Appearance inline, *Keyboard shortcuts*, links to the settings pages, Sign out;
* `KeyboardShortcuts` dialog: port the `KEYS` table; shows ⌘ on macOS and Ctrl elsewhere; opened from settings or with `?`.

**Acceptance criteria**
- [ ] Signup → login → logout works in the browser via `make up` at `http://localhost:8000`.
- [ ] Theme and UI size persist across reloads and across two browsers for the same user.
- [ ] "Load demo data" creates the Demo account and shows it in the sidebar with a count.
- [ ] Side-by-side screenshots (legacy vs new) of the empty shell in dark and light themes are attached to the PR.
- [ ] `make check` green, including the web lint/type/test steps.

**Gate (our Product Engineer):** click through the shell and compare it with the legacy app.

---

### Phase 9 — Import UX · Size M

**Goal:** the complete import experience from §7.10.

**Read first:** §7.10, §6.2 (Imports), §4.3–§4.4.

**Tasks**

9.1 Entry points:
* drag & drop anywhere (`DropOverlay` with the depth counter);
* the sidebar mini drop zone;
* ⌘/Ctrl O and the empty-state *Browse* button (hidden `<input type=file multiple accept=".csv,.txt,.tsv">`).

9.2 Upload: one request per file. A queue store holds `[{file, status, importId?, error?}]`; the stepper shows "1 of N". Handle a `409 duplicate_file` with a toast and a link to the existing import.

9.3 `/imports/:id` **Preview page:**
* `FileFacts` (encoding/delimiter selects);
* `AccountPicker` (suggested, existing, create inline → `POST /accounts`);
* `MappingEditor` (role rows, header dropdowns with type badges, detected/profile badges, required markers, inline validation from problem `errors[]`);
* `ImportOptions` (strip `'`, invert sign, amount mode with debit/credit pickers);
* `ImportStats`;
* `SampleGrid` (reuses Grid cell renderers, highlights errored rows);
* `ErrorsDrawer` (paged `onlyErrors`);
* `CommitBar` (Cancel, remember mapping, Import N transactions).

All edits → debounced `PATCH`; the screen renders from the server response.

9.4 Commit flow: `POST commit` → poll → toast "Imported X transactions · Y duplicates skipped" → invalidate queries → navigate to `/t/:accountId`. On failure, show an inline error with a "Try again" button (re-commit).

9.5 Sidebar import history per account: filename, date, status badge, new/dup counts. Context menu:
* Download original;
* Re-map and reprocess (opens `/imports/:id` in reprocess mode, which uses `POST reprocess`);
* Delete (confirm dialog that explains that transactions also present in other imports are kept).

9.6 **Port the legacy v7 import window** into the preview (§7.10):
* the raw lines panel with header-row picking (click a line; *File has no header row* + skip rows; *Automatic*; struck-through skipped lines);
* footer-drop and date-order controls;
* the click-a-column-name role menu with auto hints, and *Use detected meanings*;
* first 10 + last 5 rows with a ⋯ gap row;
* the required-fields footer with the detected encoding, delimiter, header, rows and skipped count.

Popovers stay inside the viewport and above the dialog.

9.7 **Fast path and source labels:**
* the "Import known layouts without review" preference (default on) → `autoCommit=true`;
* toast with *Review* and *Undo*;
* sidebar source labels from `profileLabel`;
* sidebar hidden by default.

**Tests:**
* component tests with MSW for each preview subcomponent;
* E2E: import overlap A, then B → the toast counts match the oracle; delete A → table count matches; re-upload A → duplicate toast; Windows-1250 fixture shows correct Polish letters; debit/credit fixture with the mapping change commits correct signs.
* E2E for v7 import settings:
  * header-less file with skip rows;
  * the padded-preamble fixture auto-detects its header;
  * the footer summary line is dropped and reported;
  * the month-first fixture;
  * the second upload of a known layout auto-commits, and *Undo* removes it.

**Acceptance criteria**
- [ ] All the above E2E cases green.
- [ ] Preview interactions feel instant: edit → re-render ≤ 300 ms locally.

**Gate (our Product Engineer):** import their own anonymised real fixtures through the UI.

---

### Phase 10 — Table view, groups and multi-selection · Size L

**Goal:** parity with the legacy table: grid, header, menus, chips, inspector, status bar, export and keyboard, all server-backed.

**Read first:** §7.6, §7.7, §7.11; `legacy/DESIGN.md` §9–§12.

**Tasks**

10.1 `useQueryContext(viewId)`: derives the `QueryContext` from `viewStore` (debounced search and conditions) and computes `ctxKey`.

10.2 `Grid`: §7.7 in full (windows, skeletons, measured row height, sticky header and gutter, colgroup widths × uiScale, resize, sort cycling, type badges, `.neg`, mono, empty state row).

10.3 `hl()` port in `table/highlight.tsx`: returns React nodes with `<mark>`. Unit tests use legacy-derived cases: Polish diacritics, `ł`, and skipping when normalisation changes the length.

10.4 `ColumnMenu`, `ColumnsMenu` (show/hide, the *Hide all / Show all* toggle, reset widths), condition help texts (port legacy `HELP`), and the distinct checklist (`POST /transactions/distinct`, search, All/None on the shown values).

10.5 `Toolbar` + `Chips` (§7.6), Clear all, and Export (`POST /transactions/export` → blob download with the server filename).

10.6 `Inspector`: fields, raw section, sources, category combobox (`PATCH`), note editor, Copy as JSON, prev/next. Toggle via row click; overlay below 1100 px (port).

10.7 `StatusBar`: shown/total, accent when filtered, visible column count, period label, Σ / avg / min / max. The totals picker appears only with 2+ visible number columns; with one, the totals carry a tooltip naming it. The "N duplicates skipped" link opens the Duplicates dialog (10.13).

10.8 Keyboard: selection movement that fetches windows as needed and keeps the row in view; the Esc chain; double-click copy.

10.9 **View switching:** `/t/all` vs `/t/:accountId` each have their own `ViewState` (legacy per-view state). The Account column is visible by default only in `all`.

10.10 **Groups backend:**
* groups CRUD + stats endpoints;
* `POST /transactions/bulk` with `set.groupId` (writes annotations);
* the `group` column in exports and in `/me/export`.

10.11 **Multi-selection + quick bar** (§7.6, §7.7):
* drag, Shift and ⌘ gestures with auto-scroll;
* `selectionStore`;
* `POST /transactions/ids` for ranges;
* `QuickBar`: count, Σ, group dots, + new group then assign, Ungroup, close; toasts shift up while it is visible.

10.12 **Groups UI:**
* `GroupsMenu`;
* `GroupForm`;
* Inspector group pills;
* the Group column, hidden until a group exists;
* renaming a group renames the active filters.

10.13 **Duplicates (§4.4, §7.14):**
* `GET /duplicates`, `PUT` and `DELETE /duplicates/{accountId}/{fingerprint}`;
* `DuplicatesDialog`, with progress while the job runs;
* the status-bar link.

**Tests:**
* unit tests for the stores and chip derivation;
* component tests for ColumnMenu and Grid with MSW;
* **E2E (port legacy §26):** search count, accent-insensitive search (`platnosc` finds `PŁATNOŚĆ`), sort order, each filter operator family, value checklist exclusion, hide/resize persistence across reload, inspector copy, export content (download and parse in the test), keyboard navigation across a window boundary.
* **Groups and selection E2E:**
  * drag-select 5 rows → the quick bar Σ equals their sum;
  * Shift-select across an unloaded window → the count and Σ are right;
  * assign a new group → its chip filter shows exactly those rows;
  * rename the group → the active filter follows;
  * delete the group → labels are gone, transactions intact;
  * a label survives delete + re-import of its file.
* **Duplicates E2E** (duplicate-versions fixture):
  * the status bar shows the skipped count, and the link opens the dialog;
  * *Use this version* changes the visible category;
  * *Keep both* adds the rows, and *Merge again* removes them;
  * the filter and paging work.

10.14 Performance: with the 100k-row seed user, scroll top → bottom → middle. Record frame timings with a Playwright trace in `docs/perf.md`.

**Acceptance criteria**
- [ ] E2E green.
- [ ] PARITY rows for table features marked `ported`.
- [ ] Perf numbers recorded and within §7.12.

**Gate (our Product Engineer):** a hands-on session comparing the legacy and new tables on the same demo data.

---

### Phase 11 — Insights: API, pane, modal · Size L

**Goal:** the six pane charts with period picker, modal, table twins and range settings.

**Read first:** §4.6 (table), §6.2 (Insights), §7.8; `legacy/DESIGN.md` §13–§15, §19; the legacy `CHARTS.sum|cat|flow|pace|payee|heat` implementations.

**Tasks**

11.1 **Backend** `internal/insights`: `Summary`, `Categories`, `Cashflow`, `Pace`, `Payees`, `Daily`, using the shared CTE builder (Phase 7, task 7.4). Pure calculators (e.g. pace cumulative arrays, `days` clipping, prevRange delta) are unit-tested with small hand-built datasets. SQL aggregation is integration-tested.

11.2 OpenAPI + handlers for `POST /insights/*`.

11.3 **Frontend** `charts/lib` ports + unit tests (`barPath` geometry, `arc` sweep flags, `spark` scaling, `cpt` formatting, `roundNice`).

11.4 Chart components, ported from the legacy drawing code and keeping the logical sizes:
* `SummaryChart`;
* `CategoriesChart` (donut + bars, single-category ring);
* `CashflowChart` (diverging bars, period highlighting, `flowMonths` capped by width/5);
* `PaceChart` (crosshair overlay);
* `PayeesChart`;
* `DailyHeatmap` (quintiles, Mon-first, fit logic, most recent weeks when too wide).

11.5 `InsightsPane`:
* resizable height (`--viz-h`, 170 px … 75 vh, double-click reset, persisted);
* collapse; Charts menu (persisted `cards`; reorder by drag or ↑/↓, persisted `cardOrder`; the drag payload is `text/plain`, so the file-drop overlay ignores it);
* horizontal scroll-snap cards;
* `PeriodPicker`: options from `dataRange` (months and years), presets, prev/next stepping, custom from/to.

11.6 `ChartModal`: expand from any card; chart/table switch; `cfgControls` (`flowMonths`, `paceCmp`, `payeeTop`, `payeeScope`, `heatRange`), persisted in `uiStore.cfg` and applied to the small card too; ←/→ navigation over the visible cards; Esc; backdrop click; ResizeObserver re-render.

11.7 Chart actions → table (§7.8) via a `useChartActions(viewId)` hook. Toggling semantics match the legacy `toggleFilter`.

**Tests:**
* backend unit + integration tests;
* frontend component tests per chart with fixed data (assert the number of bars, labels, legend values, and the table-twin content);
* E2E: click a category → filter chip appears and the table count matches; click a month → the period changes; modal table view shows the same numbers as the API response.

**Acceptance criteria**
- [ ] All six charts render correctly in both themes at UI scale 90 %, 100 % and 130 % (screenshots in the PR).
- [ ] PARITY rows marked.

**Gate (our Product Engineer):** a visual comparison against the legacy pane on the same demo data.

---

### Phase 12 — Overview: API and UI · Size L

**Goal:** the month-centric finance overview with KPIs, the twelve cards (incl. Investment, Yield on cost, FI/RE), budgets, opening balance and investment data.

**Read first:** §4.6 (Overview), §7.9; `legacy/DESIGN.md` §16; legacy `ovCtx`, `renderOverview`, `findRecurring`, `CHARTS.o_*`, the highlight rules.

**Tasks**

12.1 **Backend** `insights/overview.go`:
* assemble the `Overview` (§4.6) from a handful of SQL aggregates (per month totals, per month×category, income by source for M, daily totals for the weekday window, balance series) plus Go calculators;
* `findRecurring` port, verified against the oracle output on demo data;
* highlight rules, one function per rule with its own unit tests at and around every threshold;
* `roundNice` budgets; projected spend; `partial`/`prevToDate` logic.

12.2 Budgets: `GET /budgets`, `PUT /budgets/{categoryId}` (null resets to default).

12.3 Opening balance editing via `PATCH /accounts/{id}`.

12.4 **Frontend** `OverviewPage`:
* header (month selector, filters note with Clear, partial flag);
* `Kpis` with sparklines and deltas;
* the cards: Highlights, Cash flow, Budgets (inline editable), Recurring, Money flow (Sankey port), Largest expenses, Savings rate, Weekday, Balance (opening input only in single-account views);
* the 12-column grid with container queries and explicit card body heights;
* expand any card except Highlights into `ChartModal` with a table twin and the `ovMonths` cfg.

12.5 Actions:
* a Sankey category → set the filter and switch to table mode;
* largest expense → locate the row in the table;
* cash-flow or savings bar → select the month.

12.6 **Investments (§4.9, §4.6):**
* `GET`/`PUT /investments` (versioned document);
* calculators in `internal/investments`, with unit tests per formula (incl. the dividend/interest fallback, short histories and an unreachable FI number);
* the three cards and the large-view editors.

Oracle comparison on the Phase 1 portfolios.

**Tests:**
* backend: overview for the demo data compared with oracle-derived expectations where available (recurring, period anchors). Hand-computed expectations for KPIs on a tiny dataset. Tests per highlight rule.
* frontend component tests;
* E2E: change a budget → status updates and persists across reload; partial month shows "Projected spend".

**Acceptance criteria**
- [ ] Screenshots of the overview in both themes at 3 container widths (≥ 1100, 1100–640, < 640).
- [ ] PARITY rows marked.

**Gate (our Product Engineer):** a hands-on overview review with their own fixtures.

---

### Phase 13 — AI assistant (server-side tool calling) · Size L

**Goal:** port the legacy local-LLM assistant (legacy §29) to the server architecture:
* the same retrieval-by-tools design and the same UX;
* stricter security (browser CSP untouched, RLS-scoped tools, egress allowlist);
* **measurable quality** (eval suite).

**Read first:**
* §4.8, §6.2 (Assistant), §6.3 (`ToolFilter`, `AssistantEvent`), §7.13, §8.1 (assistant rows);
* `legacy/DESIGN.md` §29;
* legacy `sysPrompt`, `AI_TOOLS`, `FILTERS`, `aiFilter`, `execTool`, `complete`, `sse`, `runTools`, `runPlan`, `trimHistory`, `md`, `renderChat`;
* Our Product Engineer's answer to §2.1 Q6.

**Tasks**

13.1 **Config and status:**
* `FINUANCE_AI_*` (§3.5), with the private-address check at startup;
* `GET /assistant/status`: upstream `GET /models`, cached for 60 s; skip embedding models; default-model choice as in the legacy `aiTest`.

13.2 **`internal/llm`:** a streaming Chat Completions client.
* SSE parser;
* `content`/`reasoning_content`/`reasoning` deltas;
* streamed `tool_calls` assembled by index;
* `<think>` splitting;
* Qwen `<tool_call>{…}</tool_call>` text fallback;
* non-streaming JSON fallback;
* context cancellation and timeouts;
* error classification: unreachable, auth, tools rejected, context too long, bad JSON.

Table tests from recorded fragment sequences.

13.3 **Tools** (`internal/assistant/tools.go`): `search_transactions`, `aggregate_transactions`, `find_values`.
* `ToolFilter` → SQL via `internal/query`, executed in `WithUserReadOnly`.
* **Differential tests against the Phase 1 oracle** (`execTool` on demo data, groups included).
* New: `group_by: account`, `find_values field: group`.

13.4 **Prompt builder:** port `sysPrompt` (facts computed by SQL, masked examples, vocabularies), `PLAN()` and the RULES text.
* Golden test against the oracle prompt with a frozen clock.
* Allowed differences: canonical column names, scope wording.

13.5 **Run loop:**
* tools mode, plan mode, auto fallback, max 6 rounds;
* history trimming, caps, `/no_think`;
* steps and meta.

Persistence:
* each turn (user message, steps, reasoning, answer, meta) goes into `assistant_messages`;
* chat title = the first question;
* keep the last `FINUANCE_AI_MAX_CHATS` per user, or none when keep-chats is off.

Migration `000NN_assistant.sql` + RLS.

13.6 **Streaming endpoint** `POST /assistant/chats/{id}/messages` → SSE events (§6.3):
* `Content-Type: text/event-stream`, `X-Accel-Buffering: no`;
* a heartbeat comment every 15 s;
* cancellation on client disconnect, plus `POST …/stop`;
* one in-flight run per user (`409` otherwise);
* a per-replica semaphore; the rate limit.

Make sure nginx (Compose) doesn't buffer `/api/v1/assistant/` (`proxy_buffering off`, read timeout > `FINUANCE_AI_TIMEOUT`).

13.7 **Frontend** (§7.13):
* `ChatPanel`, `ChatMenu`, the settings popover, suggestions;
* the streaming reader;
* the `md()` port to React nodes, with tests for escaping, lists, tables and code;
* the step view with **Show in table** (applies `toolFilter` as a "✦" chip in the right view);
* ⌘/Ctrl J, resize/maximise, stop;
* error states with concrete fixes, e.g. "Start the Ollama/LM Studio server", "This model doesn't support tools → Plan mode", "Context too long → fewer rows per lookup".

13.8 **Mock server and tests.** A Go `httptest` OpenAI-compatible mock (`backend/testutil/llmmock`) provides:
* `/v1/models`;
* scripted streaming tool calls split mid-JSON;
* a model that rejects `tools`;
* `<think>` blocks and Qwen text tool calls;
* slow streams for stop tests.

A thin `backend/cmd/llmmock` binary wraps it for the Compose CI profile `ai-mock` and for local UI work. It is never part of the production image. Playwright E2E ports legacy §29.6:
* disabled without config;
* a tool call streamed in fragments is assembled and executed;
* its result is sent back to the model;
* a markdown table renders;
* Show in table filters correctly;
* the auto plan-mode fallback;
* stop;
* no page errors;
* the browser makes no request to any origin but its own (Playwright request log).

13.9 **Evals** (`evals/assistant/`): ≥ 40 golden questions on the demo data with a frozen "today". Cover:
* relative dates ("last week", "this month");
* merchant spelling via `find_values`;
* groups;
* rankings and trends;
* comparisons;
* out-of-range periods;
* items vs transactions;
* Polish and English phrasing.

Each question has expected tool calls, argument constraints and expected numbers. `make eval-assistant MODEL=…` runs them against `FINUANCE_AI_BASE_URL` and writes `docs/evals.md`: accuracy per category, p50/p95 latency, approximate tokens. Run at least two local models (e.g. a ~8B and a ~14B+ Qwen) and record the comparison and the chosen default.

13.10 **Observability:**
* metrics (§8.2);
* trace spans per run and per model request (no prompt text in spans or logs);
* one structured log line per run: user hash, rounds, tools, duration, result.

**Acceptance criteria**
- [ ] Tool outputs match the oracle on demo data (allowed differences listed).
- [ ] Mock-server integration and E2E tests green in CI. No real model is needed in CI.
- [ ] `docs/evals.md` with results from a real local model: tool-choice and numeric correctness ≥ 90 % with the chosen default model. Adjust the targets with our Product Engineer if the hardware forces a small model.
- [ ] Browser CSP unchanged (`connect-src 'self'`). The api reaches nothing but the configured LLM host (documented here; enforced in Phase 16).

**Gate (our Product Engineer):**
* ask their own questions about their anonymised real data via the homelab model;
* review the eval report and three step transcripts.

---

### Phase 14 — Legacy migration and quality gate: E2E, parity harness, accessibility, v0.1.0 · Size L

**Goal:** move real data over from the legacy app, prove parity and quality, then cut the first release.

**Tasks**

14.1 **Legacy migration (§4.10):**
* backend: the `legacy_imports` migration + RLS; `POST /legacy-imports` and `GET /legacy-imports/{id}`; job `LegacyImport`, idempotent, with progress per file;
* browser: Settings → Data → *Import from the single-file app*. It covers backup parsing (`deser` port), the passphrase check and WebCrypto decryption, the account wizard, upload with progress, and the report view.

Tests:
* Go unit tests for key translation (`txKeys` → fingerprint + occurrence) and for cfg → mapping translation;
* integration tests on the oracle-generated plain and encrypted backups (Phase 1);
* a wrong passphrase is rejected in the browser;
* re-running a migration creates nothing new.

14.2 **E2E completion** against `compose.ci.yaml`:
* auth;
* imports (every fixture family);
* table (incl. groups and multi-selection), insights, overview, settings;
* assistant against the mock server;
* legacy migration (plain and encrypted backups);
* keyboard map (incl. ⌘J);
* responsive 720 px (sidebar auto-collapses; popovers stay inside the viewport);
* UI scale (deviceScaleFactor 2 vs 2.5 → check the computed `html` font size in Auto mode);
* dark/light screenshot set archived as a CI artifact.

14.3 **Parity harness** `web/e2e/parity.spec.ts`:
* Load the **oracle-patched legacy file** in Chromium, with the legacy demo CSV (`testdata/fixtures/demo/demo-legacy.csv`) added through `addFiles`. Compute values via `window.__legacy` internals.
* Separately, import the **same CSV** into a fresh user in the new app and query the API.
* Compare:
  * all-time Σ;
  * summary for `all`, `l3m` and each of the last 3 months;
  * category totals;
  * cash flow per month;
  * the recurring list;
  * overview KPIs for the latest 3 months;
  * the final balance with opening 0.
* Run the same comparison **through the migration path**: migrate the oracle's demo backup (with groups, budgets, overrides and investment data) into another fresh user, then compare again. This time include group totals, budgets, the duplicates count and the investment cards.
* Write a Markdown report to `docs/parity-report.md`. The test fails on any difference not listed in `ALLOWED_DIFFS.md`.

14.4 **Accessibility:** run `@axe-core/playwright` (dev dependency; ask first) on login, table, preview, overview and settings. Fix serious and critical issues. Add a keyboard-only checklist in `docs/a11y.md`.

14.5 **Performance:** check the bundle against §7.12 and API p95s with the 100k seed. Update `docs/perf.md`.

14.6 `docs/PARITY.md`: every row `ported` or `retired: reason`.

14.7 **Release v0.1.0:**
* implement `release.yml` (§8.5) except the GitOps bump, which comes in Phase 16;
* create `deploy/compose/compose.prod.yaml`: released GHCR images (no build), `.env` for secrets, restart policies, named volumes, the web service as the only exposed port;
* `CHANGELOG.md`;
* README v0.1 with screenshots, a feature list, a quick start (`make up`), and an architecture summary linking the docs;
* tag `v0.1.0`.

**Acceptance criteria**
- [ ] CI green on `main`, including E2E and the parity harness.
- [ ] Signed images for `v0.1.0` in GHCR, with an SBOM attached.

**Gate (our Product Engineer):** migrate their real legacy backup and check the report; read the parity report; try `docker compose -f deploy/compose/compose.prod.yaml up` using the released images on a clean machine or VM.

---

### Phase 15 — Product depth: categories, rules, transfers, assistant actions, group reports (+ FX stretch) · Size L

**Goal:** features the legacy app couldn't have, built on the existing query compiler.

**Read first:** `legacy/DESIGN.md` §27 (limitations and ideas); §4.5.

**Tasks**

15.1 **Category management:**
* Settings → Categories: list with counts, rename, **merge** (reassign transactions then delete), delete (transactions → uncategorised).
* API endpoints + tests.
* Colour slots keep following all-time rank.

15.2 **Rules engine** (`internal/rules`, migration `rules`). A rule is `{name, enabled, position, match: {search?, filters: [ColumnFilter]}, actions: {setCategoryId?, markTransfer?}}`. The `match` uses **the same filter language and compiler as the table**.
* Application: `UPDATE transactions SET category_id=…, category_source='rule' WHERE <compiled match> AND category_source <> 'manual'`. Rules run in `position` order, and the last matching rule wins for category. Test and document the precedence: `manual > rule > bank`.
* Job `ApplyRules{UserID, Scope: import:<id> | all | uncategorised}`, enqueued after every import commit and by `POST /rules/apply`.
* `POST /rules/{id}/preview` → match count + 10 sample rows.
* **UX:** a toolbar button "Save filters as rule…" turns the current view's search and filters into a rule with a "set category" action. A Settings → Rules page offers reorder (drag), enable/disable, edit, delete and apply.

15.3 **Transfer detection** (job `DetectTransfers{UserID}`, after commit):
* Pairs between **different accounts of the same user** with `a.amount = −b.amount`, `|Δbooked_on| ≤ 3 days`, both unpaired. A greedy match by smallest date gap, deterministic tie-break by `seq`.
* Mark `is_transfer` + `transfer_peer_id` on both. Rules can also mark transfers.
* Defaults (ADR): insights and overview **exclude** transfers (`includeTransfers=false`). The table shows them with a "Transfer" badge and a chip to hide them. A manual unpair action is available in the inspector.

15.4 **Recurring cadence:** extend detection with weekly and yearly cadences (the legacy limitation). Keep the monthly algorithm byte-identical for parity and add a `cadence` field.

15.5 **Bulk edit:** extend the quick bar (Phase 10) with *Set category* and *Mark as transfer* via `POST /transactions/bulk`. Add Shift+↑/↓ keyboard range selection.

15.6 **FX (stretch, ask first):**
* periodic job fetching NBP table A mid rates into `fx_rates` (verify the current API format at the official NBP API docs before implementing);
* `prefs.baseCurrency`;
* insights convert non-base amounts with the rate of `booked_on`, falling back to the last available rate.

15.7 **Assistant action proposals (stretch, human-in-the-loop):**
* A fourth tool, `propose_action`, covers e.g. "label these as Italy trip", "create a rule ŻABKA → Groceries", "set category for these 12 rows".
* The server **never executes it**. The UI renders a confirmation card with the affected rows (as a QueryContext + count + sample) and the change.
* *Apply* calls the normal endpoints with the user's session. The action is audit-logged.
* Extend the evals with proposal cases.

15.8 **Assistant insight tools:** `get_month_overview(month)`, `list_recurring()`, `get_budget_status(month)`, `get_group_summary(group)`, reusing `internal/insights`. They answer questions like "am I over budget this month?" without row lookups. Extend the evals.

15.9 **Group reports:** a group detail view (modal from the Groups menu) with:
* total spent/income and the date span;
* spend per day, a categories donut, top payees;
* CSV export of the group;
* an optional **group budget** (e.g. a trip budget) with a progress bar on the Groups menu entry.

15.10 **Rule suggestions:** when the same `payee_group` has been categorised manually ≥ 3 times, suggest a rule as a dismissible card in the toolbar. Optionally, let the assistant name the category (stretch).

**Tests:**
* rule compile/apply integration (precedence, manual protection, position order);
* transfer pairing edge cases (equal amounts on the same day across 3 accounts, near-miss dates);
* the insights exclusion default;
* E2E: create a rule from filters → applied → categories updated in the table and overview.

**Acceptance criteria**
- [ ] All the above green.
- [ ] ADRs for rule precedence and the transfer defaults.

**Gate (our Product Engineer):** create rules for their real categories and check the transfer pairs between their accounts.

---

### Phase 16 — Production deployment: Helm, CloudNativePG, Argo CD, LLM endpoint · Size L

> **Phase regrouping (2026-10-24, our Product Engineer, option B).** The homelab is out of reach for
> now, so the work that needs it moves to one rollout phase. **Phase 16** builds and rehearses the
> deployment without the homelab: 16.1–16.4, 16.7, the 16.8 ADR and values, and 16.6 as a rehearsal
> on a throwaway kind cluster in CI (the cloud session can't pull ghcr.io or quay.io images).
> **Phase 17** does the parts of 17 that need no cluster (17.2, 17.3 and 17.4 as code, 17.5 against
> the Compose stack, 17.6 audit log, Activity page, chart scan, ZAP baseline, THREAT-MODEL v2) plus
> the demo-mode code of 18.1. The new **Phase 18 — Homelab rollout** deploys everything on the real
> cluster. The old Phase 18 (public demo, documentation, v1.0.0) becomes **Phase 19**, and its gate
> carries the full security scan with nothing ignored. (Regrouped again on 2026-11-03: that phase is
> now **Phase 21**; see the note at Phase 19.)


**Goal:** a GitOps deployment on our Product Engineer's k3s homelab with HA-ish API, managed Postgres, backups and a tested restore.

**Read first:** §2 (deploy rows), §3.1, §8.1, §8.5; our Product Engineer's answers to §2.1 Q5. **Before writing manifests, check the current docs** of CloudNativePG (backup method), Argo CD (Helm hook handling) and Sealed Secrets, because these projects evolve.

**Tasks**

16.1 **Helm chart** `deploy/helm/finuance`. `values.yaml` has `values.schema.json`. Templates:
* `api` Deployment:
  * 2 replicas; rolling update `maxUnavailable: 0`;
  * readiness `/readyz`, liveness `/healthz`, startup probe;
  * resources;
  * `securityContext`: non-root, read-only root filesystem, drop ALL capabilities, seccomp `RuntimeDefault`;
  * `automountServiceAccountToken: false`.
* `worker` Deployment (1 replica; River allows more), `web` Deployment.
* Services; Ingress (`className` configurable; `/api` → api, `/` → web; TLS via a cert-manager annotation; HSTS).
* **Migration Job** as a pre-install/pre-upgrade hook (confirm how Argo CD maps Helm hooks; use Argo CD `PreSync` annotations if needed). It uses `FINUANCE_MIGRATE_DATABASE_URL`.
* HPA for api (CPU 70 %); PDB (`minAvailable: 1`).
* **NetworkPolicies**: default deny; allow ingress controller → web/api; api/worker → Postgres 5432; DNS egress; worker egress to NBP only if FX is enabled; **api egress to the LLM host only when the assistant is enabled**.
* Optional `ServiceMonitor` (when the CRD exists).
* Env from ConfigMap and `existingSecret`.

CI `helm` job: `helm lint`, `helm template | kubeconform -strict` (with CRD schemas), and a Trivy config scan of the rendered manifests.

16.2 **Postgres via CloudNativePG** (`deploy/k8s/db/`):
* the operator via an Argo CD Helm app;
* a `Cluster` CR: instances per storage capacity (2 preferred); `initdb` with database `finuance` and owner `finuance_owner`; **managed role** `finuance_app`; `postInitApplicationSQL` replicating `bootstrap.sql` (extensions, grants, default privileges); the storage class; resources; PodMonitor enabled.
* **Backups** with the currently recommended CNPG method to an S3-compatible bucket, or volume snapshots if no bucket exists:
  * WAL archiving;
  * a `ScheduledBackup` daily;
  * retention 14 days.

16.3 **Secrets:** the Sealed Secrets controller (Argo CD app). Sealed secrets for the DB app/owner connection strings. `docs/RUNBOOK.md` documents how to re-seal and rotate.

16.4 **GitOps** (`deploy/k8s/argocd/`):
* app-of-apps `finuance-root` → `cnpg-operator`, `sealed-secrets`, `finuance-db`, `finuance` (chart + `deploy/environments/homelab/values.yaml`), and later `observability`;
* automated sync with prune + selfHeal for the app; manual sync for the DB cluster.
* `release.yml` now opens a PR bumping the image tags in the homelab values (`yq`). Merging the PR deploys. Rollback = revert the PR.

16.5 **Access:** expose via the homelab ingress on the tailnet with cert-manager TLS (issuer from §2.1 Q5).

16.6 **Restore drill** (mandatory, documented in RUNBOOK with measured times):
1. Take an on-demand backup.
2. Restore into a new Cluster in a scratch namespace (recovery bootstrap).
3. Point a temporary `finuance` deployment at it.
4. Run the smoke scripts.
5. Record RPO/RTO.

16.7 `docs/RUNBOOK.md`: deploy, rollback, failed migration recovery, rotating DB credentials, scaling, restore, "Postgres down" and "disk full" playbooks, upgrade procedure for the Postgres major version.

16.8 **LLM endpoint** (§2.1 Q6), recorded in an ADR. Either:
* deploy Ollama into the cluster: an Argo CD app with a PVC for models, resource limits, and GPU passthrough only if available; or
* point at the existing homelab Ollama, or LM Studio on the Mac, over the LAN/tailnet.

Then:
* set `FINUANCE_AI_*` in the homelab values, with a sealed secret for the API key if there is one;
* verify that SSE streams through the ingress without buffering, with an idle timeout > `FINUANCE_AI_TIMEOUT`;
* run the eval suite against the deployed instance once.

**Acceptance criteria**
- [ ] Argo CD shows all apps Synced/Healthy; the app is reachable over HTTPS on the tailnet.
- [ ] Deleting an api pod during a running k6 smoke test causes zero failed requests (2 replicas + PDB + readiness).
- [ ] The restore drill was executed and its timings recorded.
- [ ] The CI helm job is green.

**Gate (our Product Engineer):** watch a deploy via PR merge, perform a rollback via revert, read the restore drill notes.

---

### Phase 17 — Observability, SLOs, load testing, security hardening · Size M

**Goal:** operate it like a production service and have numbers to talk about.

**Tasks**

17.1 **Observability stack** via Argo CD apps, with resource-light homelab values (ask our Product Engineer which pieces they already run and reuse those):
* kube-prometheus-stack (Prometheus, Alertmanager, Grafana);
* Loki + Grafana Alloy for logs (Promtail is deprecated);
* Tempo for traces;
* an OpenTelemetry Collector (OTLP in → Tempo).

  Set `FINUANCE_OTEL_EXPORTER_OTLP_ENDPOINT` in the homelab values.

17.2 **Trace propagation into jobs:** store `traceparent` in the River job metadata at enqueue. The worker starts the job span linked to the request span. Verify in Tempo that an import commit trace shows API → job → SQL.

17.3 **Dashboards as code** (`deploy/k8s/observability/dashboards/*.json`, provisioned through the Grafana sidecar label):
* API RED by route;
* imports and jobs;
* assistant: runs, plan-mode fallbacks, tool calls, rounds, latency, tokens, LLM errors;
* Postgres (CNPG metrics: connections, TPS, WAL archiving, last backup age);
* Go runtime;
* SLO burn.

17.4 **SLOs and alerts** (`docs/SLO.md`):
* availability: 99.5 % non-5xx over 30 days;
* latency: p95 < 300 ms for `/transactions/query` and `/insights/*`;
* `PrometheusRule` multi-window burn-rate alerts;
* other alerts: import job failure rate, last successful backup > 26 h, CrashLoopBackOff, Postgres not ready.
* Alertmanager receiver per our Product Engineer's preference (e.g. ntfy, email, Discord).

17.5 **Load testing** (`loadtest/`, k6):
* seeding: `finuance seed-demo` for 50 users × 20k rows;
* scenarios: browse (query with random filters, sorts and windows), insights (all charts), overview, import (5k-row file upload + commit);
* ramp to the breaking point on the homelab hardware;
* thresholds aligned with the SLOs.

Record the results with Grafana screenshots in `docs/perf.md`. **Find, fix and document at least one real bottleneck** (before/after numbers).

17.6 **Security hardening:**
* migration `audit_log` + events: login success/failure, logout, password change, session revoke, import commit/delete, account delete, export, rule apply, legacy import, duplicate override changes;
* Settings → Activity page (own events only);
* kubescape or Trivy k8s scan of the chart (fix HIGH findings);
* an **OpenVAS / Nessus Essentials** scan (both already run in the homelab) of the deployed instance, with findings triaged in `docs/security-scan.md`;
* optional OWASP ZAP baseline scan in nightly CI against the compose stack;
* `THREAT-MODEL.md` v2.

**Acceptance criteria**
- [ ] Dashboards live; a test alert fires and is received.
- [ ] Load-test report with SLO compliance and the bottleneck write-up.
- [ ] Audit events visible.

**Gate (our Product Engineer):** a Grafana walkthrough; read the perf write-up.

---

### Phase 18 — Homelab rollout · Size M

**Goal:** the Phase 16 and 17 artifacts running on our Product Engineer's k3s homelab (added by the
2026-10-24 regrouping; needs physical or remote access to the homelab).

> **Reference environment (2026-10-27, our Product Engineer, ADR 0022).** The homelab is still out of reach,
> so Phase 18 runs on single-node k3s on a personal machine with a small NVIDIA GPU: Tailscale
> `*.ts.net` TLS, RustFS in the cluster for backups, Ollama on the host. The hands-on work runs in a
> Claude Code session on that machine; its details are in the private environment repository. The task list is in `docs/PROGRESS.md`.

**Read first:** Phase 16 and 17 above, `docs/RUNBOOK.md`, the ADRs written in Phases 16–17, ADR 0022.

**Tasks**
* Homelab answers (§2.1 Q5): ingress (Traefik, confirmed), certificate issuer (Let's Encrypt with a
  domain, or Tailscale `*.ts.net` certificates without one), storage class, backup target (S3-compatible
  bucket or volume snapshots), hostname, LLM address. Fill `deploy/environments/homelab/values.yaml`.
* 16.4 on the real cluster: bootstrap Argo CD, the app-of-apps, Sealed Secrets keys (real sealed
  secrets), CNPG cluster with backups; 16.5 access over the tailnet with TLS.
* 16.6 restore drill on the homelab with measured RPO/RTO; the pod-kill k6 test on real hardware.
* 16.8 the LLM endpoint: values, SSE through Traefik, one eval run against the deployed instance.
* 17.1 observability stack (reuse what the homelab already runs), 17.3 dashboards provisioned,
  17.4 alert receiver and a test alert, 17.5 load test on the homelab hardware.
* 17.6 OpenVAS / Nessus scans of the deployed instance, triaged in `docs/security-scan.md`.
* 18.2 (now 21.8) the demo environment can be prepared here or in Phase 21.
* Every deferred gate check of Phases 13–17 that needs the homelab or the real data.

**Gate (our Product Engineer):** watch a deploy via PR merge, roll back via revert, read the restore
drill notes, a Grafana walkthrough.

---

### Phase 19 — Admin, themes, friends and shared groups · Size L–XL (new, 2026-11-03)

> **Phase regrouping (2026-11-03, our Product Engineer).** Phase 18 is done. Before the public
> release, two product phases are added: **Phase 19** (this one) and **Phase 20** (agentic assistant,
> advice library, CLI). The public release (demo, documentation, v1.0.0), until now Phase 19, becomes
> **Phase 21**. The task list with files, tests and checks is in `docs/PROGRESS.md`.

**Goal:** several people on one instance can share expenses with each other, and an administrator can
run the instance (users, limits, defaults, themes) without ever seeing anyone's financial data.

**Read first:** §4.7 (groups), §5.3 (RLS), ADR 0007 (multi-tenancy), ADR 0017 (rules and category
identity), ADR 0021 (audit log), §7.5 (tokens and scaling), §7.8 (charts), `docs/a11y.md`.

**Tasks** (three pull requests into `main`, each with full CI and our Product Engineer's OK)

*PR 1 — admin and themes*

19.1 **Admin role without financial data.** `users.is_admin`, disabled users, a one-time password that
must be changed; `instance_settings` and `user_limits`; `store.WithAdmin` reaches only the admin tables
(users, limits, settings, themes, sessions), never a user-owned financial table; usage as counts from
one `SECURITY DEFINER` function; first admin through `finuance admin grant-admin`; the last admin
can't be removed; every admin action is an audit event for the admin and for the affected user.

19.2 **Instance defaults, feature toggles and limits.** Defaults under each user's own preferences
(theme, size, charts and order, insights pane, sidebar, Overview); features users can't override
(Overview, Shared groups, Friends, Assistant); limit defaults (upload size, imports per day, accounts,
groups). Environment variables stay the outer bound. Enforced on the server (`404` for a feature
that is off, `422 limit_reached`).

19.3 **Admin panel** `/admin/:section`: users, defaults (with a preview), features, themes, usage.

19.4 **Colour themes.** Per-theme token sets; `--on-accent` and the logo follow the theme; the
categorical chart palette stays shared. Four built-in themes with dark and light variants:
**Indigo** (today's look), **Fjord** (cool blue, after Nord), **Grove** (sage and aqua, after
Everforest), **Ember** (warm gold, after Gruvbox). Every pair passes WCAG AA (unit test + axe over
every theme and mode).

19.5 **Theme editor** (admins): colour tokens of both variants with a live preview and a contrast
report; approval needs AA everywhere (checked again on the server); only known keys and hex colours
(no CSS); approved themes are offered to users.

*PR 2 — manual expenses, friends, notifications*

19.6 **Manual cash expenses**: cash accounts with transactions entered by hand (`origin = 'manual'`),
editable and deletable, never touched by imports or re-materialisation. Manual entries in bank
accounts (with merging when the import arrives) come with receipts in Phase 20.

19.7 **Friends**: an 8-character friend code per user that changes every hour; entering a friend's
code makes you friends; rate-limited and audited; friends see a display name, never an e-mail.

19.8 **In-app notifications** (bell in the title bar), used by friends, sharing and Phase 20's digests.

*PR 3 — shared groups and the Shared tab*

19.9 **Shared groups.** A personal group can be shared with friends. Each shared transaction becomes a
**copy** in the group (date, amount, payee, category, note, who paid); members never see accounts, bank
descriptions or raw rows. The copy is updated in the same transaction as the group assignment and
survives delete + re-import (keyed like annotations, in a member-private link table). Expenses without
a transaction (cash), splits (equal by default; shares, exact amounts), balances, "who owes whom" with
a minimal-transfer simplification, settlements. RLS by membership through a `SECURITY DEFINER`
function; the group currency only (FX stays in the backlog). A deleted user's expenses stay as
"Former member", so the others' balances don't change.

19.10 **Shared tab** after Overview (⇄ icon): a timeline of trips and other groups (personal and
shared) with per-year totals, a comparison across trips and "next time" hints; a group page laid out
like insights + table, with balances and *Settle up*.

19.11 ADRs (admin, themes, friends, shared groups and membership RLS, manual transactions),
THREAT-MODEL (a boundary between users, admin abuse, friend-code guessing, CSS injection through
themes), `docs/admin.md`, `docs/sharing.md`, release `v0.3.0` deployed to the homelab.

**Acceptance criteria**
- [ ] An admin context reads no financial table (RLS test over every user-owned table).
- [ ] RLS suite for shared data: non-member, invited, active, left and former member; the private links stay hidden.
- [ ] Balance property test: the sum is zero, the simplification keeps every net balance, at most n−1 transfers.
- [ ] E2E with two users: friends by code → share a trip → both add expenses → balances → settle up; neither sees the other's accounts.
- [ ] axe: zero serious/critical violations on table, overview, settings and the Shared tab in every built-in theme and mode.

**Gate (our Product Engineer):** with a second account, share a trip and settle up; walk through the
admin panel; create, approve and pick a theme; watch the homelab deploy of `v0.3.0`.

---

### Phase 20 — Agentic assistant, advice library, CLI · Size L (new, 2026-11-03)

**Goal:** the assistant can do things for you (with your consent and an undo), and agents can use the
app through a CLI.

**Read first:** §4.8, ADR 0015, ADR 0019, `docs/assistant.md`, `docs/evals.md`, Phase 19's ADRs.

**Outline** (planned in detail when the phase starts; decided 2026-11-03: proposals plus opt-in
auto-apply in the panel, tools stay read-only, background digests only propose)

1. **Proposals v2 and auto-apply**: `add_expense`, `set_budget`, `create_goal`/`update_goal`,
   `add_to_shared_group`, `split_expense`; applied by the panel through the normal endpoints, on a
   click or automatically for the action types the user opts into. Every applied change is recorded
   (`ai_changes`, before and after) and can be **reverted** with one click; a `Source` column, an
   "AI assisted" chip and ✦ badges show what the assistant changed.
2. **Receipts by photo**: image attachments (re-encoded in the browser, which removes EXIF and the
   location), content parts in `llm.Message`, vision capability detection; a receipt matches an
   imported transaction or becomes a manual expense; manual ↔ import merge suggestions.
3. **Goals in plain language** → structured goals, a `goal_status` tool, an Overview card.
4. **Digests**: a daily or weekly server-side run with read-only tools; results as notifications with
   proposals waiting for *Apply*; within the user's token budget; off in the demo.
5. **Advice library**: admin-edited documents searched with PostgreSQL full-text search through a
   `search_advice` tool; an admin addition to the system prompt; a starter library with a
   "not financial advice" note.
6. **Admin → Assistant**: connections edited in the UI (in the database, API keys encrypted under an
   instance key; the connections file stays as the starting point), per-model tuning, token budgets,
   usage per user (never prompt text). No model training.
7. **Personal access tokens and the `fin` CLI** (generated client, JSON/CSV/table output); optionally an
   MCP server (`fin mcp`).
8. Evals for every new action, receipts, goals and digests; ADRs; THREAT-MODEL.

**Gate (our Product Engineer):** receipts, goals and a digest with the real model; revert an AI change;
the CLI against the homelab.

**Planned 2026-11-06** (the task list is in `docs/PROGRESS.md`): four pull requests (AI changes,
proposals v2, auto-apply and goals; receipts; digests, advice and Admin → Assistant; tokens and the
CLI). Goals are savings targets and reductions (category limits stay budgets); one multilingual
model for text and receipts chosen by measurement on the reference machine; `fin mcp` moved to
the Backlog.

---

### Phase 21 — Public release: privacy clean-up, security, demo, documentation, v1.0.0 · Size L (was Phase 19, before that Phase 18)

**Goal:** a public repository and something a recruiter can click on, plus documents that make the
engineering visible.

**Decided 2026-11-03** (the full plan and the private-data audit are in `docs/PROGRESS.md`): publish into
a **new repository** with one clean root commit (the current one stays private as the archive);
replace the anonymised real bank exports with **synthetic** look-alikes; process documents "honest and
tidy" (this plan and the full progress log go to the archive, a lean progress file and a roadmap stay,
a short note says how the project is built); the demo runs on a **small VPS with single-node k3s**
through the same charts and Argo CD. Demo mode itself was built in Phase 17.

**Tasks**

21.0 Re-run the private-data audit on the tree Phases 19–20 leave behind.

21.1 **Private-data guardrails**: the existing `tools/check-env-details.sh` (patterns kept outside the
repository, in the private environment repository) also covers personal terms and commit authors,
prints only file and line, and runs in CI with the patterns from a repository secret; `tools/check-private.sh` also catches e-mail addresses outside
reserved domains, `*.ts.net` names other than `example.ts.net`, card patterns and real postcodes in
`testdata/`; matching gitleaks rules.

21.2 **Synthetic bank look-alikes** replace `testdata/fixtures/real/` (each reproduces every quirk of
the bank's layout); oracle and golden files regenerated, the header-fix tests moved onto them.

21.3 **Legacy app**: the built-in `SAMPLE` becomes clearly synthetic (same layout).

21.4 **Tidy the repository**: neutral wording instead of role names; `docs/DOMAIN.md` from §4 as built;
this plan and the full progress log archived; a lean `docs/PROGRESS.md` and `docs/ROADMAP.md`; the
agent instruction files trimmed; no references to commits or pull requests of the old history.

21.5 **Security in CI**: actions pinned by commit SHA; CodeQL (Go, TypeScript, Actions); dependency
review with a license allowlist; zizmor and actionlint for the workflows; osv-scanner; trufflehog
over the history; Trivy results as SARIF in code scanning; OpenSSF Scorecard once public.

21.6 `SECURITY.md` for 1.x, THREAT-MODEL v3 (the internet-facing demo, DoS at the ingress, the public
repository's supply chain), `docs/security-scan.md`.

21.7 **Signed images only**: images deployed by digest (chart value, the bump pull request writes it);
Sigstore policy-controller verifies the release workflow's keyless signature on the homelab, the demo
and the kind rehearsal; the pull secret goes once the images are public.

21.8 **Demo environment** `deploy/environments/demo/` (own namespace and database, small resources, demo
mode, assistant off): its own Argo CD, Let's Encrypt, rate limits at the ingress (and optionally a CDN
proxy), SSH only over the tailnet, the Sealed Secrets keys copied off the machines, a daily smoke test
of the public URL.

21.9 **Publish**: `tools/publish-public.sh` creates the single root commit after every guard has
passed and refuses a remote that already has history; the RUNBOOK's move procedure (Argo CD switched
first, old repository renamed and kept private, rulesets, secret scanning with push protection,
private vulnerability reporting, packages made public).

21.10 **Documentation**:
* `README.md` final:
  * one-line pitch;
  * screenshot/GIF;
  * live demo link;
  * feature list;
  * architecture diagram;
  * stack table;
  * quick start;
  * an **"Engineering highlights"** section linking ADRs and write-ups: a privacy-preserving LLM assistant (tool calling under RLS, with an eval suite), labels that survive re-imports, duplicate review via re-materialisation with a property test, migration from the single-file app, idempotent de-dupe via a unique constraint, filter DSL → parameterised SQL, RLS multi-tenancy (and sharing across tenants), transactional job enqueue, differential testing against the legacy app with a headless browser, fuzzing, GitOps, SLOs and load testing, signed images.
* `docs/ARCHITECTURE.md` complete; ADR index; `CONTRIBUTING.md`; `SECURITY.md`.

21.11 **Write-ups** in `docs/writeups/`, 600–1200 words each, with diagrams and code excerpts:
1. "De-duplicating overlapping bank exports with one unique constraint";
2. "Porting a 2,600-line single-file app with a headless-browser oracle";
3. "From a filter mini-language to safe parameterised SQL";
4. "Running it like production on a homelab: GitOps, CNPG, SLOs";
5. "Letting a local LLM query your bank data without seeing it: tools, RLS and evals";
6. "Migrating users off a single-file app: client-side decryption and replay through the import pipeline";
7. (optional) "Sharing expenses across tenants under row-level security".

21.12 Screenshots in every theme; a GIF of the assistant with a real model (the demo's assistant is off).

21.13 Final PARITY and PROGRESS update. `CHANGELOG`. Tag **v1.0.0** in the public repository.

**Acceptance criteria**
- [ ] The public repository has one commit; the denylist, gitleaks, trufflehog and CodeQL find nothing.
- [ ] A full scan at every severity with nothing ignored: Trivy, govulncheck, `npm audit`, osv-scanner, CodeQL, ZAP and OpenVAS against the demo, an outside port scan (only 80/443 open).
- [ ] The public demo works from a phone and a laptop; the 24 h cleanup is verified.
- [ ] README reviewed by our Product Engineer; all write-ups present.
- [ ] `v1.0.0` released with signed images and deployed via GitOps by digest, with signatures enforced.

**Gate (our Product Engineer):** final review, then share the link.

---

## 10. Appendices

### Appendix A — `CLAUDE.md` (repo root) template

Keep it short. It loads into every session, so link to details instead of copying them.

````markdown
# Finuance — agent instructions

Finuance is a self-hosted personal-finance app for bank CSV exports: Go API + worker, PostgreSQL, React SPA.
It is a rebuild of the single-file legacy app in `legacy/`.

## Start of every session
1. Read `docs/PROGRESS.md` (current phase, open tasks, decisions) — imported below.
2. Read the current phase in `docs/PLAN.md` §9 and the sections it lists under "Read first".
3. Start in plan mode. Propose tasks and wait for approval before editing code.

@docs/PROGRESS.md

## Sources of truth
- Plan and specs: `docs/PLAN.md` (§4 domain rules, §5 schema, §6 API, §7 frontend).
- Behaviour not covered there: `legacy/DESIGN.md` and `legacy/finuance.html`.
- API contract: `api/openapi.yaml`. Decisions: `docs/adr/`. Legacy parity: `docs/PARITY.md`.

## Hard rules
- Only work on the current phase. Out-of-scope ideas go to the Backlog in PROGRESS.md.
- Every change ships with tests. Never skip, delete or weaken a failing test; ask instead.
- Run `make check` before every commit; `make test-integration` before closing a phase.
- Never edit generated code: `backend/internal/httpapi/gen/`, `backend/internal/store/db/`, `web/src/api/schema.d.ts`.
  Change `api/openapi.yaml` or `backend/internal/store/queries/*.sql`, then run `make gen`.
- No new dependencies without asking. No float64 for money. No `innerHTML`/`dangerouslySetInnerHTML`.
- Assistant tools are read-only and run under `store.WithUserReadOnly`. Never log prompts, model output or tool results.
- All user-data DB access goes through `store.WithUser`. Every query filters by `user_id`.
- Never open, print or commit anything under `testdata/private/`.
- Conventional Commits. Branch `phase/NN-name`. Update `docs/PROGRESS.md` after each task.
- New architectural decision → ADR in `docs/adr/` (template `0000-template.md`).

## Commands
- `make check` — lint, typecheck, unit tests, codegen drift, private-data check
- `make test-integration` — Go tests with Postgres (Docker required)
- `make gen` — sqlc + oapi-codegen + openapi-typescript
- `make up` / `make down` — full stack via Docker Compose (web on http://localhost:8000)
- `make dev-api`, `make dev-worker`, `make dev-web` — local dev processes
- `make oracle` — regenerate legacy expectations (Playwright)
- `make eval-assistant MODEL=…` — assistant eval suite against a real local model (not part of CI)
````

### Appendix B — directory `CLAUDE.md` templates

`backend/CLAUDE.md`:

````markdown
# Backend conventions (Go)

## Package rules
- `ingest`, `money`, `query/dsl` are pure: no DB, no HTTP, no time.Now (use platform.Clock).
- Domain packages (`ledger`, `query`, `insights`, `rules`, `auth`) return domain types, never sqlc rows.
- `httpapi` maps HTTP ⇄ domain only. No SQL strings outside `store` and `query/sql.go`.

## Recipes
- **New endpoint:** edit `api/openapi.yaml` → `make gen` → implement the strict handler method in
  `internal/httpapi/handlers_<area>.go` → map errors in `problems.go` → HTTP test with real DB + tenancy test.
- **New static query:** add to `internal/store/queries/<area>.sql` → `make gen` → wrap in a domain repo → integration test.
- **Dynamic query:** extend `query.ColumnRegistry` / the CTE builder; never concatenate user input; add a fuzz seed.
- **New migration:** `make migrate-new name=…` → SQL with `-- +goose Up/Down` → `make test-integration` (runs up/down/up).
  New user-owned table ⇒ `user_id` column + RLS policy + RLS test.
- **New job:** args struct + worker in `internal/jobs`, register in the registry, enqueue with `InsertTx` inside the
  same transaction as the state change, set `MaxAttempts`, add metrics + a test using River's test helpers.

## Style
- Errors: wrap with `%w`; sentinel errors per package; no panics in request paths.
- `context.Context` first parameter; respect cancellation in loops over rows.
- Table-driven tests; `go-cmp` for diffs; golden files via `-update`.
- Logging: `slog` with request-scoped logger from context; never log bodies, cookies or transaction fields.
````

`web/CLAUDE.md`:

````markdown
# Frontend conventions (React + TS)

## Rules
- Visual parity with `legacy/finuance.html`. Use tokens from `src/styles/tokens.css`; never hard-code colours.
- CSS sizes in rem (hairlines and SVG text in px). Charts draw in logical units via `<Svg w h>`.
- Server data via TanStack Query hooks in `src/api/hooks`; UI state in Zustand stores. No fetch calls in components.
- No `any`. No `dangerouslySetInnerHTML`. Format money/dates only via `src/lib/format.ts` and `src/lib/dates.ts`.
- Sentence-case copy; actions keep the same verb through the flow; errors explain how to fix.

## Recipes
- **New API usage:** `make gen` (types from openapi.yaml) → hook in `src/api/hooks/<area>.ts` with a stable query key
  → invalidate in the relevant mutations.
- **New chart:** add a `ChartDef` in `src/charts/registry.ts` with `toTable` (table twin is mandatory) and tests.
- **New persisted preference:** add to the persisted slice in `uiStore`/`viewStore`, include in the prefs schema,
  test hydration.
- **New grid column:** add to the column registry (type, label, default width, visibility) and to the backend
  `ColumnRegistry`; extend export.

## Tests
- `npm run test` (Vitest + MSW), `npm run e2e` (Playwright; needs `make up`).
- Screenshot UI changes in dark and light themes for the PR.
````

### Appendix C — ADRs

Template `docs/adr/0000-template.md`:

```markdown
# NNNN. Title
- Status: proposed | accepted | superseded by NNNN
- Date: YYYY-MM-DD
## Context
## Decision
## Consequences (positive, negative, follow-ups)
## Alternatives considered
```

**Initial ADRs (Phase 0):**

| ADR | Title |
|---|---|
| 0001 | Project foundations: name, license, monorepo, self-hosted positioning |
| 0002 | Go stdlib `net/http` + spec-first OpenAPI 3.0.3 with `oapi-codegen` strict server |
| 0003 | PostgreSQL with `pgx`, `sqlc` and `goose`; dynamic filters via a whitelisted builder |
| 0004 | Money as `int64` minor units |
| 0005 | Booking dates as date-only values (and the Go type chosen) |
| 0006 | River for background jobs; transactional enqueue |
| 0007 | Multi-tenancy: `user_id` everywhere + Postgres RLS as defence in depth |
| 0008 | Query endpoints use `POST` with a `QueryContext`; offset windows for the virtual grid |
| 0009 | Frontend stack: React + Vite, TanStack Query/Virtual, Zustand, CSS Modules with legacy tokens, no chart or grid libraries |

**Expected later:**

| ADR | Title | Phase |
|---|---|---|
| 0010 | Password hashing parameters | 5 |
| 0011 | Sessions and CSRF strategy | 5 |
| 0012 | Rate limiting per replica | 5 |
| 0013 | CSV export format | 7 |
| 0014 | Rule precedence | 15 |
| 0015 | Transfer defaults | 15 |
| 0016 | CNPG backup method | 16 |
| 0017 | GitOps promotion flow | 16 |
| 0018 | SLO targets | 17 |
| 0019 | Assistant: server-side LLM orchestration, SSE, egress allowlist (vs the legacy browser→LLM with runtime CSP) | 13 |
| 0020 | User annotations keyed by dedupe identity | 4 |
| 0021 | Groups: one label per transaction (tags in the backlog) | 10 |
| 0022 | Import fast path for known layouts | 6 |
| 0023 | LLM endpoint placement (in-cluster Ollama vs LAN/tailnet) | 16 |
| 0024 | Duplicate overrides: split scope, value source rows, re-materialisation | 6 |
| 0025 | Legacy backup migration: client-side decryption, server-side replay | 14 |
| 0026 | Investments: one manual document per user, PLN only, `float64` for ratios and projections | 12 |

### Appendix D — `docs/PROGRESS.md` template

```markdown
# Progress

**Current phase:** 0 — Repository bootstrap · **Branch:** phase/00-bootstrap · **Status:** in progress

## Decisions from our Product Engineer
- Name: … · GitHub owner: … · License: … · Demo hosting: … · Homelab: ingress …, issuer …, storage class …, backup bucket …

## Current phase tasks
- [ ] 0.1 …
- [ ] 0.2 …

## Deviations from the plan (with reason)
-

## Follow-ups (must be done in a later phase)
-

## Backlog (ideas, not scheduled)
-

## Session log
- YYYY-MM-DD — what was done — what is next
```

### Appendix E — PR template (`.github/pull_request_template.md`)

```markdown
## What
## Why (plan phase/task refs)
## How to verify (commands, URLs)
## Screenshots (dark + light, if UI)
## Checklist
- [ ] Tests added/updated; `make check` green; integration green (backend)
- [ ] `make gen` run; generated code committed
- [ ] PROGRESS.md (and PARITY.md / ADR if applicable) updated
- [ ] No new dependencies, or approved in: …
- [ ] No private data
```

### Appendix F — Makefile targets

| Target | Does |
|---|---|
| `help` | List targets |
| `check` | `lint` + `typecheck` + `test` + `gen-check` + `tools/check-private.sh` |
| `lint` / `fmt` | golangci-lint + ESLint / gofmt, goimports, Prettier |
| `typecheck` | `tsc --noEmit` |
| `test` | Go unit tests (`-short`) + Vitest |
| `test-integration` | All Go tests incl. testcontainers |
| `test-e2e` | Playwright against `compose.ci.yaml` |
| `fuzz` | All fuzz targets for `FUZZTIME` (default 30s each) |
| `bench` | Go benchmarks with `-benchmem` |
| `gen` / `gen-check` | sqlc + oapi-codegen + openapi-typescript / regenerate and fail on diff |
| `oracle` | Run `tools/legacy-oracle` → `testdata/oracle/` |
| `db-up` `db-down` `db-reset` `db-psql` | Local Postgres |
| `migrate-up` `migrate-down` `migrate-status` `migrate-new name=` | goose via `finuance migrate` |
| `up` `down` `logs` | Full Compose stack |
| `dev-api` `dev-worker` `dev-web` | Local processes with `.env` |
| `seed-demo email=` | Demo data for a user |
| `images` | Build both images locally |
| `helm-lint` | `helm lint` + `kubeconform` |
| `loadtest` | k6 against `LOADTEST_BASE_URL` |
| `eval-assistant` | Run `evals/assistant` against `FINUANCE_AI_BASE_URL` (`MODEL=` optional) |
| `llmmock` | Start the mock OpenAI-compatible server for local UI work (`go run ./cmd/llmmock`) |

### Appendix G — Definition of done (every PR)

* The behaviour matches the spec (§4–§7) or the deviation is recorded (PROGRESS + `ALLOWED_DIFFS.md` or an ADR).
* Tests at the right layer(s); no flaky tests (run the new tests 3× locally).
* `make check` green; `make test-integration` green for backend changes; E2E green for UI flows from Phase 9 on.
* The OpenAPI contract is updated first and generated code is committed.
* Logs, metrics and traces exist for new server-side operations that can fail or be slow.
* Docs updated: PROGRESS, PARITY (if a legacy feature is touched), README (if setup changed), RUNBOOK (if ops changed).
* Screenshots in both themes for UI changes.
* No secrets, no private data, no new dependencies without approval.

### Appendix H — Risks and mitigations

| Risk | Mitigation |
|---|---|
| Scope creep / never finishing | Strict phases and gates; backlog; M3 (`v0.1.0`) is a shippable parity product on its own |
| Silent behaviour drift from the legacy app | Legacy oracle (Phase 1), differential tests (Phase 3), parity harness (Phase 14), `ALLOWED_DIFFS.md` |
| Server round-trips make the UI feel slower than the in-browser legacy app | Windows + `keepPreviousData`, debounces, indexes and `EXPLAIN` reviews, perf budgets. Fallback (ADR if needed): small views fetch all rows once and filter client-side. |
| Real financial data leaks into git or prompts | Anonymiser run by our Product Engineer, `.gitignore`, `check-private.sh`, gitleaks, the CLAUDE.md rule |
| `COPY` into RLS tables fails | Use `INSERT … SELECT FROM unnest(…)` (Phase 4, task 4.4) |
| `unaccent`/collation differences between Go and SQL | Normalisation only in Go (shadow `_n` columns); ICU collation only for sorting |
| Homelab resources too small for the full observability stack | Light values; reuse what our Product Engineer already runs; Tempo/Loki optional |
| Tooling changes (CNPG backups, Argo CD hooks, library majors) | Check current docs at the start of Phases 16–17; pin versions; Renovate |
| Agent sessions lose context in long phases | `PROGRESS.md` hand-off, small tasks, directory-level CLAUDE.md files |
| Owner doesn't learn Go because the agent writes everything | Appendix I |
| Small local models make wrong tool calls (dates, spellings) | Legacy prompt rules, `find_values`, plan-mode fallback, eval suite with per-category scores, model comparison |
| LLM latency on homelab hardware | Streaming, visible steps, stop button, small default `maxRows`, concurrency caps; the assistant is optional and the app is complete without it |
| Assistant leaks data to an unintended endpoint | Config-only endpoint, private-address check, NetworkPolicy egress, browser CSP `self` only, E2E request-origin assertion |
| Legacy backup format drifts (the single-file app keeps changing) | Migration tests run on backups that the oracle generates from the current legacy file; unknown keys are reported, never fatal |
| Duplicate overrides make the dedupe model hard to reason about | One reference implementation (`Rematerialize`) plus a property test against it |

### Appendix I — Learning Go with this project (for our Product Engineer)

* **Phase 3 is the best place to write Go yourself.** Let Claude Code write the failing tests first: oracle-driven table tests for `Norm`, `ParseNumber`, `DetectDelimiter`, the tokenizer and `Shape`. Then implement them yourself, and ask Claude Code to review rather than write.
* At each gate, ask Claude Code to explain one package top-down: types, data flow, why the errors are shaped the way they are. Then write the explanation in your own words in the PR.
* Practise reading generated code once (`oapi-codegen` strict server, sqlc). Knowing what it generates is a classic interview question.
* Learn the concurrency patterns by writing the import concurrency test (Phase 6) yourself.
* Write the `internal/llm` SSE parser yourself (Phase 13). It is a compact exercise in `bufio`, streaming and `context` cancellation.
* Keep a `docs/notes/go-learnings.md`. It becomes interview material.

### Appendix J — Backlog (not scheduled)

* OIDC login (Authentik/Keycloak/Zitadel); WebAuthn passkeys.
* Household sharing (multiple users per account with roles). *(Phase 19 shares groups of expenses between friends instead; shared accounts stay here.)*
* Manual transactions and split transactions; attachments (receipts). *(Manual cash expenses and splits in shared groups: Phase 19; receipts: Phase 20.)*
* Budgets per month with rollover; savings goals. *(Goals: Phase 20.)*
* Bank profile presets shipped with the app (PKO BP, mBank, ING, Santander, Pekao, Millennium…).
* Local-LLM categorisation suggestions behind the rules engine (builds on Phases 13 and 15).
* Multi-label tags (many-to-many) in addition to the single group.
* Saved views (named filter sets), shareable as links.
* Assistant: mini charts rendered from `aggregate_transactions` results; voice input; answers pinned to the overview.
* Embedding-based payee clustering to merge merchant spelling variants.
* Investments: price feeds, broker statement import, net worth over time.
* Duplicate review across accounts (e.g. the same card appearing in two exports).
* Import from email attachments, e.g. an n8n workflow posting statements to `POST /imports`, with an API token. *(API tokens: Phase 20.)*
* Anomaly alerts (unusual spend, new subscription detected), email/ntfy notifications. *(In-app notifications: Phase 19; assistant digests: Phase 20.)*
* Re-import of `GET /me/export` archives (full restore).
* PWA / offline read-only cache; mobile layout.
* Desktop app (Wails v3) reusing the SPA. Explicitly out of scope for this plan.
* UI i18n (Polish).
