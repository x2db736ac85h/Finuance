# 0015. Assistant: provider-agnostic connections, typed read-only tools, server-side run loop

- Status: accepted
- Date: 2026-10-23

## Context

The legacy app talked to one local OpenAI-compatible server from the browser. The rebuild runs the
assistant on the server (PLAN §4.8), and our Product Engineer wants it **provider- and
model-agnostic**: LM Studio in dev and prod, but users should be able to pick whatever an instance
offers, local or cloud (Ollama, vLLM, llama.cpp, OpenAI, Anthropic, any compatible server), with
thinking used when a model supports it, and with the model able to query the data for context.

Constraints: the user's data must not leave the instance unless an admin and the user both chose
so; tools must stay read-only and scoped by RLS; the browser CSP stays `connect-src 'self'`; models
differ widely in tool calling and reasoning output; local models are small and slow.

## Decision

1. **Connections are instance config.** `FINUANCE_AI_CONNECTIONS_FILE` (JSON) lists named
   connections: wire format (`openai` or `anthropic`), base URL, API key *environment variable*,
   a `cloud` flag, a default model, an optional model allowlist (globs), a context size and
   capability overrides. `FINUANCE_AI_BASE_URL` stays as a one-connection shorthand. Users pick a
   connection and a model by name; they never enter a URL. Per-user API keys are in the backlog.
2. **Two wire adapters behind one internal chat model** (`internal/llm`): OpenAI Chat Completions
   covers nearly every server; Anthropic Messages covers Anthropic and LM Studio's `/v1/messages`.
   Each adapter tolerates its servers' quirks (reasoning fields or `<think>` tags, tool calls
   streamed in pieces or printed as Qwen `<tool_call>` text, non-streaming JSON) and classifies
   errors into kinds the run loop and the UI act on.
3. **Capabilities per model** (tools, reasoning style, context size) come from the server's own
   report (LM Studio's native model list), the model id and the admin's overrides, and are
   corrected while running: a model that rejects tools is remembered as plan-only. Thinking is one
   setting (off, auto, low, medium, high) mapped per style: `reasoning_effort`, an Anthropic
   thinking budget, or Qwen 3's `/no_think`.
4. **Typed, read-only tools only.** The model calls `search_transactions`,
   `aggregate_transactions`, `find_values` (the legacy three, oracle-tested), `month_overview` and
   `recurring_payments`. Every call compiles to SQL in `internal/query` and runs in
   `store.WithUserReadOnly`. The model never writes SQL. Small models and plan mode get the legacy
   three (a compact set); the evals decide each model's default.
5. **Cloud connections need two agreements**: the admin's `cloud` flag (without it every dial to a
   public address is refused, checked at startup and on each connection) and a one-time consent per
   user and connection. Tool results sent to a cloud model have digit runs of six or more cut to
   two digits.
6. **The run loop is the legacy's, on the server**: tools mode (six rounds), plan mode for models
   without tool calling, auto fallback when a server rejects tools before any lookup; history
   trimmed to the last 30 messages and to the model's context; answers streamed as server-sent
   events; stop and disconnect cancel the model request; one run per user, a per-replica limit and
   an hourly rate per user.
7. **Chats are kept 48 h by default**; each user picks 1–7 days or forever (with a warning); an
   hourly job enforces it and reads skip expired chats. At most `FINUANCE_AI_MAX_CHATS` (25).

## Consequences (positive, negative, follow-ups)

- Any OpenAI- or Anthropic-compatible server works without code changes; adding a provider with a
  third wire format means a third adapter behind the same interface.
- The sandbox and CI never reach a model: `testutil/llmmock` speaks both protocols, the adapters are
  tested on recorded streams, and `finuance admin llm-probe` records real ones at the gate. Answer
  quality is measured by `make eval-assistant` on real models, outside CI.
- Capability guessing by model id is a heuristic; the admin overrides fix wrong guesses, and the
  runtime fallback covers a model that refuses tools.
- Prompt injection through bank text stays possible; its blast radius is a read-only, RLS-scoped
  lookup of the user's own data, and every number in an answer can be traced to a visible step.
- Follow-ups: per-user API keys (backlog); write actions with confirmation (Phase 15); an egress
  NetworkPolicy to the configured hosts (Phase 16).

## Alternatives considered

- **One OpenAI-compatible endpoint only (the legacy design).** Simplest, but rules out Anthropic's
  native API and multiple servers; LM Studio alone already exposes both formats.
- **A provider SDK or an LLM gateway library** (LangChain-style, LiteLLM). A large dependency for
  two wire formats we can parse in a few hundred lines; harder to make tolerant of local servers'
  quirks, and a new dependency needs approval.
- **Model-written SQL ("text to SQL").** More flexible questions, but no way to guarantee read-only,
  RLS-safe, bounded queries, and small models write wrong SQL often; typed tools are testable
  against the oracle.
- **User-entered endpoints.** Server-side requests to user-chosen URLs are an SSRF hole; endpoints
  stay instance config.
- **Storing chats until deleted (the legacy kept the last N).** Our Product Engineer chose a short
  default retention with a user choice instead: chats hold financial details in free text.
