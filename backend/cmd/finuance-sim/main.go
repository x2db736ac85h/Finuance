// Command finuance-sim is the traffic simulator (PLAN Phase 25, ADR 0043): personas that use a lab
// instance through its front door all day. It is its own binary and image; the production image
// doesn't carry it.
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
	"strings"
	"syscall"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/actions"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

var version = "dev"

const usage = `usage: finuance-sim <command> [flags]

commands:
  run -target=https://lab.example [-personas=N] [-intensity=‰] [-tz=Europe/Warsaw]
                    play the personas against a lab (FINUANCE_SIM_SEED in the environment);
                    refuses an instance whose /meta does not report simulation: true
  password <animal> print one persona's password (to sign in as it and look at its screens)
  version           print the version

The seed comes from FINUANCE_SIM_SEED (at least 32 bytes), the same one given to
finuance admin synthetic-users on the lab. It is never printed.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "finuance-sim", version)
		return 0
	case "password":
		if len(args) != 2 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		p, ok := identity.BySlug(args[1])
		if !ok {
			fmt.Fprintf(stderr, "finuance-sim: no persona %q (the animals: %s; then otter-2 …)\n", args[1], strings.Join(identity.Animals[:], ", "))
			return 2
		}
		pw, err := identity.Password([]byte(getenv("FINUANCE_SIM_SEED")), p.Slug)
		if err != nil {
			fmt.Fprintln(stderr, "finuance-sim: FINUANCE_SIM_SEED:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\t%s\n", p.Email, pw)
		return 0
	case "run":
		return runEngine(ctx, args[1:], getenv, stderr)
	}
	fmt.Fprintf(stderr, "finuance-sim: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func runEngine(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := sim.Defaults()
	target := fs.String("target", getenv("FINUANCE_SIM_TARGET"), "the lab's public address (FINUANCE_SIM_TARGET)")
	fs.IntVar(&cfg.Personas, "personas", cfg.Personas, "how many personas play (the first N)")
	fs.IntVar(&cfg.Intensity, "intensity", cfg.Intensity, "the pace in per mille of the characters' own (0 … 3000)")
	fs.BoolVar(&cfg.ActiveHours, "active-hours", cfg.ActiveHours, "follow the characters' daily curves")
	tz := fs.String("tz", "Europe/Warsaw", "the instance's time zone, for the active hours")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim: -tz:", err)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	e, err := sim.New(sim.Options{Target: *target, Seed: []byte(getenv("FINUANCE_SIM_SEED")), Version: version,
		Actions: actions.All(), Config: cfg, Location: loc, Log: log})
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	log.InfoContext(ctx, "starting", slog.String("target", *target), slog.Int("personas", cfg.Personas))
	if err := e.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	return 0
}
