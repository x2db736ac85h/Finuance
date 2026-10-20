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
func tokenizeEach(text string, delim rune, each func([]string) bool) [][]string {
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
