package httpapi

import (
	"testing"
)

// TestReceiptsHTTP: a receipt proposal from the (mock) model applied through PATCH
// /transactions/{id} with the assistant headers, recorded with the note and undone; a typed card
// payment merged into the bank's row through the merge suggestions (ADR 0032).
func TestReceiptsHTTP(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b := signedIn(t, e, testEmail(t))
	acc := b.do("POST", "/accounts", map[string]any{"name": "Konto"})
	expect(t, acc, 201, "")
	accID := acc.body["id"].(string)
	up := b.upload("", "a.csv", []byte("Data operacji;Kontrahent;Tytuł;Kwota\n2026-11-03;BIEDRONKA SKLEP 123;zakupy;-54,30\n"), accID)
	expect(t, up, 201, "")
	b.commit(obj(t, up.body, "import")["id"].(string))

	chat := b.do("POST", "/assistant/chats", nil).body["id"].(string)
	ask := b.do("POST", "/assistant/chats/"+chat+"/messages", map[string]any{"text": `receipt 54.30 "Biedronka" on 2026-11-03`,
		"settings": map[string]any{"mode": "auto", "think": "auto"}})
	expect(t, ask, 200, "")
	evs := parseSSE(t, ask.raw)
	p := obj(t, evs[slicesIndexEv(evs, "step")].data, "proposal")
	ids := p["ids"].([]any)
	if p["action"] != "attach_receipt" || len(ids) != 1 || p["receiptLine"] != "Receipt: Biedronka · bread, butter" || p["accountName"] != "Konto" {
		t.Fatalf("proposal = %v", p)
	}
	tx := ids[0].(string)
	b.header["X-Assistant-Chat"], b.header["X-Assistant-Proposal"] = chat, p["key"].(string)
	expect(t, b.do("PATCH", "/transactions/"+tx, map[string]any{"note": p["note"]}), 200, "")
	delete(b.header, "X-Assistant-Chat")
	delete(b.header, "X-Assistant-Proposal")
	if got := b.do("GET", "/transactions/"+tx, nil); got.body["note"] != "Receipt: Biedronka · bread, butter" {
		t.Errorf("note = %v", got.body["note"])
	}
	c := b.do("GET", "/ai-changes", nil).body["changes"].([]any)[0].(map[string]any)
	if c["action"] != "attach_receipt" {
		t.Fatalf("change = %v", c)
	}
	expect(t, b.do("POST", "/ai-changes/"+c["id"].(string)+"/revert", nil), 200, "")
	if got := b.do("GET", "/transactions/"+tx, nil); got.body["note"] != nil && got.body["note"] != "" {
		t.Errorf("note after the undo = %v", got.body["note"])
	}

	// a card payment typed before the file had it, then merged into the bank's row
	typed := b.do("POST", "/transactions", map[string]any{"accountId": accID, "bookedOn": "2026-11-04", "amountMinor": -1200, "payee": "Kiosk",
		"note": "gazeta"})
	expect(t, typed, 201, "")
	up2 := b.upload("", "b.csv", []byte("Data operacji;Kontrahent;Tytuł;Kwota\n2026-11-05;KIOSK RUCH;prasa;-12,00\n"), accID)
	expect(t, up2, 201, "")
	b.commit(obj(t, up2.body, "import")["id"].(string))
	list := b.do("GET", "/transactions/merge-suggestions", nil)
	expect(t, list, 200, "")
	sugs := list.body["suggestions"].([]any)
	if len(sugs) != 1 {
		t.Fatalf("suggestions = %s", list.raw)
	}
	s := sugs[0].(map[string]any)
	if s["typedId"] != typed.body["id"] || s["importedPayee"] != "KIOSK RUCH" || obj(t, s, "account")["name"] != "Konto" || s["typedNote"] != "gazeta" {
		t.Errorf("suggestion = %v", s)
	}
	expect(t, b.do("POST", "/transactions/"+s["importedId"].(string)+"/merge", map[string]any{"into": s["typedId"]}), 422, "")
	expect(t, b.do("POST", "/transactions/"+s["typedId"].(string)+"/merge", map[string]any{"into": s["importedId"]}), 204, "")
	if got := b.do("GET", "/transactions/"+s["importedId"].(string), nil); got.body["note"] != "gazeta" {
		t.Errorf("merged row = %s", got.raw)
	}
	expect(t, b.do("GET", "/transactions/"+s["typedId"].(string), nil), 404, "")
	if n := len(b.do("GET", "/transactions/merge-suggestions", nil).body["suggestions"].([]any)); n != 0 {
		t.Errorf("%d suggestions after the merge", n)
	}
	merged := false
	for _, ev := range b.do("GET", "/me/activity", nil).body["events"].([]any) {
		merged = merged || ev.(map[string]any)["action"] == "transaction.merged"
	}
	if !merged {
		t.Error("the merge isn't in the activity")
	}

	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("POST", "/transactions/"+s["typedId"].(string)+"/merge", map[string]any{"into": s["importedId"]}), 404, "")
}
