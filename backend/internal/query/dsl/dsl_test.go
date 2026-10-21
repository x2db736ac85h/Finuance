package dsl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Cond
		ok   bool
	}{
		{"", Cond{}, false},
		{"   ", Cond{}, false},
		{"\u00a0\ufeff", Cond{}, false},
		{"kino", Cond{Body: "kino"}, true},
		{"  >  100  ", Cond{Body: ">  100", Kind: Op, Op: ">", V: "100"}, true},
		{">=0", Cond{Body: ">=0", Kind: Op, Op: ">=", V: "0"}, true},
		{"=", Cond{Body: "=", Kind: Op, Op: "="}, true},
		{"!=", Cond{Neg: true, Body: "=", Kind: Op, Op: "="}, true},
		{"!", Cond{Neg: true}, true},
		{"! kino", Cond{Neg: true, Body: "kino"}, true},
		{"!!x", Cond{Neg: true, Body: "!x"}, true},
		{"-100..-50", Cond{Body: "-100..-50", Kind: Range, A: "-100", B: "-50"}, true},
		{"-50 .. -100", Cond{Body: "-50 .. -100", Kind: Range, A: "-50", B: "-100"}, true},
		{"1...2", Cond{Body: "1...2", Kind: Range, A: "1", B: ".2"}, true}, // the first ".." separates
		{"..", Cond{Body: ".."}, true},
		{"1..", Cond{Body: "1.."}, true},
		{"..1", Cond{Body: "..1"}, true},
		// Both shapes: the range wins for numbers and dates; the operator stays for the text fallback.
		{"=a..b", Cond{Body: "=a..b", Kind: Range, A: "=a", B: "b", Op: "=", V: "a..b"}, true},
		{">1..5", Cond{Body: ">1..5", Kind: Range, A: ">1", B: "5", Op: ">", V: "1..5"}, true},
		// "." in the legacy regex does not match line terminators.
		{">a\nb", Cond{Body: ">a\nb"}, true},
		{"a\n..b", Cond{Body: "a\n..b", Kind: Range, A: "a", B: "b"}, true}, // \s* absorbs the newline
		{"a..\nb", Cond{Body: "a..\nb", Kind: Range, A: "a", B: "b"}, true}, // \s* may span it
	} {
		got, ok := Parse(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestOracleConditionsParse checks the shape of every condition in the legacy filter oracle:
// a condition the legacy compiled parses, one it ignored does not.
func TestOracleConditionsParse(t *testing.T) {
	var oracle struct {
		Cases []struct {
			Cond     string `json:"cond"`
			Compiled bool   `json:"compiled"`
		} `json:"cases"`
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "oracle", "filters", "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) < 80 {
		t.Fatalf("only %d oracle cases", len(oracle.Cases))
	}
	for _, c := range oracle.Cases {
		got, ok := Parse(c.Cond)
		if ok != c.Compiled {
			t.Errorf("Parse(%q) ok = %v, legacy compiled = %v", c.Cond, ok, c.Compiled)
		}
		if again, _ := Parse(got.String()); ok && again != got {
			t.Errorf("round trip of %q: %+v → %q → %+v", c.Cond, got, got.String(), again)
		}
	}
}

func FuzzParseCond(f *testing.F) {
	for _, s := range []string{"", "!", "!=", ">=1", "-1..2", "a .. b", "=x..y", "!!a", " \u00a0>\u3000 5 ", "a\n..b", "1...2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, ok := Parse(s)
		if !ok {
			if c != (Cond{}) {
				t.Fatalf("no condition but %+v", c)
			}
			return
		}
		again, ok2 := Parse(c.String())
		if !ok2 && !c.Neg { // "!" alone prints as "!", which parses
			t.Fatalf("%q → %q does not parse", s, c.String())
		}
		if diff := cmp.Diff(c, again); ok2 && diff != "" {
			t.Fatalf("round trip %q → %q: %s", s, c.String(), diff)
		}
		if c.Kind == Range && (c.A == "" || c.B == "") {
			t.Fatalf("empty range operand in %+v", c)
		}
	})
}
