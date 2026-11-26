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
	// Type is the provider's own name for the error when it gives one (Anthropic's
	// invalid_request_error, OpenAI's type or code): a word of its fixed vocabulary, never the
	// request's text, so it may go on a log line and a span (issue 109). "" otherwise.
	Type string
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
// try. A thinking block refused for its signature is not about tools: plan mode would hide it and
// teach the connection that the model has none (issue 109).
func ToolsMayBeRejected(err error) bool {
	var e *Error
	if !errors.As(err, &e) || ThinkingBlockRefused(err) {
		return false
	}
	return e.Kind == KindToolsRejected || (e.Kind == KindUpstream || e.Kind == KindBadResponse) &&
		(e.Status == 400 || e.Status == 422 || e.Status == 500)
}

// ThinkingBlockRefused reports whether the provider refused a thinking block sent back, for its
// signature: the conversation before the block is not what it was when the block was made.
func ThinkingBlockRefused(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == 400 && signatureRe.MatchString(e.Message)
}

var (
	contextRe = regexp.MustCompile(`(?i)context (length|window|size)|too many tokens|maximum context|exceeds? .*context|prompt is too long|n_ctx|context_length_exceeded`)
	toolsRe   = regexp.MustCompile(`(?i)(does not|doesn't|not) support (tools|function|tool)|tools? (are|is) not supported|tool_choice|unknown (field|parameter).*tools`)
	notFound  = regexp.MustCompile(`(?i)model .*(not found|not loaded|does not exist)|no (such )?model|model_not_found`)
	// Anthropic: "Invalid `signature` in `thinking` block. The block is bound to a different conversation."
	signatureRe = regexp.MustCompile("(?i)signature.{0,8}in.{0,8}thinking|thinking.{0,40}bound to a different")
	// a provider's error type is a word: anything else is left out
	errTypeRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// Public is what a user who is not an administrator is told about the error: one fixed sentence
// per kind. Message can quote the model server's own words, its address, a resolved IP or part of
// a rejected key, which are the administrator's to see (Administration → Assistant), not every
// signed-in user's (AI-4).
func (e *Error) Public() string {
	switch e.Kind {
	case KindUnreachable:
		return "The model server can't be reached. An administrator can see why under Administration → Assistant."
	case KindAuth:
		return "The model server refused the connection's key. An administrator needs to check it."
	case KindToolsRejected:
		return "This model can't look things up with tools."
	case KindContextTooLong:
		return "The conversation is too long for this model. Start a new chat, or pick a model with more room."
	case KindRateLimited:
		return "The model provider is limiting requests right now. Try again in a moment."
	case KindNotFound:
		return "The model isn't available on the model server."
	case KindTimeout:
		return "The model didn't answer in time."
	case KindBadResponse:
		return "The model server's answer couldn't be read."
	default:
		return "The model server reported an error."
	}
}

// statusError classifies a non-2xx answer from its status and body.
func statusError(status int, body []byte) *Error {
	msg := errorText(body)
	e := &Error{Status: status, Message: msg, Type: errorType(body)}
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

// errorType is the provider's error type in an Anthropic ({error:{type}}) or OpenAI
// ({error:{type}} or {error:{code}}) body, when it is a word; "" otherwise.
func errorType(body []byte) string {
	var j struct {
		Error struct {
			Type string          `json:"type"`
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &j) != nil {
		return ""
	}
	t := j.Error.Type
	if t == "" {
		_ = json.Unmarshal(j.Error.Code, &t) // a string code; a number is left out
	}
	if !errTypeRe.MatchString(t) {
		return ""
	}
	return t
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
	switch {
	case errors.Is(err, errRedirect):
		return &Error{Kind: KindBadResponse, Message: "the model server redirected to another address, which is not followed"}
	case errors.Is(err, errPlaintext):
		return &Error{Kind: KindUnreachable, Message: "a cloud connection must use https"}
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
