package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "connections.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAIShorthand(t *testing.T) {
	c, err := Load(env(map[string]string{
		"FINUANCE_AI_BASE_URL":      "http://192.168.1.20:1234/v1",
		"FINUANCE_AI_DEFAULT_MODEL": "qwen3-14b",
		"FINUANCE_AI_API_KEY":       "lm-studio",
	}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	want := AIConnection{Name: "default", Label: "192.168.1.20", API: llm.OpenAI, APIKey: "lm-studio", DefaultModel: "qwen3-14b", ContextTokens: 8192}
	got := c.AIConnections[0]
	if got.BaseURL.String() != "http://192.168.1.20:1234/v1" {
		t.Errorf("base = %s", got.BaseURL)
	}
	got.BaseURL = nil
	if got.Name != want.Name || got.Label != want.Label || got.API != want.API || got.APIKey != want.APIKey ||
		got.DefaultModel != want.DefaultModel || got.ContextTokens != want.ContextTokens || got.Cloud {
		t.Errorf("connection = %+v", got)
	}
	// a public IP needs the cloud flag
	_, err = Load(env(map[string]string{"FINUANCE_AI_BASE_URL": "https://203.0.113.9/v1"}), ForHealthcheck)
	if !strings.Contains(err.Error(), "public address") {
		t.Errorf("public literal: %v", err)
	}
	c, err = Load(env(map[string]string{"FINUANCE_AI_BASE_URL": "https://api.anthropic.com", "FINUANCE_AI_API": "anthropic",
		"FINUANCE_AI_ALLOW_REMOTE": "true"}), ForHealthcheck)
	if err != nil || !c.AIConnections[0].Cloud || c.AIConnections[0].API != llm.Anthropic {
		t.Errorf("cloud shorthand: %+v %v", c.AIConnections, err)
	}
}

func TestAIConnectionsFile(t *testing.T) {
	f := writeFile(t, `{"connections": [
	  {"name": "lmstudio", "label": "LM Studio (homelab)", "api": "openai", "baseUrl": "http://lmstudio.lan:1234/v1",
	   "defaultModel": "qwen3-14b", "contextTokens": 16384, "caps": [{"pattern": "gemma-*", "tools": false}]},
	  {"name": "ollama", "api": "openai", "baseUrl": "http://127.0.0.1:11434/v1"},
	  {"name": "anthropic", "label": "Claude (cloud)", "api": "anthropic", "baseUrl": "https://api.anthropic.com",
	   "apiKeyEnv": "ANTHROPIC_KEY", "cloud": true, "models": ["claude-*"]}
	]}`)
	c, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": f, "ANTHROPIC_KEY": "sk-ant-x", "FINUANCE_AI_CONTEXT_TOKENS": "4096"}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	cs := c.AIConnections
	if len(cs) != 3 || cs[0].Label != "LM Studio (homelab)" || cs[0].ContextTokens != 16384 || len(cs[0].Caps) != 1 ||
		cs[1].Label != "ollama" || cs[1].ContextTokens != 4096 || cs[2].APIKey != "sk-ant-x" || !cs[2].Cloud ||
		cs[2].Models[0] != "claude-*" {
		t.Errorf("connections = %+v", cs)
	}
}

func TestAIConnectionsFileProblems(t *testing.T) {
	cases := []struct {
		name, body string
		extra      map[string]string
		want       []string
	}{
		{"not JSON", `{`, nil, []string{"not a valid connections file"}},
		{"unknown field", `{"connections":[{"name":"a","api":"openai","baseUrl":"http://x/v1","apiKey":"secret"}]}`, nil,
			[]string{`unknown field "apiKey"`}},
		{"empty", `{"connections":[]}`, nil, []string{"lists no connections"}},
		{"every problem at once", `{"connections":[
			{"name":"Bad Name","api":"grpc","baseUrl":"http://10.0.0.2/v1"},
			{"name":"b","api":"openai","baseUrl":"ftp://x"},
			{"name":"c","api":"openai","baseUrl":"http://8.8.8.8/v1"},
			{"name":"d","api":"openai","baseUrl":"http://api.example.com/v1","cloud":true},
			{"name":"f","api":"openai","baseUrl":"https://192.168.1.20/v1","cloud":true},
			{"name":"g","api":"openai","baseUrl":"https://169.254.169.254/v1","cloud":true},
			{"name":"e","api":"openai","baseUrl":"http://10.0.0.3/v1","apiKeyEnv":"MISSING_KEY","models":["["],
			 "caps":[{"pattern":"x","reasoning":"lots"}],"contextTokens":-1},
			{"name":"e","api":"anthropic","baseUrl":"http://10.0.0.4"}]}`, nil,
			[]string{`"Bad Name": name must be`, "api must be openai or anthropic", `"b": baseUrl must be`,
				"8.8.8.8 is a public address", "must use https", "192.168.1.20 is not a public address", "169.254.169.254 is not a public address", "MISSING_KEY, which is not set", `"[" is not a valid pattern`,
				"reasoning must be none", "contextTokens must be", "name is used twice"}},
		{"both ways", `{"connections":[{"name":"a","api":"openai","baseUrl":"http://x/v1"}]}`,
			map[string]string{"FINUANCE_AI_BASE_URL": "http://localhost:1234/v1"}, []string{"not both"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": writeFile(t, tc.body)}
			for k, v := range tc.extra {
				kv[k] = v
			}
			_, err := Load(env(kv), ForHealthcheck)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("missing %q in:\n%v", w, err)
				}
			}
			if strings.Contains(err.Error(), "secret") {
				t.Error("the error echoes a secret")
			}
		})
	}
	if _, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": "/nonexistent/c.json"}), ForHealthcheck); err == nil ||
		!strings.Contains(err.Error(), "can't read") {
		t.Errorf("missing file: %v", err)
	}
	big := writeFile(t, `{"connections":[`+strings.Repeat(" ", maxConnectionsFile)+`]}`)
	if _, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": big}), ForHealthcheck); err == nil ||
		!strings.Contains(err.Error(), "larger than") {
		t.Errorf("big file: %v", err)
	}
}

// The example file in deploy/ stays valid.
func TestAIConnectionsExample(t *testing.T) {
	c, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": "../../../deploy/assistant/connections.example.json",
		"OPENAI_API_KEY": "sk-x", "ANTHROPIC_API_KEY": "sk-ant-x"}), ForHealthcheck)
	if err != nil || len(c.AIConnections) != 4 {
		t.Fatalf("example: %v", err)
	}
}

// TestAIConnectionsFileGoogle (PLAN 26.2): a connection of the file with `"auth": "google"` has a
// project and a chosen location, no key, and Vertex AI's address for them.
func TestAIConnectionsFileGoogle(t *testing.T) {
	f := writeFile(t, `{"connections": [
	  {"name": "vertex", "label": "Gemini", "api": "openai", "auth": "google", "project": "demo-project",
	   "location": "europe-west4", "cloud": true, "models": ["google/gemini-3.5-flash-lite"]},
	  {"name": "local", "api": "openai", "baseUrl": "http://127.0.0.1:11434/v1"}
	]}`)
	c, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": f}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	v, local := c.AIConnections[0], c.AIConnections[1]
	want := "https://europe-west4-aiplatform.googleapis.com/v1/projects/demo-project/locations/europe-west4/endpoints/openapi"
	if !v.Google() || v.BaseURL.String() != want || v.APIKey != "" || !v.Cloud || v.Project != "demo-project" || v.Location != "europe-west4" {
		t.Errorf("the Google connection = %+v", v)
	}
	if local.Google() || local.Auth != AuthKey {
		t.Errorf("the local connection = %+v", local)
	}

	for name, entry := range map[string]string{
		"no location":         `{"name": "v", "api": "openai", "auth": "google", "project": "demo-project", "cloud": true}`,
		"not cloud":           `{"name": "v", "api": "openai", "auth": "google", "project": "demo-project", "location": "eu"}`,
		"a key":               `{"name": "v", "api": "openai", "auth": "google", "project": "demo-project", "location": "eu", "cloud": true, "apiKeyEnv": "K"}`,
		"another host":        `{"name": "v", "api": "openai", "auth": "google", "project": "demo-project", "location": "eu", "cloud": true, "baseUrl": "https://example.test/v1"}`,
		"an unknown auth":     `{"name": "v", "api": "openai", "auth": "azure", "baseUrl": "https://example.test/v1", "cloud": true}`,
		"a project for a key": `{"name": "v", "api": "openai", "baseUrl": "http://127.0.0.1:1/v1", "project": "demo-project"}`,
	} {
		bad := writeFile(t, `{"connections": [`+entry+`]}`)
		if _, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": bad, "K": "sk"}), ForHealthcheck); err == nil {
			t.Errorf("%s: the file was accepted", name)
		}
	}
}

// TestVertexExampleFile: the example in deploy/assistant for a Google connection is a file the
// server accepts as it is.
func TestVertexExampleFile(t *testing.T) {
	c, err := Load(env(map[string]string{"FINUANCE_AI_CONNECTIONS_FILE": "../../../deploy/assistant/connections.vertex.example.json"}), ForHealthcheck)
	if err != nil {
		t.Fatal(err)
	}
	v := c.AIConnections[0]
	if !v.Google() || v.Location != "europe-west4" || v.DefaultModel != "google/gemini-3.5-flash-lite" || len(v.Caps) != 1 {
		t.Errorf("connection = %+v", v)
	}
}
