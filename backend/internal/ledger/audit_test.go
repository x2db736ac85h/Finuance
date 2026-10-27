package ledger_test

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// TestAuditLog: events are recorded with their address and browser, paged newest first, kept
// apart per user, and the cleanup removes only events past the retention.
func TestAuditLog(t *testing.T) {
	e := newEnv(t)
	a, b := e.user(), e.user()
	ip := netip.MustParseAddr("::ffff:203.0.113.7") // a v4-mapped address is stored as IPv4
	for i := range 5 {
		if err := e.l.RecordAudit(e.ctx, a, ledger.AuditEvent{Action: ledger.AuditLogin, IP: ip, Browser: "Firefox on Linux",
			Meta: map[string]any{"n": i}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.l.RecordAudit(e.ctx, b, ledger.AuditEvent{Action: ledger.AuditExportData, EntityID: "x"}); err != nil {
		t.Fatal(err)
	}

	page, err := e.l.AuditPage(e.ctx, a, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 || page[0].ID <= page[1].ID || page[0].Meta["n"] != float64(4) {
		t.Fatalf("first page = %+v, want the 3 newest", page)
	}
	if got := page[0]; got.Action != ledger.AuditLogin || got.IP.String() != "203.0.113.7" || got.Browser != "Firefox on Linux" || got.At.IsZero() {
		t.Errorf("event = %+v", got)
	}
	rest, err := e.l.AuditPage(e.ctx, a, page[2].ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 2 || rest[0].ID >= page[2].ID {
		t.Errorf("second page = %+v", rest)
	}
	other, err := e.l.AuditPage(e.ctx, b, 0, 50)
	if err != nil || len(other) != 1 || other[0].EntityID != "x" || other[0].IP.IsValid() {
		t.Errorf("B's events = %+v, %v (only its own, no address)", other, err)
	}

	// invalid events are refused by the table, not truncated silently
	for _, bad := range []ledger.AuditEvent{{Action: "Login"}, {Action: "auth.login", Meta: map[string]any{"x": string(make([]byte, 5000))}}} {
		if err := e.l.RecordAudit(e.ctx, a, bad); err == nil {
			t.Errorf("%q accepted", bad.Action)
		}
	}

	// retention: the superuser (no RLS) backdates two events of each user
	tdb := testutil.Require(t, testDB)
	super, err := pgxpool.New(e.ctx, tdb.SuperDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer super.Close()
	if _, err := super.Exec(e.ctx, `UPDATE audit_log SET at = now() - interval '366 days'
		WHERE id IN (SELECT min(id) FROM audit_log WHERE user_id = ANY($1) GROUP BY user_id)`, []any{a, b}); err != nil {
		t.Fatal(err)
	}
	n, err := e.l.CleanupAudit(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 { // other tests' users may have expired events too; these two must be gone
		t.Errorf("cleanup deleted %d events", n)
	}
	if e.count(a, "audit_log") != 4 || e.count(b, "audit_log") != 0 {
		t.Errorf("after cleanup A has %d (want 4), B %d (want 0)", e.count(a, "audit_log"), e.count(b, "audit_log"))
	}
}

// TestCommitRecordsTheImport: a commit writes import.committed in its own transaction, with the
// file name and counts and without an address (a job has none); the data export carries it.
func TestCommitRecordsTheImport(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	acc := e.account(u, "Konto")
	imp, res := e.importFile(u, acc.ID, "synthetic/overlap/A_jan-mar.csv")
	evs, err := e.l.AuditPage(e.ctx, u, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	ev := evs[0]
	if ev.Action != ledger.AuditImportCommitted || ev.EntityID != imp.ID.String() || ev.IP.IsValid() ||
		ev.Meta["file"] != "A_jan-mar.csv" || ev.Meta["new"] != float64(res.New) {
		t.Errorf("event = %+v (commit %+v)", ev, res)
	}

	docs, err := e.l.Export(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	act, ok := docs["activity.json"].([]ledger.ExportActivity)
	if !ok || len(act) != 1 || act[0].Action != ledger.AuditImportCommitted || act[0].IP != "" {
		t.Errorf("activity.json = %#v", docs["activity.json"])
	}
}

func TestAuditActionsAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range ledger.AuditActions {
		if seen[a] || len(a) > 48 || !slices.Contains([]byte(a), '.') {
			t.Errorf("action %q duplicated or malformed", a)
		}
		seen[a] = true
	}
}
