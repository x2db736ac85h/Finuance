# 0039. Image signatures at admission: Sigstore policy-controller, images by digest

- Status: accepted
- Date: 2026-11-19

## Context

ADR 0024 put a registry list in front of every pod (a built-in `ValidatingAdmissionPolicy`) and
left two things for later: Finuance's images were deployed by tag, and nothing in the cluster
checked who had built them. A tag can be moved, and a registry we allow could serve other bytes
under it. Since Phase 22 every release is signed with keyless cosign by `release.yml` on a version
tag, carries build provenance, and needs the maintainer's approval to run (ADR 0037); the verify
commands in `SECURITY.md` name that identity. The cluster should refuse what those commands refuse.

Checked on 2026-11-19: policy-controller's chart 0.10.8 (application v0.13.1) pins its own image
by digest, asks for 100m CPU and 128 MiB, fails closed by default, looks only at namespaces
labelled `policy.sigstore.dev/include: "true"`, and refuses an image no policy names. Its uninstall
job uses a moving tag. It can also require an attestation (the SLSA provenance), in the newer
bundle format only for attestations.

## Decision

- **By digest:** the chart takes `image.digests.api` and `image.digests.web` and then renders
  `name@sha256:…`; the release workflow writes the digests it pushed and signed into the
  environment's values together with the version, and `tools/check-pins.sh` requires them there.
- **Signatures:** Sigstore policy-controller, installed by the Argo CD application
  `policy-controller` (manual sync), with two `ClusterImagePolicy` objects:
  - `ghcr.io/x2db736ac85h/**` needs a keyless signature whose identity is `release.yml` on a
    version tag (issuer GitHub Actions), checked against the public transparency log;
  - `ghcr.io/cloudnative-pg/**` passes without a signature check (a static authority), because the
    database's images run in the same namespaces and the controller would refuse them otherwise.
- **Order of checks:** the registry list of ADR 0024 stays the first check and also covers the
  controller's own namespace; the signature is the second.
- **Fail closed:** the webhook's failure policy is `Fail` and an image no policy names is refused.
  The switch is the namespace label; removing it is the documented way back (RUNBOOK "Image
  signatures").
- **Proof:** the rehearsal installs the controller with the same values and policies and checks
  that the released images are admitted by digest, and that an image without the release workflow's
  signature, the unsigned local build and an image no policy names are refused.

## Consequences (positive, negative, follow-ups)

- What runs in Finuance's namespaces is what a tagged release built: a copy pushed by someone with
  write access to the registry, or an image built by a branch, does not start.
- New pods in a labelled namespace need the registry and Sigstore's services to be reachable, and
  the webhook's pod to be up. On one machine that is a new way for a restart to wait: running pods
  are not affected, and the label comes off with one command.
- The rehearsal can't push an unsigned copy into Finuance's registry, so its refusal test holds
  another public image to the same signer with a test-only policy.
- CloudNativePG's images are trusted by registry, not by signature. Checking their signatures
  would tie the database to the layout of their release workflow; revisit if they publish a
  stable identity to pin.
- The provenance attestation is not required at admission yet: the signature already names the
  workflow and the tag. Worth adding when the controller verifies bundle-format signatures too.
- Follow-ups: the label and the namespaces in git, with the application moved before the
  admission policy's wave; the demo host (Phase 29) installs the same.

## Alternatives considered

- **Kyverno:** verifies signatures too and does much more; a larger component with its own policy
  language for one rule.
- **Verify in the registry list's `ValidatingAdmissionPolicy`:** CEL can't check a signature.
- **Digests alone:** pin the bytes, but a wrong digest written into the values would run; the
  signature says who built them.
- **Warn first, then enforce:** the controller's `warn` mode. The namespace label already gives a
  step-by-step rollout, and a scratch namespace shows the refusals without touching the app.
