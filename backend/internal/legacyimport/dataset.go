package legacyimport

import (
	"context"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
)

// The legacy app labelled transactions with keys computed from its own view of each file (legacy
// txKeys, DESIGN §7.1 of the legacy design document): the table it built with its header rule and
// its per-file settings, the roles it detected on that table, and a key per row. To move the
// labels and duplicate decisions over, this file rebuilds those tables exactly and recomputes
// the keys; each key then points at a row of the new import, whose fingerprint and transaction
// are known. That is more reliable than translating a key into a fingerprint: the legacy roles
// (auto-detected) and the new mapping may pick different columns, and the key alone could not say.
//
// Numbers here are float64 because the legacy keys are made from floats (amount.toFixed(2)); they
// never become stored amounts.

// FileCfg is a legacy file's import settings (e.cfg, legacy v7). Every field is optional.
type FileCfg struct {
	Enc       string            `json:"enc,omitempty"`
	Delim     string            `json:"delim,omitempty"`
	Header    *int              `json:"header,omitempty"` // nil: automatic; 0: no header; n: record n
	Skip      int               `json:"skip,omitempty"`
	DropShort *bool             `json:"dropShort,omitempty"`
	DateOrder string            `json:"dateOrder,omitempty"`
	Invert    bool              `json:"invert,omitempty"`
	Map       map[string]string `json:"map,omitempty"` // column index → legacy role
}

// ParseOpts are the legacy global parsing options (S.opts in the preferences).
type ParseOpts struct {
	Enc   string `json:"enc,omitempty"`
	Delim string `json:"delim,omitempty"`
	Strip *bool  `json:"strip,omitempty"`
}

// fileOpts is the legacy fileOpts(e): the file's settings over the global options.
type fileOpts struct {
	enc, delim string
	strip      bool
	header     *int
	skip       int
	dropShort  bool
	mdy        bool
	invert     bool
	roles      map[int]string // column → role; unknown roles are dropped
}

func optsOf(cfg FileCfg, g ParseOpts) fileOpts {
	o := fileOpts{enc: firstNonEmpty(cfg.Enc, g.Enc, "auto"), delim: firstNonEmpty(cfg.Delim, g.Delim, "auto"),
		strip: g.Strip == nil || *g.Strip, header: cfg.Header, skip: max(cfg.Skip, 1),
		dropShort: cfg.DropShort == nil || *cfg.DropShort, mdy: cfg.DateOrder == "mdy", invert: cfg.Invert, roles: map[int]string{}}
	for k, role := range cfg.Map {
		j, err := strconv.Atoi(k)
		if err != nil || j < 0 || (role != "ignore" && role != "debit" && role != "credit" && canon[role] == "") {
			continue
		}
		o.roles[j] = role
	}
	return o
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// canon is the legacy CANON: the column names applyMapping gives mapped roles.
var canon = map[string]string{"date": "Date", "vdate": "Value date", "amount": "Amount", "payee": "Payee", "desc": "Description",
	"category": "Category", "currency": "Currency", "ref": "Reference", "type": "Type", "account": "Account", "balance": "Balance"}

// ingestOptions are the reading options with which ingest.Parse shapes the table as the legacy
// buildTable did (its header rule included).
func (o fileOpts) ingestOptions() ingest.Options {
	out := ingest.Options{Encoding: ingest.Encoding(o.enc),
		Shape: ingest.ShapeOptions{KeepShort: !o.dropShort, KeepQuote: !o.strip, LegacyHeader: true}}
	if o.delim != "auto" {
		out.Delimiter, _ = utf8.DecodeRuneInString(o.delim)
	}
	switch {
	case o.header == nil:
	case *o.header == 0:
		out.Shape.Header, out.Shape.SkipRows = ingest.HeaderNone, o.skip
	default:
		out.Shape.Header, out.Shape.HeaderRow = ingest.HeaderRow, *o.header
	}
	return out
}

// legacyFile is one file as the legacy app saw it: the table after applyMapping. Err is set for a
// file the legacy marked as an error (empty); such files are not part of the merged view.
type legacyFile struct {
	headers  []string
	rows     [][]string
	headerAt int // 1-based record of the header (0: none), to align the rows with the import's
	err      error
}

// readLegacy rebuilds the legacy table of a file (legacy parseEntry).
func readLegacy(ctx context.Context, raw []byte, o fileOpts) legacyFile {
	res, err := ingest.Parse(ctx, raw, o.ingestOptions())
	if err != nil {
		return legacyFile{err: err}
	}
	h, r := applyMapping(res.Table.Headers, res.Table.Rows, o)
	return legacyFile{headers: h, rows: r, headerAt: res.Table.HeaderAt}
}

var (
	reInvertName = regexp.MustCompile(`(?i)kwota|amount|warto|value|suma|betrag|montant`)
	reBalance    = regexp.MustCompile(`(?i)saldo|balance`)
	reDMYCell    = regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(.*)$`)
	reCommaCents = regexp.MustCompile(`,\d{1,2}\s*$`)
)

// applyMapping is the legacy applyMapping: with a role map, invert or month-first dates, the table
// is rewritten into canonical columns (Date, Amount, …), debit + credit become one signed Amount,
// inverted amounts are negated and month-first dates rewritten to ISO.
func applyMapping(headers []string, rows [][]string, o fileOpts) ([]string, [][]string) {
	mapped := false
	for j, role := range o.roles {
		if role != "" && j < len(headers) {
			mapped = true
		}
	}
	if !mapped && !o.invert && !o.mdy {
		return headers, rows
	}
	type col struct {
		j          int
		name, role string
	}
	cols := make([]col, len(headers))
	amt, deb, cred := -1, -1, -1
	for j, name := range headers {
		cols[j] = col{j: j, name: name, role: o.roles[j]}
		switch {
		case cols[j].role == "amount" && amt < 0:
			amt = j
		case cols[j].role == "debit" && deb < 0:
			deb = j
		case cols[j].role == "credit" && cred < 0:
			cred = j
		}
	}
	cell := func(r []string, j int) string {
		if j < len(r) {
			return r[j]
		}
		return ""
	}
	invCol := amt
	if o.invert && invCol < 0 && deb < 0 && cred < 0 {
		for _, c := range cols {
			if reInvertName.MatchString(c.name) && !reBalance.MatchString(c.name) {
				invCol = c.j
				break
			}
		}
		if invCol < 0 {
			for _, c := range cols {
				n, ok := 0, 0
				for i := 0; i < min(len(rows), 200); i++ {
					if v := cell(rows[i], c.j); v != "" {
						n++
						if _, isNum := ingest.ParseNumber(v); isNum {
							ok++
						}
					}
				}
				if n > 0 && float64(ok)/float64(n) >= .9 && !reBalance.MatchString(c.name) {
					invCol = c.j
					break
				}
			}
		}
	}
	dateCols := map[int]bool{}
	if o.mdy {
		for _, c := range cols {
			if c.role == "date" || c.role == "vdate" {
				dateCols[c.j] = true
				continue
			}
			if c.role != "" {
				continue
			}
			n, m := 0, 0
			for i := 0; i < min(len(rows), 200); i++ {
				if v := cell(rows[i], c.j); v != "" {
					n++
					if reDMYCell.MatchString(v) {
						m++
					}
				}
			}
			if n > 0 && float64(m)/float64(n) >= .8 {
				dateCols[c.j] = true
			}
		}
	}
	fmtAmt := func(x float64, like string) string {
		s := toFixed2(x)
		if reCommaCents.MatchString(like) {
			s = strings.Replace(s, ".", ",", 1)
		}
		return s
	}
	type outCol struct {
		name  string
		canon bool
		j     int
		calc  func(r []string) string
	}
	var out []outCol
	pairDone := false
	for _, c := range cols {
		if c.role == "ignore" {
			continue
		}
		if c.role == "debit" || c.role == "credit" {
			if amt >= 0 { // a signed Amount is mapped: keep both as plain columns
				name := "Debit"
				if c.role == "credit" {
					name = "Credit"
				}
				out = append(out, outCol{name: name, canon: true, j: c.j})
				continue
			}
			if pairDone {
				continue
			}
			pairDone = true
			out = append(out, outCol{name: "Amount", canon: true, calc: func(r []string) string {
				d, k := math.NaN(), math.NaN()
				if deb >= 0 {
					d = parseNum(cell(r, deb))
				}
				if cred >= 0 {
					k = parseNum(cell(r, cred))
				}
				if math.IsNaN(d) && math.IsNaN(k) {
					return ""
				}
				x := 0.0
				if !math.IsNaN(k) {
					x = math.Abs(k)
				}
				if !math.IsNaN(d) {
					x -= math.Abs(d)
				}
				if o.invert {
					x = -x
				}
				like := ""
				if deb >= 0 {
					like = cell(r, deb)
				}
				if like == "" && cred >= 0 {
					like = cell(r, cred)
				}
				return fmtAmt(x, like)
			}})
			continue
		}
		name := c.name
		if c.role != "" {
			name = canon[c.role]
		}
		switch {
		case c.j == invCol && o.invert:
			out = append(out, outCol{name: name, canon: c.role != "", calc: func(r []string) string {
				v := cell(r, c.j)
				x := parseNum(v)
				if math.IsNaN(x) {
					return v
				}
				return fmtAmt(-x, v)
			}})
		case dateCols[c.j]:
			out = append(out, outCol{name: name, canon: c.role != "", calc: func(r []string) string {
				v := cell(r, c.j)
				m := reDMYCell.FindStringSubmatch(v)
				if m == nil {
					return v
				}
				return m[3] + "-" + pad2(m[1]) + "-" + pad2(m[2]) + m[4]
			}})
		default:
			out = append(out, outCol{name: name, canon: c.role != "", j: c.j})
		}
	}
	canonNames := map[string]bool{}
	for _, c := range out {
		if c.canon {
			canonNames[c.name] = true
		}
	}
	for i := range out {
		if !out[i].canon && canonNames[out[i].name] {
			out[i].name += " (original)"
		}
	}
	seen := map[string]int{}
	for i := range out {
		if seen[out[i].name] > 0 {
			seen[out[i].name]++
			out[i].name += " (" + strconv.Itoa(seen[out[i].name]) + ")"
		} else {
			seen[out[i].name] = 1
		}
	}
	hs := make([]string, len(out))
	for i, c := range out {
		hs[i] = c.name
	}
	rs := make([][]string, len(rows))
	for i, r := range rows {
		row := make([]string, len(out))
		for k, c := range out {
			if c.calc != nil {
				row[k] = c.calc(r)
			} else {
				row[k] = cell(r, c.j)
			}
		}
		rs[i] = row
	}
	return hs, rs
}

func pad2(s string) string {
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

func parseNum(s string) float64 {
	if x, ok := ingest.ParseNumber(s); ok {
		return x
	}
	return math.NaN()
}

// toFixed2 is Number.prototype.toFixed(2): the nearest of the two-decimal values, an exact tie
// going away from zero (Go's formatting would round it to even). -0 prints as "0.00".
func toFixed2(x float64) string {
	if x == 0 {
		return "0.00"
	}
	sign := ""
	if x < 0 {
		sign, x = "-", -x
	}
	t := new(big.Float).SetPrec(200).SetFloat64(x)
	t.Mul(t, big.NewFloat(1000).SetPrec(200))
	if t.IsInt() {
		n, _ := t.Int(nil)
		if new(big.Int).Mod(n, big.NewInt(10)).Int64() == 5 {
			n.Add(n, big.NewInt(5)).Quo(n, big.NewInt(10))
			s := n.String()
			for len(s) < 3 {
				s = "0" + s
			}
			return sign + s[:len(s)-2] + "." + s[len(s)-2:]
		}
	}
	return sign + strconv.FormatFloat(x, 'f', 2, 64)
}

// dataset is the legacy makeDataset: typed columns, detected roles and normalised cells.
type dataset struct {
	headers []string
	rows    [][]string
	cols    []ingest.Column // Index, Name, Type (as analyzeCol)
	num     [][]float64     // per column, NaN where a cell is not a number
	ms      [][]float64     // per column, legacy date value (ms since the epoch, UTC) or NaN
	norm    [][]string      // per column, norm(cell)
	roles   ingest.Roles
}

func makeDataset(headers []string, rows [][]string) *dataset {
	ds := &dataset{headers: headers, rows: rows, cols: make([]ingest.Column, len(headers)),
		num: make([][]float64, len(headers)), ms: make([][]float64, len(headers)), norm: make([][]string, len(headers))}
	for ci, name := range headers {
		num, ms, nrm := make([]float64, len(rows)), make([]float64, len(rows)), make([]string, len(rows))
		nonEmpty, nOk, dOk := 0, 0, 0
		for i, r := range rows {
			v := ""
			if ci < len(r) {
				v = r[ci]
			}
			nrm[i], num[i], ms[i] = ingest.Norm(v), math.NaN(), math.NaN()
			if v == "" {
				continue
			}
			nonEmpty++
			if d, ok := parseDateMS(v); ok {
				ms[i] = float64(d)
				dOk++
			}
			if x, ok := ingest.ParseNumber(v); ok {
				num[i] = x
				nOk++
			}
		}
		typ := ingest.TypeText
		if nonEmpty > 0 {
			switch {
			case float64(dOk)/float64(nonEmpty) >= .9:
				typ = ingest.TypeDate
			case float64(nOk)/float64(nonEmpty) >= .9:
				typ = ingest.TypeNumber
			}
		}
		ds.cols[ci] = ingest.Column{Index: ci, Name: name, Type: typ}
		ds.num[ci], ds.ms[ci], ds.norm[ci] = num, ms, nrm
	}
	ds.roles = ingest.DetectRoles(ds.cols, rows)
	return ds
}

var (
	reISODate = regexp.MustCompile(`^(\d{4})[-./](\d{1,2})[-./](\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?)?`)
	reDMYDate = regexp.MustCompile(`^(\d{1,2})[./-](\d{1,2})[./-](\d{4})(?:[ T,]+(\d{1,2}):(\d{2})(?::(\d{2}))?)?$`)
)

// parseDateMS is the legacy parseDate: Date.UTC of the cell's date (and time), in milliseconds.
func parseDateMS(s string) (int64, bool) {
	s = ingest.JSTrim(s)
	if m := reISODate.FindStringSubmatch(s); m != nil {
		return mkDate(m[1], m[2], m[3], m[4], m[5], m[6])
	}
	if m := reDMYDate.FindStringSubmatch(s); m != nil {
		return mkDate(m[3], m[2], m[1], m[4], m[5], m[6])
	}
	return 0, false
}

// mkDate is the legacy mkDate (Date.UTC: years 0–99 mean 1900–1999; days past the month's end
// roll over into the next month).
func mkDate(y, mo, d, h, mi, se string) (int64, bool) {
	yy, _ := strconv.Atoi(y)
	mm, _ := strconv.Atoi(mo)
	dd, _ := strconv.Atoi(d)
	if mm < 1 || mm > 12 || dd < 1 || dd > 31 {
		return 0, false
	}
	hh, _ := strconv.Atoi(h)
	mn, _ := strconv.Atoi(mi)
	ss, _ := strconv.Atoi(se)
	if yy <= 99 {
		yy += 1900
	}
	return time.Date(yy, time.Month(mm), dd, hh, mn, ss, 0, time.UTC).UnixMilli(), true
}

var (
	reRefCol   = regexp.MustCompile(`(?i)referenc|^ref\b|transaction id|id transakcji|numer transakcji`)
	reTitleCol = regexp.MustCompile(`(?i)tytu|title|opis|description|memo`)
)

// keyer computes the legacy identity of a dataset's rows (the recipe shared by txKeys and the
// merged de-dupe): "r|" + reference + "|" + amount when a reference column has a value, else
// date|amount|payee|title when the dataset has date and amount roles, else the normalised row.
type keyer struct {
	ds                  *dataset
	date, amount, payee int
	ref, title          int
	rowKey              func(i int) string
}

func newKeyer(ds *dataset, rowKey func(i int) string) keyer {
	k := keyer{ds: ds, date: ds.roles.BookedOn, amount: ds.roles.Amount, payee: ds.roles.Payee, ref: -1, title: -1, rowKey: rowKey}
	for ci, name := range ds.headers {
		if k.ref < 0 && reRefCol.MatchString(name) {
			k.ref = ci
		}
		if k.title < 0 && ci != k.payee && reTitleCol.MatchString(name) {
			k.title = ci
		}
	}
	return k
}

func (k keyer) key(i int) string {
	ds := k.ds
	amt := ""
	if k.amount >= 0 && !math.IsNaN(ds.num[k.amount][i]) {
		amt = toFixed2(ds.num[k.amount][i])
	}
	if k.ref >= 0 && ds.rows[i][k.ref] != "" {
		return "r|" + ingest.Norm(ds.rows[i][k.ref]) + "|" + amt
	}
	if k.date >= 0 && k.amount >= 0 {
		d := "NaN"
		if v := ds.ms[k.date][i]; !math.IsNaN(v) {
			d = strconv.FormatInt(int64(v), 10)
		}
		p, t := "", ""
		if k.payee >= 0 {
			p = ingest.Collapse(ds.norm[k.payee][i])
		}
		if k.title >= 0 {
			t = ingest.Collapse(ds.norm[k.title][i])
		}
		return d + "|" + amt + "|" + p + "|" + t
	}
	return k.rowKey(i)
}

// numbered appends the legacy "#n" suffix that keeps identical twins in one dataset apart.
func numbered(keys []string) []string {
	seen := map[string]int{}
	out := make([]string, len(keys))
	for i, k := range keys {
		seen[k]++
		if n := seen[k]; n > 1 {
			out[i] = k + "#" + strconv.Itoa(n)
		} else {
			out[i] = k
		}
	}
	return out
}

// groupColumn is the name of the legacy group column of a dataset.
func groupColumn(headers []string) string {
	for _, h := range headers {
		if h == "Grouping" {
			return "App grouping"
		}
	}
	return "Grouping"
}

// fileKeys is txKeys of a dataset. The legacy dataset also had its group column (empty here, so
// it changes no role or key); the normalised-row branch skips it and the "Source file" column.
func fileKeys(headers []string, rows [][]string) []string {
	ds := makeDataset(headers, rows)
	skip := map[string]bool{groupColumn(headers): true, sourceColumn: true}
	k := newKeyer(ds, func(i int) string {
		var parts []string
		for ci, h := range headers {
			if !skip[h] {
				parts = append(parts, ds.norm[ci][i])
			}
		}
		return strings.Join(parts, "|")
	})
	keys := make([]string, len(rows))
	for i := range keys {
		keys[i] = k.key(i)
	}
	return numbered(keys)
}

// RowRef is one row of one legacy file: File is the index in the migration's file list.
type RowRef struct {
	File, Row int
}

// merged is the legacy "All data" view of several files: the de-dupe keys of every row (no #n),
// the rows kept after the multiset rule and overrides, and their txKeys.
type merged struct {
	keys   map[string][]RowRef // de-dupe key → every row with it (dupPick / dupKeep use these keys)
	kept   []RowRef            // rows of the merged view, in its order
	txKeys []string            // txKeys of the merged view, aligned with kept
}

const sourceColumn = "Source file"

// mergeFiles is the legacy getDataset('merged'). files are in the legacy order (by added); fileIDs
// are the legacy file ids (for dupPick); files with an error are left out, as the legacy did.
func mergeFiles(files []legacyFile, names, fileIDs []string, dupPick map[string]string, dupKeep map[string]bool) merged {
	var ok []int
	for fi, f := range files {
		if f.err == nil {
			ok = append(ok, fi)
		}
	}
	var headers []string
	idx := map[string]int{}
	for _, fi := range ok {
		for _, h := range files[fi].headers {
			if _, seen := idx[h]; !seen {
				idx[h] = len(headers)
				headers = append(headers, h)
			}
		}
	}
	headers = append(headers, sourceColumn)
	var rows [][]string
	var src []RowRef
	for _, fi := range ok {
		f := files[fi]
		for ri, r := range f.rows {
			o := make([]string, len(headers))
			for k, v := range r {
				if k < len(f.headers) {
					o[idx[f.headers[k]]] = v
				}
			}
			o[len(headers)-1] = names[fi]
			rows = append(rows, o)
			src = append(src, RowRef{File: fi, Row: ri})
		}
	}
	tmp := makeDataset(headers, rows)
	k := newKeyer(tmp, func(i int) string {
		// norm of the whole row joined with U+0001, without the last field (the source file)
		parts := make([]string, len(headers)-1)
		for ci := range parts {
			parts[ci] = tmp.norm[ci][i]
		}
		return strings.Join(parts, "\u0001")
	})
	keys := make([]string, len(rows))
	m := merged{keys: map[string][]RowRef{}}
	cnt := map[string]map[int]int{} // key → file → count
	var order []string
	for i := range rows {
		keys[i] = k.key(i)
		m.keys[keys[i]] = append(m.keys[keys[i]], src[i])
		if cnt[keys[i]] == nil {
			cnt[keys[i]] = map[int]int{}
			order = append(order, keys[i])
		}
		cnt[keys[i]][src[i].File]++
	}
	owner, kept := map[string]int{}, map[string]int{}
	for _, key := range order {
		byFile := cnt[key]
		o := -1
		if id, set := dupPick[key]; set {
			for fi := range fileIDs {
				if fileIDs[fi] == id && byFile[fi] > 0 {
					o = fi
					break
				}
			}
		}
		if o < 0 {
			o = math.MaxInt
			for fi := range byFile {
				o = min(o, fi)
			}
		}
		owner[key], kept[key] = o, byFile[o]
	}
	occ := map[RowRef]int{} // (file, key) occurrences, keyed by file and a key index
	keyIdx := map[string]int{}
	for i, key := range order {
		keyIdx[key] = i
	}
	var out [][]string
	for i, r := range rows {
		key, f := keys[i], src[i].File
		ok2 := RowRef{File: f, Row: keyIdx[key]}
		occ[ok2]++
		n := occ[ok2]
		mine := f == owner[key]
		if dupKeep[key] || mine || n > kept[key] {
			if !mine && !dupKeep[key] {
				kept[key] = n
			}
			m.kept = append(m.kept, src[i])
			out = append(out, r)
		}
	}
	m.txKeys = fileKeys(headers, out)
	return m
}
