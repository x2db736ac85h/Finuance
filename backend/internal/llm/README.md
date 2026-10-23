# llm

Clients for the model servers the assistant talks to (Phase 13, ADR 0015). The package knows the
wire formats only: no prompts, tools or storage.

- `Client` with two adapters behind one message model (`Message`, `ToolCall`, `Result`):
  - `openai.go` — Chat Completions, as served by LM Studio, Ollama, llama.cpp, vLLM and OpenAI.
    Reasoning comes from `reasoning_content`, `reasoning` or `<think>` tags; tool calls are native
    (assembled by index) or printed as `<tool_call>{…}</tool_call>` text (`split.go`).
  - `anthropic.go` — Messages, as served by Anthropic and LM Studio. Thinking blocks are kept in
    `Result.Replay` and sent back with the next request, as the API requires.
- `http.go` — transport: total and idle timeouts, SSE reading, and a dial-time guard that refuses
  public addresses unless the connection is marked as a cloud one (`AllowRemote`).
- `errors.go` — every failure is an `*Error` with a `Kind` the run loop and the UI act on
  (`tools_rejected` → plan mode, `context_too_long`, `auth`, `unreachable`, …).
- `caps.go` — capabilities per model (tools, reasoning style, context size) from the server's own
  report, the model id and the admin's overrides.

Recorded streams for the tests live in `testdata/`; `finuance admin llm-probe` records new ones from
a real server.
