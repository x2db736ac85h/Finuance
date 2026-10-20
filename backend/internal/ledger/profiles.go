package ledger

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Profile is a remembered mapping for a file layout, keyed by ingest.HeaderSignature (PLAN §4.4).
// Mapping and Options are the JSON documents of PLAN §4.3, stored as given.
type Profile struct {
	HeaderSignature [32]byte
	Name            string
	Mapping         json.RawMessage
	Options         json.RawMessage
	UpdatedAt       time.Time
}

func profileOf(p db.ImportProfile) Profile {
	out := Profile{Name: p.Name, Mapping: p.Mapping, Options: p.Options, UpdatedAt: p.UpdatedAt}
	copy(out.HeaderSignature[:], p.HeaderSignature)
	return out
}

// GetProfile returns the profile for a header signature, or ErrNotFound.
func (l *Ledger) GetProfile(ctx context.Context, userID uuid.UUID, sig [32]byte) (Profile, error) {
	var out Profile
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		p, err := q.GetImportProfile(ctx, db.GetImportProfileParams{UserID: userID, HeaderSignature: sig[:]})
		out = profileOf(p)
		return err
	})
	return out, dbErr("get profile", err)
}

// SaveProfile creates or replaces the profile for p.HeaderSignature ("remember mapping").
func (l *Ledger) SaveProfile(ctx context.Context, userID uuid.UUID, p Profile) error {
	if !json.Valid(p.Mapping) || !json.Valid(p.Options) {
		return invalidf("profile mapping and options must be JSON")
	}
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		return q.UpsertImportProfile(ctx, db.UpsertImportProfileParams{UserID: userID, HeaderSignature: p.HeaderSignature[:],
			Name: p.Name, Mapping: p.Mapping, Options: p.Options, Now: l.clock.Now()})
	})
	return dbErr("save profile", err)
}

// ListProfiles returns the user's profiles, most recently used first.
func (l *Ledger) ListProfiles(ctx context.Context, userID uuid.UUID) ([]Profile, error) {
	var out []Profile
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListImportProfiles(ctx, userID)
		for i := range rows {
			out = append(out, profileOf(rows[i]))
		}
		return err
	})
	return out, dbErr("list profiles", err)
}

// DeleteProfile forgets a remembered mapping.
func (l *Ledger) DeleteProfile(ctx context.Context, userID uuid.UUID, sig [32]byte) error {
	var n int64
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteImportProfile(ctx, db.DeleteImportProfileParams{UserID: userID, HeaderSignature: sig[:]})
		return err
	})
	return notFound("delete profile", n, err)
}
