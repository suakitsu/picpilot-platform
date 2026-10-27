package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

// NewHTTPClient exposes the gateway's SSRF/TLS policy to other HTTP adapters.
// It installs no credentials or cookie jar. Explicit request auth is preserved
// for API calls; every redirect drops headers except Accept and Range. Only GET
// and HEAD redirects are followed, and POST bodies cannot be replayed.
// MaxRequestBytes and MaxResponseBytes apply to streamed bodies too. For video,
// set MaxResponseBytes and Timeout to accommodate the result-download budget.
// Do not replace Transport, add a Jar, or relax CheckRedirect. The standard
// http.Client may wrap errors in url.Error (including the URL); callers must not
// log raw HTTP errors. Gateway methods and SafeDownload return sanitized errors.
func NewHTTPClient(o Options) (*http.Client, error) {
	g, err := NewGateway(o)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     &policyTransport{gateway: g},
		Timeout:       g.opts.Timeout,
		CheckRedirect: g.fetch.CheckRedirect,
	}, nil
}

type policyTransport struct{ gateway *Gateway }

func (t *policyTransport) CloseIdleConnections() { t.gateway.CloseIdleConnections() }

func (t *policyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	g := t.gateway
	if err := g.checkURL(r.URL); err != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	if r.ContentLength > g.opts.MaxRequestBytes {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, fail(LimitExceeded, NotSubmitted)
	}
	var started atomic.Bool
	ctx := httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{
		WroteHeaders: func() { started.Store(true) },
		WroteRequest: func(httptrace.WroteRequestInfo) { started.Store(true) },
	})
	out := r.Clone(ctx)
	out.GetBody = nil
	out.Header.Del("Proxy-Authorization")
	if out.Method != http.MethodGet && out.Method != http.MethodHead {
		// These headers opt otherwise non-idempotent methods into net/http's
		// retry machinery. Preserve the caller's original request, not the opt-in.
		out.Header.Del("Idempotency-Key")
		out.Header.Del("X-Idempotency-Key")
	}
	if out.Body != nil && out.Body != http.NoBody {
		out.Body = &boundedBody{body: out.Body, remaining: g.opts.MaxRequestBytes, ctx: ctx, state: Uncertain}
	}
	resp, err := g.transport.RoundTrip(out)
	state := NotSubmitted
	if out.Method != http.MethodGet && out.Method != http.MethodHead && started.Load() {
		state = Uncertain
	}
	if err != nil {
		return nil, safeTransportError(ctx, err, state)
	}
	if resp.ContentLength > g.opts.MaxResponseBytes || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		_ = resp.Body.Close()
		return nil, fail(LimitExceeded, state)
	}
	resp.Body = &boundedBody{body: resp.Body, remaining: g.opts.MaxResponseBytes, ctx: ctx, state: state}
	return resp, nil
}

type boundedBody struct {
	body      io.ReadCloser
	remaining int64
	ctx       context.Context
	state     Submission
	overflow  bool
}

func (b *boundedBody) Close() error { return b.body.Close() }

func (b *boundedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.overflow {
		return 0, fail(LimitExceeded, b.state)
	}
	if b.ctx.Err() != nil {
		return 0, contextual(b.ctx.Err(), b.state)
	}
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.body.Read(probe[:])
		if n > 0 {
			b.overflow = true
			_ = b.body.Close()
			return 0, fail(LimitExceeded, b.state)
		}
		if err != nil && err != io.EOF {
			err = safeTransportError(b.ctx, err, b.state)
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:int(b.remaining)]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	if err != nil && err != io.EOF {
		err = safeTransportError(b.ctx, err, b.state)
	}
	return n, err
}

func safeTransportError(ctx context.Context, err error, state Submission) *Error {
	if ctx.Err() != nil {
		return contextual(ctx.Err(), state)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fail(TimedOut, state)
	}
	return contextual(err, state)
}

// SafeDownload is a credential-free, bounded binary fetch for one-off callers.
// Repeated callers should reuse Gateway.Download. MaxResponseBytes applies.
func SafeDownload(ctx context.Context, rawURL string, o Options) ([]byte, error) {
	g, err := NewGateway(o)
	if err != nil {
		return nil, err
	}
	defer g.CloseIdleConnections()
	return g.Download(ctx, rawURL)
}

// Download retrieves bytes without credentials, cookies or unbounded reads.
// It does not validate a media format; use DownloadImage for uploaded image URLs.
func (g *Gateway) Download(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := g.callContext(ctx, Provider{})
	defer cancel()
	return g.exchange(ctx, http.MethodGet, rawURL, nil, "", nil, g.opts.MaxResponseBytes, NotSubmitted)
}

// DownloadImage validates byte/pixel ceilings and fully decodes PNG/JPEG/WebP
// references. Returned dimensions are real; returned bytes are never transformed.
func (g *Gateway) DownloadImage(ctx context.Context, rawURL string) (Image, error) {
	ctx, cancel := g.callContext(ctx, Provider{})
	defer cancel()
	b, err := g.exchange(ctx, http.MethodGet, rawURL, nil, "", nil, g.opts.MaxImageBytes, NotSubmitted)
	if err != nil {
		return Image{}, err
	}
	img, err := g.inspectImage(b, NotSubmitted)
	if err != nil {
		return Image{}, err
	}
	if ctx.Err() != nil {
		return Image{}, contextual(ctx.Err(), NotSubmitted)
	}
	return img, nil
}
