package video_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/suakitsu/picpilot-platform-go/core/video"
)

// This end-to-end integration example uses only a local HTTPS mock.
func ExampleGateway() {
	var resultURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			_, _ = w.Write([]byte(`{"data":[{"task_id":"mock_task"}]}`))
		case "/v1/tasks/mock_task":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"status": "completed", "result": map[string]any{"video_url": resultURL}},
			})
		case "/result.mp4":
			if r.Header.Get("Authorization") != "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	resultURL = server.URL + "/result.mp4"
	gateway, err := video.NewGateway(video.Config{
		Provider: video.Seedance,
		APIKey:   "mock-credential",
		BaseURL:  server.URL,
		Client:   server.Client(), // Production: parent-supplied policy transport.
	})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	task, err := gateway.Submit(ctx, video.Request{
		Model: "seedance-mini", Prompt: "Show the product rotating slowly",
		SourceURLs: []string{"https://images.example.test/product.png"},
	})
	if err != nil {
		panic(err) // Production: persist/quarantine ErrAmbiguous; never auto-resubmit.
	}
	result, err := gateway.Poll(ctx, task)
	if err != nil {
		panic(err)
	}
	var destination bytes.Buffer
	fetched, err := gateway.FetchResult(ctx, result.ResultURL, &destination)
	if err != nil {
		panic(err) // Production: discard partial destination on every fetch error.
	}
	fmt.Println(task.Model, result.Status, fetched.Bytes)
	// Output: doubao-seedance-2.0-mini completed 5
}
