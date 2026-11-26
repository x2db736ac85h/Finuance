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

Since 0.8 it can also propose the opposite of two of these: taking transactions **out of their group**, and taking a **hand-set category off** again (back to the bank's or a rule's). It can add a transaction to **any account** by its name, money in as well as out, and it is told when a payment of that amount is in the account already (within two days), so that a payment isn't typed in twice; for a receipt it then proposes attaching it instead. A proposal that takes something away is never applied by itself, whatever kinds you switched on. What it cannot do, and says so: delete or edit an imported transaction, delete or rename a category, a group, a rule or an account, or undo an applied change (you do that in Settings → Activity, one change at a time or with **Undo several…** for a period or for everything).

### Memory

Since 0.8 you can keep a note for the assistant in **Settings → General → Assistant memory**: a few lines about you and how you like answers ("I am paid on the 10th", "answer in Polish"), at most 4,000 characters, about 1,000 tokens. While it is on it is added to every question, after the app's own rules, so it costs those tokens each time; switch it off to keep it without using it, or clear it. The assistant never writes it: when you say something lasting about yourself it proposes a line to remember, which you add with a click, or which is added by itself when **Automatic memories** is on in the assistant's settings (off by default). With a cloud connection the note goes to the provider like your questions, with long numbers shortened; the agreement says so (ADR 0046).

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

By default the address must be private: loopback, RFC 1918, or a Tailscale/CGNAT address (not link-local). A connection marked `cloud` is the opposite: it reaches public addresses only, over https, and never the instance's own network. Neither kind follows a redirect to another host. The api checks this at startup and again on every connection, so a DNS change can't redirect it. Set `FINUANCE_AI_ALLOW_REMOTE=true` only for a cloud service; it then counts as a cloud connection (see below).

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
| `auth` | `key` (the default: `apiKeyEnv`, or no key) or `google`: short-lived tokens from Google's credentials instead of a key (see [Using a cloud model](#using-a-cloud-model-google-cloud)). |
| `project`, `location` | For `"auth": "google"`: the Google Cloud project and the Vertex AI location. The address follows from them, so `baseUrl` is left out. |
| `cloud` | Allows public addresses (https only). Users must agree once before their data goes there, and long digit runs in tool results are masked. |
| `defaultModel` | Used when the user picks none |
| `models` | Allowlist of model ids (globs, case-insensitive). If omitted, every chat model is offered; embedding models are never offered. |
| `contextTokens` | Context size when the server doesn't report one (default `FINUANCE_AI_CONTEXT_TOKENS`, 8192). LM Studio reports the loaded size itself. |
| `caps` | Corrections per model glob: `tools` (false = plan mode), `reasoning` (`none`, `effort`, `budget`, `adaptive`, `switch`), `contextTokens` |

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
| `adaptive` | current Claude (4.6 and later, and any newer id), on Anthropic's API and on Vertex AI | the model decides when to think (`thinking: adaptive`); low / medium / high become `output_config.effort`, and "off" is the lowest effort. No token budget and no temperature are sent: these models answer `400` to both |
| `budget` | Claude 3.7, 4, 4.1 and 4.5 (Haiku 4.5, Sonnet 4.5, …) | a thinking token budget (1k / 4k / 16k) |
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

### Model notes (Phase 26): Claude through Anthropic's API

The 72 questions on 2026-11-25, thinking on auto, tools mode (`docs/evals.md`):

| Model | Correct | p50 | p95 |
|---|---:|---:|---:|
| `claude-haiku-5-5` | 69 / 72 (96 %) | 2.9 s | 6.2 s |
| `claude-sonnet-5-5` | 72 / 72 | 3.5 s | 10.1 s |
| `claude-opus-5-5` | 72 / 72 | 5.2 s | 12.7 s |

Haiku missed one Polish question on a month's income and two on splitting an expense. On 2026-11-26, with four questions added (taking rows out of a group, clearing a category, a new group), Haiku answered 73 of 76: the four new ones, and the same three misses. Sonnet and Opus were not run again. For
comparison, the best local model on the reference machine reached 83 % at a median of 143 s. These
runs used synthetic demo data; the decision that a cloud model sees synthetic data only still
stands.

## Cloud providers: what leaves and what they keep

A connection marked *cloud* sends data to somebody else's computers. This section says what is
sent and, per provider, what was found to need a decision. Read the part for your provider before
the first question, then write what applies to your account into **Administration → Assistant →
What a cloud provider keeps**: that paragraph is shown to every person where they agree.

The providers' terms change. Each part carries the date it was checked; check again, and trust your
own contract and your account's settings over this page.

### What leaves the instance

Exactly what leaves for any cloud connection: the question, the photos attached to it, and the
transactions the model looks up through its tools (payees, amounts, dates, titles, categories).
Runs of digits of account-number length are shortened first; photos go as they are. Each person
agrees once per connection, and the dialog names the provider (and for Google Cloud the location:
"Google Cloud (Vertex AI), in europe-west4") with the administrator's note on what is kept. A
service the app doesn't know by name is named by its host ("openrouter.ai").

### What is the same everywhere

- **A key or a credential identifies your account, not the person asking.** The provider sees one
  customer; the app sends no user id.
- **Not used for training by default** on the paid API of each provider below, as checked on the
  dates given. Free tiers and consumer apps are a different matter, and so are routers (below).
- **Kept for a while anyway.** "Not trained on" is not "not stored": each provider may keep
  requests for a time to look for abuse, and longer if the law requires it. Storing nothing ("zero
  data retention") is an arrangement you ask the provider for, not a switch in this app.
- **This app's own limits apply before anything leaves:** the consent per person, the shortening of
  long digit runs and e-mail addresses, the daily token budget per connection, https only.

### Anthropic and OpenAI (their own APIs, with a key)

Both are a model maker's own service: one company gets the data, under one set of terms.

**Anthropic** (`api.anthropic.com`), checked on 2026-11-26:

- Under the commercial terms, API content is not used to train models unless you agree to it.
- **Retention:** Anthropic's pages did not say the same thing on that day. The console's data
  controls describe 30 days by default, readable by Anthropic for safety and security; the API
  documentation says prompts and answers are not retained by default, except for its *Covered
  Models* (the Fable and Mythos class), which always keep 30 days and are assessed by automated
  safety systems. Your organisation's **Console → Settings → Privacy** shows what applies to you.
- **Zero data retention** is granted per organisation by Anthropic's sales team, and is not
  available for the Covered Models.
- **Where it is processed** is not something this connection chooses.

**OpenAI** (`api.openai.com`), checked on 2026-11-26:

- API data is not used for training unless you opt in.
- **Retention:** abuse-monitoring logs keep requests for up to 30 days by default, longer where the
  law requires it. *Modified abuse monitoring* and *zero data retention* need OpenAI's approval.
- **Where it is processed:** European data residency is set per project, for new projects, and
  usually has to be enabled by OpenAI's sales team. This connection does not choose it.
- The app uses the Chat Completions endpoint only, which stores nothing for later use by itself
  (no assistants, threads or files).

### OpenRouter and other routers

A router is **not** a model maker: it passes each request on to one of several companies, so two
parties see the data, and which second party can differ from request to request. It works here as
an *Other* connection (OpenAI format, `https://openrouter.ai/api/v1`, marked as a cloud service,
with its key); the consent dialog then names it by its host. It has not been tried with this app.

OpenRouter, checked on 2026-11-26:

- **OpenRouter itself** logs request metadata (time, model, token counts) and, by default, not the
  prompts or answers. Prompt logging is something you can opt into for a discount: leave it off.
- **The provider behind it** has its own policy. Your OpenRouter account settings decide which
  providers a request may reach: one setting keeps requests away from providers that may train on
  them, another (or `provider.zdr` per request, which this app does not send) allows only
  endpoints that store nothing. They are separate: "no training" is not "nothing stored". Set both
  in the account if you need both.
- **Where it is processed:** in-region routing (EU or US) is a feature of its business plans, on a
  different address; account and billing data may still be handled in the US.
- With your own provider key behind OpenRouter, the provider's terms above apply as well.

Because the party that finally answers is chosen by the router, say so in the paragraph people read
before they agree, and prefer a provider's own API when you can't pin the route down.

### Google Cloud (Vertex AI)

Vertex AI is a platform, not one model maker: Google runs Gemini, open models and Claude there
under Google Cloud's terms, with a location you choose and no stored key. It has its own section
with its own list: [What Google may keep](#what-google-may-keep-read-this-before-the-first-question).

## Using a cloud model (Google Cloud)

Since 0.7 a connection can use the models on Google Cloud's Vertex AI (Gemini, and the open models
behind the same endpoint) **without a stored key** (ADR 0045). The instance asks Google for a token
that lives about an hour, with credentials it already has, and sends it to Vertex AI's hosts only.

> **Synthetic data only, for now.** The project's own decision (2026-11-16) is that a cloud model
> sees synthetic data until the questions below are settled for the instance with real data.
> Nothing changes for an instance without a cloud connection.

> **Not yet run against Google.** As of 0.7.0 this path is tested against a mock of Vertex AI only
> (token renewal, the allowed hosts, both wire formats). Nobody has used it with a real project yet,
> so expect first-contact problems (an API not enabled, a model id, a quota) and please report them.

What leaves the instance is the same for every cloud connection: see [Cloud providers](#cloud-providers-what-leaves-and-what-they-keep) above. What follows is Google's side.

### What Google may keep (read this before the first question)

These are Google Cloud's terms as checked on 2026-11-16; they change, so check them again:

- **Abuse monitoring.** On an account without invoiced billing, prompts and answers may be logged
  for abuse monitoring (for up to about 30 to 90 days) unless an exception is granted. Google's
  documentation on abuse monitoring for Vertex AI says how to ask for it.
- **Caching.** A 24-hour in-memory cache of prompts is on by default at project level, and can be
  switched off for the project (Google's documentation on data governance for Vertex AI says how).
- **Where it is processed.** A regional location (`europe-west4`) or the `eu` multi-region keeps
  processing there. `global` gives no say over where: Google chooses per request. That is why the
  connection has **no default location**: you pick one, and `global` only if you name it.

What Google does with customer data beyond that is in its own terms for Vertex AI; read them for
your kind of account. This document only lists what was found to need a decision.

Claude on Vertex AI is under Google Cloud's terms for the platform, and the retention rules of
Anthropic's Covered Models apply there too (see Anthropic above).

Write what applies to your project into **Administration → Assistant → What a cloud provider
keeps**: that paragraph is shown to every person where they agree.

### Setting it up

1. **A project** with the Vertex AI API enabled (`aiplatform.googleapis.com`) and billing.
2. **Credentials for the instance**, one of:
   - *on Google Kubernetes Engine:* workload identity. Give the api's and the worker's Kubernetes
     service account a Google service account with the role **`roles/aiplatform.user`** on the
     project, and nothing else. No key, no Secret.
   - *on your own machine* (evals, local work): `gcloud auth application-default login`, with your
     own account holding the same role.
   - *on a cluster outside Google:* an external-account file (workload identity federation) named
     by `GOOGLE_APPLICATION_CREDENTIALS`. Not a service-account key.
3. **The connection**, in Administration → Assistant → *Add a connection* → **Google** (project,
   location, the model ids to offer), or in the connections file
   (`deploy/assistant/connections.vertex.example.json`):

   ```json
   {"name": "vertex", "label": "Gemini (Google Cloud)", "api": "openai", "auth": "google",
    "project": "my-project-123", "location": "europe-west4", "cloud": true,
    "models": ["google/gemini-3.5-flash-lite"]}
   ```

   Gemini models are named `google/<id>`; open models `<publisher>/<model>-maas`. Vertex AI's
   endpoint has no model list, so the ids you write are the list (a pattern such as `google/*`
   offers nothing by itself; it only allows ids).
4. **A budget.** Under the connection's *Models*, set a **daily token budget**: tokens per UTC day
   for everyone together, counted from what Google reports. When it is used up, questions on the
   connection answer `limit_reached` until the next day. Google Cloud's own budgets *alert* but do
   not stop spending, so set one there too (Billing → Budgets & alerts) and treat this limit as the
   stop.
5. **Egress.** The api and the worker need `443` to Google's API hosts, and on GKE the metadata
   server. The client itself refuses to send a Google token to any host that is not Vertex AI's
   (`aiplatform.googleapis.com`, `{location}-aiplatform.googleapis.com`,
   `aiplatform.{multi-region}.rep.googleapis.com`), and follows no redirect to another host.

### Claude on Vertex AI

The same connection kind reaches Anthropic's Claude models on Vertex AI: choose *Claude (Anthropic
on Vertex AI)* in the form, or write `"api": "anthropic"` with `"auth": "google"` in the file. The
address is then the location itself (`…/v1/projects/{project}/locations/{location}`), the model is
part of the request's address, and `models` lists Claude's ids as your project has them enabled
(Vertex AI's Model Garden). Everything else is the same: no key, the host allowlist, the budget,
consent. Not tried against Google yet (the tests use the mock); the eval is the first real run.

### What is different from a local server

- No key is stored, shown or asked for. *Test* under Models asks Google for a token and the
  endpoint for nothing more (it has no model list).
- Loading and unloading models and the idle timer don't apply.
- What a model can do (tools, images, thinking) comes from its name; correct it with `caps`.
- Token counts are Google's own (the request asks for the stream's usage), so the dashboard's
  "Tokens reported by the model servers" and both budgets are exact.

### Trying it without Google

`llmmock -vertex demo-project/europe-west4` answers like the endpoint and like the metadata server
(tokens that run out, `401` for a stale one, no model list). The tests use it
(`TestGoogleConnection`); with `GCE_METADATA_HOST` pointing at it, a local stack can too.

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

A connection can have a **daily token budget** for everyone together (Administration → Assistant → Models; ADR 0045): `422 limit_reached` with the limit `connectionTokensPerDay` once it is used up, until the next UTC day.

Each user can only have one answer in progress at a time (`409 run_in_progress`). A run's tokens are what the model server reported, or the length of what was sent and written / 4 where it reports none; a run that started under the budget finishes even if it goes over. The worker writes digests one at a time, so on a small GPU a question asked while a digest is written waits for the digest's current model request. Chats are deleted 48 hours after their last message unless the user picks 1–7 days or *forever* in the assistant settings. An hourly worker job applies the setting, and expired chats are never shown even before it runs. Chats are part of *Download my data* and are deleted with the account.

## Checking a model: `finuance admin llm-probe`

```sh
finuance admin llm-probe                                  # every connection's models, then the default model
finuance admin llm-probe -connection=lmstudio -model=qwen3-8b -out=probe/
```

It lists each connection's chat models with their detected capabilities. Then it sends a fixed set of requests with made-up data: plain text, a tool call with all tools, the answer after a tool result, plan mode, and high thinking when the model has a control. For each one it prints what was understood. With `-out`, every request body and raw response is written to a file, ready to become a recorded stream in `backend/internal/llm/testdata`. Headers, and so API keys, are never written.

## Measuring quality: `make eval-assistant`

`make eval-assistant CONNECTION=… MODEL=… [THINK=…]` runs the 76 golden questions in `evals/assistant/` against a real model and rebuilds `docs/evals.md`. See `evals/assistant/README.md`.

Against Vertex AI (synthetic demo data, your own Google login, no key anywhere):

```sh
gcloud auth application-default login
FINUANCE_AI_CONNECTIONS_FILE=deploy/assistant/connections.vertex.example.json \
  make eval-assistant CONNECTION=vertex MODEL=google/gemini-3.5-flash-lite
```

(with your project and location in a copy of the file). The run's cost shows in Google Cloud's billing report under Vertex AI a few hours later.

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
