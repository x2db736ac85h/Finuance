package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseLimitsAndCancellation(t *testing.T) {
	ctx := context.Background()
	many := strings.Repeat("a;b\n", 30)
	if _, err := Parse(ctx, []byte(many), Options{MaxRows: 10}); !errors.Is(err, ErrTooManyRows) {
		t.Errorf("rows: %v", err)
	}
	wide := strings.Repeat("x;", 300) + "\n1;2\n"
	if _, err := Parse(ctx, []byte(wide), Options{}); !errors.Is(err, ErrTooManyColumns) {
		t.Errorf("columns: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Parse(cancelled, []byte(strings.Repeat("d;1\n", ctxCheckEvery+5)), Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if _, err := Parse(ctx, []byte("a;b\n"), Options{Encoding: "ebcdic"}); !errors.Is(err, ErrUnknownEncoding) {
		t.Errorf("encoding: %v", err)
	}
	if _, err := Parse(ctx, []byte("a;b\n1;2\n"), Options{Shape: ShapeOptions{Header: HeaderRow, HeaderRow: 9}}); !errors.Is(err, ErrHeaderRowOutOfRange) {
		t.Errorf("header row: %v", err)
	}
	if _, err := Parse(ctx, []byte("\n\n  \n"), Options{Shape: ShapeOptions{Header: HeaderNone}}); !errors.Is(err, ErrEmptyFile) {
		t.Errorf("empty none: %v", err)
	}
}

func TestNonASCIIDelimiterUsesRuneTokenizer(t *testing.T) {
	recs := Tokenize("a§b\n\"c§d\"§e\n", '§')
	if len(recs) != 2 || recs[0][1] != "b" || recs[1][0] != "c§d" {
		t.Errorf("records = %q", recs)
	}
	res, err := Parse(context.Background(), []byte("Data§Kwota\n02.01.2025§-1,00\n"), Options{Delimiter: '§'})
	if err != nil || res.Table.Rows[0][1] != "-1,00" {
		t.Errorf("parse with § delimiter: %v %+v", err, res)
	}
}

func TestDecodeDetails(t *testing.T) {
	// WHATWG UTF-8 replacement: one U+FFFD per maximal subpart (E6 9C is a truncated 3-byte sequence).
	got, enc, _ := Decode([]byte{0xE6, 0x9C, 'A', 0xF0, 0x9F, 0x98, 'B', 0xFF, 'C', 0xED, 0xA0, 0x80}, EncodingUTF8)
	if enc != EncodingUTF8 || got != "�A�B�C���" {
		t.Errorf("lossy UTF-8 = %q", got)
	}
	// Unassigned Windows-1250 bytes decode to C1 controls, as in the browser.
	if got, _, _ := Decode([]byte{0x81, 0x83, 0xB9}, EncodingWindows1250); got != "\u0081\u0083ą" {
		t.Errorf("windows-1250 = %q", got)
	}
	if got, enc, _ := Decode([]byte{0xEF, 0xBB, 0xBF, 'x'}, EncodingAuto); got != "x" || enc.Label() != "UTF-8" {
		t.Errorf("BOM: %q %s", got, enc.Label())
	}
	if got, enc, _ := Decode([]byte{0xA6, 0xB6}, EncodingISO88592); got != "Śś" || enc.Label() != "ISO-8859-2" {
		t.Errorf("iso-8859-2: %q", got)
	}
	if got, _, _ := Decode([]byte{0x80, 0x81}, EncodingWindows1252); got != "€\u0081" || EncodingWindows1252.Label() != "Windows-1252" {
		t.Errorf("windows-1252: %q", got)
	}
	if Encoding("x").Label() != "x" || EncodingWindows1250.Label() != "Windows-1250" {
		t.Error("labels")
	}
}

func TestDetectDelimiterRules(t *testing.T) {
	for _, tc := range []struct {
		text string
		want rune
	}{
		{"", ';'}, // no delimiter at all: the first candidate wins with score 0
		{"a,b\nc,d\n", ','},
		{"a;b,c\nd;e\nf;g,h,i\n", ';'}, // consistent ; beats more numerous ,
		{"a\tb\nc\td\n", '\t'},
		{"\"a;b\",c\n\"d;e\",f\n", ','}, // delimiters inside quotes are not counted
		{"a;b\na,b\n", ';'},             // tie: the earlier candidate wins
	} {
		if got := DetectDelimiter(tc.text); got != tc.want {
			t.Errorf("DetectDelimiter(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestShapeRules(t *testing.T) {
	raw := Tokenize("Bank;;;\nData;Opis;Kwota;Kwota\n02.01.2025;'A;-1,00;x\n\n03.01.2025;B;-2,00;y\nSuma;3\n", ';')
	before := Tokenize("Bank;;;\nData;Opis;Kwota;Kwota\n02.01.2025;'A;-1,00;x\n\n03.01.2025;B;-2,00;y\nSuma;3\n", ';')
	tb, err := Shape(raw, ShapeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tb.HeaderAt != 2 || tb.Dropped != 1 || len(tb.Rows) != 2 || tb.Rows[0][1] != "A" || tb.Headers[3] != "Kwota (2)" {
		t.Errorf("shape = %+v", tb)
	}
	if diff := cmp.Diff(before, raw); diff != "" {
		t.Fatalf("Shape modified its input (-before +after):\n%s", diff)
	}
	kept, _ := Shape(raw, ShapeOptions{KeepShort: true, KeepQuote: true})
	if kept.Dropped != 0 || len(kept.Rows) != 3 || kept.Rows[0][1] != "'A" {
		t.Errorf("keep options = %+v", kept)
	}
	none, _ := Shape(raw, ShapeOptions{Header: HeaderNone, SkipRows: 3})
	if none.HeaderAt != 0 || none.Headers[0] != "Column 1" || none.Rows[0][0] != "02.01.2025" {
		t.Errorf("headerless = %+v", none)
	}
}

// TestFindHeaderBeforeData covers the Phase 9 header rule (ALLOWED_DIFFS.md row 4) on small tables;
// the oracle suite covers the bank fixtures.
func TestFindHeaderBeforeData(t *testing.T) {
	rec := func(s string) []string { return strings.Split(s, ";") }
	tests := []struct {
		name string
		rows []string
		want int // 0-based index into the non-blank records
	}{
		{"plain header", []string{"Data;Opis;Kwota", "02.01.2025;Sklep;-10,00"}, 0},
		{"unpadded preamble", []string{"Wyciąg", "Okres: 01.01.2025 - 31.01.2025", "Data;Opis;Kwota", "02.01.2025;Sklep;-10,00"}, 2},
		{"header fewer fields than data", []string{"#Data;#Opis;#Kwota;", "2025-01-02;Sklep;-10,00 PLN;;", "2025-01-03;Kino;-20,00 PLN;;"}, 0},
		{"blank-named header columns", []string{"Data;;Kwota;", "02.01.2025;x;-10,00;y", "03.01.2025;z;-20,00;w"}, 0},
		// preamble rows with numbers or dates but not both are neither header nor data
		{"summary table in the preamble", []string{"#Waluta;#Wpływy;#Wydatki", "PLN;584,30;-2 132,22", "#Data;#Opis;#Kwota", "2025-01-02;Sklep;-10,00"}, 2},
		// no text-only record before the first data record: the legacy rule decides (record 0)
		{"headerless", []string{"02.01.2025;Sklep;-10,00", "03.01.2025;Kino;-20,00"}, 0},
		{"single-name record before data", []string{"Operacje", "02.01.2025;Sklep;-10,00"}, 1},
		{"numbers in the record before data", []string{"Nr;Data;Kwota", "1;02.01.2025;-10,00", "2;03.01.2025;-20,00"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([][]string, len(tc.rows))
			for i, r := range tc.rows {
				rows[i] = rec(r)
			}
			if got := findHeader(rows); got != tc.want {
				t.Errorf("findHeader = %d, want %d", got, tc.want)
			}
		})
	}
}
