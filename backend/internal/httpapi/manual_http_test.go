package httpapi

import (
	"strings"
	"testing"
)

// TestManualTransactionsHTTP (ADR 0029): a cash account takes typed expenses that can be changed
// and deleted, a bank account doesn't, files don't go into a cash account, and another user can't
// touch them. Every change is in the Activity, without amounts or payees.
func TestManualTransactionsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b := signedIn(t, e, testEmail(t))
	wallet := b.do("POST", "/accounts", map[string]any{"name": "Portfel", "kind": "cash"})
	expect(t, wallet, 201, "")
	if wallet.body["kind"] != "cash" {
		t.Fatalf("kind = %v", wallet.body["kind"])
	}
	walletID := wallet.body["id"].(string)
	bank := b.do("POST", "/accounts", map[string]any{"name": "Konto"})
	expect(t, bank, 201, "")
	if bank.body["kind"] != "bank" {
		t.Fatalf("default kind = %v", bank.body["kind"])
	}

	add := b.do("POST", "/transactions", map[string]any{"accountId": walletID, "bookedOn": "2026-10-18",
		"amountMinor": -2450, "payee": "Piekarnia", "categoryName": "Jedzenie", "note": "drożdżówki"})
	expect(t, add, 201, "")
	id := add.body["id"].(string)
	if add.body["origin"] != "manual" || num(add.body["amount"].(map[string]any)["amountMinor"]) != -2450 ||
		add.body["category"].(map[string]any)["name"] != "Jedzenie" || add.body["note"] != "drożdżówki" {
		t.Errorf("added = %s", add.raw)
	}
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"accountIds": []string{walletID}, "search": "piekarnia"}, "limit": 5})
	expect(t, q, 200, "")
	if num(q.body["total"]) != 1 {
		t.Errorf("search found %v rows", q.body["total"])
	}

	expect(t, b.do("POST", "/transactions", map[string]any{"accountId": bank.body["id"], "bookedOn": "2026-10-18",
		"amountMinor": -1, "payee": "x"}), 422, "validation_failed")
	expect(t, b.do("POST", "/transactions", map[string]any{"accountId": walletID, "bookedOn": "2026-10-18",
		"amountMinor": 0, "payee": "x"}), 422, "validation_failed")
	expect(t, b.upload("", "a.csv", []byte("Data;Opis;Kwota\n02.01.2025;Kawa;-12,00\n"), walletID), 422, "validation_failed")

	patch := b.do("PATCH", "/transactions/"+id, map[string]any{"amountMinor": -3000, "payee": "Piekarnia Pod Lipą", "note": "bułki"})
	expect(t, patch, 200, "")
	if num(patch.body["amount"].(map[string]any)["amountMinor"]) != -3000 || patch.body["payee"] != "Piekarnia Pod Lipą" || patch.body["note"] != "bułki" {
		t.Errorf("patched = %s", patch.raw)
	}

	// another user
	other := signedIn(t, e, strings.Replace(testEmail(t), "@", "-other@", 1))
	expect(t, other.do("GET", "/transactions/"+id, nil), 404, "not_found")
	expect(t, other.do("PATCH", "/transactions/"+id, map[string]any{"payee": "x"}), 404, "not_found")
	expect(t, other.do("DELETE", "/transactions/"+id, nil), 404, "not_found")
	expect(t, other.do("POST", "/transactions", map[string]any{"accountId": walletID, "bookedOn": "2026-10-18",
		"amountMinor": -1, "payee": "x"}), 404, "not_found")

	expect(t, b.do("DELETE", "/transactions/"+id, nil), 204, "")
	expect(t, b.do("GET", "/transactions/"+id, nil), 404, "not_found")

	act := b.do("GET", "/me/activity", nil)
	expect(t, act, 200, "")
	for _, want := range []string{"transaction.added", "transaction.edited", "transaction.deleted"} {
		if !strings.Contains(act.raw, want) {
			t.Errorf("activity has no %s: %s", want, act.raw)
		}
	}
	if strings.Contains(act.raw, "Piekarnia") || strings.Contains(act.raw, "2450") {
		t.Errorf("activity carries transaction details: %s", act.raw)
	}
}

// TestImportedRowsStayTheBanks: the typed fields of an imported transaction can't be changed and
// it can't be deleted on its own.
func TestImportedRowsStayTheBanks(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{"accountIds": []string{accID}}, "limit": 1})
	expect(t, q, 200, "")
	id := rowsOf(t, q)[0]["id"].(string)
	expect(t, b.do("PATCH", "/transactions/"+id, map[string]any{"payee": "x"}), 422, "validation_failed")
	expect(t, b.do("DELETE", "/transactions/"+id, nil), 409, "conflict")
}
