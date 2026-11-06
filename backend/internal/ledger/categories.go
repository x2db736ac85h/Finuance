package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Category is a spending category. Bank categories from imports become categories by name.
type Category struct {
	ID   uuid.UUID
	Name string
}

const maxCategoryName = 80

// CreateCategory adds a category; a name already used (after normalisation) is ErrConflict. A
// category created by hand takes over its name: an alias of that name (left by a rename, merge or
// delete) goes, so the bank's rows of that name land in the new category on the next import.
func (l *Ledger) CreateCategory(ctx context.Context, userID uuid.UUID, name string) (Category, error) {
	name, err := cleanName("category", name, maxCategoryName)
	if err != nil {
		return Category{}, err
	}
	var out Category
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if err := q.DeleteCategoryAlias(ctx, db.DeleteCategoryAliasParams{UserID: userID, NameN: nameKey(name)}); err != nil {
			return err
		}
		c, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: userID, Name: name, NameN: nameKey(name)})
		if err != nil {
			return err
		}
		out = Category{ID: c.ID, Name: c.Name}
		return RecordAIChange(ctx, q, l.clock, userID, []AIItem{{Entity: AIEntityCategory, ID: c.ID, After: CreatedState{Created: true, Name: c.Name}}})
	})
	return out, dbErr("create category", err)
}

// EnsureCategories returns the id of each name, creating the missing ones (the inspector's "new
// category" and the legacy budgets). A name goes through its alias first: a name merged into
// another category gives that category, a deleted one gives nothing. Names that normalise to the
// same key share one id; blank names are skipped. The result maps nameKey(name) to id.
func (l *Ledger) EnsureCategories(ctx context.Context, userID uuid.UUID, names []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		for _, n := range names {
			n, err := cleanName("category", n, maxCategoryName)
			if err != nil {
				continue
			}
			key := nameKey(n)
			if _, ok := out[key]; ok {
				continue
			}
			alias, err := q.GetCategoryAlias(ctx, db.GetCategoryAliasParams{UserID: userID, NameN: key})
			switch {
			case err == nil:
				if alias != nil {
					out[key] = *alias
				}
				continue
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
			id, err := q.UpsertCategory(ctx, db.UpsertCategoryParams{ID: platform.NewID(), UserID: userID, Name: n, NameN: key})
			if err != nil {
				return err
			}
			out[key] = id
		}
		return nil
	})
	return out, dbErr("ensure categories", err)
}

// ListCategories returns the user's categories in natural Polish order.
func (l *Ledger) ListCategories(ctx context.Context, userID uuid.UUID) ([]Category, error) {
	var out []Category
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListCategories(ctx, userID)
		for _, c := range rows {
			out = append(out, Category{ID: c.ID, Name: c.Name})
		}
		return err
	})
	return out, dbErr("list categories", err)
}

// CategoryStat is a category on Settings → Categories: its transactions (all accounts), how many
// were set by hand and by rules, and the other bank names that map to it.
type CategoryStat struct {
	Category
	TxCount, ManualCount, RuleCount int64
	Aliases                         []string
}

// CategoryStats lists every category with its counts, in natural Polish order.
func (l *Ledger) CategoryStats(ctx context.Context, userID uuid.UUID) ([]CategoryStat, error) {
	var out []CategoryStat
	err := l.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.CategoryStats(ctx, userID)
		for _, r := range rows {
			out = append(out, CategoryStat{Category: Category{ID: r.ID, Name: r.Name}, TxCount: r.TxCount,
				ManualCount: r.ManualCount, RuleCount: r.RuleCount, Aliases: r.Aliases})
		}
		return err
	})
	return out, dbErr("category stats", err)
}

// RenameCategory changes a category's name. The bank's rows of the old name keep coming to it (an
// alias for the old name), and an alias of the new name goes (the category takes the name over).
// A name another category has is ErrConflict: merge them instead.
func (l *Ledger) RenameCategory(ctx context.Context, userID, id uuid.UUID, name string) (Category, error) {
	name, err := cleanName("category", name, maxCategoryName)
	if err != nil {
		return Category{}, err
	}
	var out Category
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		old, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		key := nameKey(name)
		if taken, err := q.CategoryNameTaken(ctx, db.CategoryNameTakenParams{UserID: userID, NameN: key, ID: id}); err != nil || taken {
			return errors.Join(err, map[bool]error{true: fmt.Errorf("another category is called %q: %w", name, ErrConflict)}[taken])
		}
		if err := q.DeleteCategoryAlias(ctx, db.DeleteCategoryAliasParams{UserID: userID, NameN: key}); err != nil {
			return err
		}
		if old.NameN != key {
			if err := q.SetCategoryAlias(ctx, db.SetCategoryAliasParams{UserID: userID, NameN: old.NameN, CategoryID: &id}); err != nil {
				return err
			}
		}
		c, err := q.RenameCategory(ctx, db.RenameCategoryParams{UserID: userID, ID: id, Name: name, NameN: key})
		out = Category{ID: c.ID, Name: c.Name}
		return err
	})
	return out, dbErr("rename category", err)
}

// MergeCategory moves everything of category from into category into and deletes from: its
// transactions (keeping how each was categorised), manual categories, rules, its budget when into
// has none, its goals, and its bank names (from's own name and its aliases now mean into).
func (l *Ledger) MergeCategory(ctx context.Context, userID, from, into uuid.UUID) (int64, error) {
	if from == into {
		return 0, fmt.Errorf("a category can't be merged into itself: %w", ErrInvalid)
	}
	var moved int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		src, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: from})
		if err != nil {
			return err
		}
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: into}); err != nil {
			return err
		}
		if moved, err = q.MergeCategoryTransactions(ctx, db.MergeCategoryTransactionsParams{UserID: userID, FromID: from, IntoID: into}); err != nil {
			return err
		}
		steps := []func() error{
			func() error {
				return q.MergeCategoryAnnotations(ctx, db.MergeCategoryAnnotationsParams{UserID: userID, FromID: from, IntoID: into})
			},
			func() error {
				return q.MergeCategoryRules(ctx, db.MergeCategoryRulesParams{UserID: userID, FromID: from, IntoID: into})
			},
			func() error {
				return q.MergeCategoryBudget(ctx, db.MergeCategoryBudgetParams{UserID: userID, FromID: from, IntoID: into})
			},
			func() error {
				return q.MergeCategoryGoals(ctx, db.MergeCategoryGoalsParams{UserID: userID, FromID: from, IntoID: into})
			},
			func() error {
				return q.MoveCategoryAliases(ctx, db.MoveCategoryAliasesParams{UserID: userID, FromID: from, IntoID: into})
			},
			func() error {
				return q.SetCategoryAlias(ctx, db.SetCategoryAliasParams{UserID: userID, NameN: src.NameN, CategoryID: &into})
			},
			func() error {
				_, err := q.DeleteCategory(ctx, db.DeleteCategoryParams{UserID: userID, ID: from})
				return err
			},
		}
		for _, step := range steps {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	})
	return moved, dbErr("merge category", err)
}

// DeleteCategory deletes a category: its transactions become uncategorised, however they were
// categorised, and its budget goes. Its bank names stay uncategorised on later imports (an alias to
// nothing), until a category of that name is created again.
func (l *Ledger) DeleteCategory(ctx context.Context, userID, id uuid.UUID) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		c, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // n stays 0: not found
		} else if err != nil {
			return err
		}
		if _, err = q.UncategoriseCategoryTransactions(ctx, db.UncategoriseCategoryTransactionsParams{UserID: userID, ID: id}); err != nil {
			return err
		}
		if err := q.SetCategoryAlias(ctx, db.SetCategoryAliasParams{UserID: userID, NameN: c.NameN}); err != nil {
			return err
		}
		if n, err = q.DeleteCategory(ctx, db.DeleteCategoryParams{UserID: userID, ID: id}); err != nil {
			return err
		}
		_, err = q.DeleteEmptyAnnotations(ctx, userID) // annotations that only held this category
		return err
	})
	return notFound("delete category", n, err)
}
