package ingest

import "strings"

// The legacy code is JavaScript. Two of its primitives behave differently from the Go functions
// with the same name, and the difference matters for bank exports:
//
//   - String.prototype.trim() and the regex class \s match every Unicode whitespace character
//     plus U+FEFF (BOM). Go's strings.TrimSpace does not strip U+FEFF, and Go regexp's \s is
//     ASCII-only — so a non-breaking space (U+00A0) in "-2 500,00" would survive.
//   - These helpers implement the ECMAScript definition (WhiteSpace + LineTerminator), so the
//     port matches the oracle byte for byte.

// isJSSpace reports whether r is whitespace in ECMAScript (what trim() and \s match).
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim is String.prototype.trim().
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// jsCollapse is s.replace(/\s+/g, ' '): every run of JS whitespace becomes one space (no trim).
func jsCollapse(s string) string {
	if !needsCollapse(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if isJSSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// needsCollapse reports whether jsCollapse would change s: a whitespace character other than a
// single ASCII space, or two whitespace characters in a row.
func needsCollapse(s string) bool {
	prev := false
	for _, r := range s {
		sp := isJSSpace(r)
		if sp && (prev || r != ' ') {
			return true
		}
		prev = sp
	}
	return false
}

// splitWideSpace is s.split(/\s{2,}/): split on runs of two or more JS whitespace characters.
func splitWideSpace(s string) []string {
	var parts []string
	start, runStart, run := 0, 0, 0
	for i, r := range s {
		if isJSSpace(r) {
			if run == 0 {
				runStart = i
			}
			run++
			continue
		}
		if run >= 2 {
			parts = append(parts, s[start:runStart])
			start = i
		}
		run = 0
	}
	if run >= 2 {
		parts = append(parts, s[start:runStart])
		start = len(s)
	}
	return append(parts, s[start:])
}

// hasNonSpace reports whether s contains anything but JS whitespace (legacy: v.trim() !== ”).
func hasNonSpace(s string) bool {
	for _, r := range s {
		if !isJSSpace(r) {
			return true
		}
	}
	return false
}

// TrimJS is String.prototype.trim() for other packages that port legacy string handling (the
// filter language in internal/query).
func TrimJS(s string) string { return jsTrim(s) }

// FieldsJS splits s at runs of JS whitespace and drops empty parts: the legacy
// s.split(/\s+/).filter(Boolean), used for search tokens.
func FieldsJS(s string) []string { return strings.FieldsFunc(s, isJSSpace) }
