package provider

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStrictNoCropDimensionsAndPixels(t *testing.T) {
	data := fixture(t, "png", 57, 11)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageJSONResponse(w, data) }))
	defer s.Close()
	out, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock", Size: "1024x1024"})
	if err != nil {
		t.Fatal(err)
	}
	if out.RequestedSize != "1024x1024" || out.Image.Width != 57 || out.Image.Height != 11 || !bytes.Equal(out.Image.Bytes, data) {
		t.Fatal("requested square size altered non-square output")
	}
	original, _, _ := image.Decode(bytes.NewReader(data))
	decoded, _, err := image.Decode(bytes.NewReader(out.Image.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if original.Bounds() != decoded.Bounds() {
		t.Fatal("bounds changed")
	}
	for y := range 11 {
		for x := range 57 {
			r1, g1, b1, a1 := original.At(x, y).RGBA()
			r2, g2, b2, a2 := decoded.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				t.Fatalf("pixel changed at %d,%d", x, y)
			}
		}
	}
}

func TestOutputPixelAndRawByteCeilings(t *testing.T) {
	data := fixture(t, "png", 5, 3)
	for _, tc := range []struct {
		name         string
		pixels, size int64
		want         ErrorKind
	}{
		{"exact", 15, int64(len(data)), ""},
		{"one_pixel_over", 14, int64(len(data)), LimitExceeded},
		{"one_byte_over", 15, int64(len(data) - 1), LimitExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageJSONResponse(w, data) }))
			defer s.Close()
			_, err := testGateway(t, func(o *Options) { o.MaxPixels, o.MaxImageBytes = tc.pixels, tc.size }).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
			if tc.want != "" {
				assertError(t, err, tc.want, Uncertain)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	// A valid PNG header advertising billions of pixels must be rejected before
	// decoding its compressed stream, irrespective of the small encoded byte count.
	huge := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(huge[16:20], 65000)
	binary.BigEndian.PutUint32(huge[20:24], 65000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageJSONResponse(w, huge) }))
	defer s.Close()
	_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
	assertError(t, err, LimitExceeded, Uncertain)
}

func TestRawReferenceByteCeilingBeforeSubmission(t *testing.T) {
	data := fixture(t, "png", 8, 3)
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("oversize reference was submitted") }))
	defer s.Close()
	_, err := testGateway(t, func(o *Options) { o.MaxImageBytes = int64(len(data) - 1) }).EditImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock", References: []Reference{{Data: data}}})
	assertError(t, err, LimitExceeded, NotSubmitted)
}

func TestReferenceCancellationDoesNotSubmit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			return
		}
		close(started)
		<-release
	}))
	defer s.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	g := testGateway(t, nil)
	go func() {
		_, err := g.GenerateImage(ctx, testProvider(s), ImageRequest{Prompt: "mock", References: []Reference{{URL: s.URL + "/ref"}}})
		done <- err
	}()
	<-started
	cancel()
	assertError(t, <-done, Canceled, NotSubmitted)
	if posts.Load() != 0 {
		t.Fatal("reference failure triggered generation")
	}
}

func TestPendingJobTimeoutAndUnknownAsyncDoNotResubmit(t *testing.T) {
	for _, profile := range []ImageProfile{Aishuch, LegacyOpenAI} {
		var posts atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posts.Add(1)
			}
			_, _ = io.WriteString(w, `{"data":{"status":"pending","task_id":"job-1"}}`)
		}))
		p := testProvider(s)
		p.ImageProfile, p.Timeout = profile, 50*time.Millisecond
		_, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"})
		s.Close()
		kind := TimedOut
		if profile == LegacyOpenAI {
			kind = Unsupported
		}
		assertError(t, err, kind, Uncertain)
		if posts.Load() != 1 {
			t.Fatal("pending task was submitted again")
		}
	}
}

func TestDeepJSONRejected(t *testing.T) {
	deep := strings.Repeat(`{"data":`, 70) + `null` + strings.Repeat("}", 70)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, deep) }))
	defer s.Close()
	_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
	assertError(t, err, InvalidResponse, Uncertain)
}

func TestTrustedPrivateCAWithoutDisablingTLSVerification(t *testing.T) {
	data := fixture(t, "png", 2, 1)
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageJSONResponse(w, data) }))
	defer s.Close()
	pool := x509.NewCertPool()
	pool.AddCert(s.Certificate())
	g := testGateway(t, func(o *Options) { o.TrustedRoots = pool })
	if g.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
	if _, err := g.GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeOpenAIAndLegacyResponseFormatDiffer(t *testing.T) {
	data := fixture(t, "png", 2, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if rawString(body["response_format"]) != "url" {
			t.Error("legacy URL request contract lost")
		}
		imageJSONResponse(w, data) // Upstream may choose inline output even when URL requested.
	}))
	defer s.Close()
	g := testGateway(t, nil)
	p := testProvider(s)
	r := ImageRequest{Prompt: "mock", ResponseFormat: "url"}
	_, err := g.GenerateImage(context.Background(), p, r)
	assertError(t, err, Unsupported, NotSubmitted)
	p.ImageProfile = LegacyOpenAI
	if _, err := g.GenerateImage(context.Background(), p, r); err != nil {
		t.Fatal(err)
	}
}

func TestAishuchNestedURLArray(t *testing.T) {
	data := fixture(t, "png", 3, 1)
	var base string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/out" {
			_, _ = w.Write(data)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"status": "completed", "result": map[string]any{"images": []any{map[string]any{"url": []string{base + "/out"}}}}}})
	}))
	defer s.Close()
	base = s.URL
	p := testProvider(s)
	p.ImageProfile = Aishuch
	if _, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"}); err != nil {
		t.Fatal(err)
	}
}
