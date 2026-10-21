package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// HTTP tests of the transaction read and edit endpoints (PLAN Phase 7) on the demo year.

// demoBrowser signs a user in and commits the legacy demo year into one account.
func demoBrowser(t *testing.T, e *authEnv) (*browser, string) {
	t.Helper()
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Demo"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)
	up := b.upload("", "demo.csv", fixture(t, "demo/demo-legacy.csv"), accID)
	expect(t, up, 201, "")
	b.commit(obj(t, up.body, "import")["id"].(string))
	return b, accID
}

func rowsOf(t *testing.T, r resp) []map[string]any {
	t.Helper()
	list, ok := r.body["rows"].([]any)
	if !ok {
		t.Fatalf("no rows: %s", r.raw)
	}
	out := make([]map[string]any, len(list))
	for i, x := range list {
		out[i] = x.(map[string]any)
	}
	return out
}

func TestTransactionQueryHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)

	all := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{}, "limit": 5})
	expect(t, all, 200, "")
	if num(all.body["total"]) != 917 || len(rowsOf(t, all)) != 5 || num(all.body["duplicatesSkipped"]) != 0 {
		t.Fatalf("all = %s", all.raw)
	}
	first := rowsOf(t, all)[0]
	if first["bookedOn"] != "2025-03-17" || first["accountName"] != "Demo" || first["category"].(map[string]any)["name"] != "Artykuły spożywcze" {
		t.Errorf("first row = %v", first)
	}

	// Filters, search, period, sort and stats together.
	q := b.do("POST", "/transactions/query", map[string]any{
		"context": map[string]any{"accountIds": []string{accID}, "search": "warszawa",
			"filters": []any{map[string]any{"column": "amount", "cond": "<-100"}}, "period": map[string]any{"year": 2024}},
		"sort": map[string]any{"column": "amount", "dir": "asc"}, "limit": 3})
	expect(t, q, 200, "")
	p := q.body["period"].(map[string]any)
	if p["label"] != "2024" || p["from"] != "2024-01-01" || p["to"] != "2025-01-01" || q.body["dataRange"].(map[string]any)["max"] != "2025-03-14" { // filtered, period ignored
		t.Errorf("period/dataRange = %s", q.raw)
	}
	rows := rowsOf(t, q)
	st := q.body["stats"].(map[string]any)
	if num(q.body["total"]) == 0 || num(rows[0]["amount"].(map[string]any)["amountMinor"]) != num(st["min"]) || st["column"] != "amount" {
		t.Errorf("sorted rows / stats = %s", q.raw)
	}

	// locateId moves to the window that holds the row.
	ids := b.do("POST", "/transactions/ids", map[string]any{"context": map[string]any{}, "sort": map[string]any{"column": "payee"}, "offset": 200, "limit": 10})
	expect(t, ids, 200, "")
	var idList []map[string]any
	_ = json.Unmarshal([]byte(ids.raw), &idList)
	if len(idList) != 10 || idList[0]["amountMinor"] == nil {
		t.Fatalf("ids = %s", ids.raw)
	}
	loc := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{}, "sort": map[string]any{"column": "payee"},
		"limit": 50, "locateId": idList[3]["id"]})
	expect(t, loc, 200, "")
	if num(loc.body["locatedOffset"]) != 203 || num(loc.body["offset"]) != 200 || rowsOf(t, loc)[3]["id"] != idList[3]["id"] {
		t.Errorf("locate = offset %v located %v", loc.body["offset"], loc.body["locatedOffset"])
	}

	// Distinct ignores the column's own filter.
	d := b.do("POST", "/transactions/distinct", map[string]any{"context": map[string]any{"filters": []any{
		map[string]any{"column": "category", "cond": "=Rachunki", "excluded": []string{"Rachunki"}}}}, "column": "category"})
	expect(t, d, 200, "")
	var vals []map[string]any
	_ = json.Unmarshal([]byte(d.raw), &vals)
	if len(vals) != 12 || vals[0]["value"] != "Artykuły spożywcze" || num(vals[0]["count"]) != 403 {
		t.Errorf("distinct = %s", d.raw)
	}

	// Validation: the OpenAPI enum and limits, and the period shape.
	for _, body := range []map[string]any{
		{"context": map[string]any{"filters": []any{map[string]any{"column": "t.payee"}}}, "limit": 5},
		{"context": map[string]any{}, "limit": 501},
		{"context": map[string]any{"period": map[string]any{"preset": "l30", "year": 2024}}, "limit": 5},
		{"context": map[string]any{"period": map[string]any{"from": "2025-01-02", "to": "2025-01-01"}}, "limit": 5},
		{"context": map[string]any{"toolFilter": map[string]any{"minAmount": "1e3"}}, "limit": 5},
	} {
		expect(t, b.do("POST", "/transactions/query", body), 422, "validation_failed")
	}
}

func TestTransactionDetailAndPatchHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{
		"filters": []any{map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}}}, "limit": 1})
	id := rowsOf(t, q)[0]["id"].(string)

	d := b.do("GET", "/transactions/"+id, nil)
	expect(t, d, 200, "")
	srcs := d.body["sources"].([]any)
	if len(srcs) != 1 || srcs[0].(map[string]any)["filename"] != "demo.csv" || len(d.body["raw"].([]any)) == 0 ||
		d.body["categorySource"] != "bank" || d.body["origin"] != "import" || d.body["accountId"] != accID {
		t.Fatalf("detail = %s", d.raw)
	}

	// A new category by name becomes a manual one; the note is kept.
	p := b.do("PATCH", "/transactions/"+id, map[string]any{"categoryName": "  Internet ", "note": "=HYPERLINK(\"x\")"})
	expect(t, p, 200, "")
	if p.body["category"].(map[string]any)["name"] != "Internet" || p.body["categorySource"] != "manual" || p.body["note"] != `=HYPERLINK("x")` {
		t.Errorf("patched = %s", p.raw)
	}
	cats := b.do("GET", "/categories?accountIds="+accID, nil)
	expect(t, cats, 200, "")
	var list []map[string]any
	_ = json.Unmarshal([]byte(cats.raw), &list)
	slots := map[string]any{}
	for _, c := range list {
		slots[c["name"].(string)] = c["colorSlot"]
		if c["name"] == "Internet" && num(c["txCount"]) != 1 {
			t.Errorf("Internet = %v", c)
		}
	}
	if len(list) != 13 || num(slots["Mieszkanie"]) != 1 || num(slots["Artykuły spożywcze"]) != 2 || slots["Ogród"] != nil {
		t.Errorf("categories = %s", cats.raw)
	}

	// Reset goes back to the bank's category.
	r := b.do("PATCH", "/transactions/"+id, map[string]any{"resetCategory": true})
	expect(t, r, 200, "")
	if r.body["category"].(map[string]any)["name"] != "Rachunki" || r.body["categorySource"] != "bank" {
		t.Errorf("reset = %s", r.raw)
	}
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{"resetCategory": true, "categoryName": "X"}), 422, "validation_failed")
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{}), 422, "validation_failed")
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{"categoryName": strings.Repeat("x", 81)}), 422, "validation_failed")

	// Another user sees none of it.
	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("GET", "/transactions/"+id, nil), 404, "not_found")
	expect(t, other.do("PATCH", "/transactions/"+id, map[string]any{"note": "x"}), 404, "not_found")
	oq := other.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"accountIds": []string{accID}}, "limit": 5})
	expect(t, oq, 200, "")
	if num(oq.body["total"]) != 0 {
		t.Errorf("other user's query = %s", oq.raw)
	}
	oc := other.do("GET", "/categories?accountIds="+accID, nil)
	if oc.code != 200 || oc.raw != "[]\n" {
		t.Errorf("other user's categories = %d %q", oc.code, oc.raw)
	}
	od := other.do("POST", "/transactions/distinct", map[string]any{"context": map[string]any{"accountIds": []string{accID}}, "column": "payee"})
	if od.code != 200 || od.raw != "[]\n" {
		t.Errorf("other user's distinct = %d %q", od.code, od.raw)
	}
	// The other user's own category can't be assigned to my transaction (and vice versa).
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{"categoryId": "00000000-0000-7000-8000-000000000001"}), 404, "not_found")
}

func TestExportCSVHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, _ := demoBrowser(t, e)
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{
		"filters": []any{map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}}}, "limit": 1})
	id := rowsOf(t, q)[0]["id"].(string)
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{"note": "=1+2;x"}), 200, "")

	ex := b.do("POST", "/transactions/export", map[string]any{
		"context": map[string]any{"filters": []any{map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}}},
		"columns": []string{"amount", "bookedOn", "payee", "note"}, "view": "Rachunki 2025!"})
	expect(t, ex, 200, "")
	if ct := ex.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := ex.header.Get("Content-Disposition"); !strings.Contains(cd, "finuance-Rachunki_2025_-") || !strings.HasSuffix(cd, `.csv"`) && !strings.HasSuffix(cd, ".csv") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !strings.HasPrefix(ex.raw, "\ufeffAmount;Date;Payee;Note\r\n") {
		t.Fatalf("header = %q", ex.raw[:min(60, len(ex.raw))])
	}
	lines := strings.Split(strings.TrimPrefix(ex.raw, "\ufeff"), "\r\n")
	if len(lines) != 15 { // header + 14 internet bills
		t.Fatalf("%d lines", len(lines))
	}
	// The newest bill carries the formula-like note: defused with ' and quoted (it contains ";").
	if lines[1] != `-79,99;20.02.2025;OPERATOR INTERNETU;"'=1+2;x"` || lines[2] != "-79,99;20.01.2025;OPERATOR INTERNETU;" {
		t.Errorf("rows = %q", lines[1:3])
	}
	tab := b.do("POST", "/transactions/export", map[string]any{"context": map[string]any{}, "columns": []string{"payee", "amount"}, "delimiter": `\t`})
	expect(t, tab, 200, "")
	if !strings.HasPrefix(tab.raw, "\ufeffPayee\tAmount\r\n") || strings.Count(tab.raw, "\r\n") != 917 {
		t.Errorf("tab export: %q…, %d lines", tab.raw[:min(40, len(tab.raw))], strings.Count(tab.raw, "\r\n"))
	}
	expect(t, b.do("POST", "/transactions/export", map[string]any{"context": map[string]any{}, "columns": []string{}}), 422, "validation_failed")
	expect(t, b.do("POST", "/transactions/export", map[string]any{"context": map[string]any{"period": map[string]any{"month": "2025-13"}},
		"columns": []string{"payee"}}), 422, "validation_failed")
}
