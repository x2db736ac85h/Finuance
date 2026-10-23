# The assistant: setup and operation

The assistant answers questions about your transactions with a language model that looks the data up through read-only tools (PLAN §4.8, ADR 0015). It works with any server that speaks the OpenAI Chat Completions or Anthropic Messages API: LM Studio, Ollama, vLLM, llama.cpp, OpenAI, Anthropic, OpenRouter and others, local or in the cloud.

The model never gets a dump of your data. It gets a short description (date range, categories, frequent payees, column examples) and five tools. Each tool runs on the server as a read-only query over the asking user's data only:

| Tool | What it does |
|---|---|
| `search_transactions` | Lists matching transactions, with the count and totals |
| `aggregate_transactions` | Counts and totals per category, payee, month, week, day, weekday, type, group or account |
| `find_values` | Finds how a payee, category or group is spelled |
| `month_overview` | One month at a glance: totals, savings rate, projection, budgets, largest expenses |
| `recurring_payments` | Subscriptions and regular bills found in the data |

Every lookup is shown under the answer, with exactly what went back to the model and a *Show in table* link.

## Configure one server

For a single server, set these variables on the api:

```sh
FINUANCE_AI_BASE_URL=http://192.168.1.20:1234/v1   # LM Studio; Ollama: http://host:11434/v1
FINUANCE_AI_API=openai                             # or anthropic (base URL without /v1)
FINUANCE_AI_DEFAULT_MODEL=qwen3-14b                # optional; otherwise the first Qwen, then the first chat model
FINUANCE_AI_API_KEY=                               # if the server wants one
```

By default the address must be private: loopback, RFC 1918, link-local, or a Tailscale/CGNAT address. The api checks this at startup and again on every connection, so a DNS change can't redirect it. Set `FINUANCE_AI_ALLOW_REMOTE=true` only for a cloud service; it then counts as a cloud connection (see below).

## Configure several connections

To offer several servers, point `FINUANCE_AI_CONNECTIONS_FILE` at a JSON file instead. `deploy/assistant/connections.example.json` is a working example:

```json
{
  "connections": [
    {"name": "lmstudio", "label": "LM Studio (homelab)", "api": "openai", "baseUrl": "http://lmstudio.lan:1234/v1",
     "defaultModel": "qwen3-14b", "caps": [{"pattern": "gemma-*", "tools": false}]},
    {"name": "anthropic", "label": "Claude (cloud)", "api": "anthropic", "baseUrl": "https://api.anthropic.com",
     "apiKeyEnv": "ANTHROPIC_API_KEY", "cloud": true, "models": ["claude-*"]}
  ]
}
```

| Field | Meaning |
|---|---|
| `name` | Stable id, `a-z 0-9 _ -`, at most 40 characters. Chats remember it. |
| `label` | What users see |
| `api` | `openai` (Chat Completions) or `anthropic` (Messages) |
| `baseUrl` | The API root: `…/v1` for OpenAI-compatible servers; the host for Anthropic (with or without `/v1`) |
| `apiKeyEnv` | Name of the environment variable that holds the key. The key itself never goes in the file, so the file can live in a GitOps repository. |
| `cloud` | Allows public addresses (https only). Users must agree once before their data goes there, and long digit runs in tool results are masked. |
| `defaultModel` | Used when the user picks none |
| `models` | Allowlist of model ids (globs, case-insensitive). If omitted, every chat model is offered; embedding models are never offered. |
| `contextTokens` | Context size when the server doesn't report one (default `FINUANCE_AI_CONTEXT_TOKENS`, 8192). LM Studio reports the loaded size itself. |
| `caps` | Corrections per model glob: `tools` (false = plan mode), `reasoning` (`none`, `effort`, `budget`, `switch`), `contextTokens` |

An invalid file stops the api at startup and names every problem at once.

## Capabilities and thinking

For each model the assistant works out whether it calls tools, how its thinking is controlled, and its context size. It reads the server's own report (LM Studio's model list) and the model id, then applies your `caps` overrides:

| Style | Models | How "thinking" is set |
|---|---|---|
| `effort` | gpt-oss, OpenAI o-series and GPT-5, *reasoner*, Magistral | `reasoning_effort` low / medium / high |
| `budget` | Claude (Anthropic API) | a thinking token budget (1k / 4k / 16k) |
| `switch` | Qwen 3 | "off" appends `/no_think` to the question |
| `none` | everything else | not controlled; reasoning the model prints is still shown |

If a server rejects tool definitions before any lookup, the chat switches to **plan mode**: the model writes its lookups as JSON, the server runs them and then asks for the answer. The model is remembered as plan-only until the api restarts. If a server rejects the temperature or the reasoning setting, the request is retried once without it.

## Limits and retention

| Variable | Default | |
|---|---|---|
| `FINUANCE_AI_TIMEOUT` | `120s` | One model request; a stream that stays silent for 60 s also ends |
| `FINUANCE_AI_MAX_CONCURRENT` | `2` | Answers written at once per api replica (`503 assistant_busy` beyond) |
| `FINUANCE_AI_RATE_PER_HOUR` | `30` | Questions per user per hour (`429`) |
| `FINUANCE_AI_MAX_CHATS` | `25` | Chats kept per user; the oldest go first |

Each user can only have one answer in progress at a time (`409 run_in_progress`). Chats are deleted 48 hours after their last message unless the user picks 1–7 days or *forever* in the assistant settings. An hourly worker job applies the setting, and expired chats are never shown even before it runs. Chats are part of *Download my data* and are deleted with the account.

## Checking a model: `finuance admin llm-probe`

```sh
finuance admin llm-probe                                  # every connection's models, then the default model
finuance admin llm-probe -connection=lmstudio -model=qwen3-8b -out=probe/
```

It lists each connection's chat models with their detected capabilities. Then it sends a fixed set of requests with made-up data: plain text, a tool call with all tools, the answer after a tool result, plan mode, and high thinking when the model has a control. For each one it prints what was understood. With `-out`, every request body and raw response is written to a file, ready to become a recorded stream in `backend/internal/llm/testdata`. Headers, and so API keys, are never written.

## Measuring quality: `make eval-assistant`

`make eval-assistant CONNECTION=… MODEL=… [THINK=…]` runs the 46 golden questions in `evals/assistant/` against a real model and rebuilds `docs/evals.md`. See `evals/assistant/README.md`.

## Local work without a model

`make llmmock` (or `docker compose --profile ai-mock up`, with `FINUANCE_AI_BASE_URL=http://ai-mock:11434/v1`) starts a mock server that speaks both APIs. It offers `mock-tools`, `mock-text-tools` and `mock-no-tools` (which rejects tools, to try plan mode). The E2E stack uses it.

## Troubleshooting

| The panel says | What to do |
|---|---|
| *unreachable* | Start the server (LM Studio: Developer tab → Start server; Ollama: `ollama serve`) and check the address and port from the api's host or container |
| *auth* | Check the API key variable named by `apiKeyEnv` (or `FINUANCE_AI_API_KEY`) |
| *model_not_found* | Load the model in LM Studio, or `ollama pull` it; check the model allowlist |
| *tools_rejected* | The model can't call tools: auto mode already switched to plan mode, or set `"tools": false` for it |
| *context_too_long* | Load the model with a larger context (LM Studio), set `contextTokens`, or lower *rows per lookup* |
| *timeout* | Slow hardware: raise `FINUANCE_AI_TIMEOUT`, choose a smaller model, or turn thinking off |
| a startup error about a public address | Mark the connection as `cloud` if that is intended, or fix the address |
