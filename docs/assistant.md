# The assistant: setup and operation

The assistant answers questions about your transactions with a language model that looks the data up through read-only tools (PLAN §4.8, ADR 0015). It works with any server that speaks the OpenAI Chat Completions or Anthropic Messages API: LM Studio, Ollama, vLLM, llama.cpp, OpenAI, Anthropic, OpenRouter and others, local or in the cloud.

The model never gets a dump of your data. It gets a short description (date range, categories, frequent payees, column examples) and eleven tools (the first three only, for small models and plan mode, with the compact tool set). Each tool runs on the server as a read-only query over the asking user's data only:

| Tool | What it does |
|---|---|
| `search_transactions` | Lists matching transactions, with the count and totals |
| `aggregate_transactions` | Counts and totals per category, payee, month, week, day, weekday, type, group or account |
| `find_values` | Finds how a payee, category or group is spelled |
| `month_overview` | One month at a glance: totals, savings rate, projection, budgets, largest expenses |
| `recurring_payments` | Subscriptions and regular bills found in the data (weekly, monthly or yearly; `cadence` on each payment) |
| `budget_status` | One month's budgets: spent, budget and what is left per category, over budget or ahead of pace first |
| `group_summary` | One transaction group (e.g. a trip): totals, days, spending per day, the group budget, categories and top payees |
| `goal_status` | The user's goals: saved so far against a steady pace, or this month's spending against a reduction goal's limit (ADR 0031) |
| `find_receipt_match` | The payments a receipt may belong to: the exact amount, within 3 days, ranked by the day and the shop's name (ADR 0032) |
| `search_advice` | Finds published articles of the instance's advice library for a general money question (ADR 0034) |
| `propose_action` | Proposes a change (a category or group, a rule, a budget, an expense, sharing or a split, a goal, a receipt in a note); it changes nothing by itself |

Every lookup is shown under the answer, with exactly what went back to the model and a *Show in table* link.

### Proposed changes

The assistant never changes your data. When you ask it to ("put these in Groceries", "make a rule for ŻABKA", "label these as Italy trip"), it proposes the change and the panel shows a card: what would change, why, a few of the affected transactions and *Show in table*. Nothing happens until you click **Apply**; the change then goes through the same requests as when you make it yourself. A proposal changes exactly the transactions it listed when it was made (at most 1,000), and a new category or group is created first. Applied proposals are logged without any content (ADR 0019).

Since Phase 20 the assistant can also propose a category's budget, a cash expense typed into a cash account, sharing transactions with a shared group, a new split of one of your shared expenses, and a new or changed goal. Every applied proposal is recorded with what it replaced (ADR 0030): the table marks the rows with ✦ (*Source* says Assistant for a row it added; "Only what the assistant changed" in the Columns menu filters them), and **Undo** on the card or in Settings → Activity → *Assistant changes* puts everything back, as long as nothing was changed since (otherwise it says so and changes nothing). In the panel's settings, *Apply by itself* lists the kinds of proposals the panel may apply without the click (none by default); it never does so for more than 50 transactions, and the card says *Applied automatically* with Undo.

### Receipts by photo

With a model that reads images (the model list in the panel's settings and `llm-probe` show `vision`), the panel has a camera button: pick or take a photo of a receipt, and ask (or just send it: "Here is a receipt."). The browser shrinks the photo to at most 1,600 px and saves it as a new JPEG, so the camera's EXIF data (location, device, time) never leaves the device; the server checks it and encodes it again. The photo goes to the model with that question only and is kept with the chat (its retention deletes it).

The model reads the shop, the date, the amount paid and the currency, and looks for the payment with `find_receipt_match` (money out of exactly that amount, booked within 3 days of the receipt). If it finds it, the card proposes to add "Receipt: shop · what was bought" to that transaction's note (Undo puts the old note back); if not, it proposes the expense: in a cash account, or in the bank account of a card payment whose file isn't imported yet. When that file arrives, a card above the table offers to **Merge** the typed payment into the bank's row (the bank's row stays, with the category, note, group and shared-group copy of the typed one) or to **Keep both** (ADR 0032).

### Advice library

For general questions about managing money ("how big should an emergency fund be?", "which debt first?"), the assistant searches the instance's advice library with `search_advice`: short articles an administrator wrote and published in Administration → Assistant. It bases the answer on them and names the article; the lookup lists the articles found, and each opens in a window. Articles are general information, not financial advice. The search folds case and accents and matches word beginnings, so it works in any language; a question finds articles in its own language first, then in any. An administrator can also add a few lines of **instructions** to every question's instructions (a tone, a language), after the app's own rules (ADR 0034).

### Digests

In the panel's settings, *Digest* asks the assistant for a short summary every day or every week (off by default), at an hour of your time zone: what you spent, budgets that are over or ahead of pace, how your goals are doing, payments due in the next week, and changes worth making. The worker writes it in the background with the connection and model the panel used when you switched it on, through the same read-only tools, into a chat of its own (marked *digest* in the recent chats). The bell says when it is ready and how many proposals wait in it; its proposals are never applied by themselves, whatever *Apply by itself* says. *Write one now* asks for one at once. A model server that is off at that hour means no digest that day (the bell says it couldn't be written). Demo visitors have no digests, and an administrator can switch them off for everyone (ADR 0033).

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

## Connections and tuning in the admin panel

Administrators can add further connections in **Administration → Assistant** (ADR 0035), next to the file's, which are shown there read only. The same rules apply (a local server on a private address, a cloud service over https with each user's consent). An API key entered there is sealed with AES-256-GCM under `FINUANCE_INSTANCE_KEY` (32 random bytes in base64: `openssl rand -base64 32`, or `tools/seal-secrets.sh <env> instance-key` and the chart's `assistant.instanceKeySecret` on Kubernetes); it is never shown again, and changing the connection's address drops it. Without the variable, connections without a key still work. Every api and worker process reads the stored connections every minute, so other replicas follow a change within a minute.

**Controls per connection** (Phase 20 follow-ups, ADR 0035 addendum): an administrator switches a connection on or off, sets the model questions use and a **model for photos**, and how long the server keeps a model loaded after the last question. **Who picks the model** is the administrator by default: questions and digests then run on the default connection and its default model, and the panel shows them read only; an administrator can let each person pick. A question with photos goes to the model for photos when the model in use can't read images.

**Unloading after idle time** uses the server's own timer. Ollama keeps a model for its `keep_alive` after the last request; requests through `/v1` use Ollama's default (`OLLAMA_KEEP_ALIVE`, 5 minutes unless set; 30 on the reference machine), so after each question and digest Finuance moves the model's unload time to the connection's setting with a load request without a prompt (`POST /api/generate` with `keep_alive`). Ollama then unloads the model by itself; nothing in Finuance keeps a timer. LM Studio gets the time as `ttl` with each request (for models it loaded on demand). In Administration, **Load** and **Unload** work on Ollama, recognised by its native API; the panel's **Test** shows whether the model is loaded (the first answer after unloading takes a few seconds longer while it loads).

**Tuning per model** sets, for one model of any connection, the temperature, the thinking level and the tool set used when a question doesn't set them, the context size to plan with, and a short note added after the instance's instructions. These are settings; no model is trained.

## Capabilities and thinking

For each model the assistant works out whether it calls tools, how its thinking is controlled, and its context size. It reads the server's own report (LM Studio's model list) and the model id, then applies your `caps` overrides:

| Style | Models | How "thinking" is set |
|---|---|---|
| `effort` | gpt-oss, OpenAI o-series and GPT-5, *reasoner*, Magistral | `reasoning_effort` low / medium / high |
| `budget` | Claude (Anthropic API) | a thinking token budget (1k / 4k / 16k) |
| `switch` | Qwen 3 | "off" appends `/no_think` to the question |
| `none` | everything else | not controlled; reasoning the model prints is still shown |

If a server rejects tool definitions before any lookup, the chat switches to **plan mode**: the model writes its lookups as JSON, the server runs them and then asks for the answer. The model is remembered as plan-only until the api restarts. If a server rejects the temperature or the reasoning setting, the request is retried once without it.

### Model notes (Phase 18)

- **`qwen3:4b` on Ollama is not the hybrid model any more**: Ollama's library tag now points at `qwen3:4b-thinking-2507` (the same model id in `ollama list`), which always thinks. That is why `/no_think`, Ollama's `think: false` (OpenAI-compatible and native API) and `reasoning_effort: none` changed nothing in Phase 18 (the last only moved the reasoning into the answer text). Name the 2507 models explicitly.
- **Qwen3 2507 pairs** split this into two models of the same size: `qwen3:…-instruct-2507` never thinks, `qwen3:…-thinking-2507` always does. The assistant reads both names as Qwen3 with a `/no_think` switch, so give them `reasoning: none` in `caps` (`{"pattern": "*-instruct*", "reasoning": "none"}`, the same for `*-thinking*`); offer both in the connection so users can choose per question.
- **Gemma** (planned as an alternative): some servers don't pass tool definitions to Gemma models; set `caps` `tools: false` for them (plan mode) if the status shows tool calls failing, and run the evals before making it the default.
- **Evals on the reference machine** (2026-10-31, `docs/evals.md`; a 4 GB GPU through Vulkan): `qwen3:4b-thinking-2507` with the full toolset answered 46 of 53 correctly (87 %, arguments right 98 %), `qwen3:4b-instruct-2507` 35 (full) and 26 (compact). Thinking costs about twice the wait there (p50 112 s vs 60 s). The reference machine's connection offers both, thinking-2507 as the default; the full toolset is the default in tools mode already.
- **Choose by answer quality**, not by the speed of a small test machine: the evals (`make eval-assistant`) on the server and model you'll actually use decide the default model and toolset.

### Model notes (Phase 20): text and receipts on a 4 GB GPU

Measured on the reference machine on 2026-11-06 (Ollama through Vulkan, 8,192 tokens of context, `docs/evals.md`): the 65 text questions (the 53 of Phase 18 and 12 new proposal and goal questions) and 17 receipts.

| Model | On the GPU | Text: the 53 | Text: 12 new | Text: all | p50 | Receipts |
|---|---|---:|---:|---:|---:|---:|
| `qwen3:4b-thinking-2507` | 3.2 GB, all of it | 47 (46 on 2026-10-31) | 7 | 54 / 65 | 143 s | doesn't read images |
| `qwen3-vl:4b-instruct` | 3.8 GB, ¾ (¼ on the CPU) | 36 | 10 | 46 / 65 | 29 s | 14 / 17 |
| `qwen3-vl:4b-thinking` | 3.8 GB, ¾ | 27 | 6 | 33 / 65 | 300 s | 16 / 17 |
| `gemma3:4b` | 2.9 GB, 90 % | 1 | 0 | 1 / 65 | 19 s | 10 / 17 |

- **No single model holds up on both here**, so the reference machine keeps two: `qwen3:4b-thinking-2507` as the default for questions, and `qwen3-vl:4b-instruct` for receipt photos (the camera button appears only for a model that reads images). Ollama swaps them on the GPU, which costs a few seconds when switching.
- **`qwen3-vl:4b-thinking`** reads receipts best but doesn't fit the GPU: with a quarter of it on the CPU it writes about 5 tokens a second, and 29 of the 65 questions ran into the 300 s limit. On a larger GPU it may be the one model for both; measure it there.
- **`qwen3-vl:4b-instruct`** calls an action's name as a tool (`set_budget` instead of `propose_action` with that action); the run now reads such a call as the proposal (9 of the 12 new questions went from wrong to right with that). It is also the fastest usable model (p50 29 s), a choice for people who prefer speed.
- **`gemma3:4b`** has no tool calling on Ollama (its capabilities say so), so it runs in plan mode and wrote ```` ```tool_code ```` instead of the plan's JSON; not usable for questions. On receipts it took the cash given or a subtotal for the total.
- Receipts misread by the Qwen models: a UK date read as a US one (both), and by the instruct model a French total and a cash receipt's amount given.

## Limits and retention

| Variable | Default | |
|---|---|---|
| `FINUANCE_AI_TIMEOUT` | `120s` | One model request, first byte to last |
| `FINUANCE_AI_IDLE_TIMEOUT` | `60s` | A model stream silent this long ends, also before its first token (at most `FINUANCE_AI_TIMEOUT`). A small GPU reading a long prompt (system prompt + tool results, ~8,000 tokens) can be silent for over a minute: raise both (e.g. `300s` / `150s` on a 4 GB card) |
| `FINUANCE_AI_MAX_CONCURRENT` | `2` | Answers written at once per api replica (`503 assistant_busy` beyond) |
| `FINUANCE_AI_RATE_PER_HOUR` | `30` | Questions per user per hour (`429`) |
| `FINUANCE_INSTANCE_KEY` | (none) | Seals the API keys of connections added in the admin panel (32 bytes, base64; ADR 0035) |
| `FINUANCE_AI_TOKENS_PER_DAY` | `200000` | Tokens per user per UTC day, questions and digests together: the outer bound of the `assistantTokensPerDay` limit (`422 limit_reached` once used; a digest is skipped) |
| `FINUANCE_AI_MAX_CHATS` | `25` | Chats kept per user; the oldest go first |

Each user can only have one answer in progress at a time (`409 run_in_progress`). A run's tokens are what the model server reported, or the length of what was sent and written / 4 where it reports none; a run that started under the budget finishes even if it goes over. The worker writes digests one at a time, so on a small GPU a question asked while a digest is written waits for the digest's current model request. Chats are deleted 48 hours after their last message unless the user picks 1–7 days or *forever* in the assistant settings. An hourly worker job applies the setting, and expired chats are never shown even before it runs. Chats are part of *Download my data* and are deleted with the account.

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
| *timeout* | Slow hardware: raise `FINUANCE_AI_TIMEOUT` and `FINUANCE_AI_IDLE_TIMEOUT`, choose a smaller model, or turn thinking off |
| a startup error about a public address | Mark the connection as `cloud` if that is intended, or fix the address |
