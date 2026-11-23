// Command finuance-sim is the traffic simulator (PLAN Phase 25, ADR 0043): personas that use a lab
// instance through its front door all day. It is its own binary and image; the production image
// doesn't carry it.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/actions"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/control"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

var version = "dev"

const usage = `usage: finuance-sim <command> [flags]

commands:
  run -target=https://lab.example [-listen=:8090] [-personas=N] [-intensity=‰] [-tz=Europe/Warsaw]
      [-prometheus=http://prometheus:9090 -namespace=finuance]
                    play the personas against a lab, with the control API, the tuning page and
                    /metrics on -listen; refuses an instance whose /meta does not report
                    simulation: true
  seed -target=https://lab.example [-count=N] [-rows=N | -density=N] [-years=N] [-spread=%] [-variety=N]
                    give the first N animals a history of that profile through the normal upload
                    (ADR 0044) and exit: a rebuild, or only the days since for an animal that
                    already has that profile; -rows picks the density for about that many
                    transactions each
  users -count=N -out=FILE
                    write the first N animals' e-mail addresses and passwords as JSON (mode 0600),
                    for k6 (loadtest/finuance.js, USERS_FILE)
  password <animal> print one persona's password (to sign in as it and look at its screens)
  version           print the version

The seed comes from FINUANCE_SIM_SEED (at least 32 bytes), the same one given to
finuance admin synthetic-users on the lab, and the control API's token from
FINUANCE_SIM_CONTROL_TOKEN (at least 32 bytes). Neither is ever printed.
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
	case "seed":
		return seed(ctx, args[1:], getenv, stdout, stderr)
	case "users":
		return users(args[1:], getenv, stderr)
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
	listen := fs.String("listen", envOr(getenv, "FINUANCE_SIM_LISTEN", ":8090"), "the control API's, the page's and /metrics' address")
	grafana := fs.String("grafana", getenv("FINUANCE_SIM_GRAFANA_URL"), "Grafana's address, for the feed's links")
	prom := fs.String("prometheus", getenv("FINUANCE_SIM_PROMETHEUS_URL"), "the lab's Prometheus, for the page's live strip of what the load costs")
	namespace := fs.String("namespace", envOr(getenv, "FINUANCE_SIM_LAB_NAMESPACE", "finuance"), "the lab's namespace in that Prometheus")
	origin := fs.String("origin", getenv("FINUANCE_SIM_ORIGIN"), "the Origin to send, when the target is reached by another name than the lab's public one")
	caFile := fs.String("ca-file", getenv("FINUANCE_SIM_CA_FILE"), "a PEM file with the authority that signed the lab's certificate, besides the system's")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim: -tz:", err)
		return 2
	}
	transport, err := transportWith(*caFile)
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim: -ca-file:", err)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	e, err := sim.New(sim.Options{Target: *target, Origin: *origin, Transport: transport, Seed: []byte(getenv("FINUANCE_SIM_SEED")), Version: version,
		Actions: actions.All(), Config: cfg, Location: loc, Log: log, Metrics: sim.NewMetrics(reg)})
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	h, err := control.Handler(control.Options{Engine: e, Token: getenv("FINUANCE_SIM_CONTROL_TOKEN"), Version: version,
		Grafana: *grafana, Prometheus: *prom, Namespace: *namespace, Registry: reg, Log: log})
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute, IdleTimeout: 2 * time.Minute}
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	log.InfoContext(ctx, "starting", slog.String("target", *target), slog.String("listen", *listen), slog.Int("personas", cfg.Personas))
	code := 0
	if err := e.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		code = 1
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		code = 1
	}
	return code
}

// transportWith is the default transport with room for load mode's connections, trusting also the
// authority in caFile ("" for none): a lab with a certificate of its own (the rehearsal's, a
// private authority).
func transportWith(caFile string) (http.RoundTripper, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// the default keeps two idle connections per host: at hundreds of requests a second the rest
	// would be opened and closed again, until the local ports run out (ADR 0044)
	t.MaxIdleConns, t.MaxIdleConnsPerHost = 512, 256
	if caFile == "" {
		return t, nil
	}
	pem, err := os.ReadFile(caFile) //nolint:gosec // the operator's own file
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", caFile)
	}
	t.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return t, nil
}

// seed builds the first -count animals' histories (actions.Seed) and reports each one.
func seed(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := sim.Defaults()
	target := fs.String("target", getenv("FINUANCE_SIM_TARGET"), "the lab's public address (FINUANCE_SIM_TARGET)")
	origin := fs.String("origin", getenv("FINUANCE_SIM_ORIGIN"), "the Origin to send, when the target is reached by another name")
	caFile := fs.String("ca-file", getenv("FINUANCE_SIM_CA_FILE"), "a PEM file with the authority that signed the lab's certificate")
	count := fs.Int("count", 10, fmt.Sprintf("how many animals, the first N (1 … %d)", sim.MaxLoadPersonas))
	rows := fs.Int("rows", 0, "about this many transactions per animal (picks -density for -years)")
	fs.IntVar(&cfg.History.Density, "density", 1, fmt.Sprintf("the day's spending drawn this many times (1 … %d)", statements.MaxDensity))
	fs.IntVar(&cfg.History.Years, "years", 1, fmt.Sprintf("how many years back the history starts (1 … %d)", statements.MaxYears))
	fs.IntVar(&cfg.History.Spread, "spread", 100, "the amounts' range in percent of the default (50 … 300)")
	fs.IntVar(&cfg.History.Variety, "variety", 1, fmt.Sprintf("shops per merchant (1 … %d)", statements.MaxVariety))
	parallel := fs.Int("parallel", sim.MaxRebuilds, "how many animals at once")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	if *count < 1 || *count > sim.MaxLoadPersonas {
		fmt.Fprintf(stderr, "finuance-sim: -count must be between 1 and %d\n", sim.MaxLoadPersonas)
		return 2
	}
	if *rows > 0 {
		cfg.History.Density = statements.DensityFor(*rows, cfg.History.Normal().Years)
	}
	transport, err := transportWith(*caFile)
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim: -ca-file:", err)
		return 2
	}
	e, err := sim.New(sim.Options{Target: *target, Origin: *origin, Transport: transport, Seed: []byte(getenv("FINUANCE_SIM_SEED")),
		Version: version, Actions: actions.All(), Config: cfg})
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	if err := e.CheckTarget(ctx); err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	h, _ := e.Config()
	fmt.Fprintf(stdout, "seeding %d animals with history %q (about %d transactions each)\n", *count, h.History.Label(),
		statements.Estimate(h.History))
	var mu sync.Mutex
	err = actions.Seed(ctx, e, *count, *parallel, func(ev sim.Event) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", ev.Persona, ev.Action, ev.Result, ev.Duration.Round(time.Second))
	})
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	return 0
}

// users writes the first -count animals' credentials for k6, readable by this user only.
func users(args []string, getenv func(string) string, stderr io.Writer) int {
	fs := flag.NewFlagSet("users", flag.ContinueOnError)
	fs.SetOutput(stderr)
	count := fs.Int("count", 10, fmt.Sprintf("how many animals, the first N (1 … %d)", sim.MaxLoadPersonas))
	out := fs.String("out", "", "the file to write (JSON; replaced, mode 0600)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *out == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	ps, err := identity.First(*count)
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 2
	}
	type cred struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	creds := make([]cred, 0, len(ps))
	for _, p := range ps {
		pw, err := identity.Password([]byte(getenv("FINUANCE_SIM_SEED")), p.Slug)
		if err != nil {
			fmt.Fprintln(stderr, "finuance-sim: FINUANCE_SIM_SEED:", err)
			return 1
		}
		creds = append(creds, cred{p.Email, pw})
	}
	b, err := json.MarshalIndent(creds, "", "  ") //nolint:gosec // the command's purpose: the animals' passwords for k6, in a file only this user reads
	if err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	_ = os.Remove(*out) // a new file, so an older one's wider mode doesn't carry over
	if err := os.WriteFile(*out, append(b, '\n'), 0o600); err != nil {
		fmt.Fprintln(stderr, "finuance-sim:", err)
		return 1
	}
	return 0
}

func envOr(getenv func(string) string, name, def string) string {
	if v := getenv(name); v != "" {
		return v
	}
	return def
}
