package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

// AdminInput is one `finuance admin` command.
type AdminInput struct {
	Command     string // create-user | set-password | grant-admin | revoke-admin
	Email       string
	DisplayName string
	Admin       bool      // create-user -admin
	Password    io.Reader // stdin; the first line is the password (create-user, set-password)
}

// Admin runs a user-administration command against the database (app role, like the api):
//
//	finuance admin create-user --email … [--display-name …] [--admin] --password-stdin
//	finuance admin set-password --email … --password-stdin
//	finuance admin grant-admin|revoke-admin --email …
//
// create-user works even when FINUANCE_ALLOW_SIGNUP=false (personal instances). set-password is the
// way to recover a forgotten password — there is no email sending — and signs the user out
// everywhere. The password is read from stdin so it never appears in shell history or ps.
func Admin(ctx context.Context, cfg config.Config, in AdminInput, out func(format string, args ...any)) error {
	var password string
	if in.Command == "create-user" || in.Command == "set-password" {
		var err error
		if password, err = readPassword(in.Password); err != nil {
			return err
		}
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL, 2, noop.NewTracerProvider())
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)
	svc := NewAuth(st, cfg)
	switch in.Command {
	case "create-user":
		u, err := svc.CreateUser(ctx, auth.SignupInput{Email: in.Email, Password: password, DisplayName: in.DisplayName, Admin: in.Admin})
		if err != nil {
			return err
		}
		role := "user"
		if u.IsAdmin {
			role = "administrator"
		}
		out("created %s %s (%s)", role, u.Email, u.ID)
		return nil
	case "grant-admin", "revoke-admin":
		u, err := NewAdmin(st, svc, cfg).SetAdminByEmail(ctx, in.Email, in.Command == "grant-admin")
		if err != nil {
			return err
		}
		if u.IsAdmin {
			out("%s is an administrator", u.Email)
		} else {
			out("%s is no longer an administrator", u.Email)
		}
		return nil
	case "set-password":
		if err := svc.SetPassword(ctx, in.Email, password); err != nil {
			return err
		}
		out("password set for %s; all of their sessions were signed out", strings.TrimSpace(in.Email))
		return nil
	}
	return fmt.Errorf("unknown admin command %q (want create-user, set-password, grant-admin or revoke-admin)", in.Command)
}

// readPassword takes the first line of r, without the line ending.
func readPassword(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("the password is read from stdin (--password-stdin)")
	}
	line, err := bufio.NewReader(io.LimitReader(r, auth.MaxPasswordBytes+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("empty password on stdin")
	}
	return line, nil
}

// SeedInput is finuance seed-demo's input.
type SeedInput struct {
	Email string
	// Rows: about this many demo rows instead of the demo year (0: the demo year).
	Rows int
	// Users > 0 seeds that many users for the load test (PLAN 17.5), named after Email with a
	// number (loadtest@example.test → loadtest+001@example.test …), creating the missing ones with
	// the password from Password; users that already have a demo account are skipped.
	Users    int
	Password io.Reader
}

// SeedDemo gives the user with this email the demo account and its committed demo data
// (finuance seed-demo -email …). It refuses when the user already has a demo account. With
// Users it seeds a whole set of load-test users.
func SeedDemo(ctx context.Context, cfg config.Config, in SeedInput, out func(format string, args ...any)) error {
	pool, err := store.Open(ctx, cfg.DatabaseURL, 2, noop.NewTracerProvider())
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)
	svc := NewAuth(st, cfg)
	led := ledger.New(st, platform.SystemClock{})
	days := 0
	if in.Rows > 0 {
		days = int(math.Ceil(float64(in.Rows) / ledger.DemoRowsPerDay))
	}
	seed := func(u auth.User) error {
		s, err := led.SeedDemoDays(ctx, u.ID, days)
		if err != nil {
			return err
		}
		out("seeded %q for %s: %d transactions (%s – %s)", s.Account.Name, u.Email, s.Result.New, s.Account.FirstOn, s.Account.LastOn)
		return nil
	}
	if in.Users <= 0 {
		u, err := svc.UserByEmail(ctx, in.Email)
		if err != nil {
			return err
		}
		return seed(u)
	}
	password, err := readPassword(in.Password)
	if err != nil {
		return err
	}
	local, domain, ok := strings.Cut(strings.TrimSpace(in.Email), "@")
	if !ok || local == "" || domain == "" {
		return fmt.Errorf("-email %q: want name@domain (the users become name+001@domain …)", in.Email)
	}
	for i := 1; i <= in.Users; i++ {
		email := fmt.Sprintf("%s+%03d@%s", local, i, domain)
		u, err := svc.UserByEmail(ctx, email)
		if errors.Is(err, platform.ErrNotFound) {
			u, err = svc.CreateUser(ctx, auth.SignupInput{Email: email, Password: password})
		}
		if err != nil {
			return err
		}
		if err := seed(u); errors.Is(err, ledger.ErrConflict) {
			out("%s already has demo data", email)
		} else if err != nil {
			return err
		}
	}
	return nil
}
