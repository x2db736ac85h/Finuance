# Security policy

Finuance stores people's financial history, so security reports are very welcome.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub instead: on the repository's **Security** tab, choose **Report a vulnerability** ([private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)).

Include what you found, how to reproduce it, and what an attacker could do with it. Never include real bank data: synthetic data or a description of the file's layout is enough.

## What you can expect

- An acknowledgement within a week.
- An assessment within two weeks: whether it is a vulnerability, how severe, and which releases it affects.
- A fix in the next release, sooner for anything serious, published together with a GitHub security advisory that credits you unless you'd rather not be named.
- Updates in the private report as it moves along. Please keep the details private until the advisory is published.

## Supported versions

Finuance is pre-1.0 software. Security fixes land on `main` and ship in the next release; **only the latest release is supported** (today the 0.5 line), so please check that a problem still exists there. Release candidates (`-rc.N`) are not supported once their final release is out.

| Version | Supported |
|---|---|
| 0.5.x | yes |
| 0.4.x and older | no: upgrade to 0.5 |

Upgrading to 0.5 signs everyone out once and runs two migrations; the [changelog](CHANGELOG.md) names what an operator does around it (the database chart before the app chart on Kubernetes, one statement on an existing database).

## Verifying a release

Every release's images are signed with [cosign](https://github.com/sigstore/cosign) (keyless, by the release workflow) and carry an SPDX SBOM and SLSA build provenance. Each release's notes list the image digests and these two commands for them:

```sh
# the signature: made by .github/workflows/release.yml for a version tag, and by nothing else
cosign verify ghcr.io/x2db736ac85h/finuance-api@sha256:<digest> \
  --certificate-identity-regexp '^https://github\.com/x2db736ac85h/Finuance/\.github/workflows/release\.yml@refs/tags/v[0-9]' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# the build provenance: which commit and workflow built the image (from v0.4.2)
gh attestation verify oci://ghcr.io/x2db736ac85h/finuance-api@sha256:<digest> \
  --repo x2db736ac85h/Finuance --signer-workflow x2db736ac85h/Finuance/.github/workflows/release.yml
```

The same for `finuance-web`. The provenance bundles are attached to each release as well (`finuance-api.intoto.jsonl`, `finuance-web.intoto.jsonl`). The `fin` command line comes with a checksum file, signed the same way:

```sh
cosign verify-blob --bundle fin_<version>_checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/x2db736ac85h/Finuance/\.github/workflows/release\.yml@refs/tags/v[0-9]' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com fin_<version>_checksums.txt
```

On Kubernetes the cluster can make the same check before a pod starts: deploy by digest (`image.digests`) and let [Sigstore policy-controller](https://docs.sigstore.dev/policy-controller/overview/) admit only images that carry this workflow's build provenance. The policy used by the reference environment is in [`deploy/k8s/policy-controller`](deploy/k8s/policy-controller), the rollout in [docs/RUNBOOK.md](docs/RUNBOOK.md) ("Image signatures"), the decision in [ADR 0039](docs/adr/0039-image-signatures.md).

## Protecting your account

Since 0.5 an account can ask for a second step at sign-in: a six-digit code from an authenticator app (Settings → Security), with recovery codes for a lost phone. An instance can require it of its administrators. [ADR 0040](docs/adr/0040-two-step-sign-in.md) says how it is stored and what an administrator can and cannot do about it.

## Scope and status

The protections in place today, the risks accepted on purpose, and what is still planned are listed in the [threat model](docs/THREAT-MODEL.md); the scans, their findings and every pinned version are in [docs/security-scan.md](docs/security-scan.md). A scan of the released images runs every day; a new finding opens an issue labelled `security-scan`.
