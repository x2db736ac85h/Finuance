// Package instance holds the instance-wide settings an administrator manages (ADR 0025): the
// defaults new users' views start from, the feature switches, the limits, and the colour-theme
// contract (ADR 0026). It is pure: no database, no HTTP. The admin package stores the documents;
// auth and httpapi read them.
package instance

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Settings is the instance_settings document.
type Settings struct {
	Defaults Defaults `json:"defaults"`
	Features Features `json:"features"`
	Limits   Limits   `json:"limits"`
}

// Defaults are the preferences a user's view starts from. The browser merges them under the
// user's stored preferences, so only keys the user never saved take them; the names are the
// preference keys (web/src/stores/prefs.ts).
type Defaults struct {
	ThemeID        *string  `json:"themeId,omitempty"`
	Theme          *string  `json:"theme,omitempty"` // mode: system, dark or light
	UI             *string  `json:"ui,omitempty"`    // "auto" or a scale such as "1.1"
	NoViz          *bool    `json:"noViz,omitempty"` // insights pane hidden
	SidebarOpen    *bool    `json:"sbOpen,omitempty"`
	Cards          []string `json:"cards,omitempty"`
	CardOrder      []string `json:"cardOrder,omitempty"`
	ImportFastPath *bool    `json:"importFastPath,omitempty"`
}

// Features switch parts of the app off for everyone. A missing switch means on.
type Features struct {
	Overview     *bool `json:"overview,omitempty"`
	Assistant    *bool `json:"assistant,omitempty"`
	Friends      *bool `json:"friends,omitempty"`
	SharedGroups *bool `json:"sharedGroups,omitempty"`
}

// Effective features: nil means on.
func (f Features) Effective() EffectiveFeatures {
	on := func(p *bool) bool { return p == nil || *p }
	return EffectiveFeatures{Overview: on(f.Overview), Assistant: on(f.Assistant), Friends: on(f.Friends), SharedGroups: on(f.SharedGroups)}
}

// EffectiveFeatures are the switches as the API reports them.
type EffectiveFeatures struct {
	Overview, Assistant, Friends, SharedGroups bool
}

// Chart and overview cards a default may name (web/src/stores/prefs.ts CARD_KEYS).
var cardKeys = []string{"sum", "cat", "flow", "pace", "payee", "heat"}

var uiScales = []string{"auto", "0.9", "1", "1.1", "1.2", "1.3"}

// ParseSettings decodes and validates a stored or submitted document. Unknown keys are refused
// (the document is small and admin-written; a typo should not pass silently).
func ParseSettings(raw []byte) (Settings, error) {
	var s Settings
	if len(raw) == 0 {
		return s, nil
	}
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		ve := &platform.ValidationError{}
		ve.Add("settings", fmt.Sprintf("not a settings document: %v", err))
		return Settings{}, ve
	}
	return s, s.Validate()
}

// Validate checks every field and reports all problems at once.
func (s Settings) Validate() error {
	ve := &platform.ValidationError{}
	d := s.Defaults
	if d.ThemeID != nil && !validSlug(*d.ThemeID) {
		ve.Add("defaults.themeId", "must be a theme id")
	}
	if d.Theme != nil && !slices.Contains([]string{"system", "dark", "light"}, *d.Theme) {
		ve.Add("defaults.theme", "must be system, dark or light")
	}
	if d.UI != nil && !slices.Contains(uiScales, *d.UI) {
		ve.Add("defaults.ui", "must be auto or one of the interface sizes")
	}
	for _, list := range []struct {
		field string
		keys  []string
	}{{"defaults.cards", d.Cards}, {"defaults.cardOrder", d.CardOrder}} {
		seen := map[string]bool{}
		for _, k := range list.keys {
			if !slices.Contains(cardKeys, k) || seen[k] {
				ve.Add(list.field, "must list chart cards, each once")
				break
			}
			seen[k] = true
		}
	}
	s.Limits.validate("limits", ve)
	return ve.Err()
}

// WithTheme returns the defaults with the theme id dropped when it no longer names an offered
// theme (built-in or approved), so a retired theme never stays the default.
func (d Defaults) WithTheme(offered func(id string) bool) Defaults {
	if d.ThemeID != nil && !offered(*d.ThemeID) {
		d.ThemeID = nil
	}
	return d
}

func validSlug(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for i, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && (r != '-' || i == 0) {
			return false
		}
	}
	return true
}
