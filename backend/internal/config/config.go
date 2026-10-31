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
	DemoAssistant        bool // the assistant on a demo instance (off unless set; DemoAssistantMessages, DemoMaxRows)
	MaxUploadBytes       int64
	MaxLegacyImportBytes int64
	OTLPEndpoint         string
	LogLevel             slog.Level
	TrustedProxies       []netip.Prefix
	// AIConnections are the model servers the assistant may use; none = assistant disabled.
	AIConnections   []AIConnection
	AITimeout       time.Duration // one model request, first byte to last
	AIIdleTimeout   time.Duration // a model stream silent this long ends (also before its first token)
	AIMaxConcurrent int           // assistant runs at once per replica
	AIMaxChats      int           // chats kept per user (the oldest go first)
	AIRatePerHour   int           // messages per user per hour
	// Argon2id parameters for new password hashes (ADR 0010). Existing hashes keep theirs until
	// the next successful login rehashes them.
	Argon2MemoryKiB uint32
	Argon2Time      uint32
	Argon2Threads   uint8
}

// Requirement lists the variables a subcommand cannot run without.
type Requirement []string

// Requirements per subcommand.
var (
	ForServe       = Requirement{"FINUANCE_PUBLIC_ORIGIN", "FINUANCE_DATABASE_URL"}
	ForWorker      = Requirement{"FINUANCE_DATABASE_URL"}
	ForMigrate     = Requirement{"FINUANCE_MIGRATE_DATABASE_URL"}
	ForAdmin       = Requirement{"FINUANCE_DATABASE_URL"}
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
		DemoAssistant:        p.bool("FINUANCE_DEMO_ASSISTANT", false),
		MaxUploadBytes:       int64(p.int("FINUANCE_MAX_UPLOAD_BYTES", 20<<20, 1024, 1<<31)),
		MaxLegacyImportBytes: int64(p.int("FINUANCE_MAX_LEGACY_IMPORT_BYTES", 200<<20, 1024, 1<<33)),
		OTLPEndpoint:         p.str("FINUANCE_OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		LogLevel:             p.level("FINUANCE_LOG_LEVEL", slog.LevelInfo),
		TrustedProxies:       p.prefixes("FINUANCE_TRUSTED_PROXIES"),
		AITimeout:            p.duration("FINUANCE_AI_TIMEOUT", 120*time.Second),
		AIIdleTimeout:        p.duration("FINUANCE_AI_IDLE_TIMEOUT", 60*time.Second),
		AIMaxConcurrent:      p.int("FINUANCE_AI_MAX_CONCURRENT", 2, 1, 64),
		AIMaxChats:           p.int("FINUANCE_AI_MAX_CHATS", 25, 1, 1000),
		AIRatePerHour:        p.int("FINUANCE_AI_RATE_PER_HOUR", 30, 1, 10000),
		Argon2MemoryKiB:      uint32(p.int("FINUANCE_ARGON2_MEMORY_KIB", 19*1024, 8*1024, 1<<20)), //nolint:gosec // bounded by p.int
		Argon2Time:           uint32(p.int("FINUANCE_ARGON2_TIME", 2, 1, 10)),                     //nolint:gosec // bounded by p.int
		Argon2Threads:        uint8(p.int("FINUANCE_ARGON2_THREADS", 1, 1, 16)),                   //nolint:gosec // bounded by p.int
	}
	c.AIConnections = p.aiConnections(p.int("FINUANCE_AI_CONTEXT_TOKENS", 8192, 1024, 10_000_000))
	switch c.Env {
	case EnvDev, EnvTest, EnvProd:
	default:
		p.fail("FINUANCE_ENV", fmt.Sprintf("must be dev, test or prod, got %q", c.Env))
	}
	c.CookieSecure = p.bool("FINUANCE_COOKIE_SECURE", true)
	if !c.CookieSecure && c.Env == EnvProd {
		p.fail("FINUANCE_COOKIE_SECURE", "false is only allowed when FINUANCE_ENV is dev or test")
	}
	if c.DemoMode {
		c.demoLimits(&p)
	}
	if c.SessionIdleTTL > c.SessionTTL {
		p.fail("FINUANCE_SESSION_IDLE_TTL", "must not exceed FINUANCE_SESSION_TTL")
	}
	if len(p.errs) > 0 {
		return Config{}, p.errs
	}
	return c, nil
}

// Demo mode (PLAN 19.1): visitors get ephemeral demo users (POST /auth/demo), so the instance
// takes no sign-ups, smaller uploads, and no assistant unless FINUANCE_DEMO_ASSISTANT asks for it.
const (
	DemoMaxUploadBytes    = 2 << 20
	DemoAssistantMessages = 10 // per demo user over its lifetime
	DemoMaxRows           = 25 // rows one assistant lookup may return
)

func (c *Config) demoLimits(p *parser) {
	if v, ok := p.raw("FINUANCE_ALLOW_SIGNUP"); ok {
		if b, err := strconv.ParseBool(v); err == nil && b {
			p.fail("FINUANCE_ALLOW_SIGNUP", "must not be true when FINUANCE_DEMO_MODE is (visitors use the demo instead)")
		}
	}
	c.AllowSignup = false
	c.MaxUploadBytes = min(c.MaxUploadBytes, DemoMaxUploadBytes)
	if !c.DemoAssistant {
		c.AIConnections = nil
	}
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
