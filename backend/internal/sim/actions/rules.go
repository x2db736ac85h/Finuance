package actions

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// Rules and categories: a rule from one of the suggestions, a rule's preview, a run of every
// rule (the server recomputes categories), a new category, two categories merged (rare), a
// category archived and brought back (rare).
func rules() []sim.Action {
	return []sim.Action{
		{Name: "rules.suggestion", Area: "rules", Needs: sim.NeedData, Run: ruleFromSuggestion},
		{Name: "rules.preview", Area: "rules", Needs: sim.NeedData, Run: rulePreview},
		{Name: "rules.apply", Area: "rules", Cost: sim.Heavy, Needs: sim.NeedData, Recomputes: true, Run: rulesApply},
		{Name: "rules.category", Area: "rules", Run: categoryCreate},
		{Name: "rules.merge", Area: "rules", Recomputes: true, Run: categoryMerge},
		{Name: "rules.archive", Area: "rules", Recomputes: true, Run: categoryArchive},
	}
}

// maxRules keeps a persona's rule list short.
const maxRules = 25

func ruleFromSuggestion(ctx context.Context, a *sim.Actor) error {
	list, err := a.API.ListRulesWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("rules", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	if len(*list.JSON200) >= maxRules {
		return sim.ErrNothingToDo
	}
	sug, err := a.API.RuleSuggestionsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("rule suggestions", sug.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	if len(*sug.JSON200) == 0 {
		return sim.ErrNothingToDo
	}
	s := (*sug.JSON200)[a.IntN(len(*sug.JSON200))]
	r, err := a.API.CreateRuleWithResponse(ctx, nil, gen.CreateRuleJSONRequestBody{Name: s.PayeeGroup + " → " + s.Category.Name,
		Match: s.Match, SetCategoryId: &s.Category.Id, Enabled: ptr(true)})
	if err != nil {
		return err
	}
	return sim.Expect("make a rule", r.HTTPResponse, http.StatusCreated, http.StatusConflict)
}

func rulePreview(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.PreviewRuleWithResponse(ctx, gen.PreviewRuleJSONRequestBody{Match: gen.RuleMatch{
		Filters: &[]gen.ColumnFilter{sim.Pick(a, filters...)}}})
	if err != nil {
		return err
	}
	return sim.Expect("preview a rule", r.HTTPResponse, http.StatusOK)
}

func rulesApply(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.ApplyRulesWithResponse(ctx)
	if err != nil {
		return err
	}
	return sim.Expect("run the rules", r.HTTPResponse, http.StatusAccepted)
}

// simCategories are the categories personas make (and merge back into each other).
var simCategories = []string{"Hobby", "Zwierzęta", "Dzieci", "Podróże", "Prezenty"}

func categoryCreate(ctx context.Context, a *sim.Actor) error {
	name := sim.Pick(a, simCategories...)
	r, err := a.API.CreateCategoryWithResponse(ctx, nil, gen.CreateCategoryJSONRequestBody{Name: name})
	if err != nil {
		return err
	}
	return sim.Expect("make a category", r.HTTPResponse, http.StatusCreated, http.StatusConflict)
}

// categoryMerge merges one of the categories personas make into another of them, once a week at
// most per persona.
func categoryMerge(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.ListCategoriesWithResponse(ctx, nil)
	if err != nil {
		return err
	}
	if err := sim.Expect("categories", r.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	var mine []gen.Category
	for _, c := range *r.JSON200 {
		for _, n := range simCategories {
			if c.Name == n {
				mine = append(mine, c)
			}
		}
	}
	if len(mine) < 2 || !a.Every("rules.merge", 7*24*time.Hour) {
		return sim.ErrNothingToDo
	}
	from, into := mine[0], mine[1+a.IntN(len(mine)-1)]
	m, err := a.API.MergeCategoryWithResponse(ctx, from.Id, gen.MergeCategoryJSONRequestBody{IntoId: into.Id})
	if err != nil {
		return err
	}
	if err := sim.Expect(fmt.Sprintf("merge %s into %s", from.Name, into.Name), m.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	return nil
}

// categoryArchive archives one of the categories personas make, finds it in the archived list and
// brings it back, as someone who archived the wrong one would; once a week at most per persona.
func categoryArchive(ctx context.Context, a *sim.Actor) error {
	r, err := a.API.ListCategoriesWithResponse(ctx, nil)
	if err != nil {
		return err
	}
	if err := sim.Expect("categories", r.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	var mine []gen.Category
	for _, c := range *r.JSON200 {
		for _, n := range simCategories {
			if c.Name == n {
				mine = append(mine, c)
			}
		}
	}
	if len(mine) == 0 || !a.Every("rules.archive", 7*24*time.Hour) {
		return sim.ErrNothingToDo
	}
	c := mine[a.IntN(len(mine))]
	ar, err := a.API.ArchiveCategoryWithResponse(ctx, c.Id)
	if err != nil {
		return err
	}
	if err := sim.Expect("archive "+c.Name, ar.HTTPResponse, http.StatusNoContent); err != nil {
		return err
	}
	list, err := a.API.ArchivedCategoriesWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("archived categories", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	un, err := a.API.UnarchiveCategoryWithResponse(ctx, c.Id)
	if err != nil {
		return err
	}
	return sim.Expect("unarchive "+c.Name, un.HTTPResponse, http.StatusOK)
}
