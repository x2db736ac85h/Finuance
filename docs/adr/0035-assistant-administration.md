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

## Addendum (2026-11-08, Phase 20 follow-ups 20.17–20.20): controls, model loading, who picks

Our Product Engineer asked for a way to switch the homelab's Ollama connection off, unload and load
models, unload a model by itself after an idle time, see what *Test* found, and decide whether
people may pick the model.

- **Controls per connection** (migration 00024, `assistant_connection_controls`, an instance table
  outside RLS like the connections): on or off, how long the server keeps a model after the last
  question (10, 15, 30 or 45 minutes, 1, 2 or 4 hours, or the server's own setting), the model
  questions use (over the connection's own default) and a **model for photos**. They apply to the
  connections file's connections too, whose own fields stay read only. A connection without a row is
  on with 30 minutes. They reload with the connections every minute.
- **Switched off**: the connection is left out of people's status and refused for questions; its
  chats and digests go to the first connection that is on, and its loaded models are unloaded
  (Ollama). Administration still checks it.
- **Unloading after idle time uses the server's own timer**: Ollama keeps a model for its
  `keep_alive` after the last request and unloads it by itself. After every question and digest the
  runner moves that time to the connection's setting (`POST /api/generate` without a prompt and
  with `keep_alive`), because requests through the OpenAI-compatible endpoint leave Ollama's own
  default. No Finuance process keeps a timer, so the api replicas and the worker never have to
  agree; the host's `OLLAMA_KEEP_ALIVE` stays as the fallback when the setting is the server's own
  or a reset fails. LM Studio gets the time as `ttl` with each request (it applies to models it
  loaded on demand).
- **Loading and unloading** from Administration (`/admin/assistant/connections/{name}/load` and
  `/unload`, audited) work on Ollama, which the client recognises by its native API (`/api/show`,
  else `/api/version`); its `/api/ps` lists the loaded models with their memory and unload time.
  LM Studio's list says which models are loaded, but its REST API is not used to load or unload;
  other servers get the switch only.
- **Who picks** (`instance_settings.assistant.modelChoice`, `defaultConnection`): **the
  administrator** by default: every question and digest runs on the default connection and its
  default model, whatever the panel asks for, and people's status lists only that connection;
  people's own choices stay saved for when the administrator lets them pick again (**each
  person**). A question with photos runs on the connection's model for photos whenever the model in
  use can't read images, in both modes (the default text model on a 4 GB GPU reads no images,
  20.8).
- **Test** reports what the check found: the models, how long the list took and, where the server
  says, whether the model is loaded (the first answer after unloading takes longer).

Alternatives considered: a timer in Finuance (the api replicas and the worker would need to share
the last question's time, through the database or a leader); unloading through a host script
(outside the app and the audit log); locking the model per connection only (an administrator asked
for one switch; the connection's default model plays that role).

