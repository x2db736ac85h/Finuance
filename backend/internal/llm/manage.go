package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model servers that load models on demand (Phase 20 follow-ups, ADR 0035 addendum). Ollama keeps
// a model in memory for a while after the last request (its keep_alive) and loads or unloads one
// when asked; LM Studio says which models are loaded and takes an idle time with each request.
// Other OpenAI-compatible servers and cloud services do neither. The server is learned from its
// model list (Models), so a client that hasn't read one yet counts as ServerOther.

// Server is the kind of model server behind an OpenAI-compatible connection.
type Server string

// The server kinds told apart.
const (
	ServerOllama   Server = "ollama"
	ServerLMStudio Server = "lmstudio"
	ServerOther    Server = "other"
)

// LoadedModel is a model the server holds in memory.
type LoadedModel struct {
	ID        string
	SizeBytes int64
	VRAMBytes int64
	Until     time.Time // when the server unloads it if nothing uses it; zero: it doesn't say or never
}

// ErrCannotManage is returned for loading and unloading on a server that can't do it from here.
var ErrCannotManage = errors.New("this server doesn't load or unload models from here")

// Manager is what a client offers for a server that loads models on demand.
type Manager interface {
	// Server is the server's kind as its last model list showed it.
	Server() Server
	// Loaded lists the models in memory (Ollama, LM Studio).
	Loaded(ctx context.Context) ([]LoadedModel, error)
	// Load loads a model, or only moves its unload time when it is loaded already, keeping it for
	// keep after the last request (0: the server's own setting). Ollama only.
	Load(ctx context.Context, model string, keep time.Duration) error
	// Unload frees a model's memory now. Ollama only.
	Unload(ctx context.Context, model string) error
}

var _ Manager = (*openAIClient)(nil)

// Server is what the last model list showed.
func (o *openAIClient) Server() Server {
	if s := o.server.Load(); s != nil {
		return *s
	}
	return ServerOther
}

func (o *openAIClient) setServer(s Server) { o.server.Store(&s) }

// root is the server's address without the /v1 of its OpenAI-compatible API; "" when the base URL
// doesn't end in /v1 (the native APIs then can't be found).
func (o *openAIClient) root() string {
	r := strings.TrimSuffix(o.c.BaseURL, "/v1")
	if r == o.c.BaseURL {
		return ""
	}
	return r
}

// ollamaVersion tells Ollama apart when it offers no model to ask /api/show about.
func (o *openAIClient) ollamaVersion(ctx context.Context) bool {
	root := o.root()
	if root == "" {
		return false
	}
	res, done, err := o.c.call(ctx, http.MethodGet, root+"/api/version", o.header(), nil)
	if err != nil {
		return false
	}
	defer done()
	defer func() { _ = res.Body.Close() }()
	var j struct {
		Version string `json:"version"`
	}
	return res.StatusCode/100 == 2 && json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&j) == nil && j.Version != ""
}

// neverAfter: Ollama reports a model kept forever (keep_alive -1) with an unload time centuries
// away; anything this far off counts as never.
const neverAfter = 10 * 365 * 24 * time.Hour

// Loaded lists the models in memory: Ollama's /api/ps, LM Studio's /api/v0/models (state loaded).
func (o *openAIClient) Loaded(ctx context.Context) ([]LoadedModel, error) {
	root := o.root()
	switch o.Server() {
	case ServerOllama:
		var j struct {
			Models []struct {
				Name      string    `json:"name"`
				Model     string    `json:"model"`
				Size      int64     `json:"size"`
				SizeVRAM  int64     `json:"size_vram"`
				ExpiresAt time.Time `json:"expires_at"`
			} `json:"models"`
		}
		if err := o.native(ctx, http.MethodGet, root+"/api/ps", nil, &j); err != nil {
			return nil, err
		}
		out := make([]LoadedModel, 0, len(j.Models))
		for _, m := range j.Models {
			id := m.Model
			if id == "" {
				id = m.Name
			}
			lm := LoadedModel{ID: id, SizeBytes: m.Size, VRAMBytes: m.SizeVRAM, Until: m.ExpiresAt}
			if !lm.Until.IsZero() && time.Until(lm.Until) > neverAfter {
				lm.Until = time.Time{}
			}
			out = append(out, lm)
		}
		return out, nil
	case ServerLMStudio:
		var j struct {
			Data []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"data"`
		}
		if err := o.native(ctx, http.MethodGet, root+"/api/v0/models", nil, &j); err != nil {
			return nil, err
		}
		var out []LoadedModel
		for _, m := range j.Data {
			if m.State == "loaded" {
				out = append(out, LoadedModel{ID: m.ID})
			}
		}
		return out, nil
	default:
		return nil, ErrCannotManage
	}
}

// Load asks Ollama to load the model (a generate request without a prompt). With keep it stays
// that long after the last request; Ollama's own keep_alive setting otherwise.
func (o *openAIClient) Load(ctx context.Context, model string, keep time.Duration) error {
	if o.Server() != ServerOllama {
		return ErrCannotManage
	}
	body := map[string]any{"model": model, "stream": false}
	if keep > 0 {
		body["keep_alive"] = keep.String()
	}
	return o.generate(ctx, body)
}

// Unload asks Ollama to free the model now (keep_alive 0).
func (o *openAIClient) Unload(ctx context.Context, model string) error {
	if o.Server() != ServerOllama {
		return ErrCannotManage
	}
	return o.generate(ctx, map[string]any{"model": model, "keep_alive": 0, "stream": false})
}

func (o *openAIClient) generate(ctx context.Context, body map[string]any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var j struct {
		Error string `json:"error"`
	}
	return o.native(ctx, http.MethodPost, o.root()+"/api/generate", b, &j)
}

// native calls one of the server's own endpoints and decodes its JSON answer into out.
func (o *openAIClient) native(ctx context.Context, method, url string, body []byte, out any) error {
	res, done, err := o.c.call(ctx, method, url, o.header(), body)
	if err != nil {
		return err
	}
	defer done()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return readError(res)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
		return &Error{Kind: KindBadResponse, Status: res.StatusCode, Message: "the server's answer is not JSON"}
	}
	return nil
}
