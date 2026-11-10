package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const testKey = "test-key-not-secret"

var fixturesDir = filepath.Join("..", "..", "..", "testdata", "fixtures", "synthetic")

// acct builds a 26-digit, realistic-looking account number at runtime, so the source file never
// contains one (tools/check-private.sh would flag it).
func acct(seed string) string {
	return seed + strings.Repeat("1234567890", 3)[:26-len(seed)]
}

// realistic inputs: invented, but shaped like real exports, with values that look personal.
func pekaoReal() string {
	a1, a2 := acct("32124023"), acct("91124017")
	return strings.Join([]string{
		"Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria",
		"01.03.2025;01.03.2025;KWIATOSZ MARKETPLACE;KWIATOSZ.PL 38 AVENUE JOHNA KENNEDYEGO,LUXEMBOURG;'" + a1 + ";;BLIK REF       86772307062;-127,13;PLN;'C992506002418351;PŁATNOŚĆ BLIK;Zakupy przez internet",
		"01.03.2025;28.02.2025;ZABCIA Z1001 K.1        GDANSK;;'" + a1 + ";;*********0229197;-44,98;PLN;'C992505922368031;TRANSAKCJA KARTĄ PŁATNICZĄ;Artykuły spożywcze",
		"15.04.2024;15.04.2024;BALTAZAR GĄBKA;UL. PASTELOWA 8 60-198 KRAKÓW;'" + a1 + ";'" + a2 + ";Zwrot za obiad Baltazar;1 200,00;PLN;'328244F604005273;PRZELEW MOBILE;Bez kategorii",
		"15.04.2024;14.04.2024;UBERTO   * EATS PENDING  AMSTERDAM;;'" + a1 + ";;*********0060980;-235,00;PLN;'C992410521886798;TRANSAKCJA KARTĄ PŁATNICZĄ;Restauracje i kawiarnie",
	}, "\n") + "\n"
}

func mbankReal() string {
	a := acct("53114020")
	row := func(d, desc, cat, amt string) string {
		return d + `;"` + desc + `";"Baltazar 0000 ... 0003";"` + cat + `";` + amt + " PLN;;"
	}
	return "\xef\xbb\xbf" + strings.Join([]string{
		"mBank S.A. Bankowość Detaliczna;", "\t\t\tSkrytka Pocztowa 2108;", "", "#Klient;", "BALTAZAR GĄBKA;", "", "#Za okres:;", "09.04.2025;09.04.2026;", "",
		"      #dla rachunków:;", "      Baltazar - " + a + ";", "      #Waluta;#Wpływy;#Wydatki;", "PLN;584,30;-5 521,28;", "      ",
		"#Data operacji;#Opis operacji;#Rachunek;#Kategoria;#Kwota;",
		row("2026-04-08", "BLIK WYPŁATA ATM WŁASNY  PLANETARIUM CASH          BLIK WYPŁATA ATM WŁASNY     ", "Wypłata gotówki", "-800,00"),
		row("2026-04-02", "FUNDACJA MAKATKA, DAROWIZNA NA CELE STATUTOWE        PRZELEW WEWNĘTRZNY WYCHODZĄCY      "+a+"  ", "Bez kategorii", "-1 200,00"),
		row("2026-04-01", "KONTRAHENT GRZEGORZ  UL.STRASZNA 3        60-123 KRAKÓW   PRZELEW WEWNĘTRZNY PRZYCHODZĄCY", "Wpływy - inne", "300,00"),
		"", "",
	}, "\r\n")
}

func pkoReal() []byte {
	text := strings.Join([]string{
		"Data operacji,Data waluty,Typ transakcji,Kwota,Waluta,Opis transakcji,,,,,",
		"2026-04-04,2026-04-03,Płatność kartą,-43.52,PLN,Tytuł: 000134679 00000000000000000000000,Lokalizacja: Adres: LIDLOWO Długa Miasto: Krakow Kraj: POLSKA,Data wykonania operacji: 2026-04-03 07:21,Kwota Cash Back:  0.00,Oryginalna kwota operacji: 43.52,Numer karty: 425125******7890",
		`2026-04-04,2026-04-04,Opłata za użytkowanie karty,-10,PLN,"Tytuł: OPŁATA MIESIĘCZNA ZA KARTĘ 425125******7890, 28.02-28.03",,,,,`,
		"",
		"2026-02-20,2026-02-19,Wypłata z bankomatu,-1600,PLN,Tytuł: PKO BP 10204027S1PO1965N6625C1252,Lokalizacja: Adres: Szeroka 67 Miasto: KRAKOW Kraj: POLSKA,Data wykonania operacji: 2026-02-19 17:57,Kwota Cash Back:  0.00,Oryginalna kwota operacji: 1600.00,Numer karty: 425125******7890",
		"",
	}, "\r\n")
	b, err := encode(text, source{Enc: "windows-1250"})
	if err != nil {
		panic(err)
	}
	return b
}

// sensitive lists tokens of the realistic inputs that must not survive anonymisation.
var sensitive = []string{
	"KWIATOSZ", "KENNEDYEGO", "ZABCIA", "GDANSK", "BALTAZAR", "GĄBKA", "PASTELOWA", "UBERTO", "AMSTERDAM",
	"86772307062", "0229197", "992506002418351", "604005273", "0060980", "Baltazar", "PLANETARIUM", "MAKATKA",
	"GRZEGORZ", "STRASZNA", "LIDLOWO", "Długa", "Szeroka", "425125", "000134679", "10204027", "1965",
	acct("32124023"), acct("91124017"), acct("53114020"),
}

func anon(t *testing.T, in []byte) ([]byte, report) {
	t.Helper()
	out, rep, err := newAnonymizer(testKey, -100).file(in, "auto", 0)
	if err != nil {
		t.Fatal(err)
	}
	return out, rep
}

func decoded(t *testing.T, b []byte) string {
	t.Helper()
	s, _, err := decode(b, "auto")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNoOriginalTokenSurvives(t *testing.T) {
	for name, in := range map[string][]byte{"pekao": []byte(pekaoReal()), "mbank": []byte(mbankReal()), "pko": pkoReal()} {
		t.Run(name, func(t *testing.T) {
			out, _ := anon(t, in)
			text := norm(decoded(t, out))
			for _, tok := range sensitive {
				if strings.Contains(text, norm(tok)) {
					t.Errorf("original token %q survived", tok)
				}
			}
			if bytes.Equal(out, in) {
				t.Error("output equals input")
			}
		})
	}
}

// signature captures layout: per record the field count, quoting and line terminator.
func signature(t *testing.T, b []byte) []string {
	t.Helper()
	text, src, err := decode(b, "auto")
	if err != nil {
		t.Fatal(err)
	}
	d := detectDelim(text)
	sig := []string{src.Enc, string(d), map[bool]string{true: "bom", false: "nobom"}[src.BOM]}
	for _, r := range parse(text, d) {
		var s strings.Builder
		for _, f := range r.Fields {
			s.WriteString(map[bool]string{true: "q", false: "u"}[f.Quoted])
		}
		sig = append(sig, s.String()+"|"+strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(r.EOL))
	}
	return sig
}

func TestStructurePreserved(t *testing.T) {
	inputs := map[string][]byte{"pekao": []byte(pekaoReal()), "mbank": []byte(mbankReal()), "pko": pkoReal()}
	err := filepath.WalkDir(fixturesDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".csv") {
			b, err := os.ReadFile(p)
			inputs[p] = b
			return err
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			out, _ := anon(t, in)
			if diff := cmp.Diff(signature(t, in), signature(t, out)); diff != "" {
				t.Errorf("layout changed (-in +out):\n%s", diff)
			}
		})
	}
}

func TestRoundTripIdentity(t *testing.T) {
	err := filepath.WalkDir(fixturesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".csv") {
			return err
		}
		in, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		text, src, err := decode(in, "auto")
		if err != nil {
			return err
		}
		out, err := encode(write(parse(text, detectDelim(text)), detectDelim(text)), src)
		if err != nil {
			return err
		}
		if !bytes.Equal(in, out) {
			t.Errorf("%s: parse/write is not byte-identical", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsistentAcrossFiles(t *testing.T) {
	a1 := acct("32124023")
	h := "Data;Nadawca / Odbiorca;Rachunek;Tytułem;Kwota;Kategoria\n"
	shared := "05.01.2025;KAWIARNIA MRUCZEK        GNIEZNO;'" + a1 + ";Kawa dla Zenobiusza;-12,00;Restauracje\n"
	f1 := h + "03.01.2025;APTEKA PIGULKA;'" + a1 + ";Leki;-25,99;Zdrowie\n" + shared
	f2 := h + shared + "07.01.2025;SKLEP ZENOBIUSZ;'" + a1 + ";Zakupy Zenobiusza;-99,00;Zakupy\n"
	o1, _ := anon(t, []byte(f1))
	o2, _ := anon(t, []byte(f2))
	l1, l2 := strings.Split(string(o1), "\n"), strings.Split(string(o2), "\n")
	if l1[2] != l2[1] {
		t.Errorf("shared row anonymised differently:\n%s\n%s", l1[2], l2[1])
	}
	acctOut := func(line string) string { return strings.Split(line, ";")[2] }
	if acctOut(l1[1]) != acctOut(l2[2]) {
		t.Error("the same account number maps to different values across files")
	}
	word := func(line string, i int) string { return strings.Fields(strings.Split(line, ";")[3])[i] }
	if word(l1[2], 2) != word(l2[2], 1) { // "Zenobiusza" in "Kawa dla Zenobiusza" and "Zakupy Zenobiusza"
		t.Errorf("the same word maps differently: %q vs %q", word(l1[2], 2), word(l2[2], 1))
	}
}

func TestDeterministicPerKey(t *testing.T) {
	in := []byte(pekaoReal())
	a, _ := anon(t, in)
	b, _ := anon(t, in)
	if !bytes.Equal(a, b) {
		t.Error("same key gave different output")
	}
	c, _, err := newAnonymizer("another-key", -100).file(in, "auto", 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Error("different keys gave identical output")
	}
}

func TestPekaoCells(t *testing.T) {
	out, rep := anon(t, []byte(pekaoReal()))
	lines := strings.Split(string(out), "\n")
	in := strings.Split(pekaoReal(), "\n")
	if lines[0] != in[0] {
		t.Errorf("header changed: %q", lines[0])
	}
	if rep.HeaderAt != 1 {
		t.Errorf("HeaderAt = %d, want 1", rep.HeaderAt)
	}
	f := func(i int) []string { return strings.Split(lines[i], ";") }
	row := f(1)
	// dates shifted by -100 days, same format
	if row[0] != "21.11.2024" || row[1] != "21.11.2024" {
		t.Errorf("dates = %q, %q", row[0], row[1])
	}
	// kept columns
	if row[8] != "PLN" || row[10] != "PŁATNOŚĆ BLIK" || row[11] != "Zakupy przez internet" {
		t.Errorf("kept columns changed: %q", row[8:])
	}
	// amount scaled, format kept
	if !regexp.MustCompile(`^-\d+,\d{2}$`).MatchString(row[7]) || row[7] == "-127,13" {
		t.Errorf("amount = %q", row[7])
	}
	if got := f(3)[7]; !regexp.MustCompile(`^\d{1,3}( \d{3})*,\d{2}$`).MatchString(got) {
		t.Errorf("grouped amount = %q", got)
	}
	// account numbers become the allowlisted all-zero form, keeping the ' marker
	if !regexp.MustCompile(`^'0{21}\d{5}$`).MatchString(row[4]) {
		t.Errorf("account = %q", row[4])
	}
	// "NAME        CITY" padding: the city starts in the same column
	in2, out2 := strings.Split(in[2], ";")[2], f(2)[2]
	if strings.LastIndex(in2, "  ")+2 != strings.LastIndex(out2, "  ")+2 {
		t.Errorf("padding not preserved: %q -> %q", in2, out2)
	}
	// a date range in free text is not mistaken for two amounts
	if out, _ := anon(t, []byte("Data;Opis;Kwota\n02.01.2025;OPŁATA ZA KARTĘ 28.02-28.03 kwota: 10.00;-10,00\n")); !strings.Contains(string(out), " 28.02-28.03 ") || strings.Contains(string(out), "kwota: 10.00") {
		t.Errorf("free-text numbers: %q", out)
	}
	// card mask keeps its asterisks and length
	if got := f(2)[6]; !regexp.MustCompile(`^\*{9}\d{7}$`).MatchString(got) || got == "*********0229197" {
		t.Errorf("card fragment = %q", got)
	}
}

func TestFindHeader(t *testing.T) {
	read := func(p string) []record {
		b, err := os.ReadFile(filepath.Join(fixturesDir, p))
		if err != nil {
			t.Fatal(err)
		}
		text, _, _ := decode(b, "auto")
		return parse(text, detectDelim(text))
	}
	for _, tc := range []struct {
		file string
		want int
	}{
		{"banks/pekao-like.csv", 0},
		{"banks/pko-bp-like.csv", 0}, // legacy picks a data row here
		{"banks/mbank-like.csv", 25}, // legacy never picks this header
		{"06-bank-preamble.csv", 5},
		{"35-padded-preamble.csv", 3},
		{"38-header-on-record-7.csv", 6},
		{"37-headerless.csv", -1},
		{"30-empty.csv", -1},
	} {
		if got := findHeader(read(tc.file)); got != tc.want {
			t.Errorf("%s: header = %d, want %d", tc.file, got, tc.want)
		}
	}
}

func TestParseAmount(t *testing.T) {
	a := &anonymizer{factor: 1.1}
	for _, tc := range []struct {
		in, want string
		any      bool
	}{
		{"-127,13", "-139,84", false},
		{"-1 234,56", "-1 358,02", false},
		{"1.234,56", "1.358,02", false},
		{"1,234.56", "1,358.02", false},
		{"-800,00 PLN", "-880,00 PLN", false},
		{"-€12,50", "-€13,75", false},
		{"€12", "€13", true},
		{"-10", "-11", true},
		{"+15,00", "+16,50", false},
		{"-2 500,00", "-2 750,00", false},
		{"0.00", "0.00", false},
		{"1600.00", "1760.00", false},
	} {
		got, ok := a.scaleAmount(tc.in, tc.any)
		if !ok || got != tc.want {
			t.Errorf("scaleAmount(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
	for _, s := range []string{"0012345", "1234567890123456", "abc", "12,5,0", "-10", "1 23,45", "2025-01-02"} {
		if got, ok := a.scaleAmount(s, false); ok {
			t.Errorf("scaleAmount(%q) = %q, want no amount", s, got)
		}
	}
}

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "export_123456789.csv")
	if err := os.WriteFile(in, []byte(pekaoReal()), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := run([]string{"-out", dir, in}, &log); err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("missing key: err = %v", err)
	}
	out := filepath.Join(dir, "out")
	if err := run([]string{"-key", testKey, "-shift-days", "-10", "-out", out, in}, &log); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 || strings.Contains(entries[0].Name(), "123456789") {
		t.Errorf("output files = %v (digit runs in names must be replaced)", entries)
	}
	if strings.Contains(log.String(), "BALTAZAR") || strings.Contains(log.String(), "-127,13") {
		t.Error("the log printed cell contents")
	}
	if err := run([]string{"-key", testKey, "-out", dir, filepath.Join(dir, "out", entries[0].Name())}, &log); err != nil {
		t.Fatal(err) // a different directory than the input: allowed
	}
}
