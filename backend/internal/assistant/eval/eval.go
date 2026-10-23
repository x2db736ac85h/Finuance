// Package eval runs the assistant's golden questions (evals/assistant/questions.json) through the
// real run loop against a model and scores each answer: the tool the model chose, its arguments
// (date ranges especially), and the numbers and names in the answer (PLAN task 13.9). Expected
// values come from reference lookups the harness runs itself with the oracle-checked tools, so
// no number is written by hand.
//
// `make eval-assistant CONNECTION=… MODEL=…` runs it against a real model (not part of CI); the
// unit tests check the scoring against the mock server.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// File is the questions file.
type File struct {
	Today     time.Time  `json:"today"`
	Questions []Question `json:"questions"`
}

// Question is one golden question.
type Question struct {
	ID       string              `json:"id"`
	Category string              `json:"category"`
	Lang     string              `json:"lang"`
	Question string              `json:"question"`
	Tools    []string            `json:"tools"`
	Args     []map[string]string `json:"args"`
	Refs     []Ref               `json:"refs"`
	Expect   []string            `json:"expect"`
	Mention  string              `json:"mention"`
}

// Ref is a reference lookup.
type Ref struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Load reads a questions file.
func Load(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path) //nolint:gosec // the repository's own file
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Expected is one value the answer must contain.
type Expected struct {
	Path   string
	Number *Cents // numbers, compared by absolute value
	Text   string // names
}

// Cents is an amount or count in hundredths, without a float.
type Cents struct {
	V        int64
	Fraction bool // the source had decimals
}

// Expectations runs a question's reference lookups and picks the expected values.
func Expectations(ctx context.Context, tools *assistant.Tools, userID uuid.UUID, q Question) ([]Expected, error) {
	results := make([]any, len(q.Refs))
	for i, r := range q.Refs {
		out, err := tools.Run(ctx, userID, assistant.Scope{MaxRows: assistant.MaxRowsLimit}, r.Tool, string(r.Args))
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(strings.NewReader(out.JSON))
		dec.UseNumber()
		if err := dec.Decode(&results[i]); err != nil {
			return nil, err
		}
	}
	var out []Expected
	for _, e := range q.Expect {
		idx, path, ok := strings.Cut(e, ":")
		i, err := strconv.Atoi(idx)
		if !ok || err != nil || i < 0 || i >= len(results) {
			return nil, fmt.Errorf("%s: bad expectation %q", q.ID, e)
		}
		v, err := pick(results[i], path)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", q.ID, e, err)
		}
		x := Expected{Path: e}
		switch t := v.(type) {
		case json.Number:
			c, ok := parseCents(t.String())
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a number: %s", q.ID, e, t)
			}
			x.Number = &c
		case string:
			x.Text = t
		default:
			return nil, fmt.Errorf("%s: %s picks %T", q.ID, e, v)
		}
		out = append(out, x)
	}
	return out, nil
}

var (
	argmaxRe = regexp.MustCompile(`^argmax\((\w+),(\w+)\)$`)
	findRe   = regexp.MustCompile(`^find\((\w+),(\w+)=([^)]*)\)$`)
)

// pick follows a path: "a.b.0.c", "argmax(list,field).x", "find(list,field=value).x".
func pick(v any, path string) (any, error) {
	parts := splitPath(path)
	for _, p := range parts {
		if m := argmaxRe.FindStringSubmatch(p); m != nil {
			list, _ := field(v, m[1]).([]any)
			best, bestV := -1, Cents{}
			for i, it := range list {
				n, _ := field(it, m[2]).(json.Number)
				c, ok := parseCents(n.String())
				if ok && (best < 0 || abs(c.V) > abs(bestV.V)) {
					best, bestV = i, c
				}
			}
			if best < 0 {
				return nil, fmt.Errorf("argmax over an empty list")
			}
			v = list[best]
			continue
		}
		if m := findRe.FindStringSubmatch(p); m != nil {
			list, _ := field(v, m[1]).([]any)
			i := slices.IndexFunc(list, func(it any) bool { s, _ := field(it, m[2]).(string); return s == m[3] })
			if i < 0 {
				return nil, fmt.Errorf("no %s with %s=%s", m[1], m[2], m[3])
			}
			v = list[i]
			continue
		}
		v = field(v, p)
		if v == nil {
			return nil, fmt.Errorf("no %q", p)
		}
	}
	return v, nil
}

// splitPath splits at dots outside parentheses.
func splitPath(p string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range p {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case '.':
			if depth == 0 {
				out = append(out, p[start:i])
				start = i + 1
			}
		}
	}
	return append(out, p[start:])
}

func field(v any, k string) any {
	switch t := v.(type) {
	case map[string]any:
		return t[k]
	case []any:
		if i, err := strconv.Atoi(k); err == nil && i >= 0 && i < len(t) {
			return t[i]
		}
	}
	return nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// parseCents reads a plain decimal ("-98.07", "141197").
func parseCents(s string) (Cents, bool) {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 {
		return Cents{}, false
	}
	frac = (frac + "00")[:2]
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil {
		return Cents{}, false
	}
	v := w*100 + f
	if neg {
		v = -v
	}
	return Cents{V: v, Fraction: hasFrac}, true
}

// numberRe finds numbers as people write them: "5 569,62", "5,569.62", "5569.62", "41,9", "128".
var numberRe = regexp.MustCompile(`\d{1,3}(?:[ \x{00a0}\x{202f}.,']\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?`)

// Numbers extracts the numbers of a text in hundredths. A separator followed by exactly three
// digits groups thousands; one followed by one or two digits at the end is the decimal mark.
func Numbers(text string) []Cents {
	var out []Cents
	for _, m := range numberRe.FindAllString(text, -1) {
		digits, frac := m, ""
		if i := strings.LastIndexAny(m, ".,"); i >= 0 && len(m)-i-1 <= 2 {
			digits, frac = m[:i], m[i+1:]
		}
		digits = strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, digits)
		s := digits
		if frac != "" {
			s += "." + frac
		}
		if c, ok := parseCents(s); ok {
			out = append(out, c)
		}
	}
	return out
}

// containsNumber: the exact amount (to the cent), or, when the answer rounded it, the whole units.
func containsNumber(found []Cents, want Cents) bool {
	w := abs(want.V)
	for _, f := range found {
		if f.V == w {
			return true
		}
		if !f.Fraction && want.Fraction && abs(f.V-w) < 100 && (f.V == (w+50)/100*100 || f.V == w/100*100) {
			return true
		}
	}
	return false
}

func norm(s string) string { return ingest.Collapse(ingest.Norm(s)) }

// Lookup is one tool call of a run, as the scorer sees it.
type Lookup struct {
	Tool string
	Args map[string]any
}

// Score is how a run did on a question.
type Score struct {
	ToolOK, ArgsOK, AnswerOK bool
	Missing                  []string // expected values not found in the answer
}

// OK is the question's verdict.
func (s Score) OK() bool { return s.ToolOK && s.ArgsOK && s.AnswerOK }

// Grade scores a run.
func Grade(q Question, lookups []Lookup, answer string, exp []Expected) Score {
	s := Score{ToolOK: len(q.Tools) == 0, ArgsOK: len(q.Args) == 0, AnswerOK: true}
	for _, l := range lookups {
		if slices.Contains(q.Tools, l.Tool) {
			s.ToolOK = true
		}
		for _, alt := range q.Args {
			if argsMatch(alt, l.Args) {
				s.ArgsOK = true
			}
		}
	}
	found, na := Numbers(answer), norm(answer)
	for _, e := range exp {
		ok := false
		if e.Number != nil {
			ok = containsNumber(found, *e.Number)
		} else {
			ok = strings.Contains(na, norm(e.Text))
		}
		if !ok {
			s.AnswerOK = false
			s.Missing = append(s.Missing, e.Path)
		}
	}
	if q.Mention != "" {
		hit := false
		for _, m := range strings.Split(q.Mention, "|") {
			if strings.Contains(na, norm(m)) {
				hit = true
			}
		}
		if !hit {
			s.AnswerOK = false
			s.Missing = append(s.Missing, "mention "+q.Mention)
		}
	}
	return s
}

// argsMatch: every constraint holds; "a|b" keys accept either argument; "~x" is contains.
func argsMatch(want map[string]string, got map[string]any) bool {
	for k, v := range want {
		ok := false
		for _, key := range strings.Split(k, "|") {
			g, has := got[key]
			if !has {
				continue
			}
			gs := fmt.Sprint(g)
			if strings.HasPrefix(v, "~") {
				ok = ok || strings.Contains(norm(gs), norm(v[1:]))
			} else {
				ok = ok || (len(gs) >= 10 && len(v) == 10 && gs[:10] == v) || gs == v
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// Case is one question's run.
type Case struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Lang     string   `json:"lang"`
	OK       bool     `json:"ok"`
	ToolOK   bool     `json:"toolOk"`
	ArgsOK   bool     `json:"argsOk"`
	AnswerOK bool     `json:"answerOk"`
	Missing  []string `json:"missing,omitempty"`
	Tools    []string `json:"tools"`
	Mode     string   `json:"mode,omitempty"`
	Ms       int64    `json:"ms"`
	Tokens   int      `json:"approxTokens"`
	Answer   string   `json:"answer"`
	Error    string   `json:"error,omitempty"`
}

// Run asks every question in a new chat and scores it.
func Run(ctx context.Context, a *assistant.Assistant, userID uuid.UUID, f File, req assistant.Request, progress func(Case)) ([]Case, error) {
	out := make([]Case, 0, len(f.Questions))
	for i := range f.Questions {
		q := f.Questions[i]
		exp, err := Expectations(ctx, a.Tools, userID, q)
		if err != nil {
			return nil, err
		}
		chat, err := a.Chats.Create(ctx, userID)
		if err != nil {
			return nil, err
		}
		r := req
		r.ChatID, r.Text = chat.ID, q.Question
		c := Case{ID: q.ID, Category: q.Category, Lang: q.Lang, Tools: []string{}}
		t0 := time.Now()
		run, err := a.Runner.Prepare(ctx, userID, r)
		if err != nil {
			c.Error = err.Error()
			out = append(out, c)
			progress(c)
			continue
		}
		var answer strings.Builder
		var lookups []Lookup
		run.Execute(ctx, func(e assistant.Event) {
			switch d := e.Data.(type) {
			case assistant.DeltaEvent:
				if e.Name == "content" {
					answer.WriteString(d.Delta)
				}
			case assistant.ModeEvent:
				answer.Reset() // plan mode starts the answer again
			case assistant.StepEvent:
				var args map[string]any
				_ = json.Unmarshal(d.Args, &args)
				lookups = append(lookups, Lookup{Tool: d.Tool, Args: args})
				c.Tools = append(c.Tools, d.Tool)
			case assistant.DoneEvent:
				c.Mode, c.Tokens = d.Meta.Mode, d.Meta.ApproxTokensSent
			case assistant.ErrorEvent:
				c.Error = d.Code + ": " + d.Message
			}
		})
		c.Ms = time.Since(t0).Milliseconds()
		c.Answer = answer.String()
		if c.Error == "" {
			s := Grade(q, lookups, c.Answer, exp)
			c.OK, c.ToolOK, c.ArgsOK, c.AnswerOK, c.Missing = s.OK(), s.ToolOK, s.ArgsOK, s.AnswerOK, s.Missing
		}
		out = append(out, c)
		progress(c)
	}
	return out, nil
}

// Result is one model's run of the suite, as stored in evals/assistant/results/.
type Result struct {
	Connection string    `json:"connection"`
	Model      string    `json:"model"`
	Think      string    `json:"think"`
	RanAt      time.Time `json:"ranAt"`
	Cases      []Case    `json:"cases"`
}

// Summary is a result's numbers.
type Summary struct {
	N, OK, ToolOK, ArgsOK, AnswerOK int
	P50, P95                        int64
	AvgTokens                       int
	ByCategory                      map[string][2]int // ok, n
}

// Summarize counts a result.
func Summarize(r Result) Summary {
	s := Summary{ByCategory: map[string][2]int{}}
	ms := make([]int64, 0, len(r.Cases))
	tokens := 0
	for i := range r.Cases {
		c := &r.Cases[i]
		s.N++
		ms = append(ms, c.Ms)
		tokens += c.Tokens
		bc := s.ByCategory[c.Category]
		bc[1]++
		if c.OK {
			s.OK++
			bc[0]++
		}
		s.ByCategory[c.Category] = bc
		for _, f := range []struct {
			ok bool
			n  *int
		}{{c.ToolOK, &s.ToolOK}, {c.ArgsOK, &s.ArgsOK}, {c.AnswerOK, &s.AnswerOK}} {
			if f.ok {
				*f.n++
			}
		}
	}
	if s.N > 0 {
		slices.Sort(ms)
		s.P50, s.P95 = ms[(len(ms)-1)/2], ms[min(len(ms)-1, (len(ms)*95+99)/100-1)]
		s.AvgTokens = tokens / s.N
	}
	return s
}

func pct(a, n int) string {
	if n == 0 {
		return "–"
	}
	return strconv.Itoa((a*1000/n+5)/10) + " %"
}

// Report renders docs/evals.md from every stored result.
func Report(results []Result) string {
	var b strings.Builder
	b.WriteString("# Assistant evaluations\n\n")
	b.WriteString("Generated by `make eval-assistant CONNECTION=… MODEL=…` (PLAN task 13.9) from `evals/assistant/results/`. ")
	b.WriteString("The questions are `evals/assistant/questions.json`: the demo year, today frozen at 2025-03-20. A question counts as correct ")
	b.WriteString("when a lookup used an expected tool, the arguments matched (dates especially) and the answer contains every expected number or name. ")
	b.WriteString("Expected values come from the harness's own lookups through the oracle-checked tools.\n\n")
	if len(results) == 0 {
		b.WriteString("No runs yet. Run `make eval-assistant CONNECTION=… MODEL=…` against a real model.\n")
		return b.String()
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Model < results[j].Model })
	b.WriteString("| Connection | Model | Thinking | Correct | Tool | Arguments | Answer | p50 | p95 | ≈ tokens | Run |\n|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|\n")
	cats := map[string]bool{}
	for _, r := range results {
		s := Summarize(r)
		for c := range s.ByCategory {
			cats[c] = true
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d / %d (%s) | %s | %s | %s | %.1f s | %.1f s | %d | %s |\n", r.Connection, r.Model, r.Think,
			s.OK, s.N, pct(s.OK, s.N), pct(s.ToolOK, s.N), pct(s.ArgsOK, s.N), pct(s.AnswerOK, s.N),
			float64(s.P50)/1000, float64(s.P95)/1000, s.AvgTokens, r.RanAt.Format("2006-01-02"))
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for c := range cats {
			if !yield(c) {
				return
			}
		}
	})
	b.WriteString("\n## By category\n\n| Model |")
	for _, c := range names {
		b.WriteString(" " + c + " |")
	}
	b.WriteString("\n|---|" + strings.Repeat("---:|", len(names)) + "\n")
	for _, r := range results {
		s := Summarize(r)
		b.WriteString("| " + r.Model + " |")
		for _, c := range names {
			v := s.ByCategory[c]
			fmt.Fprintf(&b, " %d / %d |", v[0], v[1])
		}
		b.WriteString("\n")
	}
	for _, r := range results {
		fmt.Fprintf(&b, "\n## Misses: %s\n\n", r.Model)
		misses := 0
		for i := range r.Cases {
			c := &r.Cases[i]
			if c.OK {
				continue
			}
			misses++
			why := c.Error
			if why == "" {
				var parts []string
				if !c.ToolOK {
					parts = append(parts, "tool")
				}
				if !c.ArgsOK {
					parts = append(parts, "arguments")
				}
				if !c.AnswerOK {
					parts = append(parts, "answer ("+strings.Join(c.Missing, ", ")+")")
				}
				why = strings.Join(parts, "; ")
			}
			fmt.Fprintf(&b, "- `%s`: %s; lookups: %s\n", c.ID, why, strings.Join(c.Tools, ", "))
		}
		if misses == 0 {
			b.WriteString("None.\n")
		}
	}
	return b.String()
}

// Think parses the thinking setting of an eval run.
func Think(s string) llm.Thinking {
	if s == "" {
		return llm.ThinkAuto
	}
	return llm.Thinking(s)
}

// LoadResults reads every stored result of a directory.
func LoadResults(dir string) ([]Result, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // the repository's results
		if err != nil {
			return nil, err
		}
		var r Result
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}
