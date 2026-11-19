package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
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
	// AllowRemote marks a cloud connection. Every dial is checked, either way, against the address
	// the name resolved to at that moment (PLAN 23.5):
	//   - a local connection (false) reaches only loopback, private and CGNAT/tailnet addresses, so a
	//     DNS answer that changes after startup can't send data elsewhere;
	//   - a cloud connection (true) reaches only public addresses and only over TLS, so its URL or a
	//     DNS answer can't point the server at itself, its network or a metadata service.
	// Neither follows a redirect to another host.
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
	errRedirect       = errors.New("redirect to another host")
	errPlaintext      = errors.New("a cloud connection must use https")
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
		c.HTTPClient = &http.Client{Transport: Transport(c.AllowRemote)}
	}
	if c.HTTPClient.CheckRedirect == nil {
		hc := *c.HTTPClient // the caller's client stays as it is
		hc.CheckRedirect = sameHostOnly
		c.HTTPClient = &hc
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.API == Anthropic {
		return &anthropicClient{c: c}
	}
	return &openAIClient{c: c}
}

// Transport is the HTTP transport New uses: no proxy, TLS 1.2 or newer, every dial checked (see
// Config.AllowRemote), and for a cloud connection no request without TLS. Wrappers (llm-probe's
// recorder) build on it to keep the guard.
func Transport(allowRemote bool) http.RoundTripper {
	t := transport(allowRemote)
	if allowRemote {
		return httpsOnly{t}
	}
	return t
}

// httpsOnly refuses a request that would leave without TLS (the first one or a redirect's).
type httpsOnly struct{ next http.RoundTripper }

func (h httpsOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, errPlaintext
	}
	return h.next.RoundTrip(req)
}

// sameHostOnly is the clients' redirect policy: a model server may redirect within itself
// (a trailing slash, a versioned path), never to another host, port or scheme. Go drops the
// Authorization header on such a redirect but not an x-api-key header, and the request's body
// (the prompt) would follow in any case.
func sameHostOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	if first := via[0].URL; req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		return errRedirect
	}
	return nil
}

func transport(allowRemote bool) *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	// Control runs for every connection attempt with the address that was resolved, so a name that
	// resolves differently later is checked again.
	d.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		switch {
		case allowRemote && (err != nil || !PublicAddr(ip)):
			return fmt.Errorf("%w: %s is not a public address", errBlockedAddress, host)
		case !allowRemote && (err != nil || !PrivateAddr(ip)):
			return fmt.Errorf("%w: %s is not a private address", errBlockedAddress, host)
		}
		return nil
	}
	return &http.Transport{
		Proxy:                 nil, // model traffic never goes through an outbound proxy
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0, // bounded by IdleTimeout instead (a model may think before answering)
	}
}

// PrivateAddr reports whether a local model server may live at an address: loopback, private
// (RFC 1918, ULA) or the CGNAT range Tailscale uses (100.64.0.0/10). Link-local addresses are not
// among them: nothing runs a model server there, and cloud metadata services do answer there.
func PrivateAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip)
}

// PublicAddr reports whether a cloud connection may dial an address: a global unicast address
// outside every range that is private, shared, link-local, reserved or only for documentation, and
// outside the prefixes that carry an IPv4 address inside an IPv6 one.
func PublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var (
	cgnat     = netip.MustParsePrefix("100.64.0.0/10")
	notPublic = []netip.Prefix{
		cgnat,
		netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
		netip.MustParsePrefix("192.0.0.0/24"),    // protocol assignments
		netip.MustParsePrefix("192.0.2.0/24"),    // documentation
		netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
		netip.MustParsePrefix("198.51.100.0/24"), // documentation
		netip.MustParsePrefix("203.0.113.0/24"),  // documentation
		netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and broadcast
		netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: an IPv4 address inside
		netip.MustParsePrefix("64:ff9b:1::/48"),  // local NAT64
		netip.MustParsePrefix("2002::/16"),       // 6to4: an IPv4 address inside
		netip.MustParsePrefix("2001::/32"),       // Teredo
		netip.MustParsePrefix("2001:db8::/32"),   // documentation
	}
)

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
