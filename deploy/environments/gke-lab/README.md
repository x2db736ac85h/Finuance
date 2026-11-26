# The lab on Google Kubernetes Engine

The lab of [`../lab`](../lab) on a Google Cloud cluster (PLAN Phase 27): a Finuance instance with
synthetic data only, its mock model, the simulator, and a second assistant connection to Vertex
AI. **Prepared, not yet run**: these files render and are checked in CI (`tools/check-helm.sh`),
but nobody has installed them on a real cluster. The first run is the maintainer's, on demand;
`docs/gcp.md` has its checklist.

They are layers on the lab's own values, never a copy of them:

| File | On top of | What it changes |
|---|---|---|
| [`app-values.yaml`](app-values.yaml) | [`../lab/app-values.yaml`](../lab/app-values.yaml) | a Gateway in place of the Ingress, Google's load balancer admitted by address range, the pods' identity for Vertex AI, the Vertex connection and its egress |
| [`db-values.yaml`](db-values.yaml) | [`../lab/db-values.yaml`](../lab/db-values.yaml) | requests equal to limits (Autopilot bills requests), the storage class |

The mock and the simulator use [`../lab/mock-values.yaml`](../lab/mock-values.yaml) and
[`../lab/sim-values.yaml`](../lab/sim-values.yaml) as they are.

What the environment's private overlay or Terraform's outputs add (nothing here names a project):

- the project in the service account's annotation and in the Vertex connection (`<project>`);
- the lab's host name (`publicOrigin`, `gateway.host`, the simulator's `target`);
- the proxy-only subnet's range in `networkPolicy.frontDoorCIDRs` when the gateway is regional;
- the image digests, as for every environment (ADR 0024).

What is different from the homelab, and why:

- **No ingress controller in the cluster.** Google's load balancer is the front door and reaches
  the pods directly, so the network policy admits its address ranges
  (`networkPolicy.frontDoorCIDRs`) and selects no controller pods (`ingressController: null`).
- **Limits per client address** are Cloud Armor's (the security policy named in
  `gateway.gke.securityPolicy`), not Traefik middlewares.
- **No stored Google key.** The pods' Kubernetes service account is bound to a Google service
  account by workload identity; the assistant's Vertex connection asks the metadata server for a
  short-lived token (ADR 0045). The api may reach Google's API range and the metadata server, and
  nothing else outside the cluster.
- **Synthetic data only**, as on every lab: the simulator refuses an instance that is not a lab.
