# Homelab environment

The values for the self-hosted production install, used by the Argo CD applications in [`deploy/k8s/argocd/apps`](../../k8s/argocd/apps).

| File | For |
|---|---|
| [`values.yaml`](values.yaml) | the app chart ([`deploy/helm/finuance`](../../helm/finuance)) |
| [`db-values.yaml`](db-values.yaml) | the database chart ([`deploy/k8s/db`](../../k8s/db)) |
| [`secrets/`](secrets) | the SealedSecrets (made in Phase 18 with `tools/seal-secrets.sh homelab …`) |

This folder is the GitOps promotion target. A merged pull request that changes it is what deploys; `release.yml` opens one per release that bumps `image.tag`. Reverting it rolls back.

Settings marked *Phase 18* are not known yet: the host name, the certificate issuer, the pod CIDR, the storage class, the backup bucket and the LM Studio address. They are set during the homelab rollout.
