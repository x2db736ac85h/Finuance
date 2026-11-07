package httpapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// TestAssistantAdminHTTP: administrators add, change and delete connections (the key goes in and
// never comes out), tune a model, and see the assistant's runs and tokens per user; nobody else
// reaches these routes (ADR 0035).
func TestAssistantAdminHTTP(t *testing.T) {
	mock := httptest.NewServer(llmmock.New())
	t.Cleanup(mock.Close)
	e := newTestEnv(t, func(c *config.Config) { c.InstanceKey = bytes.Repeat([]byte{9}, 32) }, false)
	reset := func() {
		if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(),
			`DELETE FROM assistant_connections; DELETE FROM assistant_model_settings`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	t.Cleanup(reset)
	boss, _ := e.adminBrowser(t, "boss-"+testEmail(t))
	user := signedIn(t, e, testEmail(t))

	expect(t, user.do("GET", "/admin/assistant/connections", nil), 404, "not_found")
	in := map[string]any{"name": "gpu-box", "label": "GPU box", "api": "openai", "baseUrl": mock.URL + "/v1", "cloud": false,
		"defaultModel": "mock-tools", "models": []string{}, "contextTokens": 16384, "apiKey": "sk-very-secret"}
	expect(t, user.do("POST", "/admin/assistant/connections", in), 404, "not_found")
	created := boss.do("POST", "/admin/assistant/connections", in)
	expect(t, created, 201, "")
	if created.body["hasKey"] != true || created.body["source"] != "panel" || num(created.body["version"]) != 1 ||
		strings.Contains(created.raw, "sk-very-secret") {
		t.Errorf("created = %s", created.raw)
	}
	// users see it at once (this replica reloaded)
	st := user.do("GET", "/assistant/status", nil)
	if !strings.Contains(st.raw, `"name":"gpu-box"`) || strings.Contains(st.raw, "sk-very-secret") {
		t.Errorf("status = %s", st.raw)
	}
	in["name"] = "Bad Name"
	expect(t, boss.do("POST", "/admin/assistant/connections", in), 422, "validation_failed")

	up := map[string]any{"version": 1, "label": "GPU box (Ollama)", "api": "openai", "baseUrl": mock.URL + "/v1", "cloud": false,
		"defaultModel": "mock-tools", "models": []string{"mock-*"}, "contextTokens": 0, "keyAction": "keep"}
	r := boss.do("PUT", "/admin/assistant/connections/gpu-box", up)
	expect(t, r, 200, "")
	if r.body["hasKey"] != true || r.body["label"] != "GPU box (Ollama)" {
		t.Errorf("updated = %s", r.raw)
	}
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box", up), 409, "stale_version")
	up["version"], up["keyAction"] = 2, "replace"
	expect(t, boss.do("PUT", "/admin/assistant/connections/gpu-box", up), 422, "validation_failed") // replace without a key
	list := boss.do("GET", "/admin/assistant/connections", nil)
	if rows := list.body2(t); len(rows) != 1 || rows[0]["name"] != "gpu-box" || strings.Contains(list.raw, "sk-") {
		t.Errorf("list = %s", list.raw)
	}

	tun := boss.do("PUT", "/admin/assistant/tuning", map[string]any{"connection": "gpu-box", "model": "mock-tools", "temperature": 0.5,
		"think": "off", "extraPrompt": "Be brief."})
	expect(t, tun, 200, "")
	if tun.body["temperature"] != 0.5 || tun.body["think"] != "off" {
		t.Errorf("tuning = %s", tun.raw)
	}
	expect(t, boss.do("PUT", "/admin/assistant/tuning", map[string]any{"connection": "gpu-box", "model": "m", "temperature": 5, "extraPrompt": ""}),
		422, "validation_failed")
	if l := boss.do("GET", "/admin/assistant/tuning", nil); len(l.body2(t)) != 1 {
		t.Errorf("tuning list = %s", l.raw)
	}
	expect(t, boss.do("DELETE", "/admin/assistant/connections/gpu-box", nil), 204, "")
	expect(t, boss.do("DELETE", "/admin/assistant/connections/gpu-box", nil), 404, "not_found")

	acts := boss.do("GET", "/me/activity", nil)
	n := 0
	for _, a := range acts.body["events"].([]any) {
		ev := a.(map[string]any)
		if ev["action"] == "admin.assistant_changed" {
			n++
		}
	}
	if n != 4 || strings.Contains(acts.raw, "sk-very-secret") {
		t.Errorf("%d assistant changes in the activity: %s", n, acts.raw)
	}

	// the assistant's runs and tokens per user, never what was asked
	me := user.do("GET", "/me", nil)
	id := uuid.MustParse(me.body["id"].(string))
	if err := e.st.WithUser(context.Background(), id, func(q *db.Queries, _ pgx.Tx) error {
		return q.AddAssistantUsage(context.Background(), db.AddAssistantUsageParams{UserID: id, Day: platform.DateOf(time.Now(), time.UTC), Tokens: 4321})
	}); err != nil {
		t.Fatal(err)
	}
	u := boss.do("GET", "/admin/users/"+id.String(), nil)
	expect(t, u, 200, "")
	if usage := obj(t, u.body, "usage"); num(usage["assistantRuns"]) != 1 || num(usage["assistantTokens"]) != 4321 {
		t.Errorf("usage = %s", u.raw)
	}
	days := boss.do("GET", "/admin/usage?days=7", nil)
	if all := days.body["days"].([]any); num(all[len(all)-1].(map[string]any)["tokens"]) < 4321 {
		t.Errorf("usage by day = %s", days.raw)
	}
}
