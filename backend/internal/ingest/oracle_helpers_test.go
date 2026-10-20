package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// oracleDir holds the legacy app's recorded behaviour (make oracle).
var oracleDir = filepath.Join("..", "..", "..", "testdata", "oracle")

// fixturesDir holds the CSV fixtures the oracle was run on.
var fixturesDir = filepath.Join("..", "..", "..", "testdata", "fixtures")

// corpusCase is one recorded call of a legacy value function. In and Out keep their raw JSON so
// each test decides how to read them (null, numbers, strings, intervals).
type corpusCase struct {
	In    json.RawMessage `json:"in"`
	Out   json.RawMessage `json:"out"`
	Error string          `json:"error"`
}

func loadCorpus(t *testing.T, name string) []corpusCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(oracleDir, "corpus", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []corpusCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// jsString converts a corpus input to what the legacy code saw after String(x || ”):
// null and 0 are falsy and become "", other numbers become their decimal form.
func jsString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil && n.String() != "0" {
		return n.String()
	}
	return ""
}
