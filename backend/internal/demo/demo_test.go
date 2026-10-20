package demo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestByteIdenticalToLegacy(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "demo", "demo-legacy.csv"))
	if err != nil {
		t.Fatal(err)
	}
	got := CSV(LegacyStart, LegacyEnd)
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) && i < len(wl); i++ {
			if gl[i] != wl[i] {
				t.Fatalf("first difference at line %d:\n ours   %s\n legacy %s", i+1, gl[i], wl[i])
			}
		}
		t.Fatalf("lengths differ: %d vs %d lines", len(gl), len(wl))
	}
}

func TestToFixed2(t *testing.T) {
	for in, want := range map[float64]string{0.125: "0.13", -0.125: "-0.13", 1.005: "1.00", 2.675: "2.67", -79.99: "-79.99",
		9400: "9400.00", 0.994: "0.99", 0.995: "0.99", 9.995: "9.99", 99.999: "100.00", -0.001: "-0.00", 0: "0.00"} {
		if got := toFixed2(in); got != want {
			t.Errorf("toFixed2(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestLongerRanges(t *testing.T) {
	s := CSV(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC))
	if n := strings.Count(s, "\n"); n < 7000 {
		t.Errorf("only %d rows for 10 years", n)
	}
}
