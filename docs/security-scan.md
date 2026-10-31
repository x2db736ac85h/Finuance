# Security scan

Every finding of every scan, and what happened to it: fixed, or not exploitable and why. HIGH and
CRITICAL findings fail the build and are never accepted (`.trivyignore` is empty). The scans and
when they run are listed in [THREAT-MODEL.md](THREAT-MODEL.md) §7. Phase 18 adds the OpenVAS /
Nessus scan of the deployed instance, and the Phase 19 gate repeats everything right before the
demo goes public.

**Phase 17 run: 2026-10-26/27**, in the cloud sandbox, on the push 3 branch. The images were built
from the checkout, and the stack was `compose.ci.yaml` (sign-ups on, mock model). Trivy 0.76.0 ran
with a vulnerability database of 2026-10-26.

**Release scan before `v0.2.0-rc.1`: 2026-10-30**, on the reference machine, on `main` plus the release
changes (branch `chore/release-0.2.0`), images built locally from it; Trivy 0.76.0 with that day's
database, govulncheck 1.9.0 (`vuln.go.dev` reachable), `npm audit`. **New since Phase 17:**
GO-2026-6866 and GO-2026-6860 in `golang.org/x/net` 0.61.0 (Trivy: CVE-2026-78661, CVE-2026-97045),
not reachable: **fixed** by 0.62.0. zlib and musl in the web image: **fixed** by upgrading them in the
Dockerfile (still no rebuilt base). After the changes: web image 0 findings; api image and
repository only GO-2026-5932 (no fix exists); `npm audit` 0; govulncheck 0 reachable, 1 in a required
module (GO-2026-5932). The repository's misconfiguration findings are unchanged (below).

## Summary

| Scan | Result | Open |
|---|---|---|
| Trivy, api image | 2 findings; 1 fixed by a base image bump | 1 (UNKNOWN, not reachable) |
| Trivy, web image | 3 findings (MEDIUM / UNKNOWN), no fixed base yet; fixed 2026-10-30 by upgrading zlib and musl in the Dockerfile | 0 |
| Trivy, repository (vuln, secret, misconfig) | the api image's Go finding again; 5 LOW/MEDIUM in the rehearsal stand-ins | all deliberate |
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
| same file: KSV-0125 (image not from an allow-listed registry) | MEDIUM | **Deliberate for now:** Docker Hub images (`rustfs/rustfs`, `amazon/aws-cli`) pinned by digest; the cluster's admission policy is decided with the app's images (18.8). |
| `deploy/k8s/tempo/tempo.yaml` (Phase 18.6): KSV-0011 (CPU not limited) | LOW | **Deliberate:** as for the app's pods and RustFS: CPU request set, memory limited; non-root, read-only root, no capabilities, RuntimeDefault seccomp, default-deny NetworkPolicy (OTLP only from the app, queries only from Grafana). |
| same file: KSV-0125 (image not from an allow-listed registry) | MEDIUM | **Deliberate for now:** `grafana/tempo` pinned by digest; the admission policy is decided in 18.8. |

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
| KSV-0125: images from an untrusted registry | MEDIUM | all | **Deliberate for now:** the images are `ghcr.io/x2db736ac85h/*`, signed with cosign. An admission policy that allows only this registry and verifies the signatures belongs to the cluster (Phase 18 decides). |
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
(no SSH or Windows credentials: the host isn't the system under test). 38 minutes. Reports with the
host name stay outside git (`~/.local/state/finuance-openvas/reports/`).

**Result: 0 High, 0 Medium, 0 Low** (QoD ≥ 70); 47 Log entries (detections and inventories). The
Log entries worth a decision:

| Port | Entry | Decision |
|---|---|---|
| 443, 80 | *SSL/TLS: Report Medium Cipher Suites*: TLS 1.2 accepted `ECDHE-*-AES-*-CBC-SHA` (Traefik's defaults) | **Fixed:** Traefik's default `TLSOption` allows TLS 1.2 with ECDHE + AES-GCM / ChaCha20 only, and TLS 1.3 (`host/traefik-config.yaml`, Mozilla "intermediate"); checked with `openssl s_client`: CBC-SHA and TLS 1.1 refused |
| 80 | *SSL/TLS: Version Detection* on port 80: a TLS handshake on the plain-HTTP port served the app, because the Ingress routers attached to every entry point | **Fixed:** chart value `ingress.entryPoints` (homelab: `[websecure]`); port 80 only redirects |
| 443 | *`includeSubDomains` / `preload` Missing in HSTS Header* | **Deliberate for now:** the host is a single `*.ts.net` name; `preload` needs the registrable domain, which isn't ours. Decided again with the public demo's own domain (Phase 19) |
| 80, 443 | *HTTP Server Banner*: `Server: nginx`, no version | Accepted: no version is disclosed (`server_tokens off`) |
| 80 | *HSTS Missing* | Expected: port 80 only answers `301` to HTTPS; HSTS is sent over HTTPS |
| 11434 | *HTTP Security Headers*, *Allowed HTTP Methods*: Ollama's API answers every tailnet device without authentication | **Deliberate** (ADR 0020/0022): only the tailnet reaches it (the LAN scan below finds it closed); a Tailscale ACL that limits port 11434 to this machine would narrow it to the cluster (our Product Engineer's tailnet settings) |
| 443 | HPKP missing | Expected: HPKP is deprecated and removed from browsers |

**Port scan from the LAN** (`tools/openvas-scan.sh lan-view`: nmap 7.95, all 65,535 TCP ports from
Docker's bridge network, i.e. through the host's firewall like another device on the home network):
**no open port** (27 minutes; closed ports drop). Confirms THREAT-MODEL T7 independently of the
phone test in 18.2.

**Image admission (KSV-0125):** see "Next".

## Next

- Phase 18: an admission policy for the image registry and signatures (KSV-0125), our Product
  Engineer's decision pending. When the nginx base is rebuilt with fixed zlib and musl, the
  Dockerfile's upgrade step can go.
- Phase 19 gate: repeat every scan above on the release candidate, with nothing open above LOW.
