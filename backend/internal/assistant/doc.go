// Package assistant is the AI assistant (PLAN §4.8, ADR 0015): the configured model connections,
// the read-only tools, the system prompt, the run loop and the stored chats. Tools run under
// store.WithUserReadOnly, so a model only ever reads the signed-in user's data, and nothing here
// logs prompts, model output or tool results.
package assistant
