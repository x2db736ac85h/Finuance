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

// TestFileNamesAreCleaned: a file's name is stored without control characters and without the
// characters that reorder text on screen, so it reads the same wherever it is shown (IMP-7); a
// name made only of such characters becomes the default one.
func TestFileNamesAreCleaned(t *testing.T) {
	e := newEnv(t)
	for in, want := range map[string]string{
		"wyci\u0105g\u202egsc.exe\n": "wyciąggsc.exe",
		"\u202e\u2066\x1b":           "upload.csv",
		"zwykły plik.csv":            "zwykły plik.csv",
	} {
		u := e.user() // the same file again would be a duplicate for one user
		a := e.account(u, "Pekao")
		p, err := e.l.CreatePreview(e.ctx, u, ledger.Upload{Filename: in, Content: fixture(t, fileA), AccountID: &a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if p.Import.Filename != want {
			t.Errorf("file name %q stored as %q, want %q", in, p.Import.Filename, want)
		}
	}
}
