# Finuance Helm chart

Runs Finuance on Kubernetes: the **api** (2–4 pods with an HPA), the **worker**, the **web** front end (nginx with the SPA), and a **migrations Job** that runs before every install and upgrade. PostgreSQL is not part of the chart. It comes from the CloudNativePG cluster in [`deploy/k8s/db`](../../k8s/db), or any PostgreSQL prepared with [`deploy/db/bootstrap.sql`](../../db/bootstrap.sql).

Operating it (deploys, rollbacks, secrets, restores): [docs/RUNBOOK.md](../../../docs/RUNBOOK.md).

## Install

The homelab installs it through Argo CD ([`deploy/k8s/argocd`](../../k8s/argocd)) with [`deploy/environments/homelab/values.yaml`](../../environments/homelab/values.yaml). By hand:

```sh
helm install finuance deploy/helm/finuance -n finuance --create-namespace \
  --set publicOrigin=https://finance.example.com --set ingress.host=finance.example.com \
  --set ingress.tls.clusterIssuer=letsencrypt-prod
```

Before that, the namespace needs the two basic-auth secrets of the database roles: `finuance-db-app` and `finuance-db-owner`, each with the keys `username` and `password` ([`tools/seal-secrets.sh`](../../../tools/seal-secrets.sh) makes them as SealedSecrets).

## What it creates

| Object | Notes |
|---|---|
| Deployments `-api`, `-worker`, `-web` | non-root (distroless uid 65532, nginx as uid 10101), read-only root filesystem, no capabilities, seccomp `RuntimeDefault`, no service account token; rolling updates with `maxUnavailable: 0`; startup, readiness (`/readyz`) and liveness (`/healthz`) probes |
| Job `-migrate` | `finuance migrate up` as the owner role; Helm `pre-install,pre-upgrade` hook and Argo CD `PreSync` |
| Services `-api` (8080 + metrics 9090), `-web` (8080) | the worker exposes only its metrics port, without a Service |
| Ingress + Traefik Middleware | `/api/` → api, `/` → web; HSTS; TLS by cert-manager, Traefik's ACME resolver, an existing secret, or none |
| HPA, PodDisruptionBudgets | api 2–4 pods at 70 % CPU; one pod of api and web always up |
| NetworkPolicies | default deny; Traefik → web/api; api, worker, migrations → PostgreSQL; DNS; metrics from the monitoring namespace; api → the model servers only when the assistant is on; the OTLP collector when set |
| ConfigMap `-env` | non-secret settings, including the password-less database URL (the password comes from `PGPASSWORD`) |
| PodMonitor (api + worker, `job` = `finuance-api` / `finuance-worker`) | optional, needs the Prometheus Operator |

## Values

Everything is documented in [`values.yaml`](values.yaml) and checked by [`values.schema.json`](values.schema.json). The ones you always set:

| Value | Meaning |
|---|---|
| `publicOrigin` | the address users open (`https://host`, no path); cookies and the CSRF origin check depend on it |
| `ingress.host` | the same host |
| `ingress.tls.mode` | `certManager` (+ `clusterIssuer`), `traefik` (+ `certResolver`), `secret` (+ `secretName`), or `none` |
| `ingress.entryPoints` | Traefik entry points the routers attach to (e.g. `[websecure]`, so port 80 only redirects); empty: all |
| `trustedProxies` | the pod CIDR of the ingress controller, so rate limits see the client address |
| `database.*` | host (default `finuance-db-rw`), roles and their secrets |
| `assistant.*` | off by default; the connections file, a secret with API keys, and the egress to the model servers |
| `otel.endpoint` | the full OTLP/HTTP traces URL, path included (`http://tempo.monitoring.svc:4318/v1/traces`); a bare `host:4318` is refused |
| `podMonitor.enabled` | PodMonitors for api and worker (`job` = `finuance-api` / `finuance-worker`) |
| `extraEnv` | further `FINUANCE_*` settings, e.g. `FINUANCE_AI_TIMEOUT` / `FINUANCE_AI_IDLE_TIMEOUT` for a slow model server |

Wrong settings stop the render with a message (no host, no issuer, a path in `publicOrigin`, a non-`FINUANCE_*` name in `extraEnv`, an OTLP endpoint without a path, …).

The api's readiness probe (`/readyz`: database and migration version, 3 s) treats a database that only answers slowly as busy for 30 s, so a busy database doesn't take every replica out of the Service at once; a real error or migrations behind still fail at once.

## Checks

`make helm-lint` ([`tools/check-helm.sh`](../../../tools/check-helm.sh)) lints and renders the chart with [`ci/homelab-like-values.yaml`](ci/homelab-like-values.yaml) (every optional part on), [`ci/minimal-values.yaml`](ci/minimal-values.yaml) and the homelab values. It validates the output with `kubeconform -strict` (CRD schemas included) and checks that wrong settings fail. The CI `helm` job also scans the rendered manifests with `trivy config`, and HIGH or CRITICAL findings fail the build. The remaining LOW/MEDIUM findings are deliberate (reviewed in [docs/security-scan.md](../../../docs/security-scan.md)):
- no CPU limits, so the pods aren't throttled;
- no `namespace:` in the rendered files (Helm sets it);
- no registry allow-list;
- `FINUANCE_MAX_UPLOAD_BYTES`, which the scanner reads as sensitive.

The web pod runs nginx as uid 10101 rather than the image's 101 (Phase 17 chart scan).
