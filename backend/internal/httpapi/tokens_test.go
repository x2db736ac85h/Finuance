package httpapi

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

// TestTokenRoutePolicy: every route of the API has a class for personal access tokens, written to
// testdata/token-routes.golden. A new route changes the file, so its class is reviewed before a
// token can use it (go test -run TestTokenRoutePolicy -update). The rules that must never change
// are checked here too (ADR 0036).
func TestTokenRoutePolicy(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for path, item := range spec.Paths.Map() {
		for method := range item.Operations() {
			pattern := method + " /api/v1" + path
			class := tokenPolicy(pattern)
			lines = append(lines, string(class)+"\t"+pattern)
			switch {
			case strings.HasPrefix(path, "/admin/"), strings.HasPrefix(path, "/auth/"), strings.HasPrefix(path, "/me/tokens"),
				strings.HasPrefix(path, "/me/sessions"), path == "/me/password", pattern == "DELETE /api/v1/me":
				if class != tokenNever {
					t.Errorf("%s is %s for tokens; it must be never", pattern, class)
				}
			case method == "GET":
				if class != tokenRead {
					t.Errorf("%s is %s; a GET is a read", pattern, class)
				}
			case class == tokenRead && !tokenReadPosts[pattern]:
				t.Errorf("%s reads without being in tokenReadPosts", pattern)
			}
		}
	}
	slices.Sort(lines)
	got := strings.Join(lines, "\n") + "\n"
	file := filepath.Join("testdata", "token-routes.golden")
	if *updateGolden {
		if err := os.WriteFile(file, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("the routes' token classes changed; review them and run with -update:\n%s", diffLines(string(want), got))
	}
	for p := range tokenReadPosts {
		if !slices.Contains(lines, "read\t"+p) {
			t.Errorf("tokenReadPosts lists %s, which the API doesn't have", p)
		}
	}
}

func diffLines(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	var out []string
	for _, l := range bl {
		if !slices.Contains(al, l) {
			out = append(out, "+ "+l)
		}
	}
	for _, l := range al {
		if !slices.Contains(bl, l) {
			out = append(out, "- "+l)
		}
	}
	return strings.Join(out, "\n")
}
