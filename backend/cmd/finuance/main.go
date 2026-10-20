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
  admin                  user administration
  healthcheck            exit 0 if the local API answers /healthz (container healthcheck)
  version                print build information

Configuration comes from FINUANCE_* environment variables (docs/PLAN.md §3.5).
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv)
	stop()
	os.Exit(code)
}

// run dispatches a subcommand and returns the process exit code (2 = usage error).
func run(ctx context.Context, args []string, stdout, stderr io.Writer, env config.Lookup) int {
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
		fmt.Fprintln(stderr, "finuance seed-demo: not yet implemented (Phase 9)")
		return 1
	case "admin":
		fmt.Fprintln(stderr, "finuance admin: not yet implemented (Phase 5)")
		return 1
	}
	fmt.Fprintf(stderr, "finuance: unknown command %q\n\n%s", cmd, usage)
	return 2
}
