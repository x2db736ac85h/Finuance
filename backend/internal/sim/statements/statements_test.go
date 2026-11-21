package statements

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

var update = flag.Bool("update", false, "rewrite testdata/fixtures/sim/")

func date(y int, m time.Month, d int) platform.Date { return platform.MustDate(y, m, d) }

func addDays(d platform.Date, n int) platform.Date {
	return platform.DateOf(d.Time().AddDate(0, 0, n), time.UTC)
}

// byDay groups rows by their booking day, keeping their order.
func byDay(rows []Row) map[platform.Date][]Row {
	out := map[platform.Date][]Row{}
	for i := range rows {
		out[rows[i].Booked] = append(out[rows[i].Booked], rows[i])
	}
	return out
}

// TestSameDayInAnyRange: a day's rows don't depend on the range they are asked for in, which is
// what lets overlapping statements de-duplicate like real exports (PLAN 25.2).
func TestSameDayInAnyRange(t *testing.T) {
	s := newStream("test ranges")
	base := date(2025, time.November, 1)
	for _, i := range []int{0, 1, 6, 9, 10, 49} {
		p := identity.Nth(i)
		for _, account := range []int{Current, Savings} {
			for range 40 {
				from := addDays(base, s.intn(200))
				to := addDays(from, s.intn(70))
				days := byDay(Rows(p, account, from, to))
				for d, got := range days {
					if d.Compare(from) < 0 || d.Compare(to) > 0 {
						t.Fatalf("%s %d: a row of %s in %s … %s", p.Slug, account, d, from, to)
					}
					if want := Rows(p, account, d, d); !cmp.Equal(got, want) {
						t.Fatalf("%s %d: %s differs in %s … %s:\n%s", p.Slug, account, d, from, to, cmp.Diff(want, got))
					}
				}
			}
		}
	}
}

// TestPersonasDiffer: two personas don't share a history, and a day is not a copy of another.
func TestPersonasDiffer(t *testing.T) {
	from, to := date(2026, time.January, 1), date(2026, time.January, 31)
	a, b := Rows(identity.Nth(0), Current, from, to), Rows(identity.Nth(10), Current, from, to)
	if cmp.Equal(a, b) {
		t.Error("otter and otter-2 have the same January")
	}
	if cmp.Equal(Day(identity.Nth(0), date(2026, time.January, 5)), Day(identity.Nth(0), date(2026, time.January, 6))) {
		t.Error("two days are the same")
	}
}

// TestRecurringAndTransfers: every month has the salary, the rent and the subscriptions on their
// days, and each transfer between a persona's accounts appears on both statements on the same
// day with the opposite amount (what recurring payments and transfer detection look for).
func TestRecurringAndTransfers(t *testing.T) {
	from, to := date(2026, time.January, 1), date(2026, time.June, 30)
	for _, i := range []int{0, 3, 7} {
		p := identity.Nth(i)
		pr := profileOf(p)
		cur, sav := Rows(p, Current, from, to), Rows(p, Savings, from, to)
		count := func(rows []Row, match func(Row) bool) int {
			n := 0
			for _, r := range rows {
				if match(r) {
					n++
				}
			}
			return n
		}
		if n := count(cur, func(r Row) bool { return r.Category == "Wynagrodzenie" && r.Booked.D == pr.payday && r.Amount > 0 }); n != 6 {
			t.Errorf("%s: %d salaries in six months", p.Slug, n)
		}
		if n := count(cur, func(r Row) bool { return r.Category == "Mieszkanie" && r.Amount == -pr.rent }); n != 6 {
			t.Errorf("%s: %d rents in six months", p.Slug, n)
		}
		for _, sub := range pr.subs {
			if n := count(cur, func(r Row) bool { return r.Payee == sub.payee && r.Amount == sub.amount && r.Booked.D == sub.day }); n != 6 {
				t.Errorf("%s: %s %d times in six months", p.Slug, sub.payee, n)
			}
		}
		pair := func(from, to []Row, k kind) {
			for _, r := range from {
				if r.Kind != k {
					continue
				}
				if !slices.ContainsFunc(to, func(o Row) bool { return o.Booked == r.Booked && o.Amount == -r.Amount && o.Title == r.Title }) {
					t.Errorf("%s: the transfer %+v has no other half", p.Slug, r)
				}
			}
		}
		pair(cur, sav, ownOut)
		pair(sav, cur, ownOut)
		if n := count(sav, func(r Row) bool { return r.Kind == interest }); n != 6 {
			t.Errorf("%s: %d interest payments in six months", p.Slug, n)
		}
	}
}

// staged parses a statement as the upload endpoint does, with the detected mapping.
func staged(t *testing.T, b []byte) []ingest.StagedRow {
	t.Helper()
	res, err := ingest.Parse(context.Background(), b, ingest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows, stats, err := ingest.Stage(context.Background(), res.Table, ingest.MappingFromRoles(res.Roles),
		ingest.StageOptions{ImportCurrency: res.Roles.Currency})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Errors != 0 {
		t.Fatalf("%d rows with errors", stats.Errors)
	}
	return rows
}

// TestStampChangesBytesNotRows: each upload can be byte-unique (the API answers 409 to a file it
// already holds) while the importer reads the same rows from it, in both layouts.
func TestStampChangesBytesNotRows(t *testing.T) {
	from, to := date(2026, time.February, 1), date(2026, time.February, 28)
	for _, i := range []int{0, 1} {
		p := identity.Nth(i)
		for _, account := range []int{Current, Savings} {
			_, plain := File(p, account, from, to, "")
			_, a := File(p, account, from, to, "sim-otter-1")
			_, b := File(p, account, from, to, "sim-otter-2")
			if bytes.Equal(a, b) || bytes.Equal(a, plain) {
				t.Fatalf("%s %s: the stamp does not change the bytes", p.Slug, LayoutOf(p, account))
			}
			want := staged(t, plain)
			if len(want) != len(Rows(p, account, from, to)) {
				t.Errorf("%s %s: %d rows read, %d written", p.Slug, LayoutOf(p, account), len(want), len(Rows(p, account, from, to)))
			}
			for _, got := range [][]ingest.StagedRow{staged(t, a), staged(t, b)} {
				if len(got) != len(want) {
					t.Fatalf("%s: %d rows with a stamp, %d without", p.Slug, len(got), len(want))
				}
				for k := range got {
					if ingest.FingerprintKey(&got[k], 2) != ingest.FingerprintKey(&want[k], 2) {
						t.Errorf("%s row %d: %q with a stamp, %q without", p.Slug, k, ingest.FingerprintKey(&got[k], 2), ingest.FingerprintKey(&want[k], 2))
					}
				}
			}
		}
	}
	_, odd := File(identity.Nth(0), Current, from, from, "a;b\"c\nd é")
	if !strings.HasPrefix(string(odd), "Eksport;abcd\n") {
		t.Errorf("the stamp is not cleaned: %q", strings.SplitN(string(odd), "\n", 2)[0])
	}
}

func TestAmount(t *testing.T) {
	for g, want := range map[int64]string{0: "0,00", 5: "0,05", -1639: "-16,39", 213222: "2 132,22", -213222: "-2 132,22",
		100_000_000: "1 000 000,00", 99_999: "999,99"} {
		if got := amount(g); got != want {
			t.Errorf("amount(%d) = %q, want %q", g, got, want)
		}
	}
}

func TestNumbers(t *testing.T) {
	seen := map[string]bool{}
	for i := range identity.Max {
		for _, account := range []int{Current, Savings} {
			n := Number(identity.Nth(i), account)
			if len(n) != 26 || !strings.HasPrefix(n, strings.Repeat("0", 20)) || seen[n] {
				t.Errorf("Number(%d, %d) = %s", i, account, n)
			}
			seen[n] = true
		}
	}
}

// samples are the committed sample statements: a reader can see what the simulator uploads, and
// tools/check-private.sh scans them with everything else.
var samples = []struct {
	persona, account int
	from, to         platform.Date
}{
	{0, Current, date(2026, time.January, 1), date(2026, time.January, 31)}, // Pekao-like
	{0, Savings, date(2026, time.January, 1), date(2026, time.March, 31)},   // mBank-like
	{1, Current, date(2026, time.January, 1), date(2026, time.January, 31)}, // mBank-like, many rows
}

const sampleDir = "../../../../testdata/fixtures/sim"

// TestSamples keeps the sample statements equal to the generator (go test -update rewrites them).
func TestSamples(t *testing.T) {
	want := map[string]bool{"README.md": true}
	for _, s := range samples {
		name, body := File(identity.Nth(s.persona), s.account, s.from, s.to, "")
		want[name] = true
		path := filepath.Join(sampleDir, name)
		if *update {
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		have, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(have, body) {
			t.Errorf("%s is out of date (go test ./internal/sim/statements/ -run TestSamples -update): %v", path, err)
		}
	}
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !want[e.Name()] {
			t.Errorf("%s/%s is not a sample the generator writes", sampleDir, e.Name())
		}
	}
}

// TestEveryStatementPassesThePrivateCheck: a year of every persona's statements, both accounts,
// through tools/check-private.sh (account numbers, e-mail addresses, postcodes, card numbers).
func TestEveryStatementPassesThePrivateCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("runs tools/check-private.sh")
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl")
	}
	dir := filepath.Join(t.TempDir(), "testdata") // the postcode rule applies to paths under testdata/
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var files []string
	for i := range identity.Max {
		for _, account := range []int{Current, Savings} {
			name, body := File(identity.Nth(i), account, date(2025, time.January, 1), date(2025, time.December, 31), "sim-check-1")
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			files = append(files, path)
		}
	}
	cmd := exec.Command("../../../../tools/check-private.sh", files...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("check-private: %v\n%s", err, out)
	}
}
