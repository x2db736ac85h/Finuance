package ledger

import (
	"context"

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

// CreateCategory adds a category; a name already used (after normalisation) is ErrConflict.
func (l *Ledger) CreateCategory(ctx context.Context, userID uuid.UUID, name string) (Category, error) {
	name, err := cleanName("category", name, maxCategoryName)
	if err != nil {
		return Category{}, err
	}
	var out Category
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		c, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: platform.NewID(), UserID: userID, Name: name, NameN: nameKey(name)})
		out = Category{ID: c.ID, Name: c.Name}
		return err
	})
	return out, dbErr("create category", err)
}

// EnsureCategories returns the id of each name, creating the missing ones (the commit job does
// this for the bank categories of an import). Names that normalise to the same key share one id;
// blank names are skipped. The result maps nameKey(name) to id.
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

// RenameCategory changes a category's name.
func (l *Ledger) RenameCategory(ctx context.Context, userID, id uuid.UUID, name string) (Category, error) {
	name, err := cleanName("category", name, maxCategoryName)
	if err != nil {
		return Category{}, err
	}
	var out Category
	err = l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		c, err := q.RenameCategory(ctx, db.RenameCategoryParams{UserID: userID, ID: id, Name: name, NameN: nameKey(name)})
		out = Category{ID: c.ID, Name: c.Name}
		return err
	})
	return out, dbErr("rename category", err)
}

// DeleteCategory deletes a category; its transactions become uncategorised and its budget goes.
func (l *Ledger) DeleteCategory(ctx context.Context, userID, id uuid.UUID) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		if n, err = q.DeleteCategory(ctx, db.DeleteCategoryParams{UserID: userID, ID: id}); err != nil {
			return err
		}
		_, err = q.DeleteEmptyAnnotations(ctx, userID) // annotations that only held this category
		return err
	})
	return notFound("delete category", n, err)
}
