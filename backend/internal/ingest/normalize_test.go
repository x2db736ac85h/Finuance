package ingest

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormCorpus(t *testing.T) {
	for _, c := range loadCorpus(t, "norm") {
		var in, want string
		_ = json.Unmarshal(c.In, &in)
		_ = json.Unmarshal(c.Out, &want)
		if got := Norm(in); got != want {
			t.Errorf("Norm(%q) = %q, legacy %q", in, got, want)
		}
	}
}

func TestPayeeNameCorpus(t *testing.T) {
	for _, c := range loadCorpus(t, "payeeName") {
		var want string
		_ = json.Unmarshal(c.Out, &want)
		in := jsString(c.In)
		if got := PayeeName(in); got != want {
			t.Errorf("PayeeName(%q) = %q, legacy %q", in, got, want)
		}
	}
}

func TestNormExamples(t *testing.T) {
	for in, want := range map[string]string{
		"PŁATNOŚĆ BLIK": "platnosc blik",
		"Zażółć Gęślą":  "zazolc gesla",
		"ŁÓDŹ":          "lodz",
		"ΟΔΟΣ":          "οδος", // final sigma: ς, then NFD keeps it
		"İstanbul":      "istanbul",
		"  Tab\tKept  ": "  tab\tkept  ", // Norm never trims or collapses
		"":              "",
	} {
		if got := Norm(in); got != want {
			t.Errorf("Norm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseAndHelpers(t *testing.T) {
	if got := Collapse("a   b\t\tc  "); got != "a b c " {
		t.Errorf("Collapse = %q", got)
	}
	if got := jsTrim("\uFEFF  x 　"); got != "x" {
		t.Errorf("jsTrim = %q", got)
	}
	for in, want := range map[string]int{"a  b": 2, "a b": 1, "  a": 2, "a  ": 2, "": 1, "a  b  c": 3} {
		if got := len(splitWideSpace(in)); got != want {
			t.Errorf("splitWideSpace(%q) has %d parts, want %d", in, got, want)
		}
	}
}

// The fast paths of Norm must equal the full NFD implementation on every input.
func TestNormFastPathsMatchFull(t *testing.T) {
	inputs := []string{"PŁATNOŚĆ BLIK", "Zażółć gęślą jaźń", "ŁÓDŹ", "Ἀθῆναι", "ǅemal", "한국어", "é", "ą́",
		"İstanbul", "ΟΔΟΣ ΣΑΣ", "ﬁnance", "Ⅻ ①", "café crème", "Øre Æble œuvre", "ẞ ß", "emoji 💸 ok", "x\u200by"}
	for _, c := range loadCorpus(t, "norm") {
		var s string
		_ = json.Unmarshal(c.In, &s)
		inputs = append(inputs, s)
	}
	for _, in := range inputs {
		if got, want := Norm(in), normFull(in); got != want {
			t.Errorf("Norm(%q) = %q, full NFD %q", in, got, want)
		}
	}
}

func TestTokenizeEdgeCases(t *testing.T) {
	for _, in := range []string{`"`, `""`, `"""`, `""x`, `"""x"`, `a;`, `;`, "a\r", "a\r\nb", `"a;b";c`, `x"y;z`, "\"open\nend", `"a"b"c";d`, ""} {
		if b, r := Tokenize(in, ';'), tokenizeRunes(in, ';', nil); !reflect.DeepEqual(b, r) {
			t.Errorf("Tokenize(%q) = %q, rune reference %q", in, b, r)
		}
	}
}
