package httpapi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// HTTP tests of groups and bulk labelling (PLAN task 10.10) on the demo year.

func groupList(t *testing.T, b *browser) []map[string]any {
	t.Helper()
	r := b.do("GET", "/groups", nil)
	expect(t, r, 200, "")
	var out []map[string]any
	if err := json.Unmarshal([]byte(r.raw), &out); err != nil {
		t.Fatalf("groups = %s", r.raw)
	}
	return out
}

func idsWhere(t *testing.T, b *browser, cond map[string]any, limit int) []string {
	t.Helper()
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"filters": []any{cond}}, "limit": limit})
	expect(t, q, 200, "")
	var ids []string
	for _, r := range rowsOf(t, q) {
		ids = append(ids, r["id"].(string))
	}
	return ids
}

func totalWhere(t *testing.T, b *browser, cond map[string]any) int {
	t.Helper()
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"filters": []any{cond}}, "limit": 1})
	expect(t, q, 200, "")
	return num(q.body["total"])
}

func TestGroupsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Demo"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)
	demo := fixture(t, "demo/demo-legacy.csv")
	up := b.upload("", "demo.csv", demo, accID)
	expect(t, up, 201, "")
	impID := obj(t, up.body, "import")["id"].(string)
	b.commit(impID)

	// Create: the first free colour, empty stats; names are unique ignoring case and accents.
	g := b.do("POST", "/groups", map[string]any{"name": " Italy trip "})
	expect(t, g, 201, "")
	gid := g.body["id"].(string)
	if g.body["name"] != "Italy trip" || num(g.body["color"]) != 0 || num(g.body["count"]) != 0 ||
		obj(t, g.body, "spent")["currency"] != "PLN" || g.body["firstOn"] != nil {
		t.Fatalf("created = %s", g.raw)
	}
	expect(t, b.do("POST", "/groups", map[string]any{"name": "ITALY TRIP"}), 409, "conflict")
	g2 := b.do("POST", "/groups", map[string]any{"name": "Ślub", "color": 5})
	expect(t, g2, 201, "")
	expect(t, b.do("POST", "/groups", map[string]any{"name": "x", "color": 8}), 422, "validation_failed")
	expect(t, b.do("POST", "/groups", map[string]any{"name": ""}), 422, "validation_failed")
	expect(t, b.do("POST", "/groups", map[string]any{"name": strings.Repeat("x", 41)}), 422, "validation_failed")
	if third := b.do("POST", "/groups", map[string]any{"name": "Dom"}); num(third.body["color"]) != 1 {
		t.Errorf("next colour = %s", third.raw)
	}

	// Label the 14 internet bills; the group filter shows exactly them, and the stats add up.
	bills := idsWhere(t, b, map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}, 50)
	if len(bills) != 14 {
		t.Fatalf("%d bills", len(bills))
	}
	bu := b.do("POST", "/transactions/bulk", map[string]any{"ids": bills, "set": map[string]any{"groupId": gid}})
	expect(t, bu, 200, "")
	if num(bu.body["updated"]) != 14 {
		t.Errorf("bulk = %s", bu.raw)
	}
	if n := totalWhere(t, b, map[string]any{"column": "group", "cond": "=Italy trip"}); n != 14 {
		t.Errorf("group filter = %d", n)
	}
	var italy map[string]any
	for _, x := range groupList(t, b) {
		if x["id"] == gid {
			italy = x
		}
	}
	if num(italy["count"]) != 14 || num(obj(t, italy, "spent")["amountMinor"]) != 14*7999 || num(obj(t, italy, "income")["amountMinor"]) != 0 ||
		italy["firstOn"] != "2024-01-20" || italy["lastOn"] != "2025-02-20" {
		t.Errorf("stats = %v", italy)
	}

	// Range selection ids carry the group (the quick bar's Ungroup).
	rg := b.do("POST", "/transactions/ids", map[string]any{"context": map[string]any{"filters": []any{
		map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}}}, "limit": 50})
	expect(t, rg, 200, "")
	var ranged []map[string]any
	_ = json.Unmarshal([]byte(rg.raw), &ranged)
	if len(ranged) != 14 || ranged[0]["groupId"] != gid || num(ranged[0]["amountMinor"]) != -7999 {
		t.Errorf("ids = %s", rg.raw)
	}

	// The Group column exports like any other.
	ex := b.do("POST", "/transactions/export", map[string]any{
		"context": map[string]any{"filters": []any{map[string]any{"column": "group", "cond": "=Italy trip"}}},
		"columns": []string{"payee", "group"}, "view": "Italy"})
	expect(t, ex, 200, "")
	if lines := strings.Split(strings.TrimPrefix(ex.raw, "\ufeff"), "\r\n"); len(lines) != 15 || lines[1] != "OPERATOR INTERNETU;Italy trip" {
		t.Errorf("export = %q", lines[:2])
	}

	// Rename keeps the colour; ungroup with null.
	rn := b.do("PATCH", "/groups/"+gid, map[string]any{"name": "Włochy 2025"})
	expect(t, rn, 200, "")
	if rn.body["name"] != "Włochy 2025" || num(rn.body["color"]) != 0 || num(rn.body["count"]) != 14 {
		t.Errorf("renamed = %s", rn.raw)
	}
	expect(t, b.do("PATCH", "/groups/"+gid, map[string]any{"name": "ślub"}), 409, "conflict")
	un := b.do("POST", "/transactions/bulk", map[string]any{"ids": bills[:4], "set": map[string]any{"groupId": nil}})
	expect(t, un, 200, "")
	if n := totalWhere(t, b, map[string]any{"column": "group", "cond": "=Włochy 2025"}); n != 10 {
		t.Errorf("after ungroup = %d", n)
	}

	// Labels survive deleting and re-importing the file (annotations).
	expect(t, b.do("DELETE", "/imports/"+impID, nil), 204, "")
	if n := totalWhere(t, b, map[string]any{"column": "group", "cond": "!="}); n != 0 {
		t.Errorf("after delete = %d grouped", n)
	}
	again := b.upload("", "demo.csv", demo, accID)
	expect(t, again, 201, "")
	b.commit(obj(t, again.body, "import")["id"].(string))
	if n := totalWhere(t, b, map[string]any{"column": "group", "cond": "=Włochy 2025"}); n != 10 {
		t.Errorf("after re-import = %d", n)
	}

	// Validation.
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": []string{}, "set": map[string]any{"groupId": gid}}), 422, "validation_failed")
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": bills[:1], "set": map[string]any{}}), 422, "validation_failed")
	many := make([]string, 5001)
	for i := range many {
		many[i] = bills[0]
	}
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": many, "set": map[string]any{"groupId": gid}}), 422, "validation_failed")

	// Another user: no groups, can't label my rows, use my group or touch it.
	other := signedIn(t, e, "other-"+testEmail(t))
	if l := groupList(t, other); len(l) != 0 {
		t.Errorf("other user's groups = %v", l)
	}
	og := other.do("POST", "/groups", map[string]any{"name": "Italy trip"}) // names are per user
	expect(t, og, 201, "")
	mine := idsWhere(t, b, map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}, 50)
	ob := other.do("POST", "/transactions/bulk", map[string]any{"ids": mine, "set": map[string]any{"groupId": og.body["id"]}})
	expect(t, ob, 200, "")
	if num(ob.body["updated"]) != 0 {
		t.Errorf("other user's bulk = %s", ob.raw)
	}
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": mine, "set": map[string]any{"groupId": og.body["id"]}}), 404, "not_found")
	expect(t, other.do("PATCH", "/groups/"+gid, map[string]any{"name": "x"}), 404, "not_found")
	expect(t, other.do("DELETE", "/groups/"+gid, nil), 404, "not_found")

	// Delete removes labels only.
	expect(t, b.do("DELETE", "/groups/"+gid, nil), 204, "")
	expect(t, b.do("DELETE", "/groups/"+gid, nil), 404, "not_found")
	if n := totalWhere(t, b, map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}); n != 14 {
		t.Errorf("bills after delete = %d", n)
	}
	if n := totalWhere(t, b, map[string]any{"column": "group", "cond": "!="}); n != 0 {
		t.Errorf("grouped after delete = %d", n)
	}
	if l := groupList(t, b); len(l) != 2 {
		t.Errorf("groups left = %v", l)
	}
}

// The group window (PLAN 15.9): a budget on the group, the report, and the export by group id.
func TestGroupReportHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Demo"})
	expect(t, acc, 201, "")
	b.commit(obj(t, b.upload("", "demo.csv", fixture(t, "demo/demo-legacy.csv"), acc.body["id"].(string)).body, "import")["id"].(string))

	g := b.do("POST", "/groups", map[string]any{"name": "Trip", "budgetMinor": 150000})
	expect(t, g, 201, "")
	gid := g.body["id"].(string)
	if num(g.body["budgetMinor"]) != 150000 {
		t.Fatalf("created = %s", g.raw)
	}
	empty := b.do("GET", "/groups/"+gid+"/report", nil)
	expect(t, empty, 200, "")
	if num(empty.body["days"]) != 1 || num(empty.body["avgPerDayMinor"]) != 0 || len(empty.body["daily"].([]any)) != 0 ||
		len(empty.body["categories"].([]any)) != 0 || num(empty.body["payeeCount"]) != 0 {
		t.Errorf("empty report = %s", empty.raw)
	}

	bills := idsWhere(t, b, map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}, 50)
	big := idsWhere(t, b, map[string]any{"column": "amount", "cond": "<-1000"}, 3)
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": append(bills, big...), "set": map[string]any{"groupId": gid}}), 200, "")

	r := b.do("GET", "/groups/"+gid+"/report", nil)
	expect(t, r, 200, "")
	grp := obj(t, r.body, "group")
	spent := num(obj(t, grp, "spent")["amountMinor"])
	if num(grp["count"]) != 17 || num(grp["budgetMinor"]) != 150000 || r.body["currency"] != "PLN" {
		t.Fatalf("report group = %s", r.raw)
	}
	first, _ := time.Parse(time.DateOnly, grp["firstOn"].(string))
	last, _ := time.Parse(time.DateOnly, grp["lastOn"].(string))
	days := int(last.Sub(first).Hours()/24) + 1
	if num(r.body["days"]) != days || num(r.body["avgPerDayMinor"]) != int(math.Round(float64(spent)/float64(days))) {
		t.Errorf("days %v, avg %v; want %d days of %d", r.body["days"], r.body["avgPerDayMinor"], days, spent)
	}
	sum := func(list []any) (s int) {
		for _, x := range list {
			s += num(x.(map[string]any)["spentMinor"])
		}
		return s
	}
	daily, cats, payees := r.body["daily"].([]any), r.body["categories"].([]any), r.body["payees"].([]any)
	if sum(daily) != spent || sum(cats) != spent || daily[0].(map[string]any)["date"] != grp["firstOn"] {
		t.Errorf("daily %d, categories %d, spent %d", sum(daily), sum(cats), spent)
	}
	if num(r.body["payeeCount"]) < 2 || len(payees) > 10 {
		t.Errorf("payees = %v", payees)
	}
	for _, p := range payees {
		if p := p.(map[string]any); p["payee"] == "OPERATOR INTERNETU" && (num(p["count"]) != 14 || num(p["spentMinor"]) != 14*7999) {
			t.Errorf("internet = %v", p)
		}
	}

	// The export of the group window, by id.
	ex := b.do("POST", "/transactions/export", map[string]any{"context": map[string]any{"groupId": gid, "includeTransfers": true},
		"columns": []string{"payee", "group"}})
	expect(t, ex, 200, "")
	if lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(ex.raw, "\ufeff"), "\r\n"), "\r\n"); len(lines) != 18 {
		t.Errorf("export = %d lines", len(lines))
	}

	// The budget: an update without it keeps it, 0 removes it, a negative one is refused.
	up := b.do("PATCH", "/groups/"+gid, map[string]any{"name": "Trip 2025"})
	expect(t, up, 200, "")
	if num(up.body["budgetMinor"]) != 150000 {
		t.Errorf("kept = %s", up.raw)
	}
	up = b.do("PATCH", "/groups/"+gid, map[string]any{"name": "Trip 2025", "budgetMinor": 90000})
	if num(up.body["budgetMinor"]) != 90000 {
		t.Errorf("changed = %s", up.raw)
	}
	up = b.do("PATCH", "/groups/"+gid, map[string]any{"name": "Trip 2025", "budgetMinor": 0})
	expect(t, up, 200, "")
	if _, ok := up.body["budgetMinor"]; ok {
		t.Errorf("removed = %s", up.raw)
	}
	expect(t, b.do("PATCH", "/groups/"+gid, map[string]any{"name": "Trip 2025", "budgetMinor": -1}), 422, "validation_failed")

	// Unknown and other users' groups.
	expect(t, b.do("GET", "/groups/"+uuid.NewString()+"/report", nil), 404, "not_found")
	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("GET", "/groups/"+gid+"/report", nil), 404, "not_found")
	expect(t, other.do("PATCH", "/groups/"+gid, map[string]any{"name": "Mine", "budgetMinor": 5}), 404, "not_found")
	oex := other.do("POST", "/transactions/export", map[string]any{"context": map[string]any{"groupId": gid}, "columns": []string{"payee"}})
	expect(t, oex, 200, "")
	if lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(oex.raw, "\ufeff"), "\r\n"), "\r\n"); len(lines) != 1 {
		t.Errorf("another user's export by group id = %d lines", len(lines))
	}
}
