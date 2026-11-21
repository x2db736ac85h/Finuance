package httpapi

import (
	"context"
	"strings"
	"testing"

	promtest "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
)

// requests counts http_server_requests_total for GET /me with a 2xx answer under one traffic label.
func (e *authEnv) meRequests(traffic string) float64 {
	return promtest.ToFloat64(e.metrics.HTTPRequests.WithLabelValues("GET /api/v1/me", "GET", "2xx", traffic))
}

// TestTrafficLabelComesFromTheUser: whether a request counts as synthetic is read from the
// signed-in user's row and from nowhere else (ADR 0041, PLAN 24.7). A person can't make their
// requests look synthetic, which would hide their errors from the service-level numbers, and a
// synthetic user can't make theirs look real: no header, cookie, query or body field is read.
func TestTrafficLabelComesFromTheUser(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.Simulation = true }, false)
	ctx := context.Background()
	claims := map[string]string{
		"X-Finuance-Synthetic": "true", "X-Synthetic": "1", "X-Traffic": "synthetic", "Baggage": "finuance.synthetic=true,traffic=synthetic",
		"User-Agent": "finuance-simulator/1.0 (synthetic)", "Cookie-Extra": "traffic=synthetic",
		"Traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", "Tracestate": "finuance=synthetic",
	}

	person := signupBrowser(t, e, testEmail(t))
	for k, v := range claims {
		person.header[k] = v
	}
	if r := person.do("GET", "/me?synthetic=true&traffic=synthetic", nil); r.code != 200 {
		t.Fatalf("a person's request: %d %s", r.code, r.raw)
	}
	expect(t, person.do("PATCH", "/me", map[string]any{"displayName": "Alex", "synthetic": true, "kind": "synthetic"}), 422, "")
	if real, synth := e.meRequests("real"), e.meRequests("synthetic"); real != 1 || synth != 0 {
		t.Errorf("a person who claims to be synthetic: real = %v, synthetic = %v, want 1 and 0", real, synth)
	}
	if strings.Contains(e.logs.String(), `"traffic":"synthetic"`) {
		t.Errorf("a person's access line says synthetic:\n%s", e.logs.String())
	}

	mail := "synthetic-" + testEmail(t)
	robot := signupBrowser(t, e, mail)
	u, err := e.auth.UserByEmail(ctx, mail)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.auth.MarkSynthetic(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"X-Finuance-Synthetic": "false", "X-Traffic": "real", "Baggage": "finuance.synthetic=false"} {
		robot.header[k] = v
	}
	if r := robot.do("GET", "/me?synthetic=false", nil); r.code != 200 {
		t.Fatalf("a synthetic user's request: %d %s", r.code, r.raw)
	}
	if real, synth := e.meRequests("real"), e.meRequests("synthetic"); real != 1 || synth != 1 {
		t.Errorf("after a synthetic user's request: real = %v, synthetic = %v, want 1 and 1", real, synth)
	}
	if !strings.Contains(e.logs.String(), `"traffic":"synthetic"`) {
		t.Errorf("no access line says synthetic:\n%s", e.logs.String())
	}
}

// TestSyntheticUsersNeedASimulationInstance: on an instance without FINUANCE_SIMULATION nobody
// can be made synthetic, and a synthetic user (from a restored backup, say) can neither sign in
// nor use a session or an access token it still holds.
func TestSyntheticUsersNeedASimulationInstance(t *testing.T) {
	ctx := context.Background()
	mail := testEmail(t)

	lab := newTestEnv(t, func(c *config.Config) { c.Simulation = true }, false)
	robot := signupBrowser(t, lab, mail)
	u, err := lab.auth.UserByEmail(ctx, mail)
	if err != nil {
		t.Fatal(err)
	}
	if err := lab.auth.MarkSynthetic(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, robot.do("GET", "/me", nil), 200, "")
	expect(t, lab.browser().do("POST", "/auth/login", map[string]any{"email": mail, "password": password}), 200, "")
	if r := lab.browser().do("GET", "/meta", nil); r.body["simulation"] != true {
		t.Errorf("a lab's /meta does not say so: %s", r.raw)
	}

	// the same database behind an instance that is not a simulation
	plain := newTestEnv(t, nil, false)
	if r := plain.browser().do("GET", "/meta", nil); r.body["simulation"] != false {
		t.Errorf("/meta of an instance that is not a lab: %s", r.raw)
	}
	expect(t, plain.browser().do("POST", "/auth/login", map[string]any{"email": mail, "password": password}), 403, "account_disabled")
	other := "person-" + mail
	signupBrowser(t, plain, other)
	p, err := plain.auth.UserByEmail(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.auth.MarkSynthetic(ctx, p.ID); err == nil {
		t.Error("a user was made synthetic on an instance without FINUANCE_SIMULATION")
	}
}

// TestSyntheticGuardRails: a synthetic user is never an administrator, never a person's friend
// (in either direction, and the refused code still works for someone else), and is counted on its
// own in the administrators' totals.
func TestSyntheticGuardRails(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.Simulation = true }, false)
	ctx := context.Background()
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	personMail, robotMail, friendMail := "person-"+testEmail(t), "robot-"+testEmail(t), "friend-"+testEmail(t)
	person, robot, friend := signupBrowser(t, e, personMail), signupBrowser(t, e, robotMail), signupBrowser(t, e, friendMail)
	// the tests share one database: the count is compared before and after
	synthetic := func() float64 {
		usage := boss.do("GET", "/admin/usage", nil)
		if usage.code != 200 {
			t.Fatalf("the totals: %d %s", usage.code, usage.raw)
		}
		n, _ := usage.body["totals"].(map[string]any)["synthetic"].(float64)
		return n
	}
	before := synthetic()
	ru, err := e.auth.UserByEmail(ctx, robotMail)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.auth.MarkSynthetic(ctx, ru.ID); err != nil {
		t.Fatal(err)
	}

	// never an administrator: through the API, and not for one who already is
	if r := boss.do("PATCH", "/admin/users/"+ru.ID.String(), map[string]any{"admin": true}); r.code != 422 {
		t.Errorf("making a synthetic user an administrator: %d %s", r.code, r.raw)
	}
	bu, _ := e.auth.UserByEmail(ctx, strings.Replace(testEmail(t), "@", "-boss@", 1))
	if err := e.auth.MarkSynthetic(ctx, bu.ID); err == nil {
		t.Error("an administrator was made synthetic")
	}

	// the administrators see which accounts are synthetic, and how many
	list := boss.do("GET", "/admin/users?search="+strings.Split(robotMail, "@")[0], nil)
	if list.code != 200 || !strings.Contains(list.raw, `"isSynthetic":true`) {
		t.Errorf("the user list: %d %s", list.code, list.raw)
	}
	if after := synthetic(); after != before+1 {
		t.Errorf("synthetic users in the totals: %v before, %v after, want one more", before, after)
	}

	// never a person's friend, whoever made the code
	code := func(b *browser) string {
		r := b.do("POST", "/friends/code", nil)
		if r.code != 201 {
			t.Fatalf("a friend code: %d %s", r.code, r.raw)
		}
		c, _ := r.body["code"].(string)
		return c
	}
	personCode := code(person)
	if r := robot.do("POST", "/friends", map[string]any{"code": personCode}); r.code < 400 {
		t.Errorf("a synthetic user used a person's code: %d %s", r.code, r.raw)
	}
	if r := person.do("POST", "/friends", map[string]any{"code": code(robot)}); r.code < 400 {
		t.Errorf("a person used a synthetic user's code: %d %s", r.code, r.raw)
	}
	// the refusal did not use the person's code up: another person can still take it
	if r := friend.do("POST", "/friends", map[string]any{"code": personCode}); r.code >= 400 {
		t.Errorf("the code was used up by the refused attempt: %d %s", r.code, r.raw)
	}
}
