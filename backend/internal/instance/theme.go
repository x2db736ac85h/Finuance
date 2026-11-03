package instance

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The colour-theme contract (ADR 0026). web/src/styles/theme-contract.json holds the same lists;
// TestThemeContractMatchesTheWeb keeps them equal.
var (
	// ThemeKeys are the tokens a theme sets (CSS custom properties without the leading --).
	ThemeKeys = []string{"bg", "panel", "elev", "input", "border", "text", "muted", "accent", "accent-solid",
		"on-accent", "accent-soft", "sel", "pos", "neg", "logo-a", "logo-b"}
	// ContrastPairs are the text colours and the surfaces they are drawn on. A surface "a/b" is
	// the translucent token a laid over b.
	ContrastPairs = []ContrastPair{
		{FG: "text", BG: []string{"bg", "panel", "elev", "input", "sel/panel"}},
		{FG: "muted", BG: []string{"bg", "panel", "elev"}},
		{FG: "accent", BG: []string{"bg", "panel", "elev", "sel/panel", "accent-soft/panel"}},
		{FG: "on-accent", BG: []string{"accent-solid"}},
		{FG: "pos", BG: []string{"bg", "panel"}},
		{FG: "neg", BG: []string{"bg", "panel"}},
	}
	// MinContrast is WCAG 2.1 AA for normal text.
	MinContrast = 4.5
	// ThemeModes are the variants every theme has.
	ThemeModes = []string{"dark", "light"}
	// BuiltinThemes can't be edited or deleted; administrators duplicate them.
	BuiltinThemes = []string{"indigo", "fjord", "grove", "ember"}
)

// ContrastPair is one foreground token and the surfaces it must be readable on.
type ContrastPair struct {
	FG string   `json:"fg"`
	BG []string `json:"bg"`
}

// ThemeTokens are a theme's colours: mode → token → "#rrggbb" or "#rrggbbaa".
type ThemeTokens map[string]map[string]string

// ContrastFailure is one pair below MinContrast.
type ContrastFailure struct {
	Mode  string  `json:"mode"`
	FG    string  `json:"fg"`
	BG    string  `json:"bg"`
	Ratio float64 `json:"ratio"`
}

// ContrastError lists every failing pair. It wraps platform.ErrContrast (422).
type ContrastError struct{ Failures []ContrastFailure }

func (e *ContrastError) Error() string {
	return fmt.Sprintf("%d colour pairs are below %.1f:1", len(e.Failures), MinContrast)
}

// Unwrap maps the error to platform.ErrContrast.
func (e *ContrastError) Unwrap() error { return platform.ErrContrast }

var hexColour = regexp.MustCompile(`^#[0-9a-f]{6}([0-9a-f]{2})?$`)

// ParseThemeTokens decodes and validates submitted tokens: both modes, exactly the known keys,
// lowercase hex colours only (never other CSS, so a theme can't inject anything into a page).
func ParseThemeTokens(raw []byte) (ThemeTokens, error) {
	var t ThemeTokens
	ve := &platform.ValidationError{}
	if err := json.Unmarshal(raw, &t); err != nil {
		ve.Add("tokens", "must be an object of modes")
		return nil, ve
	}
	for mode := range t {
		if !slices.Contains(ThemeModes, mode) {
			ve.Add("tokens."+mode, "unknown mode")
		}
	}
	for _, mode := range ThemeModes {
		m, ok := t[mode]
		if !ok {
			ve.Add("tokens."+mode, "is required")
			continue
		}
		for k := range m {
			if !slices.Contains(ThemeKeys, k) {
				ve.Add("tokens."+mode+"."+k, "unknown token")
			}
		}
		for _, k := range ThemeKeys {
			v, ok := m[k]
			switch {
			case !ok:
				ve.Add("tokens."+mode+"."+k, "is required")
			case !hexColour.MatchString(v):
				ve.Add("tokens."+mode+"."+k, "must be a colour such as #1e1e1e or #8b7cf62b")
			}
		}
	}
	if err := ve.Err(); err != nil {
		return nil, err
	}
	return t, nil
}

// CheckContrast returns every pair below MinContrast (nil when all pass). The tokens must be
// valid (ParseThemeTokens).
func CheckContrast(t ThemeTokens) []ContrastFailure {
	var out []ContrastFailure
	for _, mode := range ThemeModes {
		m := t[mode]
		for _, p := range ContrastPairs {
			fg := surface(m, p.FG)
			for _, bg := range p.BG {
				r := Contrast(fg, surface(m, bg))
				if r < MinContrast {
					out = append(out, ContrastFailure{Mode: mode, FG: p.FG, BG: bg, Ratio: math.Floor(r*100) / 100})
				}
			}
		}
	}
	return out
}

// rgba is a colour with straight alpha, components 0–1.
type rgba struct{ r, g, b, a float64 }

func parseHex(s string) rgba {
	v := func(i int) float64 {
		n, _ := strconv.ParseUint(s[i:i+2], 16, 8)
		return float64(n) / 255
	}
	c := rgba{v(1), v(3), v(5), 1}
	if len(s) == 9 {
		c.a = v(7)
	}
	return c
}

// surface resolves "a" or "a/b" (a over b, b itself opaque or over the theme's bg).
func surface(m map[string]string, name string) rgba {
	parts := strings.Split(name, "/")
	c := parseHex(m[parts[len(parts)-1]])
	if c.a < 1 { // a translucent base sits on the page background
		c = over(c, parseHex(m["bg"]))
	}
	for i := len(parts) - 2; i >= 0; i-- {
		c = over(parseHex(m[parts[i]]), c)
	}
	return c
}

// over composites a translucent colour on an opaque one (sRGB, as browsers do).
func over(top, bottom rgba) rgba {
	mix := func(t, b float64) float64 { return t*top.a + b*(1-top.a) }
	return rgba{mix(top.r, bottom.r), mix(top.g, bottom.g), mix(top.b, bottom.b), 1}
}

// Contrast is the WCAG 2.1 contrast ratio of two opaque colours (1–21).
func Contrast(a, b rgba) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ContrastHex is Contrast for two opaque hex colours.
func ContrastHex(a, b string) float64 { return Contrast(parseHex(a), parseHex(b)) }

func luminance(c rgba) float64 {
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}
