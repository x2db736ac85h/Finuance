package ingest

import "strings"

// delimiterCandidates in tie-break order: the first one with the best score wins.
var delimiterCandidates = []rune{';', ',', '\t', '|'}

// DetectDelimiter picks the field delimiter (legacy `detectDelim`). For each candidate it counts the
// occurrences outside double quotes on each of the first 30 non-empty lines, takes the most common
// non-zero count (the "mode") and how many lines have it, and scores mf × 1000 + mode.
//
// Consistency beats raw frequency: a ';' file whose addresses contain a varying number of commas
// has the same ';' count on every line, so ';' wins even if commas are more numerous.
//
// Ties keep the earlier candidate, and a file with no delimiter at all gets ';' (every candidate
// scores 0 and ';' is first). The plan says "default ','", but the legacy code only uses ',' as a
// placeholder that the first candidate always replaces; the legacy behaviour is the spec.
func DetectDelimiter(text string) rune {
	var lines []string
	for rest := text; rest != "" && len(lines) < 30; {
		var l string
		l, rest = nextLine(rest)
		if hasNonSpace(l) {
			lines = append(lines, l)
		}
	}
	best, bestScore := ',', -1
	for _, d := range delimiterCandidates {
		freq := map[int]int{}
		for _, l := range lines {
			freq[countOutsideQuotes(l, d)]++
		}
		mode, mf := 0, 0
		for count, n := range freq {
			// The legacy code walks counts in ascending order and keeps the first maximum, so among
			// equally frequent counts the smallest one wins.
			if count > 0 && (n > mf || (n == mf && count < mode)) {
				mode, mf = count, n
			}
		}
		score := 0
		if mode > 0 {
			score = mf*1000 + mode
		}
		if score > bestScore {
			best, bestScore = d, score
		}
	}
	return best
}

// countOutsideQuotes counts d in a line, ignoring delimiters between double quotes. Every quote
// toggles the state, which is a deliberate simplification (a "" escape toggles twice).
func countOutsideQuotes(line string, d rune) int {
	n, inQuotes := 0, false
	for _, c := range line {
		switch {
		case c == '"':
			inQuotes = !inQuotes
		case c == d && !inQuotes:
			n++
		}
	}
	return n
}

// nextLine cuts the first line (ended by CRLF, LF or CR) off text.
func nextLine(text string) (line, rest string) {
	i := strings.IndexAny(text, "\r\n")
	if i < 0 {
		return text, ""
	}
	if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
		return text[:i], text[i+2:]
	}
	return text[:i], text[i+1:]
}

// splitLines splits on CRLF, LF or CR (legacy text.split(/\r\n|\n|\r/)).
func splitLines(text string) []string {
	return strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n")
}
