package assistant

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestProposalKey: the key is stable through the JSON the step is stored as (the streamed card
// and the chat read back later must agree) and differs between proposals.
func TestProposalKey(t *testing.T) {
	cat := uuid.New()
	floor := "-100"
	p := &Proposal{Action: "set_category", Reason: "r", Matched: 2, IDs: []uuid.UUID{uuid.New(), uuid.New()}, CategoryName: "Internet",
		CategoryID: &cat, Sample: []ProposalRow{{ID: uuid.New(), Date: "2025-01-02", Payee: "X", AmountMinor: -100, Currency: "PLN"}},
		Filter: &StepFilter{Payee: "X", MinAmount: &floor, Label: "payee X"}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back Proposal
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if k := p.Key(); len(k) != 16 || back.Key() != k {
		t.Errorf("key %q, after a round trip %q", k, back.Key())
	}
	other := *p
	other.Matched = 3
	if other.Key() == p.Key() {
		t.Error("different proposals share a key")
	}
	empty := &Proposal{Action: "create_rule"}
	var emptyBack Proposal
	eb, _ := json.Marshal(empty)
	_ = json.Unmarshal(eb, &emptyBack)
	if emptyBack.Key() != empty.Key() {
		t.Error("a proposal without a sample changes its key through JSON")
	}
}
