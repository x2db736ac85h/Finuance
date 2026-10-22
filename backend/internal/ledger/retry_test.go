package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// TestCommitAgainAfterFailure: a failed commit can be started again (the preview's "Try again",
// Phase 9); an import that is committing or committed can't.
func TestCommitAgainAfterFailure(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Pekao")
	p, err := e.l.CreatePreview(e.ctx, u, ledger.Upload{Filename: "a.csv", Content: fixture(t, fileA), AccountID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	id := p.Import.ID
	noJob := func(context.Context, pgx.Tx) error { return nil }

	if _, err := e.l.StartCommit(e.ctx, u, id, noJob); err != nil {
		t.Fatal(err)
	}
	if _, err := e.l.StartCommit(e.ctx, u, id, noJob); !errors.Is(err, platform.ErrImportNotCommitable) {
		t.Fatalf("second start while committing: %v", err)
	}
	if err := e.l.FailImport(e.ctx, u, id, "the database went away"); err != nil {
		t.Fatal(err)
	}
	imp, err := e.l.StartCommit(e.ctx, u, id, noJob)
	if err != nil {
		t.Fatalf("start after failure: %v", err)
	}
	if imp.Status != ledger.StatusCommitting || imp.Error != "" {
		t.Fatalf("after retry: status %s, error %v", imp.Status, imp.Error)
	}
	if _, err := e.l.CommitImport(e.ctx, u, id); err != nil {
		t.Fatal(err)
	}
	if got, want := e.countTransactions(u, a.ID), len(identities(t, fileA)); got != want {
		t.Errorf("%d transactions, want %d", got, want)
	}
	if _, err := e.l.StartCommit(e.ctx, u, id, noJob); !errors.Is(err, platform.ErrImportNotCommitable) {
		t.Fatalf("start when committed: %v", err)
	}
}
