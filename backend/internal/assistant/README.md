# assistant

The AI assistant (PLAN §4.8, ADR 0015; setup in [docs/assistant.md](../../../docs/assistant.md)).

| File | What it does |
|---|---|
| `connections.go` | The configured model servers: startup address check, model lists cached 60 s, capabilities per model, models learned to be plan-only |
| `tools.go`, `args.go`, `tooldefs.go` | The five read-only tools with the legacy's argument semantics and JSON schemas; SQL lives in `internal/query/assistant.go` and runs in `store.WithUserReadOnly` |
| `prompt.go` | The system prompt (port of legacy `sysPrompt`) with facts read per request |
| `run.go`, `history.go` | The run loop: tools mode, plan mode, auto fallback, history trimming, stop, limits, events, metrics, one log line per run |
| `store.go` | Settings (retention, cloud consent), chats and messages under the user's RLS context; retention cleanup |
| `probe.go` | `finuance admin llm-probe` |
| `eval/` | The eval harness behind `make eval-assistant` |

Nothing here logs prompts, model output or tool results. The tests compare the tools and the prompt with the legacy oracle (`TestToolsParity`, `TestSystemPromptParity`) and run the loop against `testutil/llmmock`.
