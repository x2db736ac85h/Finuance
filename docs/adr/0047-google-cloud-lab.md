# 0047. The Google Cloud lab: Terraform, GKE Autopilot, prepared before it is run

- Status: accepted
- Date: 2026-11-26

## Context

Phase 27 asks for a lab on Google Cloud that is made and removed on demand, with synthetic data
only (the decision of 2026-11-16). On 2026-11-26 the maintainer added that the Google account
and its payment are not arranged yet: the work is to prepare everything that can be prepared and
proven without an account, and the phase may close without a real rollout.

## Decision

- **Terraform, hand-written resources, one configuration** in `deploy/terraform/`: the provider
  is pinned exactly with its lock file committed, the state lives in a bucket the maintainer
  makes once, and no value of a real project is in the repository (variables without defaults,
  a variables file that git ignores). It is written so that OpenTofu runs it too.
- **GKE Autopilot**, one regional cluster in the EU, private nodes, the control plane through its
  DNS endpoint with IAM (the address endpoint off, the authorized-networks list on and empty).
- **Identities without keys.** The lab's identity has `roles/aiplatform.user` and is bound to
  the instance's Kubernetes service account by workload identity; the nodes have an identity of
  their own with the node's minimum role. A check fails if a service-account key is ever created.
- **The front door is a Gateway** with a Google-managed certificate and a Cloud Armor policy that
  admits the maintainer's address ranges and refuses everyone else; an allowlist of "everything"
  or of nothing is refused by the configuration itself.
- **Google's APIs by one small range**: a private DNS zone makes `*.googleapis.com` resolve to
  Private Google Access's four addresses, so the api's NetworkPolicy allows that range and the
  metadata server, and nothing else outside the cluster.
- **Secrets are made by Terraform and never stored by it**: ephemeral values written through
  write-only arguments to Secret Manager, so they are in neither git nor the state.
- **The database is CloudNativePG in the cluster**, as everywhere; no Cloud SQL (the bootstrap
  needs a superuser statement Cloud SQL doesn't allow, and one more thing would differ).
- **It runs behind three locks**: a variables file that must exist and must not be the example,
  the state bucket's name from the environment, and the project id typed before anything is
  created or destroyed. `make gcp-verify-empty` lists what is left afterwards.
- **What "prepared" means**: valid against the provider's real schemas, tested on a mocked plan,
  scanned, rendered and validated for the charts. Not applied. `docs/gcp.md` says so at its top
  and carries the checklist for the first real run.

## Consequences (positive, negative, follow-ups)

- The first apply will find what only a real project shows; the configuration is small enough to
  correct in that session, and the checklist names the places most likely to need it.
- CI checks the Terraform without credentials, so a pull request from anyone gets the same checks.
- Terraform and the provider are two more pinned versions to keep current (Renovate sees the
  tool's version in `tools/bin-tools.sh`; the provider's is a follow-up).
- The chart's Gateway support is useful beyond Google: any cluster without Traefik.

## Alternatives considered

- **A large community module for the cluster**: it changes its major version several times a
  year and hides the settings this lab cares about.
- **Standard GKE with node pools**: more to get right (node hardening, workload identity, the
  dataplane) for no gain in a lab; Autopilot bills by what pods ask for.
- **Cloud SQL**: ruled out above.
- **Waiting with all of it until an account exists**: the chart work and the checks are the
  larger part and need none.

## Addendum (2026-11-26, the lab's installation)

Decided while preparing the install, still without a Google account:

- **Argo CD per environment, in folders.** The lab's applications live in
  `deploy/k8s/argocd/gke-lab/` with a root of their own; the homelab's stay in
  `deploy/k8s/argocd/apps/`. Moving the homelab's into an `envs/` folder, as PLAN 27.2 suggested,
  would leave its root application (applied by hand in Phase 18) pointing at an empty path, and
  with pruning on that removes every application; an ApplicationSet would need its controller,
  which the homelab runs at zero. `tools/check-helm.sh` keeps the two folders apart (the lab names
  no homelab file) and their shared charts at one version.
- **Secrets are made at install, not synced.** `make gcp-install` makes the database's two
  passwords in the cluster from random values on standard input and copies the simulator's two
  from Secret Manager with the operator's own login. No Secret Manager add-on and no External
  Secrets Operator: four values for a cluster that lives hours don't need a controller, and nothing
  new is installed. Sealed Secrets don't fit a cluster that gets a new key each time.
- **Cloud NAT on one reserved address, admitted by Cloud Armor.** The simulator reaches the lab
  through its front door as a browser does (PLAN's decision), so its requests must pass Cloud Armor;
  an address picked by NAT at random can't be listed. Cloud Armor's second rule admits exactly that
  address.
- **The repository is read over https.** It is public, so the lab's Argo CD needs no key for it;
  the private overlay is read over SSH with a read-only deploy key that exists for the lab only.
- **`make gcp-down` cleans up through Kubernetes first.** Google makes the Gateway's load balancer
  and the volumes' disks outside Terraform and removes them when their Kubernetes objects go;
  destroying the cluster first would leave both behind and billed. `make gcp-verify-empty` looks
  for the lab's name inside Google's generated names and in disks' descriptions as well.
- **Backups are prepared, not switched on.** `backups = true` makes a bucket and the database's
  identity, and the database chart can write to Cloud Storage by workload identity; the lab's
  applications don't include the Barman Cloud plugin and cert-manager yet, so the restore drill
  (PLAN 27.9's last check) waits for a first run that wants it. Monitoring likewise.
