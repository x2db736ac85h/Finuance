# Architecture

How Finuance is put together, for someone reading the code for the first time. The plan (`docs/PLAN.md`) is the specification. This document explains what is built and why it is shaped that way. It grows phase by phase. Phase 4 adds the data model and tenant isolation; Phase 8 the web app.

> 🚧 This describes the system after Phase 11. Parts that come later (the `rules` package, the overview, the AI assistant and its LLM server, the Kubernetes deployment) appear in the diagrams where they will fit, and are marked as planned in the text.

## Components

```mermaid
flowchart TB
    subgraph browser["Browser"]
        spa["React SPA (web/)<br/>openapi-fetch → /api/v1/*"]
    end
    web["web<br/>nginx, static files"]
    api["api — finuance serve<br/>net/http + strict OpenAPI handlers<br/>stateless, N replicas"]
    worker["worker — finuance worker<br/>River jobs"]
    pg[("PostgreSQL<br/>app tables + RLS<br/>River job tables")]
    llm["LLM server (optional)<br/>OpenAI-compatible"]

    spa -- "static assets" --> web
    spa -- "JSON over HTTPS, session cookie<br/>same origin: /api → api" --> api
    api -- "pgx: tx + app.user_id" --> pg
    worker -- "pgx: tx + app.user_id" --> pg
    api -. "enqueue jobs (River InsertTx)" .-> pg
    api -. "assistant only" .-> llm
```

- **One Go binary, one image.** The subcommands are `serve`, `worker`, `migrate`, `seed-demo`, `admin`, `healthcheck` and `version`.
- **api** serves HTTP and never runs jobs. Slow work, such as committing an import, is enqueued in the same database transaction as the state change, so a job exists exactly when its change was committed.
- **worker** runs the River workers on the same domain packages as api.
- **web** serves static files. Everything is same-origin (ingress or nginx path routing), so there is no CORS and cookies stay `SameSite=Lax`.

### Backend packages

```
cmd → app → {httpapi, jobs} → {ledger, query, insights, rules, auth} → {store, ingest, money, platform}
```

`rules` (Phase 15) is planned; the other packages exist. `insights` holds the chart rules; its aggregation SQL lives in `query` (`insights.go`), the only package besides `store` that runs SQL.

| Package | Role |
|---|---|
| `ingest`, `money` | Pure parsing and normalisation (Phase 3): bytes in, staged rows out. No database, no clock. |
| `store` | The database: the pool, migrations, sqlc queries (`store/queries/*.sql` → `store/db`), and the only entry points to user data (`WithUser`, `WithUserReadOnly`, `WithoutUser`). |
| `ledger` | Accounts, categories, groups, imports, transactions, annotations, budgets and profiles. Every method runs through `WithUser` and returns domain types, never sqlc rows. |
| `httpapi`, `jobs` | Map HTTP requests and River jobs onto the domain packages. They contain no SQL. |

A guard test (`store/guard_test.go`) parses every non-test file outside `internal/store`. It fails on direct `Query`/`Exec`/`Begin` calls, and on building a sqlc querier (`db.New`) that would bypass `WithUser`.

### Web app (`web/`)

A Vite + React + TypeScript single-page app that recreates the legacy `finuance.html` UI: the legacy CSS is ported verbatim (`src/styles/`), and components reuse the legacy class names.

| Folder | Role |
|---|---|
| `api/` | `openapi-fetch` client typed from `api/openapi.yaml` (`schema.d.ts` is generated). Errors become `ApiError` with the RFC 9457 problem; a `401` sends the router to `/login?next=…`. TanStack Query hooks per resource in `api/hooks/`. |
| `stores/` | Zustand: `uiStore` (toggles, theme, size, chart and assistant settings), `viewStore` (per-view sort, columns, period; search and filters are session-only). `prefsSync.ts` saves the persisted slices with a 500 ms debounce and the document version; on `409` the server's document wins and local changes are laid back on top, once. |
| `auth/` | Sign-in, sign-up and `RequireAuth`, which loads `/me` and `/preferences` and hydrates the stores before the app renders. |
| `shell/` | Title bar, sidebar (accounts with their import history), status bar, empty state, drop overlay, settings popover, keyboard manager. |
| `import/` | The upload queue (`queue.ts`: one file at a time, fast path, duplicate files) and the import window at `/imports/:id`. The window's working copy is a `PreviewEditor` (`editor.ts`): edits wait 300 ms and go to the server as one `PATCH`, and the screen renders the server's re-staged preview. Committed imports are edited locally and sent with *Reprocess*. |
| `ui/` | Primitives: Popover and Menu (Floating UI), Modal, Toast queue, Tooltip, buttons, inputs. Open modals and popovers register as layers, so the global keyboard manager leaves their keys alone. |

The production image (`deploy/docker/Dockerfile.web`) is unprivileged nginx: static files with a strict CSP (`script-src 'self'; style-src 'self'; connect-src 'self'`), immutable hashed assets, `no-cache` for `index.html`, and `/api/` proxied to the API without buffering (exports stream through). No HTML is ever built from strings (an ESLint rule bans `innerHTML` and `dangerouslySetInnerHTML`), which is what makes that CSP possible.

## Data model

All ids are UUIDv7 generated in Go (`platform.NewID`), so they are time-ordered and index-friendly. Money is `bigint` minor units with an ISO currency code (ADR 0004). Booking dates are `date` (ADR 0005). The `*_n` columns hold `ingest.Collapse(ingest.Norm(x))`, computed in Go, so search and filters use the same normalisation as the importer.

The `pl_natural` ICU collation (`pl-u-kn-true`) sorts names by the Polish alphabet (`ł` after `l`, `ż` after `z`) and numbers by value (`Konto 2` before `Konto 10`). It is applied in queries (`ORDER BY name COLLATE pl_natural`), not on columns. Equality and indexes therefore stay byte-based.

```mermaid
erDiagram
    users ||--o{ sessions : "logs in"
    users ||--o| user_preferences : has
    users ||--o{ accounts : owns
    users ||--o{ categories : owns
    users ||--o{ groups : owns
    users ||--o{ imports : uploads
    users ||--o{ import_profiles : remembers
    users ||--o| investment_profile : has
    users ||--o{ investment_positions : has
    accounts ||--o{ imports : "imported into"
    accounts ||--o{ transactions : holds
    accounts ||--o{ tx_annotations : "keyed by"
    accounts ||--o{ dedupe_overrides : "keyed by"
    imports ||--|| import_files : "raw bytes"
    imports ||--o{ import_rows : "staged rows"
    imports ||--o{ transactions : "value source"
    imports ||--o{ dedupe_overrides : "preferred"
    import_rows }o--o| transactions : "linked to (provenance)"
    categories ||--o{ transactions : categorises
    categories ||--o{ budgets : "limited by"
    categories ||--o{ tx_annotations : "manual category"
    groups ||--o{ transactions : labels
    groups ||--o{ tx_annotations : "label"
    transactions |o--o| transactions : "transfer peer"

    users {
        uuid id PK
        citext email UK
        text password_hash
        bool is_demo
    }
    sessions {
        bytea token_hash PK "SHA-256 of the cookie token"
        uuid user_id FK
        timestamptz expires_at
    }
    accounts {
        uuid id PK
        uuid user_id FK
        text name_n "UNIQUE (user_id, name_n)"
        char3 currency
        text number_hint "last 4 digits only"
    }
    imports {
        uuid id PK
        uuid user_id FK
        uuid account_id FK "NULL until chosen"
        bytea sha256 "UNIQUE per user unless failed"
        text status "previewed, committing, committed, failed"
        jsonb mapping
        bytea header_signature
    }
    import_rows {
        uuid import_id PK
        int row_index PK
        bytea fingerprint
        int occurrence
        jsonb raw "[{h, v}, …] in column order"
        text parse_error
        uuid transaction_id FK
    }
    transactions {
        uuid id PK
        bigint seq "identity: stable order"
        uuid account_id FK
        bytea fingerprint
        int occurrence
        uuid split_scope "nil unless keep-both"
        uuid source_import_id FK
        date booked_on
        bigint amount_minor
        text search_text "trigram index"
    }
    tx_annotations {
        uuid account_id PK
        bytea fingerprint PK
        int occurrence PK
        uuid split_scope PK
        uuid group_id FK
        uuid category_id FK
        text note
    }
    dedupe_overrides {
        uuid account_id PK
        bytea fingerprint PK
        text mode "prefer or keep_both"
        uuid preferred_import_id FK
    }
```

Every table except `users` and `sessions` also has `user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE`. It is omitted from the diagram for readability. `budgets`, `import_profiles`, `investment_profile` and `investment_positions` are left out of the attribute lists. The full DDL is `backend/internal/store/migrations/00002_core.sql` (PLAN §5.2).

### De-duplication in the schema

- **Identity.** A transaction's identity within an account is `(fingerprint, occurrence, split_scope)`, and `UNIQUE (account_id, fingerprint, occurrence, split_scope)` enforces it.
  - The fingerprint is the SHA-256 of the legacy merge key (`ingest.FingerprintKey`).
  - `occurrence` numbers identical rows within one import, so two genuine identical coffees in one export stay two.
- **Committing** inserts staged rows with `ON CONFLICT DO NOTHING`. A row the account already has is a duplicate, linked rather than inserted. A transaction is therefore kept `max(count in any single import)` times.
- **`import_rows`** is both the staging area and the provenance record. Every committed row points at its transaction. Deleting an import deletes its rows, then the import-origin transactions that no row points at any more. A transaction that an overlapping export also contains survives.
- **`tx_annotations`** stores user edits (group, manual category, note) under the same identity, not the transaction id. Deleting and re-importing a file creates new transaction rows, and the commit re-applies the annotations to them (`ledger.CommitImport`, step 1b).
- **Value source and overrides.** Each transaction records the import its field values come from (`source_import_id`): by default the earliest committed import containing the identity. A `dedupe_overrides` row changes that per fingerprint: `prefer` takes the values from one import, `keep_both` gives each import its own copy (`split_scope` = the first 16 bytes of the file's SHA-256). `ledger.Rematerialize` recomputes one fingerprint from scratch and is the reference the incremental commit and delete paths are property-tested against.
- **One writer per account.** Commit, delete, reprocess and override changes lock the account row (`FOR NO KEY UPDATE`) before touching its transactions (ADR 0013).

### Import lifecycle

```mermaid
stateDiagram-v2
    [*] --> previewed: POST /imports (parse, stage, 409 on a known file)
    previewed --> previewed: PATCH (re-parse with new options / mapping)
    previewed --> committing: POST /commit (job enqueued in the same tx)
    previewed --> committing: POST /imports?autoCommit=true (profile + no errors + account)
    committing --> committed: CommitImport job
    committing --> failed: last of 3 attempts fails
    committed --> committing: POST /reprocess (restage in one tx, job enqueued)
    previewed --> [*]: DELETE
    committing --> [*]: DELETE (the job then finds nothing and stops)
    committed --> [*]: DELETE (orphans removed, sources re-materialised)
    failed --> [*]: DELETE
```

- **Upload.** The api reads one multipart file (`FINUANCE_MAX_UPLOAD_BYTES`), rejects binary files (NUL in the first 8 KiB) and identical bytes (SHA-256 per user → `409` with `existingImportId`). It parses with the user's saved profile for this header layout if there is one, else with detection, stages every row, and pre-fills the account when the own-account column matches exactly one account's number hint.
- **Commit job** (`jobs.CommitImportWorker`): upsert the bank categories, insert the new identities, re-apply annotations, link every staged row, mark the import committed, re-materialise overridden and re-sourced fingerprints, record the stats, and remember the mapping as the layout's profile.

### Query engine (`internal/query`)

Every transaction read — the table window, distinct values, id ranges, the CSV export, category colours, the insight charts, and later the overview — goes through one compiler, so all views agree.

```mermaid
flowchart LR
    ctx["QueryContext<br/>accounts · search · filters · period · toolFilter"] --> compile["Compile<br/>registry (§4.5 whitelist) + dsl.Parse"]
    compile --> preds["predicates with $n placeholders<br/>+ args (all user values)"]
    preds --> anchor["period anchor<br/>max(booked_on) of the accounts"]
    anchor --> base["base = filters + search"]
    base --> scoped["scoped = base ∩ period"]
    scoped --> stmts["window (ids first, then rows) · totals · distinct · ids · export"]
```

- **The registry** (`columns.go`) holds each API column's typed value, display text (`-79,99`, `dd.mm.yyyy`), normalised text and sort expression. It is the only source of SQL fragments. User text is always an argument, and LIKE patterns are escaped.
- **`base` and `scoped`.** Totals, stats and the data range are computed over `base` (filters, no period) in one pass. The window and the count use `scoped`. Comparisons use `base` within the previous period (the summary's delta), and the cash flow and pace charts read `base` for their whole history while the period only highlights.
- **Statements join only what they need**, so the common unfiltered and period-only totals are index-only scans on `tx_user_booked_idx`. Windows find their ids first and then fetch the display columns (late row lookup). See `docs/perf.md` for the measurements, and for why the trigram index on `search_text` is unused under forced RLS.
- **Execution** happens in `query/sql.go` inside `store.WithUser` (the store guard allows `internal/query`). The export streams one read-only query (ADR 0014).

## Tenant isolation

Two independent layers keep users apart.

1. **Explicit filters.** Every query on user data has `WHERE user_id = @user_id`. Every `ledger` method takes the user id.
2. **Row-level security** (ADR 0007, migration `00003_rls.sql`). Every user-owned table has RLS **enabled and forced**, with one policy:

   ```sql
   USING      (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
   WITH CHECK (user_id = nullif(current_setting('app.user_id', true), '')::uuid)
   ```

### How a request reaches the database

```mermaid
sequenceDiagram
    participant H as handler / job
    participant L as ledger
    participant S as store.WithUser
    participant P as PostgreSQL (role finuance_app)
    H->>L: ledger.Annotate(ctx, userID, ids, patch)
    L->>S: WithUser(ctx, userID, fn)
    S->>P: BEGIN
    S->>P: SELECT set_config('app.user_id', userID, true)
    S->>L: fn(q, tx)
    L->>P: UPDATE transactions … WHERE user_id = $1 AND id = ANY($2)
    Note over P: RLS adds user_id = app.user_id<br/>to every read and write
    L->>P: INSERT INTO tx_annotations … (WITH CHECK)
    S->>P: COMMIT (app.user_id ends with the transaction)
```

- **`WithUser`** opens a transaction and sets `app.user_id` with `set_config(…, true)`, the function form of `SET LOCAL`. The setting ends with the transaction, so it can't leak to the next user of a pooled connection (`TestWithUserScopesTheSettingToOneTransaction`).
- **`WithUserReadOnly`** is the same in a `READ ONLY` transaction. The assistant's tools run there, so a tool can't write even if its SQL is wrong.
- **`WithoutUser`** is for the auth tables (`users`, `sessions`), which have no RLS because a session is resolved before the user is known. In it, every user-owned table looks empty, and writes fail.
- **Roles.** The api and worker connect as `finuance_app`, which owns no tables and can't run DDL. Migrations run as `finuance_owner`. Because RLS is *forced*, even the owner sees no user rows without `app.user_id`, so a migration or a debugging session can't read user data by accident. A future data migration that must touch every user's rows switches `NO FORCE` inside its own transaction and restores `FORCE` before committing.

### What RLS does not cover

- **Foreign keys are checked without row security.** PostgreSQL would accept a transaction of user A that points at user B's group, because the group row exists. Every `ledger` write that stores a reference to a user-owned row therefore checks, in the same transaction, that the row is the caller's (`ledger/owned.go`). Under `WithUser` another user's row is invisible, so the check fails as *not found*. This covers:
  - an account on an import;
  - a group or category on an annotation;
  - a category on a budget.

  Composite foreign keys on `(user_id, id)` would enforce this in the schema itself. That would be a schema change for a later ADR.
- **Referential actions** (`ON DELETE CASCADE` / `SET NULL`) also run without row security. That is what makes deleting a user or an account remove all of its rows even under `FORCE` (`TestRLSCascadesStillWork`).
- **Bulk loads can't use `COPY FROM`.** PostgreSQL refuses it on tables with RLS. Staged rows are inserted with `INSERT … SELECT FROM unnest(…)`, 5,000 rows per statement (`store.InsertImportRows`).

### How it is tested

`backend/internal/store/rls_test.go` runs as `finuance_app` on every table the catalog lists as user-owned. A new table is covered automatically, and fails until it has a policy and seed data. Every query in the suite deliberately omits the `user_id` predicate.

| Test | Proves |
|---|---|
| `TestRLSReadsOnlyOwnRows` | With `app.user_id = A`, only A's rows are visible. |
| `TestRLSCannotWriteOtherUsersRows` | `UPDATE`/`DELETE` of B's rows affect 0 rows. |
| `TestRLSWithCheckRejectsOtherUserID` | Moving a row to B, or inserting with B's id, fails `WITH CHECK` (SQLSTATE 42501). |
| `TestRLSWithoutUserSeesNothing` | Without `app.user_id`, and as the owner role, tables are empty and writes fail. |
| `TestRLSCascadesStillWork` | Deleting a user removes their rows everywhere and leaves the other user intact. |
| `TestEveryUserTableHasForcedRLS` | The catalog: every table with `user_id` (except `sessions`) has RLS enabled, forced, and exactly its `_tenant` policy. |

Removing one table's policy makes all of them fail. This was checked by hand while the suite was written.
