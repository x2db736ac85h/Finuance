# 0037. Supply chain: pinned references, checked tools, scans on a schedule, attested releases

- Status: accepted
- Date: 2026-11-19

## Context

The repository became public in November 2026 (Phase 21). The audit of 2026-11-16 (PLAN Rev 5,
Phase 22) found that an advisory published after the last push went unnoticed for a week, that
actions and several tool images were referenced by tags that can be repointed, that workflow tokens
had more rights than their jobs used, and that a release was signed but nothing said which commit
and workflow had built it, nor who allowed it to run. CI used a Trivy container with the Docker
socket mounted, and several scanners planned for the public release were missing.

## Decision

- **Everything the repository runs but does not build is pinned to content**: actions by their
  40-character commit SHA (the version in a comment), images by digest. `tools/check-pins.sh` in
  `make check` refuses anything else; the images the repository builds itself are the exception
  until Phase 23 deploys them by digest. The repository setting "require actions pinned to a
  full-length commit SHA" and an allowlist of actions (GitHub's own and `ossf/scorecard-action`)
  enforce the same on GitHub.
- **Scanners are checked binaries**: Trivy, osv-scanner, actionlint and zizmor come from
  `tools/bin-tools.sh` at a pinned version and checksum, the same way locally and in CI. Trivy reads
  images from `docker save` tarballs or the registry, never through the Docker socket.
- **Workflows get least privilege**: read-only by default, write permissions per job, values reach
  scripts through `env:` and never as `${{ }}` inside a script, checkouts keep no token unless the
  job pushes. actionlint (with shellcheck) and zizmor gate every push.
- **Findings are looked for continuously**: CodeQL (Go, JavaScript/TypeScript, the workflows,
  Python), Trivy with every severity in code scanning, osv-scanner, a dependency review with a
  licence allowlist on pull requests, OpenSSF Scorecard weekly, and a **daily scan of the released
  images** that opens and closes one issue labelled `security-scan`. Every open alert is fixed or
  dismissed with a reason that `docs/security-scan.md` repeats; `.trivyignore` holds only findings
  without a fix, with an expiry the daily scan warns about a week ahead.
- **Updates come from Renovate** (the hosted app), which sees every kind of pin, including tool
  versions in workflows and scripts marked with `# renovate:` comments; security updates come at
  once.
- **Releases are approved and attested**: tags `v*` can only be created by the maintainer (a tag
  ruleset), the release job runs in the `release` environment with the maintainer as required
  reviewer, and it attaches SLSA build provenance next to the SBOM and the cosign signature. The
  signer identity in every verify command is anchored to `release.yml` and a version tag.

## Consequences (positive, negative, follow-ups)

- An advisory against a released image is noticed within a day; a repointed tag can no longer
  change what CI or the cluster runs; a stolen push credential alone can't publish a release.
- More moving parts: a version bump of a checked binary needs its checksums updated by hand (CI
  fails until they are), and every release waits for the maintainer's approval.
- One maintainer can't approve their own pull requests, so Scorecard's code-review and
  branch-protection checks stay low by design.
- Phase 23: the cluster admits only signed images and deploys them by digest (ADR 0024).

## Alternatives considered

- **Dependabot instead of Renovate**: it doesn't see versions in workflow `env:` blocks, scripts or
  Argo CD applications.
- **Third-party scanner actions** (for Trivy, actionlint): another dependency to pin and trust; the
  binaries checked by checksum do the same job.
- **A self-hosted Renovate workflow**: a token to keep and Actions minutes to spend; the hosted app
  needs neither.
