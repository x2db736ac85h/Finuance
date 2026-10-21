# Threat model (v1)

Version 1 was written in Phase 5, when accounts, sessions and the first protected endpoints arrived. Phase 16 revises it for the Kubernetes deployment. The baseline controls come from PLAN §8.1. This document states which of them exist today, where they live, and which test proves them.

**Status legend:** ✅ implemented and tested · 🟡 partly in place · ⏳ planned (phase noted).

## 1. What we protect (assets)

| Asset | Where it lives | Why it matters |
|---|---|---|
| **A1. Financial data** | Postgres tables `transactions`, `import_rows`, `import_files` (raw CSV bytes), annotations, budgets, investments | Bank history reveals income, spending habits, locations, counterparties and account numbers |
| **A2. Credentials** | `users.password_hash` (argon2id PHC strings) | Users reuse passwords elsewhere |
| **A3. Sessions** | `sessions.token_hash` (SHA-256 only); the raw token exists only in the browser cookie | A stolen token is a full login |
| **A4. Account identity** | `users.email`, `display_name` | Personal data; reveals that someone uses the instance |
| **A5. Availability** | api, worker, Postgres on a small homelab node | One user's misuse shouldn't take the instance down |
| **A6. Secrets** | DB role passwords and the LLM API key, in env or Sealed Secrets | Direct access around every app control |
| **A7. Assistant conversations** (Phase 13) | `assistant_messages` | They contain financial questions and answers |

## 2. Who might attack (actors)

| Actor | Capability | Typical goal |
|---|---|---|
| **Internet attacker** | Reaches the public origin; scripts requests; runs credential stuffing | Take over accounts, read data, deny service |
| **Malicious website** | Runs JavaScript in a victim's browser while the victim is signed in | CSRF: act as the victim |
| **Other tenant** | A valid account on the same instance (household member, demo user) | Read or change someone else's data |
| **Malicious file** | A crafted CSV that a user uploads | Crash or exhaust the parser, or inject formulas into exports |
| **Prompt injector** (Phase 13) | Controls text inside transactions (payee, description) | Steer the assistant into leaking or acting |
| **Operator mistake** | Shell access, logs, backups | Accidental exposure: logs with secrets, a debug query across tenants |
| **Supply chain** | A compromised dependency or base image | Code execution in the api or worker |

## 3. Trust boundaries

```mermaid
flowchart LR
    subgraph untrusted["Untrusted"]
        browser["Browser + other sites"]
        files["Uploaded CSV files"]
    end
    subgraph edge["Edge (TLS, ingress / nginx)"]
        ingress["same-origin routing, HSTS, CSP"]
    end
    subgraph app["Application (trusted code)"]
        api["api: CSRF → session → validation → handler"]
        worker["worker: River jobs"]
    end
    subgraph data["Data (Postgres, role finuance_app)"]
        rls["RLS: app.user_id per transaction"]
    end
    llm["LLM server (Phase 13, instance config only)"]

    browser -- "B1: HTTP + cookie" --> ingress --> api
    files -- "B2: upload bytes" --> api
    api -- "B3: SQL in WithUser" --> rls
    worker -- "B3" --> rls
    api -. "B4: prompts" .-> llm
```

- **B1, browser → api.** Everything is untrusted input. Requests pass CSRF checks, session resolution, OpenAPI request validation (shape) and domain validation (meaning).
- **B2, uploaded bytes → parser.** The file is hostile until parsed. Size, row, column and time limits apply: the parser limits from Phase 3, the upload limits from Phase 6.
- **B3, application → database.** The app role is subject to row-level security. Even application bugs stay within one tenant.
- **B4, application → LLM** (Phase 13). The endpoint comes from instance config, never from user input. Data is framed as data.

## 4. STRIDE

| # | Threat (STRIDE) | Boundary | Control | Status | Where / proof |
|---|---|---|---|---|---|
| S1 | **Spoofing:** password guessing, credential stuffing | B1 | argon2id m=19 MiB, t=2, p=1 (ADR 0010). Login and signup limited to 10/min per (IP, email) and 30/min per IP, checked before hashing (ADR 0012). The same error for an unknown email and a wrong password. | ✅ | `auth/password.go`, `httpapi/handlers_auth.go`; `TestLoginRateLimit`, `TestLoginRotatesAndFailsUniformly` |
| S2 | **Spoofing:** account enumeration through login timing | B1 | An unknown email still runs a full argon2id verify against a dummy hash. Signup does reveal a taken email (409); that is accepted and rate-limited (ADR 0010). | ✅ | `auth/service.go` `Login`; `TestUnknownEmailRunsArgon2` |
| S3 | **Spoofing:** session theft or fixation | B1 | 32 random bytes; only SHA-256 stored. Cookie `HttpOnly; SameSite=Lax; Secure` (not Secure only in dev/test). Idle (7 d) and absolute (30 d) expiry. Rotation on login and password change. Revocation from the sessions list. (ADR 0011) | ✅ | `auth/sessions.go`, `httpapi/session.go`; `TestSessionExpiry`, `TestAuthFlow`, `TestCookieAttributes` |
| S4 | **Spoofing:** CSRF from a malicious site | B1 | Unsafe methods need `X-Finuance-Client: web`, and an `Origin` (if sent) must equal `FINUANCE_PUBLIC_ORIGIN`. This applies to login and signup too. SameSite=Lax is the first layer (ADR 0011). | ✅ | `httpapi/middleware/csrf.go`; `TestCSRF` |
| S5 | **Spoofing:** forgotten password with no email channel | Operator | `finuance admin set-password -password-stdin` resets the password and revokes all sessions. The password never goes in argv. | ✅ | `app/admin.go`; `TestAdminCommands` |
| T1 | **Tampering:** one tenant changing another's data | B3 | Explicit `user_id` predicates, plus RLS enabled and **forced** with `WITH CHECK` (ADR 0007). FK targets are checked for ownership, because foreign keys bypass RLS. Another user's account or import id is a `404`, never a `403`. | ✅ | `00003_rls.sql`, `ledger/owned.go`; the RLS suite (`store/rls_test.go`), `TestAnnotateRules`, `TestImportErrorsHTTP` |
| T2 | **Tampering:** SQL injection | B1→B3 | sqlc-generated parameterised queries. The only hand-written statement (`InsertImportRows`) binds arrays. The dynamic filter builder (Phase 7) will whitelist columns and be fuzzed. | 🟡 | `store/queries/*.sql`; ⏳ Phase 7 for the query builder |
| T3 | **Tampering:** lost updates on preferences | B1 | Optimistic versions: a stale `PUT /preferences` gets `409 stale_version` with the current document | ✅ | `TestPreferences`, `TestPreferencesHTTP` |
| T4 | **Tampering:** CSV/formula injection in exports | B2 | Text cells starting with `= + - @ \t \r` that aren't numbers get a `'` prefix | ⏳ | Phase 7 (export) |
| R1 | **Repudiation:** who did what | — | Access log with request id, route, status and hashed user id. An audit log table arrives in Phase 16. | 🟡 | `middleware/logging.go`; ⏳ Phase 16 `audit_log` |
| I1 | **Information disclosure:** cross-tenant reads | B3 | As T1. Without `app.user_id`, every user table looks empty, even to the owner role. | ✅ | `TestRLSReadsOnlyOwnRows`, `TestRLSWithoutUserSeesNothing` |
| I2 | **Information disclosure:** secrets in logs | Operator | The access log never contains bodies, cookies, query strings, emails or user ids (hash only). Internal error text is hidden in prod problems. | ✅ | `TestLogsNeverContainSecrets`, `TestAccessLog`, `ProblemFor` |
| I3 | **Information disclosure:** stolen database dump | B3 | Password hashes are argon2id. Session tokens are stored hashed. Account numbers live only in `raw`, the counterparty fields and the stored original files (`import_files`, kept so an import can be reprocessed); `number_hint` is the last 4 digits. Volume encryption and encrypted backups are documented in the RUNBOOK. | 🟡 | Hashes ✅; ⏳ Phase 16 RUNBOOK |
| I4 | **Information disclosure:** XSS reading data in the browser | B1 | React escaping, no `dangerouslySetInnerHTML`, strict CSP from nginx or the ingress | ⏳ | Phases 8 and 16 |
| I5 | **Information disclosure:** prompt injection makes the assistant leak | B4 | Read-only tools in `WithUserReadOnly` (RLS-scoped, `READ ONLY` transaction); no network tools; prompts, outputs and tool results are never logged | 🟡 | `WithUserReadOnly` ✅ (`TestWithUserReadOnlyRejectsWrites`); ⏳ Phase 13 |
| D1 | **Denial of service:** login floods exhausting CPU and memory | B1 | Rate limits before hashing. At most 4 concurrent argon2 hashes (≈ 80 MB). Limiter memory capped at 100k keys per policy with eviction. | ✅ | `TestLimiterEviction`, ADR 0010/0012 |
| D2 | **Denial of service:** hostile CSV (huge, wide, slow) | B2 | 200k rows, 200 columns, context cancellation every 10k rows (Phase 3). Uploads: `FINUANCE_MAX_UPLOAD_BYTES` (20 MiB) checked exactly on the file, with the request body capped at that plus 64 KiB for multipart framing (every other route stays at 1 MiB); one file per request; a NUL byte in the first 8 KiB is `415`; 30 uploads/min per user. The commit runs as a River job, never in the request. | ✅ | `ingest/parse.go`, `httpapi/handlers_imports.go`; `TestParseLimitsAndCancellation`, `TestImportErrorsHTTP`, `TestUploadRateLimit` |
| D3 | **Denial of service:** oversized JSON bodies | B1 | 1 MiB body limit; the preferences document is capped at 256 KiB | ✅ | `TestBodyLimit`, `TestPreferences` |
| D4 | **Denial of service:** session table growth | B3 | Hourly `CleanupExpiredSessions` River job | ✅ | `TestWorkerRunsJobs` |
| E1 | **Elevation of privilege:** the app role escaping RLS | B3 | `finuance_app` owns no tables and can't run DDL. RLS is forced. Migrations run as `finuance_owner`, which is also bound by FORCE. | ✅ | `TestRiverTablesAndRoles`, `TestEveryUserTableHasForcedRLS` |
| E2 | **Elevation of privilege:** forgetting `WithUser` in new code | B3 | A guard test fails on direct `Query`/`Exec`/`Begin` or `db.New` outside `internal/store` | ✅ | `TestNoDatabaseAccessOutsideStore` |
| E3 | **Elevation of privilege:** supply chain | — | Pinned dependencies and checksummed tool binaries; govulncheck, Trivy and gitleaks in CI; Renovate. SBOM and signing arrive in Phase 16. | 🟡 | CI ✅; ⏳ Phase 16 |

## 5. Accepted risks and assumptions

- **Rate limits are per replica** (ADR 0012). With N api replicas an attacker gets N× the budget. Accepted for N ≤ 2. Revisit if the deployment scales out.
- **Signup reveals whether an email is registered.** Accepted for a self-hosted instance. `FINUANCE_ALLOW_SIGNUP=false` closes it.
- **There is no MFA and no email verification** in v1. Personal instances run with signup disabled and admin-created accounts. OIDC is in the backlog.
- **TLS terminates at the ingress or nginx.** The api trusts `X-Forwarded-For` only from `FINUANCE_TRUSTED_PROXIES`.
- **A compromised host or database superuser is out of scope.** It can read everything. Disk encryption and backup encryption reduce what a stolen disk or backup reveals.

## 6. Review checklist for new endpoints

1. Is the route public? If so, add it to `publicRoutes` with a reason. Otherwise it is protected automatically.
2. Does it write? CSRF applies automatically to every non-GET method.
3. Does it read or write user data? It must go through `ledger`/`auth` methods that use `WithUser`. Add an "other user gets 404" test.
4. Does it store a reference to another user-owned row? Check ownership in the same transaction (`ledger/owned.go`).
5. Is it expensive (hashing, parsing, model calls)? Give it a rate-limit policy (ADR 0012).
6. Could it log user data? It must not. Extend the redaction test if it adds a new field.
