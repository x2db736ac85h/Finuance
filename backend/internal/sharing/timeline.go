package sharing

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Timeline lists the user's personal groups with transactions and the shared groups they are an
// active member of, with the year sums and hints (insights.BuildTimeline). A personal group linked
// to a shared group appears once, as the shared group.
func (s *Service) Timeline(ctx context.Context, userID uuid.UUID) (insights.Timeline, error) {
	personal, err := s.ledger.GroupStats(ctx, userID)
	if err != nil {
		return insights.Timeline{}, err
	}
	shared, err := s.List(ctx, userID)
	if err != nil {
		return insights.Timeline{}, err
	}
	var in []insights.TripGroup
	err = s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		cats, err := q.GroupCategorySpend(ctx, userID)
		if err != nil {
			return err
		}
		byGroup := map[uuid.UUID][]insights.NamedSpend{}
		for _, c := range cats {
			byGroup[c.GroupID] = append(byGroup[c.GroupID], insights.NamedSpend{Name: c.Category, Spent: c.SpentMinor, Count: c.TxCount})
		}
		for i := range personal {
			p := &personal[i]
			if p.Count == 0 || p.SharedGroupID != nil {
				continue
			}
			in = append(in, insights.TripGroup{ID: p.ID, Source: "personal", Name: p.Name, Color: p.Color, Kind: "personal",
				Currency: p.Currency, FirstOn: p.FirstOn, LastOn: p.LastOn, Spent: p.SpentMinor, Count: int64(p.Count),
				BudgetMinor: p.BudgetMinor, Categories: byGroup[p.ID]})
		}
		stats, err := q.SharedTimelineStats(ctx)
		if err != nil {
			return err
		}
		sharedCats, err := q.SharedCategorySpend(ctx)
		if err != nil {
			return err
		}
		statOf := map[uuid.UUID]db.SharedTimelineStatsRow{}
		for _, st := range stats {
			statOf[st.GroupID] = st
		}
		catsOf := map[uuid.UUID][]insights.NamedSpend{}
		for _, c := range sharedCats {
			catsOf[c.GroupID] = append(catsOf[c.GroupID], insights.NamedSpend{Name: c.Category, Spent: c.SpentMinor, Count: c.Expenses})
		}
		for i := range shared {
			g := &shared[i]
			if g.Me.State != "active" {
				continue
			}
			t := insights.TripGroup{ID: g.ID, Source: "shared", Name: g.Name, Color: g.Color, Kind: g.Kind, Currency: g.Currency,
				StartsOn: g.StartsOn, EndsOn: g.EndsOn, BudgetMinor: g.BudgetMinor, Categories: catsOf[g.ID]}
			if st, ok := statOf[g.ID]; ok && st.Expenses > 0 {
				first, last := st.FirstOn, st.LastOn
				t.FirstOn, t.LastOn, t.Spent, t.Count = &first, &last, st.TotalMinor, st.Expenses
			}
			in = append(in, t)
		}
		return nil
	})
	if err != nil {
		return insights.Timeline{}, ledger.DBError("timeline", err)
	}
	return insights.BuildTimeline(in), nil
}

// Report returns a group's detail with its report (the group page's cards). An invited user has
// no expenses to report on: ErrState.
func (s *Service) Report(ctx context.Context, userID, groupID uuid.UUID) (Detail, insights.TripReport, error) {
	d, err := s.Get(ctx, userID, groupID)
	if err != nil {
		return d, insights.TripReport{}, err
	}
	if d.Group.Me.State != "active" {
		return d, insights.TripReport{}, statef("accept the invitation first")
	}
	exps := make([]insights.TripExpense, len(d.Expenses))
	for i := range d.Expenses {
		e := &d.Expenses[i]
		shares := make(map[uuid.UUID]int64, len(e.Shares))
		for _, sh := range e.Shares {
			shares[sh.Member] = sh.Minor
		}
		exps[i] = insights.TripExpense{ID: e.ID, PaidBy: e.PaidBy, BookedOn: e.BookedOn, Amount: e.AmountMinor, Payee: e.Payee,
			Category: e.Category, Shares: shares}
	}
	members := make([]uuid.UUID, 0, len(d.Members))
	for i := range d.Members {
		if d.Members[i].State != "invited" {
			members = append(members, d.Members[i].ID)
		}
	}
	return d, insights.BuildTripReport(exps, members, d.Group.StartsOn, d.Group.EndsOn), nil
}
