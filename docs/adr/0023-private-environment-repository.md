# 0023. Machine-specific values live in a private environment repository

- Status: accepted
- Date: 2026-10-30

## Context

Finuance is meant to run on any Kubernetes homelab. Phase 18 deploys it on one particular machine
(ADR 0022), and a deployment needs values that only make sense there: the host name (which also
goes into the public Certificate Transparency logs with the TLS certificate), the machine's tailnet
address for the model server, and notes about its hardware, network and measured performance.
Those are personal and would tie the public repository to one person's machine; they also change
when the machine does. GitOps still needs them in git: Argo CD deploys what a repository says.

## Decision

Two repositories, read together by Argo CD:

- **This repository** holds the charts, the generic homelab values
  (`deploy/environments/homelab/values.yaml`, `db-values.yaml`: image tag, storage class, ingress
  controller labels, TLS mode, backup endpoint inside the cluster, …), host-file templates and the
  RUNBOOK with placeholders (`<host>`, `<tailnet-ip>`, …).
- **A private environment repository** (`finuance-env`) holds, per environment, a small overlay
  (`homelab/host-values.yaml`: `publicOrigin`, `ingress.host`, the assistant's connections file and
  its egress rule) plus the filled-in host files and notes about the machine.

The `finuance` Argo CD application uses **multiple sources**: the chart and the
generic values from this repository, then `$env/homelab/host-values.yaml` from the environment
repository (`ref: env`); later value files win (the database needs no per-machine value yet; `finuance-db` can join the same way). The `finuance` project allows both repositories.
Argo CD reads both with read-only deploy keys (18.3).

The image tag stays in this repository, so the release's `gitops-bump` pull request deploys and a
revert rolls back exactly as before. A change of machine is a change in the environment repository
only.

`deploy/environments/homelab/host-values.example.yaml` shows the overlay with placeholders;
`make helm-lint` and CI render the charts with both layers, so the layering is checked without
access to the private repository. `make check-env-details` fails when the environment's identifiers
(patterns kept in the private repository) appear here.

## Consequences (positive, negative, follow-ups)

- The public repository stays generic; nothing in it names the machine, its network or its host.
- Two repositories to keep in step: a chart change that needs a new per-host value needs a change
  in the environment repository too. The example overlay and its render check make that visible.
- Argo CD needs credentials for a second repository (another sealed deploy key).
- Deploying from a fresh clone needs both repositories; the RUNBOOK says so.
- Follow-up: the Phase 21 demo host gets its own folder in the environment repository.

## Alternatives considered

- **Host values in this repository:** simplest, but publishes the host, the tailnet address and the
  machine's details, and ties every machine change to the public history.
- **Values set only in the cluster** (Argo CD parameters edited by hand): the deployed state is no
  longer in git and self-heal would fight manual edits.
- **Encrypting the overlay in this repository** (e.g. SOPS): hides the values but still ties the
  public history to the machine and adds a decryption plugin to Argo CD.

*Note (2026-11-03): the follow-up phase was renumbered from 19 to 21 when Phases 19–20 were added (PLAN Rev 4).*
