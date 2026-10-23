package llm

import (
	"encoding/json"
	"strings"
)

// splitter separates streamed text into answer, reasoning (<think>…</think>) and Qwen-style
// <tool_call>{…}</tool_call> blocks as it arrives, so the browser never sees half a tag (legacy
// splitThink and the <tool_call> fallback of complete(), done incrementally).
//
// Leading whitespace of the answer is dropped (legacy: a leading /^\s+/ is removed). A <think> that
// is never closed turns the rest of the text into reasoning. A <tool_call> block whose JSON
// doesn't parse is kept in the answer as written.
type splitter struct {
	emit    func(Delta)
	pending string // text that may be the start of a tag
	mode    int    // inText, inThink, inCall
	call    strings.Builder
	calls   []ToolCall
	started bool // a non-space answer character has been emitted
	answer  strings.Builder
	reason  strings.Builder
}

const (
	inText = iota
	inThink
	inCall
)

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
	callOpen   = "<tool_call>"
	callClose  = "</tool_call>"
)

func newSplitter(emit func(Delta)) *splitter { return &splitter{emit: emit} }

// write takes the next piece of streamed text.
func (s *splitter) write(text string) {
	s.pending += text
	for s.pending != "" {
		switch s.mode {
		case inText:
			i, tag := firstTag(s.pending, thinkOpen, callOpen)
			if i < 0 {
				keep := partialSuffix(s.pending, thinkOpen, callOpen)
				s.text(s.pending[:len(s.pending)-keep])
				s.pending = s.pending[len(s.pending)-keep:]
				return
			}
			s.text(s.pending[:i])
			s.pending = s.pending[i+len(tag):]
			if tag == thinkOpen {
				s.mode = inThink
			} else {
				s.mode = inCall
				s.call.Reset()
			}
		case inThink:
			i := strings.Index(s.pending, thinkClose)
			if i < 0 {
				keep := partialSuffix(s.pending, thinkClose)
				s.reasoning(s.pending[:len(s.pending)-keep])
				s.pending = s.pending[len(s.pending)-keep:]
				return
			}
			s.reasoning(s.pending[:i])
			s.pending = s.pending[i+len(thinkClose):]
			s.mode = inText
		case inCall:
			i := strings.Index(s.pending, callClose)
			if i < 0 {
				keep := partialSuffix(s.pending, callClose)
				s.call.WriteString(s.pending[:len(s.pending)-keep])
				s.pending = s.pending[len(s.pending)-keep:]
				return
			}
			s.call.WriteString(s.pending[:i])
			s.pending = s.pending[i+len(callClose):]
			s.mode = inText
			s.endCall(true)
		}
	}
}

// close flushes what is left at the end of the stream.
func (s *splitter) close() {
	switch s.mode {
	case inThink:
		s.reasoning(s.pending)
	case inCall:
		s.call.WriteString(s.pending)
		s.endCall(false)
	default:
		s.text(s.pending)
	}
	s.pending = ""
}

func (s *splitter) endCall(closed bool) {
	raw := s.call.String()
	var o struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if closed && json.Unmarshal([]byte(strings.TrimSpace(raw)), &o) == nil && o.Name != "" {
		args := string(o.Arguments)
		if args == "" || args == "null" {
			args = "{}"
		}
		s.calls = append(s.calls, ToolCall{Name: o.Name, Arguments: args})
		return
	}
	s.text(callOpen + raw + map[bool]string{true: callClose}[closed])
}

func (s *splitter) text(t string) {
	if !s.started {
		t = strings.TrimLeft(t, " \t\r\n")
		if t == "" {
			return
		}
		s.started = true
	}
	s.answer.WriteString(t)
	if s.emit != nil {
		s.emit(Delta{Content: t})
	}
}

func (s *splitter) reasoning(t string) {
	if t == "" {
		return
	}
	s.reason.WriteString(t)
	if s.emit != nil {
		s.emit(Delta{Reasoning: t})
	}
}

// firstTag returns the position and the tag that occurs first in s, or -1.
func firstTag(s string, tags ...string) (int, string) {
	at, tag := -1, ""
	for _, t := range tags {
		if i := strings.Index(s, t); i >= 0 && (at < 0 || i < at) {
			at, tag = i, t
		}
	}
	return at, tag
}

// partialSuffix is the length of the longest suffix of s that is a proper prefix of a tag (it may
// complete in the next piece, so it is held back).
func partialSuffix(s string, tags ...string) int {
	best := 0
	for _, t := range tags {
		for n := min(len(t)-1, len(s)); n > best; n-- {
			if strings.HasSuffix(s, t[:n]) {
				best = n
				break
			}
		}
	}
	return best
}
