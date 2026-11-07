package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/advice"
)

// The advice library (Phase 20, ADR 0034): search_advice finds published articles an administrator
// wrote, read only in the user's context, so the model answers general money questions from them
// and names them.

// ToolAdvice is the advice library lookup.
const ToolAdvice = "search_advice"

// WithAdvice gives the tools the advice library (nil: search_advice answers that there is none).
func (t *Tools) WithAdvice(a *advice.Service) *Tools {
	t.advice = a
	return t
}

const adviceNote = "General information written by this instance's administrator, not financial advice. Base the answer on it, " +
	"name the article's title, and say it is general information. If no article fits, answer briefly and say the library has none."

type adviceResult struct {
	Articles []advice.Hit `json:"articles"`
	Note     string       `json:"note"`
}

func (t *Tools) searchAdvice(ctx context.Context, userID uuid.UUID, a args) (Outcome, error) {
	if t.advice == nil {
		return outcome(map[string]string{"error": "There is no advice library on this instance"}, "advice · unavailable"), nil
	}
	q := strings.TrimSpace(a.str("query"))
	if q == "" {
		return outcome(map[string]string{"error": "query must be the topic to look up, e.g. emergency fund"}, "advice · invalid arguments"), nil
	}
	hits, err := t.advice.Search(ctx, userID, q, a.str("language"), advice.MaxResults)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(adviceResult{Articles: hits, Note: adviceNote},
		fmt.Sprintf("advice · %d %s", len(hits), plural(len(hits), "article", "articles"))), nil
}
