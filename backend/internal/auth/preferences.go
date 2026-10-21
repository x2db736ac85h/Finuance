package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// MaxPreferencesBytes caps the preferences document. It holds UI state (theme, card layout, chart
// settings, per-view filters), not data; 256 KiB is far beyond what the UI writes.
const MaxPreferencesBytes = 256 << 10

// Preferences is the user's UI settings document with its optimistic-concurrency version. Version 0
// means no document has been saved yet.
type Preferences struct {
	Version int32
	Prefs   json.RawMessage // a JSON object; the frontend owns its shape (PLAN §7.4)
}

// StaleVersionError is returned when the client's version is not the stored one. It carries the
// current document so the client can merge and retry (409 stale_version).
type StaleVersionError struct{ Current Preferences }

func (e *StaleVersionError) Error() string {
	return fmt.Sprintf("preferences are at version %d", e.Current.Version)
}

// Unwrap maps the error to 409 stale_version.
func (e *StaleVersionError) Unwrap() error { return platform.ErrStaleVersion }

// GetPreferences returns the user's preferences, or {0, {}} before the first save.
func (s *Service) GetPreferences(ctx context.Context, id Identity) (Preferences, error) {
	var out Preferences
	err := s.st.WithUser(ctx, id.User.ID, func(q *db.Queries, _ pgx.Tx) (err error) {
		out, err = getPreferences(ctx, q, id)
		return err
	})
	return out, err
}

func getPreferences(ctx context.Context, q *db.Queries, id Identity) (Preferences, error) {
	p, err := q.GetPreferences(ctx, id.User.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preferences{Version: 0, Prefs: json.RawMessage(`{}`)}, nil
	}
	return Preferences{Version: p.Version, Prefs: p.Prefs}, err
}

// SavePreferences replaces the document when version is the stored one (0 for the first save) and
// returns the new version. Otherwise it returns a *StaleVersionError with the current document.
func (s *Service) SavePreferences(ctx context.Context, id Identity, version int32, prefs json.RawMessage) (int32, error) {
	ve := &platform.ValidationError{}
	var obj map[string]json.RawMessage
	switch {
	case version < 0:
		ve.Add("version", "must not be negative")
	case len(prefs) > MaxPreferencesBytes:
		ve.Add("prefs", fmt.Sprintf("must be at most %d bytes", MaxPreferencesBytes))
	case json.Unmarshal(prefs, &obj) != nil || obj == nil:
		ve.Add("prefs", "must be a JSON object")
	}
	if err := ve.Err(); err != nil {
		return 0, err
	}
	var next int32
	var stale *StaleVersionError
	err := s.st.WithUser(ctx, id.User.ID, func(q *db.Queries, _ pgx.Tx) error {
		v, err := q.SavePreferences(ctx, db.SavePreferencesParams{UserID: id.User.ID, Prefs: prefs, Now: s.clock.Now(), ExpectedVersion: version})
		if errors.Is(err, pgx.ErrNoRows) {
			cur, err := getPreferences(ctx, q, id)
			if err != nil {
				return err
			}
			stale = &StaleVersionError{Current: cur}
			return nil
		}
		next = v
		return err
	})
	if err == nil && stale != nil {
		return 0, stale
	}
	return next, err
}
