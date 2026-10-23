package llm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// Config is one connection to a model server.
type Config struct {
	API     API
	BaseURL string // e.g. http://localhost:1234/v1 (OpenAI) or https://api.anthropic.com (Anthropic)
	APIKey  string
	// AllowRemote lets the connection reach public addresses. Without it every dial is checked:
	// only loopback, private, link-local and CGNAT/tailnet addresses are allowed, so a DNS answer
	// that changes after startup can't send data elsewhere.
	AllowRemote bool
	// Timeout bounds one model call from request to last byte; IdleTimeout bounds the silence
	// between two chunks of a stream (and the wait for the first one).
	Timeout     time.Duration
	IdleTimeout time.Duration
	// HTTPClient replaces the default client (tests).
	HTTPClient *http.Client
}

var (
	errIdle           = errors.New("stream idle")
	errBlockedAddress = errors.New("address not allowed")
)

// New returns a client for the connection's wire format.
func New(c Config) Client {
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Transport: transport(c.AllowRemote)}
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.API == Anthropic {
		return &anthropicClient{c: c}
	}
	return &openAIClient{c: c}
}

// Transport is the HTTP transport New uses: no proxy, and unless allowRemote, dials to public
// addresses refused. Wrappers (llm-probe's recorder) build on it to keep the guard.
func Transport(allowRemote bool) *http.Transport { return transport(allowRemote) }

func transport(allowRemote bool) *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowRemote {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !PrivateAddr(ip) {
				return fmt.Errorf("%w: %s is not a private address", errBlockedAddress, host)
			}
			return nil
		}
	}
	return &http.Transport{
		Proxy:                 nil, // model traffic never goes through an outbound proxy
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0, // bounded by IdleTimeout instead (a model may think before answering)
	}
}

// PrivateAddr reports whether an address is loopback, private (RFC 1918, ULA), link-local or in
// the CGNAT range Tailscale uses (100.64.0.0/10).
func PrivateAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// CheckHost resolves a host and reports whether every address is private (PrivateAddr).
func CheckHost(ctx context.Context, r *net.Resolver, host string) (bool, error) {
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return PrivateAddr(ip), nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, a := range addrs {
		if !PrivateAddr(a) {
			return false, nil
		}
	}
	return len(addrs) > 0, nil
}

// call sends one request. The returned body is closed by the caller; reads from it fail with
// errIdle when the server stays silent for longer than IdleTimeout, and the whole call ends after
// Timeout.
func (c *Config) call(ctx context.Context, method, url string, header http.Header, body []byte) (*http.Response, func(), error) {
	ctx, cancelTotal := context.WithTimeout(ctx, c.Timeout)
	ctx, cancel := context.WithCancelCause(ctx)
	idle := time.AfterFunc(c.IdleTimeout, func() { cancel(errIdle) })
	done := func() {
		idle.Stop()
		cancel(nil)
		cancelTotal()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		done()
		return nil, nil, &Error{Kind: KindBadResponse, Message: "bad request URL"}
	}
	req.Header = header
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		cause := context.Cause(ctx)
		done()
		if errors.Is(cause, errIdle) {
			return nil, nil, &Error{Kind: KindTimeout, Message: "the model didn't start answering in time"}
		}
		return nil, nil, transportError(ctx, err)
	}
	idle.Reset(c.IdleTimeout)
	res.Body = &idleBody{ReadCloser: res.Body, reset: func() { idle.Reset(c.IdleTimeout) }, ctx: ctx}
	return res, done, nil
}

// idleBody restarts the idle timer on every read that returns data.
type idleBody struct {
	io.ReadCloser
	reset func()
	ctx   context.Context
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.reset()
	}
	if err != nil && err != io.EOF {
		if cause := context.Cause(b.ctx); errors.Is(cause, errIdle) {
			return n, &Error{Kind: KindTimeout, Message: "the model stopped answering"}
		}
	}
	return n, err
}

// readError reads an error body (at most 8 KiB) and classifies it.
func readError(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	return statusError(res.StatusCode, b)
}

// sseEvent is one server-sent data line with the name of its event ("" for none).
type sseEvent struct {
	Name string
	Data string
}

// readSSE calls fn for every data line of a text/event-stream body. Lines may end in \n or \r\n;
// comments (":") are skipped. Both APIs send one JSON document per data line, and some servers
// leave out the blank line between events, so every data line counts on its own (legacy complete()
// read the stream line by line too). An event: name applies until the next blank line or event: line.
func readSSE(r io.Reader, fn func(sseEvent) (stop bool, err error)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var name string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			name = ""
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			if stop, err := fn(sseEvent{Name: name, Data: strings.TrimSpace(line[5:])}); stop || err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
