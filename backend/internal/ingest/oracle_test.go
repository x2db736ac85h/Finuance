package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// oracleIngest is one testdata/oracle/ingest*/…json file: what the legacy pipeline produced.
type oracleIngest struct {
	Fixture   string     `json:"fixture"`
	Encoding  string     `json:"encoding"`
	Delimiter string     `json:"delimiter"`
	Error     *string    `json:"error"`
	HeaderAt  int        `json:"headerAt"`
	Dropped   int        `json:"dropped"`
	Headers   []string   `json:"headers"`
	Rows      [][]string `json:"rows"`
	Columns   []struct {
		Name    string     `json:"name"`
		Type    string     `json:"type"`
		Numbers []*float64 `json:"numbers"`
		Dates   []*string  `json:"dates"`
	} `json:"columns"`
	Roles   map[string]string `json:"roles"`
	RowNorm []string          `json:"rowNorm"`
	Cfg     map[string]any    `json:"cfg"`
	Variant string            `json:"variant"`
}

func loadOracleIngest(t *testing.T, dir string) map[string]oracleIngest {
	t.Helper()
	out := map[string]oracleIngest{}
	root := filepath.Join(oracleDir, dir)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var o oracleIngest
		if err := json.Unmarshal(b, &o); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = o
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// legacyView renders our Result in the oracle's vocabulary so cmp.Diff can compare them. Dates are
// compared as calendar dates; cells whose date differs for an allowed reason (dateDiffReason) are
// replaced by the legacy value after checking the reason.
type legacyView struct {
	Encoding, Delimiter string
	Error               string
	HeaderAt, Dropped   int
	Headers             []string
	Rows                [][]string
	Types               []string
	Numbers             [][]string
	Dates               [][]string
	Roles               map[string]string
	RowNorm             []string
}

func viewOf(res *Result, err error) legacyView {
	v := legacyView{Encoding: res.Encoding.Label(), Delimiter: string(res.Delimiter)}
	if err != nil {
		v.Error = err.Error()
		return v
	}
	t := res.Table
	v.HeaderAt, v.Dropped, v.Headers, v.Rows = t.HeaderAt, t.Dropped, t.Headers, t.Rows
	for _, c := range res.Columns {
		v.Types = append(v.Types, string(c.Type))
		nums, dates := make([]string, len(c.Numbers)), make([]string, len(c.Dates))
		for i, x := range c.Numbers {
			nums[i] = fmtNum(x)
		}
		for i, d := range c.Dates {
			dates[i] = d.String()
		}
		v.Numbers, v.Dates = append(v.Numbers, nums), append(v.Dates, dates)
	}
	name := func(i int) string {
		if i < 0 {
			return ""
		}
		return t.Headers[i]
	}
	r := res.Roles
	v.Roles = map[string]string{"date": name(r.BookedOn), "amount": name(r.Amount), "category": name(r.Category),
		"payee": name(r.Payee), "balance": name(r.Balance), "currency": r.Currency}
	for _, row := range t.Rows {
		v.RowNorm = append(v.RowNorm, Norm(strings.Join(row, "\u0001")))
	}
	return v
}

func fmtNum(x float64) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	return fmt.Sprint(x)
}

func legacyViewOf(t *testing.T, o oracleIngest, ours legacyView) legacyView {
	v := legacyView{Encoding: o.Encoding, Delimiter: o.Delimiter, HeaderAt: o.HeaderAt, Dropped: o.Dropped,
		Headers: o.Headers, Rows: o.Rows, Roles: o.Roles, RowNorm: o.RowNorm}
	if o.Error != nil {
		v.Error = map[string]string{"Empty file": ErrEmptyFile.Error()}[*o.Error]
		v.HeaderAt, v.Dropped, v.Headers, v.Rows, v.Roles, v.RowNorm = 0, 0, nil, nil, nil, nil
		return v
	}
	for ci, c := range o.Columns {
		v.Types = append(v.Types, c.Type)
		nums, dates := make([]string, len(c.Numbers)), make([]string, len(c.Dates))
		for i, x := range c.Numbers {
			nums[i] = "NaN"
			if x != nil {
				nums[i] = fmtNum(*x)
			}
		}
		for i, d := range c.Dates {
			if d != nil {
				dates[i] = (*d)[:10]
			}
			// An intentional date difference (ALLOWED_DIFFS.md rows 1, 9, 10): accept ours.
			if ci < len(ours.Dates) && i < len(ours.Dates[ci]) && dates[i] != ours.Dates[ci][i] &&
				dateDiffReason(o.Rows[i][ci], ours.Dates[ci][i]) != "" {
				dates[i] = ours.Dates[ci][i]
			}
		}
		v.Numbers, v.Dates = append(v.Numbers, nums), append(v.Dates, dates)
	}
	return v
}

// TestOracleIngest: every fixture through Parse with default options equals the legacy pipeline.
func TestOracleIngest(t *testing.T) {
	cases := loadOracleIngest(t, "ingest")
	if len(cases) < 60 {
		t.Fatalf("only %d oracle ingest files", len(cases))
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, o.Fixture))
			if err != nil {
				t.Fatal(err)
			}
			res, err := Parse(context.Background(), raw, Options{})
			if err != nil && !errors.Is(err, ErrEmptyFile) {
				t.Fatal(err)
			}
			ours := viewOf(res, err)
			if diff := cmp.Diff(legacyViewOf(t, o, ours), ours, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s differs from the legacy (-legacy +ours):\n%s", o.Fixture, diff)
			}
		})
	}
}
