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
| A network with one subnet, a router and Cloud NAT (the nodes' way out for image pulls) | `deploy/terraform/network.tf` | NAT by the hour and by traffic |
| A private DNS zone that makes `*.googleapis.com` answer with Private Google Access's four addresses | `network.tf` | a small monthly fee per zone |
| One GKE Autopilot cluster: private nodes, the control plane through its DNS endpoint with IAM | `cluster.tf` | pods by what they request; the cluster fee |
| Two identities without keys: the nodes' (minimum role) and the lab's (Vertex AI user), the second bound to the instance's Kubernetes service account | `identity.tf` | nothing |
| One global address, a Google-managed certificate, a Cloud Armor policy that admits your addresses only | `edge.tf` | the address, the load balancer's rule, the policy |
| Two secrets in Secret Manager (the simulator's seed and control token), their values never in git or the state | `secrets.tf` | cents |
| Vertex AI requests when an animal asks the cloud model | the lab's values | per token |

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

## Up and down

```sh
export GCP_STATE_BUCKET=<your state bucket>
make gcp-plan            # what would be created; changes nothing
make gcp-up              # asks for the project id, then creates it
# point the host's A record at the address it prints; the certificate is issued once it resolves
# … install the lab on the cluster (below), use it …
make gcp-down            # asks for the project id, then destroys it
make gcp-verify-empty    # lists anything of the lab that is left; fails if something is
```

`make gcp-up` and `make gcp-down` refuse to run without the variables file, with the example's
project in it, without the state bucket's name, or when the project id typed (or `CONFIRM=`)
doesn't match.

## Installing the lab on the cluster

`deploy/environments/gke-lab/` holds the charts' values for GKE, as layers on the lab's own
(`deploy/environments/lab/`); its README says what an overlay or Terraform's outputs add
(`tools/gcp-lab.sh outputs`). The order is the homelab's (RUNBOOK "Bootstrap"): CloudNativePG,
the database, the app with its Gateway routes, the mock, the simulator.

## What is checked without Google

- `make check-terraform` (CI's `helm` job): format, `terraform validate` against the locked
  providers, `terraform test` on a plan with made-up providers (Autopilot, private nodes, no
  address endpoint, the EU region, one role per identity, the policy's two rules, the certificate's
  one name, the refusals of an open or empty allowlist), no service-account key anywhere, no
  variables or state file committed, and Trivy's scan with nothing of HIGH or above.
- `tools/check-helm.sh`: the GKE lab renders one `HTTPRoute` and no Ingress, both backends have
  Google's health check and security policy, the network policy admits the load balancer's
  ranges only, the api's egress is Google's API range and the metadata server alone.
- The Vertex AI connection itself against a mock of Vertex AI (ADR 0045's tests).

## The first real run: a checklist

- [ ] `make gcp-plan` shows only resources named after the lab, in the region you chose.
- [ ] `make gcp-up` finishes; note how long it took.
- [ ] The DNS record is set and the certificate becomes active.
- [ ] The lab answers from an allowed address and is refused (`403`) from another.
- [ ] A pod of the lab can't reach the internet; the api reaches Vertex AI.
- [ ] One animal asks the Vertex connection and gets an answer; no key exists
      (`gcloud iam service-accounts keys list` for both identities shows none of yours).
- [ ] The Vertex AI evals of the Backlog (26.7) run from here.
- [ ] `make gcp-down`, then `make gcp-verify-empty` finds nothing.
- [ ] The billing report's figure for the session goes into "What it creates" above, and the
      "not yet run" notes come out of this file, the README and `docs/assistant.md`.

Known to need a look on that run (written from Google's documentation, not from a cluster):
the Gateway object and its certificate map annotation, the proxy-only subnet if a regional
gateway is chosen instead of the global one, the Secret Manager add-on's way of mounting the two
secrets, and whether the policy controller runs on Autopilot.
