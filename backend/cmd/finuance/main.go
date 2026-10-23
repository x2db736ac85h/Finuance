// Command finuance is the single backend binary: API server, worker, migrations and admin tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/x2db736ac85h/finuance/backend/internal/app"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// Set with -ldflags "-X main.version=… -X main.commit=… -X main.date=…".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

const usage = `usage: finuance <command> [flags]

commands:
  serve                  run the HTTP API (and the admin server with /metrics)
  worker                 run background jobs (River)
  migrate up|down|status|version
                         apply / roll back one / list / show schema migrations (owner role)
  seed-demo -email=…     create demo data for a user
  admin create-user -email=… [-display-name=…] -password-stdin
                         create an account (works with signup disabled)
  admin set-password -email=… -password-stdin
                         reset a password and sign the user out everywhere
  admin llm-probe [-connection=…] [-model=…] [-out=dir]
                         list the assistant's models and test one with made-up data
  healthcheck            exit 0 if the local API answers /healthz (container healthcheck)
  version                print build information

Configuration comes from FINUANCE_* environment variables (docs/PLAN.md §3.5).
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.LookupEnv)
	stop()
	os.Exit(code)
}

// run dispatches a subcommand and returns the process exit code (2 = usage error).
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, env config.Lookup) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	b := app.Build{Version: version, Commit: commit, Date: date}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := ""
	if cmd == "seed-demo" {
		fs.StringVar(&email, "email", "", "user to seed")
	}
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	load := func(req config.Requirement) (config.Config, bool) {
		cfg, err := config.Load(env, req)
		if err != nil {
			fmt.Fprintln(stderr, "finuance:", err)
			return cfg, false
		}
		return cfg, true
	}
	fail := func(err error) int {
		if err == nil || errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintln(stderr, "finuance:", err)
		return 1
	}

	switch cmd {
	case "version", "-version", "--version":
		fmt.Fprintf(stdout, "finuance %s (commit %s, built %s, %s %s/%s)\n", version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "serve", "worker":
		req := config.ForServe
		if cmd == "worker" {
			req = config.ForWorker
		}
		cfg, ok := load(req)
		if !ok {
			return 1
		}
		log := observability.NewLogger(stderr, cfg.Env, cfg.LogLevel)
		slog.SetDefault(log) // the composition root owns the process-wide default
		if cmd == "serve" {
			return fail(app.Serve(ctx, cfg, b, log))
		}
		return fail(app.Worker(ctx, cfg, b, log))
	case "migrate":
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "usage: finuance migrate up|down|status|version")
			return 2
		}
		cfg, ok := load(config.ForMigrate)
		if !ok {
			return 1
		}
		return fail(app.Migrate(ctx, cfg, fs.Arg(0), func(f string, a ...any) { fmt.Fprintf(stdout, f+"\n", a...) }))
	case "healthcheck":
		cfg, ok := load(config.ForHealthcheck)
		if !ok {
			return 1
		}
		return fail(app.Healthcheck(ctx, cfg))
	case "seed-demo":
		if email == "" || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: finuance seed-demo -email=…")
			return 2
		}
		cfg, ok := load(config.ForAdmin)
		if !ok {
			return 1
		}
		return fail(app.SeedDemo(ctx, cfg, email, func(f string, a ...any) { fmt.Fprintf(stdout, f+"\n", a...) }))
	case "admin":
		return admin(ctx, fs.Args(), stdin, stdout, stderr, load, fail)
	}
	fmt.Fprintf(stderr, "finuance: unknown command %q\n\n%s", cmd, usage)
	return 2
}

// admin parses `finuance admin <command> [flags]`; the flags follow the command.
func admin(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer,
	load func(config.Requirement) (config.Config, bool), fail func(error) int) int {
	const adminUsage = "usage: finuance admin create-user|set-password -email=… [-display-name=…] -password-stdin\n" +
		"       finuance admin llm-probe [-connection=…] [-model=…] [-out=dir]"
	if len(args) == 0 {
		fmt.Fprintln(stderr, adminUsage)
		return 2
	}
	if args[0] == "llm-probe" {
		fs := flag.NewFlagSet("admin llm-probe", flag.ContinueOnError)
		fs.SetOutput(stderr)
		var in app.ProbeInput
		fs.StringVar(&in.Connection, "connection", "", "connection name (default: the first)")
		fs.StringVar(&in.Model, "model", "", "model id (default: the connection's default)")
		fs.StringVar(&in.OutDir, "out", "", "write each request and raw response to this directory")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			fmt.Fprintln(stderr, adminUsage)
			return 2
		}
		cfg, ok := load(config.ForHealthcheck)
		if !ok {
			return 1
		}
		return fail(app.Probe(ctx, cfg, in, func(f string, a ...any) { fmt.Fprintf(stdout, f+"\n", a...) }))
	}
	fs := flag.NewFlagSet("admin "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := app.AdminInput{Command: args[0]}
	fs.StringVar(&in.Email, "email", "", "the user's email")
	fs.StringVar(&in.DisplayName, "display-name", "", "display name (create-user)")
	fromStdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if in.Email == "" || !*fromStdin || fs.NArg() != 0 {
		fmt.Fprintln(stderr, adminUsage)
		return 2
	}
	in.Password = stdin
	cfg, ok := load(config.ForAdmin)
	if !ok {
		return 1
	}
	return fail(app.Admin(ctx, cfg, in, func(f string, a ...any) { fmt.Fprintf(stdout, f+"\n", a...) }))
}
