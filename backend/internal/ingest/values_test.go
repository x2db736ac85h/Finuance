package ingest

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

func TestParseNumberCorpus(t *testing.T) {
	for _, c := range loadCorpus(t, "parseNum") {
		in := jsString(c.In)
		var want *float64
		_ = json.Unmarshal(c.Out, &want)
		got, ok := ParseNumber(in)
		switch {
		case want == nil && ok:
			t.Errorf("ParseNumber(%q) = %v, legacy NaN", in, got)
		case want != nil && !ok:
			t.Errorf("ParseNumber(%q) failed, legacy %v", in, *want)
		case want != nil && got != *want:
			t.Errorf("ParseNumber(%q) = %v, legacy %v", in, got, *want)
		}
	}
}

// dateDiffReason explains an intentional difference between ParseDate and the legacy parseDate
// (ALLOWED_DIFFS.md rows 1, 9–11) — or returns "" when the difference is a bug. It is strict:
// it re-derives the date fields from the input and checks that our result is exactly what the
// rule predicts, so it cannot hide an unrelated mismatch.
//
//   - "impossible date": the y-m-d fields are not a calendar date (31.02.2025, 29.02.2025); the
//     legacy rolled them over, we reject them (got == "").
//   - "year below 100": Date.UTC maps years 0–99 to 1900–1999; we reject them.
//   - "time rolls the date": a time such as 24:00 or 12:60 moved the legacy date forward; times
//     are ignored here, so we return the written date.
func dateDiffReason(in, got string) string {
	y, m, d, hh, mi, ss, ok := dateFields(in)
	if !ok {
		return ""
	}
	written, err := platform.NewDate(y, time.Month(m), d)
	switch {
	case y < 100 && got == "":
		return "year below 100"
	case err != nil && got == "":
		return "impossible date"
	case err == nil && got == written.String() && (hh >= 24 || mi >= 60 || ss >= 60):
		return "time rolls the date"
	}
	return ""
}

// The legacy regexes, kept here as the reference for the hand-written scanners and dateFields.
var (
	isoDate = regexp.MustCompile(`^(\d{4})[-./](\d{1,2})[-./](\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?)?`)
	dmyDate = regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(?:[ T,]+(\d{1,2}):(\d{2})(?::(\d{2}))?)?$`)
)

// dateFields extracts the numeric fields with the legacy regexes.
func dateFields(in string) (y, m, d, hh, mi, ss int, ok bool) {
	s := jsTrim(in)
	atoi := func(v string) int { n, _ := strconv.Atoi(v); return n }
	if g := isoDate.FindStringSubmatch(s); g != nil {
		return atoi(g[1]), atoi(g[2]), atoi(g[3]), atoi(g[4]), atoi(g[5]), atoi(g[6]), true
	}
	if g := dmyDate.FindStringSubmatch(s); g != nil {
		return atoi(g[3]), atoi(g[2]), atoi(g[1]), atoi(g[4]), atoi(g[5]), atoi(g[6]), true
	}
	return 0, 0, 0, 0, 0, 0, false
}

func TestParseDateCorpus(t *testing.T) {
	reasons := map[string]int{}
	for _, c := range loadCorpus(t, "parseDate") {
		var in string
		_ = json.Unmarshal(c.In, &in)
		var legacy *string
		_ = json.Unmarshal(c.Out, &legacy)
		want := ""
		if legacy != nil {
			want = (*legacy)[:10] // times are ignored: compare the calendar date
		}
		got := ""
		if d, ok := ParseDate(in); ok {
			got = d.String()
		}
		if got != want {
			reason := dateDiffReason(in, got)
			if reason == "" {
				t.Errorf("ParseDate(%q) = %q, legacy %q: unexplained difference", in, got, want)
				continue
			}
			reasons[reason]++
		}
	}
	t.Logf("intentional differences: %v", reasons)
}

// looseDiffReason explains intentional LooseDate differences: the ParseDate reasons above, plus
// "time widens to the day" — the legacy matched one minute for "2025-01-02 07:21", we match the
// whole booking day.
func looseDiffReason(in, got string, legacy []string) string {
	from, _, ok := strings.Cut(got, "..")
	if !ok && got != "" {
		return ""
	}
	if r := dateDiffReason(in, from); r != "" && (got == "" || r == "time rolls the date") {
		return r
	}
	for _, re := range []*regexp.Regexp{looseYear, looseYearMonth, looseMonthYear} {
		if g := re.FindStringSubmatch(jsTrim(in)); g != nil && got == "" {
			y := g[1]
			if re == looseMonthYear {
				y = g[2]
			}
			if n, _ := strconv.Atoi(y); n < 100 {
				return "year below 100"
			}
		}
	}
	_, _, _, hh, mi, _, fields := dateFields(in)
	if fields && legacy != nil && strings.Contains(legacy[0], "T") && hh < 24 && mi < 60 {
		day, _ := platform.ParseDate(legacy[0][:10])
		if got == day.String()+".."+day.AddDays(1).String() {
			return "time widens to the day"
		}
	}
	return ""
}

func TestLooseDateCorpus(t *testing.T) {
	reasons := map[string]int{}
	for _, c := range loadCorpus(t, "looseDate") {
		var in string
		_ = json.Unmarshal(c.In, &in)
		var legacy []string
		_ = json.Unmarshal(c.Out, &legacy)
		want := ""
		if legacy != nil {
			// JavaScript writes years beyond 9999 in extended form ("+010000-01-01T…").
			ext := strings.NewReplacer("+010000-01", "10000-01-01") // the oracle cut this ISO string at 10 chars
			want = ext.Replace(legacy[0]) + ".." + ext.Replace(legacy[1])
		}
		got := ""
		if from, to, ok := LooseDate(in); ok {
			got = from.String() + ".." + to.String()
		}
		if got != want {
			reason := looseDiffReason(in, got, legacy)
			if reason == "" {
				t.Errorf("LooseDate(%q) = %q, legacy %q: unexplained difference", in, got, want)
				continue
			}
			reasons[reason]++
		}
	}
	t.Logf("intentional differences: %v", reasons)
}

func TestDateOrderMDY(t *testing.T) {
	for in, want := range map[string]string{"10/31/2025": "2025-10-31", "03/04/2025": "2025-03-04", "3/4/2025 10:00": "2025-03-04", "2025-03-04": "2025-03-04"} {
		d, ok := ParseDateOrdered(in, DateOrderMDY)
		if !ok || d.String() != want {
			t.Errorf("mdy %q = %v %v, want %s", in, d, ok, want)
		}
	}
	if _, ok := ParseDateOrdered("10/31/2025", DateOrderDMY); ok {
		t.Error("dmy must reject month 31")
	}
	if d, _ := ParseDate("31.12.2024"); d != platform.MustDate(2024, 12, 31) {
		t.Error("dmy")
	}
	if !strings.HasPrefix(platform.MustDate(2025, 3, 4).String(), "2025") {
		t.Error("format")
	}
}
