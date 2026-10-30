# Runbook

How to run Finuance on Kubernetes: the first install, deploys and rollbacks, secrets, scaling, backups and restores, and what to do when something breaks. The Compose install is in the [main README](../README.md#deploying).

> **Status (Phase 16).** The manifests are written and checked in CI: `make helm-lint` and the `helm` job render, validate and scan them; the kind rehearsal (`tools/rehearsal.sh`, workflow `k8s-rehearsal`) installs everything on a throwaway cluster and runs the drills below. Nothing is deployed on the homelab yet. Values marked *Phase 18* in `deploy/environments/homelab/` are set during the homelab rollout, and the timings below come from the CI rehearsal until then.

## What runs where

| Piece | Where | Source | Sync |
|---|---|---|---|
| Argo CD app-of-apps `finuance-root` | `argocd` | [`deploy/k8s/argocd/root.yaml`](../deploy/k8s/argocd/root.yaml) | automated |
| CloudNativePG operator | `cnpg-system` | Helm chart `cloudnative-pg` (pinned in [`apps/cnpg-operator.yaml`](../deploy/k8s/argocd/apps/cnpg-operator.yaml)) | automated |
| Barman Cloud plugin (backups) | `cnpg-system` | Helm chart `plugin-barman-cloud` | automated |
| Sealed Secrets controller | `kube-system` | Helm chart `sealed-secrets` | automated |
| Sealed secrets | `finuance` | [`deploy/environments/homelab/secrets/`](../deploy/environments/homelab/secrets) | automated, no prune |
| PostgreSQL cluster `finuance-db` (2 instances) | `finuance` | [`deploy/k8s/db`](../deploy/k8s/db) + `homelab/db-values.yaml` | **manual** |
| api (2–4 pods, HPA), worker, web (2 pods), migrations Job | `finuance` | [`deploy/helm/finuance`](../deploy/helm/finuance) + `homelab/values.yaml` | automated, prune + self-heal |

Traffic goes Traefik → Ingress: `/api/` → api:8080 and `/` → web:8080. NetworkPolicies deny everything else: only Traefik may reach web and api, only api, worker and the migrations Job may reach PostgreSQL, and the api reaches a model server only when the assistant is on. The namespace enforces the `restricted` Pod Security Standard.

Database logins are two CloudNativePG managed roles: `finuance_owner` (owns the schema, runs migrations) and `finuance_app` (api and worker, subject to row-level security). Their passwords live in the basic-auth secrets `finuance-db-owner` and `finuance-db-app`. The pods get them as `PGPASSWORD`, so database URLs never contain a password.

## Prerequisites

- Kubernetes ≥ 1.30 (k3s is fine), `kubectl` access as cluster admin.
- Traefik as the ingress controller (k3s ships it in `kube-system`).
- **cert-manager**: the Barman Cloud plugin needs it for its certificates, and `ingress.tls.mode: certManager` uses a ClusterIssuer (Let's Encrypt). Not part of the app-of-apps, because a homelab usually runs one already.
- Argo CD (`argocd` namespace).
- A storage class for the database volumes (k3s: `local-path`).
- An S3-compatible bucket for backups, **outside the cluster's disks** (MinIO on another machine, Backblaze B2, S3, …).
- Tools on your machine: `kubectl`, `helm` (3.18), `kubeseal` (0.40), mikefarah's `yq` (v4), optionally the `kubectl cnpg` plugin.

## Bootstrap (first install)

1. **Argo CD knows how healthy a child app is.** By default an Application has no health, so sync waves don't wait for the child apps. Add this to the `argocd-cm` ConfigMap (Argo CD's documented snippet):

   ```yaml
   data:
     resource.customizations.health.argoproj.io_Application: |
       hs = {}
       hs.status = "Progressing"
       hs.message = ""
       if obj.status ~= nil and obj.status.health ~= nil then
         hs.status = obj.status.health.status
         if obj.status.health.message ~= nil then hs.message = obj.status.health.message end
       end
       return hs
   ```

2. **Set the homelab values** in `deploy/environments/homelab/values.yaml` and `db-values.yaml` (every *Phase 18* comment: host, issuer, pod CIDR, storage class, bucket), then merge them to `main`.
3. **Apply the root app** once: `kubectl apply -f deploy/k8s/argocd/root.yaml`. Argo CD creates the projects, the operators and the Sealed Secrets controller. `finuance-db` waits for a manual sync, so `finuance` (wave 0) waits too.
4. **Back up the Sealed Secrets key** as soon as the controller runs (see [Secrets](#secrets)).
5. **Seal the secrets** and commit them:

   ```sh
   tools/seal-secrets.sh homelab db                                      # random passwords for both roles
   ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… tools/seal-secrets.sh homelab backup
   git add deploy/environments/homelab/secrets && git commit -m "chore(deploy): homelab secrets"
   ```

6. **Sync the database** in Argo CD (`finuance-db` → Sync) and wait for `kubectl -n finuance get cluster finuance-db` to report *Cluster in healthy state*. The first base backup starts right away (`backup.immediate`); check it with `kubectl -n finuance get backups`.
7. **The app syncs by itself**: the migrations Job runs (PreSync), then api, worker and web roll out.
8. **Create the first account** (sign-up is off):

   ```sh
   kubectl -n finuance exec -i deploy/finuance-api -- /finuance admin create-user --email you@example.com --password-stdin <<<'a long password'
   ```

9. Open the public origin, sign in, import a file, and check `kubectl -n finuance get pods`: no restarts.

## Deploy

1. A `vX.Y.Z` tag runs `release.yml`: images, scans, signatures, the GitHub release, then a pull request **chore(deploy): homelab to vX.Y.Z** that changes `image.tag` in `deploy/environments/homelab/values.yaml`. Release candidates (`-rc.N`) get no pull request.
   Tag a commit whose message has no `[skip ci]`: GitHub skips every push-triggered workflow for such a commit, the tag push included, and the release never starts (it happened with the first `v0.2.0-rc.1` tag).
2. The pull request is opened with the workflow token, so CI doesn't start on its own. Close and reopen it to run the checks.
3. Merge it. Argo CD syncs `main` (within its polling interval, or press *Refresh*):
   - the migrations Job `finuance-migrate` runs first (Helm `pre-upgrade` hook = Argo CD `PreSync`; the previous Job is deleted before the new one starts);
   - then the Deployments roll one pod at a time (`maxUnavailable: 0`). A new api pod gets traffic only once `/readyz` passes, which needs the database reachable and the schema at least at the version the binary expects.
4. Watch it: `kubectl -n finuance get pods -w`, Argo CD's app view, `kubectl -n finuance logs job/finuance-migrate`.

A change to the chart or to the values deploys the same way: any merged change under `deploy/helm/finuance` or `deploy/environments/homelab/values.yaml`.

## Rollback

**Revert the pull request** that deployed the version and merge the revert. Argo CD rolls the Deployments back to the previous image.

Migrations are **not** rolled back. That is safe because:

- the readiness check only requires the schema to be *at least* the binary's version, so an older binary runs against a newer schema;
- migrations are written expand-then-contract: a release adds columns and tables, and a column is dropped only in a later release, once no deployed version reads it.

Undo a migration only when it is itself the problem, and only by hand with a backup taken first ([On-demand backup](#on-demand-backup)). Run it **before** reverting, while the values still name the release that has the migration. The chart's own Job is rendered with `migrate down` instead of `migrate up`, so it keeps the security context, the network policy labels and the owner's password:

```sh
helm template finuance deploy/helm/finuance -n finuance -f deploy/environments/homelab/values.yaml -s templates/migrate-job.yaml \
  | yq '.metadata.name = "finuance-migrate-down" | del(.metadata.annotations) | .spec.template.spec.containers[0].args = ["migrate", "down"]' \
  | kubectl -n finuance apply -f -
kubectl -n finuance logs -f job/finuance-migrate-down
```

`migrate down` undoes one migration; `migrate status` lists them. Pause Argo CD's auto-sync for `finuance` while you do this, or self-heal starts the newer release again.

## Failed migration

A failed `finuance-migrate` Job fails the sync. **The old pods keep serving**, because the hook runs before anything else changes.

1. Read why: `kubectl -n finuance logs job/finuance-migrate`. Each migration runs in one transaction (goose), so a failed migration leaves nothing half-done; earlier migrations of the same run stay applied.
2. A **database problem** (unreachable, disk full, lock timeout): fix it, then *Sync* again. The hook Job is recreated.
3. A **bug in the migration**: revert the release's pull request (the database is still at the previous release's schema, plus any migrations before the failing one) and ship a fixed release.
4. The Job gives up after `migrations.backoffLimit` (2) retries or `activeDeadlineSeconds` (600 s). It is kept for a day (`ttlSecondsAfterFinished`) so its logs can still be read.

## Secrets

All secrets are SealedSecrets in `deploy/environments/homelab/secrets/`. They are encrypted with the controller's public key and bound to their name and namespace (strict scope). Only the controller in the cluster can decrypt them. [`tools/seal-secrets.sh`](../tools/seal-secrets.sh) writes each Secret into `kubeseal` through a pipe, so plain values never touch the disk or a command line.

| Secret | Made with | Rotation |
|---|---|---|
| `finuance-db-owner`, `finuance-db-app` | `tools/seal-secrets.sh homelab db` (32 random characters each) | below |
| `finuance-db-backup` | `ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… tools/seal-secrets.sh homelab backup` | new key at the store, re-seal, merge; the plugin reloads it (`cnpg.io/reload`) |
| `finuance-assistant` (optional) | `tools/seal-secrets.sh homelab assistant keys.env` (`KEY=value` lines, the connections file's `apiKeyEnv` names) | re-seal, merge, then `kubectl -n finuance rollout restart deploy/finuance-api` |

### Back up the controller key

Without it, nobody can decrypt the committed secrets after a cluster loss. Right after the controller starts, and after each key renewal (every 30 days by default):

```sh
kubectl -n kube-system get secret -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-keys.yaml
```

Keep the file **outside git and outside the cluster**, in a password manager or on an encrypted drive. Save the public certificate too, so you can seal offline: `kubeseal --fetch-cert > deploy/environments/homelab/sealed-secrets.pem` (public, may be committed); then `SEAL_CERT=deploy/environments/homelab/sealed-secrets.pem tools/seal-secrets.sh …`.

### Rotate the database passwords

1. `tools/seal-secrets.sh homelab db`, commit, merge. Argo CD updates the two Secrets.
2. CloudNativePG notices the change (`cnpg.io/reload`) and sets the new passwords on the roles within seconds. Connections that are already open stay up; new connections need the new password.
3. Restart the processes so they read the new `PGPASSWORD`: `kubectl -n finuance rollout restart deploy/finuance-api deploy/finuance-worker`. The rollout replaces one pod at a time, so the app stays up.

Rotate one role at a time if you want no window at all. Seal just that role by editing the script's `db` case, or run the steps above twice.

### Restore after losing the cluster or the key

- **Cluster lost, key backed up:** before Argo CD syncs the secrets, `kubectl apply -f sealed-secrets-keys.yaml` and restart the controller (`kubectl -n kube-system delete pod -l app.kubernetes.io/name=sealed-secrets`). The committed secrets decrypt as before.
- **Key lost:** the committed secrets can't be decrypted. Make new ones: run `tools/seal-secrets.sh` again against the new controller (backup keys from the store; new database passwords) and commit them. A database restored from backup brings the old passwords, and the managed roles then set the new ones from the new secrets.
- After a key renewal, old secrets keep working (old keys are kept). To re-encrypt them with the newest key: `kubeseal --re-encrypt < f.sealed.yaml > f.tmp && mv f.tmp f.sealed.yaml`.

## Scaling

- **api**: the HPA keeps 2–4 pods at 70 % CPU (`api.autoscaling`). The PodDisruptionBudget keeps one pod during node drains. Each pod opens at most `database.maxConns` (10) connections, so 4 api pods + the worker stay well under `max_connections` (100).
- **worker**: `worker.replicas` (1 by default). River spreads jobs over any number of workers; imports for one account are serialised by a row lock (ADR 0013).
- **web**: `web.replicas` (2); static files only.
- **PostgreSQL instances**: `cluster.instances` in `db-values.yaml`, then a manual sync. Two instances = a primary plus a streaming replica that takes over automatically.
- **Database volume**: raise `cluster.storage.size`. CNPG expands the volumes when the storage class allows expansion. **`local-path` does not**: there, restore into a new cluster with a larger size ([Restore](#restore)).

## Backups

- **Continuous WAL archiving** plus a **daily base backup** at 02:15 (`backup.schedule`, cron with seconds), both through the Barman Cloud plugin to `objectStore.destinationPath/<serverName>/`.
- **Retention**: a 14-day recovery window (`objectStore.retention`). Older backups are deleted after the next one completes.
- Check: `kubectl -n finuance get backups`, `kubectl -n finuance get scheduledbackup`, and `kubectl cnpg status finuance-db -n finuance` (shows WAL archiving). The `finuance-db-store` ObjectStore status shows the first and last recoverability points.
- **If no bucket is available**, CloudNativePG can take Kubernetes volume snapshots instead (`method: volumeSnapshot`, needs a CSI driver with snapshots; not `local-path`). This is the fallback decided in Phase 18 if there is no S3 store.

### On-demand backup

Before a risky change (a migration down, a major upgrade):

```sh
kubectl -n finuance apply -f - <<'EOF'
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata: { name: finuance-db-manual-1 }
spec:
  cluster: { name: finuance-db }
  method: plugin
  pluginConfiguration: { name: barman-cloud.cloudnative-pg.io }
EOF
kubectl -n finuance get backup finuance-db-manual-1 -w   # until phase: completed
```

## Restore

A restore creates a **new** cluster from the bucket (CNPG never restores in place). The new cluster gets the database, its roles, the extensions and the `textlike` LEAKPROOF flag from the backup. It must archive to **another folder** than its origin (`backup.serverName`); the chart refuses to render otherwise.

### Restore drill (scratch namespace)

```sh
kubectl create namespace finuance-restore
kubectl label namespace finuance-restore pod-security.kubernetes.io/enforce=restricted
# the secrets: seal copies for the scratch namespace (strict scope binds them to it)
NAMESPACE=finuance-restore SECRETS_DIR=/tmp/restore-secrets tools/seal-secrets.sh homelab db
ACCESS_KEY_ID=… ACCESS_SECRET_KEY=… NAMESPACE=finuance-restore SECRETS_DIR=/tmp/restore-secrets tools/seal-secrets.sh homelab backup
kubectl apply -n finuance-restore -f /tmp/restore-secrets/
# the cluster, from the homelab's backups, archiving into a separate folder
helm install finuance-db deploy/k8s/db -n finuance-restore -f deploy/environments/homelab/db-values.yaml \
  --set recovery.enabled=true --set recovery.serverName=finuance-db \
  --set backup.serverName=finuance-db-drill-$(date +%Y%m%d) --set cluster.instances=1
kubectl -n finuance-restore get cluster finuance-db -w     # until "Cluster in healthy state"
```

Then point a temporary app at it and run the smoke scripts:

```sh
helm install finuance deploy/helm/finuance -n finuance-restore -f deploy/environments/homelab/values.yaml \
  --set publicOrigin=http://localhost:8000 --set ingress.enabled=false --set allowSignup=true \
  --set networkPolicy.enabled=false --set api.autoscaling.enabled=false --set api.replicas=1 --set web.replicas=1
kubectl -n finuance-restore port-forward svc/finuance-api 8080:8080 &
kubectl -n finuance-restore port-forward deploy/finuance-worker 9091:9090 &
scripts/auth-smoke.sh && scripts/import-smoke.sh && scripts/query-smoke.sh
```

Sign in with a real account, too. The data is the backup's: check that the last import before the backup is there. Then clean up: `kubectl delete namespace finuance-restore` (and the drill's folder in the bucket).

Record the times in the table below: **RTO** is from `helm install finuance-db` to the smoke scripts passing; **RPO** is the time between the last write before the backup and the newest data that came back (with WAL archiving, minutes at most).

| Drill | Where | Data | RTO | RPO | Notes |
|---|---|---|---|---|---|
| 2026-10-25 | CI rehearsal (kind, 3 nodes, RustFS bucket) | smoke-test data | 75 s (restored cluster ready after 56 s) | 51 s (a write until its WAL was archived; at most `archive_timeout`, 5 min) | on-demand base backup 159 s; both markers back (before the base backup, and only in the WAL after it); `textlike` still LEAKPROOF; the restored roles took the scratch namespace's new passwords |
| — | homelab | real | *(Phase 18)* | | |

### Point-in-time recovery

Add `--set recovery.targetTime=2026-10-24T10:00:00Z` (RFC 3339; must be after the first base backup in the retention window). Use it when data was deleted by mistake: restore to just before, then copy what is needed (or switch over).

### Switch the app to a restored cluster

After a real loss: restore as above, but into `finuance` with the same cluster name `finuance-db` (delete the broken Cluster first; its volumes go with it) and a new `backup.serverName` (e.g. `finuance-db-r1`). Put `recovery.*` and the new `backup.serverName` into `db-values.yaml`, so the manual sync keeps them. The app's `database.host` (`finuance-db-rw`) stays the same. Take a new on-demand backup once it is up.

### Logical restore into another PostgreSQL

A `pg_dump` / `pg_restore` (or CNPG's `initdb.import`) into a cluster that was not made by this chart loses one setting that lives in the catalog, not in the dump. Run as a superuser in the `finuance` database:

```sql
ALTER FUNCTION pg_catalog.textlike(text, text) LEAKPROOF;
```

Without it, search still works but cannot use the trigram index under row-level security (docs/perf.md, Phase 7). The chart's bootstrap SQL already does this for new clusters.

## Playbooks

### PostgreSQL down

Symptoms: api pods not ready (`/readyz` 503), the app shows errors, `finuance-db-rw` has no endpoints.

1. `kubectl cnpg status finuance-db -n finuance` (or `kubectl -n finuance get cluster finuance-db -o yaml`): which instance is primary, and is a failover running?
2. With two instances, CNPG promotes the replica on its own (usually under a minute). The api reconnects by itself; no restart needed.
3. Both instances down: `kubectl -n finuance get pods -l cnpg.io/cluster=finuance-db`, `kubectl -n finuance describe pod …` (scheduling, volume, image pull), `kubectl -n finuance logs <pod> -c postgres`.
4. A volume that is lost or broken beyond repair: [restore](#switch-the-app-to-a-restored-cluster).

### Disk full

- **Database volume**: PostgreSQL stops accepting writes. The most common cause is **WAL piling up because archiving fails** (bucket unreachable, wrong keys). Check `kubectl cnpg status` (WAL archiving / last failed WAL) and the logs of the instance pod's plugin sidecar (`kubectl -n finuance logs <instance> --all-containers`). Fix the store or the keys; archiving catches up and PostgreSQL removes the archived WAL. If the volume can't grow (`local-path`), restore into a larger cluster.
- **Node disk** (images, logs, local-path volumes): `kubectl describe node` (DiskPressure); prune unused images on the node (`crictl rmi --prune` on k3s).
- **Bucket full**: lower `objectStore.retention` only after checking that the remaining window is enough; better, add space.

### api not ready / crash-looping

- `kubectl -n finuance describe pod -l app.kubernetes.io/component=api` and `kubectl -n finuance logs deploy/finuance-api`. Configuration problems are listed all at once at start-up (for example `FINUANCE_PUBLIC_ORIGIN`).
- `/readyz` 503 with "migrations": the migrations Job didn't run or failed ([Failed migration](#failed-migration)).
- Password errors after a rotation: restart the pods ([Rotate the database passwords](#rotate-the-database-passwords)).

### Imports stuck in "committing"

The worker runs commits: `kubectl -n finuance logs deploy/finuance-worker`, and the `finuance_jobs_total` / `finuance_imports_total` metrics on its port 9090. A failed job is retried, then the import is marked failed and can be retried from the UI (*Try again*).

## PostgreSQL major upgrade

CloudNativePG (≥ 1.26) upgrades in place when `cluster.imageName` moves to a newer major (for example `…/postgresql:19.x-standard-trixie`). It runs `pg_upgrade` offline: **the database is down for the duration** (minutes at this size).

1. Read the CNPG release notes for the target major. Check the extensions (`pg_trgm`, `citext`) and the app's tests against it: bump `postgres:` in `deploy/compose/compose.yaml` and run `make test-integration` and the e2e suite.
2. Take an [on-demand backup](#on-demand-backup) and note the time.
3. Change `cluster.imageName` and `backup.serverName` (a new major starts a new backup chain), merge, sync `finuance-db` by hand.
4. Watch `kubectl cnpg status`; the api is unready during the upgrade and comes back by itself.
5. Check `SELECT proleakproof FROM pg_proc WHERE proname = 'textlike'` is still `t`, run the smoke scripts, take a new backup.

If the in-place upgrade fails, CNPG keeps the old data directory and the cluster can be rolled back to the old image. Otherwise, restore the pre-upgrade backup into a new cluster with the old image.

## Upgrading the operators

The CloudNativePG, Barman Cloud plugin and Sealed Secrets charts are pinned in `deploy/k8s/argocd/apps/`. Renovate proposes bumps. Read the release notes, and for CloudNativePG check that the plugin version supports the operator version. Merging a bump upgrades them through Argo CD. The database pods restart with the new instance manager, one at a time (`primaryUpdateStrategy: unsupervised`), with a switchover for the primary.
