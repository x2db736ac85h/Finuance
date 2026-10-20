package ingest

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// seedFixtures adds every CSV fixture to a fuzz corpus.
func seedFixtures(f *testing.F, add func([]byte)) {
	f.Helper()
	_ = filepath.WalkDir(fixturesDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".csv") {
			if b, err := os.ReadFile(p); err == nil {
				add(b)
			}
		}
		return nil
	})
}

// FuzzTokenize: never panics; without quotes, records are exactly the lines split on the
// delimiter.
func FuzzTokenize(f *testing.F) {
	seedFixtures(f, func(b []byte) { f.Add(string(b), int32(';')) })
	f.Fuzz(func(t *testing.T, text string, d int32) {
		delim := rune(d)
		if delim == '"' || delim == '\n' || delim == '\r' || !utf8.ValidRune(delim) || !utf8.ValidString(text) {
			return
		}
		recs := Tokenize(text, delim)
		if runes := tokenizeRunes(text, delim, nil); !reflect.DeepEqual(recs, runes) {
			t.Fatalf("byte and rune tokenizers differ for %q:\n%q\n%q", text, recs, runes)
		}
		if strings.ContainsRune(text, '"') || text == "" {
			return
		}
		lines := splitLines(text)
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(recs) != len(lines) {
			t.Fatalf("%d records for %d lines", len(recs), len(lines))
		}
		for i, l := range lines {
			if strings.Join(recs[i], string(delim)) != l {
				t.Fatalf("record %d %q does not rebuild line %q", i, recs[i], l)
			}
		}
	})
}

// FuzzParseNumber: ParseNumber and money.ParseMinor accept exactly the same strings.
func FuzzParseNumber(f *testing.F) {
	for _, s := range []string{"-1 234,56", "1.234,56", "12,00 PLN", "€12", "0229197", "1e3", "+,5", "1,2.3"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, ok := ParseNumber(s)
		_, err := money.ParseMinor(s, 2)
		if ok != (err == nil) && !strings.Contains(errString(err), "range") {
			t.Fatalf("ParseNumber ok=%v but ParseMinor err=%v for %q", ok, err, s)
		}
	})
}

// FuzzParseMinor: a parsed amount always re-parses from its canonical form.
func FuzzParseMinor(f *testing.F) {
	for _, s := range []string{"-127,13", "0,125", "999999999999999", "-0", ",5", "1.234.567"} {
		f.Add(s, uint8(2))
	}
	f.Fuzz(func(t *testing.T, s string, e uint8) {
		exp := int(e % 7)
		v, err := money.ParseMinor(s, exp)
		if err != nil {
			return
		}
		back, err := money.ParseCanonical(money.FormatMinor(v, exp), exp)
		if err != nil || back != v {
			t.Fatalf("%q → %d → %q → %d, %v", s, v, money.FormatMinor(v, exp), back, err)
		}
	})
}

// FuzzParseDate: an accepted date formats as YYYY-MM-DD and parses back to itself.
func FuzzParseDate(f *testing.F) {
	for _, s := range []string{"2025-01-02", "31.02.2025", "1.2.2025, 10:36", "2025-01-02x", "10/31/2025"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, ok := ParseDate(s)
		if !ok {
			return
		}
		back, ok := ParseDate(d.String())
		if !ok || back != d {
			t.Fatalf("%q → %v does not round-trip", s, d)
		}
		if _, _, ok := LooseDate(s); !ok {
			t.Fatalf("LooseDate rejects %q, which ParseDate accepts", s)
		}
	})
}

// FuzzParse: the whole pipeline never panics, every shaped row has the header's width, and staging
// with the proposed mapping never panics either.
func FuzzParse(f *testing.F) {
	seedFixtures(f, func(b []byte) { f.Add(b) })
	f.Fuzz(func(t *testing.T, raw []byte) {
		res, err := Parse(context.Background(), raw, Options{MaxRows: 5000})
		if err != nil {
			return
		}
		w := len(res.Table.Headers)
		for i, r := range res.Table.Rows {
			if len(r) != w {
				t.Fatalf("row %d has %d cells, header has %d", i, len(r), w)
			}
		}
		if len(res.Columns) != w {
			t.Fatalf("%d columns for %d headers", len(res.Columns), w)
		}
		rows, st, err := Stage(context.Background(), res.Table, MappingFromRoles(res.Roles), StageOptions{})
		if err != nil || st.Valid+st.Errors != len(rows) {
			t.Fatalf("stage: %v %+v", err, st)
		}
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
