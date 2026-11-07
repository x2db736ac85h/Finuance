package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// AIConnection is one model server the assistant may use (ADR 0015). Connections are instance
// config: users pick a connection by name and a model, never a URL.
type AIConnection struct {
	Name  string // stable id stored with chats and settings
	Label string // shown to users
	API   llm.API
	// BaseURL is the API root: …/v1 for OpenAI-compatible servers, the host for Anthropic.
	BaseURL *url.URL
	APIKey  string
	// Cloud lets the connection reach public addresses. Users must agree once before their data
	// is sent to it, and long digit runs in tool results are masked.
	Cloud        bool
	DefaultModel string
	// Models limits the models offered (path.Match globs on the id); empty offers every chat model.
	Models []string
	// ContextTokens is the context size assumed when the server doesn't report one.
	ContextTokens int
	Caps          []llm.CapsOverride
}

// maxConnectionsFile bounds the connections file; it holds a handful of entries.
const maxConnectionsFile = 256 << 10

var connName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// connectionsFile is the JSON layout of FINUANCE_AI_CONNECTIONS_FILE (docs/assistant.md).
type connectionsFile struct {
	Connections []struct {
		Name          string             `json:"name"`
		Label         string             `json:"label"`
		API           llm.API            `json:"api"`
		BaseURL       string             `json:"baseUrl"`
		APIKeyEnv     string             `json:"apiKeyEnv"`
		Cloud         bool               `json:"cloud"`
		DefaultModel  string             `json:"defaultModel"`
		Models        []string           `json:"models"`
		ContextTokens int                `json:"contextTokens"`
		Caps          []llm.CapsOverride `json:"caps"`
	} `json:"connections"`
}

// aiConnections reads the connections file, or builds the single connection of the
// FINUANCE_AI_BASE_URL shorthand. No connection means the assistant is off.
func (p *parser) aiConnections(defaultContext int) []AIConnection {
	file, hasFile := p.raw("FINUANCE_AI_CONNECTIONS_FILE")
	_, hasBase := p.raw("FINUANCE_AI_BASE_URL")
	const fileVar = "FINUANCE_AI_CONNECTIONS_FILE"
	switch {
	case hasFile && hasBase:
		p.fail(fileVar, "set either FINUANCE_AI_CONNECTIONS_FILE or FINUANCE_AI_BASE_URL, not both")
		return nil
	case hasBase:
		u := p.httpURL("FINUANCE_AI_BASE_URL")
		api := llm.API(p.str("FINUANCE_AI_API", string(llm.OpenAI)))
		if api != llm.OpenAI && api != llm.Anthropic {
			p.fail("FINUANCE_AI_API", fmt.Sprintf("must be openai or anthropic, got %q", api))
		}
		cloud := p.bool("FINUANCE_AI_ALLOW_REMOTE", false)
		if u == nil {
			return nil
		}
		c := AIConnection{Name: "default", Label: u.Hostname(), API: api, BaseURL: u, APIKey: p.str("FINUANCE_AI_API_KEY", ""),
			Cloud: cloud, DefaultModel: p.str("FINUANCE_AI_DEFAULT_MODEL", ""), ContextTokens: defaultContext}
		if msg := checkAddress(u, cloud); msg != "" {
			p.fail("FINUANCE_AI_BASE_URL", msg+" (set FINUANCE_AI_ALLOW_REMOTE=true for a cloud service)")
			return nil
		}
		return []AIConnection{c}
	case !hasFile:
		return nil
	}
	b, err := readLimited(file)
	if err != nil {
		p.fail(fileVar, err.Error())
		return nil
	}
	var f connectionsFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		p.fail(fileVar, fmt.Sprintf("%s is not a valid connections file: %v", file, err))
		return nil
	}
	if len(f.Connections) == 0 {
		p.fail(fileVar, file+" lists no connections")
		return nil
	}
	var out []AIConnection
	var problems []string
	seen := map[string]bool{}
	for i := range f.Connections {
		fc := &f.Connections[i]
		where := fmt.Sprintf("connection %d", i+1)
		if fc.Name != "" {
			where = fmt.Sprintf("connection %q", fc.Name)
		}
		bad := func(msg string) { problems = append(problems, where+": "+msg) }
		if !connName.MatchString(fc.Name) {
			bad("name must be 1–40 characters of a-z, 0-9, _ and -, starting with a letter or digit")
		} else if seen[fc.Name] {
			bad("name is used twice")
		}
		seen[fc.Name] = true
		if fc.API != llm.OpenAI && fc.API != llm.Anthropic {
			bad(fmt.Sprintf("api must be openai or anthropic, got %q", fc.API))
		}
		u, err := url.Parse(fc.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			bad(fmt.Sprintf("baseUrl must be an absolute http(s) URL without a query, got %q", fc.BaseURL))
			continue
		}
		if msg := checkAddress(u, fc.Cloud); msg != "" {
			bad(msg + ` (add "cloud": true for a cloud service)`)
		}
		if fc.Cloud && u.Scheme != "https" {
			bad("a cloud connection must use https")
		}
		key := ""
		if fc.APIKeyEnv != "" {
			// the key itself never sits in the file, so the file can be committed to a GitOps repo
			if key, _ = p.raw(fc.APIKeyEnv); key == "" {
				bad(fmt.Sprintf("apiKeyEnv names %s, which is not set", fc.APIKeyEnv))
			}
		}
		for _, m := range fc.Models {
			if _, err := path.Match(m, ""); err != nil || m == "" {
				bad(fmt.Sprintf("models: %q is not a valid pattern", m))
			}
		}
		for _, o := range fc.Caps {
			if _, err := path.Match(o.Pattern, ""); err != nil || o.Pattern == "" {
				bad(fmt.Sprintf("caps: %q is not a valid pattern", o.Pattern))
			}
			if o.Reasoning != nil {
				switch *o.Reasoning {
				case llm.ReasonNone, llm.ReasonEffort, llm.ReasonBudget, llm.ReasonSwitch:
				default:
					bad(fmt.Sprintf("caps: reasoning must be none, effort, budget or switch, got %q", *o.Reasoning))
				}
			}
		}
		if fc.ContextTokens < 0 || fc.ContextTokens > 10_000_000 {
			bad("contextTokens must be between 0 and 10000000")
		}
		label := strings.TrimSpace(fc.Label)
		if label == "" {
			label = fc.Name
		}
		ctx := fc.ContextTokens
		if ctx == 0 {
			ctx = defaultContext
		}
		out = append(out, AIConnection{Name: fc.Name, Label: label, API: fc.API, BaseURL: u, APIKey: key, Cloud: fc.Cloud,
			DefaultModel: fc.DefaultModel, Models: fc.Models, ContextTokens: ctx, Caps: fc.Caps})
	}
	if len(problems) > 0 {
		p.fail(fileVar, strings.Join(problems, "; "))
		return nil
	}
	return out
}

// checkAddress refuses a local connection whose host is a public IP literal. Host names are
// resolved and checked at startup (assistant.CheckConnections) and on every dial (llm).
func checkAddress(u *url.URL, cloud bool) string {
	if cloud {
		return ""
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err == nil && !llm.PrivateAddr(ip) {
		return fmt.Sprintf("%s is a public address", u.Hostname())
	}
	return ""
}

func readLimited(name string) ([]byte, error) {
	return readLimitedFile(name, maxConnectionsFile, "connections file")
}

// readLimitedFile reads a small config file, refusing one larger than limit bytes.
func readLimitedFile(name string, limit int, what string) ([]byte, error) {
	f, err := os.Open(name) //nolint:gosec // the admin's own config path
	if err != nil {
		return nil, fmt.Errorf("can't read the %s: %w", what, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("can't read the %s: %w", what, err)
	}
	if len(b) > limit {
		return nil, fmt.Errorf("the %s is larger than %d KiB", what, limit>>10)
	}
	return b, nil
}

// ConnectionSpec is a connection an administrator enters in the panel (ADR 0035).
type ConnectionSpec struct {
	Name, Label, API, BaseURL, DefaultModel, APIKey string
	Cloud                                           bool
	Models                                          []string
	ContextTokens                                   int
}

// NewConnection checks a connection entered in the panel by the connections file's rules (a
// local connection on a public address is refused; a cloud one must use https). The problems map
// field names (name, baseUrl, …) to messages; empty when it is fine.
func NewConnection(s ConnectionSpec, defaultContext int) (AIConnection, map[string]string) {
	problems := map[string]string{}
	if !connName.MatchString(s.Name) {
		problems["name"] = "must be 1–40 characters of a-z, 0-9, _ and -, starting with a letter or digit"
	}
	api := llm.API(s.API)
	if api != llm.OpenAI && api != llm.Anthropic {
		problems["api"] = "must be openai or anthropic"
	}
	label := strings.TrimSpace(s.Label)
	if label == "" {
		label = s.Name
	}
	if len([]rune(label)) > 80 {
		problems["label"] = "must be at most 80 characters"
	}
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil ||
		len(s.BaseURL) > 500:
		problems["baseUrl"] = "must be an absolute http(s) URL without a query or credentials"
	case checkAddress(u, s.Cloud) != "":
		problems["baseUrl"] = checkAddress(u, s.Cloud) + " (mark the connection as a cloud service if that is intended)"
	case s.Cloud && u.Scheme != "https":
		problems["baseUrl"] = "a cloud connection must use https"
	}
	if len(s.Models) > 50 {
		problems["models"] = "at most 50 patterns"
	}
	for _, m := range s.Models {
		if _, err := path.Match(m, ""); err != nil || m == "" || len(m) > 200 {
			problems["models"] = fmt.Sprintf("%q is not a valid pattern", m)
		}
	}
	if s.ContextTokens < 0 || s.ContextTokens > 10_000_000 {
		problems["contextTokens"] = "must be between 0 and 10000000"
	}
	if len(s.DefaultModel) > 200 {
		problems["defaultModel"] = "must be at most 200 characters"
	}
	if len(s.APIKey) > 1000 || strings.ContainsAny(s.APIKey, "\r\n\x00") {
		problems["apiKey"] = "must be one line of at most 1000 characters"
	}
	ctx := s.ContextTokens
	if ctx == 0 {
		ctx = defaultContext
	}
	if len(problems) > 0 {
		return AIConnection{}, problems
	}
	return AIConnection{Name: s.Name, Label: label, API: api, BaseURL: u, APIKey: s.APIKey, Cloud: s.Cloud, DefaultModel: s.DefaultModel,
		Models: s.Models, ContextTokens: ctx}, nil
}
