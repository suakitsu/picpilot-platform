package provider_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/video"
)

func TestSecureClientVideoSubmitPollAndResult(t *testing.T) {
	var resultURL string
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/result.mp4" && r.Header.Get("Authorization") != "Bearer mock-video-key" {
			t.Error("video API auth lost")
		}
		switch r.URL.Path {
		case "/v1/videos/generations":
			_, _ = io.WriteString(w, `{"data":[{"task_id":"mock_task"}]}`)
		case "/v1/tasks/mock_task":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "completed", "result": map[string]any{"video_url": resultURL}}})
		case "/result.mp4":
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("video result auth leaked")
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "mock-video")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()
	resultURL = s.URL + "/result.mp4"
	pool := x509.NewCertPool()
	pool.AddCert(s.Certificate())
	client, err := provider.NewHTTPClient(provider.Options{AllowLocalDevelopment: true, TrustedRoots: pool, Timeout: 5 * time.Minute, MaxResponseBytes: 500 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	g, err := video.NewGateway(video.Config{Provider: video.Seedance, APIKey: "mock-video-key", BaseURL: s.URL, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	task, err := g.Submit(context.Background(), video.Request{Model: "seedance-mini", Prompt: "mock scene", SourceURLs: []string{"https://images.example.test/mock.png"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.Poll(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	fetched, err := g.FetchResult(context.Background(), result.ResultURL, &output)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Bytes != 10 || output.String() != "mock-video" {
		t.Fatal("video download corrupted")
	}
}
