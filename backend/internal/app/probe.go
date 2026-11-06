package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// ProbeInput selects what `finuance admin llm-probe` checks.
type ProbeInput struct {
	Connection string // "" = the first
	Model      string // "" = the connection's default
	OutDir     string // where the raw exchanges are written ("" = none)
}

// Probe lists every connection's models, then sends llm-probe's fixed requests (made-up data only)
// to one model. With OutDir, each request body and the raw response are written to a file, ready
// to become a recorded stream in internal/llm/testdata. Headers (API keys) are never written.
func Probe(ctx context.Context, cfg config.Config, in ProbeInput, out func(format string, args ...any)) error {
	if len(cfg.AIConnections) == 0 {
		return fmt.Errorf("no assistant connection: set FINUANCE_AI_BASE_URL or FINUANCE_AI_CONNECTIONS_FILE")
	}
	rec := &recorder{dir: in.OutDir}
	if in.OutDir != "" {
		if err := os.MkdirAll(in.OutDir, 0o750); err != nil {
			return err
		}
	}
	conns := assistant.NewConnections(cfg.AIConnections, cfg.AITimeout, cfg.AIIdleTimeout, platform.SystemClock{}, func(c llm.Config) llm.Client {
		c.HTTPClient = &http.Client{Transport: rec.wrap(llm.Transport(c.AllowRemote))}
		return llm.New(c)
	})
	warnings, err := conns.CheckHosts(ctx, nil)
	for _, w := range warnings {
		out("warning: %s", w)
	}
	if err != nil {
		return err
	}
	for _, st := range conns.Status(ctx, true) {
		kind := "local"
		if st.Cloud {
			kind = "cloud"
		}
		out("%s (%s, %s, %s)", st.Name, st.Label, st.API, kind)
		if st.Err != nil {
			out("  unreachable: %s: %s", st.Err.Kind, st.Err.Message)
			continue
		}
		for _, m := range st.Models {
			def := ""
			if m.ID == st.DefaultModel {
				def = "  [default]"
			}
			out("  %-40s tools=%-5v reasoning=%-6s vision=%-5v context=%d%s", m.ID, m.Caps.Tools, m.Caps.Reasoning, m.Caps.Vision, m.Caps.ContextTokens, def)
		}
	}
	c, m, err := conns.Resolve(ctx, in.Connection, in.Model)
	if err != nil {
		return err
	}
	out("\nprobing %s / %s", c.Name, m.ID)
	failed := 0
	assistant.Probe(ctx, c, m, assistant.UserTime(time.Now(), assistant.DefaultTimeZone), func(r assistant.ProbeResult) {
		rec.label(r.Name)
		out("  %s", r.Describe())
		if r.Err != nil {
			failed++
		}
	})
	if in.OutDir != "" {
		out("\nraw exchanges written to %s", in.OutDir)
	}
	if failed > 0 {
		return fmt.Errorf("%d probe request(s) failed", failed)
	}
	return nil
}

// recorder writes every exchange of the probe to OutDir.
type recorder struct {
	dir  string
	mu   sync.Mutex
	n    int
	name string // the label of the next chat request
}

func (r *recorder) label(name string) {
	r.mu.Lock()
	r.name = name
	r.mu.Unlock()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func (r *recorder) wrap(next http.RoundTripper) http.RoundTripper {
	if r.dir == "" {
		return next
	}
	return roundTripper(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		res, err := next.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.n++
		name := fmt.Sprintf("%02d-%s.txt", r.n, unsafeName.ReplaceAllString(req.URL.Path, "_"))
		r.mu.Unlock()
		f, ferr := os.OpenFile(filepath.Join(r.dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // the admin's -out directory, our own file name
		if ferr != nil {
			return res, nil //nolint:nilerr // recording is best effort
		}
		fmt.Fprintf(f, "%s %s\n\n%s\n\n--- response %d %s\n\n", req.Method, req.URL.Path, body, res.StatusCode, res.Header.Get("Content-Type"))
		res.Body = &teeBody{ReadCloser: res.Body, w: f}
		return res, nil
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// teeBody copies what the client reads into the file and closes it with the body.
type teeBody struct {
	io.ReadCloser
	w *os.File
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 {
		_, _ = t.w.Write(p[:n])
	}
	return n, err
}

func (t *teeBody) Close() error {
	_ = t.w.Close()
	return t.ReadCloser.Close()
}
