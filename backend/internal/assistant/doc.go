// Package assistant is the AI assistant (PLAN §4.8, ADR 0015): the configured model connections,
// the read-only tools, the system prompt, the run loop and the stored chats. Tools run under
// store.WithUserReadOnly, so a model only ever reads the signed-in user's data, and nothing here
// logs prompts, model output or tool results.
package assistant

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

// Assistant bundles the parts the API and the worker use.
type Assistant struct {
	Connections *Connections
	Chats       *Chats
	Tools       *Tools
	Runner      *Runner
}

// Config wires an Assistant.
type Config struct {
	Store         *store.Store
	Query         *query.Engine
	Connections   *Connections
	Clock         platform.Clock
	MaxChats      int
	MaxConcurrent int
	Metrics       *Metrics
	Tracer        trace.TracerProvider
	Logger        *slog.Logger
}

// New builds the assistant.
func New(c Config) *Assistant {
	chats := NewChats(c.Store, c.Clock, c.MaxChats)
	tools := NewTools(c.Query)
	return &Assistant{Connections: c.Connections, Chats: chats, Tools: tools, Runner: NewRunner(RunnerConfig{Connections: c.Connections,
		Tools: tools, Chats: chats, Clock: c.Clock, MaxConcurrent: c.MaxConcurrent, Metrics: c.Metrics, Tracer: c.Tracer, Logger: c.Logger})}
}
