package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// notTenantTables have a user_id column but are deliberately outside RLS (PLAN §5.3): sessions are
// resolved before the user is known.
var notTenantTables = []string{"sessions"}

// TestEveryUserTableHasForcedRLS reads the catalog instead of a hand-kept list, so a table added in a
// later migration fails here until it gets its policy.
func TestEveryUserTableHasForcedRLS(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	ctx := context.Background()
	rows, err := tdb.Owner.Query(ctx, `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       coalesce((SELECT array_agg(p.polname::text ORDER BY p.polname) FROM pg_policy p WHERE p.polrelid = c.oid), '{}')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
		WHERE c.relkind = 'r'
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'user_id' AND NOT a.attisdropped)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tenant []string
	for rows.Next() {
		var (
			name            string
			enabled, forced bool
			policies        []string
		)
		if err := rows.Scan(&name, &enabled, &forced, &policies); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(notTenantTables, name) {
			if enabled {
				t.Errorf("%s: RLS enabled, but it must stay outside RLS", name)
			}
			continue
		}
		tenant = append(tenant, name)
		if !enabled || !forced {
			t.Errorf("%s: row level security enabled=%v forced=%v, want both", name, enabled, forced)
		}
		if !slices.Equal(policies, []string{name + "_tenant"}) {
			t.Errorf("%s: policies %v, want [%s_tenant]", name, policies, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"accounts", "budgets", "categories", "dedupe_overrides", "groups", "import_files", "import_profiles",
		"import_rows", "imports", "investment_positions", "investment_profile", "transactions", "tx_annotations", "user_preferences"}
	if !slices.Equal(tenant, want) {
		t.Errorf("tenant tables = %v, want %v", tenant, want)
	}
}

// TestUsersTableOutsideRLS documents that users has no user_id and no RLS (only auth touches it).
func TestUsersTableOutsideRLS(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	var enabled bool
	if err := tdb.Owner.QueryRow(context.Background(), `SELECT relrowsecurity FROM pg_class WHERE oid = 'users'::regclass`).Scan(&enabled); err != nil || enabled {
		t.Errorf("users relrowsecurity = %v, %v", enabled, err)
	}
}
