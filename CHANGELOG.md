# Changelog

All notable changes to Finuance. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/). Release candidates (`vX.Y.Z-rc.N`) use the
notes of their version.

## [Unreleased]

### Added

- **Administrators** (Phase 19): an admin panel for accounts (create with a one-time password,
  role, disable, reset password, sign out everywhere, delete), per-account and default limits
  (accounts, groups, imports per day, largest file, assistant questions per hour), where a new
  user's view starts, feature switches (Overview, Assistant) and instance usage. Administrators see
  counts, never anyone's financial data; their actions appear in the affected account's Activity.
  `finuance admin grant-admin|revoke-admin` and `create-user -admin`.
- **Colour themes**: Indigo, Fjord, Grove and Ember, each dark and light, picked in Settings →
  Appearance; administrators make more in a theme editor with a live preview and approve them only
  when every text colour reaches WCAG AA.

### Changed

- The assistant's send button uses the solid accent colour, like the other filled buttons.

## [0.2.0]

Categories, rules and transfers, group reports, assistant actions, an activity log, demo mode, and
what's needed to run Finuance on Kubernetes with observability and security scans.

### Added

- **Categories and rules**: rename, merge or delete categories (re-imports follow); rules from any
  search and filters, applied in order to every import with a live preview, never overriding a
  category set by hand; rule suggestions after repeated manual categorisation; bulk category and
  transfer actions on a selection.
- **Transfers** between the user's own accounts are detected, shown with a badge (and hideable), and
  left out of charts, the overview and the assistant's totals.
- **Group reports** with totals, span, spending per day, categories, top payees, an optional budget
  and CSV export; weekly and yearly recurring payments besides monthly ones.
- **Assistant actions**: the assistant can propose categorising rows, grouping them or creating a
  rule, applied only when the user presses *Apply*; two more tools (budget status, group summary).
- **Activity log** (Settings → Activity): sign-ins and failed sign-ins, sign-outs, password changes,
  revoked sessions, imports, deletions, exports, rule runs, migrations, duplicate decisions and
  applied assistant proposals, with address and browser, kept for 365 days.
- **Demo mode** (`FINUANCE_DEMO_MODE`): "Try the demo" gives each visitor a throwaway user with a demo
  year, deleted after 24 hours, with limits on uploads, demo creation and the assistant.
- **Kubernetes deployment**: Helm chart, CloudNativePG database chart with backups and point-in-time
  recovery, Sealed Secrets, an Argo CD app-of-apps, and a weekly rehearsal on kind in CI.
- **Observability**: traces from requests into background jobs and their SQL, job metrics, six Grafana
  dashboards and SLO alert rules as code, and a local Grafana profile (`make up-observability`).
- **Load test** (k6, `make loadtest`) and **security scans**: OWASP ZAP weekly, Trivy on images and
  the repository at HIGH and CRITICAL, `npm audit` ([docs/security-scan.md](docs/security-scan.md)).
- **Running on single-node k3s** (the reference environment, step by step in
  [docs/RUNBOOK.md](docs/RUNBOOK.md#bootstrap-on-single-node-k3s)): a Tailscale `*.ts.net` certificate
  renewed by a host timer, the ingress reachable only from the tailnet, an in-cluster backup bucket
  (RustFS), per-machine values from a private repository as a second Argo CD source, the monitoring
  stack (kube-prometheus-stack, Tempo, Finuance's dashboards and e-mail alerts) through Argo CD, and
  an image admission policy (a built-in `ValidatingAdmissionPolicy`: per-namespace registries).
- `FINUANCE_AI_IDLE_TIMEOUT` (default 60 s): how long a model stream may stay silent, for model
  servers that read a long prompt for over a minute before the first token.
- Ingress monitoring: Traefik's request metrics, an "At the ingress" row on the SLO dashboard, and
  alerts when requests fail at the ingress or no api pod is ready.
- Drills and scans for a deployed cluster: `tools/homelab-drill.sh` (restore, pod kills under load,
  SSE timing) and `tools/openvas-scan.sh` (Greenbone OpenVAS, and a port scan from the LAN).

### Changed

- Search uses a per-user trigram index (migration 00011, needs the `btree_gin` extension, created by
  the migration): its cost no longer grows with the number of users. Building the index blocks writes
  to transactions for about 25 s per million rows.
- A new user's first large import no longer slows down quadratically (20,000 rows: minutes → seconds).
- The web container sends each security header once and uses relative redirects.
- The api's readiness check reports a database that only answers slowly as busy for 30 seconds
  instead of failing, so a busy database no longer takes every replica out of the Service at once
  (the probe also allows 3 s); a real error or a schema behind still fails at once.
- The database bootstrap lets PostgreSQL's monitoring role (`pg_monitor`, statistics only) connect,
  so CloudNativePG's database metrics work. An existing cluster needs the grant once (RUNBOOK
  "Monitoring").
- Helm chart: `ingress.entryPoints` (e.g. HTTPS only); `otel.endpoint` must be the full traces URL;
  the migrations Job retries in the same pod.
- The duplicates window no longer re-renders every group while you type in its filter.

### Security

- `golang.org/x/net` 0.62.0 (GO-2026-6866, GO-2026-6860; not reachable from Finuance's code).
- The web image upgrades zlib (CVE-2026-85091) and musl (CVE-2026-107891) from the Alpine mirror, since
  its nginx base image hasn't been rebuilt with the fixes (`--build-arg APK_OFFLINE=1` skips this for
  builds without network access).
- Trivy, govulncheck and `npm audit` at every severity before the release: nothing open apart from
  `golang.org/x/crypto` GO-2026-5932, which has no fix and isn't reachable ([docs/security-scan.md](docs/security-scan.md)).

## [0.1.0]

The first release: the single-file prototype rebuilt as a self-hosted, multi-user web application,
with every feature of the prototype and a way to move its backups over.

### Added

- **Import bank exports** (CSV/TSV): drag and drop or ⌘/Ctrl O. An import window shows the raw file and a
  preview before anything is saved; encoding (UTF-8, Windows-1250, ISO-8859-2, Windows-1252), delimiter,
  header row (also after a long preamble, as in mBank exports, or with blank header columns, as in PKO BP
  exports), date order, one signed amount or debit/credit columns, inverted signs and summary rows are
  detected and can be changed. Remembered layouts and a fast path for known files.
- **Overlapping exports are merged**: a transaction is kept as often as it occurs in any one file; a
  duplicates window shows every version and lets you pick one or keep both.
- **The transaction table** for 100,000+ rows: accent-insensitive search, a per-column filter language
  (`>100`, `-50..50`, `2025-03`, `!uber`, `=`), value checklists, sorting, resizable and hideable
  columns, CSV export, an inspector with the bank's original row, categories and notes, keyboard
  navigation, multi-selection with a quick bar, and colour-coded groups that survive re-imports.
- **Insights**: summary, spending by category, cash flow, month pace, top payees and a daily calendar,
  following the table's filters and period; each chart opens larger with its numbers as a table.
- **Monthly overview**: income, spending, savings and projection, highlights, budgets per category,
  recurring payments, money flow, largest expenses, savings rate, weekday spending, the account
  balance, and investment, yield-on-cost and FI/RE cards.
- **AI assistant** (optional): questions in English or Polish, answered by a model the admin connects
  (LM Studio, Ollama, vLLM or any OpenAI- or Anthropic-compatible server, local or cloud) through
  read-only lookups shown with each answer; cloud models need each user's consent; chats are deleted
  after 2 days unless the user chooses otherwise.
- **Migration from the single-file app**: Settings → Data reads a backup (decrypting a locked one in
  the browser) and moves the files, groups and labels, duplicate decisions, budgets, opening balance,
  investments, chats and appearance settings, with a report of anything that didn't match.
- **Accounts and privacy**: sign-up or admin-created users, Argon2id passwords, server-side sessions,
  CSRF protection, rate limits, row-level security in PostgreSQL for every user table, a strict
  Content Security Policy, data export (zip) and account deletion.
- **Operations**: one Go binary (API, worker, migrations, admin CLI), PostgreSQL, a React web app
  behind nginx; Prometheus metrics and OpenTelemetry traces; Docker Compose for development and
  production (`compose.prod.yaml` with the released images).
- **Quality**: a legacy oracle compares the import engine, filters, charts, overview and assistant
  tools with the prototype, and a parity harness compares both apps on the same data
  (`docs/parity-report.md`); accessibility checked with axe (WCAG 2.1 AA, `docs/a11y.md`).

### Release artefacts

- Images `ghcr.io/x2db736ac85h/finuance-api` and `ghcr.io/x2db736ac85h/finuance-web` for linux/amd64 and
  linux/arm64, signed with cosign (keyless), with SPDX SBOM attestations.
- `compose.prod.yaml` and `finuance.env.example` (copy it to `.env`): everything needed to run it with Docker Compose.
