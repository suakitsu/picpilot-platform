package provider

import (
	"bytes"
	"context"
	"errors"
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

func testHTTPClient(t *testing.T, change func(*Options)) *http.Client {
	t.Helper()
	o := Options{AllowLocalDevelopment: true, Timeout: 2 * time.Second}
	if change != nil {
		change(&o)
	}
	c, err := NewHTTPClient(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestHTTPClientDefaultPolicyAndPinnedDNS(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Goog-Api-Key") != "" {
			t.Error("factory injected credentials")
		}
		_, _ = io.WriteString(w, "mock video")
	}))
	defer s.Close()
	c := testHTTPClient(t, func(o *Options) { o.AllowLocalDevelopment = false })
	_, err := c.Get(s.URL)
	assertError(t, err, BlockedAddress, NotSubmitted)
	if hits.Load() != 0 {
		t.Fatal("default client reached loopback")
	}
	u, _ := url.Parse(s.URL)
	var lookups atomic.Int32
	c = testHTTPClient(t, func(o *Options) {
		o.AllowedHosts = []string{"mock-video.test"}
		o.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			if lookups.Add(1) != 1 {
				return nil, errors.New("second DNS lookup")
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		})
	})
	resp, err := c.Get("http://mock-video.test:" + u.Port() + "/video")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(b) != "mock video" || lookups.Load() != 1 {
		t.Fatal("factory lost pinned DNS or download")
	}
	_, err = c.Get("http://mock-video.test.attacker.invalid:" + u.Port())
	assertError(t, err, BlockedAddress, NotSubmitted)
}

func TestHTTPClientAuthIsRequestScopedAndRedirectHeadersAreStripped(t *testing.T) {
	var finalHits atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalHits.Add(1)
		for _, key := range []string{"Authorization", "Cookie", "X-Goog-Api-Key", "X-Custom-Provider-Key", "Referer"} {
			if r.Header.Get(key) != "" {
				t.Errorf("redirect leaked %s", key)
			}
		}
		if r.Header.Get("Accept") != "video/mp4" || r.Header.Get("Range") != "bytes=0-5" {
			t.Error("safe download headers lost")
		}
		_, _ = io.WriteString(w, "video")
	}))
	defer cdn.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/poll" {
			if r.Header.Get("Authorization") != "Bearer "+testKey {
				t.Error("explicit polling auth removed")
			}
			http.Redirect(w, r, cdn.URL+"/result", http.StatusFound)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("client retained credentials for next request")
		}
	}))
	defer api.Close()
	c := testHTTPClient(t, nil)
	r, _ := http.NewRequest("GET", api.URL+"/poll", nil)
	for _, key := range []string{"Authorization", "Cookie", "X-Goog-Api-Key", "X-Custom-Provider-Key", "Referer"} {
		r.Header.Set(key, "Bearer "+testKey)
	}
	r.Header.Set("Accept", "video/mp4")
	r.Header.Set("Range", "bytes=0-5")
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if r.Header.Get("Authorization") != "Bearer "+testKey || finalHits.Load() != 1 {
		t.Fatal("caller mutated or redirect failed")
	}
	resp, err = c.Get(api.URL + "/uncredentialed")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestHTTPClientNeverRedirectsOrRetriesPOST(t *testing.T) {
	for _, status := range []int{302, 303, 307, 308, 503} {
		var calls atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
				t.Error("transport retry opt-in survived")
			}
			w.Header().Set("Location", "/again")
			w.WriteHeader(status)
		}))
		r, _ := http.NewRequest("POST", s.URL, strings.NewReader(`{"mock":true}`))
		r.Header.Set("Idempotency-Key", "mock-id")
		originalReplay := r.GetBody
		resp, err := testHTTPClient(t, nil).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		s.Close()
		if calls.Load() != 1 || resp.StatusCode != status || originalReplay == nil || r.GetBody == nil || r.Header.Get("Idempotency-Key") != "mock-id" {
			t.Fatal("POST replay or caller mutation")
		}
	}
}

func TestHTTPClientRevalidatesRedirectAndBoundsBody(t *testing.T) {
	for _, mode := range []string{"redirect", "length", "chunked", "exact"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == "redirect" {
				http.Redirect(w, r, "http://169.254.169.254/metadata", 302)
				return
			}
			if mode == "length" {
				w.Header().Set("Content-Length", "128")
			}
			if mode == "chunked" {
				w.(http.Flusher).Flush()
			}
			n := 128
			if mode == "exact" {
				n = 64
			}
			_, _ = io.WriteString(w, strings.Repeat("x", n))
		}))
		c := testHTTPClient(t, func(o *Options) { o.MaxResponseBytes = 64 })
		resp, err := c.Get(s.URL)
		var data []byte
		if err == nil {
			data, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		s.Close()
		if mode == "exact" {
			if err != nil || len(data) != 64 {
				t.Fatalf("exact limit rejected: %v", err)
			}
			continue
		}
		kind := LimitExceeded
		if mode == "redirect" {
			kind = BlockedAddress
		}
		assertError(t, err, kind, NotSubmitted)
	}
}

func TestHTTPClientRequestLimitAndCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("preflight failure reached server") }))
	defer s.Close()
	c := testHTTPClient(t, func(o *Options) { o.MaxRequestBytes = 16 })
	r, _ := http.NewRequest("POST", s.URL, strings.NewReader(strings.Repeat("x", 17)))
	_, err := c.Do(r)
	assertError(t, err, LimitExceeded, NotSubmitted)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ = http.NewRequestWithContext(ctx, "POST", s.URL, strings.NewReader("mock"))
	_, err = c.Do(r)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSafeDownloadsForUploadedReferences(t *testing.T) {
	data := fixture(t, "png", 17, 3)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentialed safe download")
		}
		if r.URL.Path == "/invalid" {
			_, _ = io.WriteString(w, "plain bytes")
			return
		}
		_, _ = w.Write(data)
	}))
	defer s.Close()
	g := testGateway(t, nil)
	img, err := g.DownloadImage(context.Background(), s.URL)
	if err != nil || img.Width != 17 || img.Height != 3 || !bytes.Equal(img.Bytes, data) {
		t.Fatalf("reference transform/metadata error: %v", err)
	}
	b, err := SafeDownload(context.Background(), s.URL+"/invalid", Options{AllowLocalDevelopment: true})
	if err != nil || string(b) != "plain bytes" {
		t.Fatal("generic downloader required image format")
	}
	_, err = g.DownloadImage(context.Background(), s.URL+"/invalid")
	assertError(t, err, InvalidResponse, NotSubmitted)
	_, err = SafeDownload(context.Background(), s.URL, Options{})
	assertError(t, err, BlockedAddress, NotSubmitted)
	_, err = SafeDownload(context.Background(), s.URL+"/invalid", Options{AllowLocalDevelopment: true, MaxResponseBytes: 4})
	assertError(t, err, LimitExceeded, NotSubmitted)
}

func TestHTTPClientAmbiguousDisconnectIsNeverReplayed(t *testing.T) {
	var posts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = io.WriteString(w, "prime")
			return
		}
		_, _ = io.ReadAll(r.Body)
		posts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer s.Close()
	c := testHTTPClient(t, nil)
	resp, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	r, _ := http.NewRequest("POST", s.URL, strings.NewReader(`{"paid_generation":"mock"}`))
	r.Header.Set("Idempotency-Key", "mock-id")
	_, err = c.Do(r)
	assertError(t, err, TransportFailure, Uncertain)
	if posts.Load() != 1 {
		t.Fatal("ambiguous generation replayed")
	}
}

func TestHTTPClientUnknownLengthUploadIsBounded(t *testing.T) {
	var received atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received.Store(int64(len(b)))
	}))
	defer s.Close()
	c := testHTTPClient(t, func(o *Options) { o.MaxRequestBytes = 16 })
	r, _ := http.NewRequest("POST", s.URL, io.NopCloser(strings.NewReader(strings.Repeat("x", 128))))
	_, err := c.Do(r)
	var pe *Error
	if !errors.As(err, &pe) || pe.Submission != Uncertain {
		t.Fatalf("partial upload must be uncertain: %v", err)
	}
	if received.Load() > 16 {
		t.Fatal("upload ceiling bypassed")
	}
}
