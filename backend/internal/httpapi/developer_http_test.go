package httpapi

import (
	"net/url"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

func developerConfig(c *config.Config) {
	src, _ := url.Parse("https://github.com/example/finuance")
	grafana, _ := url.Parse("http://localhost:3000")
	c.Developer = &config.Developer{Source: src, Links: []config.DeveloperLink{
		{Group: "Observability", Name: "Grafana", URL: grafana, Note: "User admin; the password: see the command.",
			Commands: []string{"kubectl -n monitoring port-forward svc/grafana 3000:80"}},
		{Name: "Plain", URL: src},
	}}
}

func TestDeveloperInfoHTTP(t *testing.T) {
	e := newTestEnv(t, developerConfig, false)
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	alex := signupBrowser(t, e, testEmail(t))

	r := boss.do("GET", "/admin/developer", nil)
	expect(t, r, 200, "")
	if cc := r.header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	b := r.body["build"].(map[string]any)
	if b["version"] != "test" || b["commit"] != "0123abcd" || b["environment"] != "test" || b["instance"] == "" ||
		!strings.HasPrefix(b["goVersion"].(string), "go") ||
		b["schemaVersion"].(float64) != float64(store.ExpectedVersion()) || b["expectedSchemaVersion"].(float64) != float64(store.ExpectedVersion()) {
		t.Errorf("build = %v", b)
	}
	src := r.body["source"].(map[string]any)
	if src["url"] != "https://github.com/example/finuance" || src["commitUrl"] != "https://github.com/example/finuance/commit/0123abcd" ||
		src["releaseUrl"] != nil {
		t.Errorf("source = %v", src)
	}
	links := r.body["links"].([]any)
	g, plain := links[0].(map[string]any), links[1].(map[string]any)
	if len(links) != 2 || g["group"] != "Observability" || g["name"] != "Grafana" || g["url"] != "http://localhost:3000" ||
		!strings.HasPrefix(g["commands"].([]any)[0].(string), "kubectl") || plain["group"] != "Links" || plain["note"] != nil {
		t.Errorf("links = %v", links)
	}

	// anyone else: the same 404 as for any route that doesn't exist for them, and nothing elsewhere
	expect(t, alex.do("GET", "/admin/developer", nil), 404, "not_found")
	expect(t, e.browser().do("GET", "/admin/developer", nil), 401, "unauthenticated")
	for _, path := range []string{"/meta", "/me"} {
		for _, who := range []*browser{alex, boss, e.browser()} {
			raw := strings.ToLower(who.do("GET", path, nil).raw)
			for _, w := range []string{`"developer`, "developermode", "localhost:3000", "github.com", "0123abcd", "kubectl"} {
				if strings.Contains(raw, w) {
					t.Errorf("%s mentions %q: %s", path, w, raw)
				}
			}
		}
	}
}

func TestDeveloperInfoOff(t *testing.T) {
	e := newTestEnv(t, nil, false)
	boss, _ := e.adminBrowser(t, strings.Replace(testEmail(t), "@", "-boss@", 1))
	expect(t, boss.do("GET", "/admin/developer", nil), 404, "not_found")
}

func TestSourceLinks(t *testing.T) {
	s := sourceLinks("https://github.com/example/finuance.git", "github.com", "0.3.0-rc.1", "9ae8f60")
	if *s.ReleaseUrl != "https://github.com/example/finuance/releases/tag/v0.3.0-rc.1" ||
		*s.CommitUrl != "https://github.com/example/finuance/commit/9ae8f60" {
		t.Errorf("github = %+v", s)
	}
	for _, tc := range []struct{ repo, host, version, commit string }{
		{"https://git.example.com/finuance", "git.example.com", "0.3.0", "9ae8f60"}, // not GitHub: the repository only
		{"https://github.com/example/finuance", "github.com", "dev", "unknown"},     // a local build
		{"https://github.com/example/finuance", "github.com", "1.0.0/../x", "9ae8f60/x"},
	} {
		s := sourceLinks(tc.repo, tc.host, tc.version, tc.commit)
		if s.ReleaseUrl != nil || (s.CommitUrl != nil && tc.host != "github.com") || (tc.commit != "9ae8f60" && s.CommitUrl != nil) {
			t.Errorf("%+v → %+v", tc, s)
		}
	}
}
