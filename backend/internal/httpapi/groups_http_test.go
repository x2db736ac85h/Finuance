package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
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
