package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Two-step sign-in (PLAN 23.7, ADR 0040). After the password, an account with a confirmed second
// factor gets a challenge instead of a session; the session starts when the challenge is answered
// with a one-time code (totp.go) or one of ten recovery codes. The secret is stored sealed with
// the instance key; without the key the feature can't be switched on.
const (
	// ChallengeTTL is how long the second step may take after the password.
	ChallengeTTL = 5 * time.Minute
	// challengeAttempts is how many codes one challenge takes before the password is needed again.
	challengeAttempts = 5
)

// SecondStepRequiredError is what Login returns for an account with a second factor: the password
// was right, and Challenge (a random token for this browser only) must be answered with a code
// through CompleteLogin before ExpiresAt.
type SecondStepRequiredError struct {
	UserID    uuid.UUID
	Challenge string
	ExpiresAt time.Time
}

func (e *SecondStepRequiredError) Error() string { return "a code from the second step is required" }

// FailedSecondStepError is a wrong or reused code for an existing challenge (the handler writes
// the user's activity log). It reads like any failed sign-in.
type FailedSecondStepError struct{ UserID uuid.UUID }

func (e *FailedSecondStepError) Error() string { return ErrInvalidSecondStep.Error() }
func (e *FailedSecondStepError) Unwrap() error { return ErrInvalidSecondStep }

// ErrInvalidSecondStep is the one answer to a failed second step: a wrong code, a code used
// before, a challenge that is unknown, expired or used up.
var ErrInvalidSecondStep = fmt.Errorf("the code is not valid, or the sign-in took too long; sign in again: %w", platform.ErrUnauthenticated)

// SecondFactor is what a user sees of their own second factor.
type SecondFactor struct {
	Enabled       bool // confirmed and in force
	ConfirmedAt   *time.Time
	RecoveryCodes int // unused ones
}

// Enrolment is the secret of an enrolment that has started, for the authenticator app.
type Enrolment struct {
	Secret string // base32, to type in
	URI    string // otpauth://, for a QR code
}

func sealPlace(userID uuid.UUID) string { return "totp:" + userID.String() }

func (s *Service) openSecret(f db.UserSecondFactor) ([]byte, error) {
	text, err := s.cfg.Box.Open(f.SecretSealed, sealPlace(f.UserID))
	if err != nil {
		return nil, fmt.Errorf("second factor of %s: %w", platform.UserHash(f.UserID), err)
	}
	return b32.DecodeString(text)
}

// SecondFactorOf reports the state of a user's second factor.
func (s *Service) SecondFactorOf(ctx context.Context, userID uuid.UUID) (SecondFactor, error) {
	var out SecondFactor
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		f, err := q.GetSecondFactor(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && f.ConfirmedAt == nil) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err := q.CountUnusedRecoveryCodes(ctx, userID)
		out = SecondFactor{Enabled: true, ConfirmedAt: f.ConfirmedAt, RecoveryCodes: int(n)}
		return err
	})
	return out, err
}

// codeError is a validation error on the "code" field: the session is fine, the code is not.
func codeError(msg string) error {
	ve := &platform.ValidationError{}
	ve.Add("code", msg)
	return ve
}

// StartSecondFactor begins enrolment and returns the new secret. It replaces an enrolment that was
// started and never confirmed; a factor in force has to be switched off first.
func (s *Service) StartSecondFactor(ctx context.Context, id Identity) (Enrolment, error) {
	switch {
	case id.User.IsDemo:
		return Enrolment{}, fmt.Errorf("demo accounts can't use two-step sign-in: %w", platform.ErrForbidden)
	case s.cfg.Box == nil:
		return Enrolment{}, fmt.Errorf("two-step sign-in needs the instance key (FINUANCE_INSTANCE_KEY), which this instance doesn't have: %w", platform.ErrConflict)
	}
	raw, err := NewTOTPSecret()
	if err != nil {
		return Enrolment{}, err
	}
	sealed, err := s.cfg.Box.Seal(TOTPSecretText(raw), sealPlace(id.User.ID))
	if err != nil {
		return Enrolment{}, err
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		on, err := q.HasConfirmedSecondFactor(ctx, id.User.ID)
		if err != nil {
			return err
		}
		if on {
			return fmt.Errorf("two-step sign-in is already on; switch it off first to set it up again: %w", platform.ErrConflict)
		}
		return q.StartSecondFactor(ctx, db.StartSecondFactorParams{UserID: id.User.ID, SecretSealed: sealed, Now: s.clock.Now()})
	})
	if err != nil {
		return Enrolment{}, err
	}
	issuer := s.cfg.Issuer
	if issuer == "" {
		issuer = "Finuance"
	}
	return Enrolment{Secret: TOTPSecretText(raw), URI: TOTPURI(issuer, id.User.Email, raw)}, nil
}

// ConfirmSecondFactor puts the started enrolment in force once a code from the app proves that the
// secret arrived, and returns the recovery codes, which are shown this once. Other sessions of the
// user end: they were started without the second step.
func (s *Service) ConfirmSecondFactor(ctx context.Context, id Identity, code string) ([]string, error) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		f, err := q.LockSecondFactor(ctx, id.User.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("start setting up two-step sign-in first: %w", platform.ErrConflict)
		}
		if err != nil {
			return err
		}
		if f.ConfirmedAt != nil {
			return fmt.Errorf("two-step sign-in is already on: %w", platform.ErrConflict)
		}
		sec, err := s.openSecret(f)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		step, ok := VerifyTOTP(sec, code, now, f.LastStep)
		if !ok {
			return codeError("is not the current code from your authenticator app")
		}
		if n, err := q.ConfirmSecondFactor(ctx, db.ConfirmSecondFactorParams{UserID: id.User.ID, Now: &now, LastStep: step}); err != nil || n != 1 {
			return errors.Join(err, fmt.Errorf("confirm second factor: %d rows", n))
		}
		if err := s.replaceRecoveryCodes(ctx, q, id.User.ID, hashes); err != nil {
			return err
		}
		_, err = q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: id.User.ID, KeepTokenHash: id.TokenHash[:]})
		return err
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, q *db.Queries, userID uuid.UUID, hashes [][]byte) error {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return err
	}
	now := s.clock.Now()
	for _, h := range hashes {
		if err := q.CreateRecoveryCode(ctx, db.CreateRecoveryCodeParams{CodeHash: h, UserID: userID, Now: now}); err != nil {
			return err
		}
	}
	return nil
}

// checkCode accepts a one-time code or an unused recovery code for a user whose factor is in
// force, inside the caller's transaction. It reports whether a recovery code was used.
func (s *Service) checkCode(ctx context.Context, q *db.Queries, userID uuid.UUID, code string) (ok, recovery bool, err error) {
	f, err := q.LockSecondFactor(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && f.ConfirmedAt == nil) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	now := s.clock.Now()
	if looksLikeRecoveryCode(code) {
		n, err := q.UseRecoveryCode(ctx, db.UseRecoveryCodeParams{CodeHash: RecoveryCodeHash(code), UserID: userID, Now: &now})
		return n == 1, n == 1, err
	}
	sec, err := s.openSecret(f)
	if err != nil {
		return false, false, err
	}
	step, ok := VerifyTOTP(sec, code, now, f.LastStep)
	if !ok {
		return false, false, nil
	}
	return true, false, q.SetSecondFactorStep(ctx, db.SetSecondFactorStepParams{UserID: userID, LastStep: step})
}

// NewRecoveryCodesFor replaces the user's recovery codes after a current one-time code (not a
// recovery code: the app must still be in the user's hands).
func (s *Service) NewRecoveryCodesFor(ctx context.Context, id Identity, code string) ([]string, error) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if looksLikeRecoveryCode(code) {
		return nil, codeError("must be a code from your authenticator app, not a recovery code")
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		ok, _, err := s.checkCode(ctx, q, id.User.ID, code)
		if err != nil {
			return err
		}
		if !ok {
			return codeError("is not the current code from your authenticator app")
		}
		return s.replaceRecoveryCodes(ctx, q, id.User.ID, hashes)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableSecondFactor switches two-step sign-in off after the password and a code (a one-time
// code or a recovery code).
func (s *Service) DisableSecondFactor(ctx context.Context, id Identity, password, code string) error {
	if err := s.checkCurrentPassword(ctx, id.User.ID, "password", password); err != nil {
		return err
	}
	// the code is consumed in its own transaction, so a refused request can't be repeated with it
	var ok bool
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		ok, _, err = s.checkCode(ctx, q, id.User.ID, code)
		return err
	})
	if err != nil {
		return err
	}
	if !ok {
		return codeError("is not a current code or an unused recovery code")
	}
	return s.RemoveSecondFactor(ctx, id.User.ID)
}

// RemoveSecondFactor deletes a user's second factor, recovery codes and pending challenges. For
// the user's own DisableSecondFactor, an administrator's reset and the admin command; it reports
// platform.ErrNotFound when there was none.
func (s *Service) RemoveSecondFactor(ctx context.Context, userID uuid.UUID) error {
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteSecondFactor(ctx, userID)
		if err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteUserLoginChallenges(ctx, userID); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("two-step sign-in is not on for this account: %w", platform.ErrNotFound)
		}
		return nil
	})
}

func challengeHash(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(append([]byte("finuance login challenge\x00"), raw...))
	return sum[:], true
}

// newChallenge replaces the user's pending challenges with a new one.
func (s *Service) newChallenge(ctx context.Context, q *db.Queries, userID uuid.UUID) (*SecondStepRequiredError, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash, _ := challengeHash(token)
	now := s.clock.Now()
	if err := q.DeleteUserLoginChallenges(ctx, userID); err != nil {
		return nil, err
	}
	if err := q.CreateLoginChallenge(ctx, db.CreateLoginChallengeParams{TokenHash: hash, UserID: userID, Now: now, ExpiresAt: now.Add(ChallengeTTL)}); err != nil {
		return nil, err
	}
	return &SecondStepRequiredError{UserID: userID, Challenge: token, ExpiresAt: now.Add(ChallengeTTL)}, nil
}

// CompleteLogin answers a challenge with a one-time code or a recovery code and starts the
// session. Every call counts against the challenge, right or wrong, so guessing ends after a few
// tries and the password is needed again. usedRecovery tells the handler to say so in the log.
func (s *Service) CompleteLogin(ctx context.Context, challenge, code, previousToken string, c Client) (u User, token string, usedRecovery bool, err error) {
	hash, ok := challengeHash(challenge)
	if !ok {
		return User{}, "", false, ErrInvalidSecondStep
	}
	// the attempt is counted in its own transaction: a failed check must not roll it back
	var userID uuid.UUID
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		row, err := q.TryLoginChallenge(ctx, db.TryLoginChallengeParams{TokenHash: hash, Now: s.clock.Now(), MaxAttempts: challengeAttempts})
		userID = row.UserID
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", false, ErrInvalidSecondStep
	}
	if err != nil {
		return User{}, "", false, err
	}
	var dbu db.User
	var good bool
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		if dbu, err = q.GetUser(ctx, userID); err != nil {
			return err
		}
		if dbu.DisabledAt != nil {
			return fmt.Errorf("this account has been disabled by an administrator: %w", platform.ErrAccountDisabled)
		}
		if good, usedRecovery, err = s.checkCode(ctx, q, userID, code); err != nil || !good {
			return err
		}
		if h, ok := hashToken(previousToken); ok {
			if err := q.DeleteSession(ctx, h[:]); err != nil {
				return err
			}
		}
		if err := q.DeleteUserLoginChallenges(ctx, userID); err != nil {
			return err
		}
		now := s.clock.Now()
		if err := q.SetUserLastLogin(ctx, db.SetUserLastLoginParams{ID: userID, At: &now}); err != nil {
			return err
		}
		token, err = s.newSession(ctx, q, userID, c)
		return err
	})
	if err != nil {
		return User{}, "", false, err
	}
	if !good {
		return User{}, "", false, &FailedSecondStepError{UserID: userID}
	}
	u = userOf(dbu)
	u.SecondFactor = true
	return u, token, usedRecovery, nil
}

// ChallengeUser names the user a live challenge belongs to without counting an attempt, for the
// handler's limiter per user.
func (s *Service) ChallengeUser(ctx context.Context, challenge string) (uuid.UUID, error) {
	hash, ok := challengeHash(challenge)
	if !ok {
		return uuid.Nil, ErrInvalidSecondStep
	}
	var id uuid.UUID
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		id, err = q.GetLoginChallengeUser(ctx, db.GetLoginChallengeUserParams{TokenHash: hash, Now: s.clock.Now(), MaxAttempts: challengeAttempts})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalidSecondStep
	}
	return id, err
}

// CleanupExpiredChallenges deletes challenges past their expiry (the worker's cleanup job).
func (s *Service) CleanupExpiredChallenges(ctx context.Context) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteExpiredLoginChallenges(ctx, s.clock.Now())
		return err
	})
	return n, err
}
