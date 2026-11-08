package instance

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func p[T any](v T) *T { return &v }

func TestContrastKnownValues(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"#000000", "#ffffff", 21},
		{"#ffffff", "#ffffff", 1},
		{"#777777", "#ffffff", 4.48}, // the classic just-below-AA grey
		{"#767676", "#ffffff", 4.54}, // the lightest AA grey on white
		{"#6d5ce7", "#ffffff", 4.84}, // Indigo's solid button with white text (tokens.css: 4.8:1)
		{"#9789f7", "#232325", 5.40}, // Indigo's dark accent on the panel
	} {
		if got := ContrastHex(c.a, c.b); math.Abs(got-c.want) > 0.01 {
			t.Errorf("Contrast(%s, %s) = %.3f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}

func TestOverComposites(t *testing.T) {
	m := map[string]string{"bg": "#000000", "panel": "#000000", "sel": "#ffffff80"}
	got := surface(m, "sel/panel")
	if math.Abs(got.r-128.0/255) > 0.01 || got.a != 1 {
		t.Fatalf("white at 50%% over black = %+v", got)
	}
}

func validTokens() ThemeTokens {
	mode := func(bg, text, accent string) map[string]string {
		m := map[string]string{}
		for _, k := range ThemeKeys {
			m[k] = bg
		}
		for _, k := range []string{"text", "muted", "pos", "neg"} {
			m[k] = text
		}
		m["accent"] = accent
		m["accent-solid"] = accent
		m["on-accent"] = bg
		m["accent-soft"] = accent + "00"
		m["sel"] = accent + "00"
		return m
	}
	return ThemeTokens{"dark": mode("#000000", "#ffffff", "#ffff00"), "light": mode("#ffffff", "#000000", "#0000cc")}
}

func TestParseThemeTokens(t *testing.T) {
	good, _ := json.Marshal(validTokens())
	if _, err := ParseThemeTokens(good); err != nil {
		t.Fatalf("valid tokens refused: %v", err)
	}
	bad := validTokens()
	bad["dark"]["bg"] = "red; background: url(x)"
	bad["dark"]["unknown"] = "#000000"
	delete(bad["light"], "text")
	bad["sepia"] = map[string]string{}
	raw, _ := json.Marshal(bad)
	_, err := ParseThemeTokens(raw)
	var ve *platform.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %v, want a validation error", err)
	}
	var fields []string
	for _, f := range ve.Fields {
		fields = append(fields, f.Field)
	}
	slices.Sort(fields)
	want := []string{"tokens.dark.bg", "tokens.dark.unknown", "tokens.light.text", "tokens.sepia"}
	if diff := cmp.Diff(want, fields); diff != "" {
		t.Fatalf("fields (-want +got):\n%s", diff)
	}
	if _, err := ParseThemeTokens([]byte(`"#000000"`)); err == nil {
		t.Fatal("a string was accepted as tokens")
	}
}

func TestCheckContrast(t *testing.T) {
	tok := validTokens()
	if f := CheckContrast(tok); len(f) != 0 {
		t.Fatalf("black/white failed: %+v", f)
	}
	tok["light"]["muted"] = "#aaaaaa"
	f := CheckContrast(tok)
	if len(f) != 3 || f[0].Mode != "light" || f[0].FG != "muted" || f[0].Ratio >= MinContrast {
		t.Fatalf("failures = %+v, want muted on the three light surfaces", f)
	}
}

// TestThemeContractMatchesTheWeb keeps this package and web/src/styles/theme-contract.json equal,
// so the editor's report and the server's check never disagree.
func TestThemeContractMatchesTheWeb(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "styles", "theme-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var web struct {
		MinRatio float64        `json:"minRatio"`
		Keys     []string       `json:"keys"`
		Pairs    []ContrastPair `json:"pairs"`
	}
	if err := json.Unmarshal(raw, &web); err != nil {
		t.Fatal(err)
	}
	if web.MinRatio != MinContrast {
		t.Errorf("minRatio %v, want %v", web.MinRatio, MinContrast)
	}
	if diff := cmp.Diff(ThemeKeys, web.Keys); diff != "" {
		t.Errorf("keys (-go +web):\n%s", diff)
	}
	if diff := cmp.Diff(ContrastPairs, web.Pairs); diff != "" {
		t.Errorf("pairs (-go +web):\n%s", diff)
	}
}

func TestResolveLimits(t *testing.T) {
	b := Bounds{MaxUploadBytes: 20 << 20, AssistantPerHour: 30}
	if got := Resolve(Limits{}, Limits{}, b); got != (Effective{MaxUploadBytes: 20 << 20, AssistantPerHour: 30}) {
		t.Errorf("defaults = %+v", got)
	}
	inst := Limits{MaxUploadBytes: p(int64(5 << 20)), MaxAccounts: p(int64(3)), AssistantPerHour: p(int64(10))}
	user := Limits{MaxAccounts: p(int64(5)), MaxUploadBytes: p(int64(1 << 30)), AssistantPerHour: p(int64(100))}
	got := Resolve(user, inst, b)
	want := Effective{MaxUploadBytes: 20 << 20, MaxAccounts: 5, AssistantPerHour: 30}
	if got != want {
		t.Errorf("user over instance, capped by the environment = %+v, want %+v", got, want)
	}
	if got := Resolve(Limits{}, inst, b); got.MaxUploadBytes != 5<<20 || got.MaxAccounts != 3 || got.AssistantPerHour != 10 {
		t.Errorf("instance defaults = %+v", got)
	}
}

func TestParseLimits(t *testing.T) {
	l, err := ParseLimits([]byte(`{"maxAccounts": 2}`))
	if err != nil || l.MaxAccounts == nil || *l.MaxAccounts != 2 {
		t.Fatalf("got %+v, %v", l, err)
	}
	for _, bad := range []string{`{"maxAccounts": 0}`, `{"maxUploadBytes": 10}`, `{"other": 1}`, `[]`} {
		if _, err := ParseLimits([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	var le error = &LimitError{Limit: "maxAccounts", Value: 2}
	if !errors.Is(le, platform.ErrLimitReached) {
		t.Error("LimitError doesn't wrap ErrLimitReached")
	}
}

func TestParseSettings(t *testing.T) {
	s, err := ParseSettings([]byte(`{"defaults":{"themeId":"fjord","theme":"dark","ui":"1.1","noViz":true,"cards":["sum","flow"]},
		"features":{"overview":false},"limits":{"importsPerDay":20}}`))
	if err != nil {
		t.Fatal(err)
	}
	if e := s.Features.Effective(); e.Overview || !e.Assistant || !e.Friends || !e.SharedGroups {
		t.Errorf("features = %+v", e)
	}
	_, err = ParseSettings([]byte(`{"defaults":{"themeId":"Bad Id","theme":"sepia","ui":"2","cards":["sum","sum"]},"limits":{"maxGroups":0}}`))
	var ve *platform.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 5 {
		t.Fatalf("got %v, want 5 field errors", err)
	}
	if _, err := ParseSettings([]byte(`{"colours":{}}`)); err == nil || !strings.Contains(err.Error(), "settings") {
		t.Errorf("unknown key accepted: %v", err)
	}
	d := Defaults{ThemeID: p("gone")}.WithTheme(func(id string) bool { return slices.Contains(BuiltinThemes, id) })
	if d.ThemeID != nil {
		t.Error("a retired default theme was kept")
	}
}

// TestModelChoice: the administrator picks the assistant's model unless the settings say people do
// (a document saved before Phase 20's follow-ups has no assistant part); values are checked.
func TestModelChoice(t *testing.T) {
	var none *Assistant
	if none.PeopleChoose() || none.Connection() != "" {
		t.Error("without settings people choose")
	}
	for doc, people := range map[string]bool{
		`{"assistant":{"instructions":"Be brief."}}`:                          false,
		`{"assistant":{"modelChoice":"admin","defaultConnection":"homelab"}}`: false,
		`{"assistant":{"modelChoice":"people"}}`:                              true,
	} {
		s, err := ParseSettings([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		if s.Assistant.PeopleChoose() != people {
			t.Errorf("%s: people choose = %v", doc, !people)
		}
	}
	_, err := ParseSettings([]byte(`{"assistant":{"modelChoice":"anyone","defaultConnection":"Bad Name"}}`))
	var ve *platform.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 2 {
		t.Errorf("got %v, want 2 field errors", err)
	}
}
