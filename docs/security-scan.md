# Security scan

Every finding of every scan, and what happened to it: fixed, or not exploitable and why. HIGH and
CRITICAL findings fail the build. A finding is fixed, not ignored; `.trivyignore` holds only what
cannot be fixed yet, each entry with a reason and an expiry (empty since 2026-11-19, below). The scans and
when they run are listed in [THREAT-MODEL.md](THREAT-MODEL.md) §7. Phase 18 adds the OpenVAS /
Nessus scan of the deployed instance, and the Phase 23 gate repeats everything right before the
demo goes public.

**Phase 17 run: 2026-10-26/27**, in the cloud sandbox, on the push 3 branch. The images were built
from the checkout, and the stack was `compose.ci.yaml` (sign-ups on, mock model). Trivy 0.76.0 ran
with a vulnerability database of 2026-10-26.

**CI finding: 2026-11-06**, the backend job's govulncheck on PR #15 (no Go change in it): GO-2026-6865 in
`github.com/klauspost/compress` 1.20.1 (indirect, through `client_golang`'s `promhttp`), reachable from
the assistant eval harness (`eval.Run` → `multierr` → zstd decoding): **fixed** by 1.20.2. Afterwards
govulncheck 0 reachable, 1 in a required module (GO-2026-5932, no fix, not reachable). The released
`v0.3.0-rc.2` images carry 1.20.1; `v0.3.0` will carry the fix.

**Release scan before `v0.4.1-rc.1`: 2026-11-08**, on the reference machine, on `main` at `912ab56`;
both images built without the Docker cache (as the release builds), Trivy 0.76.0 with that day's
database, govulncheck, `npm audit`. The charts' templates are unchanged since `v0.4.0` (the
follow-ups changed the app, not the deployment). Nothing new: web image 0 findings, api image only
GO-2026-5932 (`x/crypto/openpgp`, not imported; govulncheck 0 reachable; the fix is still an
untagged commit), repository `minimatch` (MEDIUM, development tools only: ESLint 10.14 and
typescript-eslint 8.72.1 still require `minimatch` 10), `npm audit --omit=dev` 0, no secrets, the
misconfigurations documented below.

**Release scan before `v0.4.0-rc.1`: 2026-11-07**, on the reference machine, on `main` at `21d991c`;
images built locally from it, Trivy 0.76.0 with that day's database, govulncheck, `npm audit`, and
Trivy over the charts' renders at every severity (Phase 20 changed the templates: the worker's
connections and egress, the instance key). Web image **0 findings when built fresh**: a build from
the local Docker cache still had zlib 1.3.2-r1 (CVE-2026-4738, MEDIUM, fixed in 1.4-r0), because the
Dockerfile's `apk upgrade` only runs when its layer is built again; the release builds without a
cache, so its image has 1.4-r0. api image and repository: GO-2026-5932 (`x/crypto/openpgp`, not
imported; govulncheck 0 reachable) now lists a fix, but only as an untagged commit
(`v0.58.1-0.20261009181718-2f1f834330b2`), so `x/crypto` stays on the tagged v0.58.0 until a release
carries it. `npm audit --omit=dev` 0; `minimatch` (MEDIUM, development tools only) still open; no
secrets; misconfigurations in the repository and in the renders only the ones documented below.

**Release scan before `v0.3.0-rc.1`: 2026-11-04**, on the reference machine, on `main` at `9ae8f60`;
images built locally from it, Trivy 0.76.0 with that day's database, govulncheck, `npm audit`.
Unchanged since v0.2.0 (no dependency changes in Phase 19): web image 0 findings; api image and
repository GO-2026-5932 (no fix, not reachable); govulncheck 0 reachable; `npm audit --omit=dev` 0;
`minimatch` (MEDIUM, development tools only) still open; no secrets; the misconfiguration findings
as documented below.

**Release scan before `v0.2.0-rc.2`: 2026-11-02**, on the reference machine, on the Phase 18 branch;
images built locally from it, Trivy 0.76.0 with that day's database, govulncheck, `npm audit`. Web
image 0 findings; api image and repository GO-2026-5932 as before (no fix, not reachable);
govulncheck 0 reachable; `npm audit --omit=dev` 0. **New:** `minimatch` (MEDIUM, development tools
only, below) and KSV-0037 for the k3s Traefik config (deliberate, below).

**Release scan before `v0.2.0-rc.1`: 2026-10-30**, on the reference machine, on `main` plus the release
changes (branch `chore/release-0.2.0`), images built locally from it; Trivy 0.76.0 with that day's
database, govulncheck 1.9.0 (`vuln.go.dev` reachable), `npm audit`. **New since Phase 17:**
GO-2026-6866 and GO-2026-6860 in `golang.org/x/net` 0.61.0 (Trivy: CVE-2026-78661, CVE-2026-97045),
not reachable: **fixed** by 0.62.0. zlib and musl in the web image: **fixed** by upgrading them in the
Dockerfile (still no rebuilt base). After the changes: web image 0 findings; api image and
repository only GO-2026-5932 (no fix exists); `npm audit` 0; govulncheck 0 reachable, 1 in a required
module (GO-2026-5932). The repository's misconfiguration findings are unchanged (below).

**CI finding: 2026-11-18**, the security job on a documentation pull request: CVE-2026-112366 /
GHSA-fc5q-39xc-vjpf in `picomatch` 4.0.7 (HIGH, a denial of service by a crafted glob pattern),
from `web/package-lock.json` through `vite`, `vitest` and `tinyglobby`. **Accepted until
2026-12-02, the first entry `.trivyignore` has had:** no fixed version exists (4.0.7 is the newest
release), the package is used by development tools only with patterns from the repository's own
configuration, and nothing of it is in the web image (`npm audit --omit=dev`: 0). `npm audit` has
no way to accept one advisory, so `tools/npm-audit.mjs` now applies the same file, with the same
expiry, to the audit of every dependency. The rule from here on: a finding is fixed, not ignored;
the entry is removed as soon as a fixed version exists (PLAN task 22.15). **Withdrawn** by the
advisory itself on 2026-11-18 (GitHub shows `withdrawn_at`); neither Trivy's database nor `npm audit`
reports it any more, and the entry was removed on 2026-11-19, which left `.trivyignore` empty.

**Scan at the start of Phase 22: 2026-11-19** (PLAN task 22.0), on `main` at `76b7d7a`; both images
built from it (`make images`), Trivy 0.76.2 with that day's database at every severity, govulncheck,
`npm audit` in `web/` and `tools/legacy-oracle/`, gitleaks 8.30.1 over the history (297 commits). The
audit's table of 2026-11-16, row by row, with the newest release on the day:

| Component @ pin on `main` | Finding | Newest on 2026-11-19 | Decision |
|---|---|---|---|
| Go 1.27.4 | GO-2026-6859 and GO-2026-6848 … 6858 (1.27.3) | 1.27.4 | **Fixed** 2026-11-17 (pull request 34); govulncheck 0 reachable |
| `golang.org/x/crypto` 0.58.0 | GO-2026-5932 (`openpgp`, not imported) | 0.59.0 | **Fixed** 2026-11-19: updated; govulncheck reports nothing, not even in required modules |
| PostgreSQL 18.6 | the 18.7 fixes (policies and `ON CONFLICT`, `DROP OWNED BY` and policies, leaky quals, index corruption) | 18.7 (`postgres:18.7-bookworm`, `ghcr.io/cloudnative-pg/postgresql:18.7-standard-trixie`) | **Fixed** in the tree 2026-11-19 (Compose and the tests by digest; the Go suite with RLS and the de-duplication property test passes on it); the homelab after its manual sync and the reindex (RUNBOOK "Upgrading PostgreSQL (minor release)") |
| Grafana 13.3.0 (kube-prometheus-stack 92.16.1) | six CVEs fixed in 13.3.1 | chart 93.4.1 (Grafana 13.3.1, Prometheus Operator 0.95.2); the last 92.x still ships 13.3.0 | **Fixed** 2026-11-19: chart 93.4.1. Rendered with the homelab values against 92.16.1: the same resources; besides the versions, the operator's role loses `delete` on pods, node-exporter runs with a stricter security context, the CRDs change (server-side apply); every image is still under a registry prefix the admission policy allows. The rule tests run on promtool 3.16.0, the Prometheus it deploys |
| Tempo 2.9.0 | GHSA-p4r4-xvrq-gvmc, GHSA-ffqx-q65f-36jf | 2.9.5 (2.10.10, 3.1.1) | **Fixed** 2026-11-19: 2.9.5 by digest, a patch release (dependency fixes; it drops the OpenCensus receiver, which the config does not use) |
| Helm CLI 3.18.4 (CI, rehearsal) | three moderate advisories | 3.22.1 (4.3.2) | **Fixed** 2026-11-19: 3.22.1, the v3 line, in CI, the rehearsal and the RUNBOOK; `tools/check-helm.sh` matches its new wording of schema errors |
| Node build image 22.22.0 | five security releases since | 22.23.5 | **Fixed** 2026-11-19: `node:22.23.5-alpine` by digest; the web image builds from source on it |
| `minimatch` 10.2.6 (development tools) | GHSA-g3hv-75wg-m249 (MEDIUM) | ESLint 10.15.0, typescript-eslint 8.73.0 | **Fixed** 2026-11-19: both updated, `minimatch` 11.0.9, `npm audit` 0 at every severity; `minimatch` 5.1.9 under openapi-typescript is outside the advisory's range (≥ 7.0.0) |
| `picomatch` 4.0.7 (development tools) | GHSA-fc5q-39xc-vjpf | 4.0.7 | **Gone:** the advisory was withdrawn (above) |
| nginx 1.30.6 (web image base) | CVE-2026-113425, CVE-2026-121810, CVE-2026-123007, CVE-2026-123633, CVE-2026-123634 (HIGH; nginx 1.30.7 of 2026-11-18), found by CI's image scan on this phase's first pull request at 03:03 UTC, after the scan above | 1.30.7 (`ghcr.io/nginx/nginx-unprivileged:1.30.7-alpine`; Docker Hub did not have it yet) | **Fixed** 2026-11-19: the base from NGINX's GHCR by digest (the same digest as on their ECR); the web image scans with 0 findings at every severity |
| OpenSSL 3.5.9-r0 (`libssl3`, `libcrypto3`, the nginx 1.30.7 base) | CVE-2026-107496, CVE-2026-88822, CVE-2026-88823, CVE-2026-95691, CVE-2026-95692 (MEDIUM), found by the first SARIF upload to code scanning (pull request 41) | 3.5.9-r1 | **Fixed** 2026-11-19: the Dockerfile upgrades OpenSSL from the Alpine mirror with zlib and musl; the web image scans with 0 findings at every severity |
| Trivy 0.76.0 (CI, release) | none | 0.76.2 | **Updated** 2026-11-19 to 0.76.2 (CI, release); a binary checked by checksum instead of the image in task 22.5 |

Nothing new: web image 0 findings; api image only GO-2026-5932; repository only `minimatch` and
GO-2026-5932, and the misconfigurations documented below (the same files and checks); `npm audit
--omit=dev` 0; `tools/legacy-oracle` 0; govulncheck 0 reachable; gitleaks no leaks. The cluster's
CloudNativePG (1.30.1, its Barman Cloud plugin 0.15.1), Sealed Secrets (0.40.0), cert-manager,
Traefik and Argo CD have no advisory on OSV for the versions running; their updates are routine and
belong with the machine's maintenance.

**trufflehog over the whole history: 2026-11-19** (PLAN task 22.6, once; gitleaks stays the gate
on every push): trufflehog 3.99.9 (checked against its release's checksums), `git file://.`,
8,535 chunks (17.6 MB): **0 verified and 0 unverified secrets**.

**OpenSSF Scorecard: 2026-11-19, 6.8 of 10** (PLAN task 22.6; `scorecard.yml`, published), on
`main` at `9a5134c`. 10 of 10: pinned dependencies, token permissions, dangerous workflows,
vulnerabilities, security policy, dependency updates, fuzzing, CI tests, packaging, licence, binary
artifacts. The rest, with what was decided:

| Check | Score | Decision |
|---|---|---|
| Signed-Releases | 0 | **Fixed for `v0.4.2`:** each release attaches the provenance bundles of both images (`*.intoto.jsonl`) and a cosign signature bundle of the `fin` checksum file; the images were signed already, in the registry |
| SAST | 7 | Rises by itself: CodeQL runs on every pull request and push to `main` since this day |
| Branch-Protection | 3 | Accepted: one maintainer, who can't approve their own pull requests; the ruleset requires a pull request and every CI job |
| Code-Review | 0 | Accepted for the same reason |
| Contributors | 3 | Accepted: one maintainer |
| Maintained | 0 | Rises by itself: the repository is younger than 90 days |
| CII-Best-Practices | 0 | Open: the OpenSSF best-practices badge is a registration, the maintainer's choice |

## Summary

| Scan | Result | Open |
|---|---|---|
| Trivy, api image | 2 findings; 1 fixed by a base image bump | 1 (UNKNOWN, not reachable) |
| Trivy, web image | 3 findings (MEDIUM / UNKNOWN), no fixed base yet; fixed 2026-10-30 by upgrading zlib and musl in the Dockerfile | 0 |
| Trivy, repository (vuln, secret, misconfig) | the api image's Go finding again; LOW/MEDIUM in the rehearsal stand-ins, RustFS, Tempo and the k3s Traefik config; `minimatch` (MEDIUM, development tools, 2026-11-02) | deliberate; `minimatch` **fixed** 2026-11-19 (ESLint 10.15.0, typescript-eslint 8.73.0) |
| Trivy config, rendered charts (all severities) | 2 LOW fixed (web uid); 4 kinds LOW/MEDIUM left | all deliberate |
| `npm audit` (production and all) | 0 | – |
| govulncheck | 0 reachable (CI; 2026-10-30 locally: 2 new `x/net` module findings, fixed by 0.62.0) | – |
| gitleaks | 0 (CI) | – |
| ZAP baseline | 0 WARN, 0 FAIL, 61 rules pass; the first Actions run added 90004 (fixed), 10106 and 10049 | 3 ignored with reasons |
| ZAP API scan (active, signed in) | 0 WARN, 0 FAIL, 112 rules pass; ~100,000 requests, no 5xx | 2 INFO, 1 ignored |
| OpenVAS (Greenbone CE, Full and fast) against the instance | 0 High, 0 Medium, 0 Low; 47 Log. From the Log entries: CBC-SHA suites under TLS 1.2 and TLS on port 80 (both fixed) | HSTS without `includeSubDomains`/`preload`, `Server: nginx`, Ollama on the tailnet (deliberate) |
| Port scan from the LAN (all TCP ports) | 0 open | – |

## Trivy: images

| Image | Finding | Severity | Decision |
|---|---|---|---|
| api | DSA-6572-1 `ca-certificates` 20250419 (fixed in 20250419+deb13u1) | UNKNOWN | **Fixed:** the distroless `static-debian13:nonroot` base moved to the rebuilt digest `sha256:18f4cc26…`, which scans clean. |
| api | GO-2026-5932 `golang.org/x/crypto` v0.58.0: the `openpgp` package is unmaintained and unsafe | UNKNOWN | **Not reachable:** a module-level match. The binary uses `argon2` and, through the test tooling, `ssh`; `go list -deps ./...` contains no `openpgp`. govulncheck, which checks the call graph, reports nothing. No fixed version exists; it goes away with the next x/crypto that drops or splits the package. |
| web | CVE-2026-85091 `zlib` 1.3.2-r0 (fixed in 1.3.2-r1) | MEDIUM | **Fixed 2026-10-30:** the Dockerfile upgrades zlib from the Alpine mirror (1.3.2-r1), since the base still isn't rebuilt. Before that: **Not reachable:** a heap overflow in `gzprintf`/`gzvprintf` after a stalled non-blocking `gzwrite`, i.e. the `gzFile` API. nginx's gzip filter uses the streaming `deflate` API. The nginx-unprivileged 1.30.6 base hasn't been rebuilt yet (stable-alpine still has the same digest), and the sandbox can't reach the Alpine mirror for an `apk upgrade`; the base bump picks it up (Renovate). |
| api | GO-2026-6866 / GO-2026-6860 `golang.org/x/net` 0.61.0 (Trivy: CVE-2026-78661, CVE-2026-97045), found 2026-10-30 | UNKNOWN | **Fixed:** `golang.org/x/net` 0.62.0 (govulncheck: not called by Finuance's code before either). |
| web | CVE-2026-107891 `musl`, `musl-utils` 1.2.6-r2 (fixed in 1.2.6-r3) | UNKNOWN | **Fixed 2026-10-30:** upgraded in the Dockerfile with zlib (1.2.6-r3). Before that: **Open, no details published** (no description, no score). The same base bump fixes it; it becomes a blocker if it is rated HIGH before then. |

## Trivy: repository

`trivy fs --scanners vuln,secret,misconfig --include-dev-deps` (the CI `security` job). It skips
`deploy/helm/finuance` and `deploy/k8s/db`, which the `helm` job renders and scans instead.

| Finding | Severity | Decision |
|---|---|---|
| GO-2026-5932 in `backend/go.mod` | UNKNOWN | As in the api image. |
| `deploy/k8s/rehearsal/stand-ins.yaml`: KSV-0011, -0015, -0016, -0018 (no CPU/memory requests or limits) | LOW | **Deliberate:** the bucket and mock model of the CI rehearsal on kind, never deployed elsewhere; they are hardened like the app (non-root, read-only, no capabilities; Phase 17 push 1). |
| same file: KSV-0125 (image not from an allow-listed registry) | MEDIUM | **Deliberate:** Docker Hub images pinned by digest, in a throwaway kind cluster. |
| `deploy/k8s/rustfs/rustfs.yaml` (Phase 18): KSV-0011 (CPU not limited) for RustFS and the bucket job | LOW | **Deliberate:** as for the app's pods: CPU requests set, memory limited. Otherwise hardened like the app (non-root, read-only root, no capabilities, RuntimeDefault seccomp, default-deny NetworkPolicies). |
| same file: KSV-0125 (image not from an allow-listed registry) | MEDIUM | **Deliberate:** Docker Hub images (`rustfs/rustfs`, `amazon/aws-cli`) pinned by digest; on the homelab the admission policy (`deploy/k8s/admission`, 18.8) admits only these two, and only by digest, in `s3`. |
| `deploy/k8s/tempo/tempo.yaml` (Phase 18.6): KSV-0011 (CPU not limited) | LOW | **Deliberate:** as for the app's pods and RustFS: CPU request set, memory limited; non-root, read-only root, no capabilities, RuntimeDefault seccomp, default-deny NetworkPolicy (OTLP only from the app, queries only from Grafana). |
| same file: KSV-0125 (image not from an allow-listed registry) | MEDIUM | **Deliberate:** `grafana/tempo` pinned by digest; the admission policy (18.8) requires its digest in `monitoring`. |
| `deploy/environments/homelab/host/traefik-config.yaml` (Phase 18): KSV-0037 (resource in `kube-system`) | MEDIUM | **Deliberate:** a k3s `HelmChartConfig` for its bundled Traefik; k3s reads it only in `kube-system`. It holds chart values, no workload. |
| `web/package-lock.json`: CVE-2026-118764 / GHSA-g3hv-75wg-m249 in `minimatch` 10.2.6 and 5.1.9 (new 2026-11-02) | MEDIUM | **Fixed 2026-11-19** (Phase 22): ESLint 10.15.0 and typescript-eslint 8.73.0 require `minimatch` 11, now 11.0.9 throughout; `npm audit` reports 0 at every severity. The 5.1.9 copy under openapi-typescript's `@redocly/openapi-core` stays: the advisory covers ≥ 7.0.0 only. Before that, **not reachable:** development tools only (ESLint, and openapi-typescript's `@redocly/openapi-core`), nothing in the built app (`npm audit --omit=dev`: 0); the denial of service needs crafted glob patterns, and only our own lint and codegen configuration supplies them. The fix (11.0.1) is a major version that ESLint 10 and openapi-typescript 7 don't accept yet; re-check when they release, before the gate's "nothing above LOW". Re-checked 2026-11-03 (Phase 19 PR 1): ESLint 10.14.0 still asks for `minimatch ^10.2.6`, openapi-typescript 7.13.0 is the latest; no upgrade possible yet. |

A local checkout may hold `backend/vendor/` (gitignored, for offline image builds). Trivy finds
dozens of npm and Python advisories in it: testcontainers' documentation `Pipfile.lock` and
`golang.org/x/telemetry`'s web tooling `package-lock.json`. Neither is built, shipped or in git.
CI has no vendor directory, and local scans skip it.

## Trivy config: rendered charts, all severities

Rendered with every values file by `tools/check-helm.sh` and `tools/check-observability.sh`
(`RENDER_DIR`), then `trivy config` without a severity filter.

| Check | Severity | Where | Decision |
|---|---|---|---|
| KSV-0020 / KSV-0021: runs with UID / GID ≤ 10000 | LOW | web | **Fixed:** the web pod runs nginx as 10101:10101 (the image's own user, 101, stays for Compose). Checked: nginx starts with a read-only root filesystem and serves the app and its assets. |
| KSV-0011: CPU not limited | LOW | api, worker, web, migrations | **Deliberate:** CPU limits throttle the Go runtime and the imports; the pods have CPU requests, and the HPA scales the api. Memory is limited. |
| KSV-0110: workloads in the default namespace | LOW | all | **False positive:** the templates carry no `namespace:` and Helm/Argo CD install them into `finuance`. |
| KSV-0125: images from an untrusted registry | MEDIUM | all | **Handled in the cluster:** the images are `ghcr.io/x2db736ac85h/*`, signed with cosign. Since 18.8 a `ValidatingAdmissionPolicy` (`deploy/k8s/admission`) admits in `finuance` only that registry and CloudNativePG's; signature verification and deployment by digest follow in Phase 23, ADR 0024 (Trivy reports the rule from the manifest alone, so it stays listed). |
| KSV-01010: ConfigMap with sensitive content | MEDIUM | `finuance-env` | **False positive:** the flagged key is `FINUANCE_MAX_UPLOAD_BYTES`; the ConfigMap holds no secret (passwords come from the CloudNativePG secrets as `PGPASSWORD`). |

The observability and database charts have no findings at any severity.

## OWASP ZAP

`make zap` / `tools/zap-scan.sh` with `zaproxy/zap-stable:2.17.0` (by digest), weekly in
`weekly.yml`:
- **baseline** (passive) against the web app;
- **API scan** (active) from `api/openapi.yaml`, signed in as a fresh user with demo data through
  `.zap/hooks.py`. Four operations that would end its own session are left out (sign-out,
  delete account, change password, revoke session). Every other operation is attacked, deletes
  included: SQL, code, command, template and path injection, XSS, SSRF and the rest of ZAP's
  active rules.

The first runs found two problems, both fixed:
- **Security headers twice.** nginx added its security headers to API responses that already
  carried the api's own, so `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` and
  `Cross-Origin-Opener-Policy` arrived twice, and the two `Permissions-Policy` values differed.
  nginx now drops the api's copies (`proxy_hide_header`). `e2e/headers.spec.ts` checks every
  header arrives once on a page, an API response and an API error.
- **Redirects with the container's port.** nginx's own redirect from `/api` to `/api/` was
  absolute and named the internal port (`http://localhost:8080/api/`). Redirects are now relative
  (`absolute_redirect off`); the same spec checks it.

The first weekly run in Actions (2026-10-27) reported three more rules. ZAP updates its scan
rules at start-up, which works in Actions but not in the sandbox, so the local runs didn't have
them:
- **90004 Insufficient Site Isolation Against Spectre.** Fixed: nginx sends
  `Cross-Origin-Resource-Policy: same-origin` and `Cross-Origin-Embedder-Policy: require-corp`
  on every response, and the api sends the same `Cross-Origin-Resource-Policy` (nginx drops its
  copy). Everything the app loads is same-origin, so `require-corp` breaks nothing (all 57 E2E
  specs pass with it); `headers.spec.ts` checks both arrive once.
- **10106 HTTP Only Site** and **10049 Storable and Cacheable Content**: ignored, see below.

The weekly runs of 2026-11-02 and 2026-11-09 failed on *A server error response code was returned*
(41 and 121 instances, spread over most operations). Three causes, all fixed:
- **nginx ran out of local ports.** It opened a new connection to the api for every request, and
  each closed connection held a local port for a minute (TIME_WAIT), about 28,000 in all. On the
  Actions runner nginx answered thousands of the scan's requests `502` without the api ever seeing
  them (hundreds per operation in nginx's log, no error in the api's). The reference machine's
  run of the same scan (131,382 requests in 13 minutes) stayed below the limit, so the cause was
  shown on a web container limited to 500 local ports: 261,915 of 262,414 requests got `502`, with
  `connect() failed (99: Address not available)` in nginx's log. nginx now keeps a pool of
  connections to the api alive (`upstream … keepalive`, closed after 60 s, before the api's 120 s
  idle timeout); the same load then got no `502` in 169,763 requests. Only Compose deployments were
  affected: in Kubernetes, Traefik sends `/api/` to the api directly.
- **A NUL character in the sign-in e-mail** reached PostgreSQL, which refuses it, and the api
  answered `500`. Every NUL that reaches PostgreSQL is now `422 validation_failed`, whatever the
  route (the ledger already mapped its own; sign-in and sign-up didn't).
- **`POST /assistant/digest` answered `503 assistant_unavailable`** because the weekly stack had no
  model server. That answer is right for an instance without one; the weekly scan now runs with the
  mock model server, as the E2E stack does, so the assistant's operations are scanned working.

The step that names server errors now reports nginx's own error lines first: the first runs named
the failing routes but not the reason. With the three fixes the weekly workflow passed on their
branch (2026-11-09): no server error in either scan, no error line from nginx or the api, and the
k6 smoke test and the demo E2E green.

Remaining alerts, each with its rule in [`.zap/rules.tsv`](../.zap/rules.tsv):

| Rule | Alert | Decision |
|---|---|---|
| 10109 | Modern Web Application | IGNORE: informational; ZAP saw a single-page app and suggests its AJAX spider. The API scan covers every operation instead. |
| 100001 | Unexpected Content-Type | IGNORE: the API scan expects JSON everywhere. `/api/v1/me/export` is a zip by design, the SPA answers its own routes (and ZAP's probes such as `/swagger.json` or `/.env`) with `index.html`, and `/api`, `/api/v1` redirect to their trailing-slash forms. None of them returns data. |
| 10096 | Timestamp Disclosure (Unix) | INFO: false positive. The API returns ISO dates; the number in `GET /me` was a display name ZAP had written itself a moment earlier. |
| 10106 | HTTP Only Site | IGNORE: the scan targets the Compose stack over plain HTTP. In production TLS ends at the ingress or the host's proxy (Traefik sends HSTS; `compose.prod.yaml` sets `Secure` cookies). |
| 10049 | Storable and Cacheable Content | IGNORE: deliberate. The hashed assets are cached for a year as immutable and `index.html` is revalidated; API responses carry `Cache-Control: no-store`. |
| 2 | Private IP Disclosure | INFO: `GET /me/sessions` shows signed-in users the address of each of their own sessions (Settings → Sessions), by design; in the scan that is the Docker network's. |

Informational alerts that ZAP never counts as warnings, for the record:
- *A client error response code was returned* (607 instances): every malformed request got a
  `4xx` (`422` validation, `404`, `409`, `429` from the rate limits), and none got a `5xx`.
- *Authentication request identified*: the sign-in form.
- *User agent fuzzer*: the same responses for every browser.

## OpenVAS (Phase 18.8)

**2026-10-31**, on the reference machine: Greenbone Community Edition (gvmd 26.43, ospd-openvas
22.10.6, feed of 2026-10-30, 191,292 tests in "Full and fast") from Greenbone's compose file plus
[`tools/openvas/compose.override.yaml`](../tools/openvas/compose.override.yaml) (software images by
digest, web UI on localhost only, the scanner on the host's network), run by
[`tools/openvas-scan.sh`](../tools/openvas-scan.sh). Target: the instance's tailnet host name, TCP
80, 443 and 11434 (Ollama, reachable on the tailnet by design, ADR 0020/0022), unauthenticated
(no SSH or Windows credentials: the host isn't the system under test). 38 minutes; a rescan of 80/443 after the fixes (35 minutes) found 0 High, 0 Medium, 0 Low, 39 Log. Reports with the
host name stay outside git (`~/.local/state/finuance-openvas/reports/`).

**Result: 0 High, 0 Medium, 0 Low** (QoD ≥ 70); 47 Log entries (detections and inventories). The
Log entries worth a decision:

| Port | Entry | Decision |
|---|---|---|
| 443, 80 | *SSL/TLS: Report Medium Cipher Suites*: TLS 1.2 accepted `ECDHE-*-AES-*-CBC-SHA` (Traefik's defaults) | **Fixed:** Traefik's default `TLSOption` allows TLS 1.2 with ECDHE + AES-GCM / ChaCha20 only, and TLS 1.3 (`host/traefik-config.yaml`, Mozilla "intermediate"); checked with `openssl s_client`: CBC-SHA and TLS 1.1 refused. The rescan's "medium" list now holds only AES-GCM suites (TLS 1.2 ECDHE-ECDSA AES-128/256-GCM, TLS 1.3 AES-128-GCM): Greenbone's strength label for them, not a weakness; they are the recommended suites |
| 80 | *SSL/TLS: Version Detection* on port 80: a TLS handshake on the plain-HTTP port served the app, because the Ingress routers attached to every entry point | **Fixed:** chart value `ingress.entryPoints` (homelab: `[websecure]`): over TLS on port 80 every request now gets `404` (plain HTTP gets the `301` to HTTPS). Traefik still completes the TLS handshake there (it detects TLS on every entry point and picks the certificate by name; that can't be switched off per entry point), but serves nothing |
| 443 | *`includeSubDomains` / `preload` Missing in HSTS Header* | **Deliberate for now:** the host is a single `*.ts.net` name; `preload` needs the registrable domain, which isn't ours. Decided again with the public demo's own domain (Phase 29) |
| 80, 443 | *HTTP Server Banner*: `Server: nginx`, no version | Accepted: no version is disclosed (`server_tokens off`) |
| 80 | *HSTS Missing* | Expected: port 80 only answers `301` to HTTPS; HSTS is sent over HTTPS |
| 11434 | *HTTP Security Headers*, *Allowed HTTP Methods*: Ollama's API answers every tailnet device without authentication | **Deliberate** (ADR 0020/0022): only the tailnet reaches it (the LAN scan below finds it closed); a Tailscale ACL that limits port 11434 to this machine would narrow it to the cluster (the maintainer's tailnet settings) |
| 443 | HPKP missing | Expected: HPKP is deprecated and removed from browsers |

**Port scan from the LAN** (`tools/openvas-scan.sh lan-view`: nmap 7.95, all 65,535 TCP ports from
Docker's bridge network, i.e. through the host's firewall like another device on the home network):
**no open port** (27 minutes; closed ports drop). Confirms THREAT-MODEL T7 independently of the
phone test in 18.2.

**Image admission (KSV-0125), decided 2026-11-02 by the maintainer:** Kubernetes' built-in
`ValidatingAdmissionPolicy` (`deploy/k8s/admission/image-policy.yaml`, Argo CD app
`admission-policy`; no extra component) checks every pod created in `finuance`, `finuance-restore`,
`s3` and `monitoring`: only images from the registries listed for that namespace (Finuance's own only
from `ghcr.io/x2db736ac85h/`, CloudNativePG's from `ghcr.io/cloudnative-pg/`), and the images we pin
ourselves (RustFS, the AWS CLI, Tempo) only by digest. "Digests only" for everything isn't possible
yet: Finuance's images, CloudNativePG's PostgreSQL and the monitoring stack's charts deploy by tag.
**Phase 23:** cosign signature verification for Finuance's images, deployed by digest (the release
already records the digests it signs). Checked with server-side dry runs (RUNBOOK step 15).

## Pinned versions

Every version the repository depends on, where it is written, and what keeps it current (PLAN
tasks 22.3 and 22.4). `tools/check-pins.sh` (in `make check`) refuses an action by tag and an image
by tag alone. Renovate (the hosted app, `renovate.json`) proposes updates on Mondays and security
updates at once; `renovate-config-validator` checks the configuration in CI. A local
`renovate --platform=local --dry-run=extract` of 2026-11-19 found every row below.

| What | Where | Pinned as | Renovate manager |
|---|---|---|---|
| Go | `backend/go.mod` (`toolchain`), `mise.toml`, the `golang` image of `Dockerfile.backend` | version; the image by digest; `tools/check-toolchain.sh` keeps the three equal | `gomod`, `mise`, `dockerfile` (one group) |
| Go modules | `backend/go.mod`, `backend/go.sum` | version and checksum | `gomod` |
| npm packages | `web/package.json`, `tools/legacy-oracle/package.json` and their lockfiles | exact version and lockfile | `npm` |
| Base images | `deploy/docker/Dockerfile.backend`, `Dockerfile.web` | tag and digest | `dockerfile` |
| Compose images | `deploy/compose/*.yaml`, `tools/openvas/compose.override.yaml` | tag and digest (the repository's own images by tag) | `docker-compose` |
| Manifest images | `deploy/k8s/rehearsal`, `rustfs`, `tempo` | tag and digest | `kubernetes` |
| PostgreSQL in the cluster | `deploy/k8s/db/values.yaml` (`imageName`) | CloudNativePG tag (`18.7-standard-trixie`) | regex |
| Cluster charts | `deploy/k8s/argocd/apps/*.yaml` (CloudNativePG, Barman Cloud plugin, Sealed Secrets, kube-prometheus-stack) | chart version | `argocd` |
| GitHub actions | `.github/workflows/*.yml` (`uses:`) | commit SHA, the version in a comment | `github-actions` |
| Node in CI | `.github/workflows/*.yml` (`node-version`) | major version | `github-actions` |
| Tools installed in workflows | `env:` blocks: Helm, kind, kubeseal, kubeconform, yq, cosign, syft (`go install`), gitleaks (with its checksum), the Renovate validator | version, marked by a `# renovate:` comment | regex |
| Scanners and generators | `tools/bin-tools.sh`: sqlc, golangci-lint, Trivy, actionlint, zizmor, osv-scanner | version and four checksums each | regex; the checksums are updated by hand, CI fails until they are |
| Images run by scripts | `Makefile` (`K6_IMAGE`), `tools/rehearsal.sh`, `tools/homelab-drill.sh`, `tools/check-observability.sh` (promtool), `tools/zap-scan.sh`, `tools/openvas-scan.sh` | tag and digest | regex |
| Finuance's own images | `deploy/environments/homelab/values.yaml`, `compose.prod.yaml` | release tag; by digest and signature from Phase 23 | the release's `gitops-bump` pull request |
| The machine's software | k3s, Argo CD, Traefik, cert-manager on the reference machine | installed by the maintainer | none (RUNBOOK) |

## Next

- Phase 23: signature verification (cosign) for Finuance's images in the admission policy, and the
  images deployed by digest. When the nginx base is rebuilt with fixed zlib and musl, the
  Dockerfile's upgrade step can go.
- Phase 23 gate: repeat every scan above on the release candidate, with nothing open above LOW.
