package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// anonymizer rewrites bank exports so they keep their structure but contain no personal data.
// Every substitution is an HMAC of the original value under one key, so a value maps to the same
// fake value in every file of a run and de-duplication behaviour is preserved.
type anonymizer struct {
	key       []byte
	shiftDays int
	factor    float64 // amounts are multiplied by this, in [0.7, 1.3]
}

func newAnonymizer(key string, shiftDays int) *anonymizer {
	a := &anonymizer{key: []byte(key)}
	u := float64(binary.BigEndian.Uint64(a.mac("factor", ""))>>11) / (1 << 53)
	a.factor = math.Round((0.7+0.6*u)*1000) / 1000
	a.shiftDays = shiftDays
	if shiftDays == 0 { // derive a constant shift of 30–400 days into the past
		a.shiftDays = -(30 + int(binary.BigEndian.Uint32(a.mac("shift", ""))%371))
	}
	return a
}

func (a *anonymizer) mac(kind, v string) []byte {
	h := hmac.New(sha256.New, a.key)
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(v))
	return h.Sum(nil)
}

/* ---------- column roles ---------- */

type colKind int

const (
	colFree   colKind = iota // free text: words, digit runs, inline dates and amounts
	colKeep                  // category, currency, operation type: copied verbatim
	colAmount                // every number is an amount (integers too)
	colPayee                 // merchant / counterparty name: "NAME    CITY"
)

func (k colKind) String() string {
	return [...]string{"free", "keep", "amount", "payee"}[k]
}

var (
	reKeep    = regexp.MustCompile(`^(waluta|currency|ccy)$|kategor|categor|^(typ|rodzaj)\b|\btype\b`)
	reAmount  = regexp.MustCompile(`kwota|amount|saldo|balance|wplyw|wydatk|obciaz|uznani|debit|credit|warto|value|suma`)
	reAddress = regexp.MustCompile(`adres|address|lokaliz`)
	rePayee   = regexp.MustCompile(`nadawca|odbiorca|kontrahent|payee|merchant|recipient|beneficiar|counterpart|^name$|^nazwa`)
)

func classify(header string) colKind {
	h := strings.TrimLeft(strings.TrimSpace(norm(header)), "#")
	switch {
	case h == "":
		return colFree
	case reKeep.MatchString(h):
		return colKeep
	case reAmount.MatchString(h):
		return colAmount
	case reAddress.MatchString(h):
		return colFree
	case rePayee.MatchString(h):
		return colPayee
	}
	return colFree
}

// findHeader returns the index of the header record, or -1. Unlike the legacy rule (which mis-picks the
// header for PKO BP and mBank exports) it takes the modal field count of records holding a date or
// amount, finds the first such data record, and accepts the closest non-blank record before it if that
// one has no dates, numbers or long digit runs. Picking a data row as the header would copy
// it verbatim, so the rule errs towards "no header".
func findHeader(recs []record) int {
	counts := map[int]int{} // field counts of records holding a date or amount (preambles can outnumber data)
	for _, r := range recs {
		if hasValue(r) {
			counts[len(r.Fields)]++
		}
	}
	mode, mf := 0, 0
	for n, c := range counts {
		if c > mf || (c == mf && n > mode) {
			mode, mf = n, c
		}
	}
	first := -1
	for i, r := range recs {
		if len(r.Fields) == mode && hasValue(r) {
			first = i
			break
		}
	}
	for i := first - 1; i >= 0; i-- {
		r := recs[i]
		if r.blank() {
			continue
		}
		if r.nonEmpty() >= 2 && len(r.Fields) >= mode-1 && !hasValue(r) && !reLongDigits.MatchString(cellsText(r)) {
			return i
		}
		return -1
	}
	return -1
}

func cellsText(r record) string {
	var b strings.Builder
	for _, f := range r.Fields {
		b.WriteString(f.Value + f.Tail + " ")
	}
	return b.String()
}

func hasValue(r record) bool {
	for _, f := range r.Fields {
		v := strings.TrimPrefix(strings.TrimSpace(f.Value+f.Tail), "'")
		if v == "" {
			continue
		}
		if _, ok := parseDateCell(v); ok {
			return true
		}
		if _, ok := parseAmount(v); ok {
			return true
		}
	}
	return false
}

/* ---------- report ---------- */

type report struct {
	Encoding  string
	BOM       bool
	Delimiter rune
	Records   int
	HeaderAt  int // 1-based record number, 0 = none found
	Columns   []string
}

func (r report) String() string {
	d := map[rune]string{';': "semicolon", ',': "comma", '\t': "tab", '|': "pipe"}[r.Delimiter]
	h := "none"
	if r.HeaderAt > 0 {
		h = "record " + strconv.Itoa(r.HeaderAt)
	}
	return fmt.Sprintf("encoding=%s bom=%v delimiter=%s records=%d header=%s columns=[%s]",
		r.Encoding, r.BOM, d, r.Records, h, strings.Join(r.Columns, ", "))
}

/* ---------- file ---------- */

func (a *anonymizer) file(b []byte, enc string, delim rune) ([]byte, report, error) {
	text, src, err := decode(b, enc)
	if err != nil {
		return nil, report{}, err
	}
	if delim == 0 {
		delim = detectDelim(text)
	}
	recs := parse(text, delim)
	hdr := findHeader(recs)
	rep := report{Encoding: src.Enc, BOM: src.BOM, Delimiter: delim, Records: len(recs), HeaderAt: hdr + 1}
	var kinds []colKind
	if hdr >= 0 {
		for _, f := range recs[hdr].Fields {
			k := classify(f.Value + f.Tail)
			kinds = append(kinds, k)
			name := strings.TrimSpace(f.Value + f.Tail)
			if name == "" {
				name = "(blank)"
			}
			rep.Columns = append(rep.Columns, name+":"+k.String())
		}
	}
	for i := range recs {
		for j := range recs[i].Fields {
			f := &recs[i].Fields[j]
			switch {
			case i == hdr: // header names are kept; only long digit runs are replaced
				f.Value, f.Tail = a.digitsOnly(f.Value), a.digitsOnly(f.Tail)
			case i < hdr: // preamble: account owner, period, summary
				f.Value, f.Tail = a.cell(f.Value, colFree), a.cell(f.Tail, colFree)
			default:
				k := colFree
				if j < len(kinds) {
					k = kinds[j]
				}
				f.Value, f.Tail = a.cell(f.Value, k), a.cell(f.Tail, k)
			}
		}
	}
	out, err := encode(write(recs, delim), src)
	return out, rep, err
}

func (a *anonymizer) cell(v string, k colKind) string {
	if k == colKeep {
		return v
	}
	core := strings.TrimSpace(v)
	if core == "" {
		return v
	}
	lead := v[:len(v)-len(strings.TrimLeftFunc(v, unicode.IsSpace))]
	trail := v[len(lead)+len(core):]
	if s, ok := a.shiftDateCell(core); ok {
		return lead + s + trail
	}
	if s, ok := a.scaleAmount(core, k == colAmount); ok {
		return lead + s + trail
	}
	if k == colPayee {
		return lead + a.payee(core) + trail
	}
	return lead + a.freeText(core) + trail
}

/* ---------- dates ---------- */

var (
	reISODate = regexp.MustCompile(`^(\d{4})([-./])(\d{1,2})([-./])(\d{1,2})`)
	reDMYDate = regexp.MustCompile(`^(\d{1,2})([./-])(\d{1,2})([./-])(\d{4})`)
	reTime    = regexp.MustCompile(`^(?:[ T]|,? )\d{1,2}:\d{2}(?::\d{2})?$`)
)

type dateMatch struct {
	iso      bool
	y, m, d  int
	sep1     string
	sep2     string
	wm, wd   int // digit widths, so 1.2.2025 stays unpadded
	consumed int
}

func matchDate(s string) (dateMatch, bool) {
	var dm dateMatch
	if m := reISODate.FindStringSubmatch(s); m != nil {
		dm = dateMatch{iso: true, sep1: m[2], sep2: m[4], wm: len(m[3]), wd: len(m[5]), consumed: len(m[0])}
		dm.y, _ = strconv.Atoi(m[1])
		dm.m, _ = strconv.Atoi(m[3])
		dm.d, _ = strconv.Atoi(m[5])
	} else if m := reDMYDate.FindStringSubmatch(s); m != nil {
		dm = dateMatch{sep1: m[2], sep2: m[4], wd: len(m[1]), wm: len(m[3]), consumed: len(m[0])}
		dm.d, _ = strconv.Atoi(m[1])
		dm.m, _ = strconv.Atoi(m[3])
		dm.y, _ = strconv.Atoi(m[5])
	} else {
		return dm, false
	}
	t := time.Date(dm.y, time.Month(dm.m), dm.d, 0, 0, 0, 0, time.UTC)
	if t.Year() != dm.y || int(t.Month()) != dm.m || t.Day() != dm.d {
		return dm, false // 31.02.2025 is not a date
	}
	return dm, true
}

// parseDateCell accepts a whole cell that is a date, optionally followed by a time.
func parseDateCell(s string) (dateMatch, bool) {
	dm, ok := matchDate(s)
	if !ok {
		return dm, false
	}
	if rest := s[dm.consumed:]; rest != "" && !reTime.MatchString(rest) {
		return dm, false
	}
	return dm, true
}

func (a *anonymizer) shiftDate(dm dateMatch) string {
	t := time.Date(dm.y, time.Month(dm.m), dm.d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, a.shiftDays)
	p := func(n, w int) string { return fmt.Sprintf("%0*d", w, n) }
	if dm.iso {
		return p(t.Year(), 4) + dm.sep1 + p(int(t.Month()), dm.wm) + dm.sep2 + p(t.Day(), dm.wd)
	}
	return p(t.Day(), dm.wd) + dm.sep1 + p(int(t.Month()), dm.wm) + dm.sep2 + p(t.Year(), 4)
}

func (a *anonymizer) shiftDateCell(s string) (string, bool) {
	dm, ok := parseDateCell(s)
	if !ok {
		return "", false
	}
	return a.shiftDate(dm) + s[dm.consumed:], true
}

/* ---------- amounts ---------- */

var (
	reCurPrefix = regexp.MustCompile(`^(?i)(PLN|EUR|USD|GBP|CHF|zł|\$|€|£)\s*`)
	reCurSuffix = regexp.MustCompile(`(?i)\s*(PLN|EUR|USD|GBP|CHF|zł|\$|€|£)$`)
	reNumBody   = regexp.MustCompile(`^[0-9](?:[0-9 \x{a0}\x{202f}.,']*[0-9])?$`)
	reNonDigits = regexp.MustCompile(`[^0-9]+`)
)

type amount struct {
	pre, post   string // sign / currency text before and after the digits, kept verbatim
	group, dec  string // separators ("" when absent)
	fracDigits  int
	units       int64 // value in units of 10^-fracDigits, unsigned
	intDigitsLn int
}

// parseAmount recognises the number formats legacy parseNum accepts (-1 234,56 · 1.234,56 · 1,234.56 ·
// 12,00 PLN · €12 · -800,00 PLN) and records the exact layout so the scaled value can be written back.
func parseAmount(s string) (amount, bool) {
	var am amount
	rest := s
	take := func(re *regexp.Regexp, prefix bool) {
		if m := re.FindString(rest); m != "" {
			if prefix {
				am.pre += m
				rest = rest[len(m):]
			} else {
				am.post = m + am.post
				rest = rest[:len(rest)-len(m)]
			}
		}
	}
	if rest != "" && (rest[0] == '-' || rest[0] == '+') {
		am.pre, rest = rest[:1], rest[1:]
	}
	take(reCurPrefix, true)
	if am.pre != "" && !strings.ContainsAny(am.pre, "-+") && rest != "" && (rest[0] == '-' || rest[0] == '+') {
		am.pre, rest = am.pre+rest[:1], rest[1:]
	}
	take(reCurSuffix, false)
	if !reNumBody.MatchString(rest) {
		return am, false
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, rest)
	if len(digits) > 15 {
		return am, false // account / reference numbers
	}
	// separator roles, following legacy parseNum: with both . and , the later one is decimal;
	// a single occurrence of one of them is decimal; repeated occurrences are thousands.
	lc, ld := strings.LastIndex(rest, ","), strings.LastIndex(rest, ".")
	decAt := -1
	switch {
	case lc >= 0 && ld >= 0:
		decAt = max(lc, ld)
	case lc >= 0 && strings.Count(rest, ",") == 1:
		decAt = lc
	case ld >= 0 && strings.Count(rest, ".") == 1:
		decAt = ld
	}
	intPart, frac := rest, ""
	if decAt >= 0 {
		am.dec, intPart, frac = rest[decAt:decAt+1], rest[:decAt], rest[decAt+1:]
	}
	groups := reNonDigits.Split(intPart, -1)
	seps := reNonDigits.FindAllString(intPart, -1)
	for i, sp := range seps {
		if sp != seps[0] || utf8.RuneCountInString(sp) != 1 || sp == am.dec || len(groups[i+1]) != 3 {
			return am, false
		}
	}
	if len(seps) > 0 {
		am.group = seps[0]
		if len(groups[0]) > 3 {
			return am, false
		}
	}
	intDigits := strings.Join(groups, "")
	if len(intDigits) > 1 && intDigits[0] == '0' {
		return am, false // leading-zero identifiers
	}
	if strings.ContainsAny(frac, "   .,'") {
		return am, false
	}
	am.fracDigits, am.intDigitsLn = len(frac), len(intDigits)
	u, err := strconv.ParseInt(intDigits+frac, 10, 64)
	if err != nil {
		return am, false
	}
	am.units = u
	return am, true
}

func (am amount) format(units int64) string {
	s := strconv.FormatInt(units, 10)
	if len(s) <= am.fracDigits {
		s = strings.Repeat("0", am.fracDigits-len(s)+1) + s
	}
	ip, fp := s[:len(s)-am.fracDigits], s[len(s)-am.fracDigits:]
	if am.group != "" {
		var b strings.Builder
		for i, c := range ip {
			if i > 0 && (len(ip)-i)%3 == 0 {
				b.WriteString(am.group)
			}
			b.WriteRune(c)
		}
		ip = b.String()
	}
	if am.fracDigits > 0 {
		ip += am.dec + fp
	}
	return am.pre + ip + am.post
}

// scaleAmount multiplies a whole-cell amount by the run's factor. Outside amount columns only values
// with exactly two decimals count as amounts (integers there are ids, house numbers, counts…).
func (a *anonymizer) scaleAmount(s string, anyNumber bool) (string, bool) {
	am, ok := parseAmount(s)
	if !ok || (!anyNumber && am.fracDigits != 2) {
		return "", false
	}
	return am.format(int64(math.Round(float64(am.units) * a.factor))), true
}

/* ---------- payees ---------- */

var reWideSpace = regexp.MustCompile(`\s{2,}`)

// payee replaces "NAME    CITY": the first segment becomes a fake merchant, the last a fake city,
// middle segments are treated as free text. Padding is adjusted so later segments keep their column.
func (a *anonymizer) payee(s string) string {
	seps := reWideSpace.FindAllString(s, -1)
	segs := reWideSpace.Split(s, -1)
	if len(segs) == 1 {
		return withCase(a.pick("payee", segs[0], merchants), segs[0])
	}
	out := make([]string, len(segs))
	out[0] = withCase(a.pick("payee", segs[0], merchants), segs[0])
	for i := 1; i < len(segs)-1; i++ {
		out[i] = a.freeText(segs[i])
	}
	last := segs[len(segs)-1]
	if strings.IndexFunc(last, unicode.IsDigit) >= 0 {
		out[len(segs)-1] = a.freeText(last)
	} else {
		out[len(segs)-1] = withCase(a.pick("city", last, cities), last)
	}
	var b strings.Builder
	for i, seg := range out {
		b.WriteString(seg)
		if i < len(seps) {
			width := utf8.RuneCountInString(segs[i]) + utf8.RuneCountInString(seps[i])
			b.WriteString(strings.Repeat(seps[i][:1], max(2, width-utf8.RuneCountInString(seg))))
		}
	}
	return b.String()
}

func (a *anonymizer) pick(kind, v string, list []string) string {
	h := a.mac(kind, collapse(norm(v)))
	return list[binary.BigEndian.Uint32(h)%uint32(len(list))] //nolint:gosec // list lengths are tiny constants
}

/* ---------- free text ---------- */

var (
	reToken = regexp.MustCompile(`\d{4}[-./]\d{1,2}[-./]\d{1,2}|\d{1,2}[./-]\d{1,2}[./-]\d{4}` + // dates
		`|\d{2,4}(?: \d{4}){3,}` + // spaced account numbers (PL61 1090 1014 …)
		`|\d{1,3}(?:[ \x{a0}]\d{3})+[.,]\d{2}|\d+[.,]\d{2}` + // amounts with two decimals
		`|[\pL\d]{8,}` + // long alphanumeric codes (references, terminal ids)
		`|\d+|\pL+`) // digit runs, words
	reLongDigits  = regexp.MustCompile(`\d{6,}`)
	reDigitGroups = regexp.MustCompile(`\d+(?: \d+)*`)
)

func (a *anonymizer) freeText(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range reToken.FindAllStringIndex(s, -1) {
		b.WriteString(s[last:m[0]])
		b.WriteString(a.token(s, m[0], m[1]))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func (a *anonymizer) token(s string, i, j int) string {
	t := s[i:j]
	prev, next := byte(0), byte(0)
	if i > 0 {
		prev = s[i-1]
	}
	if j < len(s) {
		next = s[j]
	}
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	letters, digits := strings.IndexFunc(t, unicode.IsLetter) >= 0, strings.IndexFunc(t, unicode.IsDigit) >= 0
	switch {
	case letters && digits:
		return a.code(t)
	case letters:
		if utf8.RuneCountInString(t) <= 2 || allowed[norm(t)] {
			return t
		}
		return a.fakeWord(t)
	case isDigit(prev) || isDigit(next):
		return a.digits(t, true)
	}
	if dm, ok := matchDate(t); ok && dm.consumed == len(t) {
		return a.shiftDate(dm)
	}
	if strings.Contains(t, " ") && !strings.ContainsAny(t, ".,") { // spaced account number
		return a.digits(t, true)
	}
	if out, ok := a.scaleAmount(t, false); ok && strings.ContainsAny(t, ".,") && !strings.ContainsRune("-./", rune(prev)) && !strings.ContainsRune("-./", rune(next)) {
		return out // a standalone amount; "28.02-28.03" is a date range, not two amounts
	}
	return a.digits(t, prev == '*' || next == '*')
}

// digits replaces runs of 6+ digits (or every run when force is set, e.g. next to a '*' card mask)
// with fake digits of the same length. 26-digit runs (Polish account numbers) become the all-zero
// form that tools/check-private.sh allowlists, so anonymised fixtures can be committed.
func (a *anonymizer) digits(t string, force bool) string {
	return reDigitGroups.ReplaceAllStringFunc(t, func(run string) string {
		plain := strings.ReplaceAll(run, " ", "")
		if (!force && len(plain) < 6) || strings.Trim(plain, "0") == "" {
			return run
		}
		fake := a.fakeDigits(plain)
		var b strings.Builder
		k := 0
		for _, c := range run {
			if c == ' ' {
				b.WriteRune(c)
				continue
			}
			b.WriteByte(fake[k])
			k++
		}
		return b.String()
	})
}

func (a *anonymizer) digitsOnly(s string) string {
	return reLongDigits.ReplaceAllStringFunc(s, func(run string) string { return a.digits(run, false) })
}

func (a *anonymizer) fakeDigits(d string) string {
	n := len(d)
	for salt := 0; ; salt++ {
		h := a.mac("digits", d+strings.Repeat("#", salt))
		var b strings.Builder
		for b.Len() < n {
			for _, c := range h {
				b.WriteByte('0' + c%10)
			}
			h = a.mac("digits+", string(h))
		}
		out := b.String()[:n]
		if n >= 20 { // account-number length: synthetic all-zero form with a short distinguishing tail
			tail := min(5, n-20)
			out = strings.Repeat("0", n-tail) + out[n-tail:]
		}
		if out != d {
			return out
		}
	}
}

// code replaces a mixed letter/digit code character by character (letters stay letters, digits stay digits).
func (a *anonymizer) code(t string) string {
	h := a.mac("code", t)
	out := []rune(t)
	for i, r := range out {
		c := h[i%len(h)] ^ byte(i/len(h)*7) //nolint:gosec // wraps on purpose: only used to vary characters
		switch {
		case unicode.IsDigit(r):
			out[i] = rune('0' + c%10)
		case unicode.IsUpper(r):
			out[i] = rune('A' + c%26)
		case unicode.IsLetter(r):
			out[i] = rune('a' + c%26)
		}
	}
	if string(out) == t {
		return a.code(t + "#")[:len(t)]
	}
	return string(out)
}

const (
	consonants = "bcdfghjklmnprstwz"
	vowels     = "aeiouy"
)

// fakeWord returns a pronounceable ASCII word of the same length, with the same per-letter case.
func (a *anonymizer) fakeWord(w string) string {
	src := []rune(w)
	for salt := 0; ; salt++ {
		h := a.mac("word", norm(w)+strings.Repeat("#", salt))
		out := make([]rune, len(src))
		for i := range src {
			c := h[i%len(h)] ^ byte(i/len(h)) //nolint:gosec // wraps on purpose: only used to vary letters
			var r rune
			if i%2 == int(h[0]%2) {
				r = rune(consonants[int(c)%len(consonants)])
			} else {
				r = rune(vowels[int(c)%len(vowels)])
			}
			if unicode.IsUpper(src[i]) {
				r = unicode.ToUpper(r)
			}
			out[i] = r
		}
		if norm(string(out)) != norm(w) && !allowed[norm(string(out))] {
			return string(out)
		}
	}
}

// withCase applies the case style of orig (UPPER, lower, Title) to a fake value.
func withCase(fake, orig string) string {
	letters := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return r
		}
		return -1
	}, orig)
	switch {
	case letters == "" || letters == strings.ToUpper(letters):
		return strings.ToUpper(fake)
	case letters == strings.ToLower(letters):
		return strings.ToLower(fake)
	}
	words := strings.Fields(strings.ToLower(fake))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
