package httpapi

import (
	"encoding/json"
	"testing"
)

func TestLoadDemoDataHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	b := signedIn(t, e, testEmail(t))
	r := b.do("POST", "/demo-data", nil)
	expect(t, r, 201, "")
	if num(r.body["transactions"]) < 800 || r.body["accountId"] == nil || r.body["importId"] == nil {
		t.Fatalf("demo = %s", r.raw)
	}
	// The data is there when the request returns (inline commit): the account shows its count.
	var accounts []map[string]any
	_ = json.Unmarshal([]byte(b.do("GET", "/accounts", nil).raw), &accounts)
	if len(accounts) != 1 || accounts[0]["id"] != r.body["accountId"] || accounts[0]["isDemo"] != true ||
		num(accounts[0]["txCount"]) != num(r.body["transactions"]) {
		t.Errorf("accounts = %v", accounts)
	}
	imp := b.do("GET", "/imports/"+r.body["importId"].(string), nil)
	expect(t, imp, 200, "")
	if imp.body["status"] != "committed" {
		t.Errorf("demo import = %s", imp.raw)
	}
	expect(t, b.do("POST", "/demo-data", nil), 409, "conflict")
	// Deleting the demo account allows a new one.
	expect(t, b.do("DELETE", "/accounts/"+r.body["accountId"].(string), nil), 204, "")
	expect(t, b.do("POST", "/demo-data", nil), 201, "")
	// Signed out: 401.
	expect(t, e.browser().do("POST", "/demo-data", nil), 401, "unauthenticated")
}
