package httpapi

import (
	"testing"
)

// HTTP tests of the insight endpoints (PLAN task 11.2) on the demo year. The numbers themselves
// are checked against the legacy charts in internal/insights (TestChartParity).

func TestInsightsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	ctx := map[string]any{"accountIds": []string{accID}, "period": map[string]any{"month": "2025-02"}}

	sum := b.do("POST", "/insights/summary", map[string]any{"context": ctx})
	expect(t, sum, 200, "")
	if num(sum.body["spentMinor"]) != 556962 || num(sum.body["incomeMinor"]) != 958000 || num(sum.body["count"]) != 50 ||
		num(sum.body["days"]) != 28 || sum.body["currency"] != "PLN" || sum.body["period"].(map[string]any)["kind"] != "month" {
		t.Errorf("summary = %s", sum.raw)
	}
	if le := obj(t, sum.body, "largestExpense"); le["payee"] != "WSPÓLNOTA MIESZKANIOWA" || le["bookedOn"] != "2025-02-02" || num(le["amountMinor"]) != -285000 {
		t.Errorf("largest expense = %v", le)
	}
	if d, ok := sum.body["spentDelta"].(float64); !ok || d > -0.16 || d < -0.18 {
		t.Errorf("spentDelta = %v", sum.body["spentDelta"])
	}

	cat := b.do("POST", "/insights/categories", map[string]any{"context": ctx})
	expect(t, cat, 200, "")
	cats := cat.body["categories"].([]any)
	first := cats[0].(map[string]any)
	if len(cats) != 9 || first["category"].(map[string]any)["name"] != "Mieszkanie" || num(first["colorSlot"]) != 1 || num(cat.body["totalMinor"]) != 556962 {
		t.Errorf("categories = %s", cat.raw)
	}

	flow := b.do("POST", "/insights/cashflow", map[string]any{"context": ctx})
	expect(t, flow, 200, "")
	months := flow.body["months"].([]any)
	if len(months) != 15 {
		t.Fatalf("cashflow months = %d", len(months))
	}
	for _, m := range months {
		m := m.(map[string]any)
		if (m["month"] == "2025-02") != m["inPeriod"].(bool) {
			t.Errorf("month %v inPeriod %v", m["month"], m["inPeriod"])
		}
	}

	pace := b.do("POST", "/insights/pace", map[string]any{"context": ctx, "compare": "yoy"})
	expect(t, pace, 200, "")
	cmp := obj(t, pace.body, "compare")
	if pace.body["target"] != "2025-02" || num(pace.body["daysInMonth"]) != 28 || num(pace.body["lastDay"]) != 28 ||
		len(pace.body["current"].([]any)) != 31 || cmp["month"] != "2024-02" || len(cmp["values"].([]any)) != 31 {
		t.Errorf("pace = %s", pace.raw)
	}

	pay := b.do("POST", "/insights/payees", map[string]any{"context": ctx, "top": 10, "scope": "period"})
	expect(t, pay, 200, "")
	if len(pay.body["payees"].([]any)) != 10 || num(pay.body["total"]) != 16 {
		t.Errorf("payees = %s", pay.raw)
	}

	daily := b.do("POST", "/insights/daily", map[string]any{"context": ctx, "range": "3m"})
	expect(t, daily, 200, "")
	if daily.body["from"] != "2025-01-01" || daily.body["to"] != "2025-03-18" || len(daily.body["days"].([]any)) != 75 {
		t.Errorf("daily = %s", daily.raw)
	}

	// Validation: enums, unknown columns, a bad period.
	expect(t, b.do("POST", "/insights/pace", map[string]any{"context": ctx, "compare": "weekly"}), 422, "validation_failed")
	expect(t, b.do("POST", "/insights/payees", map[string]any{"context": ctx, "top": 7, "scope": "period"}), 422, "validation_failed")
	expect(t, b.do("POST", "/insights/daily", map[string]any{"context": ctx, "range": "2w"}), 422, "validation_failed")
	expect(t, b.do("POST", "/insights/summary", map[string]any{"context": map[string]any{
		"filters": []any{map[string]any{"column": "nope", "cond": "x"}}}}), 422, "validation_failed")
	expect(t, b.do("POST", "/insights/summary", map[string]any{"context": map[string]any{"period": map[string]any{"month": "2025-13"}}}), 422,
		"validation_failed")

	// Another user sees none of it, even naming my account.
	other := signedIn(t, e, "other-"+testEmail(t))
	os := other.do("POST", "/insights/summary", map[string]any{"context": ctx})
	expect(t, os, 200, "")
	if num(os.body["count"]) != 0 || num(os.body["spentMinor"]) != 0 || os.body["largestExpense"] != nil || os.body["currency"] != "" {
		t.Errorf("other user's summary = %s", os.raw)
	}
	for _, path := range []string{"/insights/cashflow", "/insights/categories"} {
		r := other.do("POST", path, map[string]any{"context": ctx})
		expect(t, r, 200, "")
		for _, k := range []string{"months", "categories"} {
			if l, ok := r.body[k].([]any); ok && len(l) != 0 {
				t.Errorf("other user's %s = %s", path, r.raw)
			}
		}
	}
	if r := other.do("POST", "/insights/pace", map[string]any{"context": ctx, "compare": "prev"}); r.body["target"] != nil {
		t.Errorf("other user's pace = %s", r.raw)
	}

	// Signed out: 401.
	anon := e.browser()
	expect(t, anon.do("POST", "/insights/summary", map[string]any{"context": ctx}), 401, "unauthenticated")
}
