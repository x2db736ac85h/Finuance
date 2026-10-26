package httpapi

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// activity returns the user's events, newest first, as GET /me/activity pages through them.
func activity(t *testing.T, b *browser) []map[string]any {
	t.Helper()
	var out []map[string]any
	path := "/me/activity?limit=200"
	for {
		r := b.do("GET", path, nil)
		expect(t, r, 200, "")
		for _, ev := range r.body["events"].([]any) {
			out = append(out, ev.(map[string]any))
		}
		next, ok := r.body["nextBefore"].(float64)
		if !ok {
			return out
		}
		path = "/me/activity?limit=200&before=" + strconv.FormatInt(int64(next), 10)
	}
}

// actions lists the events' actions, oldest first.
func actions(t *testing.T, b *browser) []string {
	t.Helper()
	evs := activity(t, b)
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[len(evs)-1-i] = ev["action"].(string)
	}
	return out
}

// TestActivityHTTP: every audited action of the account and session endpoints, the imports and
// exports lands in the user's activity log with the address and a browser name, newest first and
// paged; another user sees none of it, and an unknown email records nothing anywhere.
func TestActivityHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	email := testEmail(t)
	b, accID := demoBrowser(t, e)
	b.header["User-Agent"] = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	other := signedIn(t, e, "other-"+email)

	expect(t, b.do("POST", "/auth/logout", nil), 204, "")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": "wrong password!"}), 401, "unauthenticated")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": "nobody-" + email, "password": "wrong password!"}), 401, "unauthenticated")
	expect(t, b.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	second := e.browser()
	expect(t, second.do("POST", "/auth/login", map[string]any{"email": email, "password": password}), 200, "")
	sessions := b.do("GET", "/me/sessions", nil)
	var revoke string
	for _, s := range sessions.body2(t) {
		if s["current"] == false {
			revoke = s["id"].(string)
		}
	}
	expect(t, b.do("DELETE", "/me/sessions/"+revoke, nil), 204, "")
	expect(t, b.do("POST", "/me/password", map[string]any{"currentPassword": password, "newPassword": "brand new password"}), 204, "")

	csv := b.do("POST", "/transactions/export", map[string]any{"context": map[string]any{}, "columns": []string{"bookedOn", "amount"}, "view": "all"})
	expect(t, csv, 200, "")
	zip := b.do("GET", "/me/export", nil)
	expect(t, zip, 200, "")
	expect(t, b.do("POST", "/rules/apply", nil), 202, "")
	imps := b.do("GET", "/imports", nil)
	impID := imps.body2(t)[0]["id"].(string)
	expect(t, b.do("DELETE", "/imports/"+impID, nil), 204, "")
	expect(t, b.do("DELETE", "/accounts/"+accID, nil), 204, "")

	want := []string{"import.committed", "auth.logout", "auth.login_failed", "auth.login", "auth.login", "auth.session_revoked",
		"auth.password_changed", "export.csv", "export.data", "rules.applied", "import.deleted", "account.deleted"}
	if got := actions(t, b); !slices.Equal(got, want) {
		t.Fatalf("actions = %v\nwant      %v", got, want)
	}
	evs := activity(t, b)
	last := evs[0]
	if last["ip"] != "127.0.0.1" || last["browser"] != "Firefox on Linux" || last["entityId"] != accID || obj(t, last, "meta")["name"] != "Demo" {
		t.Errorf("account.deleted = %v", last)
	}
	byAction := map[string]map[string]any{}
	for _, ev := range evs {
		if _, seen := byAction[ev["action"].(string)]; !seen {
			byAction[ev["action"].(string)] = ev
		}
	}
	if m := obj(t, byAction["import.deleted"], "meta"); m["file"] != "demo.csv" || m["status"] != "committed" || num(m["transactionsRemoved"]) != 917 {
		t.Errorf("import.deleted meta = %v", m)
	}
	if m := obj(t, byAction["import.committed"], "meta"); m["file"] != "demo.csv" || num(m["new"]) != 917 || byAction["import.committed"]["ip"] != nil {
		t.Errorf("import.committed = %v (a job: no address)", byAction["import.committed"])
	}
	if ev := byAction["auth.session_revoked"]; ev["entityId"] != revoke {
		t.Errorf("session_revoked = %v", ev)
	}
	if m := obj(t, byAction["export.csv"], "meta"); !strings.HasPrefix(m["file"].(string), "finuance-all-") {
		t.Errorf("export.csv meta = %v", m)
	}

	// paging: newest first, nextBefore until the end
	p1 := b.do("GET", "/me/activity?limit=5", nil)
	expect(t, p1, 200, "")
	if len(p1.body["events"].([]any)) != 5 || p1.body["nextBefore"] == nil {
		t.Fatalf("first page = %s", p1.raw)
	}
	p2 := b.do("GET", "/me/activity?limit=50&before="+strconv.FormatInt(int64(num(p1.body["nextBefore"])), 10), nil)
	if len(p2.body["events"].([]any)) != len(want)-5 || p2.body["nextBefore"] != nil {
		t.Errorf("second page = %s", p2.raw)
	}
	expect(t, b.do("GET", "/me/activity?limit=0", nil), 422, "validation_failed")
	expect(t, b.do("GET", "/me/activity?limit=201", nil), 422, "validation_failed")
	expect(t, e.browser().do("GET", "/me/activity", nil), 401, "unauthenticated")

	// the other user saw only their own sign-up (nothing audited) and the unknown email nothing
	if got := actions(t, other); len(got) != 0 {
		t.Errorf("other user's activity = %v", got)
	}
}

// body2 reads a JSON array response.
func (r resp) body2(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(r.raw), &out); err != nil {
		t.Fatalf("not an array: %s", r.raw)
	}
	return out
}

func TestBrowserLabel(t *testing.T) {
	for ua, want := range map[string]string{
		"": "",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36":                         "Chrome on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15":                   "Safari on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/140.0 Mobile/15E148 Safari/604.1":  "Chrome on iOS",
		"Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36":                            "Chrome on Android",
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                                  "Firefox on Linux",
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36":                          "Chrome on ChromeOS",
		"curl/8.5.0":         "curl",
		"Go-http-client/1.1": "Other browser",
	} {
		if got := browserLabel(ua); got != want {
			t.Errorf("browserLabel(%q) = %q, want %q", ua, got, want)
		}
	}
}

// TestActivityActionsMatchTheAPI: the OpenAPI enum (which the web app words) lists exactly the
// actions the ledger records.
func TestActivityActionsMatchTheAPI(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	var api []string
	for _, v := range spec.Components.Schemas["ActivityEvent"].Value.Properties["action"].Value.Enum {
		api = append(api, v.(string))
	}
	got := slices.Clone(ledger.AuditActions)
	slices.Sort(api)
	slices.Sort(got)
	if !slices.Equal(api, got) {
		t.Errorf("OpenAPI actions %v\nledger actions  %v", api, got)
	}
}
