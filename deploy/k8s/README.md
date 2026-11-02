# Kubernetes manifests

The cluster-side pieces around the [Helm chart](../helm/finuance). How to run them: [docs/RUNBOOK.md](../../docs/RUNBOOK.md).

| Folder | What |
|---|---|
| [`db/`](db) | Helm chart `finuance-db`: the PostgreSQL cluster on CloudNativePG, with 2 instances, managed roles `finuance_owner` and `finuance_app`, and the bootstrap SQL (`files/bootstrap.sql`, kept equal to `deploy/db/bootstrap.sql` by `make check`). WAL archiving and daily base backups go to S3-compatible storage through the Barman Cloud plugin (14-day recovery window), and a NetworkPolicy guards it. Set `recovery.enabled` to restore from the bucket, optionally to a point in time. |
| [`observability/`](observability) | Helm chart `finuance-observability`: six Grafana dashboards, the SLO recording rules and burn-rate alerts ([docs/SLO.md](../../docs/SLO.md)), app, database, backup and pod alerts, and the e-mail receiver; checked by `make helm-lint` (promtool unit tests, metric names). Installed next to kube-prometheus-stack; with `ingress.traefik` it also watches what users get at the ingress. |
| [`rustfs/`](rustfs) | The in-cluster S3 bucket for backups on a cluster without an outside store (ADR 0022): RustFS by digest, a never-pruned volume, an idempotent bucket job, default-deny NetworkPolicies. |
| [`tempo/`](tempo) | Tempo in single-binary mode (OTLP over HTTP only, traces kept 72 h, read-only, NetworkPolicy); own manifests because Grafana's single-binary chart is deprecated. |
| [`admission/`](admission) | A built-in `ValidatingAdmissionPolicy`: pods in Finuance's namespaces may only use images from the registries listed per namespace, and our digest-pinned ones only by digest (Phase 18.8; signatures in Phase 19). |
| [`rehearsal/`](rehearsal) | Stand-ins for the kind rehearsal (an S3 bucket, the mock model server), hardened like the app's pods. |
| [`argocd/`](argocd) | GitOps: [`root.yaml`](argocd/root.yaml) (app-of-apps, applied once) and [`apps/`](argocd/apps). `apps/` holds the projects, the CloudNativePG operator, the Barman Cloud plugin, the Sealed Secrets controller (pinned Helm charts), the sealed secrets, the database (manual sync), the app (automated sync with prune and self-heal), RustFS, the monitoring stack (kube-prometheus-stack, Tempo, `finuance-observability`) and the admission policy. A phase may point the apps at its branch to try them on a cluster (RUNBOOK "Back to main after a phase"); `make helm-lint` requires `main` on `main` and in pull requests into it. |

The environment values are in [`deploy/environments/`](../environments).
