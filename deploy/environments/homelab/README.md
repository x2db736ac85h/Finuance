# Homelab environment

The values for the self-hosted production install, used by the Argo CD applications in [`deploy/k8s/argocd/apps`](../../k8s/argocd/apps).

| File | For |
|---|---|
| [`values.yaml`](values.yaml) | the app chart ([`deploy/helm/finuance`](../../helm/finuance)) |
| [`db-values.yaml`](db-values.yaml) | the database chart ([`deploy/k8s/db`](../../k8s/db)) |
| [`host-values.example.yaml`](host-values.example.yaml) | the shape of the per-machine overlay (host name, model server address) that lives in the private environment repository and is applied after `values.yaml` (ADR 0023); rendered by `make helm-lint` |
| [`argocd-values.yaml`](argocd-values.yaml) | Argo CD itself (chart `argo-cd`, installed once with helm; [RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s) step 5) |
| [`secrets/`](secrets) | the SealedSecrets (made in Phase 18 with `tools/seal-secrets.sh homelab …`) |
| [`host/`](host) | files installed on the k3s host itself, not in the cluster: the nftables table that keeps pods off the home network ([RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s), step 2), `ollama.conf` for the model server, `finuance-tls@.service`/`.timer` that renew the ingress certificate (step 9), and `traefik-config.yaml` that limits k3s's Traefik to the tailnet (step 10); templates, the filled-in copies live in the private environment repository |

This folder is the GitOps promotion target. A merged pull request that changes it is what deploys; `release.yml` opens one per release that bumps `image.tag`. Reverting it rolls back.

Per-machine values (host name, the model server's address) are not here: they come from the private environment repository as a second Argo CD source (ADR 0023). Settings marked *Phase 18.6* wait for the observability stack.
