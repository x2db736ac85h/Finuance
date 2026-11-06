package httpapi

import (
	"context"
	"testing"

	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestAdviceHTTP: administrators write, publish, change and delete articles and add the starter
// ones; anyone signed in reads a published article; others get 404 for the admin routes; the
// administrator's instructions are part of the instance settings and stay when a save leaves them
// out (ADR 0034).
func TestAdviceHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	db := tu.Require(t, testDB)
	reset := func() {
		for _, q := range []string{`DELETE FROM advice_articles`, `UPDATE instance_settings SET doc = '{}'`} {
			if _, err := db.Owner.Exec(context.Background(), q); err != nil {
				t.Fatal(err)
			}
		}
	}
	reset()
	t.Cleanup(reset)
	boss, _ := e.adminBrowser(t, "boss-"+testEmail(t))
	user := signedIn(t, e, testEmail(t))

	expect(t, user.do("GET", "/admin/advice", nil), 404, "not_found")
	in := map[string]any{"slug": "emergency-fund", "language": "en", "title": "Building an emergency fund", "body": "Keep **three months** aside.",
		"tags": []string{"saving"}, "status": "draft"}
	expect(t, user.do("POST", "/admin/advice", in), 404, "not_found")
	created := boss.do("POST", "/admin/advice", in)
	expect(t, created, 201, "")
	id := created.body["id"].(string)
	if created.body["status"] != "draft" || num(created.body["version"]) != 1 || num(created.body["bodyLength"]) != 28 {
		t.Errorf("created = %s", created.raw)
	}
	expect(t, boss.do("POST", "/admin/advice", in), 422, "validation_failed")  // the slug is taken in English
	expect(t, user.do("GET", "/advice/emergency-fund", nil), 404, "not_found") // a draft

	in["status"], in["version"] = "published", 1
	pub := boss.do("PUT", "/admin/advice/"+id, in)
	expect(t, pub, 200, "")
	expect(t, boss.do("PUT", "/admin/advice/"+id, in), 409, "stale_version")
	read := user.do("GET", "/advice/emergency-fund?language=pl", nil)
	expect(t, read, 200, "")
	if read.body["title"] != "Building an emergency fund" || read.body["body"] != "Keep **three months** aside." || read.body["language"] != "en" {
		t.Errorf("read = %s", read.raw)
	}
	if list := boss.do("GET", "/admin/advice", nil); len(list.body2(t)) != 1 || list.body2(t)[0]["body"] != "" {
		t.Errorf("list = %s", list.raw)
	}
	added := boss.do("POST", "/admin/advice/starter", nil)
	expect(t, added, 200, "")
	if num(added.body["added"]) != 7 { // emergency-fund is taken
		t.Errorf("starter = %s", added.raw)
	}
	expect(t, boss.do("DELETE", "/admin/advice/"+id, nil), 204, "")
	expect(t, boss.do("GET", "/admin/advice/"+id, nil), 404, "not_found")
	acts := boss.do("GET", "/me/activity", nil)
	seen := map[string]int{}
	for _, a := range acts.body["events"].([]any) {
		seen[a.(map[string]any)["action"].(string)]++
	}
	if seen["admin.advice_changed"] != 3 || seen["admin.advice_deleted"] != 1 {
		t.Errorf("activity = %v", seen)
	}

	// the instructions: saved, kept by a save without them, too long refused
	s := boss.do("GET", "/admin/settings", nil)
	doc := map[string]any{"version": s.body["version"], "defaults": map[string]any{}, "features": map[string]any{}, "limits": map[string]any{},
		"assistant": map[string]any{"instructions": "Answer in English."}}
	r := boss.do("PUT", "/admin/settings", doc)
	expect(t, r, 200, "")
	delete(doc, "assistant")
	doc["version"] = r.body["version"]
	r = boss.do("PUT", "/admin/settings", doc)
	expect(t, r, 200, "")
	if obj(t, r.body, "assistant")["instructions"] != "Answer in English." {
		t.Errorf("kept = %s", r.raw)
	}
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'x'
	}
	doc["version"], doc["assistant"] = r.body["version"], map[string]any{"instructions": string(long)}
	expect(t, boss.do("PUT", "/admin/settings", doc), 422, "validation_failed")
}
