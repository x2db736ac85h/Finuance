package rules

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Result counts what a reclassification set.
type Result struct {
	Categorised   int64 // category writes by rules (a row matched by two rules counts twice)
	RuleTransfers int64 // transactions marked as transfers by rules
	Pairs         int   // transfer pairs found this time
}

// Reclassify recomputes everything automatic for the user, from scratch, in one transaction
// (ADR 0017, ADR 0018):
//
//  1. categories set by rules go back to the bank's category;
//  2. transfers marked by rules, and pairs that lost their other half, are unmarked;
//  3. the enabled rules run in position order: a category action overwrites any category that
//     isn't manual (the last match wins), a transfer action marks unmarked rows;
//  4. transfer detection pairs the remaining unmarked rows (DetectTransfers).
//
// The result depends only on the rules, the transactions and the user's manual decisions, never
// on the order of earlier runs. It takes every account's writer lock, so it doesn't interleave with
// commits, deletes or re-materialisations.
func (s *Service) Reclassify(ctx context.Context, userID uuid.UUID) (Result, error) {
	var out Result
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, tx pgx.Tx) error {
		if err := q.LockUserAccounts(ctx, userID); err != nil {
			return err
		}
		if _, err := q.ResetRuleCategories(ctx, userID); err != nil {
			return err
		}
		if _, err := q.ResetAutomaticTransfers(ctx, userID); err != nil {
			return err
		}
		rows, err := q.ListRules(ctx, userID)
		if err != nil {
			return err
		}
		for i := range rows {
			r := &rows[i]
			if !r.Enabled {
				continue
			}
			m, err := unmarshalMatch(r.Match)
			if err != nil {
				return err
			}
			if r.SetCategoryID != nil {
				n, err := query.ApplyRuleCategory(ctx, tx, userID, r.ID, *r.SetCategoryID, m.Search, m.Filters)
				if err != nil {
					return err
				}
				out.Categorised += n
			}
			if r.MarkTransfer {
				n, err := query.ApplyRuleTransfer(ctx, tx, userID, m.Search, m.Filters)
				if err != nil {
					return err
				}
				out.RuleTransfers += n
			}
		}
		out.Pairs, err = detectTransfers(ctx, q, userID)
		return err
	})
	return out, ledger.DBError("reclassify", err)
}

// detectTransfers pairs unmarked transactions between the user's accounts (PLAN task 15.3): an
// outgoing amount with the same incoming amount in another account, booked at most 3 days apart.
// The candidates come smallest date gap first (then the older rows), and each transaction joins
// at most one pair: a greedy match that gives the same pairs on every run.
func detectTransfers(ctx context.Context, q *db.Queries, userID uuid.UUID) (int, error) {
	cands, err := q.TransferCandidates(ctx, userID)
	if err != nil {
		return 0, err
	}
	used := map[uuid.UUID]bool{}
	var outs, ins []uuid.UUID
	for _, c := range cands {
		if used[c.OutID] || used[c.InID] {
			continue
		}
		used[c.OutID], used[c.InID] = true, true
		outs, ins = append(outs, c.OutID), append(ins, c.InID)
	}
	if len(outs) == 0 {
		return 0, nil
	}
	_, err = q.PairTransfers(ctx, db.PairTransfersParams{UserID: userID, OutIds: outs, InIds: ins})
	return len(outs), err
}

// Preview shows what a match selects now (count, manual rows, the newest rows).
func Preview(ctx context.Context, e *query.Engine, userID uuid.UUID, m Match) (query.RulePreview, error) {
	if !matchesSomething(m) {
		return query.RulePreview{}, invalidf("a rule needs a search or at least one column filter")
	}
	return e.PreviewRule(ctx, userID, m.Search, m.Filters, PreviewSample)
}
