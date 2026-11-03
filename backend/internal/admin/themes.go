package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Theme is an administrator-made colour theme (ADR 0026). Users can pick approved ones.
type Theme struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	BasedOn   string
	Tokens    instance.ThemeTokens
	Status    string // draft, approved or retired
	Version   int32
	UpdatedAt time.Time
}

func themeOf(t db.Theme) Theme {
	var tok instance.ThemeTokens
	_ = json.Unmarshal(t.Tokens, &tok) // validated when stored
	return Theme{ID: t.ID, Slug: t.Slug, Name: t.Name, BasedOn: t.BasedOn, Tokens: tok, Status: t.Status,
		Version: t.Version, UpdatedAt: t.UpdatedAt}
}

// Themes lists every administrator-made theme (drafts, approved and retired).
func (s *Service) Themes(ctx context.Context) ([]Theme, error) {
	var out []Theme
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListThemes(ctx)
		for i := range rows {
			out = append(out, themeOf(rows[i]))
		}
		return err
	})
	return out, err
}

func (s *Service) themeOffered(ctx context.Context, slug string) (bool, error) {
	if slices.Contains(instance.BuiltinThemes, slug) {
		return true, nil
	}
	_, _, themes, err := s.snapshot(ctx)
	return slices.ContainsFunc(themes, func(t Theme) bool { return t.Slug == slug }), err
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 || strings.ContainsRune(name, 0) {
		return "", invalid("name", "must be 1 to 40 characters")
	}
	return name, nil
}

// ThemeInput creates or changes a theme.
type ThemeInput struct {
	Slug, Name, BasedOn string
	Tokens              json.RawMessage
}

// CreateTheme stores a new draft. The slug is its id in preferences and can't change later.
func (s *Service) CreateTheme(ctx context.Context, actor uuid.UUID, in ThemeInput) (Theme, error) {
	ve := &platform.ValidationError{}
	slug := strings.TrimSpace(in.Slug)
	if !validThemeSlug(slug) || slices.Contains(instance.BuiltinThemes, slug) {
		ve.Add("slug", "must be 1 to 32 lowercase letters, digits or dashes, and not a built-in theme's")
	}
	if !slices.Contains(instance.BuiltinThemes, in.BasedOn) {
		ve.Add("basedOn", "must be a built-in theme")
	}
	name, err := checkName(in.Name)
	if err != nil {
		ve.Add("name", "must be 1 to 40 characters")
	}
	tok, err := instance.ParseThemeTokens(in.Tokens)
	var tve *platform.ValidationError
	if errors.As(err, &tve) {
		ve.Fields = append(ve.Fields, tve.Fields...)
	}
	if err := ve.Err(); err != nil {
		return Theme{}, err
	}
	raw, _ := json.Marshal(tok)
	var out Theme
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		t, err := q.CreateTheme(ctx, db.CreateThemeParams{ID: platform.NewID(), Slug: slug, Name: name, BasedOn: in.BasedOn,
			Tokens: raw, CreatedBy: &actor, Now: s.clock.Now()})
		out = themeOf(t)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Theme{}, invalid("slug", "is taken by another theme")
	}
	return out, err
}

// UpdateTheme changes a theme's name and colours. An approved theme goes back to draft only
// through Retire; it keeps being offered while edited, so changes must pass contrast again.
func (s *Service) UpdateTheme(ctx context.Context, id uuid.UUID, version int32, name string, tokens json.RawMessage) (Theme, error) {
	name, err := checkName(name)
	if err != nil {
		return Theme{}, err
	}
	tok, err := instance.ParseThemeTokens(tokens)
	if err != nil {
		return Theme{}, err
	}
	raw, _ := json.Marshal(tok)
	var out Theme
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetTheme(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status == "approved" {
			if f := instance.CheckContrast(tok); len(f) > 0 {
				return &instance.ContrastError{Failures: f}
			}
		}
		t, err := q.UpdateTheme(ctx, db.UpdateThemeParams{ID: id, Version: version, Name: name, Tokens: raw, Now: s.clock.Now()})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("the theme changed since it was read: %w", platform.ErrStaleVersion)
		}
		out = themeOf(t)
		return err
	})
	s.invalidate()
	return out, notFound(err)
}

// SetThemeStatus approves (after the contrast check), retires or reopens (draft) a theme.
func (s *Service) SetThemeStatus(ctx context.Context, id uuid.UUID, version int32, status string) (Theme, error) {
	if !slices.Contains([]string{"draft", "approved", "retired"}, status) {
		return Theme{}, invalid("status", "must be draft, approved or retired")
	}
	var out Theme
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetTheme(ctx, id)
		if err != nil {
			return err
		}
		if status == "approved" {
			if f := instance.CheckContrast(themeOf(cur).Tokens); len(f) > 0 {
				return &instance.ContrastError{Failures: f}
			}
		}
		t, err := q.SetThemeStatus(ctx, db.SetThemeStatusParams{ID: id, Version: version, Status: status, Now: s.clock.Now()})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("the theme changed since it was read: %w", platform.ErrStaleVersion)
		}
		out = themeOf(t)
		return err
	})
	s.invalidate()
	return out, notFound(err)
}

// DeleteTheme removes a draft; approved and retired themes stay (users may still have them).
func (s *Service) DeleteTheme(ctx context.Context, id uuid.UUID) error {
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteDraftTheme(ctx, id)
		if err == nil && n == 0 {
			return fmt.Errorf("theme (only drafts can be deleted; retire an approved one): %w", platform.ErrNotFound)
		}
		return err
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("theme: %w", platform.ErrNotFound)
	}
	return err
}

func validThemeSlug(s string) bool {
	if len(s) == 0 || len(s) > 32 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
