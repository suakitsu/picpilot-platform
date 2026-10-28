package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func executionFixture(t *testing.T, s *Server) (store.User, store.Task) {
	t.Helper()
	_ = initialize(t, s)
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "Mock only", Size: "32x32"}})
	task, _, err := s.Store.SubmitTask(context.Background(), store.SubmitTask{UserID: u.ID, Capability: "image", Kind: "fixture", Request: payload, IdempotencyKey: "engine-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return u, task
}
func addMockProvider(t *testing.T, s *Server, u store.User, id, base string, priority int) {
	t.Helper()
	secret, err := s.Vault.Encrypt(u.ID+"/"+id, "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.PutProvider(context.Background(), store.Provider{ID: id, OwnerScope: u.ID, Name: "Mock", Capability: "image", Protocol: "openai_images_generations", Profile: "openai", BaseURL: base, Model: "gpt-image-2", Enabled: true, Priority: priority, ExecutionMode: "http", MaxConcurrency: 2, TimeoutSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, &secret)
	if err != nil {
		t.Fatal(err)
	}
}
func mockImageReply(w http.ResponseWriter) {
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 32, 32)))
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(data.Bytes())}}})
}

func TestPostgresExecutionDefinitiveFallbackIsSequential(t *testing.T) {
	s := testServer(t, config.Personal)
	u, task := executionFixture(t, s)
	var first, second atomic.Int32
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_api_key"}}`))
	}))
	defer one.Close()
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { second.Add(1); mockImageReply(w) }))
	defer two.Close()
	addMockProvider(t, s, u, "one", one.URL, 0)
	addMockProvider(t, s, u, "two", two.URL, 1)
	g, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.CloseIdleConnections()
	lease, err := s.Store.ClaimTask(context.Background(), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(context.Background(), lease, g)
	final, err := s.Store.Task(context.Background(), u.ID, task.ID)
	if err != nil || final.Status != "succeeded" {
		t.Fatal("fallback did not complete", final.Status, final.ErrorCode)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatal("duplicate or missing upstream calls")
	}
	if strings.Contains(string(final.Result), "b64_json") || strings.Contains(string(final.Result), "fixture-only-key") {
		t.Fatal("raw bytes or key stored in result")
	}
}

func TestPostgresExecutionAmbiguousSubmissionNeverFallsBack(t *testing.T) {
	s := testServer(t, config.Personal)
	u, task := executionFixture(t, s)
	var next atomic.Int32
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer one.Close()
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.Add(1); mockImageReply(w) }))
	defer two.Close()
	addMockProvider(t, s, u, "one", one.URL, 0)
	addMockProvider(t, s, u, "two", two.URL, 1)
	g, _ := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	defer g.CloseIdleConnections()
	lease, err := s.Store.ClaimTask(context.Background(), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(context.Background(), lease, g)
	final, _ := s.Store.Task(context.Background(), u.ID, task.ID)
	if final.Status != "uncertain" || next.Load() != 0 {
		t.Fatal("ambiguous submission retried")
	}
}

func TestPostgresTextImagesPersistReferencesNotBase64(t *testing.T) {
	s := testServer(t, config.Personal)
	_ = initialize(t, s)
	u, _ := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 3, 5)))
	content, _ := json.Marshal([]any{map[string]any{"type": "text", "text": "Keep this prompt unchanged"}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())}}})
	in := jobs.TextInput{Messages: []provider.Message{{Role: "user", Content: content}}}
	prepared, err := s.prepareTextInput(context.Background(), u.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(prepared)
	if strings.Contains(string(raw), "base64,") || !strings.Contains(string(raw), assetURIPrefix) || !strings.Contains(string(raw), "Keep this prompt unchanged") {
		t.Fatal("unsafe or rewritten stored content")
	}
	materialized, err := s.materializeTextInput(context.Background(), u.ID, prepared, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(materialized)
	if !strings.Contains(string(raw), "data:image/png;base64,") {
		t.Fatal("HTTP request not materialized")
	}
}
