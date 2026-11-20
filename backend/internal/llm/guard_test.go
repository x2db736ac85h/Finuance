package llm

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestPublicAddr: what a cloud connection may dial (PLAN 23.5).
func TestPublicAddr(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700::1111": true, "100.128.0.1": true,
		"127.0.0.1": false, "127.8.8.8": false, "::1": false, "0.0.0.0": false, "::": false,
		"10.1.2.3": false, "172.20.0.5": false, "192.168.1.20": false, "fd7a:115c:a1e0::1": false,
		"169.254.169.254": false, "fe80::1": false, "100.101.102.103": false,
		"::ffff:10.0.0.1": false, "::ffff:169.254.169.254": false, "::ffff:8.8.8.8": true,
		"64:ff9b::a00:1": false, "2002:a00:1::1": false, "2001:db8::1": false,
		"224.0.0.1": false, "ff02::1": false, "255.255.255.255": false, "240.0.0.1": false, "198.18.0.1": false,
	} {
		if got := PublicAddr(netip.MustParseAddr(ip)); got != want {
			t.Errorf("PublicAddr(%s) = %v, want %v", ip, got, want)
		}
		if a := netip.MustParseAddr(ip); PublicAddr(a) && PrivateAddr(a) {
			t.Errorf("%s is both public and private", ip)
		}
	}
}

// dialTo returns a cloud transport whose names all resolve to addr (a resolver the test controls).
func cloudClientResolvingTo(t *testing.T, addr string) *http.Client {
	t.Helper()
	tr := transport(true)
	guard := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return guard(ctx, network, addr) // as if DNS had answered with addr
	}
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // a test server's certificate
	return &http.Client{Transport: httpsOnly{tr}, Timeout: 5 * time.Second}
}

// TestCloudTransportGuard: a cloud connection can't be pointed at the instance's own network. Each
// refusal happens at dial time, on the address the name resolved to, so a name that resolves to a
// private address is refused like the literal (AI-3, AI-7).
func TestCloudTransportGuard(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	for name, addr := range map[string]string{
		"loopback": srv.Listener.Addr().String(), "private": "10.0.0.1:443", "link-local (metadata)": "169.254.169.254:80",
		"tailnet": "100.101.102.103:443", "IPv6 loopback": "[::1]:443", "unique local": "[fd00::1]:443", "mapped private": "[::ffff:192.168.1.1]:443",
		"unspecified": "0.0.0.0:" + strconv.Itoa(port),
	} {
		c := New(Config{API: OpenAI, BaseURL: "https://models.example.com/v1", AllowRemote: true, Timeout: 3 * time.Second,
			HTTPClient: cloudClientResolvingTo(t, addr)})
		_, err := c.Models(context.Background())
		if KindOf(err) != KindUnreachable || !strings.Contains(err.Error(), "not a public address") {
			t.Errorf("%s (%s): %v", name, addr, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests reached the server on loopback", hits.Load())
	}

	// the default client of a cloud connection: a private literal in the URL, and plain http
	c := New(Config{API: OpenAI, BaseURL: "https://192.168.1.20/v1", AllowRemote: true, Timeout: 2 * time.Second})
	if _, err := c.Models(context.Background()); KindOf(err) != KindUnreachable || !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("private literal: %v", err)
	}
	c = New(Config{API: OpenAI, BaseURL: "http://models.example.com/v1", AllowRemote: true, Timeout: 2 * time.Second})
	if _, err := c.Models(context.Background()); err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Errorf("plain http: %v", err)
	}
	// and a local connection still reaches loopback, but no longer a link-local address
	local := New(Config{API: OpenAI, BaseURL: "http://169.254.169.254/v1", Timeout: 2 * time.Second})
	if _, err := local.Models(context.Background()); KindOf(err) != KindUnreachable || !strings.Contains(err.Error(), "not a private address") {
		t.Errorf("link-local for a local connection: %v", err)
	}
}

// TestNoRedirectToAnotherHost: a model server's redirect to another host, port or scheme is not
// followed, so neither the API key nor the prompt goes there; a redirect within the server is.
func TestNoRedirectToAnotherHost(t *testing.T) {
	var got atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Api-Key"))
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer other.Close()
	var target atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/moved/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		http.Redirect(w, r, target.Load().(string), http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	for _, api := range []API{OpenAI, Anthropic} {
		target.Store(other.URL + "/v1/models")
		c := New(Config{API: api, BaseURL: srv.URL + map[API]string{OpenAI: "/v1", Anthropic: ""}[api], APIKey: "secret-key", Timeout: 3 * time.Second})
		_, err := c.Models(context.Background())
		var le *Error
		if !errors.As(err, &le) || le.Kind != KindBadResponse || !strings.Contains(le.Message, "redirected to another address") {
			t.Errorf("%s: redirect to another host: %v", api, err)
		}
		if v := got.Load(); v != nil {
			t.Errorf("%s: the other host was called with %q", api, v)
		}
	}
	target.Store("/v1/moved/models")
	c := New(Config{API: OpenAI, BaseURL: srv.URL + "/v1", APIKey: "secret-key", Timeout: 3 * time.Second})
	if ms, err := c.Models(context.Background()); err != nil || len(ms) != 1 {
		t.Errorf("redirect within the server: %v %v", ms, err)
	}
	// a client supplied by the caller gets the policy too, and stays unchanged itself
	mine := &http.Client{}
	_ = New(Config{API: OpenAI, BaseURL: srv.URL, HTTPClient: mine})
	if mine.CheckRedirect != nil {
		t.Error("New changed the caller's client")
	}
}

// TestTLSMinimum: the transport refuses a server that offers only TLS 1.1.
func TestTLSMinimum(t *testing.T) {
	if v := transport(true).TLSClientConfig.MinVersion; v != tls.VersionTLS12 {
		t.Errorf("cloud MinVersion = %x", v)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS11, MinVersion: tls.VersionTLS10} //nolint:gosec // the old server under test
	srv.StartTLS()
	defer srv.Close()
	tr := transport(false)
	tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // only the protocol version is under test
	_, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "protocol version") {
		t.Errorf("TLS 1.1 server: %v", err)
	}
}

// TestPublicErrorTextCarriesNothingOfTheServer: what a user who is not an administrator is told
// about a model server's failure names no address, no key and none of the server's words (AI-4).
func TestPublicErrorTextCarriesNothingOfTheServer(t *testing.T) {
	leaky := []*Error{
		statusError(401, []byte(`{"error":{"message":"Incorrect API key provided: sk-proj-abcd****wxyz"}}`)),
		statusError(500, []byte(`internal error at http://10.0.0.5:11434/api/chat: model runner crashed`)),
		statusError(429, []byte(`{"error":"quota exceeded for org-12345"}`)),
		statusError(404, []byte(`{"error":{"message":"model 'secret-internal-model' not found"}}`)),
		{Kind: KindUnreachable, Message: "dial tcp 192.168.1.20:1234: connect: connection refused"},
		{Kind: KindUnreachable, Message: "address not allowed: 169.254.169.254 is not a private address"},
		{Kind: KindBadResponse, Message: `x509: certificate is valid for gpu-box.internal, not models.example.com`},
		{Kind: KindTimeout, Message: "the model didn't answer in time"},
		{Kind: KindContextTooLong, Message: "prompt is too long: 9000 tokens > n_ctx 8192"},
		{Kind: KindToolsRejected, Message: "registry.internal/m does not support tools"},
	}
	seen := map[Kind]string{}
	for _, e := range leaky {
		pub := e.Public()
		for _, secret := range []string{"sk-proj", "10.0.0.5", "192.168", "169.254", "org-12345", "secret-internal", "gpu-box", "registry.internal", "http://", "n_ctx"} {
			if strings.Contains(pub, secret) {
				t.Errorf("%s: the public text %q carries %q", e.Kind, pub, secret)
			}
		}
		if pub == "" || pub == e.Message && e.Kind != KindTimeout {
			t.Errorf("%s: public text %q", e.Kind, pub)
		}
		if prev, ok := seen[e.Kind]; ok && prev != pub {
			t.Errorf("%s has two public texts", e.Kind)
		}
		seen[e.Kind] = pub
	}
}

// TestAnswerSizeIsBounded: a model server that never stops sending is cut off at MaxResponseBytes
// instead of being read for as long as the timeout lasts (AI-6); for both wire formats, streamed.
func TestAnswerSizeIsBounded(t *testing.T) {
	was := MaxResponseBytes
	MaxResponseBytes = 64 << 10
	t.Cleanup(func() { MaxResponseBytes = was })
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `data: {"choices":[{"delta":{"content":"` + strings.Repeat("x", 1000) + `"}}]}` + "\n\n"
		if strings.HasSuffix(r.URL.Path, "/messages") {
			chunk = "event: content_block_delta\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + strings.Repeat("x", 1000) + `"}}` + "\n\n"
		}
		for i := 0; i < 5000; i++ { // about 5 MB if nobody stops reading
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
			sent.Add(int64(len(chunk)))
		}
	}))
	defer srv.Close()
	for _, api := range []API{OpenAI, Anthropic} {
		sent.Store(0)
		c := New(Config{API: api, BaseURL: srv.URL + map[API]string{OpenAI: "/v1", Anthropic: ""}[api], Timeout: 10 * time.Second})
		var got int
		_, err := c.Chat(context.Background(), Request{Model: "m"}, func(d Delta) { got += len(d.Content) })
		if KindOf(err) != KindBadResponse || !strings.Contains(err.Error(), "too long") {
			t.Errorf("%s: an endless answer: %v", api, err)
		}
		// what arrived before the cut was passed on (the Anthropic reader wants a block start first,
		// which this endless stream never sends), and never more than the limit
		if (api == OpenAI && got == 0) || got > int(MaxResponseBytes) {
			t.Errorf("%s: %d bytes of content were passed on, want at most %d", api, got, MaxResponseBytes)
		}
		if n := sent.Load(); n > 4*MaxResponseBytes+(1<<20) {
			t.Errorf("%s: the server got to send %d bytes before the connection ended", api, n)
		}
	}
}
