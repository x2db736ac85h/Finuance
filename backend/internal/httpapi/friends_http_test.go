package httpapi

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

func named(t *testing.T, e *authEnv, suffix, name string) *browser {
	t.Helper()
	b := signedIn(t, e, strings.Replace(testEmail(t), "@", "-"+suffix+"@", 1))
	if name != "" {
		expect(t, b.do("PATCH", "/me", map[string]any{"displayName": name}), 200, "")
	}
	return b
}

// TestFriendsHTTP (ADR 0027): a code makes two people friends, the owner is notified, the e-mail
// never shows, a code works once and only within its hour plus the grace, and either side can
// remove the other.
func TestFriendsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	alex, bo := named(t, e, "alex", "Alex"), named(t, e, "bo", "Bo")
	nameless := named(t, e, "nameless", "")

	expect(t, nameless.do("POST", "/friends/code", nil), 422, "display_name_required")
	code := alex.do("POST", "/friends/code", nil)
	expect(t, code, 201, "")
	c := code.body["code"].(string)
	if len(c) != 9 || c[4] != '-' {
		t.Fatalf("code = %q", c)
	}
	expect(t, alex.do("POST", "/friends", map[string]any{"code": c}), 422, "validation_failed") // own code
	expect(t, nameless.do("POST", "/friends", map[string]any{"code": c}), 422, "display_name_required")
	expect(t, bo.do("POST", "/friends", map[string]any{"code": "ZZZZ-ZZZZ"}), 422, "validation_failed")

	// typed in lower case without the dash
	add := bo.do("POST", "/friends", map[string]any{"code": strings.ToLower(strings.ReplaceAll(c, "-", ""))})
	expect(t, add, 201, "")
	if add.body["displayName"] != "Alex" {
		t.Errorf("friend = %s", add.raw)
	}
	// the code is used up
	expect(t, nameless.do("POST", "/friends", map[string]any{"code": c}), 422, "validation_failed")

	for _, b := range []*browser{alex, bo} {
		list := b.do("GET", "/friends", nil)
		expect(t, list, 200, "")
		if strings.Contains(list.raw, "@") || strings.Count(list.raw, `"id"`) != 1 {
			t.Errorf("friends list = %s", list.raw)
		}
	}
	notes := alex.do("GET", "/notifications", nil)
	expect(t, notes, 200, "")
	if num(notes.body["unread"]) != 1 || !strings.Contains(notes.raw, `"friend.added"`) || !strings.Contains(notes.raw, `"Bo"`) || strings.Contains(notes.raw, "@") {
		t.Errorf("notifications = %s", notes.raw)
	}
	expect(t, alex.do("POST", "/notifications/read", map[string]any{}), 204, "")
	if n := alex.do("GET", "/notifications", nil); num(n.body["unread"]) != 0 {
		t.Errorf("unread after read = %v", n.body["unread"])
	}
	if n := bo.do("GET", "/notifications", nil); num(n.body["unread"]) != 0 || len(n.body["items"].([]any)) != 0 {
		t.Errorf("bo sees %s", n.raw) // notifications are per user
	}

	// friends already
	again := bo.do("POST", "/friends/code", nil)
	expect(t, again, 201, "")
	expect(t, alex.do("POST", "/friends", map[string]any{"code": again.body["code"]}), 409, "conflict")

	// expiry: the clock hour plus 10 minutes
	late := nameless
	expect(t, late.do("PATCH", "/me", map[string]any{"displayName": "Cy"}), 200, "")
	fresh := alex.do("POST", "/friends/code", nil)
	expect(t, fresh, 201, "")
	exp, err := time.Parse(time.RFC3339Nano, fresh.body["expiresAt"].(string))
	if err != nil || exp.Minute() != 10 || exp.Second() != 0 {
		t.Fatalf("expiresAt = %v (%v)", fresh.body["expiresAt"], err)
	}
	e.clock.Add(exp.Sub(e.clock.Now()) + time.Second) // from the fake clock, not the wall clock
	expect(t, late.do("POST", "/friends", map[string]any{"code": fresh.body["code"]}), 422, "validation_failed")

	// removing tells the other side
	friendID := add.body["id"].(string)
	other := alex.do("GET", "/friends", nil)
	expect(t, bo.do("DELETE", "/friends/"+friendID, nil), 204, "")
	expect(t, bo.do("DELETE", "/friends/"+friendID, nil), 404, "not_found")
	if n := alex.do("GET", "/notifications", nil); !strings.Contains(n.raw, `"friend.removed"`) {
		t.Errorf("no removal notice: %s (%s)", n.raw, other.raw)
	}
	act := alex.do("GET", "/me/activity", nil)
	if !strings.Contains(act.raw, "friend.added") || !strings.Contains(act.raw, "friend.removed") || !strings.Contains(act.raw, "friend.code_failed") {
		t.Errorf("activity = %s", act.raw)
	}
}

// TestFriendCodeTries: 10 codes an hour per user, then 429.
func TestFriendCodeTries(t *testing.T) {
	e := newTestEnv(t, nil, false)
	b := named(t, e, "guess", "Guess")
	for i := range 10 {
		expect(t, b.do("POST", "/friends", map[string]any{"code": fmt.Sprintf("AAAA-AA%02d", i)}), 422, "validation_failed")
	}
	expect(t, b.do("POST", "/friends", map[string]any{"code": "AAAA-AAAA"}), 429, "rate_limited")
}

// TestFriendsOff: the switch and demo mode take the routes away.
func TestFriendsOff(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.DemoMode = true }, false)
	b := named(t, e, "demo", "Demo")
	expect(t, b.do("GET", "/friends", nil), 404, "not_found")
	expect(t, b.do("POST", "/friends/code", nil), 404, "not_found")

	e2 := newTestEnv(t, nil, false)
	if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{"features":{"friends":false}}'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{}'`)
	})
	e2.clock.Add(time.Minute) // past the settings cache
	c := named(t, e2, "off", "Off")
	expect(t, c.do("GET", "/friends", nil), 404, "not_found")
	expect(t, c.do("GET", "/notifications", nil), 200, "")
}

// TestNotificationKindsMatchTheAPI keeps notify.Kinds and the API's NotificationKind equal (the web
// words each kind).
func TestNotificationKindsMatchTheAPI(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	var api []string
	for _, v := range spec.Components.Schemas["NotificationKind"].Value.Enum {
		api = append(api, v.(string))
	}
	got := slices.Clone(notify.Kinds)
	slices.Sort(api)
	slices.Sort(got)
	if !slices.Equal(api, got) {
		t.Errorf("API kinds %v, notify.Kinds %v", api, got)
	}
}
