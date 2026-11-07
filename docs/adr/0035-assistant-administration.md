# 0035. Administration → Assistant: connections in the database, sealed API keys, per-model tuning

- Status: accepted
- Date: 2026-11-06

## Context

Since Phase 13 the assistant's model servers are instance configuration: a connections file (or
one `FINUANCE_AI_BASE_URL`) read at start, with API keys from environment variables (ADR 0015).
Adding a server, or trying another model's settings, meant editing a file and restarting. Phase 20
moves this into the admin panel without giving up what the file guarantees: local servers only on
private addresses, cloud services only with each user's consent, keys that never reach a browser.
Administrators also need to see how much the assistant is used per person, never what was asked
(ADR 0025: administrators see counts, never financial data).

## Decision

- **Two sources of connections.** The connections file stays the starting point and is shown read
  only in Administration → Assistant (changed only in the file). Administrators add further
  connections in the panel; they are stored in `assistant_connections`, an instance table outside
  RLS (migration 00022), and may not take a file connection's name. The same rules as the file
  apply (`config.NewConnection`): http(s) without credentials, a local connection must not be (or
  resolve to) a public address, a cloud connection must use https and still needs each user's
  consent; the llm dial guard checks every connection on every dial as before.
- **API keys sealed with AES-256-GCM** under `FINUANCE_INSTANCE_KEY` (32 random bytes, base64;
  `tools/seal-secrets.sh <env> instance-key`, `assistant.instanceKeySecret` in the chart, given to
  the api and the worker). A sealed key is a version byte, a random nonce and the ciphertext, with
  the connection's name as associated data, so a value copied to another row doesn't open. Keys are
  write-only in the API: the panel shows "stored", "none" or "unreadable" (sealed under another
  instance key), never the key. Changing a connection's address or cloud setting drops a kept key,
  so a key is only ever sent where it was entered for. Without an instance key, connections without
  a key still work; a key is refused.
- **Reloading**: every api and worker process reads the stored connections at start, every minute,
  and right after a change it made itself; other replicas follow within a minute.
- **Per-model tuning** (`assistant_model_settings`, for a model of any connection): defaults for the
  temperature, the thinking level and the tool set when a question doesn't set them, the context
  size to plan with (replacing the detected one), and a note of at most 1,000 characters added
  after the instance's instructions (ADR 0034). Settings only; no model is trained.
- **Usage per user**: the assistant's runs and tokens over the last 30 days (UTC) from
  `assistant_usage` (ADR 0033), counted under each user's own RLS context like the other usage
  counts, on the user's detail; tokens per day on Administration → Usage. Never a question, an
  answer or a tool result.
- Every change is audited as `admin.assistant_changed` (the connection, what changed, whether a key
  was set; never the key).

## Consequences (positive, negative, follow-ups)

- Adding a server or trying settings needs no restart or file edit; the file still suits GitOps.
- An administrator can point a new connection at any private address the api can reach; in
  Kubernetes the NetworkPolicy egress still decides which servers are reachable at all.
- Losing `FINUANCE_INSTANCE_KEY` makes stored keys unreadable (enter them again); a database dump
  alone doesn't reveal them. Rotating the key means entering the keys again (no re-encryption tool).
- A minute's delay before another replica sees a change.

## Alternatives considered

- **Keys in Kubernetes secrets edited from the panel**: the app would need write access to the
  cluster's API.
- **Keys encrypted with PostgreSQL's pgcrypto**: a new extension, and the key would travel to the
  database with every query.
- **Moving the file's connections into the database at first start**: two sources of truth after
  the next file change; the file stays the source for its own connections.
