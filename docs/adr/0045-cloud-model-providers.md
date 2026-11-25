# 0045. Cloud model providers: Google credentials without a stored key

- Status: accepted
- Date: 2026-11-25
- Changes: decision 4 of [ADR 0020](0020-llm-endpoint-in-the-cluster.md) (the egress of a cloud
  connection); builds on [ADR 0015](0015-assistant-connections-and-run-loop.md) and
  [ADR 0035](0035-assistant-administration.md)

## Context

A connection to a model server has had one way to say who it is: an API key, from an environment
variable or sealed in the database (ADR 0035). Google Cloud's models (Gemini and the open models
behind the same endpoint, Vertex AI) are reached with an OAuth2 access token that lives about an
hour. Service-account keys exist, but Google warns against them and they are exactly the long-lived
secret this project avoids storing.

Vertex AI has an OpenAI-compatible endpoint, so the wire format is the existing `openai` client's.
What is new is the credential, where it may be sent, what the endpoint is not (a model server
somebody runs), and who pays: a cloud model's cost falls on whoever runs the instance.

The scope decision of 2026-11-16 stands: a cloud model sees synthetic data only for now. Nothing
here changes a default, and an instance without a cloud connection behaves exactly as before.

## Decision

1. **A connection says how it authenticates:** `auth: key` (the default: a key, or none) or
   `auth: google`. A Google connection names a **project** and a **location**, has **no key**, is a
   **cloud** connection (so each person agrees before their data is sent, and long numbers are
   masked), and uses the `openai` format. The connections file, the panel and the table
   (`assistant_connections`, migration 00031) all refuse anything else.
2. **Tokens come from Google's standard credentials**, found at the first request
   (`internal/llm/googleauth`, on `golang.org/x/oauth2`, approved 2026-11-17):
   - the metadata server on Google Cloud (workload identity on GKE);
   - a developer's own login (`gcloud auth application-default login`) for local runs and evals;
   - an external-account file named by `GOOGLE_APPLICATION_CREDENTIALS` for a cluster outside
     Google (workload identity federation).

   One source serves every Google connection of a process. A token is cached and replaced five
   minutes before it runs out, so a request that starts with it ends with it. A `401` drops it:
   the request is sent once more with a new one, and a second `401` is the answer. Concurrent
   requests share one renewal.
3. **A token goes only to Vertex AI's hosts:** `aiplatform.googleapis.com`,
   `{location}-aiplatform.googleapis.com` and `aiplatform.{multi-region}.rep.googleapis.com`,
   matched as whole names, over https on the standard port. The rule is checked when a connection
   is saved, again when it is loaded from the database, and before every request (the token is not
   even asked for otherwise). Redirects to another host were already refused (PLAN 23.5).
4. **The location is chosen, never defaulted.** The address is derived from project and location
   (`https://{host}/v1/projects/{project}/locations/{location}/endpoints/openapi`). `global` works
   when it is named, and the form says what it means: Google chooses where each request is
   processed. The documents recommend `eu` or a region inside the EU.
5. **A provider's endpoint is not a local model server.** For a Google connection nothing beside the
   base address is probed for LM Studio or Ollama; the model list is the connection's own (the
   endpoint has none); loading and unloading "don't apply"; and what a model can do comes from its
   name and from corrections stored with the connection (`caps`), since the endpoint reports
   nothing.
6. **A token is a credential.** It is never logged and never a span attribute; a canary test runs
   whole questions, a renewal and a failed token request with every log line and span captured and
   finds no token in them.
7. **A budget per connection.** An administrator can give a connection a number of tokens per day,
   counted across all users from the usage the provider reports
   (`stream_options.include_usage`). Once it is used up, questions on that connection are refused
   with `limit_reached` until the next UTC day. The user's own daily budget (ADR 0033) is unchanged.
8. **Consent names the provider and the place.** The dialog and the "cloud" badge say who gets the
   data and where ("Google Cloud, Vertex AI, europe-west4"), with the administrator's sentence on
   retention.
9. **Egress.** A cluster that uses a cloud connection opens `443` for the api and the worker, as
   ADR 0020 said; what bounds the destination is now the client itself (decision 3 and the dial
   guard of 23.5: public addresses only, TLS only, no redirect to another host), not the network
   policy, because a provider's addresses change. On GKE the metadata server's address is the one
   more destination the pod needs.

## Consequences (positive, negative, follow-ups)

- No Google credential is stored by Finuance, in the database or in a Secret, on Google Cloud. What
  the credentials may do is decided in Google Cloud (`roles/aiplatform.user` on one project).
- A pod that can reach the metadata server can get the same token: that is true of every workload
  with workload identity, and is the reason the service account should have this one role
  (THREAT-MODEL, T20).
- The budget counts what the provider reports, so it is exact for Vertex AI; a server that reports
  nothing is counted by the old estimate. Google Cloud's own budgets alert but do not stop spending,
  so this limit is the only one that does.
- The model list of a Google connection is typed by hand and goes stale when Google renames models.
  The eval suite (`docs/evals.md`) is where a new id is tried first.
- New module: `golang.org/x/oauth2`, with `cloud.google.com/go/compute/metadata`.
- Not done here, each a decision of its own: a cloud model for the instance with real data;
  Claude on Vertex (its own wire variant, PLAN 26.9); keys per user.

## Alternatives considered

- **A service-account key as the connection's API key.** No new code, but a long-lived credential
  that can do whatever the account can, stored where everything else is. This is what the token
  source exists to avoid.
- **Google's API keys for Vertex AI.** Bound to a service account, warned against by Google, not
  accepted everywhere.
- **The Gemini API (`generativelanguage.googleapis.com`) with a key.** Works with the existing
  `key` connection today (provider "Other"). It gives no say over where data is processed and has
  other data terms, so it is not what "Google" means in the provider list.
- **Google's Go SDK for Vertex AI.** A second client, a second wire format and a large dependency
  tree for what one header does.
- **The network policy as the only bound on where a token goes.** A CIDR can't follow a provider's
  addresses, and a policy says nothing about a wrong address in the configuration.
