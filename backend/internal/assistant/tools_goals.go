package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/goals"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Goals (Phase 20, ADR 0031): goal_status reads the user's goals with their progress, and the
// create_goal / update_goal proposals resolve a goal the panel creates or changes through
// POST /goals and PATCH /goals/{id}. The goals service reads under store.WithUserReadOnly.

// ToolGoals is the goals lookup.
const ToolGoals = "goal_status"

// WithGoals gives the tools the goals service (nil leaves goal_status and the goal proposals
// unavailable).
func (t *Tools) WithGoals(g *goals.Service) *Tools {
	t.goals = g
	return t
}

// ProposalGoal is a create_goal's new goal, or an update_goal's goal before or after the change,
// in the API's GoalInput / GoalUpdate form plus names.
type ProposalGoal struct {
	ID             *uuid.UUID `json:"id,omitempty"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	TargetMinor    *int64     `json:"targetMinor,omitempty"`
	Currency       string     `json:"currency"`
	AccountID      *uuid.UUID `json:"accountId,omitempty"`
	AccountName    string     `json:"accountName,omitempty"`
	CategoryID     *uuid.UUID `json:"categoryId,omitempty"`
	CategoryName   string     `json:"categoryName,omitempty"`
	ReducePct      int        `json:"reducePct,omitempty"`
	BaselineMonths int        `json:"baselineMonths"`
	Deadline       string     `json:"deadline"`
	Status         string     `json:"status,omitempty"`  // update_goal
	Version        int        `json:"version,omitempty"` // update_goal: the version read
}

type goalOut struct {
	Name     string      `json:"name"`
	Kind     string      `json:"kind"`
	Status   string      `json:"status"`
	Progress string      `json:"progress"`
	Percent  int         `json:"percent"`
	Currency string      `json:"currency"`
	Started  string      `json:"started"`
	Deadline string      `json:"deadline"`
	DaysLeft int         `json:"days_left"`
	Account  string      `json:"account,omitempty"`
	Target   json.Number `json:"target,omitempty"`
	Saved    json.Number `json:"saved,omitempty"`
	Expected json.Number `json:"expected_by_today,omitempty"`
	Project  json.Number `json:"projected_by_deadline,omitempty"`
	Category string      `json:"category,omitempty"`
	Reduce   int         `json:"reduce_pct,omitempty"`
	Baseline json.Number `json:"baseline_per_month,omitempty"`
	Limit    json.Number `json:"monthly_limit,omitempty"`
	This     json.Number `json:"this_month,omitempty"`
	Allowed  json.Number `json:"allowed_so_far,omitempty"`
	Kept     *int        `json:"months_kept,omitempty"`
	Past     *int        `json:"months_past,omitempty"`
}

type goalsResult struct {
	Goals   []goalOut `json:"goals"`
	Omitted int       `json:"closed_goals_omitted,omitempty"`
	Note    string    `json:"note"`
}

const goalsNote = "save: saved = income minus spending since the start (or the growth of its account); on_track = at least 95 % of the steady pace. " +
	"reduce: each month's spending in the category against the monthly limit (the baseline average less reduce_pct); " +
	"on_track = this month's spending within its allowance so far. needs_attention: the category is gone or there was no spending to reduce."

func goalJSON(g *goals.Goal) goalOut {
	p := g.Progress
	o := goalOut{Name: g.Name, Kind: g.Kind, Status: g.Status, Progress: p.State, Percent: p.Percent, Currency: g.Currency,
		Started: g.StartsOn.String(), Deadline: g.Deadline.String(), DaysLeft: p.DaysLeft, Account: g.AccountName, Category: g.CategoryName}
	if g.Kind == goals.KindSave {
		if g.TargetMinor != nil {
			o.Target = amount(*g.TargetMinor, g.Currency)
		}
		o.Saved, o.Expected, o.Project = amount(p.SavedMinor, g.Currency), amount(p.ExpectedMinor, g.Currency), amount(p.ProjectedMinor, g.Currency)
		return o
	}
	if g.ReducePct != nil {
		o.Reduce = *g.ReducePct
	}
	o.Baseline, o.Limit = amount(p.BaselineMinor, g.Currency), amount(p.MonthlyLimitMinor, g.Currency)
	o.This, o.Allowed = amount(p.ThisMonthMinor, g.Currency), amount(p.AllowedSoFarMinor, g.Currency)
	o.Kept, o.Past = &p.MonthsKept, &p.MonthsPast
	return o
}

// findGoal resolves a goal by name ignoring case, accents and spacing, or a unique part of it.
func findGoal(list []goals.Goal, name string) (*goals.Goal, []string) {
	var part []*goals.Goal
	names := make([]string, len(list))
	for i := range list {
		names[i] = list[i].Name
		if sameName(list[i].Name, name) {
			return &list[i], nil
		}
		if strings.Contains(norm(list[i].Name), norm(name)) {
			part = append(part, &list[i])
		}
	}
	if len(part) == 1 {
		return part[0], nil
	}
	return nil, names
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// goalStatus: every open goal, or the one named.
func (t *Tools) goalStatus(ctx context.Context, userID uuid.UUID, a args) (Outcome, error) {
	if t.goals == nil {
		return outcome(map[string]string{"error": "Goals are not available"}, "goal status · unavailable"), nil
	}
	list, err := t.goals.List(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	r := goalsResult{Goals: []goalOut{}, Note: goalsNote}
	if want := strings.TrimSpace(a.str("goal")); want != "" {
		g, names := findGoal(list, want)
		if g == nil {
			return outcome(map[string]any{"error": fmt.Sprintf("No goal %q", want), "goals": names}, "goal status · no such goal"), nil
		}
		r.Goals = append(r.Goals, goalJSON(g))
		return outcome(r, "goal status · "+g.Name), nil
	}
	for i := range list {
		if list[i].Status != goals.StatusOpen {
			r.Omitted++
			continue
		}
		r.Goals = append(r.Goals, goalJSON(&list[i]))
	}
	if len(list) == 0 {
		r.Note = "The user has no goals yet. They can set one in Overview → Goals, or you can propose one with propose_action create_goal."
	}
	return outcome(r, fmt.Sprintf("goal status · %d %s", len(r.Goals), plural(len(r.Goals), "goal", "goals"))), nil
}

// goalArgs reads the arguments create_goal and update_goal share into g (an update's are all
// optional); a message is for the model when one is wrong.
func (t *Tools) goalArgs(ctx context.Context, userID uuid.UUID, a args, g *ProposalGoal, create bool) (string, error) {
	if s := a.str("goal_name"); s != "" || create {
		name, ok := cleanText(s, maxProposalName)
		if !ok {
			return fmt.Sprintf("goal_name must be a short name for the goal, 1–%d characters", maxProposalName), nil
		}
		g.Name = name
	}
	if s := a.str("deadline"); s != "" || create {
		d := query.ParseToolDate(s)
		if d == nil {
			return "deadline must be the goal's last day, YYYY-MM-DD", nil
		}
		if today := t.goals.Today(); !d.After(today) {
			return fmt.Sprintf("deadline must be after today (%s)", today), nil
		}
		g.Deadline = d.String()
	}
	if g.Kind == goals.KindSave {
		if want := strings.TrimSpace(a.str("account")); want != "" {
			accs, err := t.eng.Accounts(ctx, userID)
			if err != nil {
				return "", err
			}
			var names []string
			for i := range accs {
				names = append(names, accs[i].Name)
				if sameName(accs[i].Name, want) {
					g.AccountID, g.AccountName, g.Currency = &accs[i].ID, accs[i].Name, accs[i].Currency
				}
			}
			if g.AccountID == nil {
				return fmt.Sprintf("There is no account %q; the accounts are: %s", want, strings.Join(names, ", ")), nil
			}
		}
		if s := a.str("amount"); s != "" || create { // in the account's currency
			v, ok := positiveMinor(s, g.Currency)
			if !ok {
				return "amount must be the amount to put aside, a positive number", nil
			}
			g.TargetMinor = &v
		}
		return "", nil
	}
	if s := a.str("to_category"); s != "" || create {
		name, ok := cleanText(s, maxProposalName)
		if !ok {
			return "to_category must be the name of the category to spend less on", nil
		}
		c, err := t.findCategory(ctx, userID, name)
		if err != nil {
			return "", err
		}
		if c == nil {
			return fmt.Sprintf("There is no category %q (find_values with field category lists them)", name), nil
		}
		g.CategoryID, g.CategoryName = &c.ID, c.Name
	}
	if s := a.str("reduce_pct"); s != "" || create {
		v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(s), "%"))
		if err != nil || v < 1 || v > 100 {
			return "reduce_pct must be the percentage to spend less, 1–100", nil
		}
		g.ReducePct = v
	}
	return "", nil
}

// proposeGoal: create_goal and update_goal.
func (t *Tools) proposeGoal(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	if t.goals == nil {
		return proposalError("Goals are not available"), nil
	}
	if p.Action == ActionCreateGoal {
		g := &ProposalGoal{Kind: a.str("goal_kind"), BaselineMonths: 3}
		if g.Kind == "" && (a.str("reduce_pct") != "" || (a.str("to_category") != "" && a.str("amount") == "")) {
			g.Kind = goals.KindReduce
		} else if g.Kind == "" {
			g.Kind = goals.KindSave
		}
		if g.Kind != goals.KindSave && g.Kind != goals.KindReduce {
			return proposalError("goal_kind must be save (put an amount aside) or reduce (spend a percentage less on a category)"), nil
		}
		var err error
		if g.Currency, err = t.mainCurrency(ctx, userID); err != nil {
			return Outcome{}, err
		}
		msg, err := t.goalArgs(ctx, userID, a, g, true)
		if err != nil || msg != "" {
			return proposalError(msg), err
		}
		p.Goal, p.Matched = g, 1
		return proposalOutcome(sc, p, g.Name), nil
	}
	list, err := t.goals.List(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	want := strings.TrimSpace(a.str("goal"))
	cur, names := findGoal(list, want)
	if want == "" || cur == nil {
		return proposalError(fmt.Sprintf("goal must name one of the user's goals: %s", strings.Join(names, ", "))), nil
	}
	before := proposalGoalOf(cur)
	after := before
	msg, err := t.goalArgs(ctx, userID, a, &after, false)
	if err != nil || msg != "" {
		return proposalError(msg), err
	}
	if s := a.str("goal_status"); s != "" {
		if s != goals.StatusOpen && s != goals.StatusDone && s != goals.StatusShelf {
			return proposalError("goal_status must be active, done or archived"), nil
		}
		after.Status = s
	}
	if reflect.DeepEqual(before, after) {
		return proposalError("Nothing to change: give a new goal_name, amount, reduce_pct, to_category, deadline or goal_status"), nil
	}
	p.Goal, p.GoalBefore, p.Matched = &after, &before, 1
	return proposalOutcome(sc, p, cur.Name), nil
}

// goalProposed is a proposed goal as the model reads it.
type goalProposed struct {
	Name     string      `json:"name"`
	Kind     string      `json:"kind"`
	Target   json.Number `json:"target,omitempty"`
	Currency string      `json:"currency"`
	Account  string      `json:"account,omitempty"`
	Category string      `json:"category,omitempty"`
	Reduce   int         `json:"reduce_pct,omitempty"`
	Deadline string      `json:"deadline"`
	Status   string      `json:"status,omitempty"`
}

func goalProposedOf(sc Scope, g *ProposalGoal) *goalProposed {
	if g == nil {
		return nil
	}
	o := &goalProposed{Name: g.Name, Kind: g.Kind, Currency: g.Currency, Account: sc.mask(g.AccountName), Category: g.CategoryName,
		Reduce: g.ReducePct, Deadline: g.Deadline, Status: g.Status}
	if g.TargetMinor != nil {
		o.Target = amount(*g.TargetMinor, g.Currency)
	}
	return o
}

func proposalGoalOf(g *goals.Goal) ProposalGoal {
	out := ProposalGoal{ID: &g.ID, Name: g.Name, Kind: g.Kind, TargetMinor: g.TargetMinor, Currency: g.Currency, AccountID: g.AccountID,
		AccountName: g.AccountName, CategoryID: g.CategoryID, CategoryName: g.CategoryName, BaselineMonths: g.BaselineMonths,
		Deadline: g.Deadline.String(), Status: g.Status, Version: g.Version}
	if g.ReducePct != nil {
		out.ReducePct = *g.ReducePct
	}
	return out
}
