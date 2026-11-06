package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// The RLS suite (PLAN task 4.6). It runs as finuance_app, the role the api and worker use, and
// proves that the policies — not the application's WHERE clauses — keep users apart: every query
// below is deliberately "unsafe", with no user_id predicate, and lives only in this test file.

// tenantTables reads the user-owned tables from the catalog (every table with a user_id column
// except the auth tables), so a table added later is covered without editing this file.
func tenantTables(t *testing.T, tdb *testutil.DB) []string {
	t.Helper()
	rows, err := tdb.Owner.Query(context.Background(), `
		SELECT c.relname::text FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
		WHERE c.relkind = 'r'
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'user_id' AND NOT a.attisdropped)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(all, func(s string) bool {
		return slices.Contains(notTenantTables, s) || slices.Contains(membershipTables, s)
	})
}

// membershipTables have a user_id column but are shared by membership, not owned by one user:
// shared_group_members (ADR 0028). TestSharedGroupRLS covers them.
var membershipTables = []string{"shared_group_members"}

// tenantData is what seed created for one user.
type tenantData struct {
	user    uuid.UUID
	account uuid.UUID
	imp     uuid.UUID
}

// seed gives u at least one row in every user-owned table, through the normal queries.
func seed(t *testing.T, s *store.Store, u uuid.UUID) tenantData {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := tenantData{user: u, account: platform.NewID()}
	err := s.WithUser(ctx, u, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: u, Prefs: []byte(`{"theme":"dark"}`), Now: now}); err != nil {
			return fmt.Errorf("preferences: %w", err)
		}
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{ID: d.account, UserID: u, Name: "Konto", NameN: "konto", Currency: "PLN", Kind: "bank"}); err != nil {
			return fmt.Errorf("account: %w", err)
		}
		cat, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: u, Name: "Food", NameN: "food"})
		if err != nil {
			return fmt.Errorf("category: %w", err)
		}
		grp, err := q.CreateGroup(ctx, db.CreateGroupParams{ID: platform.NewID(), UserID: u, Name: "Trip", NameN: "trip", Color: 0})
		if err != nil {
			return fmt.Errorf("group: %w", err)
		}
		imp, err := q.CreateImport(ctx, db.CreateImportParams{ID: platform.NewID(), UserID: u, AccountID: &d.account, Filename: "a.csv",
			SizeBytes: 3, Sha256: u[:], Encoding: "utf-8", Delimiter: ";", Headers: []byte(`["Data","Kwota"]`), ColumnTypes: []byte(`["date","number"]`),
			Mapping: []byte(`{}`), Options: []byte(`{}`), MappingSource: "detected", HeaderSignature: []byte{1}, Stats: []byte(`{}`), Now: now})
		if err != nil {
			return fmt.Errorf("import: %w", err)
		}
		d.imp = imp.ID
		if err := q.InsertImportFile(ctx, db.InsertImportFileParams{ImportID: imp.ID, UserID: u, Content: []byte("a;b")}); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		occ, amount := int32(1), int64(-100)
		if _, err := store.InsertImportRows(ctx, tx, imp.ID, u, []store.ImportRow{{RowIndex: 0, Fingerprint: []byte{7}, Occurrence: &occ,
			BookedOn: platform.MustDate(2025, 1, 2), AmountMinor: &amount, Currency: "PLN", Raw: []byte(`[]`), SearchText: "x"}}); err != nil {
			return fmt.Errorf("import rows: %w", err)
		}
		if _, err := q.InsertTransactionsFromImport(ctx, db.InsertTransactionsFromImportParams{UserID: u, ImportID: imp.ID, AccountID: d.account,
			Ids: []uuid.UUID{platform.NewID()}}); err != nil {
			return fmt.Errorf("transactions: %w", err)
		}
		if err := q.UpsertImportProfile(ctx, db.UpsertImportProfileParams{UserID: u, HeaderSignature: []byte{1}, Mapping: []byte(`{}`), Options: []byte(`{}`), Now: now}); err != nil {
			return fmt.Errorf("profile: %w", err)
		}
		if err := q.SetBudget(ctx, db.SetBudgetParams{UserID: u, CategoryID: cat.ID, AmountMinor: 100, Now: now}); err != nil {
			return fmt.Errorf("budget: %w", err)
		}
		if err := q.UpsertAnnotation(ctx, db.UpsertAnnotationParams{AccountID: d.account, Fingerprint: []byte{7}, Occurrence: 1, UserID: u,
			GroupID: &grp.ID, Note: "n", NoteN: "n", Now: now}); err != nil {
			return fmt.Errorf("annotation: %w", err)
		}
		if _, err := q.SetDedupeOverride(ctx, db.SetDedupeOverrideParams{AccountID: d.account, Fingerprint: []byte{7}, UserID: u, Mode: "keep_both", Now: now}); err != nil {
			return fmt.Errorf("override: %w", err)
		}
		if _, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: u, SwrBp: 400, RealReturnBp: 500, Now: now}); err != nil {
			return fmt.Errorf("investment profile: %w", err)
		}
		if _, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: platform.NewID(), UserID: u, Name: "ETF", Now: now}); err != nil {
			return fmt.Errorf("investment position: %w", err)
		}
		if _, err := q.UpsertAssistantSettings(ctx, db.UpsertAssistantSettingsParams{UserID: u, CloudConsent: []string{}, Now: now}); err != nil {
			return fmt.Errorf("assistant settings: %w", err)
		}
		chat, err := q.CreateAssistantChat(ctx, db.CreateAssistantChatParams{ID: platform.NewID(), UserID: u, Title: "Q", Now: now})
		if err != nil {
			return fmt.Errorf("assistant chat: %w", err)
		}
		if _, err := q.InsertAssistantMessage(ctx, db.InsertAssistantMessageParams{ChatID: chat.ID, UserID: u, Role: "user", Content: "Q", Now: now}); err != nil {
			return fmt.Errorf("assistant message: %w", err)
		}
		run, err := q.CreateLegacyImport(ctx, db.CreateLegacyImportParams{ID: platform.NewID(), UserID: u, Manifest: []byte(`{}`), Progress: []byte(`{}`), Now: now})
		if err != nil {
			return fmt.Errorf("legacy import: %w", err)
		}
		if err := q.AddLegacyImportFile(ctx, db.AddLegacyImportFileParams{LegacyImportID: run.ID, Position: 0, UserID: u, Content: []byte("x")}); err != nil {
			return fmt.Errorf("legacy import file: %w", err)
		}
		if err := q.SetCategoryAlias(ctx, db.SetCategoryAliasParams{UserID: u, NameN: "jedzenie", CategoryID: &cat.ID}); err != nil {
			return fmt.Errorf("category alias: %w", err)
		}
		if err := q.CreateRule(ctx, db.CreateRuleParams{ID: platform.NewID(), UserID: u, Name: "Kawa", Enabled: true,
			Match: []byte(`{"search":"kawa"}`), SetCategoryID: &cat.ID, Now: now}); err != nil {
			return fmt.Errorf("rule: %w", err)
		}
		if err := q.InsertAuditEvent(ctx, db.InsertAuditEventParams{UserID: u, Action: "auth.login", Meta: []byte(`{}`)}); err != nil {
			return fmt.Errorf("audit event: %w", err)
		}
		if err := q.InsertNotification(ctx, db.InsertNotificationParams{ID: platform.NewID(), UserID: u, Kind: "friend.added", Data: []byte(`{}`), Now: now}); err != nil {
			return fmt.Errorf("notification: %w", err)
		}
		change, err := q.OpenAIChange(ctx, db.OpenAIChangeParams{ID: platform.NewID(), UserID: u, ChatID: platform.NewID(),
			ProposalKey: "0123456789abcdef", Action: "set_category", Now: now})
		if err != nil {
			return fmt.Errorf("ai change: %w", err)
		}
		if err := q.UpsertAIChangeItem(ctx, db.UpsertAIChangeItemParams{ChangeID: change, UserID: u, Entity: "category",
			EntityID: cat.ID, After: []byte(`{"created":true}`)}); err != nil {
			return fmt.Errorf("ai change item: %w", err)
		}
		if err := seedSharedLink(ctx, tx, u, d.account); err != nil {
			return fmt.Errorf("shared expense link: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// appendOnlyTables can't be updated by the app role at all.
var appendOnlyTables = []string{"audit_log"}

// rlsFixture is two seeded users, A and B.
func rlsFixture(t *testing.T) (*testutil.DB, *store.Store, tenantData, tenantData, []string) {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	a, b := seed(t, s, newUser(t, s)), seed(t, s, newUser(t, s))
	return tdb, s, a, b, tenantTables(t, tdb)
}

func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// TestRLSReadsOnlyOwnRows: with app.user_id = A, a query without a user_id predicate returns A's
// rows and none of B's — on every user-owned table.
func TestRLSReadsOnlyOwnRows(t *testing.T) {
	_, s, a, b, tables := rlsFixture(t)
	ctx := context.Background()
	for _, who := range []tenantData{a, b} {
		err := s.WithUser(ctx, who.user, func(_ *db.Queries, tx pgx.Tx) error {
			for _, table := range tables {
				var mine, others int
				err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id = $1), count(*) FILTER (WHERE user_id <> $1) FROM `+table, who.user).Scan(&mine, &others)
				if err != nil {
					return fmt.Errorf("%s: %w", table, err)
				}
				if mine == 0 {
					t.Errorf("%s: no rows of the user; seed() must create one for every user-owned table", table)
				}
				if others != 0 {
					t.Errorf("%s: %d rows of other users visible without a user_id filter", table, others)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestRLSCannotWriteOtherUsersRows: UPDATE and DELETE aimed at B's rows affect nothing.
func TestRLSCannotWriteOtherUsersRows(t *testing.T) {
	_, s, a, b, tables := rlsFixture(t)
	ctx := context.Background()
	err := s.WithUser(ctx, a.user, func(_ *db.Queries, tx pgx.Tx) error {
		for _, table := range tables {
			var up pgconn.CommandTag
			if !slices.Contains(appendOnlyTables, table) { // no UPDATE grant at all (TestAuditLogIsAppendOnly)
				var err error
				if up, err = tx.Exec(ctx, `UPDATE `+table+` SET user_id = user_id WHERE user_id = $1`, b.user); err != nil {
					return fmt.Errorf("%s update: %w", table, err)
				}
			}
			del, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE user_id = $1`, b.user)
			if err != nil {
				return fmt.Errorf("%s delete: %w", table, err)
			}
			if up.RowsAffected() != 0 || del.RowsAffected() != 0 {
				t.Errorf("%s: updated %d and deleted %d of the other user's rows", table, up.RowsAffected(), del.RowsAffected())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// B's data is intact.
	err = s.WithUser(ctx, b.user, func(_ *db.Queries, tx pgx.Tx) error {
		for _, table := range tables {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				t.Errorf("%s: B lost its rows", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRLSWithCheckRejectsOtherUserID: a row can't be written with another user's id — neither by
// moving an own row to B (UPDATE … SET user_id) nor by inserting one.
func TestRLSWithCheckRejectsOtherUserID(t *testing.T) {
	_, s, a, b, tables := rlsFixture(t)
	ctx := context.Background()
	for _, table := range tables {
		err := s.WithUser(ctx, a.user, func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE `+table+` SET user_id = $1 WHERE user_id = $2`, b.user, a.user)
			return err
		})
		if !isPgCode(err, "42501") {
			t.Errorf("%s: moving a row to another user: %v, want a WITH CHECK violation (42501)", table, err)
		}
	}
	now := time.Now()
	inserts := map[string]func(q *db.Queries, tx pgx.Tx) error{
		"accounts": func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.CreateAccount(ctx, db.CreateAccountParams{ID: platform.NewID(), UserID: b.user, Name: "x", NameN: "x2", Currency: "PLN", Kind: "bank"})
			return err
		},
		"categories": func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: b.user, Name: "x", NameN: "x"})
			return err
		},
		"user_preferences": func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: b.user, Prefs: []byte(`{}`), Now: now})
			return err
		},
		"import_rows": func(_ *db.Queries, tx pgx.Tx) error {
			_, err := store.InsertImportRows(ctx, tx, a.imp, b.user, []store.ImportRow{{RowIndex: 9}})
			return err
		},
		"import_files": func(q *db.Queries, _ pgx.Tx) error {
			return q.InsertImportFile(ctx, db.InsertImportFileParams{ImportID: platform.NewID(), UserID: b.user, Content: []byte("x")})
		},
		"transactions": func(_ *db.Queries, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (id, user_id, account_id, fingerprint, occurrence, booked_on, amount_minor, currency, search_text)
				VALUES ($1, $2, $3, '\x01', 1, '2025-01-01', 1, 'PLN', '')`, platform.NewID(), b.user, a.account)
			return err
		},
		"investment_positions": func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: platform.NewID(), UserID: b.user, Name: "x", Now: now})
			return err
		},
	}
	for table, insert := range inserts {
		err := s.WithUser(ctx, a.user, insert)
		// import_files references an import that doesn't exist; the RLS check runs first.
		if !isPgCode(err, "42501") {
			t.Errorf("%s: insert with another user's id: %v, want 42501", table, err)
		}
	}
}

// TestRLSWithoutUserSeesNothing: no app.user_id (WithoutUser, the auth path) and the owner role
// (FORCE ROW LEVEL SECURITY) both see empty user tables and can't write to them.
func TestRLSWithoutUserSeesNothing(t *testing.T) {
	tdb, s, a, _, tables := rlsFixture(t)
	ctx := context.Background()
	for _, c := range []struct {
		name string
		st   *store.Store
	}{{"app role without user", s}, {"owner role", store.New(tdb.Owner)}} {
		err := c.st.WithoutUser(ctx, func(_ *db.Queries, tx pgx.Tx) error {
			for _, table := range tables {
				var n int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
					return fmt.Errorf("%s: %w", table, err)
				}
				if n != 0 {
					t.Errorf("%s: %s sees %d rows", c.name, table, n)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = c.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: a.user, Name: "x", NameN: "x"})
			return err
		})
		if !isPgCode(err, "42501") {
			t.Errorf("%s: insert without a user: %v, want 42501", c.name, err)
		}
	}
}

// TestRLSCascadesStillWork: deleting a user (auth path, no RLS on users) removes all of their rows
// in every table — referential actions run without row security even with FORCE — and leaves the
// other user untouched.
func TestRLSCascadesStillWork(t *testing.T) {
	_, s, a, b, tables := rlsFixture(t)
	ctx := context.Background()
	err := s.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteUser(ctx, b.user)
		if err == nil && n != 1 {
			err = fmt.Errorf("deleted %d users", n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []tenantData{a, b} {
		err := s.WithUser(ctx, who.user, func(_ *db.Queries, tx pgx.Tx) error {
			for _, table := range tables {
				var n int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
					return err
				}
				if who == b && n != 0 {
					t.Errorf("%s: %d rows of the deleted user remain", table, n)
				}
				if who == a && n == 0 {
					t.Errorf("%s: the other user's rows were deleted", table)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestPolishNaturalCollation: pl_natural sorts by Polish alphabet (ł after l, ż after z) and numbers
// by value ("Konto 2" before "Konto 10").
func TestPolishNaturalCollation(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	in := []string{"Żabka", "zebra", "Łódź", "Lodz", "ała", "Ala", "Konto 10", "Konto 2", "Ćma", "Cebula", "ąb", "az"}
	want := []string{"Ala", "ała", "az", "ąb", "Cebula", "Ćma", "Konto 2", "Konto 10", "Lodz", "Łódź", "zebra", "Żabka"}
	rows, err := tdb.App.Query(context.Background(), `SELECT s FROM unnest($1::text[]) AS s ORDER BY s COLLATE pl_natural`, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ORDER BY … COLLATE pl_natural:\n got %q\nwant %q", got, want)
	}
}

// seedSharedLink gives u a shared group of their own with one expense and the private link to it
// (test-only SQL: the sharing package's queries come with it).
func seedSharedLink(ctx context.Context, tx pgx.Tx, u, account uuid.UUID) error {
	g, m, e := platform.NewID(), platform.NewID(), platform.NewID()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO shared_groups (id, owner_id, name, currency) VALUES ($1, $2, 'Trip', 'PLN')`, []any{g, u}},
		{`INSERT INTO shared_group_members (id, group_id, user_id, role, state, display_name) VALUES ($1, $2, $3, 'owner', 'active', 'Me')`, []any{m, g, u}},
		{`INSERT INTO shared_expenses (id, group_id, added_by, added_member, paid_by, booked_on, amount_minor, payee, origin)
		  VALUES ($1, $2, $3, $4, $4, '2025-01-02', 100, 'Kawa', 'transaction')`, []any{e, g, u, m}},
		{`INSERT INTO shared_expense_splits (expense_id, group_id, member_id, share_minor) VALUES ($1, $2, $3, 100)`, []any{e, g, m}},
		{`INSERT INTO shared_expense_links (user_id, account_id, fingerprint, occurrence, split_scope, expense_id)
		  VALUES ($1, $2, '\x07', 1, '00000000-0000-0000-0000-000000000000', $3)`, []any{u, account, e}},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}
