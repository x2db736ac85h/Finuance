package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Action proposals (PLAN task 15.7, ADR 0019): propose_action never changes anything. It reads
// what the change would touch and returns a Proposal; the panel shows it as a confirmation card,
// and Apply sends the change through the normal endpoints with the user's session.

// The proposal actions.
const (
	ActionSetCategory = "set_category"
	ActionSetGroup    = "set_group"
	ActionCreateRule  = "create_rule"
)

// Limits of a proposal: the rows a set_* proposal may touch (the bulk endpoint takes 5,000; a
// proposal is meant to be reviewed), the sample rows the card shows, and the names.
const (
	MaxProposalRows  = 1000
	proposalSample   = 5
	maxProposalName  = 80 // category and rule names (ledger maxCategoryName)
	maxProposalGroup = 40 // ledger maxGroupName
	maxReason        = 300
)

// proposalCap is MaxProposalRows (a variable so tests can lower it).
var proposalCap = MaxProposalRows

// Proposal is a change the assistant proposes. IDs are the affected transactions of a set_*
// action, read when it was proposed (what the user reviews is what Apply changes); a create_rule
// carries the rule's Match. CategoryID / GroupID are set when the target exists already;
// otherwise Apply creates it first.
type Proposal struct {
	Action       string         `json:"action"`
	Reason       string         `json:"reason,omitempty"`
	Matched      int            `json:"matched"`
	IDs          []uuid.UUID    `json:"ids,omitempty"`
	CategoryName string         `json:"categoryName,omitempty"`
	CategoryID   *uuid.UUID     `json:"categoryId,omitempty"`
	GroupName    string         `json:"groupName,omitempty"`
	GroupID      *uuid.UUID     `json:"groupId,omitempty"`
	RuleName     string         `json:"ruleName,omitempty"`
	Match        *ProposalMatch `json:"match,omitempty"`
	Sample       []ProposalRow  `json:"sample"`
	Filter       *StepFilter    `json:"filter,omitempty"` // set_*: the rows' lookup filter, for "Show in table"
}

// Key identifies the proposal within its chat: a hash of its stored form, so the streamed step
// and the chat read back later give the same key (the activity log records applied ones).
func (p *Proposal) Key() string {
	b, _ := json.Marshal(p) // plain data; it can't fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// ProposalMatch is a create_rule match in the API's RuleMatch form.
type ProposalMatch struct {
	Search  string           `json:"search,omitempty"`
	Filters []ProposalFilter `json:"filters,omitempty"`
}

// ProposalFilter is one column condition of a ProposalMatch.
type ProposalFilter struct {
	Column string `json:"column"`
	Cond   string `json:"cond"`
}

// ProposalRow is a sample row of the card (unmasked: the card is the user's own screen).
type ProposalRow struct {
	ID          uuid.UUID `json:"id"`
	Date        string    `json:"date"`
	Payee       string    `json:"payee"`
	AmountMinor int64     `json:"amountMinor"`
	Currency    string    `json:"currency"`
	Category    string    `json:"category,omitempty"`
}

// proposalResult is what the model receives.
type proposalResult struct {
	Status          string      `json:"status"`
	Action          string      `json:"action"`
	Matched         int         `json:"matched"`
	ToCategory      string      `json:"to_category,omitempty"`
	CreatesCategory bool        `json:"creates_category,omitempty"`
	ToGroup         string      `json:"to_group,omitempty"`
	CreatesGroup    bool        `json:"creates_group,omitempty"`
	RuleName        string      `json:"rule_name,omitempty"`
	Sample          []sampleOut `json:"sample"`
}

type sampleOut struct {
	Date     string      `json:"date"`
	Payee    string      `json:"payee"`
	Amount   json.Number `json:"amount"`
	Category string      `json:"category,omitempty"`
}

const proposedStatus = "Proposed: shown to the user as a confirmation card. Nothing has changed; the user decides with Apply. Do not say the change is done."

func proposalError(msg string) Outcome {
	return outcome(map[string]string{"error": msg}, "propose action · not proposed")
}

// sameName compares names ignoring case, accents and spacing.
func sameName(a, b string) bool {
	return ingest.Collapse(ingest.Norm(a)) == ingest.Collapse(ingest.Norm(b))
}

func cleanText(s string, limit int) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	return s, s != "" && utf8.RuneCountInString(s) <= limit && !strings.ContainsRune(s, 0)
}

func (t *Tools) propose(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	action := a.str("action")
	reason, ok := cleanText(a.str("reason"), maxReason)
	if !ok {
		reason = "" // optional; too long is left out
	}
	switch action {
	case ActionSetCategory, ActionCreateRule:
		name, ok := cleanText(a.str("to_category"), maxProposalName)
		if !ok {
			return proposalError(fmt.Sprintf("to_category must be a category name of 1–%d characters", maxProposalName)), nil
		}
		p := &Proposal{Action: action, Reason: reason, CategoryName: name, Sample: []ProposalRow{}}
		cats, err := t.eng.CategoryRefs(ctx, userID)
		if err != nil {
			return Outcome{}, err
		}
		for _, c := range cats {
			if sameName(c.Name, name) {
				p.CategoryName, p.CategoryID = c.Name, &c.ID
				break
			}
		}
		if action == ActionCreateRule {
			return t.proposeRule(ctx, userID, sc, a, p)
		}
		return t.proposeRows(ctx, userID, sc, a, p)
	case ActionSetGroup:
		name, ok := cleanText(a.str("to_group"), maxProposalGroup)
		if !ok {
			return proposalError(fmt.Sprintf("to_group must be a group name of 1–%d characters", maxProposalGroup)), nil
		}
		p := &Proposal{Action: action, Reason: reason, GroupName: name, Sample: []ProposalRow{}}
		groups, err := t.eng.Groups(ctx, userID)
		if err != nil {
			return Outcome{}, err
		}
		for i := range groups {
			if sameName(groups[i].Name, name) {
				p.GroupName, p.GroupID = groups[i].Name, &groups[i].ID
				break
			}
		}
		return t.proposeRows(ctx, userID, sc, a, p)
	}
	return proposalError("action must be set_category, set_group or create_rule"), nil
}

// proposeRows reads the rows a set_* action would change: the filter within the chat's scope,
// as search_transactions reads them.
func (t *Tools) proposeRows(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	tf := a.filter()
	if tf.Label == "all rows" {
		return proposalError("Give a filter (query, payee, category, group, dates, type or amounts) that selects the transactions to change"), nil
	}
	c := sc.Context
	c.ToolFilter = &tf
	var rows []query.ToolRow
	err := t.eng.Insight(ctx, userID, c, func(in *query.Insight) error {
		tot, err := in.Totals(true)
		if err != nil {
			return err
		}
		p.Matched = int(tot.Count)
		if p.Matched == 0 || p.Matched > proposalCap {
			return nil
		}
		rows, err = in.ToolRows("date_desc", proposalCap)
		return err
	})
	if err != nil {
		return Outcome{}, err
	}
	switch {
	case p.Matched == 0:
		return proposalError("No transactions match the filter; nothing to propose"), nil
	case p.Matched > proposalCap:
		return proposalError(fmt.Sprintf("%d transactions match; a proposal may change at most %d. Narrow the filter.", p.Matched, proposalCap)), nil
	}
	p.IDs = make([]uuid.UUID, len(rows))
	for i := range rows {
		p.IDs[i] = rows[i].ID
		if i < proposalSample {
			r := &rows[i]
			p.Sample = append(p.Sample, ProposalRow{ID: r.ID, Date: r.BookedOn.String(), Payee: ingest.Collapse(r.Payee),
				AmountMinor: r.AmountMinor, Currency: r.Currency, Category: r.Category})
		}
	}
	p.Filter = stepFilter(&tf)
	return proposalOutcome(sc, p, tf.Label), nil
}

// proposeRule builds a rule "payee contains … and/or the search words" → the category and counts
// what it matches now (every account, all time, as rules apply).
func (t *Tools) proposeRule(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	m := &ProposalMatch{}
	if q, ok := cleanText(a.str("query"), 200); ok {
		m.Search = q
	}
	if payee, ok := cleanText(a.str("payee"), 200); ok {
		if strings.ContainsAny(payee[:1], "!=<>") || strings.Contains(payee, "..") {
			return proposalError("payee must be plain text the payee contains"), nil
		}
		m.Filters = []ProposalFilter{{Column: "payee", Cond: payee}}
	}
	if m.Search == "" && len(m.Filters) == 0 {
		return proposalError("A rule needs payee (text the payee contains) and/or query (words that must all appear)"), nil
	}
	p.Match = m
	name, ok := cleanText(a.str("rule_name"), maxProposalName)
	if !ok {
		name = m.Search
		if len(m.Filters) > 0 {
			name = m.Filters[0].Cond
		}
		name = string([]rune(name)[:min(maxProposalName, utf8.RuneCountInString(name))])
	}
	p.RuleName = name
	var filters []query.Filter
	for _, f := range m.Filters {
		filters = append(filters, query.Filter{Column: f.Column, Cond: f.Cond})
	}
	pv, err := t.eng.PreviewRule(ctx, userID, m.Search, filters, proposalSample)
	if err != nil {
		return Outcome{}, err
	}
	p.Matched = int(pv.Matched)
	for i := range pv.Sample {
		r := &pv.Sample[i]
		row := ProposalRow{ID: r.ID, Date: r.BookedOn.String(), Payee: ingest.Collapse(r.Payee), AmountMinor: r.AmountMinor, Currency: r.Currency}
		if r.CategoryName != nil {
			row.Category = *r.CategoryName
		}
		p.Sample = append(p.Sample, row)
	}
	return proposalOutcome(sc, p, "rule "+name), nil
}

func proposalOutcome(sc Scope, p *Proposal, label string) Outcome {
	r := proposalResult{Status: proposedStatus, Action: p.Action, Matched: p.Matched, RuleName: p.RuleName, Sample: []sampleOut{}}
	switch p.Action {
	case ActionSetGroup:
		r.ToGroup, r.CreatesGroup = p.GroupName, p.GroupID == nil
	default:
		r.ToCategory, r.CreatesCategory = p.CategoryName, p.CategoryID == nil
	}
	for _, s := range p.Sample {
		r.Sample = append(r.Sample, sampleOut{Date: s.Date, Payee: sc.mask(s.Payee), Amount: amount(s.AmountMinor, s.Currency), Category: s.Category})
	}
	out := outcome(r, fmt.Sprintf("proposed %s · %s → %d %s", strings.ReplaceAll(p.Action, "_", " "), label, p.Matched,
		plural(p.Matched, "transaction", "transactions")))
	out.Proposal, out.Matched = p, &p.Matched
	return out
}
