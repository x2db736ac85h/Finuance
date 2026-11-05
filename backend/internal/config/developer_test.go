package config

import (
	"strings"
	"testing"
)

func TestDeveloperMode(t *testing.T) {
	// off by default, and the links file isn't read while it is off
	c, err := Load(env(map[string]string{"FINUANCE_DEVELOPER_LINKS_FILE": "/nonexistent/links.json"}), ForHealthcheck)
	if err != nil || c.Developer != nil {
		t.Fatalf("off: %+v, %v", c.Developer, err)
	}
	// on without a file: the build only
	c, err = Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true"}), ForHealthcheck)
	if err != nil || c.Developer == nil || c.Developer.Source != nil || len(c.Developer.Links) != 0 {
		t.Fatalf("on: %+v, %v", c.Developer, err)
	}
	f := writeFile(t, `{"source": "https://github.com/example/finuance", "links": [
		{"group": "Observability", "name": "Grafana", "url": "http://localhost:3000",
		 "note": "User admin.\nPassword: see the second command.", "commands": ["kubectl -n monitoring port-forward svc/grafana 3000:80", "kubectl get secret x"]},
		{"name": "Actions", "url": "https://github.com/example/finuance/actions"}]}`)
	c, err = Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true", "FINUANCE_DEVELOPER_LINKS_FILE": f}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	d := c.Developer
	if d.Source.String() != "https://github.com/example/finuance" || len(d.Links) != 2 {
		t.Fatalf("developer = %+v", d)
	}
	if g := d.Links[0]; g.Group != "Observability" || g.Name != "Grafana" || g.URL.String() != "http://localhost:3000" ||
		!strings.Contains(g.Note, "\n") || len(g.Commands) != 2 || !strings.HasPrefix(g.Commands[0], "kubectl") {
		t.Errorf("link = %+v", g)
	}
	// never on a demo instance
	_, err = Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true", "FINUANCE_DEMO_MODE": "true"}), ForHealthcheck)
	if err == nil || !strings.Contains(err.Error(), "FINUANCE_DEVELOPER_MODE: must not be true when FINUANCE_DEMO_MODE is") {
		t.Errorf("demo: %v", err)
	}
}

func TestDeveloperLinksProblems(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"not JSON", `{`, []string{"not a valid developer links file"}},
		{"unknown field", `{"links":[{"name":"a","url":"http://x","password":"secret"}]}`, []string{`unknown field "password"`}},
		{"every problem at once", `{"source": "git@github.com:example/f.git", "links": [
			{"name": "", "url": "http://x"},
			{"name": "Script", "url": "javascript:alert(document.cookie)"},
			{"name": "Data", "url": "data:text/html,<b>x</b>"},
			{"name": "Creds", "url": "https://admin:secret@grafana.example.com"},
			{"name": "Two\nlines", "url": "http://x"},
			{"name": "Group", "group": "` + strings.Repeat("g", 41) + `", "url": "http://x"},
			{"name": "Command", "url": "http://x", "commands": ["a\nb"]},
			{"name": "Commands", "url": "http://x", "commands": ["a", "b", "c", "d", "e"]},
			{"name": "Note", "url": "http://x", "note": "` + strings.Repeat("n", 601) + `"},
			{"name": "Bell", "url": "http://x", "note": "\u0007"}]}`,
			[]string{"source: url must be", "link 1: name must be", `"Script": url must be`, `"Data": url must be`,
				`"Creds": url must not contain a user name or password`, "name must be 1–60 characters on one line",
				`"Group": group must be at most 40`, `"Command": each command must be 1–600 characters on one line`, `"Commands": at most 4 commands`,
				`"Note": note must be at most 600`, `"Bell": note must not contain control`}},
		{"too many", `{"links":[` + strings.TrimSuffix(strings.Repeat(`{"name":"a","url":"http://x"},`, 41), ",") + `]}`,
			[]string{"at most 40 links"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true",
				"FINUANCE_DEVELOPER_LINKS_FILE": writeFile(t, tc.body)}), ForHealthcheck)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("missing %q in:\n%v", w, err)
				}
			}
			if strings.Contains(err.Error(), "secret") {
				t.Error("the error echoes a secret")
			}
		})
	}
	big := writeFile(t, `{"links":[`+strings.Repeat(" ", maxDeveloperFile)+`]}`)
	if _, err := Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true", "FINUANCE_DEVELOPER_LINKS_FILE": big}),
		ForHealthcheck); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("big file: %v", err)
	}
}

// The example file in deploy/ stays valid.
func TestDeveloperLinksExample(t *testing.T) {
	c, err := Load(env(map[string]string{"FINUANCE_DEVELOPER_MODE": "true",
		"FINUANCE_DEVELOPER_LINKS_FILE": "../../../deploy/developer/links.example.json"}), ForHealthcheck)
	if err != nil || c.Developer.Source == nil || len(c.Developer.Links) != 8 {
		t.Fatalf("example: %+v, %v", c.Developer, err)
	}
}
