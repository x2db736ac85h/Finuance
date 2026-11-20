package assistant

import (
	"regexp"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// Masking for cloud connections (ADR 0015, register row AI-2). A cloud model is someone else's
// computer: what goes there is the user's question and what the tools look up, with long numbers
// and addresses cut down first. One pass covers everything that is sent, so a field added later
// is covered too: the system prompt's names, the rows of this turn, and the results stored by
// earlier turns, which may have been made with a local model and were never masked.
var (
	// six or more digits in a row: account and card numbers, references, phone numbers
	longDigits = regexp.MustCompile(`\d{6,}`)
	// the same numbers as people and banks write them: groups of four ("PL61 1090 1014 …",
	// "4111-1111-1111-1111"), three groups of three with an optional prefix ("+48 600 700 800")
	groupedDigits = regexp.MustCompile(`\b(?:[A-Z]{2})?\d{2,4}(?:[ -]\d{4}){2,}\b|(?:\+\d{1,3}[ -])?\b\d{3}[ -]\d{3}[ -]\d{3}\b`)
	emailAddress  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+`)
	// a JSON string literal, and a string that is only a UUID (the ids a proposal sends back)
	jsonString = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	uuidOnly   = regexp.MustCompile(`^"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"$`)
)

// MaskDigits cuts what identifies an account, a card, a phone or a person down to a hint: a long
// or grouped number keeps its first two digits ("12…"), an e-mail address becomes "…@…". Amounts
// and dates are not touched: they have separators the patterns don't span.
func MaskDigits(s string) string {
	s = emailAddress.ReplaceAllString(s, "…@…")
	s = groupedDigits.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.IndexAny(m, "0123456789")
		return m[:min(i+2, len(m))] + "…"
	})
	return longDigits.ReplaceAllStringFunc(s, func(m string) string { return m[:2] + "…" })
}

// maskJSONStrings masks inside the string values of a tool result and leaves its structure, its
// numbers and its ids as they are, so the model can still send a proposal's ids back. Text that
// is not JSON is masked as a whole.
func maskJSONStrings(raw string) string {
	t := strings.TrimSpace(raw)
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return MaskDigits(raw)
	}
	return jsonString.ReplaceAllStringFunc(raw, func(lit string) string {
		if uuidOnly.MatchString(lit) {
			return lit
		}
		return MaskDigits(lit)
	})
}

// maskForCloud returns the messages as a cloud model gets them. The user's own words stay: they
// typed them for this model. Everything looked up or written before is masked: tool results
// (also those of earlier turns, stored unmasked when a local model answered) and earlier answers,
// which quote rows.
func maskForCloud(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case llm.RoleTool:
			m.Content = maskJSONStrings(m.Content)
		case llm.RoleAssistant:
			m.Content = MaskDigits(m.Content)
		}
		out[i] = m
	}
	return out
}
