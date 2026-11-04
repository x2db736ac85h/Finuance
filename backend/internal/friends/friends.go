// Package friends lets two people become friends by exchanging a short code (Phase 19, 19.7,
// ADR 0027), the condition for sharing a group with someone (PR 3).
//
//   - A code is 8 characters of Crockford Base32 from crypto/rand ("K7Q4-9MZD", 40 bits), valid
//     until the end of the clock hour it was made in plus 10 minutes' grace. Only its SHA-256 is
//     stored; making a new code replaces the previous one, and a used code is gone.
//   - Entering someone's valid code makes the two of you friends at once; the code's owner gets a
//     notification. Either side can remove the friendship (the other side is told).
//   - Friends see each other's display name and its initial, never the e-mail address, so both
//     need a display name first.
//   - Guessing: 10 tries per hour per user and per address (the api's limiters) against 2^40
//     codes, and a code lives at most 70 minutes; failures are audited.
//
// friend_codes sits outside RLS like sessions (looked up by hash before the other user is known);
// friendships has forced RLS that lets either side see, add and remove the pair.
package friends

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

var (
	// ErrInvalid is a wrong, expired or own code (one message for all, so a guess learns nothing).
	ErrInvalid = errors.New("friends: invalid")
	// ErrAlready means the two are friends already.
	ErrAlready = errors.New("friends: already friends")
)

// alphabet is Crockford's Base32 (no I, L, O, U).
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// CodeLength is the number of characters of a code (shown as two groups of four).
const CodeLength = 8

// Grace is how long a code stays valid after the clock hour it was made in.
const Grace = 10 * time.Minute

// Friend is another user as a friend sees them.
type Friend struct {
	ID          uuid.UUID
	DisplayName string
	Since       time.Time
}

// Code is a freshly made code; the plain text exists only here.
type Code struct {
	Code      string // "K7Q4-9MZD"
	ExpiresAt time.Time
}

// Service manages codes and friendships.
type Service struct {
	st     *store.Store
	clock  platform.Clock
	notify *notify.Service
}

// New returns the service.
func New(st *store.Store, clock platform.Clock, n *notify.Service) *Service {
	return &Service{st: st, clock: clock, notify: n}
}

// Normalize reads a typed code: case, spaces and dashes don't matter, and the letters people
// confuse with digits (I, L → 1, O → 0) are read as Crockford does. ok is false for anything that
// can't be a code.
func Normalize(typed string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(typed) {
		switch r {
		case '-', ' ':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		if !strings.ContainsRune(alphabet, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	s := b.String()
	return s, len(s) == CodeLength
}

// Format shows a normalised code as two groups of four.
func Format(code string) string { return code[:4] + "-" + code[4:] }

func hash(code string) []byte {
	h := sha256.Sum256([]byte(code))
	return h[:]
}

// ExpiresAt is the end of the clock hour of t plus the grace.
func ExpiresAt(t time.Time) time.Time { return t.UTC().Truncate(time.Hour).Add(time.Hour + Grace) }

// displayName returns a user's display name, or ErrDisplayNameRequired when it is empty.
func displayName(ctx context.Context, q *db.Queries, id uuid.UUID) (string, error) {
	name, err := q.FriendDisplayName(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("user %s: %w", id, platform.ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("set a display name in Settings → Profile first: %w", platform.ErrDisplayNameRequired)
	}
	return name, nil
}

// NewCode makes a code for userID, replacing any previous one.
func (s *Service) NewCode(ctx context.Context, userID uuid.UUID) (Code, error) {
	var raw [CodeLength]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Code{}, err
	}
	code := make([]byte, CodeLength)
	for i, v := range raw {
		code[i] = alphabet[v&31] // 256 is a multiple of 32: no bias
	}
	now := s.clock.Now()
	out := Code{Code: Format(string(code)), ExpiresAt: ExpiresAt(now)}
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := displayName(ctx, q, userID); err != nil {
			return err
		}
		return q.SetFriendCode(ctx, db.SetFriendCodeParams{UserID: userID, CodeHash: hash(string(code)), ExpiresAt: out.ExpiresAt, Now: now})
	})
	return out, err
}

func pair(a, b uuid.UUID) (lo, hi uuid.UUID) {
	if strings.Compare(a.String(), b.String()) < 0 {
		return a, b
	}
	return b, a
}

// Redeem makes userID friends with the owner of code and tells the owner. A wrong, expired or
// own code is ErrInvalid; friends already, ErrAlready.
func (s *Service) Redeem(ctx context.Context, userID uuid.UUID, typed string) (Friend, error) {
	code, ok := Normalize(typed)
	if !ok {
		return Friend{}, fmt.Errorf("a code has 8 letters and digits, like K7Q4-9MZD: %w", ErrInvalid)
	}
	now := s.clock.Now()
	var owner uuid.UUID
	var ownerName, myName string
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		c, err := q.FindFriendCode(ctx, hash(code))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (now.After(c.ExpiresAt) || c.UserID == userID)) {
			return fmt.Errorf("this code is wrong or has expired; ask for a new one: %w", ErrInvalid)
		}
		if err != nil {
			return err
		}
		owner = c.UserID
		if myName, err = displayName(ctx, q, userID); err != nil {
			return err
		}
		if ownerName, err = displayName(ctx, q, owner); err != nil {
			return fmt.Errorf("this code is wrong or has expired; ask for a new one: %w", ErrInvalid)
		}
		return nil
	})
	if err != nil {
		return Friend{}, err
	}
	a, b := pair(userID, owner)
	err = s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.AddFriendship(ctx, db.AddFriendshipParams{UserA: a, UserB: b, Now: now})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("you are friends already: %w", ErrAlready)
		}
		return nil
	})
	if err != nil {
		return Friend{}, err
	}
	// a used code is gone, so a leaked one can't add a stranger
	if err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error { return q.DeleteFriendCode(ctx, owner) }); err != nil {
		return Friend{}, err
	}
	if err := s.notify.Send(ctx, owner, notify.FriendAdded, map[string]string{"friendId": userID.String(), "name": myName}); err != nil {
		return Friend{}, err
	}
	return Friend{ID: owner, DisplayName: ownerName, Since: now}, nil
}

// List returns the user's friends.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Friend, error) {
	var out []Friend
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListFriends(ctx, userID)
		for _, r := range rows {
			out = append(out, Friend{ID: r.ID, DisplayName: r.DisplayName, Since: r.CreatedAt})
		}
		return err
	})
	return out, err
}

// Remove ends the friendship of userID and friendID and tells the other side.
func (s *Service) Remove(ctx context.Context, userID, friendID uuid.UUID) error {
	a, b := pair(userID, friendID)
	var myName string
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteFriendship(ctx, db.DeleteFriendshipParams{UserA: a, UserB: b})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("friend %s: %w", friendID, platform.ErrNotFound)
		}
		myName, err = q.FriendDisplayName(ctx, userID)
		return err
	})
	if err != nil {
		return err
	}
	return s.notify.Send(ctx, friendID, notify.FriendRemoved, map[string]string{"friendId": userID.String(), "name": myName})
}

// Cleanup deletes expired codes (hourly job).
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteExpiredFriendCodes(ctx, s.clock.Now())
		return err
	})
	return n, err
}
