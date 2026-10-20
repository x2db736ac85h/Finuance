package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/demo"
)

// demo100k returns a Pekao-layout file of exactly 100 000 rows from the demo generator (about 130
// years of fictional transactions).
func demo100k(b *testing.B) []byte {
	b.Helper()
	csv := demo.CSV(time.Date(1890, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	lines := strings.SplitN(csv, "\n", 100_002)
	if len(lines) < 100_001 {
		b.Fatalf("generator produced only %d rows", len(lines)-1)
	}
	return []byte(strings.Join(lines[:100_001], "\n"))
}

// BenchmarkParse_100k is the whole pipeline on 100k rows: decode, delimiter, tokenize, shape,
// typing, roles, staging with fingerprints (PLAN task 3.14, target ≤ 500 ms).
func BenchmarkParse_100k(b *testing.B) {
	raw := demo100k(b)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		res, err := Parse(context.Background(), raw, Options{})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := Stage(context.Background(), res.Table, MappingFromRoles(res.Roles), StageOptions{ImportCurrency: res.Roles.Currency}); err != nil {
			b.Fatal(err)
		}
	}
}
