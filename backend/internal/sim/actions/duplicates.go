package actions

import (
	"context"
	"net/http"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Duplicates: the duplicates window (rows that overlapping exports brought twice), preferring one
// import's version, keeping both, and clearing a decision.
func duplicates() []sim.Action {
	return []sim.Action{
		{Name: "duplicates.list", Area: "duplicates", Needs: sim.NeedData, Run: duplicatesList},
		{Name: "duplicates.prefer", Area: "duplicates", Needs: sim.NeedData, Run: duplicatePrefer},
		{Name: "duplicates.keep-both", Area: "duplicates", Needs: sim.NeedData, Run: duplicateKeepBoth},
		{Name: "duplicates.clear", Area: "duplicates", Needs: sim.NeedData, Run: duplicateClear},
	}
}

func duplicatePage(ctx context.Context, a *sim.Actor) (*gen.DuplicatePage, error) {
	r, err := a.API.ListDuplicatesWithResponse(ctx, &gen.ListDuplicatesParams{Limit: ptr(50)})
	if err != nil {
		return nil, err
	}
	if err := sim.Expect("duplicates", r.HTTPResponse, http.StatusOK); err != nil {
		return nil, err
	}
	return r.JSON200, nil
}

func duplicatesList(ctx context.Context, a *sim.Actor) error {
	_, err := duplicatePage(ctx, a)
	return err
}

// someDuplicate is one group of the duplicates window, decided or not as asked, or nothing.
func someDuplicate(ctx context.Context, a *sim.Actor, decided bool) (*gen.DuplicateGroup, error) {
	page, err := duplicatePage(ctx, a)
	if err != nil {
		return nil, err
	}
	var pick []*gen.DuplicateGroup
	for i := range page.Groups {
		if (page.Groups[i].Override != nil) == decided && len(page.Groups[i].Versions) > 0 {
			pick = append(pick, &page.Groups[i])
		}
	}
	if len(pick) == 0 {
		return nil, sim.ErrNothingToDo
	}
	return pick[a.IntN(len(pick))], nil
}

func duplicatePrefer(ctx context.Context, a *sim.Actor) error {
	g, err := someDuplicate(ctx, a, false)
	if err != nil {
		return err
	}
	v := g.Versions[a.IntN(len(g.Versions))]
	r, err := a.API.DecideDuplicateWithResponse(ctx, g.AccountId, g.Fingerprint, gen.DecideDuplicateJSONRequestBody{Mode: gen.Prefer, ImportId: &v.ImportId})
	if err != nil {
		return err
	}
	return sim.Expect("prefer one version", r.HTTPResponse, http.StatusOK)
}

func duplicateKeepBoth(ctx context.Context, a *sim.Actor) error {
	g, err := someDuplicate(ctx, a, false)
	if err != nil {
		return err
	}
	r, err := a.API.DecideDuplicateWithResponse(ctx, g.AccountId, g.Fingerprint, gen.DecideDuplicateJSONRequestBody{Mode: gen.KeepBoth})
	if err != nil {
		return err
	}
	return sim.Expect("keep both", r.HTTPResponse, http.StatusOK)
}

func duplicateClear(ctx context.Context, a *sim.Actor) error {
	g, err := someDuplicate(ctx, a, true)
	if err != nil {
		return err
	}
	r, err := a.API.ResetDuplicateWithResponse(ctx, g.AccountId, g.Fingerprint)
	if err != nil {
		return err
	}
	return sim.Expect("clear a decision", r.HTTPResponse, http.StatusOK)
}
