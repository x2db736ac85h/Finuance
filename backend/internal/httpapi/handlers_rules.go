package httpapi

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"
)

// reclassifyIn enqueues the user's reclassification in the caller's transaction (rule changes).
func (a *api) reclassifyIn(userID uuid.UUID) rules.Enqueue {
	return func(ctx context.Context, tx pgx.Tx) error {
		if a.jobs == nil {
			return nil
		}
		_, err := a.jobs.InsertTx(ctx, tx, jobs.ReclassifyArgs{UserID: userID}, nil)
		return err
	}
}

// reclassifyAfter enqueues a reclassification after a change committed in its own transaction
// (deleting an import, a duplicate decision, a category merge). The change stands either way; a
// failed insert is logged and the next change (or "Apply rules") catches up.
func (a *api) reclassifyAfter(ctx context.Context, userID uuid.UUID) {
	if a.jobs == nil {
		return
	}
	if _, err := a.jobs.Insert(ctx, jobs.ReclassifyArgs{UserID: userID}, nil); err != nil {
		observability.Logger(ctx).ErrorContext(ctx, "enqueue reclassify", "error", err)
	}
}

// CategoryStats lists every category with its counts (Settings → Categories).
func (a *api) CategoryStats(ctx context.Context, _ gen.CategoryStatsRequestObject) (gen.CategoryStatsResponseObject, error) {
	stats, err := a.ledger.CategoryStats(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.CategoryStats200JSONResponse, len(stats))
	for i, s := range stats {
		out[i] = gen.CategoryStat{Id: s.ID, Name: s.Name, TxCount: s.TxCount, ManualCount: s.ManualCount, RuleCount: s.RuleCount,
			Aliases: append([]string{}, s.Aliases...)}
	}
	return out, nil
}

// CreateCategory adds a category.
func (a *api) CreateCategory(ctx context.Context, req gen.CreateCategoryRequestObject) (gen.CreateCategoryResponseObject, error) {
	c, err := a.ledger.CreateCategory(ctx, identity(ctx).User.ID, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return gen.CreateCategory201JSONResponse{Id: c.ID, Name: c.Name}, nil
}

// RenameCategory renames a category (the old bank name keeps mapping to it).
func (a *api) RenameCategory(ctx context.Context, req gen.RenameCategoryRequestObject) (gen.RenameCategoryResponseObject, error) {
	c, err := a.ledger.RenameCategory(ctx, identity(ctx).User.ID, req.Id, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return gen.RenameCategory200JSONResponse{Id: c.ID, Name: c.Name}, nil
}

// DeleteCategory deletes a category; its transactions become uncategorised.
func (a *api) DeleteCategory(ctx context.Context, req gen.DeleteCategoryRequestObject) (gen.DeleteCategoryResponseObject, error) {
	u := identity(ctx).User.ID
	if err := a.ledger.DeleteCategory(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.reclassifyAfter(ctx, u) // rules that set it lost their action
	return gen.DeleteCategory204Response{}, nil
}

// MergeCategory merges a category into another one.
func (a *api) MergeCategory(ctx context.Context, req gen.MergeCategoryRequestObject) (gen.MergeCategoryResponseObject, error) {
	u := identity(ctx).User.ID
	n, err := a.ledger.MergeCategory(ctx, u, req.Id, req.Body.IntoId)
	if err != nil {
		return nil, err
	}
	a.reclassifyAfter(ctx, u)
	return gen.MergeCategory200JSONResponse{Moved: n}, nil
}

func matchIn(m gen.RuleMatch) rules.Match {
	out := rules.Match{Search: deref(m.Search)}
	for _, f := range deref(m.Filters) {
		out.Filters = append(out.Filters, query.Filter{Column: string(f.Column), Cond: deref(f.Cond), Excluded: deref(f.Excluded)})
	}
	return out
}

func matchDTO(m rules.Match) gen.RuleMatch {
	out := gen.RuleMatch{}
	if m.Search != "" {
		out.Search = &m.Search
	}
	if len(m.Filters) > 0 {
		fs := make([]gen.ColumnFilter, len(m.Filters))
		for i, f := range m.Filters {
			fs[i] = gen.ColumnFilter{Column: gen.FilterColumn(f.Column)}
			if f.Cond != "" {
				fs[i].Cond = &f.Cond
			}
			if len(f.Excluded) > 0 {
				fs[i].Excluded = &f.Excluded
			}
		}
		out.Filters = &fs
	}
	return out
}

func ruleInputIn(b gen.RuleInput) rules.Input {
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return rules.Input{Name: b.Name, Enabled: enabled, Match: matchIn(b.Match), SetCategoryID: b.SetCategoryId, MarkTransfer: deref(b.MarkTransfer)}
}

func ruleDTO(r *rules.Rule) gen.Rule {
	return gen.Rule{Id: r.ID, Name: r.Name, Enabled: r.Enabled, Position: r.Position, Match: matchDTO(r.Match),
		SetCategory: namedRef(r.SetCategoryID, r.CategoryName), MarkTransfer: r.MarkTransfer, UpdatedAt: r.UpdatedAt}
}

func rulesDTO(rs []rules.Rule) []gen.Rule {
	out := make([]gen.Rule, len(rs))
	for i := range rs {
		out[i] = ruleDTO(&rs[i])
	}
	return out
}

// ListRules returns the rules in the order they apply.
func (a *api) ListRules(ctx context.Context, _ gen.ListRulesRequestObject) (gen.ListRulesResponseObject, error) {
	rs, err := a.rules.List(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.ListRules200JSONResponse(rulesDTO(rs)), nil
}

// CreateRule adds a rule at the end.
func (a *api) CreateRule(ctx context.Context, req gen.CreateRuleRequestObject) (gen.CreateRuleResponseObject, error) {
	u := identity(ctx).User.ID
	r, err := a.rules.Create(ctx, u, ruleInputIn(*req.Body), a.reclassifyIn(u))
	if err != nil {
		return nil, err
	}
	return gen.CreateRule201JSONResponse(ruleDTO(&r)), nil
}

// UpdateRule replaces a rule.
func (a *api) UpdateRule(ctx context.Context, req gen.UpdateRuleRequestObject) (gen.UpdateRuleResponseObject, error) {
	u := identity(ctx).User.ID
	r, err := a.rules.Update(ctx, u, req.Id, ruleInputIn(*req.Body), a.reclassifyIn(u))
	if err != nil {
		return nil, err
	}
	return gen.UpdateRule200JSONResponse(ruleDTO(&r)), nil
}

// DeleteRule removes a rule.
func (a *api) DeleteRule(ctx context.Context, req gen.DeleteRuleRequestObject) (gen.DeleteRuleResponseObject, error) {
	u := identity(ctx).User.ID
	if err := a.rules.Delete(ctx, u, req.Id, a.reclassifyIn(u)); err != nil {
		return nil, err
	}
	return gen.DeleteRule204Response{}, nil
}

// ReorderRules sets the order the rules apply in.
func (a *api) ReorderRules(ctx context.Context, req gen.ReorderRulesRequestObject) (gen.ReorderRulesResponseObject, error) {
	u := identity(ctx).User.ID
	rs, err := a.rules.Reorder(ctx, u, req.Body.Ids, a.reclassifyIn(u))
	if err != nil {
		return nil, err
	}
	return gen.ReorderRules200JSONResponse(rulesDTO(rs)), nil
}

// PreviewRule shows what a match selects now.
func (a *api) PreviewRule(ctx context.Context, req gen.PreviewRuleRequestObject) (gen.PreviewRuleResponseObject, error) {
	p, err := rules.Preview(ctx, a.query, identity(ctx).User.ID, matchIn(req.Body.Match))
	if err != nil {
		return nil, err
	}
	out := gen.PreviewRule200JSONResponse{Matched: p.Matched, Manual: p.Manual, Sample: make([]gen.TransactionRow, len(p.Sample))}
	for i := range p.Sample {
		out.Sample[i] = txRowDTO(&p.Sample[i])
	}
	return out, nil
}

// ApplyRules queues a reclassification.
func (a *api) ApplyRules(ctx context.Context, _ gen.ApplyRulesRequestObject) (gen.ApplyRulesResponseObject, error) {
	if a.jobs != nil {
		if _, err := a.jobs.Insert(ctx, jobs.ReclassifyArgs{UserID: identity(ctx).User.ID}, nil); err != nil {
			return nil, err
		}
	}
	return gen.ApplyRules202Response{}, nil
}
