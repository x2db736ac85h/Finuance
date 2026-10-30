# Homelab environment

The values for the self-hosted production install, used by the Argo CD applications in [`deploy/k8s/argocd/apps`](../../k8s/argocd/apps).

| File | For |
|---|---|
| [`values.yaml`](values.yaml) | the app chart ([`deploy/helm/finuance`](../../helm/finuance)) |
| [`db-values.yaml`](db-values.yaml) | the database chart ([`deploy/k8s/db`](../../k8s/db)) |
| [`argocd-values.yaml`](argocd-values.yaml) | Argo CD itself (chart `argo-cd`, installed once with helm; [RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s) step 5) |
| [`secrets/`](secrets) | the SealedSecrets (made in Phase 18 with `tools/seal-secrets.sh homelab …`) |
| [`host/`](host) | files installed on the k3s host itself, not in the cluster: the nftables table that keeps pods off the home network ([RUNBOOK](../../../docs/RUNBOOK.md#bootstrap-on-single-node-k3s), step 2), and `ollama.conf` for the model server; templates, the filled-in copies live in the private environment repository |

This folder is the GitOps promotion target. A merged pull request that changes it is what deploys; `release.yml` opens one per release that bumps `image.tag`. Reverting it rolls back.

Settings marked *Phase 18* are not known yet: the host name, the certificate issuer, the pod CIDR, the storage class, the backup bucket and the LM Studio address. They are set during the homelab rollout.
