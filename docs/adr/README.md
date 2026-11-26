# Architecture decision records

One file per decision, numbered in the order they were taken. A record is not rewritten when things change: it gets an update note under its status line, or a later record replaces it. New records start from [0000-template.md](0000-template.md).

| No. | Decision | Status | Date |
|---|---|---|---|
| [0001](0001-project-foundations.md) | Project foundations: name, license, monorepo, self-hosted positioning | accepted | 2026-10-20 |
| [0002](0002-http-stdlib-and-openapi.md) | Go stdlib `net/http` + spec-first OpenAPI 3.0.3 with `oapi-codegen` strict server | accepted | 2026-10-20 |
| [0003](0003-postgres-pgx-sqlc-goose.md) | PostgreSQL with `pgx`, `sqlc` and `goose`; dynamic filters via a whitelisted builder | accepted | 2026-10-20 |
| [0004](0004-money-minor-units.md) | Money as `int64` minor units | accepted | 2026-10-20 |
| [0005](0005-date-only-booking-dates.md) | Booking dates as date-only values (`platform.Date`) | accepted | 2026-10-20 |
| [0006](0006-river-background-jobs.md) | River for background jobs; transactional enqueue | accepted | 2026-10-20 |
| [0007](0007-multi-tenancy-rls.md) | Multi-tenancy: `user_id` everywhere + Postgres RLS as defence in depth | accepted | 2026-10-20 |
| [0008](0008-query-endpoints-post-querycontext.md) | Query endpoints use `POST` with a `QueryContext`; offset windows for the virtual grid | accepted | 2026-10-20 |
| [0009](0009-frontend-stack.md) | Frontend stack: React + Vite, TanStack Query/Virtual, Zustand, CSS Modules with legacy tokens, no chart or grid libraries | accepted | 2026-10-20 |
| [0010](0010-password-hashing.md) | Password hashing: argon2id in PHC format, OWASP baseline parameters | accepted | 2026-10-21 |
| [0011](0011-sessions-and-csrf.md) | Server-side sessions in an HttpOnly cookie; CSRF by custom header + Origin check | accepted | 2026-10-21 |
| [0012](0012-rate-limiting.md) | Rate limiting: in-memory token buckets per replica | accepted | 2026-10-21 |
| [0013](0013-per-account-import-lock.md) | Serialise writes to an account's transactions with a row lock on the account | accepted | 2026-10-21 |
| [0014](0014-csv-export-format.md) | CSV export: Polish-Excel-friendly text, streamed from one query | accepted | 2026-10-21 |
| [0015](0015-assistant-connections-and-run-loop.md) | Assistant: provider-agnostic connections, typed read-only tools, server-side run loop | accepted | 2026-10-23 |
| [0016](0016-legacy-migration.md) | Migrate legacy backups by replaying the legacy keys on each imported file | accepted | 2026-10-23 |
| [0017](0017-rules-and-category-identity.md) | Rules apply from scratch in order; categories keep their bank names through aliases | accepted | 2026-10-23 |
| [0018](0018-transfers.md) | Transfers: detected pairs, rule marks and the user's decision; left out of charts | accepted | 2026-10-23 |
| [0019](0019-assistant-action-proposals.md) | Assistant action proposals | accepted | 2026-10-24 |
| [0020](0020-llm-endpoint-in-the-cluster.md) | Where the assistant's model runs when Finuance is on Kubernetes | accepted | 2026-10-24 |
| [0021](0021-audit-log.md) | Audit log (Settings → Activity) | accepted | 2026-10-26 |
| [0022](0022-phase-18-reference-environment.md) | Phase 18 runs on a single-node k3s reference environment | accepted | 2026-10-27 |
| [0023](0023-private-environment-repository.md) | Machine-specific values live in a private environment repository | accepted | 2026-10-30 |
| [0024](0024-image-admission.md) | Image admission on the cluster: registries now, signatures in Phase 23 | accepted | 2026-11-02 |
| [0025](0025-instance-administration.md) | Instance administration without access to financial data | accepted | 2026-11-03 |
| [0026](0026-colour-themes.md) | Colour themes and the theme editor | accepted | 2026-11-03 |
| [0027](0027-friends-and-friend-codes.md) | Friends and friend codes | accepted | 2026-11-04 |
| [0028](0028-shared-groups.md) | Shared groups: copies, splits and row-level security by membership | accepted | 2026-11-04 |
| [0029](0029-manual-transactions.md) | Transactions added by hand (cash accounts) | accepted | 2026-11-04 |
| [0030](0030-ai-changes-and-undo.md) | The assistant's changes: recorded with the change, marked, and undoable | accepted | 2026-11-06 |
| [0031](0031-goals.md) | Goals: savings targets and reductions, with progress worked out on read | accepted | 2026-11-06 |
| [0032](0032-receipts-by-photo.md) | Receipts by photo: read by the model, matched read-only, applied as proposals | accepted | 2026-11-06 |
| [0033](0033-assistant-digests.md) | Assistant digests: background runs in the worker that only propose | accepted | 2026-11-06 |
| [0034](0034-advice-library.md) | Advice library: admin-written articles searched with PostgreSQL full-text search | accepted | 2026-11-06 |
| [0035](0035-assistant-administration.md) | Administration → Assistant: connections in the database, sealed API keys, per-model tuning | accepted | 2026-11-06 |
| [0036](0036-personal-access-tokens.md) | Personal access tokens: bearer tokens with a read or write scope, never for the sensitive routes | accepted | 2026-11-06 |
| [0037](0037-supply-chain.md) | Supply chain: pinned references, checked tools, scans on a schedule, approved and attested releases | accepted | 2026-11-19 |
| [0038](0038-request-path-database-limits.md) | Time limits for the request path's database work: one place, different for the api and the worker | accepted | 2026-11-19 |
| [0039](0039-image-signatures.md) | Image signatures at admission: Sigstore policy-controller, Finuance's images by digest | accepted | 2026-11-19 |
| [0040](0040-two-step-sign-in.md) | Two-step sign-in with one-time codes (TOTP), recovery codes, the rule for administrators | accepted | 2026-11-20 |
| [0041](0041-synthetic-users.md) | Synthetic users: the server decides whose traffic is synthetic (`users.kind`, the `traffic` label) | accepted | 2026-11-21 |
| [0042](0042-log-and-trace-pipeline.md) | The log and trace pipeline: Alloy in front of Loki and Tempo, the log stages, one switch | accepted | 2026-11-21 |
| [0043](0043-traffic-simulator.md) | The traffic simulator: a separate client that plays synthetic users on a lab, passwords from one seed, ceilings in the code | accepted; decision 7 changed by 0044 | 2026-11-21 |
| [0044](0044-load-mode.md) | Load mode: the animals load a lab within ceilings of their own and replace the load-test users | accepted | 2026-11-23 |
| [0045](0045-cloud-model-providers.md) | Cloud model providers: a connection that authenticates with Google's credentials, where its token may go, a budget per connection | accepted | 2026-11-25 |
| [0046](0046-assistant-memory.md) | The assistant's memory: one note per person, added to the system prompt, never written by the assistant itself | accepted | 2026-11-26 |
