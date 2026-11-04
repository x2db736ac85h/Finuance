package insights

import (
	"cmp"
	"slices"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Trips (Phase 19, 19.9): pure rules for the Shared tab. The timeline lists personal and shared
// groups with their span, totals and largest category, sums them per year and adds "next time"
// hints that compare a group with the others of its kind. TripReport is one shared group's
// cards. Amounts are minor units, positive = money spent.

// Hint thresholds.
const (
	HintRatio          = 1.5  // a hint needs at least 1.5× the comparison
	HintMinShare       = 0.10 // a category hint needs at least 10 % of the group's spending
	HintMinOthers      = 2    // compared with at least two other groups of the same kind
	HintNewestGroups   = 10   // hints only for the newest groups
	TripReportTopItems = 10
)

// Hint kinds.
const (
	HintCategoryHigh = "category_high"
	HintPerDayHigh   = "per_day_high"
	HintOverBudget   = "over_budget"
)

// NamedSpend is spending on one category or payee.
type NamedSpend struct {
	Name  string
	Spent int64
	Count int64
}

// TripGroup is one group on the timeline as the caller passes it in.
type TripGroup struct {
	ID          uuid.UUID
	Source      string // personal | shared
	Name        string
	Color       int
	Kind        string // personal, or the shared group's kind
	Currency    string
	FirstOn     *platform.Date // first and last expense
	LastOn      *platform.Date
	StartsOn    *platform.Date // a shared group's own dates, when set
	EndsOn      *platform.Date
	Spent       int64
	Count       int64
	BudgetMinor *int64
	Categories  []NamedSpend
}

// TimelineGroup is a TripGroup with its derived numbers.
type TimelineGroup struct {
	TripGroup
	Days     int
	PerDay   int64
	Top      *NamedSpend
	TopShare float64
}

// TimelineYear sums a year's groups (by the group's last day).
type TimelineYear struct {
	Year     int
	Groups   int
	Spent    int64 // in the year's most common currency; groups in another currency are left out
	Currency string
	Days     int
}

// Hint is a "next time" fact.
type Hint struct {
	GroupID  uuid.UUID
	Kind     string
	Category string
	Ratio    float64
}

// Timeline is the Shared tab's overview.
type Timeline struct {
	Groups []TimelineGroup
	Years  []TimelineYear
	Hints  []Hint
}

// SpanDays is a group's length in days: its own dates when both are set, else from the first to
// the last expense, inclusive; 1 for an empty group.
func SpanDays(startsOn, endsOn, firstOn, lastOn *platform.Date) int {
	from, to := firstOn, lastOn
	if startsOn != nil && endsOn != nil && !endsOn.Before(*startsOn) {
		from, to = startsOn, endsOn
	}
	if from == nil || to == nil {
		return 1
	}
	return max(1, int(to.Time().Sub(from.Time()).Hours()/24)+1)
}

// perDay divides, rounding half away from zero.
func perDay(total int64, days int) int64 {
	if days <= 0 {
		return total
	}
	d := int64(days)
	if total < 0 {
		return -((-total + d/2) / d)
	}
	return (total + d/2) / d
}

// BuildTimeline derives the numbers, sorts newest first, sums the years and finds the hints.
func BuildTimeline(in []TripGroup) Timeline {
	out := Timeline{Groups: make([]TimelineGroup, 0, len(in)), Years: []TimelineYear{}, Hints: []Hint{}}
	for i := range in {
		g := TimelineGroup{TripGroup: in[i]}
		g.Categories = slices.Clone(g.Categories)
		slices.SortFunc(g.Categories, func(a, b NamedSpend) int {
			return cmp.Or(cmp.Compare(b.Spent, a.Spent), cmp.Compare(a.Name, b.Name))
		})
		g.Days = SpanDays(g.StartsOn, g.EndsOn, g.FirstOn, g.LastOn)
		g.PerDay = perDay(g.Spent, g.Days)
		if len(g.Categories) > 0 && g.Spent > 0 && g.Categories[0].Spent > 0 {
			top := g.Categories[0]
			g.Top, g.TopShare = &top, float64(top.Spent)/float64(g.Spent)
		}
		out.Groups = append(out.Groups, g)
	}
	slices.SortStableFunc(out.Groups, func(a, b TimelineGroup) int {
		return cmp.Or(-compareDates(lastDay(a), lastDay(b)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	out.Years = years(out.Groups)
	out.Hints = hints(out.Groups)
	return out
}

func lastDay(g TimelineGroup) *platform.Date {
	if g.EndsOn != nil {
		return g.EndsOn
	}
	return g.LastOn
}

func compareDates(a, b *platform.Date) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return a.Compare(*b)
}

func years(groups []TimelineGroup) []TimelineYear {
	type acc struct {
		groups, days int
		byCur        map[string]int64
		count        map[string]int
	}
	by := map[int]*acc{}
	for i := range groups {
		g := &groups[i]
		d := lastDay(*g)
		if d == nil || g.Count == 0 {
			continue
		}
		a := by[d.Y]
		if a == nil {
			a = &acc{byCur: map[string]int64{}, count: map[string]int{}}
			by[d.Y] = a
		}
		a.groups++
		a.days += g.Days
		a.byCur[g.Currency] += g.Spent
		a.count[g.Currency]++
	}
	out := make([]TimelineYear, 0, len(by))
	for y, a := range by {
		cur := ""
		for c, n := range a.count {
			if n > a.count[cur] || (n == a.count[cur] && c < cur) {
				cur = c
			}
		}
		out = append(out, TimelineYear{Year: y, Groups: a.groups, Spent: a.byCur[cur], Currency: cur, Days: a.days})
	}
	slices.SortFunc(out, func(a, b TimelineYear) int { return cmp.Compare(b.Year, a.Year) })
	return out
}

func median(v []int64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return (float64(s[n/2-1]) + float64(s[n/2])) / 2
}

// hints compares each of the newest groups with the other groups of its kind and currency: its
// spending per day against their median, and each of its larger categories per day against the
// average of the others that have it. A group over its budget gets a hint too. One hint per kind
// per group, the strongest first.
func hints(groups []TimelineGroup) []Hint {
	out := []Hint{}
	for i := range groups {
		if i >= HintNewestGroups {
			break
		}
		g := &groups[i]
		if g.Count == 0 || g.Spent <= 0 {
			continue
		}
		if g.BudgetMinor != nil && *g.BudgetMinor > 0 && g.Spent > *g.BudgetMinor {
			out = append(out, Hint{GroupID: g.ID, Kind: HintOverBudget, Ratio: float64(g.Spent) / float64(*g.BudgetMinor)})
		}
		var others []*TimelineGroup
		for j := range groups {
			o := &groups[j]
			if j != i && o.Kind == g.Kind && o.Currency == g.Currency && o.Count > 0 && o.Spent > 0 {
				others = append(others, o)
			}
		}
		if len(others) < HintMinOthers {
			continue
		}
		perDays := make([]int64, len(others))
		for k, o := range others {
			perDays[k] = o.PerDay
		}
		if m := median(perDays); m > 0 && float64(g.PerDay)/m >= HintRatio {
			out = append(out, Hint{GroupID: g.ID, Kind: HintPerDayHigh, Ratio: float64(g.PerDay) / m})
		}
		var best *Hint
		for _, c := range g.Categories {
			if c.Spent <= 0 || float64(c.Spent)/float64(g.Spent) < HintMinShare {
				continue
			}
			var sum float64
			n := 0
			for _, o := range others {
				for _, oc := range o.Categories {
					if oc.Name == c.Name && oc.Spent > 0 {
						sum += float64(oc.Spent) / float64(o.Days)
						n++
					}
				}
			}
			if n < HintMinOthers {
				continue
			}
			ratio := (float64(c.Spent) / float64(g.Days)) / (sum / float64(n))
			if ratio >= HintRatio && (best == nil || ratio > best.Ratio) {
				best = &Hint{GroupID: g.ID, Kind: HintCategoryHigh, Category: c.Name, Ratio: ratio}
			}
		}
		if best != nil {
			out = append(out, *best)
		}
	}
	return out
}

// TripExpense is one shared expense for the report.
type TripExpense struct {
	ID       uuid.UUID
	PaidBy   uuid.UUID
	BookedOn platform.Date
	Amount   int64
	Payee    string
	Category string
	Shares   map[uuid.UUID]int64
}

// MemberPaidShare is what a member paid and what their share was.
type MemberPaidShare struct {
	Member      uuid.UUID
	Paid, Share int64
}

// DaySpend is the spending of one day.
type DaySpend struct {
	Date  platform.Date
	Spent int64
	Count int64
}

// TripReport is one shared group's cards.
type TripReport struct {
	Total           int64
	Count           int64
	FirstOn, LastOn *platform.Date
	Days            int
	AvgPerDay       int64
	Daily           []DaySpend
	Categories      []NamedSpend
	Payees          []NamedSpend
	Largest         []uuid.UUID // expense ids, largest first
	Members         []MemberPaidShare
}

// BuildTripReport computes the report of a group's expenses; members gives the order of the paid vs
// share rows (every member, also those with nothing).
func BuildTripReport(exps []TripExpense, members []uuid.UUID, startsOn, endsOn *platform.Date) TripReport {
	r := TripReport{Daily: []DaySpend{}, Categories: []NamedSpend{}, Payees: []NamedSpend{}, Largest: []uuid.UUID{}}
	daily := map[platform.Date]*DaySpend{}
	cats, payees := map[string]*NamedSpend{}, map[string]*NamedSpend{}
	paid, share := map[uuid.UUID]int64{}, map[uuid.UUID]int64{}
	add := func(m map[string]*NamedSpend, name string, amount int64) {
		x := m[name]
		if x == nil {
			x = &NamedSpend{Name: name}
			m[name] = x
		}
		x.Spent += amount
		x.Count++
	}
	for i := range exps {
		e := &exps[i]
		r.Total += e.Amount
		r.Count++
		if r.FirstOn == nil || e.BookedOn.Before(*r.FirstOn) {
			d := e.BookedOn
			r.FirstOn = &d
		}
		if r.LastOn == nil || e.BookedOn.After(*r.LastOn) {
			d := e.BookedOn
			r.LastOn = &d
		}
		day := daily[e.BookedOn]
		if day == nil {
			day = &DaySpend{Date: e.BookedOn}
			daily[e.BookedOn] = day
		}
		day.Spent += e.Amount
		day.Count++
		add(cats, e.Category, e.Amount)
		add(payees, e.Payee, e.Amount)
		paid[e.PaidBy] += e.Amount
		for m, v := range e.Shares {
			share[m] += v
		}
	}
	r.Days = SpanDays(startsOn, endsOn, r.FirstOn, r.LastOn)
	r.AvgPerDay = perDay(r.Total, r.Days)
	for _, d := range daily {
		r.Daily = append(r.Daily, *d)
	}
	slices.SortFunc(r.Daily, func(a, b DaySpend) int { return a.Date.Compare(b.Date) })
	bySpent := func(a, b NamedSpend) int { return cmp.Or(cmp.Compare(b.Spent, a.Spent), cmp.Compare(a.Name, b.Name)) }
	for _, c := range cats {
		r.Categories = append(r.Categories, *c)
	}
	slices.SortFunc(r.Categories, bySpent)
	for _, p := range payees {
		r.Payees = append(r.Payees, *p)
	}
	slices.SortFunc(r.Payees, bySpent)
	if len(r.Payees) > TripReportTopItems {
		r.Payees = r.Payees[:TripReportTopItems]
	}
	order := make([]int, len(exps))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(cmp.Compare(exps[b].Amount, exps[a].Amount), exps[b].BookedOn.Compare(exps[a].BookedOn))
	})
	for _, i := range order[:min(len(order), TripReportTopItems)] {
		r.Largest = append(r.Largest, exps[i].ID)
	}
	for _, m := range members {
		r.Members = append(r.Members, MemberPaidShare{Member: m, Paid: paid[m], Share: share[m]})
	}
	return r
}
