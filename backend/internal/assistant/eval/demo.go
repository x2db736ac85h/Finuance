package eval

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/sharing"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// SetupDemo creates a user with the demo year imported and the groups the legacy oracle assigns
// (tools/legacy-oracle/page-helpers.js assignDemoGroups: every 17th row of the file in "Wakacje
// nad morzem", the other "Ogród" rows in "Remont").
func SetupDemo(ctx context.Context, st *store.Store, csvPath string) (uuid.UUID, error) {
	content, err := os.ReadFile(csvPath) //nolint:gosec // the repository's demo file
	if err != nil {
		return uuid.Nil, err
	}
	u := platform.NewID()
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateUser(ctx, db.CreateUserParams{ID: u, Email: u.String() + "@example.test", PasswordHash: "x"})
		return err
	}); err != nil {
		return uuid.Nil, err
	}
	l := ledger.New(st, platform.SystemClock{})
	acc, err := l.CreateAccount(ctx, u, ledger.AccountInput{Name: "Demo"})
	if err != nil {
		return uuid.Nil, err
	}
	p, err := l.CreatePreview(ctx, u, ledger.Upload{Filename: "demo.csv", Content: content, AccountID: &acc.ID})
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := l.StartCommit(ctx, u, p.Import.ID, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		return uuid.Nil, err
	}
	if _, err := l.CommitImport(ctx, u, p.Import.ID); err != nil {
		return uuid.Nil, err
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n")[1:] {
		refs = append(refs, strings.Split(line, ";")[9])
	}
	eng := query.New(st)
	byRef := map[string]query.Row{}
	for off := 0; ; off += query.MaxPageRows {
		pg, err := eng.Page(ctx, u, query.PageRequest{Offset: off, Limit: query.MaxPageRows})
		if err != nil {
			return uuid.Nil, err
		}
		for i := range pg.Rows {
			byRef[pg.Rows[i].Reference] = pg.Rows[i]
		}
		if len(pg.Rows) < query.MaxPageRows {
			break
		}
	}
	var holiday, renovation []uuid.UUID
	for i, ref := range refs {
		r, ok := byRef[ref]
		if !ok {
			return uuid.Nil, fmt.Errorf("demo row %d (%s) not imported", i, ref)
		}
		switch {
		case i%17 == 0:
			holiday = append(holiday, r.ID)
		case r.CategoryName != nil && *r.CategoryName == "Ogród":
			renovation = append(renovation, r.ID)
		}
	}
	for i, g := range []struct {
		name string
		ids  []uuid.UUID
	}{{"Wakacje nad morzem", holiday}, {"Remont", renovation}} {
		grp, err := l.CreateGroup(ctx, u, g.name, i+1)
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := l.Annotate(ctx, u, g.ids, ledger.AnnotationPatch{SetGroup: true, GroupID: &grp.ID}); err != nil {
			return uuid.Nil, err
		}
	}
	// Phase 20 proposals: an empty cash account (add_expense) and a shared trip (add_to_shared_group)
	if _, err := l.CreateAccount(ctx, u, ledger.AccountInput{Name: "Portfel", Kind: ledger.KindCash}); err != nil {
		return uuid.Nil, err
	}
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		return q.SetUserDisplayName(ctx, db.SetUserDisplayNameParams{ID: u, DisplayName: "Alex"})
	}); err != nil {
		return uuid.Nil, err
	}
	trip, err := l.CreateGroup(ctx, u, "Praga", 3)
	if err != nil {
		return uuid.Nil, err
	}
	sh := sharing.New(st, l, notify.New(st, platform.SystemClock{}), platform.SystemClock{})
	if _, err := sh.Create(ctx, u, sharing.GroupInput{Name: "Wyjazd do Pragi", FromGroupID: &trip.ID}); err != nil {
		return uuid.Nil, err
	}
	return u, nil
}
