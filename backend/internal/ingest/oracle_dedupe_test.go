package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestOracleDedupe replays the legacy multi-file de-duplication (PLAN task 3.11). Each file is one
// import: Parse → proposed mapping → Stage → AssignOccurrences. The database's unique constraint
// on (account, fingerprint, occurrence) is simulated with a set, inserting files in order; the
// kept multiset of rows must equal what legacy getDataset('merged') kept.
//
// Only the default rule is compared here. "Use this version" / "Keep both" overrides need the
// database (Rematerialize, Phase 6). The two-bank set is skipped: different layouts are different
// accounts in the rebuild and are never merged (ALLOWED_DIFFS.md row 3).
func TestOracleDedupe(t *testing.T) {
	sets := map[string][]string{
		"overlap-A-B":           {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv"},
		"overlap-A-C":           {"overlap/A_jan-mar.csv", "overlap/C_q1_again.csv"},
		"overlap-C-A":           {"overlap/C_q1_again.csv", "overlap/A_jan-mar.csv"},
		"overlap-A-B-C":         {"overlap/A_jan-mar.csv", "overlap/B_mar-may.csv", "overlap/C_q1_again.csv"},
		"same-name":             {"same-name/a/export.csv", "same-name/b/export.csv"},
		"dup-versions":          {"dup-versions/v1_jan-feb.csv", "dup-versions/v2_jan-mar.csv"},
		"dup-versions-reversed": {"dup-versions/v2_jan-mar.csv", "dup-versions/v1_jan-feb.csv"},
	}
	for name, files := range sets {
		t.Run(name, func(t *testing.T) {
			var oracle struct {
				Default struct {
					Headers []string   `json:"headers"`
					Rows    [][]string `json:"rows"`
					Dupes   int        `json:"dupes"`
				} `json:"default"`
			}
			b, err := os.ReadFile(filepath.Join(oracleDir, "dedupe", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &oracle); err != nil {
				t.Fatal(err)
			}
			type key struct {
				fp  [32]byte
				occ int
			}
			stored := map[key]bool{}
			var kept []string
			total := 0
			for _, f := range files {
				raw, err := os.ReadFile(filepath.Join(fixturesDir, "synthetic", f))
				if err != nil {
					t.Fatal(err)
				}
				res, err := Parse(context.Background(), raw, Options{})
				if err != nil {
					t.Fatal(err)
				}
				if got, want := append(slices.Clone(res.Table.Headers), "Source file"), oracle.Default.Headers; !slices.Equal(got, want) {
					t.Fatalf("headers %v, oracle %v", got, want)
				}
				rows, _, err := Stage(context.Background(), res.Table, MappingFromRoles(res.Roles), StageOptions{ImportCurrency: res.Roles.Currency})
				if err != nil {
					t.Fatal(err)
				}
				for i, r := range rows {
					if r.Error != "" {
						t.Fatalf("%s row %d: %s", f, i, r.Error)
					}
					total++
					k := key{r.Fingerprint, r.Occurrence}
					if stored[k] {
						continue // ON CONFLICT DO NOTHING
					}
					stored[k] = true
					cells, _ := json.Marshal(append(slices.Clone(res.Table.Rows[i]), filepath.Base(f)))
					kept = append(kept, string(cells))
				}
			}
			slices.Sort(kept)
			want := make([]string, len(oracle.Default.Rows))
			for i, r := range oracle.Default.Rows {
				b, _ := json.Marshal(r)
				want[i] = string(b)
			}
			slices.Sort(want)
			if diff := cmp.Diff(want, kept); diff != "" {
				t.Errorf("kept rows differ from the legacy (-legacy +ours):\n%s", diff)
			}
			if dupes := total - len(kept); dupes != oracle.Default.Dupes {
				t.Errorf("duplicates skipped = %d, legacy %d", dupes, oracle.Default.Dupes)
			}
			t.Logf("%d rows in, %d kept, %d duplicates", total, len(kept), total-len(kept))
		})
	}
}
