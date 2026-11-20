package platform

import (
	"strings"
	"unicode"
)

// StripControls removes what has no business in a name that is shown to people: control
// characters (C0 and C1, among them newlines and escape sequences) and the characters that
// reorder text on screen (the bidirectional embeddings, overrides and isolates, U+202A–202E and
// U+2066–2069, and the two direction marks). With them a file can be called one thing and read as
// another (a name ending in "fdp.exe" after an override reads as ending in "exe.pdf") in the import list, the activity log and
// an archive's entries (IMP-7). Everything else, every script and emoji, stays.
func StripControls(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F:
			return -1
		}
		return r
	}, s)
}
