package assistant

import (
	"encoding/json"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// History limits (legacy trimHistory).
const (
	historyMessages = 30
	oldToolChars    = 2500
)

// historyOf turns stored messages into the model history. A turn that failed (its last assistant
// message has an error) or never finished (no final message, e.g. the process stopped) is left
// out with its question, as the legacy popped them.
func historyOf(msgs []Message) []llm.Message {
	var out []llm.Message
	for i := 0; i < len(msgs); {
		if msgs[i].Role != llm.RoleUser {
			i++ // orphaned rows (never written by Append) are skipped
			continue
		}
		j := i + 1
		for j < len(msgs) && msgs[j].Role != llm.RoleUser {
			j++
		}
		turn := msgs[i:j]
		last := turn[len(turn)-1]
		if last.Role == llm.RoleAssistant && last.Meta != nil && last.Error == nil {
			for k := range turn {
				m := &turn[k]
				out = append(out, llm.Message{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Replay: m.Replay})
			}
		}
		i = j
	}
	return out
}

// trimHistory is legacy trimHistory: the last 30 messages starting at a user message, tool
// results before the last question cut to 2,500 characters; plain (plan mode) keeps only user and
// assistant text; noThink appends /no_think to the last question (Qwen 3). Then, while the
// estimate exceeds budget tokens, the oldest turn is dropped (the last question always stays).
func trimHistory(h []llm.Message, plain, noThink bool, system string, budget int) []llm.Message {
	msgs := h
	if len(msgs) > historyMessages {
		msgs = msgs[len(msgs)-historyMessages:]
	}
	for len(msgs) > 0 && msgs[0].Role != llm.RoleUser {
		msgs = msgs[1:]
	}
	lastUser := -1
	for i, m := range msgs {
		if m.Role == llm.RoleUser {
			lastUser = i
		}
	}
	out := make([]llm.Message, 0, len(msgs))
	for i, m := range msgs {
		if m.Role == llm.RoleTool && i < lastUser && len(m.Content) > oldToolChars {
			m.Content = cutBytes(m.Content, oldToolChars) + "…(truncated)"
		}
		if plain && (m.Role == llm.RoleTool || len(m.ToolCalls) > 0 || m.Content == "") {
			continue
		}
		out = append(out, m)
	}
	for budget > 0 && estTokens(system, out) > budget {
		next := -1
		for i := 1; i < len(out); i++ {
			if out[i].Role == llm.RoleUser {
				next = i
				break
			}
		}
		if next < 0 {
			break
		}
		out = out[next:]
	}
	if noThink {
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Role == llm.RoleUser {
				out[i].Content += " /no_think"
				break
			}
		}
	}
	return out
}

// cutBytes cuts s to at most n bytes without splitting a character (legacy .slice on UTF-16).
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// estTokens is the legacy estimate: JSON length / 4.
func estTokens(system string, msgs []llm.Message) int {
	b, _ := json.Marshal(msgs)
	return (len(system) + len(b)) / 4
}

// stoppedNote and roundsNote end a turn's text like the legacy did.
const (
	stoppedNote = "_Stopped._"
	roundsNote  = "_Stopped after 6 lookups._"
)

func appendNote(content, note string) string {
	if strings.TrimSpace(content) == "" {
		return note
	}
	return content + "\n\n" + note
}
