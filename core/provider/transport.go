package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// Options configures a transport owned by the gateway. Environment proxies,
// cookie jars, arbitrary RoundTrippers and insecure TLS are intentionally absent.
// Resolver must be trusted. AllowedHosts, when set, matches exact DNS names/IPs
// for both API calls and downloads (no suffix/wildcard matching).
type Options struct {
	AllowLocalDevelopment bool
	AllowedHosts          []string
	Resolver              Resolver
	TrustedRoots          *x509.CertPool
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	MaxResponseBytes      int64
	MaxImageBytes         int64
	MaxRequestBytes       int64
	MaxPixels             int64
	MaxRedirects          int
	PollInterval          time.Duration
}

type Gateway struct {
	opts      Options
	transport *http.Transport
	api       *http.Client
	fetch     *http.Client
}

func NewGateway(o Options) (*Gateway, error) {
	if o.Timeout < 0 || o.DialTimeout < 0 || o.TLSHandshakeTimeout < 0 || o.ResponseHeaderTimeout < 0 || o.PollInterval < 0 || o.MaxRedirects < 0 || o.MaxResponseBytes < 0 || o.MaxImageBytes < 0 || o.MaxRequestBytes < 0 || o.MaxPixels < 0 {
		return nil, fail(InvalidRequest, NotSubmitted)
	}
	if o.Timeout == 0 {
		o.Timeout = 180 * time.Second
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.TLSHandshakeTimeout == 0 {
		o.TLSHandshakeTimeout = 10 * time.Second
	}
	if o.ResponseHeaderTimeout == 0 {
		o.ResponseHeaderTimeout = o.Timeout
	}
	if o.MaxResponseBytes == 0 {
		o.MaxResponseBytes = 32 << 20
	}
	if o.MaxImageBytes == 0 {
		o.MaxImageBytes = 20 << 20
	}
	if o.MaxRequestBytes == 0 {
		o.MaxRequestBytes = 64 << 20
	}
	if o.MaxPixels == 0 {
		o.MaxPixels = 32_000_000
	}
	if o.MaxRedirects == 0 {
		o.MaxRedirects = 3
	}
	if o.PollInterval == 0 {
		o.PollInterval = 3 * time.Second
	}
	// Keep +1 bounded reads and byte-buffer allocations safe on all architectures.
	if o.MaxResponseBytes > 1<<30 || o.MaxImageBytes > 1<<30 || o.MaxRequestBytes > 1<<30 || o.MaxPixels > 100_000_000 {
		return nil, fail(InvalidRequest, NotSubmitted)
	}
	if o.Resolver == nil {
		o.Resolver = net.DefaultResolver
	}
	if o.TrustedRoots != nil {
		o.TrustedRoots = o.TrustedRoots.Clone()
	}
	o.AllowedHosts = append([]string(nil), o.AllowedHosts...)
	for i, host := range o.AllowedHosts {
		o.AllowedHosts[i] = strings.ToLower(strings.TrimSuffix(host, "."))
	}
	g := &Gateway{opts: o}
	g.transport = &http.Transport{
		Proxy:                  nil,
		DialContext:            g.dialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.TrustedRoots},
		TLSHandshakeTimeout:    o.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  o.ResponseHeaderTimeout,
		MaxResponseHeaderBytes: 64 << 10,
		IdleConnTimeout:        60 * time.Second,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    4,
		MaxConnsPerHost:        16,
		ForceAttemptHTTP2:      true,
		DisableCompression:     true,
	}
	g.api = &http.Client{Transport: g.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	g.fetch = &http.Client{Transport: g.transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
			return http.ErrUseLastResponse
		}
		if len(via) > o.MaxRedirects {
			return fail(LimitExceeded, NotSubmitted)
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme != "https" {
			return fail(BlockedAddress, NotSubmitted)
		}
		// Allowlist safe download headers, dropping even provider-specific auth.
		header := make(http.Header)
		for _, name := range []string{"Accept", "Range"} {
			if value := r.Header.Get(name); value != "" {
				header.Set(name, value)
			}
		}
		r.Header = header
		return g.checkURL(r.URL)
	}}
	return g, nil
}

func (g *Gateway) CloseIdleConnections() { g.transport.CloseIdleConnections() }

func (g *Gateway) callContext(ctx context.Context, p Provider) (context.Context, context.CancelFunc) {
	timeout := g.opts.Timeout
	if p.Timeout > 0 {
		timeout = p.Timeout
	}
	return context.WithTimeout(ctx, timeout)
}

var deniedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("::/96"), netip.MustParsePrefix("fec0::/10"),
}

var developmentEgressNetwork = netip.MustParsePrefix("198.18.0.0/15")

func (g *Gateway) allowedIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.Zone() != "" || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
		return false
	}
	if a.IsPrivate() || a.IsLoopback() {
		return g.opts.AllowLocalDevelopment
	}
	if !a.IsGlobalUnicast() {
		return false
	}
	for _, p := range deniedNetworks {
		if p.Contains(a) {
			// Some local outbound gateways map public provider DNS through the
			// RFC 2544 benchmark range. This remains opt-in; metadata, link-local,
			// multicast and all other denied ranges stay blocked.
			if p == developmentEgressNetwork && g.opts.AllowLocalDevelopment {
				return true
			}
			return false
		}
	}
	return true
}

func (g *Gateway) allowedHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || strings.ContainsAny(host, "%\\\x00 \t\r\n") || host == "metadata.google.internal" {
		return false
	}
	if len(g.opts.AllowedHosts) > 0 {
		for _, allowed := range g.opts.AllowedHosts {
			if host == allowed {
				return true
			}
		}
		return false
	}
	return true
}

func (g *Gateway) checkURL(u *url.URL) error {
	if u == nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || !g.allowedHost(u.Hostname()) || (u.Scheme != "https" && u.Scheme != "http") {
		return fail(BlockedAddress, NotSubmitted)
	}
	if u.Scheme == "http" && !g.opts.AllowLocalDevelopment {
		return fail(BlockedAddress, NotSubmitted)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fail(BlockedAddress, NotSubmitted)
		}
	}
	if a, err := netip.ParseAddr(u.Hostname()); err == nil && !g.allowedIP(a) {
		return fail(BlockedAddress, NotSubmitted)
	}
	return nil
}

func (g *Gateway) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if !g.allowedHost(host) {
		return nil, fail(BlockedAddress, NotSubmitted)
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		var err error
		addrs, err = g.opts.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, contextual(err, NotSubmitted)
		}
	}
	if len(addrs) == 0 {
		return nil, fail(TransportFailure, NotSubmitted)
	}
	// Reject mixed public/private answers before dialing any address.
	for _, a := range addrs {
		if !g.allowedIP(a) {
			return nil, fail(BlockedAddress, NotSubmitted)
		}
	}
	return addrs, nil
}

func (g *Gateway) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fail(BlockedAddress, NotSubmitted)
	}
	addrs, err := g.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: g.opts.DialTimeout, KeepAlive: 30 * time.Second}
	for _, a := range addrs {
		// Dial the validated IP, never the hostname: no second DNS lookup/rebinding.
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fail(TransportFailure, NotSubmitted)
}

func (g *Gateway) endpoint(p Provider, path string) (string, error) {
	if strings.TrimSpace(p.Model) == "" || p.Timeout < 0 || len(p.Model) > 256 || strings.ContainsAny(string(p.APIKey), "\r\n\x00") {
		return "", fail(InvalidRequest, NotSubmitted)
	}
	u, err := url.Parse(strings.TrimRight(p.BaseURL, "/"))
	if err != nil || u.RawQuery != "" {
		return "", fail(InvalidRequest, NotSubmitted)
	}
	if err := g.checkURL(u); err != nil {
		return "", err
	}
	base := u.String()
	for _, suffix := range []string{"/v1", "/v1beta"} {
		if strings.HasSuffix(base, suffix) && strings.HasPrefix(path, suffix+"/") {
			return base + strings.TrimPrefix(path, suffix), nil
		}
	}
	if (path == "/v1/chat/completions" && strings.HasSuffix(base, "/chat/completions")) || (path == "/v1/responses" && strings.HasSuffix(base, "/responses")) {
		return base, nil
	}
	if p.DeepSeek && path == "/v1/chat/completions" {
		path = "/chat/completions"
	}
	return base + path, nil
}

func (g *Gateway) exchange(ctx context.Context, method, endpoint string, body []byte, contentType string, p *Provider, limit int64, prior Submission) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextual(err, prior)
	}
	if int64(len(body)) > g.opts.MaxRequestBytes {
		return nil, fail(LimitExceeded, prior)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fail(BlockedAddress, prior)
	}
	if err := g.checkURL(u); err != nil {
		return nil, contextual(err, prior)
	}
	var started atomic.Bool
	trace := &httptrace.ClientTrace{WroteHeaders: func() { started.Store(true) }, WroteRequest: func(httptrace.WroteRequestInfo) { started.Store(true) }}
	ctx = httptrace.WithClientTrace(ctx, trace)
	r, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fail(InvalidRequest, prior)
	}
	// In particular, disable transport-level replay of POST bodies.
	r.GetBody = nil
	r.Header.Set("Accept", "application/json")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	client := g.fetch
	if p != nil {
		client = g.api
		if p.APIKey != "" {
			if p.ImageProfile == GoogleGemini {
				r.Header.Set("X-Goog-Api-Key", string(p.APIKey))
			} else {
				r.Header.Set("Authorization", "Bearer "+string(p.APIKey))
			}
		}
	} else {
		r.Header.Set("Accept", "*/*")
	}
	resp, err := client.Do(r)
	state := prior
	if method == http.MethodPost && started.Load() {
		state = Uncertain
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, contextual(ctx.Err(), state)
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fail(TimedOut, state)
		}
		return nil, contextual(err, state)
	}
	defer resp.Body.Close()
	if method == http.MethodPost {
		state = Uncertain
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := UpstreamHTTP
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			kind = Authentication
		}
		if resp.StatusCode == 429 {
			kind = RateLimited
		}
		if method == http.MethodPost && resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
			state = Rejected
		}
		return nil, &Error{Kind: kind, Submission: state, StatusCode: resp.StatusCode}
	}
	if resp.ContentLength > limit || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		return nil, fail(LimitExceeded, state)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, contextual(ctx.Err(), state)
		}
		return nil, contextual(err, state)
	}
	if int64(len(b)) > limit {
		return nil, fail(LimitExceeded, state)
	}
	return b, nil
}

func (g *Gateway) jsonCall(ctx context.Context, method, endpoint string, body any, p Provider, prior Submission) (map[string]json.RawMessage, error) {
	var b []byte
	var err error
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return nil, fail(InvalidRequest, prior)
		}
		if !boundedJSON(b) {
			return nil, fail(InvalidRequest, prior)
		}
	}
	b, err = g.exchange(ctx, method, endpoint, b, "application/json", &p, g.opts.MaxResponseBytes, prior)
	if err != nil {
		return nil, err
	}
	return decodeObject(b)
}
