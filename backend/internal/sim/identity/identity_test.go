package identity

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

var testSeed = []byte("0123456789abcdef0123456789abcdef")

// TestDerivation pins the derivation: the instance and the simulator are built apart (and may be
// different releases), so a change here would lock every animal out. The vectors were checked
// against an independent RFC 5869 implementation.
func TestDerivation(t *testing.T) {
	vectors := []struct{ slug, want string }{
		{"otter", "x95xpJTduKIL9zwq6dJAR7jx"},
		{"fox", "Jiq652KqE3iPn6yz0xJ5qZoq"},
		{"otter-2", "SmDdeEeSgp9u1XWzGR78_UCU"},
	}
	for _, v := range vectors {
		got, err := Password(testSeed, v.slug)
		if err != nil || got != v.want {
			t.Errorf("%s: %q, %v; want %q", v.slug, got, err, v.want)
		}
	}
}

func TestEveryPersonaDiffers(t *testing.T) {
	seen := map[string]string{}
	other := []byte(strings.Repeat("z", MinSeedBytes))
	for i := range Max {
		p := Nth(i)
		got, err := Password(testSeed, p.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) < 12 { // auth.MinPasswordChars
			t.Errorf("%s: %d characters", p.Slug, len(got))
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("%s and %s share one", p.Slug, prev)
		}
		seen[got] = p.Slug
		if again, _ := Password(other, p.Slug); again == got {
			t.Errorf("%s: another seed gives the same", p.Slug)
		}
	}
}

func TestShortSeed(t *testing.T) {
	if _, err := Password(testSeed[:MinSeedBytes-1], "fox"); !errors.Is(err, ErrShortSeed) {
		t.Errorf("a short seed: %v", err)
	}
	if _, err := ReadSeed(strings.NewReader("short\n")); !errors.Is(err, ErrShortSeed) {
		t.Errorf("a short seed on stdin: %v", err)
	}
	if _, err := ReadSeed(nil); err == nil {
		t.Error("no stdin")
	}
	got, err := ReadSeed(strings.NewReader(string(testSeed) + "\r\nthe rest is ignored"))
	if err != nil || !bytes.Equal(got, testSeed) {
		t.Errorf("ReadSeed: %q, %v", got, err)
	}
}

func TestNames(t *testing.T) {
	cases := map[int]Persona{
		0:  {Index: 0, Animal: "otter", Slug: "otter", Name: "Otter", Email: "otter@sim.invalid"},
		6:  {Index: 6, Animal: "fox", Slug: "fox", Name: "Fox", Email: "fox@sim.invalid"},
		10: {Index: 10, Animal: "otter", Slug: "otter-2", Name: "Otter 2", Email: "otter-2@sim.invalid"},
		49: {Index: 49, Animal: "sloth", Slug: "sloth-5", Name: "Sloth 5", Email: "sloth-5@sim.invalid"},
	}
	for i, want := range cases {
		if got := Nth(i); got != want {
			t.Errorf("Nth(%d) = %+v, want %+v", i, got, want)
		}
		if got, ok := BySlug(want.Slug); !ok || got != want {
			t.Errorf("BySlug(%q) = %+v, %v", want.Slug, got, ok)
		}
	}
	if _, ok := BySlug("unicorn"); ok {
		t.Error("an unknown slug was found")
	}
	if _, err := First(0); err == nil {
		t.Error("First(0)")
	}
	if _, err := First(Max + 1); err == nil {
		t.Error("First(Max+1)")
	}
	all, err := First(Max)
	if err != nil || len(all) != Max {
		t.Fatalf("First(Max): %d, %v", len(all), err)
	}
}
