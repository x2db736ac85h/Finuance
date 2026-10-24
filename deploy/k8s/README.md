# Kubernetes manifests

The cluster-side pieces around the [Helm chart](../helm/finuance). How to run them: [docs/RUNBOOK.md](../../docs/RUNBOOK.md).

| Folder | What |
|---|---|
| [`db/`](db) | Helm chart `finuance-db`: the PostgreSQL cluster on CloudNativePG, with 2 instances, managed roles `finuance_owner` and `finuance_app`, and the bootstrap SQL (`files/bootstrap.sql`, kept equal to `deploy/db/bootstrap.sql` by `make check`). WAL archiving and daily base backups go to S3-compatible storage through the Barman Cloud plugin (14-day recovery window), and a NetworkPolicy guards it. Set `recovery.enabled` to restore from the bucket, optionally to a point in time. |
| [`argocd/`](argocd) | GitOps: [`root.yaml`](argocd/root.yaml) (app-of-apps, applied once) and [`apps/`](argocd/apps). `apps/` holds the projects, the CloudNativePG operator, the Barman Cloud plugin, the Sealed Secrets controller (pinned Helm charts), the sealed secrets, the database (manual sync) and the app (automated sync with prune and self-heal). |

The environment values are in [`deploy/environments/`](../environments). Dashboards and alert rules come in Phase 17.
