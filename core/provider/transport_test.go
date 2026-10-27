package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestSSRFDeniedByDefault(t *testing.T) {
	g := testGateway(t, func(o *Options) { o.AllowLocalDevelopment = false })
	for _, ip := range []string{"127.0.0.1", "127.1.2.3", "10.1.2.3", "172.31.0.1", "192.168.0.1", "169.254.169.254", "0.0.0.0", "100.64.0.1", "224.0.0.1", "255.255.255.255", "::1", "::", "fc00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::7f00:1", "2002:7f00:1::"} {
		if g.allowedIP(netip.MustParseAddr(ip)) {
			t.Errorf("allowed special-use IP %s", ip)
		}
	}
	for _, endpoint := range []string{"file:///image.png", "https://user:pass@example.test/a", "http://example.test/a", "https://127.0.0.1/a", "https://[fe80::1%25eth0]/a", "https://metadata.google.internal/a", "https://example.test:70000/a", "https://example.test/a#token"} {
		u, err := url.Parse(endpoint)
		if err == nil && g.checkURL(u) == nil {
			t.Errorf("allowed URL %s", endpoint)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !g.allowedIP(netip.MustParseAddr(ip)) {
			t.Errorf("public IP blocked %s", ip)
		}
	}
}

func TestLocalOptInStillBlocksMetadataAndMulticast(t *testing.T) {
	g := testGateway(t, nil)
	for _, ip := range []string{"127.0.0.1", "::1", "192.168.1.2", "10.1.2.3"} {
		if !g.allowedIP(netip.MustParseAddr(ip)) {
			t.Error("local opt-in not applied")
		}
	}
	for _, ip := range []string{"169.254.169.254", "fe80::1", "0.0.0.0", "224.0.0.1"} {
		if g.allowedIP(netip.MustParseAddr(ip)) {
			t.Error("local opt-in exposed metadata or multicast")
		}
	}
}

func TestMixedDNSAndRebindingFailBeforeDial(t *testing.T) {
	var lookups atomic.Int32
	g := testGateway(t, func(o *Options) {
		o.AllowLocalDevelopment = false
		o.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			if lookups.Add(1) == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		})
	})
	if _, err := g.resolve(context.Background(), "rebind.test"); err != nil {
		t.Fatal(err)
	}
	_, err := g.dialContext(context.Background(), "tcp", "rebind.test:443")
	assertError(t, err, BlockedAddress, NotSubmitted)
	g.opts.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}, nil
	})
	_, err = g.dialContext(context.Background(), "tcp", "mixed.test:443")
	assertError(t, err, BlockedAddress, NotSubmitted)
}

func TestDialUsesValidatedIPWithoutSecondDNSLookup(t *testing.T) {
	data := fixture(t, "png", 2, 3)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageJSONResponse(w, data) }))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	var calls atomic.Int32
	g := testGateway(t, func(o *Options) {
		o.Resolver = resolverFunc(func(ctx context.Context, network, host string) ([]netip.Addr, error) {
			if calls.Add(1) != 1 || host != "mock-api.test" {
				return nil, errors.New("unexpected second lookup")
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		})
	})
	p := testProvider(s)
	p.BaseURL = "http://mock-api.test:" + u.Port()
	_, err := g.GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("did not pin validated IP")
	}
	if g.transport.Proxy != nil {
		t.Fatal("environment proxy bypass enabled")
	}
}

func TestHostAllowlist(t *testing.T) {
	allowed := []string{"images.test"}
	g := testGateway(t, func(o *Options) { o.AllowedHosts = allowed })
	allowed[0] = "attacker.test"
	for _, host := range []string{"images.test.attacker.test", "attacker.test", "api.images.test"} {
		if g.allowedHost(host) {
			t.Error("allowlist suffix match or mutable alias")
		}
	}
	if !g.allowedHost("IMAGES.TEST.") {
		t.Error("allowlist exact canonical match failed")
	}
}

func TestDownloadsNeverCarryAPIAuth(t *testing.T) {
	data := fixture(t, "jpeg", 9, 2)
	var downloads atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		for _, key := range []string{"Authorization", "X-Goog-Api-Key", "Cookie", "Referer"} {
			if r.Header.Get(key) != "" {
				t.Errorf("download disclosed %s", key)
			}
		}
		w.Header().Set("Content-Type", "text/plain") // Sniff/decode bytes, not the header.
		_, _ = w.Write(data)
	}))
	defer cdn.Close()
	var base string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if r.Header.Get("Authorization") != "Bearer "+testKey {
				t.Error("missing POST auth")
			}
			w.Header().Set("Set-Cookie", "key="+testKey)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": base + "/redirect"}}})
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("same-origin download disclosed auth")
		}
		http.Redirect(w, r, strings.Replace(cdn.URL, "127.0.0.1", "localhost", 1)+"/out", http.StatusFound)
	}))
	defer api.Close()
	base = api.URL
	resp, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(api), ImageRequest{Prompt: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 || resp.Image.Width != 9 || resp.Image.MIMEType != "image/jpeg" {
		t.Fatal("download not decoded")
	}
}

func TestRedirectProtection(t *testing.T) {
	for _, target := range []string{"http://169.254.169.254/latest", "file:///tmp/a", "http://user:password@127.0.0.1/"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target, http.StatusFound) }))
		_, err := testGateway(t, nil).exchange(context.Background(), "GET", s.URL, nil, "", nil, 1024, Uncertain)
		s.Close()
		assertError(t, err, BlockedAddress, Uncertain)
	}
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
	assertError(t, err, UpstreamHTTP, Uncertain)
	if calls.Load() != 1 {
		t.Fatal("POST followed redirect/replayed")
	}
}

func TestStatusClassificationNoRetriesNoSecrets(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 429, 500, 502, 524} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var posts atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, testKey+" private mock prompt")
			}))
			defer s.Close()
			_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "private mock prompt"})
			state, kind := Uncertain, UpstreamHTTP
			if status >= 400 && status < 500 && status != 408 {
				state = Rejected
			}
			if status == 401 || status == 403 {
				kind = Authentication
			}
			if status == 429 {
				kind = RateLimited
			}
			pe := assertError(t, err, kind, state)
			if pe.StatusCode != status || pe.Definitive() != (state != Uncertain) || strings.Contains(pe.Error(), "private mock prompt") || posts.Load() != 1 {
				t.Fatal("unsafe error or retry")
			}
		})
	}
}

func TestCancelBeforeAndAfterSubmission(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		posts.Add(1)
		close(started)
		<-release
	}))
	defer s.Close()
	defer close(release)
	g := testGateway(t, nil)
	p := testProvider(s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.GenerateImage(ctx, p, ImageRequest{Prompt: "mock"})
	assertError(t, err, Canceled, NotSubmitted)
	if posts.Load() != 0 {
		t.Fatal("submitted canceled request")
	}
	ctx, cancel = context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := g.GenerateImage(ctx, p, ImageRequest{Prompt: "mock"}); result <- err }()
	<-started
	cancel()
	err = <-result
	assertError(t, err, Canceled, Uncertain)
	if !errors.Is(err, context.Canceled) || posts.Load() != 1 {
		t.Fatal("cancellation identity/retry mismatch")
	}
}

func TestTimeoutAndTruncatedResponseAreUncertain(t *testing.T) {
	for _, mode := range []string{"timeout", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			var posts atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)
				posts.Add(1)
				if mode == "timeout" {
					<-release
					return
				}
				w.Header().Set("Content-Length", "1000")
				_, _ = io.WriteString(w, `{"data":`)
			}))
			defer s.Close()
			defer close(release)
			p := testProvider(s)
			p.Timeout = 100 * time.Millisecond
			_, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"})
			kind := TransportFailure
			if mode == "timeout" {
				kind = TimedOut
			}
			assertError(t, err, kind, Uncertain)
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("timeout identity lost")
			}
			if posts.Load() != 1 {
				t.Fatal("generation replayed")
			}
		})
	}
}

func TestDialFailureIsNotSubmitted(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	p := testProvider(s)
	s.Close()
	_, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"})
	assertError(t, err, TransportFailure, NotSubmitted)
}

func TestBoundedResponseAndRequestBodies(t *testing.T) {
	for _, mode := range []string{"length", "chunked", "encoded"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "length" {
					w.Header().Set("Content-Length", "1024")
				}
				if mode == "encoded" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				if mode == "chunked" {
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, strings.Repeat("x", 512))
			}))
			defer s.Close()
			_, err := testGateway(t, func(o *Options) { o.MaxResponseBytes = 128 }).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
			assertError(t, err, LimitExceeded, Uncertain)
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("oversized request submitted") }))
	defer s.Close()
	_, err := testGateway(t, func(o *Options) { o.MaxRequestBytes = 64 }).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: strings.Repeat("p", 200)})
	assertError(t, err, LimitExceeded, NotSubmitted)
}

func TestDefaultGatewayCannotReachLoopback(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("default SSRF policy bypassed") }))
	defer s.Close()
	g := testGateway(t, func(o *Options) { o.AllowLocalDevelopment = false })
	_, err := g.GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
	assertError(t, err, BlockedAddress, NotSubmitted)
}

func TestTLSVerificationNotDisabled(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted certificate accepted") }))
	defer s.Close()
	_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
	assertError(t, err, TransportFailure, NotSubmitted)
}

func TestDownloadLimitAndFailureRemainUncertain(t *testing.T) {
	for _, mode := range []string{"limit", "auth", "invalid", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					target := base + "/image"
					if mode == "blocked" {
						target = "http://169.254.169.254/image"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": target}}})
					return
				}
				if mode == "auth" {
					w.WriteHeader(401)
					return
				}
				if mode == "limit" {
					_, _ = io.WriteString(w, strings.Repeat("x", 64))
					return
				}
				_, _ = io.WriteString(w, "not an image")
			}))
			defer s.Close()
			base = s.URL
			_, err := testGateway(t, func(o *Options) { o.MaxImageBytes = 32 }).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
			kinds := map[string]ErrorKind{"limit": LimitExceeded, "auth": Authentication, "invalid": InvalidResponse, "blocked": BlockedAddress}
			assertError(t, err, kinds[mode], Uncertain)
		})
	}
}
