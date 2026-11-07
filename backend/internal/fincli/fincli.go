// Package fincli is the fin command line (Phase 20, 20.13): the Finuance API from a terminal or a
// script, with a personal access token (ADR 0036). It talks to the API only through the client
// generated from api/openapi.yaml (internal/client/gen), prints tables, CSV or the API's JSON, and
// keeps its configuration (the instance's address and the token) in a file only its user can read.
package fincli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// Env is what a run reads and writes; main passes the process's, tests their own.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	ConfigDir      func() (string, error) // os.UserConfigDir
	HTTP           *http.Client
	Version        string
	Sleep          func(time.Duration) // between import status checks
}

// Config is the fin configuration file (~/.config/fin/config.json).
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

const usage = `fin — the Finuance API from the command line

Usage: fin [--url URL] [--token TOKEN] [--output table|json|csv] <command> [flags]

Commands:
  login --url URL          store the instance's address and a token read from stdin
  whoami                   the account the token belongs to
  accounts                 your accounts
  transactions             transactions: --account, --search, --category, --payee, --month,
                           --from, --to (inclusive), --limit; --export FILE writes the bank CSV
  import FILE              upload a CSV: --account NAME|ID; --commit imports it
  overview                 one month at a glance: --month YYYY-MM, --account
  groups                   your transaction groups
  shared                   your shared groups
  budgets                  category budgets
  goals                    goals and how they are doing
  ask QUESTION             ask the assistant; the answer streams to stdout
  version                  this program's version

FIN_URL and FIN_TOKEN override the configuration file. Make a token in Settings → Access tokens.
`

// Run runs one command and returns the exit code: 0 done, 1 failed, 2 wrong usage.
func Run(ctx context.Context, args []string, env Env) int {
	if env.HTTP == nil {
		env.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	if env.Sleep == nil {
		env.Sleep = time.Sleep
	}
	g := flag.NewFlagSet("fin", flag.ContinueOnError)
	g.SetOutput(env.Stderr)
	g.Usage = func() { fmt.Fprint(env.Stderr, usage) }
	url := g.String("url", "", "the instance's address")
	token := g.String("token", "", "a personal access token")
	output := g.String("output", "table", "table, json or csv")
	if err := g.Parse(args); err != nil {
		return 2
	}
	rest := g.Args()
	if len(rest) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch *output {
	case "table", "json", "csv":
	default:
		fmt.Fprintln(env.Stderr, "fin: --output must be table, json or csv")
		return 2
	}
	c := &cli{env: env, out: *output}
	name, cargs := rest[0], rest[1:]
	if name == "version" {
		fmt.Fprintln(env.Stdout, "fin", env.Version)
		return 0
	}
	if name == "help" || name == "-h" {
		fmt.Fprint(env.Stdout, usage)
		return 0
	}
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(env.Stderr, "fin: unknown command %q\n\n%s", name, usage)
		return 2
	}
	if name != "login" {
		if err := c.connect(*url, *token); err != nil {
			fmt.Fprintln(env.Stderr, "fin:", err)
			return 1
		}
	} else {
		c.url = *url
	}
	if err := cmd(ctx, c, cargs); err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(env.Stderr, "fin:", err)
			return 2
		}
		fmt.Fprintln(env.Stderr, "fin:", err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// cli is one run's state: the client and the output format.
type cli struct {
	env    Env
	out    string
	url    string
	token  string
	api    *gen.ClientWithResponses
	stream *gen.Client // for the answer's event stream, read as it arrives
}

func (c *cli) configPath() (string, error) {
	dir, err := c.env.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fin", "config.json"), nil
}

func (c *cli) readConfig() (Config, error) {
	var cfg Config
	p, err := c.configPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p) //nolint:gosec // the user's own configuration file
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

func (c *cli) writeConfig(cfg Config) (string, error) {
	p, err := c.configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	// only this user may read the token
	return p, os.WriteFile(p, append(b, '\n'), 0o600)
}

// connect picks the address and token (flags, then FIN_URL/FIN_TOKEN, then the file) and builds
// the client.
func (c *cli) connect(url, token string) error {
	cfg, err := c.readConfig()
	if err != nil {
		return fmt.Errorf("reading the configuration: %w", err)
	}
	pick := func(flagV, envName, fileV string) string {
		if flagV != "" {
			return flagV
		}
		if v := c.env.Getenv(envName); v != "" {
			return v
		}
		return fileV
	}
	c.url, c.token = pick(url, "FIN_URL", cfg.URL), pick(token, "FIN_TOKEN", cfg.Token)
	if c.url == "" || c.token == "" {
		return errors.New("not signed in: run fin login --url https://… (or set FIN_URL and FIN_TOKEN)")
	}
	return c.newClient()
}

func (c *cli) newClient() error {
	base := strings.TrimRight(c.url, "/") + "/api/v1"
	edit := gen.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+c.token)
		r.Header.Set("User-Agent", "fin/"+c.env.Version)
		return nil
	})
	var err error
	if c.api, err = gen.NewClientWithResponses(base, gen.WithHTTPClient(c.env.HTTP), edit); err != nil {
		return err
	}
	c.stream, err = gen.NewClient(base, gen.WithHTTPClient(c.env.HTTP), edit)
	return err
}

// apiError turns a failed answer into an error with the API's problem title and detail.
func apiError(status int, body []byte) error {
	var p gen.Problem
	if json.Unmarshal(body, &p) == nil && p.Title != "" {
		msg := p.Title
		if p.Detail != nil && *p.Detail != "" {
			msg += ": " + *p.Detail
		}
		if p.Errors != nil {
			for _, fe := range *p.Errors {
				msg += fmt.Sprintf("; %s %s", fe.Field, fe.Message)
			}
		}
		if status == http.StatusUnauthorized {
			msg += " (is the token still valid? fin login makes a new configuration)"
		}
		return fmt.Errorf("%s (%d %s)", msg, status, p.Code)
	}
	return fmt.Errorf("the server answered %d %s", status, http.StatusText(status))
}

// check returns the API's problem when an answer is not one of the wanted statuses.
func check(status int, body []byte, want ...int) error {
	for _, w := range want {
		if status == w {
			return nil
		}
	}
	return apiError(status, body)
}

// table is what a command prints: rows under a header, or the API's own JSON with --output json.
type table struct {
	head []string
	rows [][]string
	raw  any
}

func (c *cli) print(t table) error {
	w := c.env.Stdout
	switch c.out {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(t.raw)
	case "csv":
		cw := csv.NewWriter(w)
		if err := cw.Write(t.head); err != nil {
			return err
		}
		if err := cw.WriteAll(t.rows); err != nil {
			return err
		}
		cw.Flush()
		return cw.Error()
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.head, "\t"))
	for _, r := range t.rows {
		fmt.Fprintln(tw, strings.Join(clean(r), "\t"))
	}
	return tw.Flush()
}

// clean keeps tabs and line breaks in values from breaking the table's columns, and shows an
// empty value as a dash (CSV keeps it empty).
func clean(r []string) []string {
	out := make([]string, len(r))
	for i, v := range r {
		out[i] = orDash(strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(v))
	}
	return out
}

// amount formats minor units in the currency's decimals: -4498 PLN → "-44.98 PLN".
func amount(minor int64, currency string) string {
	exp, ok := money.Exponent(currency)
	if !ok {
		exp = 2
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	if exp == 0 {
		return fmt.Sprintf("%s%d %s", sign, minor, currency)
	}
	div := int64(1)
	for range exp {
		div *= 10
	}
	return strings.TrimSpace(fmt.Sprintf("%s%d.%0*d %s", sign, minor/div, exp, minor%div, currency))
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
