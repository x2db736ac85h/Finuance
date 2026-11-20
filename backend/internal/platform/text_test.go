package platform

import "testing"

func TestStripControls(t *testing.T) {
	for in, want := range map[string]string{
		"wyciąg 2026-11.csv":   "wyciąg 2026-11.csv",
		"ملف البنك.csv":        "ملف البنك.csv", // right-to-left script without overrides stays
		"📎 export.csv":         "📎 export.csv",
		"invoice\u202efdp.exe": "invoicefdp.exe",
		"a\u2066b\u2067c\u2068d\u2069e\u202af\u202bg\u202ch\u202di": "abcdefghi",
		"left\u200eright\u200fmark":                                 "leftrightmark",
		"line1\nline2\r\ttab":                                       "line1line2tab",
		"esc\x1b[31mred\x1b[0m":                                     "esc[31mred[0m",
		"nul\x00del\x7fc1\u0085\u009b":                              "nuldelc1",
		"":                                                          "",
	} {
		if got := StripControls(in); got != want {
			t.Errorf("StripControls(%q) = %q, want %q", in, got, want)
		}
	}
}
