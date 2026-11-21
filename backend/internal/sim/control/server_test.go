package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/control/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

var token = strings.Repeat("t", MinTokenBytes)

// lab answers what a persona needs to start, and one route a test action uses.
func lab(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/meta":
			_, _ = w.Write([]byte(`{"simulation":true}`))
		case "POST /api/v1/auth/login":
			_, _ = w.Write([]byte(`{}`))
		case "GET /api/v1/me/tokens":
			_, _ = w.Write([]byte(`[]`))
		case "POST /api/v1/me/tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"secret":"fin_x","token":{"id":"00000000-0000-4000-8000-000000000009","name":"x","prefix":"fin_","scope":"write"}}`))
		case "GET /api/v1/accounts":
			_, _ = w.Write([]byte(`[{"id":"00000000-0000-4000-8000-000000000001","name":"Current account","kind":"bank","txCount":3},` +
				`{"id":"00000000-0000-4000-8000-000000000002","name":"Savings","kind":"bank","txCount":1}]`))
		case "GET /api/v1/notifications":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func notifications(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.ListNotificationsWithResponse(ctx, nil)
	if err != nil {
		return err
	}
	return sim.Expect("notifications", r.HTTPResponse, http.StatusOK)
}

func newServer(t *testing.T) (*httptest.Server, *sim.Engine, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	e, err := sim.New(sim.Options{Target: lab(t).URL, Seed: []byte(strings.Repeat("s", identity.MinSeedBytes)), Version: "test",
		Actions: []sim.Action{{Name: "account.notifications", Area: "account", Run: notifications}}, Config: sim.Defaults(),
		Metrics: sim.NewMetrics(reg)})
	if err != nil {
		t.Fatal(err)
	}
	h, err := Handler(Options{Engine: e, Token: token, Version: "test", Grafana: "http://localhost:3000", Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, e, reg
}

// call sends a control API request as the tuning page does; edit changes it first.
func call(t *testing.T, srv *httptest.Server, method, path string, body any, edit func(*http.Request)) (*http.Response, []byte) {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, srv.URL+path, &b)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Finuance-Client", "web")
	req.Header.Set("Origin", srv.URL)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if edit != nil {
		edit(req)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp, out.Bytes()
}

// TestGuard: every API route needs the token, the client header and the simulator's own Origin
// (THREAT-MODEL D14); the page, /metrics and /healthz need none, and the page has the app's CSP.
func TestGuard(t *testing.T) {
	srv, _, _ := newServer(t)
	for name, c := range map[string]struct {
		edit func(*http.Request)
		want int
	}{
		"everything right":         {nil, http.StatusOK},
		"no token":                 {func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		"a wrong token":            {func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", MinTokenBytes)) }, http.StatusUnauthorized},
		"the token without Bearer": {func(r *http.Request) { r.Header.Set("Authorization", token) }, http.StatusUnauthorized},
		"no client header":         {func(r *http.Request) { r.Header.Del("X-Finuance-Client") }, http.StatusForbidden},
		"another site's Origin":    {func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		"no Origin (curl)":         {func(r *http.Request) { r.Header.Del("Origin") }, http.StatusOK},
	} {
		if resp, body := call(t, srv, http.MethodGet, "/api/state", nil, c.edit); resp.StatusCode != c.want {
			t.Errorf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
	for _, path := range []string{"/", "/metrics", "/healthz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
		if path == "/" && !strings.Contains(resp.Header.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Errorf("the page's CSP: %q", resp.Header.Get("Content-Security-Policy"))
		}
	}
	if _, err := Handler(Options{Token: "short"}); err == nil {
		t.Error("a short control token was accepted")
	}
}

// TestConfiguration: a stale generation is refused with the current one, every value is held to
// the ceilings, presets, pause, resume and reset change it, and the metrics follow.
func TestConfiguration(t *testing.T) {
	srv, _, reg := newServer(t)
	var doc gen.ConfigDoc
	resp, body := call(t, srv, http.MethodGet, "/api/config", nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &doc) != nil {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	stale := doc
	doc.Config.Intensity, doc.Config.Personas, doc.Config.ClockRate = 99_000, 500, 1_000_000
	resp, body = call(t, srv, http.MethodPut, "/api/config", doc, nil)
	var stored gen.ConfigDoc
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &stored) != nil {
		t.Fatalf("put: %d %s", resp.StatusCode, body)
	}
	if c := stored.Config; c.Intensity != sim.MaxIntensity || c.Personas != sim.MaxPersonas || c.ClockRate != sim.MaxClockRate ||
		stored.Generation != doc.Generation+1 {
		t.Errorf("stored %+v", stored)
	}
	if resp, body = call(t, srv, http.MethodPut, "/api/config", stale, nil); resp.StatusCode != http.StatusConflict ||
		!strings.Contains(string(body), `"intensity":3000`) {
		t.Errorf("a stale generation: %d %s", resp.StatusCode, body)
	}
	if resp, body = call(t, srv, http.MethodPut, "/api/config", map[string]any{"generation": 1, "config": map[string]any{"personas": "ten"}}, nil); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("a malformed configuration: %d %s", resp.StatusCode, body)
	}
	for path, check := range map[string]func(gen.Config) bool{
		"/api/presets/error-storm": func(c gen.Config) bool { return c.Mistakes == 300 },
		"/api/pause":               func(c gen.Config) bool { return c.Paused },
		"/api/resume":              func(c gen.Config) bool { return !c.Paused },
		"/api/config/reset":        func(c gen.Config) bool { return c.Intensity == 1000 && c.Personas == len(identity.Animals) },
	} {
		resp, body := call(t, srv, http.MethodPost, path, nil, nil)
		var d gen.ConfigDoc
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &d) != nil || !check(d.Config) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	if resp, _ := call(t, srv, http.MethodPost, "/api/presets/bitcoin", nil, nil); resp.StatusCode == http.StatusOK {
		t.Error("an unknown preset was applied")
	}
	call(t, srv, http.MethodPost, "/api/pause", nil, nil)
	if got := gauge(t, reg, "finuance_sim_paused"); got != 1 {
		t.Errorf("finuance_sim_paused = %v", got)
	}
}

func gauge(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("no metric %s", name)
	return 0
}

// TestActNowAndTheFeed: "act now" runs one persona's action at once and answers with its
// request id, the state counts it, and the feed streams it.
func TestActNowAndTheFeed(t *testing.T) {
	srv, _, reg := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Finuance-Client", "web")
	feed, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Body.Close()
	if feed.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("the feed: %d %s", feed.StatusCode, feed.Header.Get("Content-Type"))
	}

	for _, bad := range []map[string]string{{"persona": "unicorn", "action": "account.notifications"}, {"persona": "fox", "action": "fly"}} {
		if resp, body := call(t, srv, http.MethodPost, "/api/act", bad, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%v: %d %s", bad, resp.StatusCode, body)
		}
	}
	resp, body := call(t, srv, http.MethodPost, "/api/act", map[string]string{"persona": "fox", "action": "account.notifications"}, nil)
	var ev gen.Event
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &ev) != nil || ev.Result != gen.Ok ||
		ev.RequestId == nil || !strings.HasPrefix(*ev.RequestId, "sim-fox-") {
		t.Fatalf("act now: %d %s", resp.StatusCode, body)
	}
	sc := bufio.NewScanner(feed.Body)
	got := ""
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") && strings.Contains(line, *ev.RequestId) {
			got = line
			break
		}
	}
	if got == "" {
		t.Error("the feed did not show the action")
	}
	var st gen.State
	_, body = call(t, srv, http.MethodGet, "/api/state", nil, nil)
	if json.Unmarshal(body, &st) != nil || st.Totals.Ok != 1 || st.Grafana == nil {
		t.Errorf("state: %s", body)
	}
	for _, p := range st.Personas {
		if p.Slug == "fox" && (p.Counts.Ok != 1 || !p.SignedIn || p.Last == nil) {
			t.Errorf("fox: %+v", p)
		}
	}
	mfs, _ := reg.Gather()
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "finuance_sim_actions_total" {
			found = true
		}
	}
	if !found {
		t.Error("finuance_sim_actions_total was not counted")
	}
	var cat gen.Catalogue
	_, body = call(t, srv, http.MethodGet, "/api/catalogue", nil, nil)
	if json.Unmarshal(body, &cat) != nil || len(cat.Actions) != 1 || len(cat.Characters) != 10 || len(cat.Presets) != 5 ||
		cat.Ceilings.ActionsPerMinute != 30 {
		t.Errorf("catalogue: %s", body)
	}
}
