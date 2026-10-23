package httpapi

import (
	"testing"

	"github.com/google/uuid"
)

// HTTP tests of the overview, budgets and investments (PLAN tasks 12.1, 12.2, 12.6) on the demo
// file. The cards' numbers are checked against the legacy app in internal/insights
// (TestOverviewParity, TestInvestmentCardsParity).

func TestOverviewHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	ctx := map[string]any{"accountIds": []string{accID}}

	// The latest month by default: March 2025, in progress (data up to the 17th).
	ov := b.do("POST", "/overview", map[string]any{"context": ctx, "months": 12})
	expect(t, ov, 200, "")
	d := obj(t, ov.body, "data")
	if ov.body["currency"] != "PLN" || d["month"] != "2025-03" || d["prevMonth"] != "2025-02" || d["firstMonth"] != "2024-01" ||
		d["dataMax"] != "2025-03-17" || d["partial"] != true || num(d["elapsedDays"]) != 17 || num(d["daysInMonth"]) != 31 ||
		d["projectedMinor"] == nil || num(d["avgMonths"]) != 3 {
		t.Errorf("overview header = %v", d)
	}
	if months := d["months"].([]any); len(months) != 12 || months[11].(map[string]any)["month"] != "2025-03" {
		t.Errorf("months = %v", months)
	}
	if w := d["weekday"].([]any); len(w) != 7 {
		t.Errorf("weekday = %v", w)
	}
	if hl := d["highlights"].([]any); len(hl) == 0 || hl[0].(map[string]any)["kind"] != "projection" {
		t.Errorf("highlights = %v", hl)
	}
	inv := obj(t, d, "investment")
	if rows := inv["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["ppk"] != true {
		t.Errorf("investment card without data = %v", inv)
	}
	if f := obj(t, d, "fire"); num(f["swrBp"]) != 400 || num(f["months"]) != 12 {
		t.Errorf("fire = %v", f)
	}

	// A chosen complete month; the totals match the insights summary of February.
	feb := b.do("POST", "/overview", map[string]any{"context": ctx, "month": "2025-02", "months": 6})
	expect(t, feb, 200, "")
	fd := obj(t, feb.body, "data")
	cur := obj(t, fd, "current")
	if fd["partial"] != false || fd["projectedMinor"] != nil || num(cur["expenseMinor"]) != 556962 || num(cur["incomeMinor"]) != 958000 ||
		len(fd["months"].([]any)) != 6 {
		t.Errorf("February = %v", fd)
	}
	largest := fd["largest"].([]any)
	if l := largest[0].(map[string]any); len(largest) > 40 || l["payeeGroup"] != "WSPÓLNOTA MIESZKANIOWA" || num(l["amountMinor"]) != -285000 {
		t.Errorf("largest = %v", largest)
	}

	// A month outside the data is clamped to the latest one.
	if r := b.do("POST", "/overview", map[string]any{"context": ctx, "month": "2030-01", "months": 6}); obj(t, r.body, "data")["month"] != "2025-03" {
		t.Errorf("clamped month = %s", r.raw)
	}

	// Budgets: set one, see it in the card as not the default, reset it.
	var catID, catName string
	for _, c := range fd["categories"].([]any) {
		c := c.(map[string]any)
		if ref, ok := c["category"].(map[string]any); ok && c["name"] == "Transport" {
			catID, catName = ref["id"].(string), ref["name"].(string)
			if c["budgetIsDefault"] != true {
				t.Errorf("Transport has a budget before one is set: %v", c)
			}
		}
		if c["name"] == "(uncategorized)" && c["category"] != nil {
			t.Errorf("uncategorised row with a category: %v", c)
		}
	}
	if catID == "" {
		t.Fatalf("no Transport row in %v", fd["categories"])
	}
	expect(t, b.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": 50000}), 204, "")
	list := b.do("GET", "/budgets", nil)
	expect(t, list, 200, "")
	if bs := list.body["budgets"].([]any); len(bs) != 1 || num(bs[0].(map[string]any)["amountMinor"]) != 50000 ||
		bs[0].(map[string]any)["category"].(map[string]any)["name"] != catName {
		t.Errorf("budgets = %s", list.raw)
	}
	row := func() map[string]any {
		r := b.do("POST", "/overview", map[string]any{"context": ctx, "month": "2025-02", "months": 6})
		for _, c := range obj(t, r.body, "data")["categories"].([]any) {
			if c := c.(map[string]any); c["name"] == catName {
				return c
			}
		}
		return nil
	}
	if r := row(); r["budgetIsDefault"] != false || num(r["budgetMinor"]) != 50000 {
		t.Errorf("budget row = %v", r)
	}
	expect(t, b.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": nil}), 204, "")
	expect(t, b.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": nil}), 204, "") // idempotent
	if r := row(); r["budgetIsDefault"] != true {
		t.Errorf("reset budget row = %v", r)
	}
	if l := b.do("GET", "/budgets", nil); len(l.body["budgets"].([]any)) != 0 {
		t.Errorf("budgets after reset = %s", l.raw)
	}
	expect(t, b.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": -1}), 422, "validation_failed")
	expect(t, b.do("PUT", "/budgets/"+catID, map[string]any{}), 422, "validation_failed")
	expect(t, b.do("PUT", "/budgets/"+uuid.NewString(), map[string]any{"amountMinor": 100}), 404, "not_found")

	// Investments: defaults, first save with version 0, stale save, validation.
	g := b.do("GET", "/investments", nil)
	expect(t, g, 200, "")
	if num(g.body["version"]) != 0 || num(g.body["swrBp"]) != 400 || num(g.body["realReturnBp"]) != 500 || len(g.body["positions"].([]any)) != 0 {
		t.Errorf("default investments = %s", g.raw)
	}
	pos := map[string]any{"id": uuid.NewString(), "name": " ETF World ", "type": "ETF", "investedMinor": 1000000, "valueMinor": 1200000,
		"annualIncomeMinor": 0}
	doc := map[string]any{"version": 0, "ppk": map[string]any{"employeeMinor": 100000, "employerMinor": 75000, "stateMinor": 25000,
		"valueMinor": 240000}, "swrBp": 350, "realReturnBp": 400, "positions": []any{pos}}
	s := b.do("PUT", "/investments", doc)
	expect(t, s, 200, "")
	if num(s.body["version"]) != 1 || s.body["positions"].([]any)[0].(map[string]any)["name"] != "ETF World" {
		t.Errorf("saved investments = %s", s.raw)
	}
	stale := b.do("PUT", "/investments", doc) // still version 0
	expect(t, stale, 409, "stale_version")
	if c := obj(t, stale.body, "current"); num(c["version"]) != 1 || num(c["swrBp"]) != 350 {
		t.Errorf("stale current = %s", stale.raw)
	}
	saved := obj(t, b.do("POST", "/overview", map[string]any{"context": ctx, "months": 12}).body, "data")
	if inv := obj(t, saved, "investment"); len(inv["rows"].([]any)) != 2 || num(inv["valueMinor"]) != 1440000 || num(inv["ppkPaidInMinor"]) != 200000 {
		t.Errorf("investment card = %v", inv)
	}
	if f := obj(t, saved, "fire"); num(f["swrBp"]) != 350 || num(f["assetsMinor"]) != 1440000 {
		t.Errorf("fire = %v", f)
	}
	bad := func(edit func(m map[string]any)) {
		t.Helper()
		m := map[string]any{"version": 1, "ppk": doc["ppk"], "swrBp": 400, "realReturnBp": 500, "positions": []any{}}
		edit(m)
		expect(t, b.do("PUT", "/investments", m), 422, "validation_failed")
	}
	bad(func(m map[string]any) { m["swrBp"] = 20 })
	bad(func(m map[string]any) { m["realReturnBp"] = 2500 })
	bad(func(m map[string]any) { m["positions"] = []any{pos, pos} }) // duplicate id
	bad(func(m map[string]any) {
		p := map[string]any{}
		for k, v := range pos {
			p[k] = v
		}
		p["name"] = "   "
		m["positions"] = []any{p}
	})
	bad(func(m map[string]any) {
		m["ppk"] = map[string]any{"employeeMinor": -1, "employerMinor": 0, "stateMinor": 0, "valueMinor": 0}
	})

	// Overview validation.
	expect(t, b.do("POST", "/overview", map[string]any{"context": ctx, "months": 7}), 422, "validation_failed")
	expect(t, b.do("POST", "/overview", map[string]any{"context": ctx, "months": 6, "month": "2025-13"}), 422, "validation_failed")
	expect(t, b.do("POST", "/overview", map[string]any{"context": ctx}), 422, "validation_failed")

	// Another user sees none of it, even naming my account and category.
	other := signedIn(t, e, "other-"+testEmail(t))
	o := other.do("POST", "/overview", map[string]any{"context": ctx, "months": 12})
	expect(t, o, 200, "")
	if o.body["data"] != nil || o.body["currency"] != "" {
		t.Errorf("other user's overview = %s", o.raw)
	}
	expect(t, other.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": 100}), 404, "not_found")
	expect(t, other.do("PUT", "/budgets/"+catID, map[string]any{"amountMinor": nil}), 404, "not_found")
	if l := other.do("GET", "/budgets", nil); len(l.body["budgets"].([]any)) != 0 {
		t.Errorf("other user's budgets = %s", l.raw)
	}
	if g := other.do("GET", "/investments", nil); num(g.body["version"]) != 0 || len(g.body["positions"].([]any)) != 0 {
		t.Errorf("other user's investments = %s", g.raw)
	}
	// The same position id in another user's document conflicts (ids are global) without leaking it.
	od := map[string]any{"version": 0, "ppk": doc["ppk"], "swrBp": 400, "realReturnBp": 500, "positions": []any{pos}}
	if r := other.do("PUT", "/investments", od); r.code != 409 {
		t.Errorf("other user reusing a position id = %d %s", r.code, r.raw)
	}
	if g := b.do("GET", "/investments", nil); num(g.body["version"]) != 1 || len(g.body["positions"].([]any)) != 1 {
		t.Errorf("my investments after the other user's save = %s", g.raw)
	}

	// Signed out: 401.
	anon := e.browser()
	expect(t, anon.do("POST", "/overview", map[string]any{"context": ctx, "months": 12}), 401, "unauthenticated")
	expect(t, anon.do("GET", "/budgets", nil), 401, "unauthenticated")
	expect(t, anon.do("GET", "/investments", nil), 401, "unauthenticated")
}
