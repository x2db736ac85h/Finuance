package httpapi

import (
	"slices"
	"strings"
	"testing"
)

// HTTP tests of the duplicates window (PLAN task 10.13) on the duplicate-versions fixture set: the
// same four transactions in two exports, re-labelled by the bank in the newer one. Counts follow the
// legacy oracle (testdata/oracle/dedupe/dup-versions.json): 4 groups and 4 rows skipped, none
// skipped with Keep both.

func TestDuplicatesHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Konto"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)
	var imports []string
	for _, f := range []string{"v1_jan-feb.csv", "v2_jan-mar.csv"} {
		up := b.upload("", f, fixture(t, "synthetic/dup-versions/"+f), accID)
		expect(t, up, 201, "")
		id := obj(t, up.body, "import")["id"].(string)
		b.commit(id)
		imports = append(imports, id)
	}
	v1, v2 := imports[0], imports[1]

	totals := func() (int, int) {
		t.Helper()
		q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{}, "limit": 1})
		expect(t, q, 200, "")
		return num(q.body["total"]), num(q.body["duplicatesSkipped"])
	}
	if n, d := totals(); n != 7 || d != 4 {
		t.Fatalf("total %d, skipped %d; want 7, 4", n, d)
	}

	list := func(query string) resp {
		t.Helper()
		r := b.do("GET", "/duplicates"+query, nil)
		expect(t, r, 200, "")
		return r
	}
	groupsOf := func(r resp) []map[string]any {
		var out []map[string]any
		for _, g := range r.body["groups"].([]any) {
			out = append(out, g.(map[string]any))
		}
		return out
	}

	// Four groups, newest first, each with both versions; the first import's rows are in the data.
	page := list("")
	gs := groupsOf(page)
	if num(page.body["total"]) != 4 || len(gs) != 4 {
		t.Fatalf("page = %s", page.raw)
	}
	var dates []string
	for _, g := range gs {
		dates = append(dates, obj(t, g, "keyFields")["bookedOn"].(string))
	}
	if !slices.Equal(dates, []string{"2025-02-28", "2025-02-14", "2025-02-02", "2025-01-15"}) {
		t.Errorf("order = %v", dates)
	}
	alfa := gs[0]
	vers := alfa["versions"].([]any)
	first, second := vers[0].(map[string]any), vers[1].(map[string]any)
	row := func(v map[string]any) map[string]any { return v["rows"].([]any)[0].(map[string]any) }
	if alfa["accountId"] != accID || len(alfa["fingerprint"].(string)) != 64 || alfa["override"] != nil ||
		first["importId"] != v1 || first["filename"] != "v1_jan-feb.csv" || second["importId"] != v2 ||
		row(first)["inData"] != true || row(second)["inData"] != false || len(row(first)["raw"].([]any)) != 12 {
		t.Fatalf("group = %v", alfa)
	}
	if d := alfa["differingFields"].([]any); len(d) != 1 || d[0] != "bankCategory" {
		t.Errorf("differing (Sklep Alfa) = %v", d)
	}
	if d := gs[2]["differingFields"].([]any); len(d) != 1 || d[0] != "txType" { // PRZELEW MOBILE → PRZELEW
		t.Errorf("differing (Wspólnota) = %v", d)
	}

	// Filter and paging.
	if f := list("?q=kwiaciarnia"); num(f.body["total"]) != 1 || obj(t, groupsOf(f)[0], "keyFields")["payee"] != "KWIACIARNIA" {
		t.Errorf("q = %s", f.raw)
	}
	if f := list("?q=prezenty%20kwiaty"); num(f.body["total"]) != 1 { // words from both versions' rows
		t.Errorf("q (two words) = %s", f.raw)
	}
	if p2 := list("?offset=3&limit=2"); num(p2.body["total"]) != 4 || len(groupsOf(p2)) != 1 {
		t.Errorf("page 2 = %s", p2.raw)
	}
	other := b.do("POST", "/accounts", map[string]any{"name": "Inne"})
	if f := list("?accountIds=" + other.body["id"].(string)); num(f.body["total"]) != 0 {
		t.Errorf("other account = %s", f.raw)
	}

	category := func(ref string) string {
		t.Helper()
		q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"filters": []any{
			map[string]any{"column": "reference", "cond": "=" + ref}}}, "limit": 5})
		rows := rowsOf(t, q)
		if len(rows) != 1 {
			t.Fatalf("%s: %d rows", ref, len(rows))
		}
		return rows[0]["category"].(map[string]any)["name"].(string)
	}
	if c := category("D000000000000005"); c != "Bez kategorii" {
		t.Fatalf("default version category = %q", c)
	}

	// Use this version: the newer export's category shows; nothing else changes.
	path := "/duplicates/" + accID + "/" + alfa["fingerprint"].(string)
	pr := b.do("PUT", path, map[string]any{"mode": "prefer", "importId": v2})
	expect(t, pr, 200, "")
	pv := pr.body["versions"].([]any)
	if obj(t, pr.body, "override")["importId"] != v2 || row(pv[0].(map[string]any))["inData"] != false || row(pv[1].(map[string]any))["inData"] != true {
		t.Errorf("prefer = %s", pr.raw)
	}
	if c := category("D000000000000005"); c != "Artykuły spożywcze" {
		t.Errorf("preferred version category = %q", c)
	}
	if n, d := totals(); n != 7 || d != 4 {
		t.Errorf("after prefer: total %d, skipped %d", n, d)
	}

	// Keep both: two transactions; Merge again: one.
	kpath := "/duplicates/" + accID + "/" + gs[1]["fingerprint"].(string)
	kb := b.do("PUT", kpath, map[string]any{"mode": "keep_both"})
	expect(t, kb, 200, "")
	kv := kb.body["versions"].([]any)
	if obj(t, kb.body, "override")["mode"] != "keep_both" || row(kv[0].(map[string]any))["inData"] != true || row(kv[1].(map[string]any))["inData"] != true {
		t.Errorf("keep both = %s", kb.raw)
	}
	if n, d := totals(); n != 8 || d != 3 {
		t.Errorf("after keep both: total %d, skipped %d", n, d)
	}
	if f := list(""); num(f.body["total"]) != 4 { // a decided group stays listed
		t.Errorf("after keep both: %s", f.raw)
	}
	mr := b.do("DELETE", kpath, nil)
	expect(t, mr, 200, "")
	if mr.body["override"] != nil {
		t.Errorf("merge again = %s", mr.raw)
	}
	if n, d := totals(); n != 7 || d != 4 {
		t.Errorf("after merge again: total %d, skipped %d", n, d)
	}
	expect(t, b.do("DELETE", kpath, nil), 404, "not_found")

	// Errors.
	zero := strings.Repeat("0", 64)
	expect(t, b.do("PUT", "/duplicates/"+accID+"/zz", map[string]any{"mode": "keep_both"}), 422, "validation_failed")
	expect(t, b.do("PUT", "/duplicates/"+accID+"/"+zero, map[string]any{"mode": "keep_both"}), 404, "not_found")
	expect(t, b.do("PUT", kpath, map[string]any{"mode": "prefer"}), 422, "validation_failed")
	expect(t, b.do("PUT", kpath, map[string]any{"mode": "keep_both", "importId": v1}), 422, "validation_failed")
	expect(t, b.do("PUT", kpath, map[string]any{"mode": "merge"}), 422, "validation_failed")
	expect(t, b.do("GET", "/duplicates?limit=61", nil), 422, "validation_failed")

	// Each decision is in the activity log (the failed requests are not).
	var decided, reset int
	for _, a := range actions(t, b) {
		switch a {
		case "duplicates.decided":
			decided++
		case "duplicates.reset":
			reset++
		}
	}
	if decided != 2 || reset != 1 {
		t.Errorf("activity: %d decided, %d reset, want 2 and 1", decided, reset)
	}

	// Another user sees and changes nothing.
	o := signedIn(t, e, "other-"+testEmail(t))
	if f := o.do("GET", "/duplicates?accountIds="+accID, nil); f.code != 200 || num(f.body["total"]) != 0 {
		t.Errorf("other user's list = %d %s", f.code, f.raw)
	}
	if f := o.do("GET", "/duplicates", nil); num(f.body["total"]) != 0 {
		t.Errorf("other user's own list = %s", f.raw)
	}
	expect(t, o.do("PUT", path, map[string]any{"mode": "keep_both"}), 404, "not_found")
	expect(t, o.do("DELETE", path, nil), 404, "not_found")
}
