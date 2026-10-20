package ingest

import "strings"

// Tokenize splits text into records of fields (legacy `parseCSV`). It is a small state machine,
// deliberately not encoding/csv, whose rules differ on exactly the edge cases bank exports produce:
//
//   - a quote opens a quoted field only at the very start of a field; elsewhere it is literal
//     (`Sklep 5" ekran` stays as is);
//   - inside quotes, "" is a literal quote and delimiters and line breaks are data;
//   - after the closing quote, any further characters are appended to the same field;
//   - CRLF, LF and a lone CR each end a record;
//   - an unterminated quote swallows the rest of the file into one field;
//   - a trailing record without a final line break is kept.
//
// Blank lines become records with one empty field; Shape drops them later.
func Tokenize(text string, delim rune) [][]string {
	return tokenizeEach(text, delim, nil)
}

// tokenizeEach is Tokenize with a callback after each completed record; returning false stops.
// ASCII delimiters (every candidate and every manual option) take the byte-level path; it is
// equivalent because UTF-8 never uses bytes below 0x80 inside multi-byte characters.
func tokenizeEach(text string, delim rune, each func([]string) bool) [][]string {
	if delim < 0x80 && delim != '"' && delim != '\n' && delim != '\r' {
		return tokenizeBytes(text, byte(delim), each) //nolint:gosec // delim < 0x80 checked above
	}
	return tokenizeRunes(text, delim, each)
}

// tokenizeBytes is the state machine of tokenizeRunes over bytes. A field that does not start
// with a quote is a plain substring of text (no copy); quoted fields are assembled in a buffer.
func tokenizeBytes(text string, d byte, each func([]string) bool) [][]string {
	var (
		// Pre-size from the line count and the previous record's width: growing these slices was
		// the main cost of tokenizing a large file.
		records = make([][]string, 0, strings.Count(text, "\n")+1)
		width   = 8
		record  []string
		buf     []byte
	)
	n := len(text)
	i := 0
	for i < n {
		// One field starts at i.
		var field string
		if text[i] != '"' {
			j := i
			for j < n && text[j] != d && text[j] != '\n' && text[j] != '\r' {
				j++
			}
			field, i = text[i:j], j
		} else {
			// Quoted: "" is a literal quote; after the closing quote the field continues unquoted,
			// and a quote right after an empty quoted section ("" + ") opens a new one.
			buf = buf[:0]
			quoted := true
			i++
			for i < n {
				c := text[i]
				if quoted {
					if c == '"' {
						if i+1 < n && text[i+1] == '"' {
							buf = append(buf, '"')
							i += 2
							continue
						}
						quoted = false
						i++
						continue
					}
					k := strings.IndexByte(text[i:], '"')
					if k < 0 {
						k = n - i
					}
					buf = append(buf, text[i:i+k]...)
					i += k
					continue
				}
				if c == '"' && len(buf) == 0 {
					quoted = true
					i++
					continue
				}
				if c == d || c == '\n' || c == '\r' {
					break
				}
				buf = append(buf, c)
				i++
			}
			field = string(buf)
		}
		if record == nil {
			record = make([]string, 0, width)
		}
		record = append(record, field)
		if i >= n {
			// The legacy keeps a trailing record only if it has content: a file that is just `"`
			// or `""` yields no record at all.
			if field == "" && len(record) == 1 {
				record = nil
			}
			break
		}
		c := text[i]
		i++
		if c == d {
			if i == n { // "a;" at EOF: the trailing empty field
				record = append(record, "")
			}
			continue
		}
		// a line break ends the record
		if c == '\r' && i < n && text[i] == '\n' {
			i++
		}
		records = append(records, record)
		if each != nil && !each(record) {
			return records
		}
		width = max(width, len(record))
		record = nil
	}
	if len(record) > 0 {
		records = append(records, record)
		if each != nil {
			each(record)
		}
	}
	if len(records) == 0 {
		return nil // like the rune version: no records is nil, not an empty slice
	}
	return records
}

// tokenizeRunes is the direct port of the legacy parseCSV, used for non-ASCII delimiters.
func tokenizeRunes(text string, delim rune, each func([]string) bool) [][]string {
	var (
		records [][]string
		record  []string
		field   strings.Builder
		quoted  bool
	)
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if quoted {
			if c == '"' {
				if i+1 < len(rs) && rs[i+1] == '"' {
					field.WriteRune('"')
					i++
					continue
				}
				quoted = false
				continue
			}
			field.WriteRune(c)
			continue
		}
		switch {
		case c == '"' && field.Len() == 0:
			quoted = true
		case c == delim:
			record = append(record, field.String())
			field.Reset()
		case c == '\n' || c == '\r':
			if c == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			record = append(record, field.String())
			records = append(records, record)
			if each != nil && !each(record) {
				return records
			}
			record, quoted = nil, false
			field.Reset()
		default:
			field.WriteRune(c)
		}
	}
	if field.Len() > 0 || len(record) > 0 {
		record = append(record, field.String())
		records = append(records, record)
		if each != nil {
			each(record)
		}
	}
	return records
}
