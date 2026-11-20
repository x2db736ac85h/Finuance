package assistant

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// TestMayAutoApply: the server's rule for applying a proposal without a click (AI-1).
func TestMayAutoApply(t *testing.T) {
	on := []string{ActionSetCategory, ActionSetBudget}
	for name, tc := range map[string]struct {
		p    Proposal
		kind string
		ok   bool
	}{
		"a kind that is on":            {Proposal{Action: ActionSetCategory, Matched: 12}, "chat", true},
		"exactly the row limit":        {Proposal{Action: ActionSetCategory, Matched: AutoApplyMaxRows}, "chat", true},
		"a kind that is off":           {Proposal{Action: ActionSetGroup, Matched: 3}, "chat", false},
		"one row too many":             {Proposal{Action: ActionSetCategory, Matched: AutoApplyMaxRows + 1}, "chat", false},
		"more ids than it says":        {Proposal{Action: ActionSetCategory, Matched: 1, IDs: make([]uuid.UUID, AutoApplyMaxRows+1)}, "chat", false},
		"a digest's suggestion":        {Proposal{Action: ActionSetCategory, Matched: 2}, KindDigest, false},
		"a shared kind, even if saved": {Proposal{Action: ActionAddToSharedGroup, Matched: 2}, "chat", false},
	} {
		allowed := on
		if tc.p.Action == ActionAddToSharedGroup {
			allowed = append([]string{ActionAddToSharedGroup}, on...) // as if an old row still listed it
		}
		err := MayAutoApply(&tc.p, tc.kind, allowed)
		if (err == nil) != tc.ok {
			t.Errorf("%s: %v, want allowed=%v", name, err, tc.ok)
		}
		if err != nil && !errors.Is(err, platform.ErrForbidden) {
			t.Errorf("%s: %v is not a refusal", name, err)
		}
	}
}
