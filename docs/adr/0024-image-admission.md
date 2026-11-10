# 0024. Image admission on the cluster: registries now, signatures in Phase 21

- Status: accepted
- Date: 2026-11-02
- Note (2026-11-03): the follow-up phase was renumbered from 19 to 21 when Phases 19–20 were added (PLAN Rev 4).

## Context

Trivy's KSV-0125 ("image not from an allow-listed registry") has been listed as deliberate since
Phase 17, waiting for "an admission policy that allows only our registry and verifies the
signatures" (docs/security-scan.md, THREAT-MODEL E3). Releases already sign Finuance's images with
keyless cosign and attach SBOMs (Phase 14), but nothing in the cluster checks what it runs: a
mistyped image, a compromised chart value or a copy-paste from elsewhere would start.

What runs on the reference cluster (Phase 18.8, checked on the cluster):

- Finuance's api and web images and CloudNativePG's PostgreSQL images are deployed **by tag**
  (`finuance-api:0.2.0-rc.2`, `postgresql:18.6-standard-trixie`); the operator adds its own images
  to the database pods.
- The monitoring stack's charts (kube-prometheus-stack) render tags from several registries
  (quay.io, registry.k8s.io, Docker Hub, ghcr.io).
- Only our own manifests pin by digest: RustFS, the AWS CLI bucket job, Tempo.

So "only digest-pinned images" would block nearly every pod today, and signature verification needs
an extra controller (Sigstore policy-controller or Kyverno) running in the cluster.

## Decision

Now (Phase 18): a **built-in `ValidatingAdmissionPolicy`** (`deploy/k8s/admission/image-policy.yaml`,
Argo CD app `admission-policy`, GA in Kubernetes 1.30; no extra component). It checks every pod
created or updated in `finuance`, `finuance-restore`, `s3` and `monitoring`:

- every image comes from a registry prefix listed for that namespace (Finuance's own only from
  `ghcr.io/x2db736ac85h/`, CloudNativePG's from `ghcr.io/cloudnative-pg/`, the monitoring stack's
  from the registries it uses);
- the images we pin ourselves (RustFS, the AWS CLI, Tempo) carry their digest.

Phase 21: **cosign signature verification** for Finuance's images (keyless, the release workflow's
identity) in a policy controller, and Finuance's images **deployed by digest** (chart values, and the
`gitops-bump` pull request writing the digests the release signed).

Decided by the maintainer on 2026-11-02 (registries now, digests and signatures in Phase 21).

## Consequences (positive, negative, follow-ups)

- A pod with an image from anywhere else is refused at admission, with a message naming the image
  (a `FailedCreate` event on its ReplicaSet, Job or database cluster). Checked with server-side dry
  runs (RUNBOOK step 15).
- Nothing new to run or upgrade: the API server evaluates the CEL rules.
- A chart upgrade that brings an image from a new registry is refused until its prefix is added to
  the policy. That is intended, and the RUNBOOK says so; Renovate's chart bumps should be checked
  against the list.
- Tags can still move: a registry we allow could serve different bytes under the same tag. That is
  what Phase 21's digests and signatures close.
- Trivy keeps reporting KSV-0125 for the manifests (it can't see the cluster's policy); the findings
  stay listed with a reference here.

## Alternatives considered

- **Digests only, now:** blocks Finuance, CloudNativePG and the monitoring stack until every chart
  deploys by digest; most of that is the third-party charts' choice, not ours.
- **Kyverno or Sigstore policy-controller now:** verifies signatures, but adds a controller with
  cluster-wide webhooks to a single test machine; better placed with the public demo host
  (Phase 21), where it matters most.
- **No policy, keep KSV-0125 as deliberate:** leaves the gap the threat model names open.
