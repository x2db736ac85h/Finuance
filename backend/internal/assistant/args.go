package assistant

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// args are a tool call's arguments as the model wrote them. They are read with the legacy's
// JavaScript semantics (`+a.limit || 25`, `a.query ? … : …`), so a model that writes "7" for 7
// or leaves a field empty gets the same lookup as in the legacy app.
type args map[string]any

// parseArgs decodes a call's argument text. Anything that isn't a JSON object is no arguments
// (legacy runStep and execTool: `JSON.parse(…)` failures and non-objects become {}).
func parseArgs(raw string) args {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return args{}
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var a args
	if dec.Decode(&a) != nil || a == nil {
		return args{}
	}
	return a
}

// str is JavaScript's String(a[k]) for strings, numbers and booleans; absent and null are "".
func (a args) str(k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// truthy is JavaScript truthiness of a[k].
func (a args) truthy(k string) bool {
	switch v := a[k].(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case json.Number:
		f, err := v.Float64()
		return err == nil && f != 0 && !math.IsNaN(f)
	}
	return true // objects and arrays
}

// or is `a[k] || def` for text arguments.
func (a args) or(k, def string) string {
	if a.truthy(k) {
		return a.str(k)
	}
	return def
}

// count is `Math.max(1, Math.min(+a[k] || def, hi))` truncated toward zero (what slice() does with
// a fraction). Counts are row and group numbers, never money.
func (a args) count(k string, def, hi int) int {
	n := float64(def)
	if v, ok := jsPlus(a[k]); ok && v != 0 {
		n = v
	}
	n = math.Max(1, math.Min(n, float64(hi)))
	return int(n) // n is within [1, hi]
}

// jsPlus is JavaScript's unary + of a JSON value; false for NaN.
func jsPlus(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		s := ingest.TrimJS(x)
		if s == "" {
			return 0, true
		}
		if !decimalRe.MatchString(s) {
			return 0, false // hex, Infinity and the like don't occur in tool arguments
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	}
	return 0, false
}

var decimalRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// filter is the typed ToolFilter of the arguments (legacy aiFilter): strings and numbers as given,
// dates by their first ten characters.
func (a args) filter() query.ToolFilter {
	amount := func(k string) *string {
		v, ok := a[k]
		if !ok || v == nil {
			return nil
		}
		s := a.str(k)
		if _, isObj := v.(map[string]any); isObj {
			s = "NaN"
		}
		return &s
	}
	text := func(k string) string {
		if !a.truthy(k) {
			return ""
		}
		return a.str(k)
	}
	return query.ToolFilter{
		Query: text("query"), Payee: text("payee"), Category: text("category"), Group: text("group"),
		Type: a.or("type", "any"), MinAmount: amount("min_amount"), MaxAmount: amount("max_amount"),
		DateFrom: query.ParseToolDate(text("date_from")), DateTo: query.ParseToolDate(text("date_to")),
		Label: a.label(),
	}
}

// label describes the filters for step summaries and the "✦" chip (legacy fdesc). The group
// filter is named too (the legacy left it out, so a group lookup read "all rows").
func (a args) label() string {
	var parts []string
	for _, k := range []string{"query", "payee", "category", "group", "type"} {
		if a.truthy(k) && a.str(k) != "any" {
			parts = append(parts, k+": "+a.str(k))
		}
	}
	if a.truthy("date_from") || a.truthy("date_to") {
		parts = append(parts, a.or("date_from", "…")+" → "+a.or("date_to", "…"))
	}
	if len(parts) == 0 {
		return "all rows"
	}
	return strings.Join(parts, " · ")
}
