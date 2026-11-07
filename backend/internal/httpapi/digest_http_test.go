package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

func (e *authEnv) digestJobs(t *testing.T, user uuid.UUID) int {
	t.Helper()
	var n int
	if err := tu.Require(t, testDB).Owner.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'assistant_digest' AND args->>'user_id' = $1`, user.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAssistantDigestHTTP: the digest schedule in the assistant settings, a digest asked for now,
// the daily token budget, and the instance switch (ADR 0033).
func TestAssistantDigestHTTP(t *testing.T) {
	e, _ := mockEnv(t, func(c *config.Config) { c.AITokensPerDay = 1000 })
	b := signedIn(t, e, testEmail(t))
	me := b.do("GET", "/me", nil)
	user := uuid.MustParse(me.body["id"].(string))
	if num(obj(t, me.body, "limits")["assistantTokensPerDay"]) != 1000 {
		t.Errorf("limits = %s", me.raw)
	}

	st := b.do("GET", "/assistant/settings", nil)
	expect(t, st, 200, "")
	if d := obj(t, st.body, "digest"); d["schedule"] != "off" || num(d["hour"]) != 7 || num(d["weekday"]) != 1 || d["timeZone"] != "Europe/Warsaw" ||
		st.body["digestNextAt"] != nil || num(st.body["tokensToday"]) != 0 {
		t.Errorf("settings = %s", st.raw)
	}
	weekly := map[string]any{"schedule": "weekly", "hour": 7, "weekday": 1, "timeZone": "Europe/Warsaw", "connection": "mock", "model": "mock-tools"}
	put := b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 2, "cloudConsent": []string{}, "digest": weekly})
	expect(t, put, 200, "")
	next, err := time.Parse(time.RFC3339, put.body["digestNextAt"].(string))
	if err != nil || obj(t, put.body, "digest")["schedule"] != "weekly" || !next.After(time.Now()) || time.Until(next) > 8*24*time.Hour {
		t.Errorf("saved = %s", put.raw)
	}
	// left out of a PUT (an older panel), the schedule stays
	if put := b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 3, "cloudConsent": []string{}}); obj(t, put.body, "digest")["schedule"] != "weekly" ||
		put.body["digestNextAt"] == nil {
		t.Errorf("kept = %s", put.raw)
	}
	bad := map[string]any{"schedule": "weekly", "hour": 7, "weekday": 1, "timeZone": "Mars/Olympus", "connection": "", "model": ""}
	expect(t, b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 2, "cloudConsent": []string{}, "digest": bad}), 422, "validation_failed")

	// one asked for now, queued once however often it is asked for
	expect(t, b.do("POST", "/assistant/digest", nil), 202, "")
	expect(t, b.do("POST", "/assistant/digest", nil), 202, "")
	if n := e.digestJobs(t, user); n != 1 {
		t.Errorf("%d digest jobs", n)
	}
	if list := b.do("GET", "/assistant/chats", nil); len(list.body["chats"].([]any)) != 0 {
		t.Errorf("chats = %s", list.raw) // the worker writes it
	}
	chat := b.do("POST", "/assistant/chats", nil)
	if chat.body["kind"] != "chat" {
		t.Errorf("chat = %s", chat.raw)
	}

	// the day's tokens used up: no digest, no question
	if err := e.st.WithUser(context.Background(), user, func(q *db.Queries, _ pgx.Tx) error {
		return q.AddAssistantUsage(context.Background(), db.AddAssistantUsageParams{UserID: user, Day: platform.DateOf(time.Now(), time.UTC), Tokens: 1000})
	}); err != nil {
		t.Fatal(err)
	}
	if st := b.do("GET", "/assistant/settings", nil); num(st.body["tokensToday"]) != 1000 {
		t.Errorf("tokens today = %s", st.raw)
	}
	r := b.do("POST", "/assistant/digest", nil)
	expect(t, r, 422, "limit_reached")
	if l := obj(t, r.body, "limit"); l["name"] != "assistantTokensPerDay" || num(l["value"]) != 1000 {
		t.Errorf("problem = %s", r.raw)
	}
	expect(t, b.do("POST", "/assistant/chats/"+chat.body["id"].(string)+"/messages", map[string]any{"text": "Hi"}), 422, "limit_reached")

	// switched off for the instance: no schedule, no digest now, and /meta says so
	boss, _ := e.adminBrowser(t, "boss-"+testEmail(t))
	t.Cleanup(func() {
		if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{}'`); err != nil {
			t.Error(err)
		}
	})
	if meta := e.browser().do("GET", "/meta", nil); obj(t, meta.body, "features")["assistantDigests"] != true {
		t.Errorf("meta = %s", meta.raw)
	}
	s := boss.do("GET", "/admin/settings", nil)
	expect(t, boss.do("PUT", "/admin/settings", map[string]any{"version": s.body["version"], "defaults": map[string]any{},
		"features": map[string]any{"assistantDigests": false}, "limits": map[string]any{}}), 200, "")
	if meta := e.browser().do("GET", "/meta", nil); obj(t, meta.body, "features")["assistantDigests"] != false ||
		obj(t, meta.body, "features")["assistant"] != true {
		t.Errorf("meta = %s", meta.raw)
	}
	expect(t, b.do("POST", "/assistant/digest", nil), 404, "not_found")
	r = b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 2, "cloudConsent": []string{}, "digest": weekly})
	expect(t, r, 422, "validation_failed")
	off := map[string]any{"schedule": "off", "hour": 7, "weekday": 1, "timeZone": "Europe/Warsaw", "connection": "", "model": ""}
	if r := b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 2, "cloudConsent": []string{}, "digest": off}); r.code != 200 ||
		r.body["digestNextAt"] != nil {
		t.Errorf("switching off = %s", r.raw)
	}
}

// TestDigestsInDemoMode: demo visitors never get digests.
func TestDigestsInDemoMode(t *testing.T) {
	e, _ := mockEnv(t, func(c *config.Config) { demoConfig(c); c.DemoAssistant = true })
	b := e.browser()
	expect(t, b.do("POST", "/auth/demo", nil), 200, "")
	if meta := b.do("GET", "/meta", nil); obj(t, meta.body, "features")["assistantDigests"] != false {
		t.Errorf("meta = %s", meta.raw)
	}
	weekly := map[string]any{"schedule": "weekly", "hour": 7, "weekday": 1, "timeZone": "Europe/Warsaw", "connection": "", "model": ""}
	expect(t, b.do("PUT", "/assistant/settings", map[string]any{"retentionDays": 2, "cloudConsent": []string{}, "digest": weekly}), 422, "validation_failed")
	expect(t, b.do("POST", "/assistant/digest", nil), 404, "not_found")
}
