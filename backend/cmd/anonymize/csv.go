package main

import "strings"

// field is one CSV field with enough detail to write it back byte for byte.
// The grammar mirrors the legacy parseCSV: a quote opens only at the start of a field,
// "" inside quotes is a literal quote, and text after the closing quote continues the field (Tail).
type field struct {
	Value        string // unescaped content (inside the quotes when Quoted)
	Quoted       bool
	Unterminated bool   // the opening quote was never closed (field runs to EOF)
	Tail         string // literal text after the closing quote, before the delimiter
}

// record is one line of fields plus the exact line terminator that ended it ("" at EOF).
type record struct {
	Fields []field
	EOL    string
}

func (r record) blank() bool {
	for _, f := range r.Fields {
		if strings.TrimSpace(f.Value+f.Tail) != "" {
			return false
		}
	}
	return true
}

func (r record) nonEmpty() int {
	n := 0
	for _, f := range r.Fields {
		if strings.TrimSpace(f.Value+f.Tail) != "" {
			n++
		}
	}
	return n
}

// parse splits text into records. Unlike the legacy tokenizer it keeps blank lines and line
// terminators, so write(parse(t)) == t for every input.
func parse(text string, delim rune) []record {
	var (
		recs []record
		cur  record
		f    field
		b    strings.Builder // current Value or Tail
		inQ  bool
		tail bool // writing into f.Tail (after a closing quote)
	)
	flushField := func() {
		if tail {
			f.Tail = b.String()
		} else {
			f.Value = b.String()
		}
		cur.Fields = append(cur.Fields, f)
		f, tail = field{}, false
		b.Reset()
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if inQ {
			if c == '"' {
				if i+1 < len(rs) && rs[i+1] == '"' {
					b.WriteRune('"')
					i++
					continue
				}
				inQ = false
				f.Value = b.String()
				b.Reset()
				tail = true
				continue
			}
			b.WriteRune(c)
			continue
		}
		switch {
		case c == '"' && !f.Quoted && !tail && b.Len() == 0:
			inQ, f.Quoted = true, true
		case c == delim:
			flushField()
		case c == '\n' || c == '\r':
			eol := string(c)
			if c == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				eol = "\r\n"
				i++
			}
			flushField()
			cur.EOL = eol
			recs = append(recs, cur)
			cur = record{}
		default:
			b.WriteRune(c)
		}
	}
	if inQ {
		f.Unterminated = true
	}
	if b.Len() > 0 || len(cur.Fields) > 0 || f.Quoted {
		flushField()
		recs = append(recs, cur)
	}
	return recs
}

func write(recs []record, delim rune) string {
	var b strings.Builder
	for _, r := range recs {
		for i, f := range r.Fields {
			if i > 0 {
				b.WriteRune(delim)
			}
			if f.Quoted {
				b.WriteByte('"')
				b.WriteString(strings.ReplaceAll(f.Value, `"`, `""`))
				if !f.Unterminated {
					b.WriteByte('"')
				}
				b.WriteString(f.Tail)
			} else {
				b.WriteString(f.Value)
			}
		}
		b.WriteString(r.EOL)
	}
	return b.String()
}

// detectDelim ports the legacy rule: for each candidate, count delimiters outside quotes on the
// first 30 non-empty lines; score = frequency of the modal count × 1000 + modal count.
func detectDelim(text string) rune {
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	var sample []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			sample = append(sample, l)
			if len(sample) == 30 {
				break
			}
		}
	}
	best, bestScore := ',', -1
	for _, d := range []rune{';', ',', '\t', '|'} {
		freq := map[int]int{}
		for _, l := range sample {
			n, q := 0, false
			for _, c := range l {
				if c == '"' {
					q = !q
				} else if c == d && !q {
					n++
				}
			}
			freq[n]++
		}
		mode, mf := 0, 0
		for k, v := range freq {
			if k > 0 && (v > mf || (v == mf && k < mode)) {
				mf, mode = v, k
			}
		}
		score := 0
		if mode > 0 {
			score = mf*1000 + mode
		}
		if score > bestScore {
			bestScore, best = score, d
		}
	}
	return best
}
