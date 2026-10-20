package ledger_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// BenchmarkCreateImport_100k stores a staged 100k-row file (PLAN task 4.4, target ≤ 3 s): the
// conversion to import_rows, the file bytes and 20 batched unnest INSERTs, in one transaction.
// Parsing and staging happen once, outside the timer.
func BenchmarkCreateImport_100k(b *testing.B) {
	if testDB == nil {
		b.Skip("needs Postgres (run without -short)")
	}
	ctx := context.Background()
	csv := demo.CSV(time.Date(1890, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	lines := strings.SplitN(csv, "\n", 100_002)
	content := []byte(strings.Join(lines[:100_001], "\n"))
	res, err := ingest.Parse(ctx, content, ingest.Options{})
	if err != nil {
		b.Fatal(err)
	}
	rows, _, err := ingest.Stage(ctx, res.Table, ingest.MappingFromRoles(res.Roles), ingest.StageOptions{ImportCurrency: res.Roles.Currency})
	if err != nil || len(rows) != 100_000 {
		b.Fatalf("staged %d rows: %v", len(rows), err)
	}
	st := store.New(testDB.App)
	l := ledger.New(st, platform.SystemClock{})
	u := platform.NewID()
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(ctx, db.CreateUserParams{ID: u, Email: u.String() + "@bench.test", PasswordHash: "x"})
		return err
	}); err != nil {
		b.Fatal(err)
	}
	in := ledger.NewImport{Filename: "demo.csv", Encoding: "utf-8", Delimiter: ";", Headers: res.Table.Headers,
		Mapping: []byte(`{}`), Options: []byte(`{}`), MappingSource: "detected", Rows: rows}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		in.Content = append(append([]byte(nil), content...), byte(i), byte(i>>8)) // a new SHA-256 each run
		if _, err := l.CreateImport(ctx, u, in); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(rows))*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
}
