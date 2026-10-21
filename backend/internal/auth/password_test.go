package auth

import (
	"strings"
	"testing"
)

// testParams keeps the unit tests fast; the cost parameters don't change the logic.
var testParams = Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("correct horse battery staple", testParams)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Errorf("PHC string = %s", h)
	}
	if ok, err := Verify("correct horse battery staple", h); !ok || err != nil {
		t.Errorf("Verify(right) = %v, %v", ok, err)
	}
	if ok, err := Verify("correct horse battery stapl", h); ok || err != nil {
		t.Errorf("Verify(wrong) = %v, %v", ok, err)
	}
	h2, _ := Hash("correct horse battery staple", testParams)
	if h == h2 {
		t.Error("two hashes of the same password are equal: the salt isn't random")
	}
	// Unicode passwords are hashed as their UTF-8 bytes.
	u, _ := Hash("zażółć gęślą jaźń", testParams)
	if ok, _ := Verify("zażółć gęślą jaźń", u); !ok {
		t.Error("unicode password")
	}
}

func TestPasswordLengthCap(t *testing.T) {
	long := strings.Repeat("x", MaxPasswordBytes+1)
	if _, err := Hash(long, testParams); err == nil {
		t.Error("Hash accepted an over-long password")
	}
	h, _ := Hash(strings.Repeat("x", MaxPasswordBytes), testParams)
	if ok, _ := Verify(strings.Repeat("x", MaxPasswordBytes), h); !ok {
		t.Error("password of exactly the cap")
	}
	if ok, err := Verify(long, h); ok || err != nil {
		t.Errorf("over-long password: %v, %v", ok, err)
	}
}

func TestMalformedHashes(t *testing.T) {
	good, _ := Hash("pw", testParams)
	parts := strings.Split(good, "$")
	for _, bad := range []string{
		"",
		"plain",
		"$argon2i$v=19$m=8192,t=1,p=1$" + parts[4] + "$" + parts[5],
		"$argon2id$v=18$m=8192,t=1,p=1$" + parts[4] + "$" + parts[5],
		"$argon2id$v=19$m=0,t=1,p=1$" + parts[4] + "$" + parts[5],
		"$argon2id$v=19$m=8192,t=1$" + parts[4] + "$" + parts[5],
		"$argon2id$v=19$m=8192,t=1,p=300$" + parts[4] + "$" + parts[5],
		"$argon2id$v=19$m=8192,t=1,p=1,x=2$" + parts[4] + "$" + parts[5],
		"$argon2id$v=19$m=8192,t=1,p=1$!!$" + parts[5],
		"$argon2id$v=19$m=8192,t=1,p=1$" + parts[4] + "$c2hvcnQ",
		good + "$extra",
	} {
		if ok, err := Verify("pw", bad); ok || err == nil {
			t.Errorf("Verify(%q) = %v, %v; want ErrMalformedHash", bad, ok, err)
		}
		if !NeedsRehash(bad, testParams) {
			t.Errorf("NeedsRehash(%q) = false", bad)
		}
	}
}

func TestNeedsRehash(t *testing.T) {
	h, _ := Hash("pw", testParams)
	if NeedsRehash(h, testParams) {
		t.Error("same parameters need a rehash")
	}
	for _, p := range []Params{{16 * 1024, 1, 1}, {8 * 1024, 2, 1}, {8 * 1024, 1, 2}} {
		if !NeedsRehash(h, p) {
			t.Errorf("parameters %+v don't trigger a rehash", p)
		}
	}
}

// BenchmarkHash measures one hash per parameter set (ADR 0010):
//
//	go test -run '^$' -bench Hash ./internal/auth/
func BenchmarkHash(b *testing.B) {
	for _, c := range []struct {
		name string
		p    Params
	}{
		{"owasp-19MiB-t2-p1", DefaultParams},
		{"46MiB-t1-p1", Params{46 * 1024, 1, 1}},
		{"64MiB-t3-p1", Params{64 * 1024, 3, 1}},
		{"64MiB-t3-p4", Params{64 * 1024, 3, 4}},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Hash("correct horse battery staple", c.p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
