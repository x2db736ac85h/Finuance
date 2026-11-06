package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func unzip(t *testing.T, raw string) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader([]byte(raw)), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(r)
		_ = r.Close()
	}
	return out
}

func TestExportMeHTTP(t *testing.T) {
	e := newTestEnv(t, nil, true)
	b, accID := demoBrowser(t, e)
	q := b.do("POST", "/transactions/query", map[string]any{"context": map[string]any{}, "limit": 1})
	expect(t, b.do("PATCH", "/transactions/"+rowsOf(t, q)[0]["id"].(string), map[string]any{"note": "zakupy", "categoryName": "Moje"}), 200, "")
	expect(t, b.do("PUT", "/preferences", map[string]any{"version": 0, "prefs": map[string]any{"theme": "dark"}}), 200, "")
	// Phase 15: a rule, a renamed category (an alias) and a group budget are exported too.
	var rachunki string
	for _, c := range list(t, b.do("GET", "/categories/stats", nil)) {
		if c["name"] == "Rachunki" {
			rachunki = c["id"].(string)
		}
	}
	expect(t, b.do("PATCH", "/categories/"+rachunki, map[string]any{"name": "Opłaty"}), 200, "")
	expect(t, b.do("POST", "/rules", map[string]any{"name": "Internet", "match": map[string]any{"search": "operator internetu"},
		"setCategoryId": rachunki}), 201, "")
	expect(t, b.do("POST", "/groups", map[string]any{"name": "Trip", "budgetMinor": 50000}), 201, "")

	r := b.do("GET", "/me/export", nil)
	expect(t, r, 200, "")
	if r.header.Get("Content-Type") != "application/zip" || !strings.Contains(r.header.Get("Content-Disposition"), "finuance-export-") {
		t.Errorf("headers = %v", r.header)
	}
	files := unzip(t, r.raw)
	for _, name := range []string{"manifest.json", "accounts.json", "categories.json", "groups.json", "budgets.json", "annotations.json",
		"dedupe-overrides.json", "imports.json", "investments.json", "preferences.json", "assistant.json", "transactions.csv",
		"rules.json", "category-aliases.json", "activity.json", "friends.json", "notifications.json", "shared-groups.json", "ai-changes.json"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s (have %d files)", name, len(files))
		}
	}
	var manifest map[string]any
	_ = json.Unmarshal(files["manifest.json"], &manifest)
	if manifest["format"] != "finuance-export" || num(manifest["version"]) != 1 || manifest["user"].(map[string]any)["email"] != testEmail(t) {
		t.Errorf("manifest = %s", files["manifest.json"])
	}
	var accounts, annotations, imports []map[string]any
	_ = json.Unmarshal(files["accounts.json"], &accounts)
	_ = json.Unmarshal(files["annotations.json"], &annotations)
	_ = json.Unmarshal(files["imports.json"], &imports)
	if len(accounts) != 1 || accounts[0]["id"] != accID || accounts[0]["kind"] != "bank" || len(annotations) != 1 || annotations[0]["note"] != "zakupy" ||
		len(imports) != 1 || imports[0]["status"] != "committed" {
		t.Errorf("accounts %s\nannotations %s\nimports %s", files["accounts.json"], files["annotations.json"], files["imports.json"])
	}
	var rules, aliases, groups []map[string]any
	_ = json.Unmarshal(files["rules.json"], &rules)
	_ = json.Unmarshal(files["category-aliases.json"], &aliases)
	_ = json.Unmarshal(files["groups.json"], &groups)
	if len(rules) != 1 || rules[0]["setCategory"] != "Opłaty" || obj(t, rules[0], "match")["search"] != "operator internetu" ||
		len(aliases) != 1 || aliases[0]["bankCategory"] != "rachunki" || aliases[0]["categoryName"] != "Opłaty" ||
		len(groups) != 1 || num(groups[0]["budgetMinor"]) != 50000 {
		t.Errorf("rules %s\naliases %s\ngroups %s", files["rules.json"], files["category-aliases.json"], files["groups.json"])
	}
	// Phase 17: the activity log; this export is recorded after its documents were read
	var act []map[string]any
	_ = json.Unmarshal(files["activity.json"], &act)
	if len(act) != 1 || act[0]["action"] != "import.committed" || act[0]["entityId"] != imports[0]["id"] || act[0]["at"] == nil {
		t.Errorf("activity %s", files["activity.json"])
	}
	if got := actions(t, b); len(got) != 2 || got[1] != "export.data" {
		t.Errorf("activity after the export = %v", got)
	}
	if !strings.Contains(string(files["preferences.json"]), `"theme": "dark"`) || string(files["investments.json"]) != "null\n" {
		t.Errorf("preferences %s investments %s", files["preferences.json"], files["investments.json"])
	}
	csv := string(files["transactions.csv"])
	if !strings.HasPrefix(csv, "\ufeffDate;Value date;Payee;Description;Amount;") || strings.Count(csv, "\r\n") != 917 ||
		!strings.Contains(csv, ";Moje;") || !strings.Contains(csv, ";zakupy;") {
		t.Errorf("transactions.csv: %q… %d lines", csv[:min(120, len(csv))], strings.Count(csv, "\r\n"))
	}
	orig := fixture(t, "demo/demo-legacy.csv")
	if got := files["imports/"+imports[0]["id"].(string)+"/demo.csv"]; !bytes.Equal(got, orig) {
		t.Errorf("original file: %d bytes, want %d", len(got), len(orig))
	}

	// Another user's export holds only their own (empty) data.
	other := signedIn(t, e, "other-"+testEmail(t))
	o := other.do("GET", "/me/export", nil)
	expect(t, o, 200, "")
	of := unzip(t, o.raw)
	if string(of["accounts.json"]) != "[]\n" || strings.Count(string(of["transactions.csv"]), "\r\n") != 0 || len(of) != 19 || string(of["ai-changes.json"]) != "[]\n" ||
		string(of["rules.json"]) != "[]\n" || string(of["category-aliases.json"]) != "[]\n" || string(of["activity.json"]) != "[]\n" {
		t.Errorf("other user's export: accounts %s, %d files", of["accounts.json"], len(of))
	}
}

func TestZipName(t *testing.T) {
	for in, want := range map[string]string{"a.csv": "a.csv", "../../etc/passwd": "passwd", `C:\x\y.csv`: "y.csv", "..": "file.csv", "": "file.csv", "/": "file.csv"} {
		if got := zipName(in); got != want {
			t.Errorf("zipName(%q) = %q, want %q", in, got, want)
		}
	}
}
