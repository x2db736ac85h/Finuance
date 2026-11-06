package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Proposals v2 (Phase 20, ADR 0030): a budget, an expense typed into a cash account, and
// transactions shared with a shared group. Like the first three they only read (the targets and
// what would change) and are applied by the panel through the normal endpoints, which record the
// change as the assistant's.

const maxExpensePayee = 200 // ledger maxManualPayee

// findCategory resolves a category by name ignoring case, accents and spacing.
func (t *Tools) findCategory(ctx context.Context, userID uuid.UUID, name string) (*query.NamedRef, error) {
	cats, err := t.eng.CategoryRefs(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range cats {
		if sameName(cats[i].Name, name) {
			return &cats[i], nil
		}
	}
	return nil, nil
}

// mainCurrency is the currency budgets are kept in: that of the user's first account (budgets are
// per category, in the currency the overview shows).
func (t *Tools) mainCurrency(ctx context.Context, userID uuid.UUID) (string, error) {
	accs, err := t.eng.Accounts(ctx, userID)
	if err != nil || len(accs) == 0 {
		return "PLN", err
	}
	return accs[0].Currency, nil
}

// positiveMinor reads a positive amount in major units ("1500", "12,50") into minor units.
func positiveMinor(s, currency string) (int64, bool) {
	exp, ok := money.Exponent(currency)
	if !ok {
		exp = money.DefaultExponent
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "-")
	v, err := money.ParseMinor(s, exp)
	return v, err == nil && v > 0
}

// proposeBudget: set_budget sets or removes a category's monthly budget.
func (t *Tools) proposeBudget(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	name, ok := cleanText(a.str("to_category"), maxProposalName)
	if !ok {
		return proposalError("to_category must be the name of an existing category"), nil
	}
	c, err := t.findCategory(ctx, userID, name)
	if err != nil {
		return Outcome{}, err
	}
	if c == nil {
		return proposalError(fmt.Sprintf("There is no category %q; budgets are per existing category (find_values with field category lists them)", name)), nil
	}
	p.CategoryName, p.CategoryID = c.Name, &c.ID
	if p.Currency, err = t.mainCurrency(ctx, userID); err != nil {
		return Outcome{}, err
	}
	budgets, err := t.eng.Budgets(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	if v, ok := budgets[c.ID]; ok {
		p.BudgetBeforeMinor = &v
	}
	if a.truthy("remove_budget") {
		if p.BudgetBeforeMinor == nil {
			return proposalError(fmt.Sprintf("%s has no budget of its own to remove", c.Name)), nil
		}
		p.RemoveBudget = true
	} else {
		v, ok := positiveMinor(a.str("amount"), p.Currency)
		if !ok {
			return proposalError("amount must be the monthly budget, a positive number (or remove_budget: true)"), nil
		}
		p.BudgetMinor = &v
	}
	p.Matched = 1
	return proposalOutcome(sc, p, "budget "+c.Name), nil
}

// proposeExpense: add_expense types a payment into one of the user's cash accounts (bank accounts
// get their rows from imported files).
func (t *Tools) proposeExpense(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	accs, err := t.eng.Accounts(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	var cash []query.AccountRef
	for _, acc := range accs {
		if acc.Kind == "cash" {
			cash = append(cash, acc)
		}
	}
	names := func() string {
		out := make([]string, len(cash))
		for i := range cash {
			out[i] = cash[i].Name
		}
		return strings.Join(out, ", ")
	}
	var acc *query.AccountRef
	switch want := strings.TrimSpace(a.str("account")); {
	case len(cash) == 0:
		return proposalError("The user has no cash account; expenses are typed into cash accounts (they can add one with New cash account in the sidebar)"), nil
	case want == "" && len(cash) == 1:
		acc = &cash[0]
	case want == "":
		return proposalError("Name the cash account (account): " + names()), nil
	default:
		for i := range cash {
			if sameName(cash[i].Name, want) {
				acc = &cash[i]
			}
		}
		if acc == nil {
			return proposalError(fmt.Sprintf("%q is not a cash account; the cash accounts are: %s", want, names())), nil
		}
	}
	d := query.ParseToolDate(a.str("date"))
	if d == nil {
		return proposalError("date must be the day of the payment, YYYY-MM-DD"), nil
	}
	v, ok := positiveMinor(a.str("amount"), acc.Currency)
	if !ok {
		return proposalError("amount must be the amount paid, a positive number"), nil
	}
	payee, ok := cleanText(a.str("payee"), maxExpensePayee)
	if !ok {
		return proposalError(fmt.Sprintf("payee must say who was paid, 1–%d characters", maxExpensePayee)), nil
	}
	p.AccountID, p.AccountName, p.Currency = &acc.ID, acc.Name, acc.Currency
	p.Date, p.AmountMinor, p.Payee = d.String(), -v, payee
	if note, ok := cleanText(a.str("note"), 500); ok {
		p.Note = note
	}
	if name, ok := cleanText(a.str("to_category"), maxProposalName); ok {
		p.CategoryName = name
		c, err := t.findCategory(ctx, userID, name)
		if err != nil {
			return Outcome{}, err
		}
		if c != nil {
			p.CategoryName, p.CategoryID = c.Name, &c.ID
		}
	}
	p.Matched = 1
	return proposalOutcome(sc, p, payee), nil
}

// proposeShared: add_to_shared_group puts transactions into the user's personal group linked to a
// shared group, which shares a copy of each with its members (ADR 0028).
func (t *Tools) proposeShared(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	links, err := t.eng.SharedLinks(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	if len(links) == 0 {
		return proposalError("The user is in no shared group (they share a group with friends from the Shared tab)"), nil
	}
	want := strings.TrimSpace(a.str("shared_group"))
	var link *query.SharedLink
	for i := range links {
		if sameName(links[i].SharedName, want) || sameName(links[i].GroupName, want) {
			link = &links[i]
		}
	}
	if link == nil {
		names := make([]string, len(links))
		for i := range links {
			names[i] = links[i].SharedName
		}
		return proposalError("shared_group must be one of: " + strings.Join(names, ", ")), nil
	}
	p.GroupID, p.GroupName = &link.GroupID, link.GroupName
	p.SharedGroupID, p.SharedGroupName = &link.SharedGroupID, link.SharedName
	return t.proposeRows(ctx, userID, sc, a, p)
}

// meWords are how a user names themselves in a split ("split it between me and Bo").
var meWords = map[string]bool{"me": true, "i": true, "myself": true, "ja": true, "mnie": true, "ich": true, "moi": true, "yo": true}

// proposeSplit: split_expense changes how one of the user's own expenses in a shared group is
// split: equally between named members (one member: "for someone else"), by shares, or by exact
// amounts summing to the expense.
func (t *Tools) proposeSplit(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	links, err := t.eng.SharedLinks(ctx, userID)
	if err != nil {
		return Outcome{}, err
	}
	want := strings.TrimSpace(a.str("shared_group"))
	var link *query.SharedLink
	for i := range links {
		if sameName(links[i].SharedName, want) || sameName(links[i].GroupName, want) {
			link = &links[i]
		}
	}
	if link == nil {
		if len(links) == 0 {
			return proposalError("The user is in no shared group"), nil
		}
		names := make([]string, len(links))
		for i := range links {
			names[i] = links[i].SharedName
		}
		return proposalError("shared_group must be one of: " + strings.Join(names, ", ")), nil
	}
	members, expenses, err := t.eng.SharedGroup(ctx, userID, link.SharedGroupID)
	if err != nil {
		return Outcome{}, err
	}
	// the expense: one of the user's own (only they may change its split), by payee and date
	payee := ingest.Collapse(ingest.Norm(a.str("payee")))
	day := query.ParseToolDate(a.str("date"))
	var hits []query.SharedExpense
	for i := range expenses {
		x := &expenses[i]
		if !x.Mine || (payee != "" && !strings.Contains(ingest.Collapse(ingest.Norm(x.Payee)), payee)) || (day != nil && x.BookedOn != *day) {
			continue
		}
		hits = append(hits, *x)
	}
	list := func(xs []query.SharedExpense) string {
		var b strings.Builder
		for i := range xs {
			x := &xs[i]
			if i == 8 {
				b.WriteString(", …")
				break
			}
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%s %s %s", x.BookedOn, sc.mask(x.Payee), amount(x.AmountMinor, link.Currency))
		}
		return b.String()
	}
	switch {
	case payee == "" && day == nil:
		return proposalError("Say which expense: payee (and date when there are several)"), nil
	case len(hits) == 0:
		var mine []query.SharedExpense
		for i := range expenses {
			if expenses[i].Mine {
				mine = append(mine, expenses[i])
			}
		}
		if len(mine) == 0 {
			return proposalError("The user added no expense to " + link.SharedName + "; only the member who added one can change its split"), nil
		}
		return proposalError("No expense of the user's in " + link.SharedName + " matches; theirs are: " + list(mine)), nil
	case len(hits) > 1:
		return proposalError(fmt.Sprintf("%d expenses match; give the date: %s", len(hits), list(hits))), nil
	}
	x := hits[0]
	p.SharedGroupID, p.SharedGroupName, p.ExpenseID = &link.SharedGroupID, link.SharedName, &x.ID
	p.Date, p.AmountMinor, p.Currency, p.Payee, p.Note, p.ExpenseCategory, p.PaidBy = x.BookedOn.String(), x.AmountMinor, link.Currency, x.Payee, x.Note, x.Category, &x.PaidBy

	// the members, by name (or "me"), active ones only
	var active []query.SharedMember
	for _, m := range members {
		if m.Active {
			active = append(active, m)
		}
	}
	activeNames := func() string {
		out := make([]string, len(active))
		for i, m := range active {
			out[i] = m.Name
		}
		return strings.Join(out, ", ")
	}
	resolve := func(name string) (query.SharedMember, bool) {
		n := strings.ToLower(strings.TrimSpace(name))
		for _, m := range active {
			if (meWords[n] && m.Me) || sameName(m.Name, name) {
				return m, true
			}
		}
		return query.SharedMember{}, false
	}
	raw, _ := a["members"].([]any)
	mode := a.or("split_mode", "equal")
	split := &ProposalSplit{Mode: mode}
	switch mode {
	case "equal", "shares", "exact":
	default:
		return proposalError("split_mode must be equal, shares or exact"), nil
	}
	if len(raw) == 0 && mode == "equal" {
		for _, m := range active {
			split.Members = append(split.Members, ProposalShare{MemberID: m.ID, Name: m.Name})
		}
	}
	var sum int64
	for _, r := range raw {
		item, _ := r.(map[string]any)
		ma := args(item)
		name := ma.str("name")
		if name == "" {
			name, _ = r.(string) // ["me", "Bo"] for an equal split
		}
		m, ok := resolve(name)
		if !ok {
			return proposalError(fmt.Sprintf("%q is not an active member of %s; the members are: %s", name, link.SharedName, activeNames())), nil
		}
		sh := ProposalShare{MemberID: m.ID, Name: m.Name}
		switch mode {
		case "shares":
			w := ma.count("share", 0, 1000)
			if w < 1 {
				return proposalError("shares needs a share of 1–1000 for every member"), nil
			}
			sh.Weight = w
		case "exact":
			v, ok := positiveMinor(ma.str("amount"), link.Currency)
			if !ok {
				return proposalError("exact needs a positive amount for every member"), nil
			}
			sh.AmountMinor = v
			sum += v
		}
		split.Members = append(split.Members, sh)
	}
	if len(split.Members) == 0 {
		return proposalError("members must name who shares it (shares and exact need a share or an amount for each)"), nil
	}
	if mode == "exact" {
		total := x.AmountMinor
		if total < 0 {
			total = -total
		}
		if sum != total {
			return proposalError(fmt.Sprintf("The exact amounts add up to %s, the expense is %s", amount(sum, link.Currency), amount(total, link.Currency))), nil
		}
		if x.AmountMinor < 0 { // shares carry the expense's sign
			for i := range split.Members {
				split.Members[i].AmountMinor = -split.Members[i].AmountMinor
			}
		}
	}
	p.Split, p.Matched = split, 1
	return proposalOutcome(sc, p, x.Payee), nil
}
