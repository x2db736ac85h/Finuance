package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// notTenantTables have a user_id column but are deliberately outside RLS (PLAN §5.3): sessions are
// resolved before the user is known, and so are friend codes (looked up by their hash, ADR 0027).
var notTenantTables = []string{"friend_codes", "sessions"}

// ownPolicies are tables whose policies differ from the single "<table>_tenant" policy: the audit
// log has one per command, so it can be read and appended to but never updated, and only expired
// rows can be deleted (migration 00010, TestAuditLogIsAppendOnly).
var ownPolicies = map[string][]string{
	"audit_log": {"audit_log_expire", "audit_log_read", "audit_log_write"},
	"shared_group_members": {"shared_group_members_change", "shared_group_members_invite", "shared_group_members_read",
		"shared_group_members_withdraw"},
}

// notForced have RLS enabled but not forced, with the reason: shared_group_members is read by the
// SECURITY DEFINER membership function, which FORCE RLS would blind (migration 00014, ADR 0028).
// The app role is still bound by its policies.
var notForced = map[string]string{"shared_group_members": "read by shared_member_role()"}

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
		if _, ok := notForced[name]; ok {
			if !enabled || forced {
				t.Errorf("%s: row level security enabled=%v forced=%v, want enabled and not forced", name, enabled, forced)
			}
		} else if !enabled || !forced {
			t.Errorf("%s: row level security enabled=%v forced=%v, want both", name, enabled, forced)
		}
		want := []string{name + "_tenant"}
		if own, ok := ownPolicies[name]; ok {
			want = own
		}
		if !slices.Equal(policies, want) {
			t.Errorf("%s: policies %v, want %v", name, policies, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"accounts", "assistant_chats", "assistant_messages", "assistant_settings", "audit_log", "budgets", "categories", "category_aliases", "dedupe_overrides", "groups", "import_files", "import_profiles",
		"import_rows", "imports", "investment_positions", "investment_profile", "legacy_import_files", "legacy_imports", "notifications", "rules", "shared_expense_links", "shared_group_members", "transactions", "tx_annotations", "user_preferences"}
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

// unindexedForeignKeys may lack an index on their columns: the referenced rows are deleted one at
// a time (a user, a category, a group, an import), so the check PostgreSQL runs on delete costs at
// most one scan. A foreign key whose referenced rows are deleted in bulk needs an index:
// transactions.transfer_peer_id without one made deleting a 917-row import take ~27 s
// (migration 00005).
var unindexedForeignKeys = []string{
	"budgets_category_id_fkey",
	"dedupe_overrides_preferred_import_id_fkey",
	"dedupe_overrides_user_id_fkey",
	"import_files_user_id_fkey",
	"transactions_category_id_fkey",
	"transactions_group_id_fkey",
	"tx_annotations_category_id_fkey",
	"tx_annotations_group_id_fkey",
	"tx_annotations_user_id_fkey",
}

// TestEveryForeignKeyIsIndexed reads the catalog: a new foreign key fails here until it has an index
// that starts with its columns, or a reason in unindexedForeignKeys.
func TestEveryForeignKeyIsIndexed(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	rows, err := tdb.Owner.Query(context.Background(), `
		SELECT c.conname
		FROM pg_constraint c
		WHERE c.contype = 'f' AND c.connamespace = 'public'::regnamespace
		  AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = c.conrelid
		    AND (i.indkey::int2[])[0:array_length(c.conkey, 1) - 1] @> c.conkey
		    AND (i.indkey::int2[])[0:array_length(c.conkey, 1) - 1] <@ c.conkey)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(missing, unindexedForeignKeys) {
		t.Errorf("foreign keys without an index:\n got  %v\n want %v", missing, unindexedForeignKeys)
	}
}
