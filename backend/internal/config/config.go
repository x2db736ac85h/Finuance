// Package config loads and validates the FINUANCE_* environment variables (docs/PLAN.md §3.5).
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Env is the deployment environment; it controls log format and cookie defaults.
type Env string

// Environments.
const (
	EnvDev  Env = "dev"
	EnvTest Env = "test"
	EnvProd Env = "prod"
)

// Config is the validated application configuration.
type Config struct {
	Env                  Env
	HTTPAddr             string
	AdminAddr            string
	PublicOrigin         *url.URL
	DatabaseURL          string
	MigrateDatabaseURL   string
	DBMaxConns           int32
	SessionTTL           time.Duration
	SessionIdleTTL       time.Duration
	CookieSecure         bool
	AllowSignup          bool
	DemoMode             bool
	MaxUploadBytes       int64
	MaxLegacyImportBytes int64
	OTLPEndpoint         string
	LogLevel             slog.Level
	TrustedProxies       []netip.Prefix
	AIBaseURL            *url.URL // nil = assistant disabled
	AIAPIKey             string
	AIDefaultModel       string
	AIAllowRemote        bool
	AITimeout            time.Duration
	AIMaxConcurrent      int
	AIMaxChats           int
}

// Requirement lists the variables a subcommand cannot run without.
type Requirement []string

// Requirements per subcommand.
var (
	ForServe       = Requirement{"FINUANCE_PUBLIC_ORIGIN", "FINUANCE_DATABASE_URL"}
	ForWorker      = Requirement{"FINUANCE_DATABASE_URL"}
	ForMigrate     = Requirement{"FINUANCE_MIGRATE_DATABASE_URL"}
	ForHealthcheck = Requirement{}
)

// FieldError is one invalid or missing variable.
type FieldError struct {
	Var     string
	Message string
}

// ValidationError reports every problem found, so a misconfigured deploy is fixed in one go.
type ValidationError []FieldError

func (v ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("invalid configuration:")
	for _, e := range v {
		fmt.Fprintf(&b, "\n  %s: %s", e.Var, e.Message)
	}
	return b.String()
}

// Lookup reads one environment variable; os.LookupEnv in production, a map in tests.
type Lookup func(string) (string, bool)

// Load reads the configuration from lookup, applies the §3.5 defaults and validates it.
// The returned error, if any, is a ValidationError naming every bad variable.
func Load(lookup Lookup, req Requirement) (Config, error) {
	p := parser{lookup: lookup}
	for _, name := range req {
		if v, _ := lookup(name); strings.TrimSpace(v) == "" {
			p.fail(name, "is required")
		}
	}
	c := Config{
		Env:                  Env(p.str("FINUANCE_ENV", "dev")),
		HTTPAddr:             p.addr("FINUANCE_HTTP_ADDR", ":8080"),
		AdminAddr:            p.addr("FINUANCE_ADMIN_ADDR", ":9090"),
		PublicOrigin:         p.origin("FINUANCE_PUBLIC_ORIGIN"),
		DatabaseURL:          p.dsn("FINUANCE_DATABASE_URL"),
		MigrateDatabaseURL:   p.dsn("FINUANCE_MIGRATE_DATABASE_URL"),
		DBMaxConns:           int32(p.int("FINUANCE_DB_MAX_CONNS", 10, 1, 1000)), //nolint:gosec // bounded to 1..1000 by p.int
		SessionTTL:           p.duration("FINUANCE_SESSION_TTL", 720*time.Hour),
		SessionIdleTTL:       p.duration("FINUANCE_SESSION_IDLE_TTL", 168*time.Hour),
		AllowSignup:          p.bool("FINUANCE_ALLOW_SIGNUP", true),
		DemoMode:             p.bool("FINUANCE_DEMO_MODE", false),
		MaxUploadBytes:       int64(p.int("FINUANCE_MAX_UPLOAD_BYTES", 20<<20, 1024, 1<<31)),
		MaxLegacyImportBytes: int64(p.int("FINUANCE_MAX_LEGACY_IMPORT_BYTES", 200<<20, 1024, 1<<33)),
		OTLPEndpoint:         p.str("FINUANCE_OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:             p.level("FINUANCE_LOG_LEVEL", slog.LevelInfo),
		TrustedProxies:       p.prefixes("FINUANCE_TRUSTED_PROXIES"),
		AIBaseURL:            p.httpURL("FINUANCE_AI_BASE_URL"),
		AIAPIKey:             p.str("FINUANCE_AI_API_KEY", ""),
		AIDefaultModel:       p.str("FINUANCE_AI_DEFAULT_MODEL", ""),
		AIAllowRemote:        p.bool("FINUANCE_AI_ALLOW_REMOTE", false),
		AITimeout:            p.duration("FINUANCE_AI_TIMEOUT", 120*time.Second),
		AIMaxConcurrent:      p.int("FINUANCE_AI_MAX_CONCURRENT", 2, 1, 64),
		AIMaxChats:           p.int("FINUANCE_AI_MAX_CHATS", 10, 1, 1000),
	}
	switch c.Env {
	case EnvDev, EnvTest, EnvProd:
	default:
		p.fail("FINUANCE_ENV", fmt.Sprintf("must be dev, test or prod, got %q", c.Env))
	}
	c.CookieSecure = p.bool("FINUANCE_COOKIE_SECURE", true)
	if !c.CookieSecure && c.Env == EnvProd {
		p.fail("FINUANCE_COOKIE_SECURE", "false is only allowed when FINUANCE_ENV is dev or test")
	}
	if c.SessionIdleTTL > c.SessionTTL {
		p.fail("FINUANCE_SESSION_IDLE_TTL", "must not exceed FINUANCE_SESSION_TTL")
	}
	if len(p.errs) > 0 {
		return Config{}, p.errs
	}
	return c, nil
}

type parser struct {
	lookup Lookup
	errs   ValidationError
}

func (p *parser) fail(name, msg string) {
	for _, e := range p.errs {
		if e.Var == name {
			return // one message per variable is enough
		}
	}
	p.errs = append(p.errs, FieldError{Var: name, Message: msg})
}

func (p *parser) raw(name string) (string, bool) {
	v, ok := p.lookup(name)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (p *parser) str(name, def string) string {
	if v, ok := p.raw(name); ok {
		return v
	}
	return def
}

func (p *parser) int(name string, def, lo, hi int) int {
	v, ok := p.raw(name)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		p.fail(name, fmt.Sprintf("must be an integer between %d and %d, got %q", lo, hi, v))
		return def
	}
	return n
}

func (p *parser) bool(name string, def bool) bool {
	v, ok := p.raw(name)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail(name, fmt.Sprintf("must be true or false, got %q", v))
		return def
	}
	return b
}

func (p *parser) duration(name string, def time.Duration) time.Duration {
	v, ok := p.raw(name)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		p.fail(name, fmt.Sprintf("must be a positive duration like 30s or 720h, got %q", v))
		return def
	}
	return d
}

func (p *parser) level(name string, def slog.Level) slog.Level {
	v, ok := p.raw(name)
	if !ok {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(v)); err != nil {
		p.fail(name, fmt.Sprintf("must be debug, info, warn or error, got %q", v))
		return def
	}
	return l
}

func (p *parser) addr(name, def string) string {
	v := p.str(name, def)
	_, port, err := net.SplitHostPort(v)
	if err != nil {
		p.fail(name, fmt.Sprintf("must be host:port or :port, got %q", v))
		return def
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		p.fail(name, fmt.Sprintf("has an invalid port in %q", v))
	}
	return v
}

func (p *parser) httpURL(name string) *url.URL {
	v, ok := p.raw(name)
	if !ok {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		p.fail(name, fmt.Sprintf("must be an absolute http(s) URL, got %q", v))
		return nil
	}
	return u
}

func (p *parser) origin(name string) *url.URL {
	u := p.httpURL(name)
	if u != nil && (u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "") {
		p.fail(name, "must be an origin (scheme://host[:port]) without a path, query or fragment")
		return nil
	}
	if u != nil {
		u.Path = ""
	}
	return u
}

func (p *parser) dsn(name string) string {
	v, ok := p.raw(name)
	if !ok {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		p.fail(name, "must be a postgres:// URL with a host") // never echo a DSN: it holds a password
		return ""
	}
	return v
}

func (p *parser) prefixes(name string) []netip.Prefix {
	v, ok := p.raw(name)
	if !ok {
		return nil
	}
	var out []netip.Prefix
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		pf, err := netip.ParsePrefix(s)
		if err != nil {
			if a, err2 := netip.ParseAddr(s); err2 == nil {
				pf = netip.PrefixFrom(a, a.BitLen())
			} else {
				p.fail(name, fmt.Sprintf("must be a comma-separated list of CIDRs, got %q", s))
				return nil
			}
		}
		out = append(out, pf.Masked())
	}
	return out
}
