package ledger_test

import (
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/demo"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// TestSeedDemoRollsBack: when the demo import cannot be created (here: the user already imported
// the same bytes into another account), SeedDemo leaves no empty demo account behind.
func TestSeedDemoRollsBack(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	a := e.account(u, "Regular")
	if _, err := e.l.CreatePreview(e.ctx, u, ledger.Upload{Filename: "mine.csv", Content: []byte(demo.Recent(clock.Now())), AccountID: &a.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.l.SeedDemo(e.ctx, u)
	isErr(t, err, platform.ErrDuplicateFile)
	accounts, err := e.l.ListAccounts(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != a.ID {
		t.Errorf("accounts after a failed seed = %+v", accounts)
	}
}
