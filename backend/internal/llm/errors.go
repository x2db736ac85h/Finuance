package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Kind classifies a failed model call, so the assistant can say how to fix it.
type Kind string

// The error kinds.
const (
	KindUnreachable    Kind = "unreachable"      // no connection (server not running, wrong address)
	KindAuth           Kind = "auth"             // 401/403: missing or wrong API key
	KindToolsRejected  Kind = "tools_rejected"   // the server refused a request with tools
	KindContextTooLong Kind = "context_too_long" // the prompt doesn't fit the model's context
	KindRateLimited    Kind = "rate_limited"     // 429 from the provider
	KindNotFound       Kind = "model_not_found"  // the model isn't there (not loaded, misspelled)
	KindTimeout        Kind = "timeout"          // no answer, or the stream went silent
	KindBadResponse    Kind = "bad_response"     // not a chat response we can read
	KindUpstream       Kind = "upstream"         // any other error status
)

// Error is a classified model-call error. Message is the server's own text, shortened; it never
// contains the prompt.
type Error struct {
	Kind    Kind
	Status  int // HTTP status, 0 when there was no response
	Message string
}

func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("llm %s (HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("llm %s: %s", e.Kind, e.Message)
}

// KindOf returns the kind of an error from this package (KindUpstream for others).
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUpstream
}

// ToolsMayBeRejected reports whether a failed request with tools may mean the server or model
// doesn't support them (legacy: HTTP 400, 422 or 500 before any step), so plan mode is worth a
// try.
func ToolsMayBeRejected(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Kind == KindToolsRejected || (e.Kind == KindUpstream || e.Kind == KindBadResponse) &&
		(e.Status == 400 || e.Status == 422 || e.Status == 500)
}

var (
	contextRe = regexp.MustCompile(`(?i)context (length|window|size)|too many tokens|maximum context|exceeds? .*context|prompt is too long|n_ctx|context_length_exceeded`)
	toolsRe   = regexp.MustCompile(`(?i)(does not|doesn't|not) support (tools|function|tool)|tools? (are|is) not supported|tool_choice|unknown (field|parameter).*tools`)
	notFound  = regexp.MustCompile(`(?i)model .*(not found|not loaded|does not exist)|no (such )?model|model_not_found`)
)

// statusError classifies a non-2xx answer from its status and body.
func statusError(status int, body []byte) *Error {
	msg := errorText(body)
	e := &Error{Status: status, Message: msg}
	switch {
	case status == 401 || status == 403:
		e.Kind = KindAuth
	case status == 429:
		e.Kind = KindRateLimited
	case contextRe.MatchString(msg):
		e.Kind = KindContextTooLong
	case toolsRe.MatchString(msg):
		e.Kind = KindToolsRejected
	case status == 404 || notFound.MatchString(msg):
		e.Kind = KindNotFound
	default:
		e.Kind = KindUpstream
	}
	return e
}

// errorText takes the message out of an OpenAI ({error:{message}} or {error:"…"}), Anthropic
// ({type:"error", error:{message}}) or plain-text error body, at most 300 characters.
func errorText(body []byte) string {
	var j struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	msg := ""
	if json.Unmarshal(body, &j) == nil {
		var inner struct {
			Message string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(j.Error, &inner) == nil && inner.Message != "":
			msg = inner.Message
		case json.Unmarshal(j.Error, &s) == nil && s != "":
			msg = s
		case j.Message != "":
			msg = j.Message
		case j.Detail != "":
			msg = j.Detail
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	return clip(msg, 300)
}

// transportError classifies an error that happened before or while reading a response.
func transportError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errIdle) {
		return &Error{Kind: KindTimeout, Message: "the model didn't answer in time"}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &Error{Kind: KindTimeout, Message: "the model didn't answer in time"}
	}
	var oe *net.OpError
	if errors.As(err, &oe) || errors.Is(err, errBlockedAddress) {
		return &Error{Kind: KindUnreachable, Message: shortNetError(err)}
	}
	return &Error{Kind: KindUnreachable, Message: shortNetError(err)}
}

func shortNetError(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 && strings.Contains(s, "dial") {
		s = s[i+2:]
	}
	return clip(s, 200)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
