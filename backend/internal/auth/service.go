package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Input rules (PLAN task 5.4).
const (
	MinPasswordChars = 12 // no composition rules (NIST SP 800-63B)
	MaxDisplayName   = 80
	maxEmail         = 254
	// maxConcurrentHashes bounds memory: each argon2id hash holds Params.MemoryKiB (19 MiB by default).
	maxConcurrentHashes = 4
)

// ErrInvalidCredentials is the one answer to a failed login, whether the email is unknown or the
// password wrong. It wraps platform.ErrUnauthenticated (401).
var ErrInvalidCredentials = fmt.Errorf("invalid email or password: %w", platform.ErrUnauthenticated)

// FailedLoginError is ErrInvalidCredentials for an existing account with a wrong password. It
// carries the account's id for that user's audit log (PLAN 17.6) and is otherwise the same error:
// same message, same 401, same timing, so it reveals nothing about which emails exist.
type FailedLoginError struct{ UserID uuid.UUID }

func (e *FailedLoginError) Error() string { return ErrInvalidCredentials.Error() }
func (e *FailedLoginError) Unwrap() error { return ErrInvalidCredentials }

// Config are the auth settings (from config.Config).
type Config struct {
	Params         Params
	SessionTTL     time.Duration
	SessionIdleTTL time.Duration
	AllowSignup    bool
}

// User is an account as the API shows it.
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	IsDemo      bool
	CreatedAt   time.Time
	// Phase 19 (ADR 0025): the administrator flag, a pending forced password change (after a
	// one-time password) and the per-user limit overrides.
	IsAdmin            bool
	MustChangePassword bool
	Limits             instance.Limits
	Disabled           bool // an administrator disabled the account (background work skips it)
}

func userOf(u db.User) User {
	limits, _ := instance.ParseLimits(u.Limits) // the admin service validated them when it stored them
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, CreatedAt: u.CreatedAt,
		IsAdmin: u.IsAdmin, MustChangePassword: u.MustChangePassword, Limits: limits, Disabled: u.DisabledAt != nil}
}

// Service implements signup, login, sessions and the /me operations.
type Service struct {
	st     *store.Store
	clock  platform.Clock
	cfg    Config
	dummy  string        // hash checked for unknown emails (constant-time failure path)
	hashes chan struct{} // semaphore, see maxConcurrentHashes
}

// New returns a Service. It computes the dummy hash once (one argon2id run).
func New(st *store.Store, clock platform.Clock, cfg Config) *Service {
	return &Service{st: st, clock: clock, cfg: cfg, dummy: dummyHash(cfg.Params), hashes: make(chan struct{}, maxConcurrentHashes)}
}

// acquire waits for a hashing slot.
func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.hashes <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() { <-s.hashes }

func (s *Service) hash(ctx context.Context, password string) (string, error) {
	if err := s.acquire(ctx); err != nil {
		return "", err
	}
	defer s.release()
	return Hash(password, s.cfg.Params)
}

func (s *Service) verify(ctx context.Context, password, phc string) (bool, error) {
	if err := s.acquire(ctx); err != nil {
		return false, err
	}
	defer s.release()
	return Verify(password, phc)
}

// SignupInput is the body of POST /auth/signup.
type SignupInput struct {
	Email       string
	Password    string
	DisplayName string
	// Set only by administrators (finuance admin create-user -admin, the admin panel).
	Admin              bool
	MustChangePassword bool
}

// cleanEmail trims the address and checks it is one plain address (no display name, no list).
// Case is kept as typed; the citext column compares case-insensitively.
func cleanEmail(email string, ve *platform.ValidationError) string {
	email = strings.TrimSpace(email)
	a, err := mail.ParseAddress(email)
	switch {
	case email == "":
		ve.Add("email", "is required")
	case len(email) > maxEmail:
		ve.Add("email", fmt.Sprintf("must be at most %d characters", maxEmail))
	case err != nil || a.Address != email || a.Name != "":
		ve.Add("email", "is not a valid email address")
	}
	return email
}

func checkPassword(field, password string, ve *platform.ValidationError) {
	switch {
	case utf8.RuneCountInString(password) < MinPasswordChars:
		ve.Add(field, fmt.Sprintf("must be at least %d characters", MinPasswordChars))
	case len(password) > MaxPasswordBytes:
		ve.Add(field, fmt.Sprintf("must be at most %d bytes", MaxPasswordBytes))
	}
}

func cleanDisplayName(name string, ve *platform.ValidationError) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxDisplayName {
		ve.Add("displayName", fmt.Sprintf("must be at most %d characters", MaxDisplayName))
	}
	if strings.ContainsRune(name, 0) { // PostgreSQL text can't hold NUL (JSON allows \u0000)
		ve.Add("displayName", "must not contain NUL characters")
	}
	return name
}

// Signup creates an account and signs it in. It is platform.ErrForbidden when signup is disabled
// and platform.ErrConflict when the email is taken.
func (s *Service) Signup(ctx context.Context, in SignupInput, c Client) (User, string, error) {
	if !s.cfg.AllowSignup {
		return User{}, "", fmt.Errorf("signup is disabled on this instance: %w", platform.ErrForbidden)
	}
	u, err := s.createUser(ctx, in)
	if err != nil {
		return User{}, "", err
	}
	var token string
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		token, err = s.newSession(ctx, q, u.ID, c)
		return err
	})
	return u, token, err
}

// CreateUser creates an account without signing in (finuance admin create-user). It works even
// when signup is disabled.
func (s *Service) CreateUser(ctx context.Context, in SignupInput) (User, error) {
	return s.createUser(ctx, in)
}

func (s *Service) createUser(ctx context.Context, in SignupInput) (User, error) {
	ve := &platform.ValidationError{}
	email := cleanEmail(in.Email, ve)
	checkPassword("password", in.Password, ve)
	name := cleanDisplayName(in.DisplayName, ve)
	if err := ve.Err(); err != nil {
		return User{}, err
	}
	phc, err := s.hash(ctx, in.Password)
	if err != nil {
		return User{}, err
	}
	var out User
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		u, err := q.CreateUserWithRole(ctx, db.CreateUserWithRoleParams{ID: platform.NewID(), Email: email, PasswordHash: phc,
			DisplayName: name, IsAdmin: in.Admin, MustChangePassword: in.MustChangePassword})
		out = userOf(u)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, fmt.Errorf("an account with this email already exists: %w", platform.ErrConflict)
	}
	return out, err
}

// Login checks the credentials and starts a new session. previousToken, the cookie the browser
// sent (if any), is revoked: a login always rotates the session id, so a session fixed before login
// is useless afterwards. Unknown emails still run a full argon2id verify against a dummy hash, so
// the timing doesn't tell whether an account exists. Outdated hashes are upgraded (ADR 0010).
func (s *Service) Login(ctx context.Context, email, password, previousToken string, c Client) (User, string, error) {
	email = strings.TrimSpace(email)
	var u db.User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		u, err = q.GetUserByEmail(ctx, email)
		return err
	})
	known := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", err
	}
	phc := s.dummy
	if known {
		phc = u.PasswordHash
	}
	ok, err := s.verify(ctx, password, phc)
	if err != nil && !errors.Is(err, ErrMalformedHash) {
		return User{}, "", err
	}
	if !known {
		return User{}, "", ErrInvalidCredentials
	}
	if !ok {
		return User{}, "", &FailedLoginError{UserID: u.ID}
	}
	if u.DisabledAt != nil { // only after the right password, so this reveals no more than a failure
		return User{}, "", fmt.Errorf("this account has been disabled by an administrator: %w", platform.ErrAccountDisabled)
	}
	var rehash string
	if NeedsRehash(u.PasswordHash, s.cfg.Params) {
		if rehash, err = s.hash(ctx, password); err != nil {
			return User{}, "", err
		}
	}
	var token string
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if h, ok := hashToken(previousToken); ok {
			if err := q.DeleteSession(ctx, h[:]); err != nil {
				return err
			}
		}
		if rehash != "" {
			if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: u.ID, PasswordHash: rehash}); err != nil {
				return err
			}
		}
		now := s.clock.Now()
		if err := q.SetUserLastLogin(ctx, db.SetUserLastLoginParams{ID: u.ID, At: &now}); err != nil {
			return err
		}
		var err error
		token, err = s.newSession(ctx, q, u.ID, c)
		return err
	})
	return userOf(u), token, err
}

// checkCurrentPassword verifies the signed-in user's password (password change, account deletion).
// A wrong password is a validation error on field, not a 401: the session itself is fine.
func (s *Service) checkCurrentPassword(ctx context.Context, userID uuid.UUID, field, password string) error {
	var u db.User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		u, err = q.GetUser(ctx, userID)
		return err
	})
	if err != nil {
		return err
	}
	ok, err := s.verify(ctx, password, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		ve := &platform.ValidationError{}
		ve.Add(field, "is not your current password")
		return ve
	}
	return nil
}

// ChangePassword sets a new password after checking the current one, revokes every session —
// including the current one — and returns a fresh session for this browser (rotation).
func (s *Service) ChangePassword(ctx context.Context, id Identity, current, next string, c Client) (string, error) {
	ve := &platform.ValidationError{}
	checkPassword("newPassword", next, ve)
	if err := ve.Err(); err != nil {
		return "", err
	}
	if err := s.checkCurrentPassword(ctx, id.User.ID, "currentPassword", current); err != nil {
		return "", err
	}
	if next == current && id.User.MustChangePassword {
		ve.Add("newPassword", "must differ from the one-time password")
		return "", ve
	}
	return s.setPassword(ctx, id.User.ID, next, &c)
}

// SetPassword replaces a user's password without the old one and revokes all their sessions
// (finuance admin set-password).
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	ve := &platform.ValidationError{}
	checkPassword("password", password, ve)
	if err := ve.Err(); err != nil {
		return err
	}
	u, err := s.UserByEmail(ctx, email)
	if err != nil {
		return err
	}
	_, err = s.setPassword(ctx, u.ID, password, nil)
	return err
}

// UserByEmail finds a user for the admin commands (finuance admin, seed-demo).
func (s *Service) UserByEmail(ctx context.Context, email string) (User, error) {
	var u db.User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		u, err = q.GetUserByEmail(ctx, strings.TrimSpace(email))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user %q: %w", email, platform.ErrNotFound)
	}
	if err != nil {
		return User{}, err
	}
	return userOf(u), nil
}

// UserByID finds a user for background work (the assistant's digests read the user's limits).
func (s *Service) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u db.User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		u, err = q.GetUser(ctx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, platform.ErrNotFound)
	}
	if err != nil {
		return User{}, err
	}
	return userOf(u), nil
}

// setPassword stores a new hash, deletes every session of the user and, when c is given, opens a
// new one for the caller.
func (s *Service) setPassword(ctx context.Context, userID uuid.UUID, password string, c *Client) (string, error) {
	phc, err := s.hash(ctx, password)
	if err != nil {
		return "", err
	}
	var token string
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: userID, PasswordHash: phc}); err != nil {
			return err
		}
		if c != nil { // the user chose this password themselves: a pending forced change is done
			if err := q.SetUserMustChangePassword(ctx, db.SetUserMustChangePasswordParams{ID: userID, MustChange: false}); err != nil {
				return err
			}
		}
		if _, err := q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: userID, KeepTokenHash: []byte{}}); err != nil {
			return err
		}
		if c == nil {
			return nil
		}
		var err error
		token, err = s.newSession(ctx, q, userID, *c)
		return err
	})
	return token, err
}

// UpdateDisplayName changes the signed-in user's display name.
func (s *Service) UpdateDisplayName(ctx context.Context, id Identity, name string) (User, error) {
	ve := &platform.ValidationError{}
	name = cleanDisplayName(name, ve)
	if err := ve.Err(); err != nil {
		return User{}, err
	}
	var out User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if err := q.SetUserDisplayName(ctx, db.SetUserDisplayNameParams{ID: id.User.ID, DisplayName: name}); err != nil {
			return err
		}
		u, err := q.GetUser(ctx, id.User.ID)
		out = userOf(u)
		return err
	})
	return out, err
}

// DeleteAccount deletes the signed-in user after checking the password. Every user-owned row goes
// with it (ON DELETE CASCADE), sessions included.
func (s *Service) DeleteAccount(ctx context.Context, id Identity, password string) error {
	if err := s.checkCurrentPassword(ctx, id.User.ID, "password", password); err != nil {
		return err
	}
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if err := GuardLastAdmin(ctx, q, id.User.ID); err != nil {
			return err
		}
		_, err := q.DeleteUser(ctx, id.User.ID)
		return err
	})
}

// GuardLastAdmin fails with platform.ErrLastAdmin when userID is the only active administrator,
// so the instance never loses its last one (to a deletion, a demotion or disabling). It locks the
// administrators' rows for the rest of the transaction.
func GuardLastAdmin(ctx context.Context, q *db.Queries, userID uuid.UUID) error {
	u, err := q.GetUser(ctx, userID)
	if err != nil || !u.IsAdmin || u.DisabledAt != nil {
		return err
	}
	n, err := q.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return fmt.Errorf("this is the only administrator; make someone else an administrator first: %w", platform.ErrLastAdmin)
	}
	return nil
}

// HashPassword checks a new password's length and hashes it (the admin service's one-time and
// administrator-set passwords).
func (s *Service) HashPassword(ctx context.Context, field, password string) (string, error) {
	ve := &platform.ValidationError{}
	checkPassword(field, password, ve)
	if err := ve.Err(); err != nil {
		return "", err
	}
	return s.hash(ctx, password)
}

// CheckDisplayName validates a display name the way sign-up does.
func CheckDisplayName(name string) (string, error) {
	ve := &platform.ValidationError{}
	name = cleanDisplayName(name, ve)
	return name, ve.Err()
}
