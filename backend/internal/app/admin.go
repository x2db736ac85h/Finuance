package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	Command     string // create-user | set-password
	Email       string
	DisplayName string
	Password    io.Reader // stdin; the first line is the password
}

// Admin runs a user-administration command against the database (app role, like the api):
//
//	finuance admin create-user --email … [--display-name …] --password-stdin
//	finuance admin set-password --email … --password-stdin
//
// create-user works even when FINUANCE_ALLOW_SIGNUP=false (personal instances). set-password is the
// way to recover a forgotten password — there is no email sending — and signs the user out
// everywhere. The password is read from stdin so it never appears in shell history or ps.
func Admin(ctx context.Context, cfg config.Config, in AdminInput, out func(format string, args ...any)) error {
	password, err := readPassword(in.Password)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL, 2, noop.NewTracerProvider())
	if err != nil {
		return err
	}
	defer pool.Close()
	svc := NewAuth(store.New(pool), cfg)
	switch in.Command {
	case "create-user":
		u, err := svc.CreateUser(ctx, auth.SignupInput{Email: in.Email, Password: password, DisplayName: in.DisplayName})
		if err != nil {
			return err
		}
		out("created user %s (%s)", u.Email, u.ID)
		return nil
	case "set-password":
		if err := svc.SetPassword(ctx, in.Email, password); err != nil {
			return err
		}
		out("password set for %s; all of their sessions were signed out", strings.TrimSpace(in.Email))
		return nil
	}
	return fmt.Errorf("unknown admin command %q (want create-user or set-password)", in.Command)
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

// SeedDemo gives the user with this email the demo account and its committed demo year
// (finuance seed-demo --email …). It refuses when the user already has a demo account.
func SeedDemo(ctx context.Context, cfg config.Config, email string, out func(format string, args ...any)) error {
	pool, err := store.Open(ctx, cfg.DatabaseURL, 2, noop.NewTracerProvider())
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)
	u, err := NewAuth(st, cfg).UserByEmail(ctx, email)
	if err != nil {
		return err
	}
	seed, err := ledger.New(st, platform.SystemClock{}).SeedDemo(ctx, u.ID)
	if err != nil {
		return err
	}
	acc, res := seed.Account, seed.Result
	out("seeded %q for %s: %d transactions (%s – %s)", acc.Name, u.Email, res.New, acc.FirstOn, acc.LastOn)
	return nil
}
