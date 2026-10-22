package store_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

// Queries whose repositories arrive in later phases (auth in 5, investments with their API,
// overrides in 6) are tested here against the real schema, so every query in queries/ runs.

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestUserQueries(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	id := platform.NewID()
	email := "Alex." + id.String()[:8] + "@Example.test"
	err := s.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.CreateUser(ctx, db.CreateUserParams{ID: id, Email: email, PasswordHash: "$argon2id$x", DisplayName: "Alex"}); err != nil {
			return err
		}
		// citext: the lookup ignores case.
		got, err := q.GetUserByEmail(ctx, "alex."+id.String()[:8]+"@example.TEST")
		if err != nil || got.ID != id || got.DisplayName != "Alex" || got.LastLoginAt != nil {
			t.Errorf("by email = %+v, %v", got, err)
		}
		if err := q.SetUserLastLogin(ctx, db.SetUserLastLoginParams{ID: id, At: &t0}); err != nil {
			return err
		}
		if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: id, PasswordHash: "new"}); err != nil {
			return err
		}
		if err := q.SetUserDisplayName(ctx, db.SetUserDisplayNameParams{ID: id, DisplayName: "I."}); err != nil {
			return err
		}
		u, err := q.GetUser(ctx, id)
		if err != nil || u.PasswordHash != "new" || u.DisplayName != "I." || u.LastLoginAt == nil || !u.LastLoginAt.Equal(t0) {
			t.Errorf("user = %+v, %v", u, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The same address in another case is the same user (citext UNIQUE).
	err = s.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(ctx, db.CreateUserParams{ID: platform.NewID(), Email: "ALEX." + id.String()[:8] + "@example.test", PasswordHash: "x"})
		return err
	})
	if !isPgCode(err, "23505") {
		t.Errorf("duplicate email: %v, want unique_violation", err)
	}
}

func TestSessionQueries(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	u := newUser(t, s)
	tok, other := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	ip := netip.MustParseAddr("192.0.2.1")
	err := s.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		for _, h := range [][]byte{tok, other} {
			if err := q.CreateSession(ctx, db.CreateSessionParams{TokenHash: h, UserID: u, Now: t0, ExpiresAt: t0.Add(time.Hour), UserAgent: "test", Ip: &ip}); err != nil {
				return err
			}
		}
		idle := func(now time.Time) time.Time { return now.Add(-20 * time.Minute) } // a 20-minute idle timeout
		get := func(now time.Time) error {
			_, err := q.GetSession(ctx, db.GetSessionParams{TokenHash: tok, Now: now, IdleCutoff: idle(now)})
			return err
		}
		got, err := q.GetSession(ctx, db.GetSessionParams{TokenHash: tok, Now: t0.Add(time.Minute), IdleCutoff: idle(t0.Add(time.Minute))})
		if err != nil || got.UserID != u || got.Email == "" {
			t.Errorf("live session = %+v, %v", got, err)
		}
		// Idle for 25 minutes: gone.
		if err := get(t0.Add(25 * time.Minute)); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("idle session returned: %v", err)
		}
		// Activity at minute 15 keeps it alive at minute 25 …
		if err := q.TouchSession(ctx, db.TouchSessionParams{TokenHash: tok, Now: t0.Add(15 * time.Minute)}); err != nil {
			return err
		}
		if err := get(t0.Add(25 * time.Minute)); err != nil {
			t.Errorf("touched session expired: %v", err)
		}
		// … but never past the absolute expiry.
		if err := q.TouchSession(ctx, db.TouchSessionParams{TokenHash: tok, Now: t0.Add(55 * time.Minute)}); err != nil {
			return err
		}
		if err := get(t0.Add(61 * time.Minute)); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("session past its absolute expiry returned: %v", err)
		}
		list, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{UserID: u, Now: t0.Add(16 * time.Minute), IdleCutoff: idle(t0.Add(16 * time.Minute))})
		if err != nil || len(list) != 2 {
			t.Errorf("list = %d sessions, %v", len(list), err)
		}
		// Revoke by the public id (first 8 bytes of the hash).
		if n, err := q.DeleteUserSessionByPrefix(ctx, db.DeleteUserSessionByPrefixParams{UserID: u, Prefix: other[:8]}); err != nil || n != 1 {
			t.Errorf("revoke by prefix = %d, %v", n, err)
		}
		if err := q.CreateSession(ctx, db.CreateSessionParams{TokenHash: other, UserID: u, Now: t0, ExpiresAt: t0.Add(time.Hour), UserAgent: "test"}); err != nil {
			return err
		}
		// Log out everywhere else.
		if n, err := q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: u, KeepTokenHash: tok}); err != nil || n != 1 {
			t.Errorf("delete other sessions = %d, %v", n, err)
		}
		if n, err := q.DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{Now: t0.Add(4 * time.Hour), IdleCutoff: idle(t0.Add(4 * time.Hour))}); err != nil || n != 1 {
			t.Errorf("delete expired = %d, %v", n, err)
		}
		return q.DeleteSession(ctx, tok)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreferencesVersioning(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	u := newUser(t, s)
	err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		v, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: u, Prefs: []byte(`{"theme":"dark"}`), Now: t0})
		if err != nil || v != 1 {
			t.Fatalf("first save = %d, %v", v, err)
		}
		if v, err = q.SavePreferences(ctx, db.SavePreferencesParams{UserID: u, Prefs: []byte(`{"theme":"light"}`), Now: t0, ExpectedVersion: 1}); err != nil || v != 2 {
			t.Errorf("second save = %d, %v", v, err)
		}
		p, err := q.GetPreferences(ctx, u)
		if err != nil || p.Version != 2 || string(p.Prefs) != `{"theme": "light"}` {
			t.Errorf("prefs = %+v (%s), %v", p, p.Prefs, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A stale version, or a second "first" save, changes nothing and returns no row.
	for _, v := range []int32{1, 0, 3} {
		err = s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
			_, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: u, Prefs: []byte(`{}`), Now: t0, ExpectedVersion: v})
			return err
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("save with version %d over version 2: %v, want no rows (version conflict)", v, err)
		}
	}
	// Without a document, only version 0 creates it.
	other := newUser(t, s)
	err = s.WithUser(ctx, other, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: other, Prefs: []byte(`{}`), Now: t0, ExpectedVersion: 4})
		return err
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("first save with version 4: %v, want no rows", err)
	}
}

func TestInvestmentQueries(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	u := newUser(t, s)
	err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: u, PpkEmployeeMinor: 100, SwrBp: 400, RealReturnBp: 500, Now: t0}); err != nil {
			return err
		}
		if v, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: u, PpkEmployeeMinor: 200, SwrBp: 350, RealReturnBp: 500, Now: t0, ExpectedVersion: 1}); err != nil || v != 2 {
			t.Errorf("profile save = %d, %v", v, err)
		}
		p, err := q.GetInvestmentProfile(ctx, u)
		if err != nil || p.PpkEmployeeMinor != 200 || p.SwrBp != 350 {
			t.Errorf("profile = %+v, %v", p, err)
		}
		a, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: platform.NewID(), UserID: u, Position: 2, Name: "Obligacje", Now: t0})
		if err != nil {
			return err
		}
		if _, err := q.CreateInvestmentPosition(ctx, db.CreateInvestmentPositionParams{ID: platform.NewID(), UserID: u, Position: 1, Name: "ETF", Type: "etf", Now: t0}); err != nil {
			return err
		}
		up, err := q.UpdateInvestmentPosition(ctx, db.UpdateInvestmentPositionParams{UserID: u, ID: a.ID, Position: 0, Name: "Obligacje skarbowe", InvestedMinor: 1000, ValueMinor: 1100, Now: t0})
		if err != nil || up.ValueMinor != 1100 {
			t.Errorf("update = %+v, %v", up, err)
		}
		list, err := q.ListInvestmentPositions(ctx, u)
		if err != nil || len(list) != 2 || list[0].ID != a.ID {
			t.Errorf("list (by position) = %+v, %v", list, err)
		}
		if n, err := q.DeleteInvestmentPosition(ctx, db.DeleteInvestmentPositionParams{UserID: u, ID: a.ID}); err != nil || n != 1 {
			t.Errorf("delete = %d, %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// CHECK constraints guard the ranges.
	err = s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.SaveInvestmentProfile(ctx, db.SaveInvestmentProfileParams{UserID: u, SwrBp: 5000, RealReturnBp: 500, Now: t0, ExpectedVersion: 2})
		return err
	})
	if !isPgCode(err, "23514") {
		t.Errorf("swr 50 %%: %v, want check_violation", err)
	}
}

func TestOverrideAndImportQueries(t *testing.T) {
	tdb := testutil.Require(t, testDB)
	s := store.New(tdb.App)
	ctx := context.Background()
	d := seed(t, s, newUser(t, s))
	u := d.user
	err := s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		// seed() set keep_both on fingerprint 7; switch it to prefer this import.
		o, err := q.SetDedupeOverride(ctx, db.SetDedupeOverrideParams{AccountID: d.account, Fingerprint: []byte{7}, UserID: u, Mode: "prefer", PreferredImportID: &d.imp, Now: t0})
		if err != nil || o.Mode != "prefer" {
			t.Errorf("set override = %+v, %v", o, err)
		}
		list, err := q.ListDedupeOverrides(ctx, db.ListDedupeOverridesParams{UserID: u, AccountID: d.account})
		if err != nil || len(list) != 1 || *list[0].PreferredImportID != d.imp {
			t.Errorf("overrides = %+v, %v", list, err)
		}
		if n, err := q.ClearDedupeOverride(ctx, db.ClearDedupeOverrideParams{UserID: u, AccountID: d.account, Fingerprint: []byte{7}}); err != nil || n != 1 {
			t.Errorf("clear = %d, %v", n, err)
		}
		a, err := q.GetAnnotation(ctx, db.GetAnnotationParams{UserID: u, AccountID: d.account, Fingerprint: []byte{7}, Occurrence: 1, SplitScope: uuid.Nil})
		if err != nil || a.Note != "n" {
			t.Errorf("annotation = %+v, %v", a, err)
		}
		f, err := q.GetImportFile(ctx, db.GetImportFileParams{UserID: u, ImportID: d.imp})
		if err != nil || string(f) != "a;b" {
			t.Errorf("file = %q, %v", f, err)
		}
		imp, err := q.UpdateImportPreview(ctx, db.UpdateImportPreviewParams{UserID: u, ID: d.imp, ExpectedStatus: "previewed", AccountID: &d.account, Encoding: "windows-1250", Delimiter: ",",
			Headers: []byte(`["A"]`), ColumnTypes: []byte(`["text"]`), Mapping: []byte(`{"x":1}`), Options: []byte(`{}`), MappingSource: "user", HeaderSignature: []byte{2}, Stats: []byte(`{}`)})
		if err != nil || imp.Encoding != "windows-1250" || imp.MappingSource != "user" {
			t.Errorf("update preview = %+v, %v", imp, err)
		}
		// The status move is guarded by the expected current status.
		if _, err := q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: u, ID: d.imp, FromStatus: "committing", ToStatus: "committed", Now: t0}); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("move from the wrong status: %v, want no rows", err)
		}
		msg := "boom"
		failed, err := q.SetImportStatus(ctx, db.SetImportStatusParams{UserID: u, ID: d.imp, FromStatus: "previewed", ToStatus: "failed", Error: &msg, Now: t0})
		if err != nil || failed.Status != "failed" || failed.CommittedAt != nil || *failed.Error != "boom" {
			t.Errorf("fail import = %+v, %v", failed, err)
		}
		// A failed import no longer blocks re-uploading the same file.
		if _, err := q.FindImportBySHA(ctx, db.FindImportBySHAParams{UserID: u, Sha256: u[:]}); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("failed import still counts as a duplicate: %v", err)
		}
		// Preview updates only apply while previewed.
		if _, err := q.UpdateImportPreview(ctx, db.UpdateImportPreviewParams{UserID: u, ID: d.imp, ExpectedStatus: "previewed", Headers: []byte(`[]`), ColumnTypes: []byte(`[]`),
			Mapping: []byte(`{}`), Options: []byte(`{}`), MappingSource: "user", HeaderSignature: []byte{2}, Stats: []byte(`{}`)}); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("preview update on a failed import: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// prefer requires an import, keep_both forbids one (CHECK constraint).
	err = s.WithUser(ctx, u, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.SetDedupeOverride(ctx, db.SetDedupeOverrideParams{AccountID: d.account, Fingerprint: []byte{8}, UserID: u, Mode: "prefer", Now: t0})
		return err
	})
	if !isPgCode(err, "23514") {
		t.Errorf("prefer without an import: %v, want check_violation", err)
	}
}
