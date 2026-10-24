# Homelab secrets (sealed)

The SealedSecrets of the homelab, applied by the Argo CD application `finuance-secrets`
(`deploy/k8s/argocd/apps/finuance-secrets.yaml`). Only encrypted `*.sealed.yaml` files belong here.
The Sealed Secrets controller in the cluster is the only one that can decrypt them.

They are made with `tools/seal-secrets.sh homelab` against the cluster's public certificate.
It makes random database passwords and asks for the backup store's keys. The steps, rotation
and re-sealing after a controller key loss are in [docs/RUNBOOK.md](../../../../docs/RUNBOOK.md#secrets).
The files are created in Phase 18, the homelab rollout; until then this folder is empty.

| File | Secret | Keys | Used by |
|---|---|---|---|
| `finuance-db-owner.sealed.yaml` | `finuance-db-owner` (basic-auth) | `username`, `password` | CNPG owner role, migrations Job |
| `finuance-db-app.sealed.yaml` | `finuance-db-app` (basic-auth) | `username`, `password` | CNPG app role, api, worker |
| `finuance-db-backup.sealed.yaml` | `finuance-db-backup` | `ACCESS_KEY_ID`, `ACCESS_SECRET_KEY` | Barman Cloud plugin (object store) |
| `finuance-assistant.sealed.yaml` (optional) | `finuance-assistant` | the `apiKeyEnv` names of the connections file | api (cloud model connections) |
