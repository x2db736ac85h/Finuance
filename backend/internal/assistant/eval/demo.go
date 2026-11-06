package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/advice"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/goals"
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
	var cinema uuid.UUID // the cinema on 2025-01-28, an expense of the shared trip (split_expense)
	for ref := range byRef {
		if r := byRef[ref]; strings.HasPrefix(r.Payee, "KINO") && r.BookedOn == platform.MustDate(2025, 1, 28) {
			cinema = r.ID
		}
	}
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
	notes := notify.New(st, platform.SystemClock{})
	sh := sharing.New(st, l, notes, platform.SystemClock{})
	sg, err := sh.Create(ctx, u, sharing.GroupInput{Name: "Wyjazd do Pragi", FromGroupID: &trip.ID})
	if err != nil {
		return uuid.Nil, err
	}
	// Bo, a friend, on the trip; the cinema of 28 January is a shared expense (split_expense)
	if err := addTripMember(ctx, st, u, sg.ID, notes, sh); err != nil {
		return uuid.Nil, err
	}
	if cinema == uuid.Nil {
		return uuid.Nil, errors.New("the demo year has no cinema on 2025-01-28")
	}
	if _, err := l.Annotate(ctx, u, []uuid.UUID{cinema}, ledger.AnnotationPatch{SetGroup: true, GroupID: &trip.ID}); err != nil {
		return uuid.Nil, err
	}
	// the advice library's starter articles, published (search_advice, ADR 0034)
	if err := publishStarter(ctx, st); err != nil {
		return uuid.Nil, err
	}
	// a savings goal of the year (goal_status, update_goal)
	target, start := int64(500_000), platform.MustDate(2025, 1, 1)
	if _, err := goals.New(st, platform.SystemClock{}).Create(ctx, u, goals.Input{Name: "Wakacje", Kind: goals.KindSave,
		TargetMinor: &target, StartsOn: &start, Deadline: platform.MustDate(2025, 8, 31)}); err != nil {
		return uuid.Nil, err
	}
	return u, nil
}

// publishStarter adds the advice library's starter articles and publishes them (the advice table is
// the instance's; an eval runs on its own database).
func publishStarter(ctx context.Context, st *store.Store) error {
	lib := advice.New(st, platform.SystemClock{})
	if _, err := lib.AddStarter(ctx); err != nil {
		return err
	}
	list, err := lib.List(ctx)
	if err != nil {
		return err
	}
	for i := range list {
		a, err := lib.Get(ctx, list[i].ID)
		if err != nil {
			return err
		}
		if a.Status == advice.Published {
			continue
		}
		if _, err := lib.Update(ctx, a.ID, a.Version, advice.Input{Slug: a.Slug, Language: a.Language, Title: a.Title, Body: a.Body,
			Tags: a.Tags, Status: advice.Published}); err != nil {
			return err
		}
	}
	return nil
}

// addTripMember makes Bo the user's friend and a member of the shared group.
func addTripMember(ctx context.Context, st *store.Store, u, groupID uuid.UUID, notes *notify.Service, sh *sharing.Service) error {
	bo := platform.NewID()
	if err := st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.CreateUser(ctx, db.CreateUserParams{ID: bo, Email: "bo-" + bo.String() + "@example.test", PasswordHash: "x"}); err != nil {
			return err
		}
		return q.SetUserDisplayName(ctx, db.SetUserDisplayNameParams{ID: bo, DisplayName: "Bo"})
	}); err != nil {
		return err
	}
	fr := friends.New(st, platform.SystemClock{}, notes)
	code, err := fr.NewCode(ctx, u)
	if err != nil {
		return err
	}
	if _, err := fr.Redeem(ctx, bo, code.Code); err != nil {
		return err
	}
	if _, err := sh.Invite(ctx, u, groupID, bo); err != nil {
		return err
	}
	_, err = sh.Accept(ctx, bo, groupID)
	return err
}
