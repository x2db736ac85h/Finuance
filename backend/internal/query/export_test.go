package query

import "testing"

func TestGuardCell(t *testing.T) {
	for in, want := range map[string]string{
		"":            "",
		"-79,99":      "-79,99",
		"+12":         "+12",
		"-1 000,50":   "-1 000,50",
		"=SUM(A1)":    "'=SUM(A1)",
		"+48 600 000": "+48 600 000", // reads as a number
		"+48 abc":     "'+48 abc",
		"-abc":        "'-abc",
		"@cmd":        "'@cmd",
		"\tx":         "'\tx",
		"\rx":         "'\rx",
		"kino":        "kino",
		"a=b":         "a=b",
		"'=already":   "'=already",
		"=1+2":        "'=1+2",
		"-2025-03-01": "'-2025-03-01",
		"10.03.2025":  "10.03.2025",
	} {
		if got := GuardCell(in); got != want {
			t.Errorf("GuardCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVLine(t *testing.T) {
	for _, tc := range []struct {
		cells []string
		delim string
		want  string
	}{
		{[]string{"a", "b"}, ";", "a;b"},
		{[]string{"a;b", `say "hi"`, "x\ny", ""}, ";", `"a;b";"say ""hi""";"x` + "\n" + `y";`},
		{[]string{"a,b", "c;d"}, ",", `"a,b",c;d`},
		{[]string{"a\tb", "=x"}, "\t", "\"a\tb\"\t'=x"},
		{[]string{"=x;y"}, ";", `"'=x;y"`},
	} {
		if got := CSVLine(tc.cells, tc.delim); got != tc.want {
			t.Errorf("CSVLine(%q, %q) = %q, want %q", tc.cells, tc.delim, got, tc.want)
		}
	}
}
