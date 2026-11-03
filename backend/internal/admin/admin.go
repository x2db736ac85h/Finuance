// Package admin runs the instance on behalf of its administrators (PLAN Phase 19, ADR 0025):
// accounts, per-user limits, the instance settings (defaults, features, limits) and colour themes.
//
// It never reads financial data. users, sessions, instance_settings and themes are outside
// row-level security and are reached through store.WithoutUser, like the auth package does; the
// only queries that touch user-owned tables are the usage counts in usage.go, which run under
// each user's own context and return numbers. The package doesn't import the ledger, query,
// insights, rules or assistant packages (TestAdminImportsNoFinancialPackage).
package admin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Service is the administration service.
type Service struct {
	st     *store.Store
	auth   *auth.Service
	clock  platform.Clock
	bounds instance.Bounds
	cache  cache
}

// New returns a Service. bounds are the environment's outer limits.
func New(st *store.Store, a *auth.Service, clock platform.Clock, bounds instance.Bounds) *Service {
	return &Service{st: st, auth: a, clock: clock, bounds: bounds}
}

// User is an account as the admin panel shows it: no financial data, only counts.
type User struct {
	ID                 uuid.UUID
	Email              string
	DisplayName        string
	IsDemo             bool
	IsAdmin            bool
	Disabled           bool
	MustChangePassword bool
	CreatedAt          time.Time
	LastLoginAt        *time.Time
	Limits             instance.Limits
	Usage              Usage
}

func userOf(u db.User) User {
	limits, _ := instance.ParseLimits(u.Limits)
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, IsAdmin: u.IsAdmin,
		Disabled: u.DisabledAt != nil, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt,
		LastLoginAt: u.LastLoginAt, Limits: limits}
}

// UsersPage is one page of ListUsers. Next is the cursor of the following page ("" at the end).
type UsersPage struct {
	Users  []User
	Next   string
	Totals Totals
}

// Totals count the instance's accounts.
type Totals struct {
	Users, Admins, Disabled, Demo, Active7, Active30 int64
}

// ListUsers returns up to limit accounts, newest first, whose e-mail contains search.
func (s *Service) ListUsers(ctx context.Context, search, cursor string, limit int32) (UsersPage, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	search = strings.TrimSpace(search)
	if len(search) > 254 || strings.ContainsRune(search, 0) {
		return UsersPage{}, invalid("search", "is not a valid search")
	}
	before, beforeID, err := parseCursor(cursor)
	if err != nil {
		return UsersPage{}, err
	}
	var page UsersPage
	now := s.clock.Now()
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.AdminListUsers(ctx, db.AdminListUsersParams{Search: search, BeforeCreated: before, BeforeID: beforeID, PageSize: limit + 1})
		if err != nil {
			return err
		}
		t, err := q.AdminCountUsers(ctx, db.AdminCountUsersParams{Active7: now.AddDate(0, 0, -7), Active30: now.AddDate(0, 0, -30)})
		if err != nil {
			return err
		}
		page.Totals = Totals{Users: t.Total, Admins: t.Admins, Disabled: t.Disabled, Demo: t.Demo, Active7: t.Active7, Active30: t.Active30}
		if len(rows) > int(limit) {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			page.Next = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "_" + last.ID.String()
		}
		for i := range rows {
			page.Users = append(page.Users, userOf(rows[i]))
		}
		return nil
	})
	if err != nil {
		return UsersPage{}, err
	}
	for i := range page.Users {
		if page.Users[i].Usage, err = s.usage(ctx, page.Users[i].ID); err != nil {
			return UsersPage{}, err
		}
	}
	return page, nil
}

func parseCursor(c string) (*time.Time, uuid.UUID, error) {
	if c == "" {
		return nil, uuid.Nil, nil
	}
	at, id, ok := strings.Cut(c, "_")
	t, err1 := time.Parse(time.RFC3339Nano, at)
	u, err2 := uuid.Parse(id)
	if !ok || err1 != nil || err2 != nil {
		return nil, uuid.Nil, invalid("cursor", "is not a page cursor")
	}
	return &t, u, nil
}

// GetUser returns one account with its usage.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		row, err := q.GetUser(ctx, id)
		u = userOf(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user: %w", platform.ErrNotFound)
	}
	if err != nil {
		return User{}, err
	}
	u.Usage, err = s.usage(ctx, id)
	return u, err
}

// CreateInput creates an account. An empty Password means "generate a one-time password",
// which the user must replace at the first sign-in.
type CreateInput struct {
	Email, DisplayName, Password string
	Admin                        bool
}

// CreateUser creates an account and returns it with the one-time password it generated (empty
// when the administrator set the password).
func (s *Service) CreateUser(ctx context.Context, in CreateInput) (User, string, error) {
	oneTime := ""
	if in.Password == "" {
		oneTime = OneTimePassword()
		in.Password = oneTime
	}
	u, err := s.auth.CreateUser(ctx, auth.SignupInput{Email: in.Email, DisplayName: in.DisplayName, Password: in.Password,
		Admin: in.Admin, MustChangePassword: oneTime != ""})
	if err != nil {
		return User{}, "", err
	}
	created, err := s.GetUser(ctx, u.ID)
	return created, oneTime, err
}

// UpdateInput changes an account; nil fields stay as they are.
type UpdateInput struct {
	Admin       *bool
	Disabled    *bool
	DisplayName *string
	Limits      *instance.Limits
}

// UpdateUser applies in. actor is the administrator: they can't disable themselves, and the last
// active administrator can't be demoted or disabled.
func (s *Service) UpdateUser(ctx context.Context, actor, id uuid.UUID, in UpdateInput) (User, error) {
	if id == actor && in.Disabled != nil && *in.Disabled {
		return User{}, invalid("disabled", "you can't disable your own account")
	}
	var name string
	if in.DisplayName != nil {
		var err error
		if name, err = auth.CheckDisplayName(*in.DisplayName); err != nil {
			return User{}, err
		}
	}
	var limits []byte
	if in.Limits != nil {
		raw, _ := json.Marshal(in.Limits) // plain data; it can't fail
		if _, err := instance.ParseLimits(raw); err != nil {
			return User{}, err
		}
		limits = raw
	}
	now := s.clock.Now()
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return err
		}
		if u.IsDemo && in.Admin != nil && *in.Admin {
			return invalid("admin", "a demo account can't be an administrator")
		}
		demote := in.Admin != nil && !*in.Admin && u.IsAdmin
		disable := in.Disabled != nil && *in.Disabled && u.DisabledAt == nil
		if demote || disable {
			if err := auth.GuardLastAdmin(ctx, q, id); err != nil {
				return err
			}
		}
		if in.Admin != nil {
			if _, err := q.SetUserAdmin(ctx, db.SetUserAdminParams{ID: id, IsAdmin: *in.Admin}); err != nil {
				return err
			}
		}
		if in.Disabled != nil {
			var at *time.Time
			if *in.Disabled {
				at = &now
				if _, err := q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: id, KeepTokenHash: []byte{}}); err != nil {
					return err
				}
			}
			if _, err := q.SetUserDisabled(ctx, db.SetUserDisabledParams{ID: id, DisabledAt: at}); err != nil {
				return err
			}
		}
		if in.DisplayName != nil {
			if err := q.SetUserDisplayName(ctx, db.SetUserDisplayNameParams{ID: id, DisplayName: name}); err != nil {
				return err
			}
		}
		if limits != nil {
			if _, err := q.SetUserLimits(ctx, db.SetUserLimitsParams{ID: id, Limits: limits}); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user: %w", platform.ErrNotFound)
	}
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

// ResetPassword gives the account a new one-time password (returned once), signs it out
// everywhere and makes the user choose a new password at the next sign-in.
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID) (string, error) {
	pw := OneTimePassword()
	phc, err := s.auth.HashPassword(ctx, "password", pw)
	if err != nil {
		return "", err
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetUser(ctx, id); err != nil {
			return err
		}
		if err := q.SetUserPasswordMustChange(ctx, db.SetUserPasswordMustChangeParams{ID: id, PasswordHash: phc}); err != nil {
			return err
		}
		_, err := q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: id, KeepTokenHash: []byte{}})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("user: %w", platform.ErrNotFound)
	}
	return pw, err
}

// RevokeSessions signs the account out everywhere and returns how many sessions ended.
func (s *Service) RevokeSessions(ctx context.Context, id uuid.UUID) (int64, error) {
	var n int64
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.GetUser(ctx, id); err != nil {
			return err
		}
		var err error
		n, err = q.DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: id, KeepTokenHash: []byte{}})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("user: %w", platform.ErrNotFound)
	}
	return n, err
}

// DeleteUser deletes an account and everything it owns. confirmEmail must repeat its address;
// administrators delete their own account from Settings → Profile instead.
func (s *Service) DeleteUser(ctx context.Context, actor, id uuid.UUID, confirmEmail string) (User, error) {
	if id == actor {
		return User{}, invalid("id", "delete your own account from Settings → Profile")
	}
	var gone User
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(confirmEmail), u.Email) {
			return invalid("confirmEmail", "must repeat the account's e-mail address")
		}
		if err := auth.GuardLastAdmin(ctx, q, id); err != nil {
			return err
		}
		gone = userOf(u)
		_, err = q.DeleteUser(ctx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user: %w", platform.ErrNotFound)
	}
	return gone, err
}

// SetAdminByEmail grants or revokes the administrator role (finuance admin grant-admin|revoke-admin).
func (s *Service) SetAdminByEmail(ctx context.Context, email string, admin bool) (User, error) {
	u, err := s.auth.UserByEmail(ctx, email)
	if err != nil {
		return User{}, err
	}
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if u.IsDemo && admin {
			return invalid("email", "a demo account can't be an administrator")
		}
		if !admin {
			if err := auth.GuardLastAdmin(ctx, q, u.ID); err != nil {
				return err
			}
		}
		_, err := q.SetUserAdmin(ctx, db.SetUserAdminParams{ID: u.ID, IsAdmin: admin})
		return err
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, u.ID)
}

// oneTimeAlphabet leaves out look-alike characters (0/O, 1/l/I).
const oneTimeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// OneTimePassword returns 16 random characters in four groups (about 79 bits), long enough for
// the password rules and easy to read out.
func OneTimePassword() string {
	const n = len(oneTimeAlphabet)
	limit := byte(256 / n * n) // reject the top bytes, so every character is equally likely
	var sb strings.Builder
	buf := make([]byte, 32)
	for picked := 0; picked < 16; {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		for _, c := range buf {
			if c >= limit || picked == 16 {
				continue
			}
			if picked > 0 && picked%4 == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(oneTimeAlphabet[int(c)%n])
			picked++
		}
	}
	return sb.String()
}

func invalid(field, msg string) error {
	ve := &platform.ValidationError{}
	ve.Add(field, msg)
	return ve
}
