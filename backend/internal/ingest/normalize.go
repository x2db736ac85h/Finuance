package ingest

import (
	"strings"
	"unicode"
	"unicode/utf8"

	unorm "golang.org/x/text/unicode/norm"
)

// Norm is the one normalisation used for search, filters and fingerprints (legacy `norm`, PLAN §4.1):
//
//  1. lowercase (ECMAScript toLowerCase, see lowerJS);
//  2. Unicode NFD (decompose "ą" into "a" + U+0328);
//  3. remove combining marks in U+0300–U+036F — exactly that block, as the legacy regex does;
//  4. replace "ł" with "l" (ł has no decomposition, so step 3 cannot reach it).
//
// "PŁATNOŚĆ" → "platnosc": accent-insensitive search was chosen because users type Polish without
// diacritics.
func Norm(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s) // NFD and the mark/ł steps cannot change ASCII text
	}
	if out, ok := normFast(s); ok {
		return out
	}
	return normFull(s)
}

// normFast lowercases and decomposes character by character in one pass. That equals full NFD
// of lowerJS(s) as long as s has no combining marks of its own (their canonical reordering needs
// the whole string), no Hangul syllables (decomposed algorithmically) and none of the characters
// lowerJS special-cases (Σ, İ); for those it reports false and Norm uses normFull.
func normFast(s string) (string, bool) {
	var b strings.Builder
	b.Grow(len(s))
	var buf [utf8.UTFMax]byte
	for _, r := range s {
		switch {
		case r < 0x80:
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteByte(byte(r)) //nolint:gosec // r < 0x80 in this case
			continue
		case r == 'Σ' || r == 'İ':
			return "", false
		}
		r = unicode.ToLower(r)
		switch {
		case r < 0x80:
			b.WriteByte(byte(r)) //nolint:gosec // r < 0x80 in this case
			continue
		case r == 'ł':
			b.WriteByte('l')
			continue
		case r >= 0xAC00 && r <= 0xD7A3:
			return "", false
		}
		n := utf8.EncodeRune(buf[:], r)
		p := unorm.NFD.Properties(buf[:n])
		if p.CCC() != 0 {
			return "", false
		}
		d := p.Decomposition()
		if d == nil {
			if r < 0x0300 || r > 0x036F {
				b.WriteRune(r)
			}
			continue
		}
		for _, dr := range string(d) {
			switch {
			case dr >= 0x0300 && dr <= 0x036F:
			case dr == 'ł':
				b.WriteByte('l')
			default:
				b.WriteRune(dr)
			}
		}
	}
	return b.String(), true
}

// normFull is the reference implementation of Norm: full NFD of the lowercased string.
func normFull(s string) string {
	d := unorm.NFD.String(lowerJS(s))
	var b strings.Builder
	b.Grow(len(d))
	for _, r := range d {
		switch {
		case r >= 0x0300 && r <= 0x036F:
			continue
		case r == 'ł':
			b.WriteByte('l')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lowerJS is String.prototype.toLowerCase(). It differs from strings.ToLower in two places:
//   - "İ" (U+0130) lowercases to "i" + U+0307 (full case mapping); Go yields plain "i". After NFD and
//     mark removal both end as "i", so Norm needs no special case — it is kept here for clarity.
//   - "Σ" lowercases to final sigma "ς" at the end of a word (Final_Sigma context); Go always
//     yields "σ".
func lowerJS(s string) string {
	if !strings.ContainsAny(s, "Σİ") {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch r {
		case 'İ':
			b.WriteString("i̇")
		case 'Σ':
			if finalSigma(rs, i) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// finalSigma implements the Unicode Final_Sigma condition: preceded by a cased letter and not
// followed by one (case-ignorable characters in between are skipped).
func finalSigma(rs []rune, i int) bool {
	cased := func(r rune) bool { return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) }
	ignorable := func(r rune) bool { return unicode.Is(unicode.Mn, r) || r == '\'' || r == '.' || r == ':' }
	before := false
	for j := i - 1; j >= 0; j-- {
		if ignorable(rs[j]) {
			continue
		}
		before = cased(rs[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(rs); j++ {
		if ignorable(rs[j]) {
			continue
		}
		return !cased(rs[j])
	}
	return true
}

// Collapse replaces each run of whitespace with one space (legacy `.replace(/\s+/g, ' ')`). It does
// not trim. Used on normalised payee and description text in fingerprints and shadow columns.
func Collapse(s string) string { return jsCollapse(s) }

// PayeeName is the grouping key for payees and recurring payments (legacy `payeeName`, PLAN §4.1).
// Bank exports pad the merchant name with spaces before the city:
//
//	"ZABKA Z1001 K.1        GDANSK" → "ZABKA Z1001 K.1"
//
// Steps: trim; take the text before the first run of 2+ whitespace; collapse whitespace; an empty
// result becomes "(unknown)".
func PayeeName(s string) string {
	p := jsCollapse(splitWideSpace(jsTrim(s))[0])
	if p == "" {
		return "(unknown)"
	}
	return p
}
