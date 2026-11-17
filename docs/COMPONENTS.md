# Component map

Every part of Finuance as it is built at `v0.4.1` (written 2026-11-16), where its code lives and what it talks to. Parts that are planned and not built yet are marked **planned** with their phase (`docs/PLAN.md` §9). [ARCHITECTURE.md](ARCHITECTURE.md) explains the data model, tenant isolation and the query engine in depth; this page is the overview.

## 1. What runs

```mermaid
flowchart TB
    subgraph clients["Clients"]
        spa["Browser<br/>React app (web/)"]
        cli["fin CLI and scripts<br/>access token"]
    end

    subgraph edge["Edge"]
        ingress["Traefik (Kubernetes)<br/>TLS, HSTS"]
        web["web<br/>nginx: static files, CSP,<br/>/api proxy (Compose)"]
    end

    subgraph app["One Go binary, one image"]
        api["api — finuance serve<br/>HTTP, sessions, rate limits,<br/>assistant runs (streamed)"]
        worker["worker — finuance worker<br/>River jobs, digests, cleanups"]
        migrate["migrate<br/>before every rollout,<br/>owner role"]
        admincli["finuance admin / seed-demo<br/>users, first administrator, demo data"]
    end

    pg[("PostgreSQL 18<br/>app tables, forced row-level security<br/>River job tables")]
    backup[("Backup bucket<br/>WAL + daily base backups<br/>(RustFS or S3)")]
    llm["Model server<br/>Ollama / LM Studio<br/>or a cloud API"]

    subgraph obs["Monitoring"]
        prom["Prometheus + Alertmanager<br/>SLO rules, e-mail"]
        graf["Grafana<br/>6 dashboards"]
        tempo["Tempo<br/>traces, 72 h"]
    end

    spa --> ingress
    cli --> ingress
    ingress -- "/" --> web
    ingress -- "/api/" --> api
    api -- "app role, one user per transaction" --> pg
    worker -- "app role" --> pg
    migrate -- "owner role" --> pg
    admincli -- "app role" --> pg
    api -. "enqueue in the same transaction" .-> pg
    pg -- "Barman Cloud plugin" --> backup
    api -. "assistant" .-> llm
    worker -. "digests" .-> llm
    api -. "metrics, OTLP traces" .-> obs
    worker -. "metrics, OTLP traces" .-> obs
```

| Component | What it does | Code | Runs as | Talks to |
|---|---|---|---|---|
| **React app** | The whole interface; typed API client; no HTML built from strings | `web/src/` | Static files in the browser | `/api/v1/*` on the same origin, session cookie |
| **`fin`** | Command line for people and agents (JSON, CSV, table output) | `backend/cmd/fin`, `internal/fincli`, `internal/client/gen` | A local binary | The API with an access token (ADR 0036) |
| **web** | Serves the app with a strict CSP; proxies `/api/` under Compose | `deploy/docker/Dockerfile.web`, `nginx.conf` | Deployment ×2, unprivileged nginx | The api |
| **api** | Every HTTP endpoint (113 paths, 155 operations), sessions and CSRF, rate limits, the assistant's run loop | `backend/cmd/finuance`, `internal/app`, `internal/httpapi` | Deployment ×2–4 (HPA), `finuance serve` | PostgreSQL, the model server, the OTLP endpoint |
| **worker** | Background jobs: committing imports, reclassifying, legacy imports, digests, cleanups | `internal/jobs` | Deployment ×1, `finuance worker` | PostgreSQL, the model server |
| **migrate** | Schema migrations (goose, 24 files) | `internal/store/migrations` | Job before each rollout, owner role | PostgreSQL |
| **admin CLI** | `create-user`, `set-password`, `grant-admin`, `revoke-admin`, `llm-probe`; `seed-demo` | `internal/app/admin.go` | Run by hand in a pod or locally | PostgreSQL |
| **PostgreSQL** | All data and the job queue; forced row-level security; roles `finuance_owner` and `finuance_app` | `deploy/db/bootstrap.sql`, `deploy/k8s/db` | CloudNativePG cluster ×2, or one Compose container | The backup bucket |
| **Model server** | Answers the assistant; optional | `internal/llm` (client side), `deploy/assistant/` | Outside the cluster (ADR 0020) | — |
| **`llmmock`** | A scripted model server for tests, CI and the rehearsal; never in a release | `backend/testutil/llmmock`, `cmd/llmmock` | Compose profile, kind namespace | — |
| **Monitoring** | Metrics, SLO rules, alerts by e-mail, dashboards, traces | `deploy/k8s/observability`, `deploy/k8s/tempo` | kube-prometheus-stack, Tempo | Scrapes api, worker, database, Traefik |

Not there yet:

| Planned component | Phase |
|---|---|
| Signature check at admission (Sigstore policy-controller), images by digest | 23 |
| Loki and Grafana Alloy: collected logs, traces routed through Alloy | 24 |
| `finuance-sim`: the traffic simulator with its tuning page; a lab instance; `llmmock` as a lab image | 25 |
| A Vertex AI connection (token source in `internal/llm`) | 26 |
| A lab on GKE Autopilot built by Terraform (`deploy/terraform/`), Gateway API in the chart | 27 |
| The public demo host | 29 |

## 2. Inside the Go binary

```mermaid
flowchart TB
    cmd["cmd/finuance<br/>serve · worker · migrate · admin · seed-demo · healthcheck · version"] --> appp["app<br/>wiring, lifecycle"]
    appp --> httpapi["httpapi<br/>generated strict handlers, middleware,<br/>sessions, tokens, problems"]
    appp --> jobs["jobs<br/>River workers, periodic jobs, telemetry"]

    subgraph money["Money in, money read"]
        ledger["ledger<br/>accounts, imports, transactions,<br/>annotations, budgets, audit"]
        query["query<br/>filter language → SQL,<br/>windows, export, insight SQL"]
        insights["insights<br/>chart rules"]
        rules["rules<br/>categorisation, transfers"]
        legacy["legacyimport"]
        invest["investments"]
    end

    subgraph people["People"]
        auth["auth<br/>passwords, sessions, tokens, demo"]
        admin["admin · instance<br/>users, limits, settings, themes"]
        sharing["sharing · splits<br/>shared groups, balances"]
        friends["friends · notify"]
    end

    subgraph ai["Assistant"]
        assistant["assistant<br/>connections, tools, run loop,<br/>chats, digests, budget"]
        llm["llm<br/>OpenAI and Anthropic formats,<br/>dial guard, capabilities"]
        aiother["goals · aichanges · advice ·<br/>secret · images"]
    end

    subgraph base["Foundation"]
        store["store<br/>pool, migrations, sqlc,<br/>WithUser / WithUserReadOnly / WithoutUser"]
        pure["ingest · money<br/>pure parsing, no database"]
        platform["platform · config · observability"]
    end

    httpapi --> money
    httpapi --> people
    httpapi --> ai
    jobs --> money
    jobs --> ai
    money --> base
    people --> base
    ai --> base
    assistant --> llm
```

Rules that hold the shape (each is a test or a lint):

- Only `store` and `query` run SQL (`store/guard_test.go`). All user data goes through `store.WithUser`, which sets `app.user_id` for one transaction; the assistant's tools use `WithUserReadOnly`.
- `admin` can't import the financial packages; it reads user tables only for counts.
- `ingest` and `money` have no database and no clock.
- Generated code is never edited: `httpapi/gen`, `client/gen`, `store/db`, `web/src/api/schema.d.ts` (`make gen`).

### The request path

`Recover → AssignRequestID → RealIP → Logging → Metrics → Tracing → SecurityHeaders → MaxBytes → Route`, then the session and token layer (CSRF header and `Origin` check, access-token scopes, feature switches, the one-time-password gate), the handler, a domain package, `store.WithUser`, PostgreSQL.

### Background jobs (River, in PostgreSQL)

| Kind | Started by | Does |
|---|---|---|
| `commit_import` | `POST /imports/{id}/commit`, reprocess | Inserts new transactions, links rows, re-applies annotations, records statistics |
| `reclassify` | Most structural writes (rules, categories, manual transactions, duplicates, import delete) | Rules from scratch, then transfer detection |
| `rematerialize_fingerprint` | Duplicate decisions | Recomputes one fingerprint from its imports |
| `legacy_import` | `POST /legacy-imports` | Replays a backup of the single-file app |
| `assistant_digest`, `assistant_digest_scan` | A request, or the hourly scan | A read-only assistant run whose result is a notification |
| `cleanup_*` (sessions, demo users, audit log, notifications, assistant chats, friend codes) | Periodic | Retention |

A job is inserted in the same transaction as the change that needs it, and carries the request's trace context.

## 3. The web app

| Folder (`web/src/`) | What is in it |
|---|---|
| `api/` | The typed client (`openapi-fetch`), TanStack Query hooks, problem handling |
| `stores/` | Zustand stores and the preferences sync |
| `shell/`, `ui/`, `styles/` | Title bar, sidebar, keyboard manager; primitives (popover, modal, toast); the theme tokens |
| `auth/`, `settings/`, `admin/` | Sign-in, the user's settings, the administration panel (`/admin/:section`) |
| `import/`, `table/`, `groups/`, `duplicates/`, `rules/`, `cash/` | The import window and queue, the transaction table, labels, duplicate review, rules, hand-typed transactions |
| `insights/`, `charts/`, `overview/` | The insight pane and window, chart components, the monthly overview |
| `assistant/` | The chat panel, proposals, consent |
| `shared/` | Shared groups and the Shared tab |
| `legacy/` | Migrating a backup of the single-file app (decrypted in the browser) |

Tests: Vitest beside the code; Playwright in `web/e2e/` (23 spec files, with axe checks, screenshots and the parity run against the legacy app).

## 4. Data

One PostgreSQL database. Tables by area (migrations `00001`–`00024`):

| Area | Tables (examples) | Isolation |
|---|---|---|
| Identity | `users`, `sessions`, access tokens, friend codes | No RLS (resolved before the user is known); every query by `user_id` |
| Ledger | `accounts`, `imports`, `import_files`, `import_rows`, `transactions`, `tx_annotations`, `dedupe_overrides`, `categories`, `groups`, `budgets`, `import_profiles`, rules, goals, investments | Forced RLS, one tenant policy |
| Sharing | `shared_groups`, members, expenses, splits, settlements, links; `friendships`; `notifications` | Forced RLS by membership (`shared_member_role()`), or by either side |
| Assistant | chats, attachments, `ai_changes`, usage, digests | Forced RLS |
| Activity | `audit_log` | Forced RLS, append-only for the app role, 365 days |
| Instance | settings, themes, the advice library, assistant connections and tuning | No user data |
| Queue | River's tables | Job arguments carry ids, never content |

## 5. How a change reaches a cluster

```mermaid
flowchart LR
    pr["Pull request"] --> ci["CI<br/>guardrails · backend · web · images ·<br/>security · helm · oracle · e2e"]
    ci --> main["main"]
    main --> tag["Tag vX.Y.Z(-rc.N)"]
    tag --> rel["release<br/>multi-arch build, Trivy,<br/>SBOM, cosign keyless"]
    rel --> ghcr[("ghcr.io<br/>finuance-api, finuance-web")]
    rel --> bump["gitops-bump<br/>pull request with the new tag"]
    bump --> main
    main --> argo["Argo CD<br/>app of apps"]
    priv["Private overlay repository<br/>host names, addresses"] --> argo
    argo --> cluster["Cluster"]
    ghcr --> cluster
```

| Workflow | When | What |
|---|---|---|
| `ci.yml` | Pushes and pull requests | gitleaks and the private-data guards; Go lint, tests, fuzz smoke, `govulncheck`; integration tests with PostgreSQL; both images built and scanned; Trivy on the tree; `npm audit`; Helm lint, kubeconform, rules tests, sealing round trip; the legacy oracle is fresh; web lint, types, Vitest, build; Playwright on Compose |
| `nightly.yml` | Daily | Fuzzing, 60 s per target |
| `weekly.yml` | Mondays | ZAP baseline and API scan, k6 smoke against the SLO thresholds, the demo-mode E2E |
| `k8s-rehearsal.yml` | Mondays, deploy pushes | The whole deployment on kind: operators, secrets, database with backup, the app, NetworkPolicy probes, streaming through the ingress, pod kills under load, restore |
| `release.yml` | Version tags | Build, scan, SBOM, sign, the bump pull request |

Argo CD applications, in sync order: projects → admission policy → Tempo → CloudNativePG operator, Sealed Secrets → Barman Cloud plugin, kube-prometheus-stack → RustFS → the secrets → the database (synced by hand) → Finuance → its dashboards and rules.

Namespaces on the reference cluster: `finuance` (web, api, worker, database), `finuance-restore` (restore drills), `s3` (RustFS), `monitoring`, `cnpg-system`, `kube-system` (Traefik, the admission policy), `argocd`. Pod Security `restricted`, default-deny NetworkPolicies, images admitted by registry.

## 6. What the system tells you

| Signal | Emitted by | Where it ends up today | Gap (phase) |
|---|---|---|---|
| Access log, one JSON line per request | api (`middleware/logging.go`) | The container's standard output only | Not collected; no trace id on other lines (24) |
| Job and assistant log lines | worker, api | Standard output | No job id, kind or trace id (24) |
| Metrics | api and worker on the admin port | Prometheus; 6 dashboards; SLO and burn-rate alerts | Model latency missing; panics not counted (24) |
| Traces: request → job → SQL, assistant run and tools | api, worker (OTLP) | Tempo, 72 hours | No span for the model request; client trace context trusted (23, 24) |
| Activity log | Handlers, after a successful change | `audit_log`, shown in Settings → Activity | — |
| Health | `/healthz`, `/readyz` | Probes | Logged and traced like real requests (24) |
| Browser errors | — | Nowhere | Planned (24) |
| Synthetic traffic | — | Indistinguishable | A server-side marker (24) |

## 7. Guard scripts and tools

| Tool | Purpose |
|---|---|
| `tools/check-private.sh`, `check-env-details.sh`, gitleaks | No private data, no environment details, no secrets |
| `tools/check-toolchain.sh`, `check-no-float.sh`, `check-compose-prod.sh`, `check-db-bootstrap.sh`, `check-sealed.sh`, `check-port-forwards.sh`, `check-helm.sh`, `check-observability.sh` | One Go version; no floats for money; the copies of the bootstrap SQL agree; sealed secrets hold no plain values; ports, charts, dashboards and rules are consistent |
| `tools/legacy-oracle/` | Drives the single-file app in a headless browser to produce expectations and the synthetic fixtures |
| `tools/rehearsal.sh`, `homelab-drill.sh` | The deployment rehearsal on kind; backup and restore drills |
| `tools/observe.sh` | The administration tools' port-forwards in one command |
| `tools/zap-scan.sh`, `openvas-scan.sh` | Dynamic scans |
| `tools/seal-secrets.sh`, `bump-homelab-tag.sh`, `publish-public.sh` | Secrets, the deploy bump, the alternative way to publish |
| `loadtest/` (k6), `evals/assistant/` | Load tests against the SLO thresholds; assistant quality per model |
