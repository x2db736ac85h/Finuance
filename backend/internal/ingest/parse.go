package ingest

import (
	"context"
	"errors"
	"fmt"
)

// Limits on one import file (PLAN §8.1, malicious uploads).
const (
	DefaultMaxRows    = 200_000
	DefaultMaxColumns = 200
	ctxCheckEvery     = 10_000 // rows between cancellation checks
)

// Errors from Parse besides ErrEmptyFile and ErrHeaderRowOutOfRange.
var (
	ErrTooManyRows    = errors.New("too many rows")
	ErrTooManyColumns = errors.New("too many columns")
)

// Options control parsing. The zero value is the legacy default: auto encoding, auto delimiter,
// automatic header, footer drop and ' strip on, day-first dates.
type Options struct {
	Encoding   Encoding // "" or EncodingAuto
	Delimiter  rune     // 0 = detect
	Shape      ShapeOptions
	DateOrder  DateOrder
	MaxRows    int // 0 = DefaultMaxRows
	MaxColumns int // 0 = DefaultMaxColumns
}

// Result is a parsed file: the shaped table, its typed columns and the detected roles, plus what
// was used to read it (the preview shows encoding and delimiter so the user can override them).
type Result struct {
	Encoding  Encoding
	Delimiter rune
	Table     *Table
	Columns   []Column
	Roles     Roles
}

// Parse runs the pipeline up to role detection (see the package doc). Mapping the columns and
// staging canonical rows is a separate step (Stage), because the mapping comes from the user.
func Parse(ctx context.Context, raw []byte, o Options) (*Result, error) {
	maxRows, maxCols := o.MaxRows, o.MaxColumns
	if maxRows <= 0 {
		maxRows = DefaultMaxRows
	}
	if maxCols <= 0 {
		maxCols = DefaultMaxColumns
	}
	text, enc, err := Decode(raw, o.Encoding)
	if err != nil {
		return nil, err
	}
	delim := o.Delimiter
	if delim == 0 {
		delim = DetectDelimiter(text)
	}
	res := &Result{Encoding: enc, Delimiter: delim}
	records, err := tokenize(ctx, text, delim, maxRows, maxCols)
	if err != nil {
		return res, err
	}
	if res.Table, err = Shape(records, o.Shape); err != nil {
		return res, err
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	res.Columns = AnalyzeTable(res.Table, o.DateOrder)
	res.Roles = DetectRoles(res.Columns, res.Table.Rows)
	return res, nil
}

// tokenize is Tokenize with the file limits and cancellation checks. Blank records count towards
// the row limit too: a file of 300k empty lines is rejected rather than shaped.
func tokenize(ctx context.Context, text string, delim rune, maxRows, maxCols int) ([][]string, error) {
	var outErr error
	n := 0
	records := tokenizeEach(text, delim, func(rec []string) bool {
		n++
		switch {
		case len(rec) > maxCols:
			outErr = fmt.Errorf("%w: record %d has %d fields (limit %d)", ErrTooManyColumns, n, len(rec), maxCols)
		case n > maxRows:
			outErr = fmt.Errorf("%w: more than %d records", ErrTooManyRows, maxRows)
		case n%ctxCheckEvery == 0:
			outErr = ctx.Err()
		}
		return outErr == nil
	})
	return records, outErr
}
