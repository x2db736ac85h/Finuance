package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query/dsl"
)

// Compiled is a context turned into WHERE predicates over transactions t, accounts a and the
// optional joins. The arguments hold every user-supplied value; Where never contains user text.
type Compiled struct {
	Where  []string // AND-ed predicates, the user filter first
	Scope  []string // the user and account predicates only: the period anchor ignores filters
	Joins  map[Join]bool
	Args   []any // the arguments of Where; Scope uses the first ScopeN of them
	ScopeN int
}

// Add appends an argument for SQL built around the compiled predicates and returns its placeholder.
func (c *Compiled) Add(v any) string {
	c.Args = append(c.Args, v)
	return "$" + strconv.Itoa(len(c.Args))
}

// Compile turns c into predicates for userID. Column names are checked against the registry
// (unknown names are validation errors); conditions themselves never fail: what doesn't parse
// as a number or date falls back to a text match, as in the legacy.
func Compile(userID uuid.UUID, c Context) (*Compiled, error) {
	a := &args{}
	out := &Compiled{Joins: map[Join]bool{}}
	out.Scope = append(out.Scope, "t.user_id = "+a.add(userID))
	if len(c.AccountIDs) > 0 {
		out.Scope = append(out.Scope, "t.account_id = ANY("+a.add(c.AccountIDs)+"::uuid[])")
	}
	out.Where = append(out.Where, out.Scope...)
	out.ScopeN = len(a.vals)
	if !c.IncludeTransfers {
		out.Where = append(out.Where, "NOT t.is_transfer")
	}
	out.Where = append(out.Where, searchPreds("t.search_text", c.Search, a)...)
	ve := &platform.ValidationError{}
	checkText(ve, "context.search", c.Search)
	for i, f := range c.Filters {
		checkText(ve, fmt.Sprintf("context.filters[%d].cond", i), f.Cond)
		for _, x := range f.Excluded {
			checkText(ve, fmt.Sprintf("context.filters[%d].excluded", i), x)
		}
		col, ok := columns[f.Column]
		if !ok {
			ve.Add(fmt.Sprintf("context.filters[%d].column", i), fmt.Sprintf("unknown column %q", f.Column))
			continue
		}
		if col.Join != "" {
			out.Joins[col.Join] = true
		}
		if cond, ok := dsl.Parse(f.Cond); ok {
			out.Where = append(out.Where, condition(col, cond, a))
		}
		if len(f.Excluded) > 0 {
			out.Where = append(out.Where, "coalesce("+col.Text+", '') <> ALL("+a.add(f.Excluded)+"::text[])")
		}
	}
	if tf := c.ToolFilter; tf != nil {
		for name, v := range map[string]string{"query": tf.Query, "payee": tf.Payee, "category": tf.Category, "group": tf.Group} {
			checkText(ve, "context.toolFilter."+name, v)
		}
	}
	if err := ve.Err(); err != nil {
		return nil, err
	}
	if tf := c.ToolFilter; tf != nil {
		out.Where = append(out.Where, toolFilter(*tf, a, out.Joins)...)
	}
	out.Args = a.vals
	return out, nil
}

// checkText rejects text PostgreSQL can't take as a parameter: a NUL character (JSON allows
// \u0000; stored text never contains it, so such a filter could not match anything anyway).
func checkText(ve *platform.ValidationError, field, s string) {
	if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
		ve.Add(field, "must be valid text without NUL characters")
	}
}

// condition compiles one parsed condition for col (legacy compileCond). Numbers and dates try
// their typed reading first; anything that doesn't parse falls back to text. "!" negates the
// result, and a NULL value counts as "no match" before the negation (legacy NaN comparisons).
func condition(col Column, c dsl.Cond, a *args) string {
	var pred string
	switch col.Type {
	case Number:
		pred = numberPred(col, c, a)
	case Date:
		pred = datePred(col, c, a)
	}
	if pred == "" {
		pred = textPred(col, c, a)
	}
	if c.Neg {
		return "NOT coalesce(" + pred + ", false)"
	}
	return pred
}

func numberPred(col Column, c dsl.Cond, a *args) string {
	v := col.Value
	switch {
	case c.Kind == dsl.Range:
		x, okx := decimal(c.A)
		y, oky := decimal(c.B)
		if okx && oky {
			px, py := a.add(x), a.add(y)
			return fmt.Sprintf("(%s BETWEEN LEAST(%s::numeric, %s::numeric) AND GREATEST(%s::numeric, %s::numeric))", v, px, py, px, py)
		}
	case c.Kind == dsl.Op && ingest.TrimJS(c.V) != "":
		if x, ok := decimal(c.V); ok {
			return "(" + v + " " + c.Op + " " + a.add(x) + "::numeric)"
		}
	}
	return ""
}

// decimal reads a filter operand with the import's number rules (ParseNumber: "1 000", "-2 850,00",
// "1.234,56") and returns it as an exact decimal literal for a numeric parameter.
func decimal(s string) (string, bool) {
	p, ok := money.Split(s)
	if !ok || p.Int+p.Frac == "" {
		return "", false
	}
	d := p.Int
	if d == "" {
		d = "0"
	}
	if p.Frac != "" {
		d += "." + p.Frac
	}
	if p.Negative {
		d = "-" + d
	}
	return d, true
}

func datePred(col Column, c dsl.Cond, a *args) string {
	v := col.Value
	switch {
	case c.Kind == dsl.Range:
		xl, xh, okx := ingest.LooseDate(c.A)
		yl, yh, oky := ingest.LooseDate(c.B)
		if okx && oky {
			lo, hi := minDate(xl, yl), maxDate(xh, yh)
			return "(" + v + " >= " + a.add(lo.String()) + "::date AND " + v + " < " + a.add(hi.String()) + "::date)"
		}
	case c.Kind == dsl.Op && ingest.TrimJS(c.V) != "":
		lo, hi, ok := ingest.LooseDate(c.V)
		if !ok {
			return ""
		}
		switch c.Op {
		case ">":
			return "(" + v + " >= " + a.add(hi.String()) + "::date)"
		case ">=":
			return "(" + v + " >= " + a.add(lo.String()) + "::date)"
		case "<":
			return "(" + v + " < " + a.add(lo.String()) + "::date)"
		case "<=":
			return "(" + v + " < " + a.add(hi.String()) + "::date)"
		}
		return "(" + v + " >= " + a.add(lo.String()) + "::date AND " + v + " < " + a.add(hi.String()) + "::date)"
	case c.Kind == dsl.Bare:
		if lo, hi, ok := ingest.LooseDate(c.Body); ok {
			return "(" + v + " >= " + a.add(lo.String()) + "::date AND " + v + " < " + a.add(hi.String()) + "::date)"
		}
	}
	return ""
}

// textPred is the text fallback: "=v" is equality and "=" alone matches empty values; anything
// else is a substring match. Both compare normalised, whitespace-collapsed text (the _n columns;
// ALLOWED_DIFFS row 12).
func textPred(col Column, c dsl.Cond, a *args) string {
	norm := "coalesce(" + col.Norm + ", '')"
	if c.Op == "=" {
		return "(" + norm + " = " + a.add(shadow(ingest.TrimJS(c.V))) + ")"
	}
	return "(" + norm + " LIKE " + a.add(contains(shadow(c.Body))) + ")"
}

// shadow normalises user text the way the _n columns were filled at import.
func shadow(s string) string { return ingest.Collapse(ingest.Norm(s)) }

// contains is a LIKE pattern matching s anywhere, with LIKE's metacharacters escaped (PostgreSQL's
// default escape character is the backslash).
func contains(s string) string {
	return "%" + likeEscaper.Replace(s) + "%"
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// searchPreds: every token of the normalised search text must appear in expr (legacy rowNorm
// match; search_text is Norm of the row's raw values).
func searchPreds(expr, search string, a *args) []string {
	toks := ingest.FieldsJS(ingest.Norm(search))
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		out = append(out, expr+" LIKE "+a.add(contains(tok)))
	}
	return out
}

// toolFilter compiles the assistant's lookup filter (port of legacy aiFilter).
func toolFilter(f ToolFilter, a *args, joins map[Join]bool) []string {
	out := searchPreds("t.search_text", f.Query, a)
	if f.DateFrom != nil {
		out = append(out, "t.booked_on >= "+a.add(f.DateFrom.String())+"::date")
	}
	if f.DateTo != nil { // inclusive: before the next day
		out = append(out, "t.booked_on < "+a.add(f.DateTo.AddDays(1).String())+"::date")
	}
	switch f.Type {
	case "expense":
		out = append(out, "t.amount_minor < 0")
	case "income":
		out = append(out, "t.amount_minor > 0")
	}
	amount := "abs(" + major("t.amount_minor", "t.currency") + ")"
	for _, b := range []struct {
		v  *string
		op string
	}{{f.MinAmount, ">="}, {f.MaxAmount, "<="}} {
		if b.v == nil {
			continue
		}
		if d, ok := jsNumber(*b.v); ok {
			out = append(out, amount+" "+b.op+" abs("+a.add(d)+"::numeric)")
		} else {
			out = append(out, "false") // legacy: Math.abs(NaN) fails every comparison
		}
	}
	if f.Payee != "" {
		out = append(out, "t.payee_n LIKE "+a.add(contains(shadow(f.Payee))))
	}
	if f.Category != "" {
		joins[JoinCategories] = true
		out = append(out, "coalesce(c.name_n, '') LIKE "+a.add(contains(shadow(f.Category))))
	}
	if f.Group != "" {
		joins[JoinGroups] = true
		out = append(out, "coalesce(g.name_n, '') LIKE "+a.add(contains(shadow(f.Group))))
	}
	return out
}

var jsNumberRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// jsNumber reads an amount the way JavaScript's unary + does for the assistant's arguments
// ("50", " 50 ", "1e3"; "" is 0) and returns a numeric literal. Other text is not a number.
func jsNumber(s string) (string, bool) {
	s = ingest.TrimJS(s)
	if s == "" {
		return "0", true
	}
	if !jsNumberRe.MatchString(s) {
		return "", false
	}
	return s, true
}

// ParseToolDate reads an assistant date argument like legacy isoD: the first 10 characters as an
// ISO date ("2025-03-10T15:00:00" is 10 March). Anything else is no constraint (nil).
func ParseToolDate(s string) *platform.Date {
	if len(s) > 10 {
		s = s[:10]
	}
	d, err := platform.ParseDate(s)
	if err != nil {
		return nil
	}
	return &d
}

func minDate(x, y platform.Date) platform.Date {
	if y.Before(x) {
		return y
	}
	return x
}

func maxDate(x, y platform.Date) platform.Date {
	if x.Before(y) {
		return y
	}
	return x
}
