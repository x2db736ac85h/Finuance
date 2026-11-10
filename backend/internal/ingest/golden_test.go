package ingest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/ingest/*.json")

var goldenDir = filepath.Join("..", "..", "..", "testdata", "golden", "ingest")

// golden is the reviewed, human-readable result of parsing and staging one fixture with the
// proposed mapping: everything a preview would show.
type golden struct {
	Fixture   string            `json:"fixture"`
	Encoding  string            `json:"encoding"`
	Delimiter string            `json:"delimiter"`
	Error     string            `json:"error,omitempty"`
	HeaderAt  int               `json:"headerAt"`
	Dropped   int               `json:"dropped"`
	Headers   []string          `json:"headers"`
	Types     []string          `json:"types"`
	Roles     map[string]string `json:"roles"`
	Currency  string            `json:"currency,omitempty"`
	Signature string            `json:"headerSignature,omitempty"`
	Stats     *Stats            `json:"stats,omitempty"`
	Rows      []goldenRow       `json:"rows,omitempty"`
}

type goldenRow struct {
	StagedRow
	Fingerprint string `json:"fingerprint,omitempty"`
}

func goldenOf(fixture string, raw []byte) golden {
	res, err := Parse(context.Background(), raw, Options{})
	g := golden{Fixture: fixture, Encoding: string(res.Encoding), Delimiter: string(res.Delimiter)}
	if err != nil {
		g.Error = err.Error()
		return g
	}
	t := res.Table
	g.HeaderAt, g.Dropped, g.Headers, g.Currency = t.HeaderAt, t.Dropped, t.Headers, res.Roles.Currency
	sig := HeaderSignature(t.Headers, res.Delimiter)
	g.Signature = hex.EncodeToString(sig[:])
	for _, c := range res.Columns {
		g.Types = append(g.Types, string(c.Type))
	}
	g.Roles = map[string]string{}
	r := res.Roles
	for name, i := range map[string]int{"bookedOn": r.BookedOn, "amount": r.Amount, "payee": r.Payee, "description": r.Description,
		"reference": r.Reference, "category": r.Category, "currency": r.CurrencyColumn, "valueOn": r.ValueOn, "txType": r.TxType,
		"counterpartyAccount": r.CounterpartyAccount, "ownAccount": r.OwnAccount, "balance": r.Balance} {
		if i >= 0 {
			g.Roles[name] = t.Headers[i]
		}
	}
	rows, st, _ := Stage(context.Background(), t, MappingFromRoles(r), StageOptions{ImportCurrency: r.Currency})
	g.Stats = &st
	for i := range rows {
		gr := goldenRow{StagedRow: rows[i]}
		if rows[i].Error == "" {
			gr.Fingerprint = hex.EncodeToString(rows[i].Fingerprint[:])
		}
		g.Rows = append(g.Rows, gr)
	}
	return g
}

// TestGolden compares every fixture's full result with its reviewed golden file. Run
// `go test ./internal/ingest -run Golden -update` after an intended change and review the diff.
func TestGolden(t *testing.T) {
	var fixtures []string
	for _, dir := range []string{"synthetic"} {
		err := filepath.WalkDir(filepath.Join(fixturesDir, dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".csv") {
				rel, _ := filepath.Rel(fixturesDir, p)
				fixtures = append(fixtures, filepath.ToSlash(rel))
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, fx := range fixtures {
		t.Run(fx, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, fx))
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(goldenOf(fx, raw), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(goldenDir, strings.TrimSuffix(fx, ".csv")+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("missing golden file %s; run with -update", path)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s changed; run `go test ./internal/ingest -run Golden -update` and review the diff", path)
			}
		})
	}
}
