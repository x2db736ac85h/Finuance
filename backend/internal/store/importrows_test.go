package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// newImport creates a previewed import for u and returns its id.
func newImport(t *testing.T, s *store.Store, u uuid.UUID) uuid.UUID {
	t.Helper()
	id := platform.NewID()
	err := s.WithUser(context.Background(), u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateImport(context.Background(), db.CreateImportParams{
			ID: id, UserID: u, Filename: "a.csv", SizeBytes: 1, Sha256: id[:], Encoding: "utf-8", Delimiter: ";",
			Headers: []byte(`[]`), ColumnTypes: []byte(`[]`), Mapping: []byte(`{}`), Options: []byte(`{}`),
			MappingSource: "detected", HeaderSignature: []byte{1}, Stats: []byte(`{}`), Now: platform.SystemClock{}.Now(),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func TestInsertImportRowsRoundTrip(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	u := newUser(t, s)
	imp := newImport(t, s, u)
	n := store.ImportRowBatch + 2 // two statements
	rows := make([]store.ImportRow, n)
	for i := range rows {
		rows[i] = store.ImportRow{
			RowIndex: int32(i), Fingerprint: []byte{byte(i), byte(i >> 8)}, Occurrence: ptr(int32(1)),
			BookedOn: platform.MustDate(2025, 1, 2), AmountMinor: ptr(int64(-i)), Currency: "PLN",
			Payee: "Żabka", PayeeN: "zabka", PayeeGroup: "Żabka", Raw: []byte(`[{"h":"b","v":"2"},{"h":"a","v":"1"}]`),
			SearchText: "zabka",
		}
	}
	// Row 1 is an errored row: no canonical values, an error message.
	rows[1] = store.ImportRow{RowIndex: 1, ParseError: ptr(`amount "x" is not an amount`), Raw: []byte(`[{"h":"Kwota","v":"x"}]`)}
	err := s.WithUser(ctx, u, func(_ *db.Queries, tx pgx.Tx) error {
		got, err := store.InsertImportRows(ctx, tx, imp, u, rows)
		if err == nil && got != int64(n) {
			t.Errorf("inserted %d rows, want %d", got, n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		c, err := q.CountImportRows(ctx, db.CountImportRowsParams{UserID: u, ImportID: imp})
		if err != nil {
			return err
		}
		if c.Rows != int64(n) || c.Errors != 1 || c.Linked != 0 {
			t.Errorf("counts = %+v", c)
		}
		page, err := q.ListImportRows(ctx, db.ListImportRowsParams{UserID: u, ImportID: imp, MaxRows: 3})
		if err != nil {
			return err
		}
		ok, bad := page[0], page[1]
		if ok.BookedOn == nil || *ok.BookedOn != platform.MustDate(2025, 1, 2) || ok.ValueOn != nil || *ok.AmountMinor != 0 ||
			*ok.Currency != "PLN" || *ok.PayeeN != "zabka" || ok.ParseError != nil || ok.SplitScope != uuid.Nil {
			t.Errorf("valid row = %+v", ok)
		}
		var raw []map[string]string
		if err := json.Unmarshal(ok.Raw, &raw); err != nil || raw[0]["h"] != "b" || raw[1]["h"] != "a" {
			t.Errorf("raw order lost: %s", ok.Raw)
		}
		if bad.Fingerprint != nil || bad.Occurrence != nil || bad.BookedOn != nil || bad.AmountMinor != nil || bad.Currency != nil ||
			bad.ParseError == nil || *bad.ParseError != `amount "x" is not an amount` {
			t.Errorf("errored row = %+v", bad)
		}
		errs, err := q.ListImportRows(ctx, db.ListImportRowsParams{UserID: u, ImportID: imp, OnlyErrors: true, MaxRows: 10})
		if err == nil && (len(errs) != 1 || errs[0].RowIndex != 1) {
			t.Errorf("only errors = %d rows", len(errs))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestInsertImportRowsChecksTheUser: rows for another user's id fail the RLS WITH CHECK, even when
// the import id is the caller's own.
func TestInsertImportRowsChecksTheUser(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	a, b := newUser(t, s), newUser(t, s)
	imp := newImport(t, s, a)
	err := s.WithUser(ctx, a, func(_ *db.Queries, tx pgx.Tx) error {
		_, err := store.InsertImportRows(ctx, tx, imp, b, []store.ImportRow{{RowIndex: 0}})
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" { // insufficient_privilege: new row violates row-level security policy
		t.Fatalf("insert as another user: %v, want SQLSTATE 42501", err)
	}
}
