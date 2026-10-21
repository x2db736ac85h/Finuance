package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLinesAgreeWithShape runs Lines and Parse on every committed fixture in each header mode and
// checks they agree: the header record, the number of data rows and of dropped footer rows.
func TestLinesAgreeWithShape(t *testing.T) {
	var files []string
	_ = filepath.WalkDir(filepath.Join("..", "..", "..", "testdata", "fixtures"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".csv") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) < 50 {
		t.Fatalf("found only %d fixtures", len(files))
	}
	modes := []ShapeOptions{{}, {KeepShort: true}, {Header: HeaderRow, HeaderRow: 2}, {Header: HeaderNone, SkipRows: 3}}
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, so := range modes {
			res, perr := Parse(context.Background(), raw, Options{Shape: so})
			lines, lerr := Lines(context.Background(), raw, Options{Shape: so}, 1<<30)
			if lerr != nil {
				if perr == nil {
					t.Errorf("%s %+v: Lines failed (%v) but Parse didn't", f, so, lerr)
				}
				continue
			}
			if perr != nil { // empty file, header row out of range: nothing to compare
				continue
			}
			header, data, dropped := 0, 0, 0
			for _, l := range lines.Lines {
				switch l.Status {
				case LineHeader:
					header = l.Record
				case LineData:
					data++
				case LineFooterDropped:
					dropped++
				}
			}
			if header != res.Table.HeaderAt || data != len(res.Table.Rows) || dropped != res.Table.Dropped {
				t.Errorf("%s %+v: Lines header %d / data %d / dropped %d; Shape %d / %d / %d", filepath.Base(f), so,
					header, data, dropped, res.Table.HeaderAt, len(res.Table.Rows), res.Table.Dropped)
			}
			checked++
		}
	}
	t.Logf("compared %d (file, mode) pairs", checked)
}

func TestLinesStatuses(t *testing.T) {
	raw := []byte("Bank XYZ;;;\nOkres: 2025;;;\n\nData;Opis;Kwota;Saldo\n02.01.2025;a;-1,00;10\n\n03.01.2025;b;-2,00;8\nSuma;3\n")
	got, err := Lines(context.Background(), raw, Options{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []LineStatus{LinePreamble, LinePreamble, LineBlank, LineHeader, LineData}
	if got.Total != 8 || len(got.Lines) != 5 || got.Delimiter != ';' {
		t.Fatalf("lines = %+v", got)
	}
	for i, w := range want {
		if got.Lines[i].Status != w || got.Lines[i].Record != i+1 {
			t.Errorf("record %d: %s, want %s", i+1, got.Lines[i].Status, w)
		}
	}
	all, _ := Lines(context.Background(), raw, Options{}, 100)
	if all.Lines[7].Status != LineFooterDropped || all.Lines[5].Status != LineBlank {
		t.Errorf("tail = %+v", all.Lines[5:])
	}
	none, _ := Lines(context.Background(), raw, Options{Shape: ShapeOptions{Header: HeaderNone, SkipRows: 5}}, 100)
	if none.Lines[3].Status != LineSkipped || none.Lines[4].Status != LineData {
		t.Errorf("headerless: %+v", none.Lines[:5])
	}
	if l, _ := Lines(context.Background(), raw, Options{}, -1); len(l.Lines) != 0 {
		t.Error("negative limit")
	}
}
