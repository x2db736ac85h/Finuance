package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(kv map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{
		"FINUANCE_PUBLIC_ORIGIN": "http://localhost:8000",
		"FINUANCE_DATABASE_URL":  "postgres://finuance_app:x@localhost:5432/finuance",
	}), ForServe)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"Env", c.Env, EnvDev},
		{"HTTPAddr", c.HTTPAddr, ":8080"},
		{"AdminAddr", c.AdminAddr, ":9090"},
		{"PublicOrigin", c.PublicOrigin.String(), "http://localhost:8000"},
		{"DBMaxConns", c.DBMaxConns, int32(10)},
		{"SessionTTL", c.SessionTTL, 720 * time.Hour},
		{"SessionIdleTTL", c.SessionIdleTTL, 168 * time.Hour},
		{"CookieSecure", c.CookieSecure, true},
		{"AllowSignup", c.AllowSignup, true},
		{"DemoMode", c.DemoMode, false},
		{"MaxUploadBytes", c.MaxUploadBytes, int64(20971520)},
		{"MaxLegacyImportBytes", c.MaxLegacyImportBytes, int64(209715200)},
		{"LogLevel", c.LogLevel, slog.LevelInfo},
		{"AIConnections", len(c.AIConnections), 0},
		{"AITimeout", c.AITimeout, 120 * time.Second},
		{"AIIdleTimeout", c.AIIdleTimeout, 60 * time.Second},
		{"AIMaxConcurrent", c.AIMaxConcurrent, 2},
		{"AIMaxChats", c.AIMaxChats, 25},
		{"AIRatePerHour", c.AIRatePerHour, 30},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}
}

func TestRequirementsPerSubcommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Requirement
		want []string
	}{
		{"serve", ForServe, []string{"FINUANCE_PUBLIC_ORIGIN", "FINUANCE_DATABASE_URL"}},
		{"worker", ForWorker, []string{"FINUANCE_DATABASE_URL"}},
		{"migrate", ForMigrate, []string{"FINUANCE_MIGRATE_DATABASE_URL"}},
		{"healthcheck", ForHealthcheck, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(map[string]string{"FINUANCE_DATABASE_URL": "  "}), tc.req)
			got := vars(err)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("missing vars = %v, want %v", got, tc.want)
			}
		})
	}
}

func vars(err error) []string {
	var ve ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	for _, e := range ve {
		out = append(out, e.Var)
	}
	return out
}

func TestInvalidValuesAllReported(t *testing.T) {
	_, err := Load(env(map[string]string{
		"FINUANCE_ENV":               "staging",
		"FINUANCE_HTTP_ADDR":         "8080",
		"FINUANCE_ADMIN_ADDR":        ":99999",
		"FINUANCE_PUBLIC_ORIGIN":     "localhost:8000",
		"FINUANCE_DATABASE_URL":      "mysql://secret-password@db/x",
		"FINUANCE_DB_MAX_CONNS":      "0",
		"FINUANCE_SESSION_TTL":       "30 days",
		"FINUANCE_SESSION_IDLE_TTL":  "-1h",
		"FINUANCE_ALLOW_SIGNUP":      "maybe",
		"FINUANCE_LOG_LEVEL":         "loud",
		"FINUANCE_TRUSTED_PROXIES":   "10.0.0.0/8, nonsense",
		"FINUANCE_AI_BASE_URL":       "ftp://x",
		"FINUANCE_AI_TIMEOUT":        "soon",
		"FINUANCE_AI_IDLE_TIMEOUT":   "-5s",
		"FINUANCE_MAX_UPLOAD_BYTES":  "lots",
		"FINUANCE_AI_MAX_CONCURRENT": "1000",
		"FINUANCE_AI_MAX_CHATS":      "-3",
		"FINUANCE_DEMO_MODE":         "yes",
		"FINUANCE_AI_ALLOW_REMOTE":   "no",
		"FINUANCE_AI_API":            "grpc",
		"FINUANCE_AI_RATE_PER_HOUR":  "0",
	}), ForServe)
	want := []string{"FINUANCE_HTTP_ADDR", "FINUANCE_ADMIN_ADDR", "FINUANCE_PUBLIC_ORIGIN", "FINUANCE_DATABASE_URL",
		"FINUANCE_DB_MAX_CONNS", "FINUANCE_SESSION_TTL", "FINUANCE_SESSION_IDLE_TTL", "FINUANCE_ALLOW_SIGNUP", "FINUANCE_DEMO_MODE",
		"FINUANCE_MAX_UPLOAD_BYTES", "FINUANCE_LOG_LEVEL", "FINUANCE_TRUSTED_PROXIES", "FINUANCE_AI_BASE_URL", "FINUANCE_AI_ALLOW_REMOTE",
		"FINUANCE_AI_TIMEOUT", "FINUANCE_AI_IDLE_TIMEOUT", "FINUANCE_AI_MAX_CONCURRENT", "FINUANCE_AI_MAX_CHATS", "FINUANCE_ENV",
		"FINUANCE_AI_API", "FINUANCE_AI_RATE_PER_HOUR"}
	got := map[string]bool{}
	for _, v := range vars(err) {
		got[v] = true
	}
	for _, v := range want {
		if !got[v] {
			t.Errorf("%s not reported; error:\n%v", v, err)
		}
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Error("the error message leaks a DSN password")
	}
}

func TestCookieSecure(t *testing.T) {
	base := map[string]string{"FINUANCE_COOKIE_SECURE": "false"}
	for _, tc := range []struct {
		env     string
		wantErr bool
	}{{"dev", false}, {"test", false}, {"prod", true}} {
		base["FINUANCE_ENV"] = tc.env
		_, err := Load(env(base), ForHealthcheck)
		if (err != nil) != tc.wantErr {
			t.Errorf("env %s: err = %v", tc.env, err)
		}
	}
}

func TestParsing(t *testing.T) {
	c, err := Load(env(map[string]string{
		"FINUANCE_ENV":              "prod",
		"FINUANCE_PUBLIC_ORIGIN":    "https://finuance.example.ts.net/",
		"FINUANCE_TRUSTED_PROXIES":  "10.0.0.0/8, 192.168.1.7 ,fd00::/8",
		"FINUANCE_LOG_LEVEL":        "DEBUG",
		"FINUANCE_SESSION_TTL":      "24h",
		"FINUANCE_SESSION_IDLE_TTL": "1h30m",
		"FINUANCE_AI_BASE_URL":      "http://ollama.ollama.svc:11434/v1",
	}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicOrigin.String() != "https://finuance.example.ts.net" {
		t.Errorf("origin = %s", c.PublicOrigin)
	}
	if len(c.TrustedProxies) != 3 || c.TrustedProxies[1].String() != "192.168.1.7/32" {
		t.Errorf("proxies = %v", c.TrustedProxies)
	}
	if c.LogLevel != slog.LevelDebug || c.SessionIdleTTL != 90*time.Minute || len(c.AIConnections) != 1 ||
		c.AIConnections[0].BaseURL.Host != "ollama.ollama.svc:11434" {
		t.Errorf("parsed = %+v", c)
	}
	if _, err := Load(env(map[string]string{"FINUANCE_PUBLIC_ORIGIN": "https://x.example/app"}), ForHealthcheck); err == nil {
		t.Error("an origin with a path must be rejected")
	}
}

// TestDemoMode: a demo instance takes no sign-ups, uploads of at most 2 MiB and no assistant
// unless FINUANCE_DEMO_ASSISTANT turns it on; asking for sign-ups as well is a mistake.
func TestDemoMode(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		m := map[string]string{"FINUANCE_DEMO_MODE": "true", "FINUANCE_AI_BASE_URL": "http://localhost:1234/v1"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := Load(env(base(nil)), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	if c.AllowSignup || c.MaxUploadBytes != DemoMaxUploadBytes || len(c.AIConnections) != 0 || c.DemoAssistant {
		t.Errorf("demo defaults: signup %v, upload %d, connections %d, assistant %v", c.AllowSignup, c.MaxUploadBytes, len(c.AIConnections), c.DemoAssistant)
	}
	c, err = Load(env(base(map[string]string{"FINUANCE_DEMO_ASSISTANT": "true", "FINUANCE_MAX_UPLOAD_BYTES": "1048576"})), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AIConnections) != 1 || c.MaxUploadBytes != 1<<20 {
		t.Errorf("demo with assistant: connections %d, upload %d (a smaller limit stays)", len(c.AIConnections), c.MaxUploadBytes)
	}
	_, err = Load(env(base(map[string]string{"FINUANCE_ALLOW_SIGNUP": "1"})), ForHealthcheck)
	if v := vars(err); len(v) != 1 || v[0] != "FINUANCE_ALLOW_SIGNUP" {
		t.Errorf("demo with sign-up: %v", err)
	}
	c, err = Load(env(map[string]string{"FINUANCE_AI_BASE_URL": "http://localhost:1234/v1"}), ForHealthcheck)
	if err != nil || !c.AllowSignup || c.MaxUploadBytes != 20<<20 || len(c.AIConnections) != 1 {
		t.Errorf("not a demo: %+v, %v", c, err)
	}
}
