package video

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReferenceAndDurationBoundaries(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		var calls atomic.Int32
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); createResponse(w, provider) })
		maxRefs := map[Provider]int{Gemini: 3, Seedance: 4, HFSY: 4, Agnes: 1}[provider]
		r := validRequest()
		r.SourceURLs = make([]string, maxRefs)
		for i := range r.SourceURLs {
			r.SourceURLs[i] = fixtureImage
		}
		if _, err := g.Submit(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		r.SourceURLs = append(r.SourceURLs, fixtureImage)
		if _, err := g.Submit(context.Background(), r); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("too many references: %v", err)
		}
		for _, duration := range []int{2, 3, 4, 5, 8, 10, 11, 14, 15, 16} {
			r = validRequest()
			r.DurationSeconds = duration
			valid := duration >= 3 && duration <= 10
			if provider == Seedance && duration == 15 {
				valid = true
			}
			if provider == HFSY {
				valid = duration >= 4 && duration <= 15
			}
			before := calls.Load()
			_, err := g.Submit(context.Background(), r)
			if (err == nil) != valid || (!valid && calls.Load() != before) {
				t.Fatalf("provider=%s duration=%d err=%v", provider, duration, err)
			}
		}
	}
}

func TestIdentityAndContextGuards(t *testing.T) {
	var calls atomic.Int32
	g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, task := range []Submission{
		{}, {UpstreamTaskID: "task_1", Provider: HFSY, Model: "sd-2.0-fast"},
		{UpstreamTaskID: "../task", Provider: Gemini, Model: "gemini-omni-flash-preview"},
		{UpstreamTaskID: "task_1", Provider: Gemini, Model: "other"},
	} {
		if _, err := g.Poll(context.Background(), task); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid poll identity: %v", err)
		}
	}
	if _, err := g.Poll(nil, fixtureTask(t, Gemini)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := g.FetchResult(nil, fixtureVideo, io.Discard); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := g.FetchResult(context.Background(), fixtureVideo, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := g.FetchResult(context.Background(), "file:///private.mp4", io.Discard); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("guard made an HTTP call")
	}
	agnes, _ := gatewayFor(t, Agnes, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"status":"running"}`)) }, func(c *Config) { c.AgnesModel = "agnes-custom/v3" })
	if _, err := agnes.Poll(context.Background(), Submission{UpstreamTaskID: "task_1", Provider: Agnes, Model: "agnes-custom/v3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agnes.ParseCallback(context.Background(), []byte(`{}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
}

func TestSingleTaskArrayRequired(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"task_1"},{"id":"task_2"}]}`, `{"id":"task_1","data":[1]}`} {
		g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := g.Submit(context.Background(), validRequest()); !errors.Is(err, ErrAmbiguous) {
			t.Fatalf("unexpected task array: %v", err)
		}
	}
}

func TestConcurrentGatewayUse(t *testing.T) {
	var calls atomic.Int32
	g, _ := gatewayFor(t, Seedance, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); createResponse(w, Seedance) })
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Submit(context.Background(), validRequest()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 16 {
		t.Fatal("unexpected number of explicit submissions")
	}
}

func TestUntrustedMetadataIsNotEchoed(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"private full prompt","quota":"` + fixtureKey + `","seconds":"private full prompt","cost":"` + fixtureKey + `","size":"private full prompt","metadata":{"size_mapping":{"resolution":"` + fixtureKey + `","width":-1,"height":999999}},"prompt":"private full prompt"}`))
		})
		result, err := g.Poll(context.Background(), fixtureTask(t, provider))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range result.Metadata {
			if strings.Contains(value, fixtureKey) || strings.Contains(value, "private full prompt") {
				t.Fatal("untrusted metadata echoed")
			}
		}
	}
}
