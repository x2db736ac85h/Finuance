# The Google Cloud lab

An on-demand lab on Google Cloud: a Finuance instance with **synthetic data only**, the traffic
simulator, and the assistant's connection to Vertex AI with the pods' own identity. It is made
with Terraform, costs money only while it is up, and is destroyed again (PLAN Phase 27, ADR 0047).
The instance with real data stays where it is; the public demo is a small VPS, not this.

> **Prepared, not yet run.** Everything here is written and checked without a Google account: the
> Terraform is valid against the pinned provider's schemas and has tests on a mocked plan, the
> charts render for GKE and are validated, the scripts' refusals are tested. Nobody has applied it
> to a real project yet. The first run will meet things only a real project shows (a quota, an
> organisation policy, a field Google changed); the checklist at the end is for that run.

## What it creates

| What | Where it is written | Billed while up |
|---|---|---|
| A network with one subnet, a router and Cloud NAT on one reserved address (the nodes' way out for image pulls, and the simulator's to the lab's front door) | `deploy/terraform/network.tf` | NAT by the hour and by traffic; the address |
| A private DNS zone that makes `*.googleapis.com` answer with Private Google Access's four addresses | `network.tf` | a small monthly fee per zone |
| One GKE Autopilot cluster: private nodes, the control plane through its DNS endpoint with IAM | `cluster.tf` | pods by what they request; the cluster fee |
| Two identities without keys: the nodes' (minimum role) and the lab's (Vertex AI user), the second bound to the instance's Kubernetes service account | `identity.tf` | nothing |
| One global address, a Google-managed certificate, a Cloud Armor policy that admits your addresses and the lab's NAT address only | `edge.tf` | the address, the load balancer's rule, the policy |
| The Gateway (Google's load balancer), made by the cluster from `deploy/environments/gke-lab/platform/` | Argo CD, after `make gcp-install` | the load balancer's rule and traffic |
| Two secrets in Secret Manager (the simulator's seed and control token), their values never in git or the state | `secrets.tf` | cents |
| Vertex AI requests when an animal asks the cloud model | the lab's values | per token |
| With `backups = true` only: a bucket for the database's backups and the identity that writes to it | `backups.tf` | storage, by the gigabyte |

Nothing else: no Cloud SQL (the database is CloudNativePG in the cluster, as everywhere), no
second region, nothing that runs after `make gcp-down` except the state bucket.

What a run costs is not written here: it has not been measured. Read it from the billing report
after the first run and put the figure in this section.

## Once, by hand (the maintainer)

1. **A project with billing**, and yourself as its owner. Set a **budget alert** of a few euros on
   it (Billing → Budgets & alerts): Google's budgets warn, they don't stop anything.
2. **A bucket for the state**, in the project, with versioning on and no public access. Its name
   goes into `GCP_STATE_BUCKET`, never into the repository.
3. **The tools**: `gcloud` (signed in: `gcloud auth login` and `gcloud auth application-default
   login`), `kubectl`, `helm`; Terraform comes with `tools/bin-tools.sh terraform`.
4. **A host name you control** for the lab, and the addresses you will open it from.
5. `cp deploy/terraform/example.tfvars deploy/terraform/lab.tfvars` and fill it in (the file is
   ignored by git).
6. **A read-only deploy key on `finuance-env`** (the private overlay, ADR 0023): the lab's Argo CD
   reads its `gke-lab/` folder with it. Keep the private half in a file outside both repositories;
   `make gcp-install` takes its path in `ENV_DEPLOY_KEY`. This repository is public, so it needs no key.
7. **The overlay's two files** in `finuance-env/gke-lab/`: `host-values.yaml` and `sim-values.yaml`,
   copied from the examples in `deploy/environments/gke-lab/` and filled in with Terraform's outputs
   (`make gcp-outputs`) and the release's image digests. They name the project and the host, which
   this repository never does.

## Up and down

```sh
export GCP_STATE_BUCKET=<your state bucket>
make gcp-plan            # what would be created; changes nothing
make gcp-up              # asks for the project id, then creates it
# point the host's A record at the address it prints; the certificate is issued once it resolves
make gcp-outputs         # the values for the overlay's two files (step 7 above); push them
ENV_DEPLOY_KEY=<file> make gcp-install   # Argo CD, the Secrets, the lab's root application
# … Argo CD installs the lab from main; use it …
TARGET_BASE=https://<host> TARGET_NAMESPACE=finuance-lab tools/rehearsal.sh target
make gcp-down            # asks for the project id, cleans up the cluster, then destroys it
make gcp-verify-empty    # lists anything of the lab that is left; fails if something is
```

`make gcp-up` and `make gcp-down` refuse to run without the variables file, with the example's
project in it, without the state bucket's name, or when the project id typed (or `CONFIRM=`)
doesn't match.

## Installing the lab on the cluster

`make gcp-install` (after `make gcp-up`) gets the cluster's credentials through its DNS endpoint
and then:

1. installs **Argo CD** from the homelab's chart and values (`deploy/environments/homelab/argocd-values.yaml`,
   the version `tools/check-helm.sh` keeps equal to the RUNBOOK's), and gives it the deploy key for
   the overlay;
2. makes the lab's two namespaces under restricted Pod Security, and their **Secrets**: the
   database's two role passwords, made at that moment and never shown, and the simulator's seed
   and control token, read from Secret Manager (where Terraform wrote them) with your own login.
   Every value goes to `kubectl` on standard input, never on a command line. No Sealed Secrets
   here: a cluster made again has a new sealing key every time;
3. applies the lab's **root application** (`deploy/k8s/argocd/gke-lab/root.yaml`), which installs
   the rest from `main` in this order: the projects, the image admission policy, CloudNativePG, the
   Gateway, the database, the mock model and the instance, the simulator.

The values are layers on the lab's own (`deploy/environments/lab/`); the README in
`deploy/environments/gke-lab/` says what each layer changes. The simulator reaches the lab through
its front door as a browser would: its requests leave through Cloud NAT's reserved address, which
Cloud Armor admits next to yours.

`make gcp-down` first deletes the lab's Argo CD applications and its namespaces and waits: Google
removes the Gateway's load balancer and the database's disk only when their Kubernetes objects go,
and a destroyed cluster would leave both behind, billed. Then it destroys the rest.

**Backups and the restore drill** (PLAN 27.9's last check): `backups = true` in the variables file
makes a bucket and the database's identity (`backups.tf`), and the database chart writes to Cloud
Storage with that identity (`objectStore.auth: gke`, `cluster.serviceAccountAnnotations`, no key).
The lab's applications don't switch it on yet: the Barman Cloud plugin and cert-manager, which it
needs, are not among them. A lab is made again, not restored.

**Monitoring** is not installed on the lab yet: the homelab's stack takes its alert mail and Grafana
login from the private repository, and Autopilot forbids what node-exporter needs (host access).
Google's own Cloud Monitoring shows the cluster meanwhile.

## What is checked without Google

- `make check-terraform` (CI's `helm` job): format, `terraform validate` against the locked
  providers, `terraform test` on a plan with made-up providers (Autopilot, private nodes, no
  address endpoint, the EU region, one role per identity, the policy's two rules, the certificate's
  one name, the refusals of an open or empty allowlist), no service-account key anywhere, no
  variables or state file committed, and Trivy's scan with nothing of HIGH or above.
- `tools/check-helm.sh`: the GKE lab renders one `HTTPRoute` and no Ingress, both backends have
  Google's health check and security policy, the network policy admits the load balancer's
  ranges only, the api's egress is Google's API range and the metadata server alone; the route
  attaches to the lab's Gateway, which is Google's HTTPS load balancer with Terraform's address and
  certificate map and admits routes from the lab's namespace alone; with the overlay's examples
  every image is deployed by digest and the simulator may reach the front door's address only; the
  lab's Argo CD applications name the lab's files only, every values file they name exists, and
  their charts are at the homelab's versions.
- `tools/gcp-lab-test.sh` (in `make check-terraform`): each lock of `tools/gcp-lab.sh` refuses
  (no variables file, the example's project, no confirmation or the wrong project id, no state
  bucket, no deploy key, no `gcloud`), with no Google account.
- The rehearsal on kind (`tools/rehearsal.sh`, every deploy push): the chart's Gateway route with
  Traefik as the implementation, a Gateway laid out as here, the smoke scripts, the streamed answer
  and the HSTS header through it; no pod of the app reaches an address outside the cluster.
- The Vertex AI connection itself against a mock of Vertex AI (ADR 0045's tests).

## The first real run: a checklist

- [ ] `make gcp-plan` shows only resources named after the lab, in the region you chose.
- [ ] `make gcp-up` finishes; note how long it took.
- [ ] The DNS record is set and the certificate becomes active.
- [ ] `make gcp-install` finishes and every application in `kubectl -n argocd get applications`
      becomes Synced and Healthy.
- [ ] The lab answers from an allowed address and is refused (`403`) from another.
- [ ] `tools/rehearsal.sh target` passes: the smoke scripts, the streamed answer, no pod of the app
      reaching the internet, no failed request while pods are deleted and rolled.
- [ ] The api reaches Vertex AI.
- [ ] One animal asks the Vertex connection and gets an answer; no key exists
      (`gcloud iam service-accounts keys list` for both identities shows none of yours).
- [ ] The Vertex AI evals of the Backlog (26.7) run from here.
- [ ] `make gcp-down`, then `make gcp-verify-empty` finds nothing.
- [ ] The billing report's figure for the session goes into "What it creates" above, and the
      "not yet run" notes come out of this file, the README and `docs/assistant.md`.

Known to need a look on that run (written from Google's documentation, not from a cluster):
the Gateway object and its certificate map annotation, the proxy-only subnet if a regional
gateway is chosen instead of the global one, whether the load balancer's health checks reach the
pods through the network policy's two ranges, Argo CD on Autopilot (resource requests it adds),
whether the policy controller runs on Autopilot (it is not installed on the lab yet), and the
simulator's way in through Cloud NAT and Cloud Armor.
