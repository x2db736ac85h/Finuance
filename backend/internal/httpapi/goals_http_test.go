package httpapi

import (
	"testing"
)

// TestGoalsHTTP: create, list, change (by version), delete, validation and tenancy (ADR 0031).
func TestGoalsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	var food string
	for _, c := range list(t, b.do("GET", "/categories/stats", nil)) {
		if c["name"] == "Artykuły spożywcze" {
			food = c["id"].(string)
		}
	}
	if food == "" {
		t.Fatalf("the demo data has no groceries category: %s", b.do("GET", "/categories/stats", nil).raw)
	}

	save := b.do("POST", "/goals", map[string]any{"name": "Holiday", "kind": "save", "targetMinor": 500000, "accountId": accID,
		"startsOn": "2025-01-01", "deadline": "2025-12-31"})
	expect(t, save, 201, "")
	sp := obj(t, save.body, "progress")
	if save.body["currency"] != "PLN" || obj(t, save.body, "account")["name"] != "Demo" || num(save.body["version"]) != 1 || save.body["ai"] != false ||
		sp["savedMinor"] == nil || sp["baselineMinor"] != nil || sp["state"] == nil {
		t.Errorf("save goal = %s", save.raw)
	}
	cut := b.do("POST", "/goals", map[string]any{"name": "Less on groceries", "kind": "reduce", "categoryId": food, "reducePct": 20,
		"startsOn": "2025-06-01", "deadline": "2025-12-31"})
	expect(t, cut, 201, "")
	cp := obj(t, cut.body, "progress")
	if num(cp["baselineMinor"]) <= 0 || num(cp["monthlyLimitMinor"]) != num(cp["baselineMinor"])*80/100 || cp["savedMinor"] != nil ||
		num(cut.body["baselineMonths"]) != 3 {
		t.Errorf("reduce goal = %s", cut.raw)
	}
	all := b.do("GET", "/goals", nil)
	expect(t, all, 200, "")
	if gs := all.body["goals"].([]any); len(gs) != 2 {
		t.Errorf("goals = %s", all.raw)
	}

	id := save.body["id"].(string)
	expect(t, b.do("PATCH", "/goals/"+id, map[string]any{"name": "Holiday", "targetMinor": 600000, "deadline": "2025-12-31", "version": 2}),
		409, "stale_version")
	upd := b.do("PATCH", "/goals/"+id, map[string]any{"name": "Holiday", "targetMinor": 600000, "deadline": "2025-12-31", "version": 1,
		"status": "archived"})
	expect(t, upd, 200, "")
	if num(upd.body["targetMinor"]) != 600000 || upd.body["status"] != "archived" || num(upd.body["version"]) != 2 || upd.body["account"] != nil {
		t.Errorf("updated = %s (an update without accountId leaves the account out)", upd.raw)
	}

	for name, body := range map[string]map[string]any{
		"save without a target": {"name": "x", "kind": "save", "deadline": "2030-01-01"},
		"reduce without one":    {"name": "x", "kind": "reduce", "reducePct": 10, "deadline": "2030-01-01"},
		"deadline in the past":  {"name": "x", "kind": "save", "targetMinor": 1, "deadline": "2020-01-01"},
		"unknown kind":          {"name": "x", "kind": "spend", "targetMinor": 1, "deadline": "2030-01-01"},
		"over 100 %":            {"name": "x", "kind": "reduce", "categoryId": food, "reducePct": 101, "deadline": "2030-01-01"},
	} {
		if r := b.do("POST", "/goals", body); r.code != 422 {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	expect(t, b.do("PATCH", "/goals/"+id, map[string]any{"name": "x", "deadline": "2030-01-01", "version": 2}), 422, "")

	// another user sees none of it and can't change it or name the first user's category
	other := signedIn(t, e, "other-"+testEmail(t))
	if gs := other.do("GET", "/goals", nil).body["goals"].([]any); len(gs) != 0 {
		t.Errorf("other user's goals = %v", gs)
	}
	expect(t, other.do("PATCH", "/goals/"+id, map[string]any{"name": "x", "targetMinor": 1, "deadline": "2030-01-01", "version": 2}), 404, "")
	expect(t, other.do("DELETE", "/goals/"+id, nil), 404, "")
	expect(t, other.do("POST", "/goals", map[string]any{"name": "x", "kind": "reduce", "categoryId": food, "reducePct": 10,
		"deadline": "2030-01-01"}), 404, "")

	expect(t, b.do("DELETE", "/goals/"+id, nil), 204, "")
	expect(t, b.do("DELETE", "/goals/"+id, nil), 404, "")
}

// TestGoalProposalsHTTP: create_goal proposals from the (mock) model, applied through POST /goals
// with the assistant headers, recorded as the assistant's and undone (ADR 0030, 0031).
func TestGoalProposalsHTTP(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b, _ := demoBrowser(t, e)
	chat := b.do("POST", "/assistant/chats", nil).body["id"].(string)
	propose := func(text string) map[string]any {
		t.Helper()
		ask := b.do("POST", "/assistant/chats/"+chat+"/messages", map[string]any{"text": text, "settings": map[string]any{"mode": "auto", "think": "auto"}})
		expect(t, ask, 200, "")
		evs := parseSSE(t, ask.raw)
		i := slicesIndexEv(evs, "step")
		if i < 0 {
			t.Fatalf("no step for %q: %s", text, ask.raw)
		}
		return obj(t, evs[i].data, "proposal")
	}
	apply := func(p map[string]any) resp {
		t.Helper()
		b.header["X-Assistant-Chat"], b.header["X-Assistant-Proposal"] = chat, p["key"].(string)
		defer func() { delete(b.header, "X-Assistant-Chat"); delete(b.header, "X-Assistant-Proposal") }()
		g := obj(t, p, "goal")
		body := map[string]any{"name": g["name"], "kind": g["kind"], "currency": g["currency"], "baselineMonths": g["baselineMonths"], "deadline": g["deadline"]}
		for _, k := range []string{"targetMinor", "accountId", "categoryId", "reducePct"} {
			if v, ok := g[k]; ok {
				body[k] = v
			}
		}
		return b.do("POST", "/goals", body)
	}

	save := propose("save 10000 by 2030-06-30")
	if g := obj(t, save, "goal"); save["action"] != "create_goal" || g["kind"] != "save" || num(g["targetMinor"]) != 1_000_000 || g["deadline"] != "2030-06-30" {
		t.Fatalf("save proposal = %v", save)
	}
	created := apply(save)
	expect(t, created, 201, "")
	if created.body["ai"] != true {
		t.Errorf("created goal = %s", created.raw)
	}
	cut := propose(`spend 20% less on "Artykuły spożywcze" by 2030-04-30`)
	if g := obj(t, cut, "goal"); g["kind"] != "reduce" || g["categoryName"] != "Artykuły spożywcze" || num(g["reducePct"]) != 20 {
		t.Fatalf("reduce proposal = %v", cut)
	}
	expect(t, apply(cut), 201, "")

	changes := b.do("GET", "/ai-changes", nil).body["changes"].([]any)
	if len(changes) != 2 || changes[0].(map[string]any)["action"] != "create_goal" {
		t.Fatalf("changes = %v", changes)
	}
	// undoing the first deletes its goal; the second stays
	var first map[string]any
	for _, c := range changes {
		if c := c.(map[string]any); c["proposalKey"] == save["key"] {
			first = c
		}
	}
	expect(t, b.do("POST", "/ai-changes/"+first["id"].(string)+"/revert", nil), 200, "")
	gs := b.do("GET", "/goals", nil).body["goals"].([]any)
	if len(gs) != 1 || gs[0].(map[string]any)["kind"] != "reduce" || gs[0].(map[string]any)["ai"] != true {
		t.Errorf("goals after the undo = %v", gs)
	}
}
