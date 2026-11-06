package httpapi

import (
	"testing"
)

// TestAIChangesHTTP: a proposal applied through the normal endpoints with the assistant's headers
// is recorded as one change (the category it created and every row), can be undone once, and is
// refused while anything changed since (ADR 0030).
func TestAIChangesHTTP(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b, _ := demoBrowser(t, e)
	chat := b.do("POST", "/assistant/chats", nil)
	id := chat.body["id"].(string)
	ask := b.do("POST", "/assistant/chats/"+id+"/messages", map[string]any{"text": `put "OPERATOR INTERNETU" in Internet`,
		"settings": map[string]any{"mode": "auto", "think": "auto"}})
	expect(t, ask, 200, "")
	evs := parseSSE(t, ask.raw)
	p := obj(t, evs[slicesIndexEv(evs, "step")].data, "proposal")
	key, ids := p["key"].(string), p["ids"].([]any)
	internet := map[string]any{"column": "category", "cond": "=Internet"}

	apply := func() {
		t.Helper()
		b.header["X-Assistant-Chat"], b.header["X-Assistant-Proposal"] = id, key
		defer func() { delete(b.header, "X-Assistant-Chat"); delete(b.header, "X-Assistant-Proposal") }()
		nc := b.do("POST", "/categories", map[string]any{"name": "Internet"})
		expect(t, nc, 201, "")
		expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": ids, "category": map[string]any{"id": nc.body["id"]}}), 200, "")
	}
	apply()
	page := b.do("GET", "/ai-changes", nil)
	expect(t, page, 200, "")
	cs := page.body["changes"].([]any)
	c := cs[0].(map[string]any)
	if len(cs) != 1 || c["action"] != "set_category" || c["proposalKey"] != key || c["chatId"] != id || c["auto"] != false ||
		num(c["items"]) != 15 || c["revertedAt"] != nil {
		t.Fatalf("changes = %s (one change: the category and the 14 rows)", page.raw)
	}

	// the rows carry the marker, the "AI assisted" chip finds exactly them, and the source column
	// still says where they came from (the bank's file)
	assisted := func() (int, []any) {
		t.Helper()
		q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"aiAssisted": true}, "limit": 20})
		expect(t, q, 200, "")
		return num(q.body["total"]), q.body["rows"].([]any)
	}
	n, rows := assisted()
	if r0 := rows[0].(map[string]any); n != 14 || r0["ai"] != true || r0["source"] != "Bank" {
		t.Errorf("assisted rows = %d, first %v", n, r0)
	}
	if n := totalWhere(t, b, map[string]any{"column": "source", "cond": "=Assistant"}); n != 0 {
		t.Errorf("%d rows the assistant added (it only changed some)", n)
	}

	rv := b.do("POST", "/ai-changes/"+c["id"].(string)+"/revert", nil)
	expect(t, rv, 200, "")
	if num(rv.body["rows"]) != 14 || num(rv.body["deleted"]) != 1 || num(rv.body["kept"]) != 0 || obj(t, rv.body, "change")["revertedAt"] == nil {
		t.Errorf("revert = %s", rv.raw)
	}
	if n := totalWhere(t, b, internet); n != 0 {
		t.Errorf("%d rows still in Internet", n)
	}
	if n, _ := assisted(); n != 0 {
		t.Errorf("%d rows still marked after the revert", n)
	}
	expect(t, b.do("POST", "/ai-changes/"+c["id"].(string)+"/revert", nil), 409, "conflict")
	reverted := 0
	for _, ev := range b.do("GET", "/me/activity", nil).body["events"].([]any) {
		if ev := ev.(map[string]any); ev["action"] == "assistant.change_reverted" && ev["entityId"] == c["id"] {
			reverted++
			if m := obj(t, ev, "meta"); m["action"] != "set_category" || num(m["rows"]) != 14 || len(m) != 2 {
				t.Errorf("meta = %v", m)
			}
		}
	}
	if reverted != 1 {
		t.Errorf("revert events = %d", reverted)
	}

	// applied again, then one row edited by hand: the revert is refused and changes nothing
	apply()
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": ids[:1], "category": map[string]any{"reset": true}}), 200, "")
	again := b.do("GET", "/ai-changes", nil).body["changes"].([]any)[0].(map[string]any)
	if again["id"] == c["id"] || again["revertedAt"] != nil {
		t.Fatalf("second apply = %v (a new change after the revert)", again)
	}
	expect(t, b.do("POST", "/ai-changes/"+again["id"].(string)+"/revert", nil), 409, "changed_since")
	if n := totalWhere(t, b, internet); n != 13 {
		t.Errorf("rows in Internet after a refused revert = %d, want 13", n)
	}

	// a key that isn't one of the chat's proposals is refused before anything is written
	b.header["X-Assistant-Chat"], b.header["X-Assistant-Proposal"] = id, "0000000000000000"
	expect(t, b.do("POST", "/transactions/bulk", map[string]any{"ids": ids[:1], "category": map[string]any{"reset": true}}), 422, "validation_failed")
	delete(b.header, "X-Assistant-Chat")
	delete(b.header, "X-Assistant-Proposal")
	if n := len(b.do("GET", "/ai-changes", nil).body["changes"].([]any)); n != 2 {
		t.Errorf("changes = %d, want 2", n)
	}

	// another user sees none of it and can't undo it
	other := signupBrowser(t, e, "other-"+testEmail(t))
	if n := len(other.do("GET", "/ai-changes", nil).body["changes"].([]any)); n != 0 {
		t.Errorf("another user sees %d changes", n)
	}
	expect(t, other.do("POST", "/ai-changes/"+again["id"].(string)+"/revert", nil), 404, "not_found")
}
