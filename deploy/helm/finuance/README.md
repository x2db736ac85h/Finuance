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
| Deployments `-api`, `-worker`, `-web` | non-root (distroless uid 65532, nginx uid 101), read-only root filesystem, no capabilities, seccomp `RuntimeDefault`, no service account token; rolling updates with `maxUnavailable: 0`; startup, readiness (`/readyz`) and liveness (`/healthz`) probes |
| Job `-migrate` | `finuance migrate up` as the owner role; Helm `pre-install,pre-upgrade` hook and Argo CD `PreSync` |
| Services `-api` (8080 + metrics 9090), `-web` (8080) | the worker exposes only its metrics port, without a Service |
| Ingress + Traefik Middleware | `/api/` → api, `/` → web; HSTS; TLS by cert-manager, Traefik's ACME resolver, an existing secret, or none |
| HPA, PodDisruptionBudgets | api 2–4 pods at 70 % CPU; one pod of api and web always up |
| NetworkPolicies | default deny; Traefik → web/api; api, worker, migrations → PostgreSQL; DNS; metrics from the monitoring namespace; api → the model servers only when the assistant is on; the OTLP collector when set |
| ConfigMap `-env` | non-secret settings, including the password-less database URL (the password comes from `PGPASSWORD`) |
| ServiceMonitor | optional, needs the Prometheus Operator |

## Values

Everything is documented in [`values.yaml`](values.yaml) and checked by [`values.schema.json`](values.schema.json). The ones you always set:

| Value | Meaning |
|---|---|
| `publicOrigin` | the address users open (`https://host`, no path); cookies and the CSRF origin check depend on it |
| `ingress.host` | the same host |
| `ingress.tls.mode` | `certManager` (+ `clusterIssuer`), `traefik` (+ `certResolver`), `secret` (+ `secretName`), or `none` |
| `trustedProxies` | the pod CIDR of the ingress controller, so rate limits see the client address |
| `database.*` | host (default `finuance-db-rw`), roles and their secrets |
| `assistant.*` | off by default; the connections file, a secret with API keys, and the egress to the model servers |

Wrong settings stop the render with a message (no host, no issuer, a path in `publicOrigin`, a non-`FINUANCE_*` name in `extraEnv`, …).

## Checks

`make helm-lint` ([`tools/check-helm.sh`](../../../tools/check-helm.sh)) lints and renders the chart with [`ci/homelab-like-values.yaml`](ci/homelab-like-values.yaml) (every optional part on), [`ci/minimal-values.yaml`](ci/minimal-values.yaml) and the homelab values. It validates the output with `kubeconform -strict` (CRD schemas included) and checks that wrong settings fail. The CI `helm` job also scans the rendered manifests with `trivy config`, and HIGH or CRITICAL findings fail the build. The LOW/MEDIUM findings are deliberate:
- no CPU limits, so the pods aren't throttled;
- nginx's fixed uid 101;
- no `namespace:` in the rendered files (Helm sets it);
- no registry allow-list;
- `FINUANCE_MAX_UPLOAD_BYTES`, which the scanner reads as sensitive.

Phase 17 reviews them with the chart scan.
