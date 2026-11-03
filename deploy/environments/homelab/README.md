# Homelab environment

The values for the self-hosted production install, used by the Argo CD applications in [`deploy/k8s/argocd/apps`](../../k8s/argocd/apps).

| File | For |
|---|---|
| [`values.yaml`](values.yaml) | the app chart ([`deploy/helm/finuance`](../../helm/finuance)) |
| [`db-values.yaml`](db-values.yaml) | the database chart ([`deploy/k8s/db`](../../k8s/db)) |
| [`host-values.example.yaml`](host-values.example.yaml) | the shape of the per-machine overlay (host name, model server address) that lives in the private environment repository and is applied after `values.yaml` (ADR 0023); rendered by `make helm-lint` |
| [`monitoring-values.yaml`](monitoring-values.yaml) | kube-prometheus-stack, sized for one node (Grafana's admin login from a SealedSecret) |
| [`observability-values.yaml`](observability-values.yaml) | Finuance's dashboards, rules and receiver (`deploy/k8s/observability`, Traefik included); the alert addresses come from the private overlay ([`observability-values.example.yaml`](observability-values.example.yaml) shows its shape) |
| [`monitoring/`](monitoring) | SealedSecrets in `monitoring`: the alert mail account's app password and Grafana's admin login |
| [`rustfs/`](rustfs) | the SealedSecret with the in-cluster bucket's keys |
| [`restore-db-values.yaml`](restore-db-values.yaml), [`restore-app-values.yaml`](restore-app-values.yaml) | the restore drill's scratch cluster and app (`tools/homelab-drill.sh restore`) |
| [`sealed-secrets.pem`](sealed-secrets.pem) | the controller's public certificate, for sealing offline (`SEAL_CERT`) |
| [`argocd-values.yaml`](argocd-values.yaml) | Argo CD itself (chart `argo-cd`, installed once with helm; [RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s) step 5) |
| [`secrets/`](secrets) | the SealedSecrets (made in Phase 18 with `tools/seal-secrets.sh homelab …`) |
| [`host/`](host) | files installed on the k3s host itself, not in the cluster: the nftables table that keeps pods off the home network ([RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s), step 2), `ollama.conf` for the model server, `finuance-tls@.service`/`.timer` that renew the ingress certificate (step 9), and `traefik-config.yaml` for k3s's Traefik (host ports with the client's address, HTTP → HTTPS, TLS 1.2 with AEAD suites only, per-router metrics; step 10); templates, the filled-in copies live in the private environment repository |

This folder is the GitOps promotion target. A merged pull request that changes it is what deploys; `release.yml` opens one per release that bumps `image.tag`. Reverting it rolls back.

Per-machine values (host name, the model server's address and model, alert addresses) are not here: they come from the private environment repository as a second Argo CD source (ADR 0023). How this environment was built, step by step: [RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s) "Bootstrap on single-node k3s" (steps 1–15).
