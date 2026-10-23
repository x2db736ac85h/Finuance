package assistant

import (
	"encoding/json"
	"testing"
)

func TestArgsJSSemantics(t *testing.T) {
	for _, c := range []struct {
		raw   string
		limit int
	}{
		{`{}`, 25}, {`{"limit":5}`, 5}, {`{"limit":0}`, 25}, {`{"limit":1000}`, 50}, {`{"limit":-3}`, 1},
		{`{"limit":"7"}`, 7}, {`{"limit":" 8 "}`, 8}, {`{"limit":""}`, 25}, {`{"limit":"lots"}`, 25}, {`{"limit":2.9}`, 2},
		{`{"limit":"1e1"}`, 10}, {`{"limit":null}`, 25}, {`{"limit":true}`, 1}, {`not json`, 25}, {`null`, 25}, {`[1]`, 25}, {`"x"`, 25},
	} {
		if got := parseArgs(c.raw).count("limit", 25, 50); got != c.limit {
			t.Errorf("%s: limit %d, want %d", c.raw, got, c.limit)
		}
	}
	a := parseArgs(`{"query":"kino","payee":"","category":0,"group":"Remont","type":"any","date_from":"2025-03-01","min_amount":"50","max_amount":{"x":1}}`)
	if l := a.label(); l != "query: kino · group: Remont · 2025-03-01 → …" {
		t.Errorf("label = %q", l)
	}
	f := a.filter()
	if f.Query != "kino" || f.Payee != "" || f.Category != "" || f.Group != "Remont" || f.Type != "any" || f.DateFrom.String() != "2025-03-01" ||
		f.DateTo != nil || *f.MinAmount != "50" || *f.MaxAmount != "NaN" {
		t.Errorf("filter = %+v", f)
	}
	if parseArgs(`{"type":"expense","min_amount":-100}`).label() != "type: expense" {
		t.Error("label with amounts only")
	}
	if parseArgs(`{"category":5}`).filter().Category != "5" {
		t.Error("a number is written as text")
	}
}

func TestAmountAndKeys(t *testing.T) {
	for _, c := range []struct {
		minor int64
		cur   string
		want  json.Number
	}{{-9807, "PLN", "-98.07"}, {-2740, "PLN", "-27.4"}, {14119700, "PLN", "141197"}, {0, "PLN", "0"}, {5, "PLN", "0.05"}, {-150000, "EUR", "-1500"}} {
		if got := amount(c.minor, c.cur); got != c.want {
			t.Errorf("amount(%d, %s) = %s, want %s", c.minor, c.cur, got, c.want)
		}
	}
	for _, c := range []struct{ by, key, want string }{
		{"category", "", "(uncategorized)"}, {"group", "", "(no group)"}, {"week", "2025-02-03", "week of 2025-02-03"},
		{"weekday", "1", "Monday"}, {"weekday", "7", "Sunday"}, {"payee", "", ""}, {"bogus", "all", "all"},
	} {
		if got := groupKey(c.by, c.key); got != c.want {
			t.Errorf("groupKey(%s, %q) = %q", c.by, c.key, got)
		}
	}
	if MaskDigits("ACC 12345678901234567890123456 card *****0229 ref 123456") != "ACC 12… card *****0229 ref 12…" {
		t.Error(MaskDigits("ACC 12345678901234567890123456 card *****0229 ref 123456"))
	}
	if clip("zażółć gęślą", 6) != "zażółć" {
		t.Error(clip("zażółć gęślą", 6))
	}
}
