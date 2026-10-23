package query

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// A rule's match is a Context with only a search and column filters (PLAN task 15.2): it compiles
// exactly like the table's filters, over every account and all time, transfers included.
func ruleMatch(userID uuid.UUID, search string, filters []Filter) (*Compiled, error) {
	return Compile(userID, Context{Search: search, Filters: filters, IncludeTransfers: true})
}

// matching is the subquery of the ids a compiled rule match selects.
func matching(c *Compiled) string {
	return "SELECT t.id FROM " + source(c.joins()...) + " WHERE " + where(c.Where)
}

// ValidateRuleMatch checks a rule's match the way the table checks its filters.
func ValidateRuleMatch(userID uuid.UUID, search string, filters []Filter) error {
	_, err := ruleMatch(userID, search, filters)
	return err
}

// ApplyRuleCategory gives the matching transactions categoryID, recording the rule, in the caller's
// transaction (rules.Reclassify). Manual categories are never touched; a category an earlier rule
// set is overwritten, so the last matching rule wins (ADR 0017). Returns the rows changed.
func ApplyRuleCategory(ctx context.Context, tx pgx.Tx, userID, ruleID, categoryID uuid.UUID, search string, filters []Filter) (int64, error) {
	c, err := ruleMatch(userID, search, filters)
	if err != nil {
		return 0, err
	}
	sql := "UPDATE transactions t SET category_id = " + c.Add(categoryID) + ", category_source = 'rule', category_rule_id = " + c.Add(ruleID) +
		" WHERE t.user_id = " + c.Add(userID) + " AND t.category_source <> 'manual' AND t.id IN (" + matching(c) + ")"
	tag, err := tx.Exec(ctx, sql, c.Args...)
	return tag.RowsAffected(), err
}

// ApplyRuleTransfer marks the matching transactions as transfers (transfer_source 'rule'). Rows
// already marked (a detected pair, an earlier rule, the user) are left alone.
func ApplyRuleTransfer(ctx context.Context, tx pgx.Tx, userID uuid.UUID, search string, filters []Filter) (int64, error) {
	c, err := ruleMatch(userID, search, filters)
	if err != nil {
		return 0, err
	}
	sql := "UPDATE transactions t SET is_transfer = true, transfer_source = 'rule'" +
		" WHERE t.user_id = " + c.Add(userID) + " AND t.transfer_source = 'none' AND t.id IN (" + matching(c) + ")"
	tag, err := tx.Exec(ctx, sql, c.Args...)
	return tag.RowsAffected(), err
}

// RulePreview is what a rule's match selects now: every matching transaction, the ones whose
// category was set by hand (a category action leaves those alone), and the newest rows.
type RulePreview struct {
	Matched, Manual int64
	Sample          []Row
}

// PreviewRule counts and samples a rule's match, read-only.
func (e *Engine) PreviewRule(ctx context.Context, userID uuid.UUID, search string, filters []Filter, sample int) (RulePreview, error) {
	var out RulePreview
	err := e.st.WithUserReadOnly(ctx, userID, func(_ *db.Queries, tx pgx.Tx) error {
		c, err := ruleMatch(userID, search, filters)
		if err != nil {
			return err
		}
		counts := "SELECT count(*), count(*) FILTER (WHERE t.category_source = 'manual') FROM " + source(c.joins()...) + " WHERE " + where(c.Where)
		if err := tx.QueryRow(ctx, counts, c.Args...).Scan(&out.Matched, &out.Manual); err != nil {
			return err
		}
		rc, err := ruleMatch(userID, search, filters)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, windowFirst(source(rc.joins()...)+" WHERE "+where(rc.Where)+" ORDER BY "+tieBreak+
			" LIMIT "+rc.Add(sample), rowColumns, tieBreak), rc.Args...)
		if err != nil {
			return err
		}
		out.Sample, err = pgx.CollectRows(rows, scanRow)
		return err
	})
	return out, err
}
