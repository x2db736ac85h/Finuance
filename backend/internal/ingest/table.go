package ingest

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// HeaderMode selects how Shape finds the header row.
type HeaderMode int

// Header modes. HeaderAuto is the default.
const (
	HeaderAuto HeaderMode = iota // detect it (legacy "automatic")
	HeaderNone                   // the file has no header: every record is data, columns are "Column N"
	HeaderRow                    // ShapeOptions.HeaderRow is the 1-based record number of the header
)

// ShapeOptions are the per-import table options (legacy e.cfg header/skip/dropShort and strip).
type ShapeOptions struct {
	Header    HeaderMode
	HeaderRow int  // with HeaderRow: 1-based record number
	SkipRows  int  // with HeaderNone: 1-based record where data starts (0 or 1 = the first)
	KeepShort bool // disable the footer drop (legacy dropShort:false)
	KeepQuote bool // keep a leading ' in data cells (legacy strip:false)
}

// Table is a shaped file: unique, non-empty headers and rectangular rows of trimmed cells.
type Table struct {
	Headers  []string
	Rows     [][]string
	HeaderAt int // 1-based record number of the header row; 0 when there is none
	Dropped  int // footer/summary rows removed by the short-row rule
}

// Shaping errors.
var (
	ErrEmptyFile           = errors.New("empty file")
	ErrHeaderRowOutOfRange = errors.New("header row is beyond the end of the file")
)

// Shape turns tokenized records into a Table (legacy `buildTable`, v7). The steps, in order:
//
//  1. Find the header (see findHeader for auto mode) and the data records after it; blank records
//     (every cell whitespace) are dropped.
//  2. Footer drop: when the table has ≥ 4 columns, data rows with at most half that many fields
//     are summary lines ("Saldo końcowe;1234,56") and are removed.
//  3. Pad ragged rows to the widest row, trim every cell, strip one leading ' from data cells
//     (Excel's "treat as text" marker on account numbers).
//  4. Drop columns that have no header name and no data (trailing delimiters).
//  5. Name blank headers "Column N" and de-duplicate names: "Kwota", "Kwota (2)", …
func Shape(raw [][]string, o ShapeOptions) (*Table, error) {
	var (
		headers  []string
		data     [][]string
		headerAt int
	)
	switch o.Header {
	case HeaderNone:
		skip := max(o.SkipRows, 1)
		if skip-1 < len(raw) {
			data = nonBlank(raw[skip-1:])
		}
		if len(data) == 0 {
			return nil, ErrEmptyFile
		}
	case HeaderRow:
		if o.HeaderRow < 1 || o.HeaderRow > len(raw) {
			return nil, fmt.Errorf("%w: row %d of %d", ErrHeaderRowOutOfRange, o.HeaderRow, len(raw))
		}
		headers = trimAll(raw[o.HeaderRow-1])
		data = nonBlank(raw[o.HeaderRow:])
		headerAt = o.HeaderRow
	default:
		rows, rowAt := nonBlankIndexed(raw)
		if len(rows) == 0 {
			return nil, ErrEmptyFile
		}
		h := findHeader(rows)
		headers = trimAll(rows[h])
		data = rows[h+1:]
		headerAt = rowAt[h] + 1
	}

	// Footer drop. The reference width is the header's, or — without a header — the most common
	// row width (ties: the narrower one, as the legacy sort keeps ascending key order).
	dropped := 0
	ref := len(headers)
	if ref == 0 {
		ref = modalWidth(data, false)
	}
	if !o.KeepShort && ref >= 4 {
		kept := data[:0:0]
		for _, r := range data {
			if float64(len(r)) > float64(ref)/2 {
				kept = append(kept, r)
			}
		}
		dropped = len(data) - len(kept)
		data = kept
	}

	width := len(headers)
	for _, r := range data {
		width = max(width, len(r))
	}
	cells := make([][]string, len(data))
	for i, r := range data {
		row := r // rows that already have the full width are trimmed in place (they are ours)
		if len(r) != width {
			row = make([]string, width)
		}
		for k := range row {
			if k < len(r) {
				v := jsTrim(r[k])
				if !o.KeepQuote && len(v) > 0 && v[0] == '\'' {
					v = v[1:]
				}
				row[k] = v
			}
		}
		cells[i] = row
	}
	for len(headers) < width {
		headers = append(headers, "")
	}

	// Drop columns with neither a header nor any data (the empty columns trailing ";;" create).
	keep := make([]bool, width)
	anyDropped := false
	for k := range keep {
		keep[k] = headers[k] != ""
		for _, r := range cells {
			if keep[k] {
				break
			}
			keep[k] = r[k] != ""
		}
		anyDropped = anyDropped || !keep[k]
	}
	if anyDropped {
		headers = pick(headers, keep)
		for i := range cells {
			cells[i] = pick(cells[i], keep)
		}
	}

	return &Table{Headers: uniqueNames(headers), Rows: cells, HeaderAt: headerAt, Dropped: dropped}, nil
}

// findHeader is the automatic header rule. It skips bank preambles:
//
//  1. the modal field count among non-blank records (ties: the wider count);
//  2. among the first 50 records, the first one with that count whose number of non-empty cells is
//     ≥ ceil(0.8 × the largest non-empty count among those modal-width records). This skips
//     padded preamble lines such as "Bank export;;;" that have the right width but are mostly
//     empty;
//  3. record 0 if nothing qualifies.
//
// Known limitation, ported on purpose (fix planned for Phase 9): when data rows have more filled
// cells than the header (PKO BP spills descriptions into unnamed columns) or more fields (mBank
// rows end in ";;"), a data row or a preamble line is chosen. See ALLOWED_DIFFS.md row 4.
func findHeader(rows [][]string) int {
	mode := modalWidth(rows, true)
	top := rows[:min(len(rows), 50)]
	maxFilled := 0
	for _, r := range top {
		if len(r) == mode {
			maxFilled = max(maxFilled, filled(r))
		}
	}
	need := int(math.Ceil(float64(maxFilled) * 0.8))
	for i, r := range top {
		if len(r) == mode && filled(r) >= need {
			return i
		}
	}
	return 0
}

// modalWidth returns the most common record length. preferWide breaks ties towards the longer
// length (header detection); otherwise towards the shorter one (footer reference width), matching
// the two different sorts in the legacy code.
func modalWidth(rows [][]string, preferWide bool) int {
	freq := map[int]int{}
	for _, r := range rows {
		freq[len(r)]++
	}
	widths := make([]int, 0, len(freq))
	for w := range freq {
		widths = append(widths, w)
	}
	sort.Ints(widths)
	sort.SliceStable(widths, func(i, j int) bool {
		if freq[widths[i]] != freq[widths[j]] {
			return freq[widths[i]] > freq[widths[j]]
		}
		if preferWide {
			return widths[i] > widths[j]
		}
		return false
	})
	if len(widths) == 0 {
		return 0
	}
	return widths[0]
}

func filled(r []string) int {
	n := 0
	for _, v := range r {
		if hasNonSpace(v) {
			n++
		}
	}
	return n
}

func nonBlank(rows [][]string) [][]string {
	out, _ := nonBlankIndexed(rows)
	return out
}

// nonBlankIndexed drops blank records and remembers where each kept record was in rows.
func nonBlankIndexed(rows [][]string) ([][]string, []int) {
	out := make([][]string, 0, len(rows))
	at := make([]int, 0, len(rows))
	for i, r := range rows {
		if filled(r) > 0 {
			out = append(out, r)
			at = append(at, i)
		}
	}
	return out, at
}

func trimAll(r []string) []string {
	out := make([]string, len(r))
	for i, v := range r {
		out[i] = jsTrim(v)
	}
	return out
}

func pick(r []string, keep []bool) []string {
	out := make([]string, 0, len(r))
	for k, v := range r {
		if keep[k] {
			out = append(out, v)
		}
	}
	return out
}

// uniqueNames names blank headers "Column N" (1-based position) and suffixes repeats: the second
// "Kwota" becomes "Kwota (2)". Like the legacy code it does not re-check a generated name against
// later headers, so "A (2)", "A", "A" yields "A (2)" twice; that is kept for parity.
func uniqueNames(headers []string) []string {
	seen := map[string]int{}
	out := make([]string, len(headers))
	for k, h := range headers {
		name := h
		if name == "" {
			name = "Column " + strconv.Itoa(k+1)
		}
		if n := seen[name]; n > 0 {
			seen[name] = n + 1
			name = name + " (" + strconv.Itoa(n+1) + ")"
		} else {
			seen[name] = 1
		}
		out[k] = name
	}
	return out
}
