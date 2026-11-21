# Lab environment

A lab for the traffic simulator on Kubernetes (PLAN 25.9, ADR 0043): a second Finuance instance with
synthetic data only, its mock model, and the simulator in a namespace of its own. The homelab does
not run one (the maintainer, 2026-11-21); the CI rehearsal installs it (`tools/rehearsal.sh lab`), and
Phase 27's cloud cluster is meant to. **Never point the simulator at an instance with real data**:
it refuses one whose `/meta` does not report `simulation: true`, and such an instance refuses its
users anyway.

| File | Chart | Namespace |
|---|---|---|
| [`db-values.yaml`](db-values.yaml) | the database ([`deploy/k8s/db`](../../k8s/db)): one small instance, its clients with the personas' job | `finuance-lab` |
| [`app-values.yaml`](app-values.yaml) | the app ([`deploy/helm/finuance`](../../helm/finuance)): `simulation`, sign-up off, the assistant on the mock | `finuance-lab` |
| [`mock-values.yaml`](mock-values.yaml) | the simulator's chart ([`deploy/helm/finuance-sim`](../../helm/finuance-sim)) with the mock model only | `finuance-lab` |
| [`sim-values.yaml`](sim-values.yaml) | the simulator's chart with the simulator only | `finuance-sim` |

What a cluster adds (an overlay, as for the homelab, ADR 0023):

- **the lab's host name and certificate**: `publicOrigin`, `ingress.host` and the simulator's `target`;
- **two Secrets**:
  - the seed, at least 32 bytes, in both namespaces (`finuance-sim-seed`, key `seed`), so that the personas' job and the simulator derive the same passwords;
  - the control token (`finuance-sim-control`, key `token`) in `finuance-sim`;
- **the simulator's egress** to the cluster's ingress controller, and the release's images;
- for the dashboards and rules of [`deploy/k8s/observability`](../../k8s/observability) on the lab's namespace, `slo.includeSynthetic: true`: on a lab the objectives count the simulator's traffic.

These files deploy by the release's tag; a cluster's overlay pins the digests, as the homelab's values do.

The admission policy ([`deploy/k8s/admission`](../../k8s/admission)) admits the mock only in
`finuance-lab` and the simulator only in `finuance-sim`, never in `finuance`.

Running it: `make sim-up` is the same lab on Docker Compose. On a cluster:

```sh
helm upgrade --install finuance-db deploy/k8s/db -n finuance-lab -f deploy/environments/lab/db-values.yaml
helm upgrade --install finuance deploy/helm/finuance -n finuance-lab -f deploy/environments/lab/app-values.yaml -f <overlay>
helm upgrade --install finuance-lab-mock deploy/helm/finuance-sim -n finuance-lab -f deploy/environments/lab/mock-values.yaml
helm upgrade --install finuance-sim deploy/helm/finuance-sim -n finuance-sim -f deploy/environments/lab/sim-values.yaml -f <overlay>
kubectl -n finuance-sim port-forward svc/finuance-sim 8090   # the tuning page, with the control token
```
