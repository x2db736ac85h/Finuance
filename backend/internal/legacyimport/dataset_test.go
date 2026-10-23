package legacyimport

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	oracleDir   = filepath.Join("..", "..", "..", "testdata", "oracle")
	fixturesDir = filepath.Join("..", "..", "..", "testdata", "fixtures")
)

type oracleTable struct {
	Fixture string            `json:"fixture"`
	Error   *string           `json:"error"`
	Headers []string          `json:"headers"`
	Rows    [][]string        `json:"rows"`
	Roles   map[string]string `json:"roles"`
	Cfg     FileCfg           `json:"cfg"`
	Options ParseOpts         `json:"options"`
}

// TestLegacyTablesMatchOracle rebuilds the legacy table, after applyMapping, of every recorded file
// and setting, with the legacy header rule, and compares it and its detected roles with what the
// legacy app produced.
func TestLegacyTablesMatchOracle(t *testing.T) {
	n := 0
	for _, dir := range []string{"ingest", "ingest-cfg"} {
		err := filepath.WalkDir(filepath.Join(oracleDir, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			n++
			var o oracleTable
			readJSON(t, path, &o)
			t.Run(strings.TrimPrefix(path, oracleDir), func(t *testing.T) {
				raw, err := os.ReadFile(filepath.Join(fixturesDir, o.Fixture))
				if err != nil {
					t.Fatal(err)
				}
				f := readLegacy(context.Background(), raw, optsOf(o.Cfg, o.Options))
				if o.Error != nil {
					if f.err == nil {
						t.Fatalf("legacy error %q, we read %d rows", *o.Error, len(f.rows))
					}
					return
				}
				if f.err != nil {
					t.Fatal(f.err)
				}
				if diff := cmp.Diff(o.Headers, f.headers, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("headers (-legacy +ours):\n%s", diff)
				}
				if diff := cmp.Diff(o.Rows, f.rows, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("rows (-legacy +ours):\n%s", diff)
				}
				ds := makeDataset(f.headers, f.rows)
				name := func(i int) string {
					if i < 0 {
						return ""
					}
					return f.headers[i]
				}
				got := map[string]string{"date": name(ds.roles.BookedOn), "amount": name(ds.roles.Amount), "category": name(ds.roles.Category),
					"payee": name(ds.roles.Payee), "balance": name(ds.roles.Balance), "currency": ds.roles.Currency}
				if diff := cmp.Diff(o.Roles, got); diff != "" {
					t.Errorf("roles (-legacy +ours):\n%s", diff)
				}
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n < 80 {
		t.Fatalf("only %d recorded tables", n)
	}
}

// TestTxKeysMatchOracle recomputes the legacy group keys (txKeys) of each file and of the merged
// view of every recorded file set.
func TestTxKeysMatchOracle(t *testing.T) {
	sets := map[string][]string{
		"overlap-A-B":   {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv"},
		"overlap-A-C":   {"overlap/A_jan-mar.csv", "overlap/C_q1_again.csv"},
		"overlap-C-A":   {"overlap/C_q1_again.csv", "overlap/A_jan-mar.csv"},
		"overlap-A-B-C": {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv", "overlap/C_q1_again.csv"},
		"same-name":     {"same-name/a/export.csv", "same-name/b/export.csv"},
	}
	for name, paths := range sets {
		t.Run(name, func(t *testing.T) {
			var o struct {
				Datasets []struct {
					Dataset string   `json:"dataset"`
					Keys    []string `json:"keys"`
				} `json:"datasets"`
			}
			readJSON(t, filepath.Join(oracleDir, "txkeys", name+".json"), &o)
			files, names, ids := readSet(t, paths)
			for fi, f := range files {
				if diff := cmp.Diff(o.Datasets[fi].Keys, fileKeys(f.headers, f.rows)); diff != "" {
					t.Errorf("%s (-legacy +ours):\n%s", names[fi], diff)
				}
			}
			m := mergeFiles(files, names, ids, nil, nil)
			if diff := cmp.Diff(o.Datasets[len(files)].Keys, m.txKeys); diff != "" {
				t.Errorf("merged (-legacy +ours):\n%s", diff)
			}
		})
	}
}

// TestMergeMatchesOracleDedupe replays the merged view with the recorded overrides: the rows it
// keeps must be the legacy's, with and without dupPick / dupKeep.
func TestMergeMatchesOracleDedupe(t *testing.T) {
	sets := map[string][]string{
		"overlap-A-B":           {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv"},
		"overlap-A-B-C":         {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv", "overlap/C_q1_again.csv"},
		"dup-versions":          {"dup-versions/v1_jan-feb.csv", "dup-versions/v2_jan-mar.csv"},
		"dup-versions-reversed": {"dup-versions/v2_jan-mar.csv", "dup-versions/v1_jan-feb.csv"},
		"bank-layouts":          {"42-bank-a-layout.csv", "43-bank-b-layout.csv"},
	}
	type variant struct {
		DupKeep map[string]int    `json:"dupKeep"`
		DupPick map[string]string `json:"dupPick"`
		Dupes   int               `json:"dupes"`
		Groups  []struct {
			Key      string `json:"key"`
			Versions []struct {
				FileIndex int  `json:"fileIndex"`
				Kept      bool `json:"kept"`
			} `json:"versions"`
		} `json:"groups"`
	}
	for name, paths := range sets {
		t.Run(name, func(t *testing.T) {
			var o map[string]json.RawMessage
			readJSON(t, filepath.Join(oracleDir, "dedupe", name+".json"), &o)
			files, names, ids := readSet(t, paths)
			total := 0
			for _, f := range files {
				total += len(f.rows)
			}
			for _, vn := range []string{"default", "preferLastFile", "keepBoth"} {
				raw, ok := o[vn]
				if !ok {
					continue
				}
				var v variant
				if err := json.Unmarshal(raw, &v); err != nil {
					t.Fatal(err)
				}
				keep := map[string]bool{}
				for k := range v.DupKeep {
					keep[k] = true
				}
				m := mergeFiles(files, names, ids, v.DupPick, keep)
				if got := total - len(m.kept); got != v.Dupes {
					t.Errorf("%s: %d duplicates skipped, legacy %d", vn, got, v.Dupes)
				}
				kept := map[RowRef]bool{}
				for _, r := range m.kept {
					kept[r] = true
				}
				for _, g := range v.Groups {
					rows := m.keys[g.Key]
					if len(rows) != len(g.Versions) {
						t.Fatalf("%s %s: %d rows, legacy %d versions", vn, g.Key, len(rows), len(g.Versions))
					}
					for i, ver := range g.Versions {
						if rows[i].File != ver.FileIndex || kept[rows[i]] != ver.Kept {
							t.Errorf("%s %s version %d: file %d kept %v, legacy file %d kept %v", vn, g.Key, i, rows[i].File, kept[rows[i]], ver.FileIndex, ver.Kept)
						}
					}
				}
			}
		})
	}
}

func readSet(t *testing.T, paths []string) (files []legacyFile, names, ids []string) {
	t.Helper()
	for i, p := range paths {
		raw, err := os.ReadFile(filepath.Join(fixturesDir, "synthetic", p))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, readLegacy(context.Background(), raw, optsOf(FileCfg{}, ParseOpts{})))
		names = append(names, filepath.Base(p))
		ids = append(ids, "f"+string(rune('1'+i)))
	}
	return files, names, ids
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestToFixed2(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "0.00"}, {math.Copysign(0, -1), "0.00"}, {-98.07, "-98.07"}, {8500, "8500.00"}, {0.125, "0.13"}, {-0.125, "-0.13"},
		{1.005, "1.00"}, {2.675, "2.67"}, {-0.001, "-0.00"}, {1234.5, "1234.50"}, {0.375, "0.38"}, {10.0049, "10.00"},
	} {
		if got := toFixed2(c.in); got != c.want {
			t.Errorf("toFixed2(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseDateMS(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"2025-03-02", 1740873600000, true},
		{"02.03.2025", 1740873600000, true},
		{"2025-03-02 10:30", 1740911400000, true},
		{"31.02.2025", 1740960000000, true}, // rolls over into March, as Date.UTC does
		{"0099-01-01", 915148800000, true},  // Date.UTC: years 0–99 are 1900–1999
		{"13.13.2025", 0, false},
		{"kawa", 0, false},
	} {
		got, ok := parseDateMS(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseDateMS(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
