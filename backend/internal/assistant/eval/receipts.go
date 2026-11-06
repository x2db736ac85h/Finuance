package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// Receipt evals (PLAN 20.8): synthetic receipts in several languages (evals/assistant/receipts/),
// each sent as an image with one instruction to read the shop, the day, the amount paid and the
// currency. They measure what the receipt flow needs from a model before any tool runs: reading
// the photo. A receipt is correct when all four match.

// ReceiptSet is evals/assistant/receipts/receipts.json.
type ReceiptSet struct {
	Receipts []Receipt `json:"receipts"`
}

// Receipt is one image and what it says.
type Receipt struct {
	ID    string            `json:"id"`
	Lang  string            `json:"lang"`
	Style map[string]any    `json:"style"`
	Truth ReceiptTruth      `json:"truth"`
	Lines []json.RawMessage `json:"lines"`
}

// ReceiptTruth is what a model should read; MerchantKey must appear in the shop's name it reads.
type ReceiptTruth struct {
	Merchant    string `json:"merchant"`
	MerchantKey string `json:"merchantKey"`
	Date        string `json:"date"`
	Total       string `json:"total"`
	Currency    string `json:"currency"`
}

// LoadReceipts reads a receipt set.
func LoadReceipts(path string) (ReceiptSet, error) {
	var s ReceiptSet
	b, err := os.ReadFile(path) //nolint:gosec // the repository's receipt set
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// ReceiptPrompt is the instruction sent with every receipt.
const ReceiptPrompt = "You read photos of shop receipts in any language. Reply with only one JSON object, no other text: " +
	`{"merchant": "the shop's name as printed", "date": "YYYY-MM-DD", "total": the amount paid as a number, "currency": "ISO 4217 code"}. ` +
	"The total is the grand total actually paid (after tax and discounts), not a subtotal, a tax line, the cash handed over or the change. " +
	"Read the date in the receipt's own convention (a US receipt writes month/day)."

// ReceiptRead is what a model answered.
type ReceiptRead struct {
	Merchant string `json:"merchant"`
	Date     string `json:"date"`
	Total    string `json:"total"`
	Currency string `json:"currency"`
}

// ParseReceiptAnswer takes the JSON object out of an answer (code fences and words around it are
// ignored); the total may be a number or text with a comma.
func ParseReceiptAnswer(text string) (ReceiptRead, bool) {
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return ReceiptRead{}, false
	}
	var raw map[string]any
	dec := json.NewDecoder(strings.NewReader(text[i : j+1]))
	dec.UseNumber() // amounts stay text, never a float
	if dec.Decode(&raw) != nil {
		return ReceiptRead{}, false
	}
	str := func(k string) string {
		switch v := raw[k].(type) {
		case string:
			return strings.TrimSpace(v)
		case json.Number:
			return v.String()
		case nil:
			return ""
		default:
			return fmt.Sprint(v)
		}
	}
	return ReceiptRead{Merchant: str("merchant"), Date: str("date"), Total: str("total"), Currency: str("currency")}, true
}

// currencySigns maps what models write instead of the code.
var currencySigns = map[string]string{"€": "EUR", "£": "GBP", "$": "USD", "US$": "USD", "ZŁ": "PLN", "ZL": "PLN", "KČ": "CZK", "KC": "CZK"}

// ReceiptScore is one receipt's fields, right or wrong.
type ReceiptScore struct {
	Merchant, Date, Total, Currency bool
}

// OK is every field right.
func (s ReceiptScore) OK() bool { return s.Merchant && s.Date && s.Total && s.Currency }

// minorOf reads an amount in major units ("54.30", "54,30", "1 234,50", "£9.30") as minor units.
func minorOf(s, currency string) (int64, bool) {
	s = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' || r == '-' {
			return r
		}
		return -1
	}, s)
	if s == "" {
		return 0, false
	}
	if k := strings.LastIndexAny(s, ".,"); k >= 0 && len(s)-k-1 <= 2 { // the last separator is the decimal one
		s = strings.NewReplacer(".", "", ",", "").Replace(s[:k]) + "." + s[k+1:]
	} else {
		s = strings.NewReplacer(".", "", ",", "").Replace(s)
	}
	exp, ok := money.Exponent(currency)
	if !ok {
		exp = money.DefaultExponent
	}
	v, err := money.ParseMinor(s, exp)
	return v, err == nil
}

// GradeReceipt compares a reading with the truth: the merchant key appears in the name (ignoring
// case, accents and spacing), the date is exact, the total is exact to the cent, and the currency
// is the code (or its sign).
func GradeReceipt(t ReceiptTruth, r ReceiptRead) ReceiptScore {
	var s ReceiptScore
	s.Merchant = r.Merchant != "" && strings.Contains(norm(r.Merchant), norm(t.MerchantKey))
	s.Date = r.Date == t.Date
	want, _ := minorOf(t.Total, t.Currency)
	got, ok := minorOf(r.Total, t.Currency)
	s.Total = ok && got == want
	cur := strings.ToUpper(strings.TrimSpace(r.Currency))
	if c, ok := currencySigns[cur]; ok {
		cur = c
	}
	s.Currency = cur == t.Currency
	return s
}

// ReceiptCase is one receipt's run.
type ReceiptCase struct {
	ID     string      `json:"id"`
	Lang   string      `json:"lang"`
	OK     bool        `json:"ok"`
	Fields []string    `json:"wrong,omitempty"` // the fields read wrong
	Read   ReceiptRead `json:"read"`
	Ms     int64       `json:"ms"`
	Tokens int         `json:"tokens"`
	Error  string      `json:"error,omitempty"`
}

// ReceiptResult is a receipt run of one model.
type ReceiptResult struct {
	Connection string        `json:"connection"`
	Model      string        `json:"model"`
	Think      string        `json:"think"`
	RanAt      time.Time     `json:"ranAt"`
	Cases      []ReceiptCase `json:"cases"`
}

// FileName is where a receipt run is stored (results/receipts/).
func (r ReceiptResult) FileName() string {
	return strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(fmt.Sprintf("%s__%s__%s", r.Connection, r.Model, r.Think)) + ".json"
}

// RunReceipts sends every receipt of the set (images read from dir) to the model, one at a time.
func RunReceipts(ctx context.Context, client llm.Client, base llm.Request, set ReceiptSet, dir string, progress func(ReceiptCase)) ([]ReceiptCase, error) {
	out := make([]ReceiptCase, 0, len(set.Receipts))
	for i := range set.Receipts {
		rc := &set.Receipts[i]
		img, err := os.ReadFile(filepath.Join(dir, rc.ID+".jpg")) //nolint:gosec // the repository's receipts
		if err != nil {
			return nil, err
		}
		req := base
		req.System = ReceiptPrompt
		req.Messages = []llm.Message{{Role: llm.RoleUser, Content: "Read this receipt.", Images: []llm.Image{{MIME: "image/jpeg", Data: img}}}}
		c := ReceiptCase{ID: rc.ID, Lang: rc.Lang}
		t0 := time.Now()
		res, err := client.Chat(ctx, req, nil)
		c.Ms = time.Since(t0).Milliseconds()
		if err != nil {
			c.Error = err.Error()
		} else {
			c.Tokens = res.Usage.Output
			read, ok := ParseReceiptAnswer(res.Content)
			if !ok {
				c.Error = "no JSON object in the answer"
			}
			c.Read = read
			s := GradeReceipt(rc.Truth, read)
			c.OK = ok && s.OK()
			for _, f := range []struct {
				ok   bool
				name string
			}{{s.Merchant, "merchant"}, {s.Date, "date"}, {s.Total, "total"}, {s.Currency, "currency"}} {
				if !f.ok {
					c.Fields = append(c.Fields, f.name)
				}
			}
		}
		out = append(out, c)
		progress(c)
	}
	return out, nil
}

// LoadReceiptResults reads every receipt run in dir (none when it doesn't exist).
func LoadReceiptResults(dir string) ([]ReceiptResult, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []ReceiptResult
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // the repository's results
		if err != nil {
			return nil, err
		}
		var r ReceiptResult
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// ReceiptReport is the receipts section of docs/evals.md.
func ReceiptReport(results []ReceiptResult) string {
	var b strings.Builder
	b.WriteString("\n# Receipts\n\n")
	b.WriteString("Generated by `make eval-receipts CONNECTION=… MODEL=…` (PLAN task 20.8) from `evals/assistant/results/receipts/`. ")
	b.WriteString("Each synthetic receipt of `evals/assistant/receipts/` (invented shops; English, German, French, Polish, Spanish, Italian, Czech; ")
	b.WriteString("some rotated, noisy, blurred or faded) goes to the model as an image with one instruction; it counts as correct when the shop, ")
	b.WriteString("the date, the amount paid and the currency are all read right.\n\n")
	if len(results) == 0 {
		b.WriteString("No runs yet.\n")
		return b.String()
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Model < results[j].Model })
	b.WriteString("| Connection | Model | Thinking | Correct | Shop | Date | Total | Currency | p50 | p95 | Run |\n|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|\n")
	langs := map[string]bool{}
	for _, r := range results {
		n := len(r.Cases)
		var ok int
		wrong := map[string]int{}
		ms := make([]int64, 0, n)
		for i := range r.Cases {
			c := &r.Cases[i]
			langs[c.Lang] = true
			if c.OK {
				ok++
			}
			if c.Error != "" && len(c.Fields) == 0 {
				for _, f := range []string{"merchant", "date", "total", "currency"} {
					wrong[f]++
				}
			}
			for _, f := range c.Fields {
				wrong[f]++
			}
			ms = append(ms, c.Ms)
		}
		slices.Sort(ms)
		fmt.Fprintf(&b, "| %s | %s | %s | %d / %d (%s) | %s | %s | %s | %s | %.1f s | %.1f s | %s |\n", r.Connection, r.Model, r.Think,
			ok, n, pct(ok, n), pct(n-wrong["merchant"], n), pct(n-wrong["date"], n), pct(n-wrong["total"], n), pct(n-wrong["currency"], n),
			float64(quantile(ms, 50))/1000, float64(quantile(ms, 95))/1000, r.RanAt.Format("2006-01-02"))
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for l := range langs {
			if !yield(l) {
				return
			}
		}
	})
	b.WriteString("\n## Receipts by language\n\n| Model |")
	for _, l := range names {
		b.WriteString(" " + l + " |")
	}
	b.WriteString("\n|---|" + strings.Repeat("---:|", len(names)) + "\n")
	for _, r := range results {
		b.WriteString("| " + r.Model + " |")
		for _, l := range names {
			var ok, n int
			for i := range r.Cases {
				if c := &r.Cases[i]; c.Lang == l {
					n++
					if c.OK {
						ok++
					}
				}
			}
			fmt.Fprintf(&b, " %d / %d |", ok, n)
		}
		b.WriteString("\n")
	}
	for _, r := range results {
		fmt.Fprintf(&b, "\n## Receipts misread: %s\n\n", r.Model)
		misses := 0
		for i := range r.Cases {
			c := &r.Cases[i]
			if c.OK {
				continue
			}
			misses++
			if c.Error != "" {
				fmt.Fprintf(&b, "- `%s`: %s\n", c.ID, c.Error)
				continue
			}
			fmt.Fprintf(&b, "- `%s`: %s (read %q, %s, %s %s)\n", c.ID, strings.Join(c.Fields, ", "), c.Read.Merchant, c.Read.Date, c.Read.Total, c.Read.Currency)
		}
		if misses == 0 {
			b.WriteString("None.\n")
		}
	}
	return b.String()
}

// quantile of sorted values (nearest rank).
func quantile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted)*p + 99) / 100
	return sorted[max(0, min(len(sorted)-1, i-1))]
}
