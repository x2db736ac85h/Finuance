// Package dsl parses the per-column filter conditions of the table (PLAN §4.5, port of the parsing
// half of legacy compileCond):
//
//	cond    = [ "!" ] body ;               "!" negates the whole condition
//	body    = range | op_expr | bare ;
//	range   = value ".." value ;           whitespace allowed around ".."
//	op_expr = ( ">=" | "<=" | ">" | "<" | "=" ) [ value ] ;
//	bare    = value ;
//
// The parser only splits the text; what a condition means depends on the column type and is
// decided by the compiler (internal/query). A body can match both the range and the operator
// shape ("=a..b"): the legacy tries the range first for number and date columns, but its text
// fallback still looks at the operator, so Cond keeps both readings.
//
// The package is pure and never builds SQL.
package dsl

import (
	"regexp"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
)

// Kind is the primary shape of a condition body.
type Kind int

// The shapes, in the order the legacy tries them.
const (
	Bare  Kind = iota // a value on its own
	Op                // an operator followed by an optional value
	Range             // value .. value
)

// Cond is a parsed condition.
type Cond struct {
	Neg  bool   // a leading "!"
	Body string // the trimmed text after "!" (what the text fallback matches)
	Kind Kind
	A, B string // the range operands (Kind == Range), untrimmed as the legacy regex captures them
	Op   string // ">=", "<=", ">", "<" or "=" when the body starts with an operator (any Kind)
	V    string // the text after Op and the whitespace that follows it
}

// The legacy regexes, with JavaScript's \s and "." spelled out: JS \s includes Unicode spaces and
// U+FEFF, and "." (without the s flag) excludes only the line terminators.
const (
	jsSpace = `[\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`
	jsDot   = `[^\n\r\x{2028}\x{2029}]`
)

var (
	// /^(.+?)\s*\.\.\s*(.+)$/ — the lazy first group makes the first ".." the separator.
	rangeRe = regexp.MustCompile(`^(` + jsDot + `+?)` + jsSpace + `*\.\.` + jsSpace + `*(` + jsDot + `+)$`)
	// /^(>=|<=|>|<|=)\s*(.*)$/
	opRe = regexp.MustCompile(`^(>=|<=|>|<|=)` + jsSpace + `*(` + jsDot + `*)$`)
)

// Parse reads a condition. ok is false when there is no condition at all (empty or only
// whitespace): the column is then not filtered. "!" alone is a condition (it matches nothing).
func Parse(raw string) (c Cond, ok bool) {
	s := ingest.TrimJS(raw)
	if s == "" {
		return Cond{}, false
	}
	if s[0] == '!' {
		c.Neg = true
		s = ingest.TrimJS(s[1:])
	}
	c.Body = s
	if m := opRe.FindStringSubmatch(s); m != nil {
		c.Kind, c.Op, c.V = Op, m[1], m[2]
	}
	if m := rangeRe.FindStringSubmatch(s); m != nil {
		c.Kind, c.A, c.B = Range, m[1], m[2]
	}
	return c, true
}

// String prints the condition in canonical form; Parse(c.String()) == c.
func (c Cond) String() string {
	if c.Neg {
		return "!" + c.Body
	}
	return c.Body
}
