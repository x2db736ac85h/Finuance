package httpapi

import (
	"encoding/json"
	"testing"
	"time"
)

// HTTP tests of category management, rules and transfers (PLAN tasks 15.1–15.3). The worker runs,
// so rules and transfer detection are applied by the real Reclassify job.

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func list(t *testing.T, r resp) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(r.raw), &out); err != nil {
		t.Fatalf("list = %s", r.raw)
	}
	return out
}

func demoAccount(t *testing.T, b *browser) string {
	t.Helper()
	acc := b.do("POST", "/accounts", map[string]any{"name": "Demo"})
	expect(t, acc, 201, "")
	id := acc.body["id"].(string)
	up := b.upload("", "demo.csv", fixture(t, "demo/demo-legacy.csv"), id)
	expect(t, up, 201, "")
	b.commit(obj(t, up.body, "import")["id"].(string))
	return id
}

func TestCategoriesHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	demoAccount(t, b)
	stats := list(t, b.do("GET", "/categories/stats", nil))
	byName := map[string]map[string]any{}
	for _, s := range stats {
		byName[s["name"].(string)] = s
	}
	rachunki := byName["Rachunki"]
	if rachunki == nil || num(rachunki["txCount"]) == 0 || num(rachunki["manualCount"]) != 0 || len(rachunki["aliases"].([]any)) != 0 {
		t.Fatalf("stats = %s", b.do("GET", "/categories/stats", nil).raw)
	}
	id := rachunki["id"].(string)
	before := num(rachunki["txCount"])

	nc := b.do("POST", "/categories", map[string]any{"name": "Media"})
	expect(t, nc, 201, "")
	expect(t, b.do("POST", "/categories", map[string]any{"name": "MEDIA"}), 409, "conflict")
	expect(t, b.do("POST", "/categories", map[string]any{"name": ""}), 422, "validation_failed")
	rn := b.do("PATCH", "/categories/"+id, map[string]any{"name": "Opłaty"})
	expect(t, rn, 200, "")
	if rn.body["name"] != "Opłaty" {
		t.Errorf("renamed = %s", rn.raw)
	}
	expect(t, b.do("PATCH", "/categories/"+id, map[string]any{"name": "media"}), 409, "conflict")

	mg := b.do("POST", "/categories/"+id+"/merge", map[string]any{"intoId": nc.body["id"]})
	expect(t, mg, 200, "")
	if num(mg.body["moved"]) != before {
		t.Errorf("merge = %s, want %d moved", mg.raw, before)
	}
	expect(t, b.do("PATCH", "/categories/"+id, map[string]any{"name": "x"}), 404, "not_found")
	if n := totalWhere(t, b, map[string]any{"column": "category", "cond": "=Media"}); n != before {
		t.Errorf("Media has %d rows, want %d", n, before)
	}
	for _, s := range list(t, b.do("GET", "/categories/stats", nil)) {
		if s["name"] == "Media" && (len(s["aliases"].([]any)) != 2) {
			t.Errorf("Media aliases = %v, want the old and the renamed name", s["aliases"])
		}
	}
	expect(t, b.do("DELETE", "/categories/"+nc.body["id"].(string), nil), 204, "")
	if n := totalWhere(t, b, map[string]any{"column": "category", "cond": "=Media"}); n != 0 {
		t.Errorf("%d rows still in the deleted category", n)
	}

	var mine []string // two of the user's categories, untouched above
	for name, s := range byName {
		if name != "Rachunki" && len(mine) < 2 {
			mine = append(mine, s["id"].(string))
		}
	}
	other := signedIn(t, e, testEmail(t)+".other")
	expect(t, other.do("DELETE", "/categories/"+mine[0], nil), 404, "not_found")
	expect(t, other.do("POST", "/categories/"+mine[0]+"/merge", map[string]any{"intoId": mine[1]}), 404, "not_found")
	if s := list(t, other.do("GET", "/categories/stats", nil)); len(s) != 0 {
		t.Errorf("other user sees %d categories", len(s))
	}
	expect(t, e.browser().do("GET", "/categories/stats", nil), 401, "unauthenticated")
}

func TestRulesHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	demoAccount(t, b)
	cat := b.do("POST", "/categories", map[string]any{"name": "Internet"})
	expect(t, cat, 201, "")
	catID := cat.body["id"].(string)
	match := map[string]any{"filters": []any{map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}}}

	pv := b.do("POST", "/rules/preview", map[string]any{"match": match})
	expect(t, pv, 200, "")
	if num(pv.body["matched"]) != 14 || num(pv.body["manual"]) != 0 || len(pv.body["sample"].([]any)) != 10 {
		t.Errorf("preview = matched %v, manual %v, %d rows", pv.body["matched"], pv.body["manual"], len(pv.body["sample"].([]any)))
	}
	expect(t, b.do("POST", "/rules/preview", map[string]any{"match": map[string]any{}}), 422, "validation_failed")

	r := b.do("POST", "/rules", map[string]any{"name": "Internet", "match": match, "setCategoryId": catID})
	expect(t, r, 201, "")
	ruleID := r.body["id"].(string)
	if r.body["enabled"] != true || num(r.body["position"]) != 0 || obj(t, r.body, "setCategory")["name"] != "Internet" {
		t.Errorf("created = %s", r.raw)
	}
	eventually(t, "the rule to apply", func() bool {
		return totalWhere(t, b, map[string]any{"column": "category", "cond": "=Internet"}) == 14
	})
	id := idsWhere(t, b, map[string]any{"column": "payee", "cond": "=OPERATOR INTERNETU"}, 1)[0]
	d := b.do("GET", "/transactions/"+id, nil)
	if d.body["categorySource"] != "rule" || obj(t, d.body, "categoryRule")["name"] != "Internet" {
		t.Errorf("detail = source %v, rule %v", d.body["categorySource"], d.body["categoryRule"])
	}

	// a second rule over the same rows, later in the order, wins; reorder brings the first back
	cat2 := b.do("POST", "/categories", map[string]any{"name": "Abonamenty"})
	r2 := b.do("POST", "/rules", map[string]any{"name": "Abonamenty", "match": map[string]any{"search": "operator internetu"},
		"setCategoryId": cat2.body["id"]})
	expect(t, r2, 201, "")
	eventually(t, "the later rule to win", func() bool {
		return totalWhere(t, b, map[string]any{"column": "category", "cond": "=Abonamenty"}) == 14
	})
	ro := b.do("PUT", "/rules/order", map[string]any{"ids": []string{r2.body["id"].(string), ruleID}})
	expect(t, ro, 200, "")
	if rs := list(t, ro); rs[0]["id"] != r2.body["id"] || num(rs[1]["position"]) != 1 {
		t.Errorf("order = %s", ro.raw)
	}
	eventually(t, "the reordered rules", func() bool {
		return totalWhere(t, b, map[string]any{"column": "category", "cond": "=Internet"}) == 14
	})
	expect(t, b.do("PUT", "/rules/order", map[string]any{"ids": []string{ruleID}}), 422, "validation_failed")

	// disabling and deleting give the categories back
	up := b.do("PUT", "/rules/"+ruleID, map[string]any{"name": "Internet", "enabled": false, "match": match, "setCategoryId": catID})
	expect(t, up, 200, "")
	expect(t, b.do("DELETE", "/rules/"+r2.body["id"].(string), nil), 204, "")
	eventually(t, "the bank's categories to return", func() bool {
		return totalWhere(t, b, map[string]any{"column": "category", "cond": "=Internet"}) == 0 &&
			totalWhere(t, b, map[string]any{"column": "category", "cond": "=Abonamenty"}) == 0
	})
	expect(t, b.do("POST", "/rules/apply", nil), 202, "")
	if rs := list(t, b.do("GET", "/rules", nil)); len(rs) != 1 || rs[0]["enabled"] != false {
		t.Errorf("rules = %v", rs)
	}

	for name, body := range map[string]map[string]any{
		"no action":   {"name": "x", "match": match},
		"no match":    {"name": "x", "match": map[string]any{}, "markTransfer": true},
		"bad column":  {"name": "x", "match": map[string]any{"filters": []any{map[string]any{"column": "nope", "cond": "x"}}}, "markTransfer": true},
		"blank name":  {"name": " ", "match": match, "markTransfer": true},
		"unknown cat": {"name": "x", "match": match, "setCategoryId": "01a16a55-0000-7000-8000-000000000000"},
	} {
		if r := b.do("POST", "/rules", body); r.code != 422 && r.code != 404 {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}

	other := signedIn(t, e, testEmail(t)+".other")
	expect(t, other.do("PUT", "/rules/"+ruleID, map[string]any{"name": "x", "match": match, "markTransfer": true}), 404, "not_found")
	expect(t, other.do("DELETE", "/rules/"+ruleID, nil), 404, "not_found")
	expect(t, other.do("POST", "/rules", map[string]any{"name": "x", "match": match, "setCategoryId": catID}), 404, "not_found")
	if rs := list(t, other.do("GET", "/rules", nil)); len(rs) != 0 {
		t.Errorf("other user sees %d rules", len(rs))
	}
	if p := other.do("POST", "/rules/preview", map[string]any{"match": match}); num(p.body["matched"]) != 0 {
		t.Errorf("other user's preview matched %v", p.body["matched"])
	}
	expect(t, e.browser().do("GET", "/rules", nil), 401, "unauthenticated")
}

func TestTransfersHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	accounts := []string{}
	files := []string{
		"Data operacji;Kontrahent;Tytuł;Kwota\n02.01.2025;PRZELEW WLASNY;Oszczędności;-500,00\n03.01.2025;ZABKA;Zakupy;-30,00\n",
		"Data operacji;Kontrahent;Tytuł;Kwota\n03.01.2025;PRZELEW WLASNY;Z konta;500,00\n",
	}
	for i, f := range files {
		acc := b.do("POST", "/accounts", map[string]any{"name": []string{"Konto", "Oszczędności"}[i]})
		expect(t, acc, 201, "")
		accounts = append(accounts, acc.body["id"].(string))
		up := b.upload("", "f.csv", []byte(f), acc.body["id"].(string))
		expect(t, up, 201, "")
		b.commit(obj(t, up.body, "import")["id"].(string))
	}
	total := func(include bool) int {
		q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"includeTransfers": include}, "limit": 1})
		expect(t, q, 200, "")
		return num(q.body["total"])
	}
	eventually(t, "the pair", func() bool { return total(false) == 1 })
	if total(true) != 3 {
		t.Errorf("with transfers: %d rows, want 3", total(true))
	}
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"includeTransfers": true,
		"accountIds": []string{accounts[0]}, "search": "przelew"}, "limit": 5})
	out := rowsOf(t, q)[0]
	if out["isTransfer"] != true {
		t.Fatalf("row = %v", out)
	}
	d := b.do("GET", "/transactions/"+out["id"].(string), nil)
	peer := obj(t, d.body, "transferPeer")
	if d.body["transferSource"] != "pair" || peer["accountName"] != "Oszczędności" || num(obj(t, peer, "amount")["amountMinor"]) != 50000 {
		t.Errorf("detail = %s", d.raw)
	}
	// "not a transfer" unpairs both sides, for good
	nt := b.do("PATCH", "/transactions/"+out["id"].(string), map[string]any{"transfer": "no"})
	expect(t, nt, 200, "")
	if nt.body["isTransfer"] != false || nt.body["transferSource"] != "manual" || nt.body["transferPeer"] != nil {
		t.Errorf("after no = %s", nt.raw)
	}
	if total(false) != 3 {
		t.Errorf("both sides should count again: %d", total(false))
	}
	expect(t, b.do("POST", "/rules/apply", nil), 202, "")
	time.Sleep(300 * time.Millisecond)
	if total(false) != 3 {
		t.Error("a reclassification paired them again")
	}
	// automatic again: the pair comes back
	expect(t, b.do("PATCH", "/transactions/"+out["id"].(string), map[string]any{"transfer": "auto"}), 200, "")
	peerID := peer["id"].(string)
	expect(t, b.do("PATCH", "/transactions/"+peerID, map[string]any{"transfer": "auto"}), 200, "")
	eventually(t, "the pair to come back", func() bool { return total(false) == 1 })
	// marked by hand
	zabka := idsWhere(t, b, map[string]any{"column": "payee", "cond": "=ZABKA"}, 1)[0]
	yes := b.do("PATCH", "/transactions/"+zabka, map[string]any{"transfer": "yes"})
	if yes.body["isTransfer"] != true || yes.body["transferSource"] != "manual" {
		t.Errorf("after yes = %s", yes.raw)
	}
	expect(t, b.do("PATCH", "/transactions/"+zabka, map[string]any{"transfer": "maybe"}), 422, "validation_failed")
	other := signedIn(t, e, testEmail(t)+".other")
	expect(t, other.do("PATCH", "/transactions/"+zabka, map[string]any{"transfer": "no"}), 404, "not_found")
}
