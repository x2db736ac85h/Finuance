package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// cacheTTL is how long a replica serves the settings and approved themes it last read. Each
// replica drops its own copy when it writes; others catch up within this time.
const cacheTTL = 10 * time.Second

type cache struct {
	mu       sync.Mutex
	at       time.Time
	settings instance.Settings
	version  int32
	themes   []Theme
}

func (s *Service) invalidate() {
	s.cache.mu.Lock()
	s.cache.at = time.Time{}
	s.cache.mu.Unlock()
}

// snapshot returns the cached settings and approved themes, reading them when stale.
func (s *Service) snapshot(ctx context.Context) (instance.Settings, int32, []Theme, error) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	now := s.clock.Now()
	if !s.cache.at.IsZero() && now.Sub(s.cache.at) < cacheTTL && !now.Before(s.cache.at) {
		return s.cache.settings, s.cache.version, s.cache.themes, nil
	}
	var (
		row    db.InstanceSetting
		themes []db.Theme
	)
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		if row, err = q.GetInstanceSettings(ctx); errors.Is(err, pgx.ErrNoRows) {
			row, err = db.InstanceSetting{}, nil // no row yet: everything at its default
		}
		if err != nil {
			return err
		}
		themes, err = q.ListApprovedThemes(ctx)
		return err
	})
	if err != nil {
		return instance.Settings{}, 0, nil, err
	}
	set, err := instance.ParseSettings(row.Doc)
	if err != nil { // stored documents were validated; a bad one must not take the app down
		set = instance.Settings{}
	}
	s.cache.settings, s.cache.version, s.cache.at = set, row.Version, now
	s.cache.themes = s.cache.themes[:0]
	for i := range themes {
		s.cache.themes = append(s.cache.themes, themeOf(themes[i]))
	}
	return set, row.Version, slices.Clone(s.cache.themes), nil
}

// Settings returns the instance document and its version (for the next PutSettings).
func (s *Service) Settings(ctx context.Context) (instance.Settings, int32, error) {
	set, v, _, err := s.snapshot(ctx)
	return set, v, err
}

// PutSettings replaces the document if version is the one stored (else platform.ErrStaleVersion).
func (s *Service) PutSettings(ctx context.Context, actor uuid.UUID, raw json.RawMessage, version int32) (instance.Settings, int32, error) {
	set, err := instance.ParseSettings(raw)
	if err != nil {
		return instance.Settings{}, 0, err
	}
	if set.Defaults.ThemeID != nil {
		ok, err := s.themeOffered(ctx, *set.Defaults.ThemeID)
		if err != nil {
			return instance.Settings{}, 0, err
		}
		if !ok {
			return instance.Settings{}, 0, invalid("defaults.themeId", "must be a built-in or approved theme")
		}
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if err := q.EnsureInstanceSettings(ctx); err != nil {
			return err
		}
		if set.Assistant == nil { // saved without the assistant part (Defaults and features): keep it
			row, err := q.GetInstanceSettings(ctx)
			if err != nil {
				return err
			}
			if cur, err := instance.ParseSettings(row.Doc); err == nil {
				set.Assistant = cur.Assistant
			}
		}
		doc, _ := json.Marshal(set) // plain data; it can't fail
		n, err := q.PutInstanceSettings(ctx, db.PutInstanceSettingsParams{Doc: doc, Now: s.clock.Now(), UpdatedBy: &actor, Version: version})
		if err == nil && n == 0 {
			err = fmt.Errorf("the settings changed since they were read: %w", platform.ErrStaleVersion)
		}
		return err
	})
	s.invalidate()
	if err != nil {
		return instance.Settings{}, 0, err
	}
	return set, version + 1, nil
}

// Features are the switches in effect.
func (s *Service) Features(ctx context.Context) (instance.EffectiveFeatures, error) {
	set, _, _, err := s.snapshot(ctx)
	return set.Features.Effective(), err
}

// Limits are the limits that apply to u.
func (s *Service) Limits(ctx context.Context, u auth.User) (instance.Effective, error) {
	set, _, _, err := s.snapshot(ctx)
	return instance.Resolve(u.Limits, set.Limits, s.bounds), err
}

// Public is what every visitor may know: the switches, the default theme and the approved themes.
type Public struct {
	Features instance.EffectiveFeatures
	Defaults instance.Defaults
	Themes   []Theme
}

// Public returns the instance's public settings. A default theme that was retired is dropped.
func (s *Service) Public(ctx context.Context) (Public, error) {
	set, _, themes, err := s.snapshot(ctx)
	if err != nil {
		return Public{}, err
	}
	offered := func(id string) bool {
		return slices.Contains(instance.BuiltinThemes, id) || slices.ContainsFunc(themes, func(t Theme) bool { return t.Slug == id })
	}
	return Public{Features: set.Features.Effective(), Defaults: set.Defaults.WithTheme(offered), Themes: themes}, nil
}
