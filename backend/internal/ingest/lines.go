package ingest

import (
	"context"
)

// LineStatus says what Shape did with one record (GET /imports/{id}/lines, the raw-lines panel
// of the import preview, port of the legacy v7 import window).
type LineStatus string

// Line statuses.
const (
	LineHeader        LineStatus = "header"         // the header row
	LinePreamble      LineStatus = "preamble"       // before the header (bank name, period, …)
	LineData          LineStatus = "data"           // a data row of the table
	LineFooterDropped LineStatus = "footer_dropped" // a short row removed as a summary line
	LineBlank         LineStatus = "blank"          // every cell is whitespace
	LineSkipped       LineStatus = "skipped"        // before SkipRows in a headerless file
)

// Line is one decoded record with its status. Record is 1-based, as the UI shows it and as
// ShapeOptions.HeaderRow counts.
type Line struct {
	Record int
	Fields []string
	Status LineStatus
}

// LinesResult is the decoded file's first records plus what was detected.
type LinesResult struct {
	Encoding  Encoding
	Delimiter rune
	Total     int // records in the file
	Lines     []Line
}

// Lines decodes and tokenizes raw like Parse and classifies the first limit records exactly as
// Shape treats them, so the preview can show which line is the header, which are preamble and which
// summary lines were dropped. The rules are Shape's, record by record; TestLinesAgreeWithShape
// checks the two never disagree.
func Lines(ctx context.Context, raw []byte, o Options, limit int) (*LinesResult, error) {
	text, enc, err := Decode(raw, o.Encoding)
	if err != nil {
		return nil, err
	}
	delim := o.Delimiter
	if delim == 0 {
		delim = DetectDelimiter(text)
	}
	maxRows, maxCols := o.MaxRows, o.MaxColumns
	if maxRows <= 0 {
		maxRows = DefaultMaxRows
	}
	if maxCols <= 0 {
		maxCols = DefaultMaxColumns
	}
	records, err := tokenize(ctx, text, delim, maxRows, maxCols)
	if err != nil {
		return nil, err
	}
	status := classify(records, o.Shape)
	n := min(max(limit, 0), len(records))
	out := &LinesResult{Encoding: enc, Delimiter: delim, Total: len(records), Lines: make([]Line, n)}
	for i := range n {
		out.Lines[i] = Line{Record: i + 1, Fields: records[i], Status: status[i]}
	}
	return out, nil
}

// classify returns the status of every record under o (the same decisions as shape).
func classify(raw [][]string, o ShapeOptions) []LineStatus {
	st := make([]LineStatus, len(raw))
	blank := func(r []string) bool { return filled(r) == 0 }
	var data []int // record indexes of the data rows, before the footer drop
	headerLen := 0
	switch o.Header {
	case HeaderNone:
		skip := max(o.SkipRows, 1)
		for i, r := range raw {
			switch {
			case i < skip-1:
				st[i] = LineSkipped
			case blank(r):
				st[i] = LineBlank
			default:
				data = append(data, i)
			}
		}
	case HeaderRow:
		h := o.HeaderRow - 1
		for i, r := range raw {
			switch {
			case i < h:
				st[i] = LinePreamble
			case i == h:
				st[i] = LineHeader
				headerLen = len(r)
			case blank(r):
				st[i] = LineBlank
			default:
				data = append(data, i)
			}
		}
	default:
		rows, rowAt := nonBlankIndexed(raw)
		h := -1
		if len(rows) > 0 {
			h = rowAt[findHeader(rows)]
			headerLen = len(raw[h])
		}
		for i, r := range raw {
			switch {
			case blank(r):
				st[i] = LineBlank
			case i < h:
				st[i] = LinePreamble
			case i == h:
				st[i] = LineHeader
			default:
				data = append(data, i)
			}
		}
	}
	ref := headerLen
	if ref == 0 { // no header: the most common row width, as in shape
		rows := make([][]string, len(data))
		for k, i := range data {
			rows[k] = raw[i]
		}
		ref = modalWidth(rows, false)
	}
	for _, i := range data {
		st[i] = LineData
		if !o.KeepShort && ref >= 4 && float64(len(raw[i])) <= float64(ref)/2 {
			st[i] = LineFooterDropped
		}
	}
	return st
}
