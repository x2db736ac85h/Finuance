package insights

import (
	"context"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// GroupTopPayees is the number of payees in a group report.
const GroupTopPayees = 10

// GroupReport is the group window's breakdown (PLAN 15.9): the group's transactions over all
// accounts, ignoring filters and with transfers included, as the group stats count them. Amounts
// are minor units as they are; the group's currency is that of its stats (ledger.GroupStat).
type GroupReport struct {
	Count          int64
	SpentMinor     int64
	IncomeMinor    int64
	Range          query.DateRange
	Days           int   // first to last transaction, inclusive; 1 for an empty group
	AvgPerDayMinor int64 // money out per day over Days
	Daily          []query.DaySpend
	Categories     []CategoryShare
	Payees         []query.PayeeSpend
	PayeeCount     int
}

// GroupReport breaks one group down. A group the user doesn't have reads as an empty one; the
// caller checks the group first.
func (s *Service) GroupReport(ctx context.Context, userID, groupID uuid.UUID) (GroupReport, error) {
	var out GroupReport
	c := query.Context{GroupID: &groupID, IncludeTransfers: true}
	err := s.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		t, err := in.Totals(true)
		if err != nil {
			return err
		}
		out.Count, out.SpentMinor, out.IncomeMinor, out.Range = t.Count, t.SpentMinor, t.IncomeMinor, t.Range
		out.Days = Days(nil, query.DateRange{}, t.Range)
		out.AvgPerDayMinor = DivRound(t.SpentMinor, int64(out.Days))
		if t.Range.Min != nil && t.Range.Max != nil {
			if out.Daily, err = in.DailySpend(true, *t.Range.Min, t.Range.Max.AddDays(1)); err != nil {
				return err
			}
		}
		cats, err := in.CategorySpend()
		if err != nil {
			return err
		}
		out.Categories = make([]CategoryShare, len(cats))
		for i, x := range cats {
			out.Categories[i] = CategoryShare{CategorySpend: x, Share: Share(x.SpentMinor, t.SpentMinor)}
		}
		out.Payees, out.PayeeCount, err = in.PayeeSpend(true, GroupTopPayees)
		return err
	})
	return out, err
}
