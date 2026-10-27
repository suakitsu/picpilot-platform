package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmissionRejectionAndAmbiguity(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		for _, tc := range []struct {
			name string
			http int
			body string
			want error
		}{
			{"bad_request", 400, "private full prompt " + fixtureKey, ErrRejected},
			{"auth", 401, "private full prompt " + fixtureKey, ErrRejected},
			{"forbidden", 403, "rejected", ErrRejected},
			{"not_found", 404, "rejected", ErrRejected},
			{"conflict", 409, "rejected", ErrRejected},
			{"invalid", 422, "rejected", ErrRejected},
			{"rate_limit", 429, "rejected", ErrRejected},
			{"timeout", 408, "unknown", ErrAmbiguous},
			{"server", 500, `{"success":false}`, ErrAmbiguous},
			{"unavailable", 503, `{"error":"busy"}`, ErrAmbiguous},
			{"proxy", 502, "proxy error", ErrAmbiguous},
			{"edge", 524, "timeout", ErrAmbiguous},
			{"empty", 200, "", ErrAmbiguous},
			{"no_id", 202, `{"status":"submitted"}`, ErrAmbiguous},
			{"malformed", 200, `{"id":`, ErrAmbiguous},
			{"html", 200, "<html>upstream proxy</html>", ErrAmbiguous},
			{"null", 200, "null", ErrAmbiguous},
			{"multiple_values", 200, `{"id":"task_1"} {"id":"task_2"}`, ErrAmbiguous},
			{"explicit_code", 200, `{"code":400,"message":"private full prompt"}`, ErrRejected},
			{"explicit_success_false", 200, `{"success":false}`, ErrRejected},
			{"server_envelope", 200, `{"code":500,"success":false}`, ErrAmbiguous},
			{"opaque_error", 200, `{"error":{"message":"private full prompt"}}`, ErrAmbiguous},
			{"credential_id", 200, `{"id":"` + fixtureKey + `"}`, ErrAmbiguous},
		} {
			t.Run(string(provider)+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Retry-After", "99999")
					w.Header().Set("X-Request-ID", fixtureKey)
					w.WriteHeader(tc.http)
					_, _ = w.Write([]byte(tc.body))
				})
				_, err := g.Submit(context.Background(), validRequest())
				if !errors.Is(err, tc.want) || calls.Load() != 1 {
					t.Fatalf("calls=%d err=%v want=%v", calls.Load(), err, tc.want)
				}
				var detail *Error
				if !errors.As(err, &detail) {
					t.Fatal("missing structured error")
				}
				if tc.http >= 300 && (detail.HTTPStatus != tc.http || detail.RetryAfter != 300*time.Second) {
					t.Error("status/retry-after not normalized")
				}
				data, _ := json.Marshal(detail)
				if strings.Contains(err.Error()+string(data), fixtureKey) || strings.Contains(err.Error()+string(data), "private full prompt") {
					t.Fatal("sensitive body/header exposed")
				}
			})
		}
	}
}

func TestContradictoryIDAndQueueFull(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"id":"task_1","code":400,"success":false}`))
		})
		s, err := g.Submit(context.Background(), validRequest())
		if !errors.Is(err, ErrAmbiguous) || s.UpstreamTaskID != "task_1" {
			t.Fatalf("contradiction lost task identity: %+v %v", s, err)
		}
		if provider == Gemini || provider == Seedance {
			g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"task_id":"task_1","success":false}]}`))
			})
			if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) {
				t.Fatalf("contradictory array response: %v", err)
			}
		}
		for _, body := range []string{`{"code":"video_queue_full"}`, `{"code":"video_queue_full","id":"task_1"}`, `{"message":"video queue is full"}`} {
			g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); _, _ = w.Write([]byte(body)) })
			_, err := g.Submit(context.Background(), validRequest())
			want := ErrAmbiguous
			if provider == Agnes && body == `{"code":"video_queue_full"}` {
				want = ErrRejected
			}
			if !errors.Is(err, want) {
				t.Fatalf("queue-full classification: %v want %v", err, want)
			}
		}
	}
}

func TestNetworkFailureAfterPOSTIsAmbiguous(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		var calls atomic.Int32
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		})
		if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) {
			t.Fatalf("lost response: %v", err)
		}
		if calls.Load() != 1 {
			t.Error("paid submission retried")
		}
	}
}

func TestContextCancellationAndTimeout(t *testing.T) {
	var calls atomic.Int32
	g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, func(c *Config) { c.SubmitTimeout = 50 * time.Millisecond; c.PollTimeout = 50 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Submit(ctx, validRequest()); !errors.Is(err, ErrInvalidRequest) || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled submit: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("pre-cancelled context sent a request")
	}
	if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submit body timeout: %v", err)
	}
	if _, err := g.Poll(context.Background(), fixtureTask(t, Gemini)); !errors.Is(err, ErrTransport) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("poll timeout: %v", err)
	}
	if calls.Load() != 2 {
		t.Error("unexpected retry")
	}
}

func TestBoundedJSONAndCallbacks(t *testing.T) {
	g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"task_1","padding":"` + strings.Repeat("x", 4096) + `"}`))
	}, func(c *Config) { c.MaxResponseBytes = 2048 })
	if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("oversize submit response: %v", err)
	}
	if _, err := g.Poll(context.Background(), fixtureTask(t, Gemini)); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize poll response: %v", err)
	}
	if _, err := g.ParseCallback(context.Background(), bytes.Repeat([]byte("x"), 2049)); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize callback: %v", err)
	}
	if _, err := g.ParseCallback(nil, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil callback context: %v", err)
	}
}

func TestRedirectsNeverReplayOrForwardCredentials(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		var originCalls atomic.Int32
		var redirectHook atomic.Int32
		g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
			originCalls.Add(1)
			w.Header().Set("Location", destination.URL+"/download")
			w.WriteHeader(status)
		}, func(c *Config) {
			c.Client.CheckRedirect = func(r *http.Request, via []*http.Request) error { redirectHook.Add(1); return nil }
		})
		if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) {
			t.Fatalf("redirected POST: %v", err)
		}
		if _, err := g.Poll(context.Background(), fixtureTask(t, Gemini)); !errors.Is(err, ErrTransport) {
			t.Fatalf("redirected poll: %v", err)
		}
		if _, err := g.FetchResult(context.Background(), g.base.String()+"/file", io.Discard); !errors.Is(err, ErrProtocol) {
			t.Fatalf("redirected download: %v", err)
		}
		if originCalls.Load() != 3 || destinationCalls.Load() != 0 || redirectHook.Load() != 0 {
			t.Fatal("redirect followed or replayed")
		}
	}
}

type inspectingTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (r inspectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == "POST" && (req.GetBody != nil || req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != "") {
		r.t.Error("paid POST is replayable")
	}
	return r.base.RoundTrip(req)
}

func TestPOSTBodyNotReplayableAndClientNotMutated(t *testing.T) {
	var original *http.Client
	g, server := gatewayFor(t, Seedance, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie jar was used")
		}
		createResponse(w, Seedance)
	}, func(c *Config) {
		c.Client.Transport = inspectingTransport{base: c.Client.Transport, t: t}
		c.Client.Jar, _ = cookiejar.New(nil)
		u, _ := url.Parse(c.BaseURL)
		c.Client.Jar.SetCookies(u, []*http.Cookie{{Name: "credential", Value: fixtureKey}})
		original = c.Client
	})
	if _, err := g.Submit(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	if original.Jar == nil || original.CheckRedirect != nil || server.Client() != original {
		t.Fatal("injected client was mutated")
	}
}

func TestResultFetchBoundsAndNoAuth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		mime    string
		chunked bool
		wantErr error
	}{
		{"video", "video", "video/mp4; charset=binary", false, nil},
		{"octet", "video", "application/octet-stream", false, nil},
		{"exact_limit", "12345678", "video/webm", true, nil},
		{"content_length", "123456789", "video/mp4", false, ErrLimit},
		{"chunked", "123456789", "video/mp4", true, ErrLimit},
		{"html", "error", "text/html", false, ErrProtocol},
		{"empty", "", "video/mp4", false, ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			g, server := gatewayFor(t, Agnes, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Method != "GET" {
					t.Error("download leaked credentials or used wrong method")
				}
				w.Header().Set("Content-Type", tc.mime)
				if tc.chunked {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write([]byte(tc.body))
			}, func(c *Config) { c.MaxResultBytes = 8 })
			var dst bytes.Buffer
			result, err := g.FetchResult(context.Background(), server.URL+"/file.mp4", &dst)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want=%v", err, tc.wantErr)
			}
			if dst.Len() > 8 || calls.Load() != 1 {
				t.Fatal("write limit or request count exceeded")
			}
			if err == nil && (result.Bytes != int64(len(tc.body)) || dst.String() != tc.body || !strings.HasPrefix(result.ContentType, "video/")) {
				t.Errorf("fetch=%+v bytes=%d", result, dst.Len())
			}
		})
	}
}

func TestDownloadAcrossOriginDoesNotUseCredentialOrCookie(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("download leaked credentials")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("video"))
	}))
	defer destination.Close()
	g, _ := gatewayFor(t, HFSY, func(w http.ResponseWriter, r *http.Request) { t.Error("fetch called provider") }, func(c *Config) {
		c.Client.Jar, _ = cookiejar.New(nil)
		u, _ := url.Parse(destination.URL)
		c.Client.Jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "private"}})
	})
	if _, err := g.FetchResult(context.Background(), destination.URL+"/video.mp4", io.Discard); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Error("incorrect download count")
	}
}

func TestFetchTimeoutAndTruncatedResponse(t *testing.T) {
	g, server := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, func(c *Config) { c.FetchTimeout = 50 * time.Millisecond })
	if _, err := g.FetchResult(context.Background(), server.URL, io.Discard); !errors.Is(err, ErrTransport) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("download timeout: %v", err)
	}
	g, server = gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("partial"))
	})
	if _, err := g.FetchResult(context.Background(), server.URL, io.Discard); !errors.Is(err, ErrTransport) {
		t.Fatalf("truncated download: %v", err)
	}
}

func TestConfigurationValidationAndSensitiveFormatting(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("configuration caused I/O") }))
	defer server.Close()
	base := Config{Provider: Gemini, APIKey: fixtureKey, BaseURL: server.URL, Client: server.Client()}
	for _, change := range []func(*Config){
		func(c *Config) { c.Client = nil },
		func(c *Config) { c.Client = &http.Client{} },
		func(c *Config) { c.APIKey = "" },
		func(c *Config) { c.APIKey = "bad\r\nkey" },
		func(c *Config) { c.Provider = "other" },
		func(c *Config) { c.BaseURL = "https://user:pass@example.test" },
		func(c *Config) { c.BaseURL = "http://example.test" },
		func(c *Config) { c.BaseURL = "https://example.test?key=private" },
		func(c *Config) { c.PublicBaseURL = "https://example.test?key=private" },
		func(c *Config) { c.WebhookURL = "file:///secret" },
		func(c *Config) { c.Provider, c.WebhookURL = Agnes, "https://example.test/callback" },
		func(c *Config) { c.AgnesModel = "custom" },
		func(c *Config) { c.Provider, c.AgnesModel = Agnes, "bad model" },
		func(c *Config) { c.SubmitTimeout = time.Minute },
		func(c *Config) { c.PollTimeout = -1 },
		func(c *Config) { c.FetchTimeout = time.Hour },
		func(c *Config) { c.MaxResponseBytes = 5 << 20 },
		func(c *Config) { c.MaxResultBytes = 501 << 20 },
	} {
		c := base
		change(&c)
		if _, err := NewGateway(c); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("invalid configuration accepted: %v", err)
		}
	}
	g, err := NewGateway(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		for _, value := range []any{base, g, validRequest()} {
			formatted := fmt.Sprintf(format, value)
			if strings.Contains(formatted, fixtureKey) || strings.Contains(formatted, validRequest().Prompt) {
				t.Error("sensitive values visible in formatting")
			}
		}
	}
}
