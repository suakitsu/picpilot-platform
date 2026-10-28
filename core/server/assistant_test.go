package server

import (
	"context"
	"strings"
	"testing"

	"encoding/json"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func TestPostgresAssistantStatusQueuedResultAndNoFakeSuccess(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	status := projectOK(t, s, "GET", "/api/assistant/status", "", "", nil)
	if status["enabled"] != false || status["model"] != "" || len(status["models"].([]any)) != 0 {
		t.Fatal("clean install advertised nonexistent models")
	}
	request := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "Only the prompt, do not open a page"}}, "locale": "en-US", "page_context": map[string]any{"tab": "copy"}}
	code, _ := projectRequest(s, "POST", "/api/assistant/chat", token, "assistant-disabled", request)
	if code != 503 {
		t.Fatal("assistant pretended enabled", code)
	}
	projectProvider(t, s, u, "text", "https://example.invalid")
	status = projectOK(t, s, "GET", "/api/assistant/status", token, "", nil)
	if status["enabled"] != true || status["model"] != "fixture-text" {
		t.Fatal("configured model not reflected")
	}
	queued := projectOK(t, s, "POST", "/api/assistant/chat", token, "assistant-request", request)
	if queued["reply"] != nil || queued["status"] != "queued" {
		t.Fatal("assistant fabricated sync success")
	}
	finishProjectText(t, s, queued["task_id"].(string), `{"reply":"A clear photograph of the supplied product.","intent":"image","action":"open_image_agent","action_href":"https://bad.invalid/","suggested_prompt":"A clear product photograph","confidence":0.9}`, map[string]any{})
	result := projectOK(t, s, "GET", "/api/assistant/tasks/"+queued["task_id"].(string), token, "", nil)
	if result["action"] != "none" || result["action_href"] != "" || !strings.Contains(result["reply"].(string), "photograph") {
		t.Fatal("assistant content intent or navigation policy lost")
	}
	replay := projectOK(t, s, "POST", "/api/assistant/chat", token, "assistant-request", request)
	if replay["task_id"] != queued["task_id"] || replay["reply"] != result["reply"] {
		t.Fatal("assistant replay mismatch")
	}
	var images int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&images)
	if images != 0 {
		t.Fatal("assistant submitted generation")
	}
	bad := projectOK(t, s, "POST", "/api/assistant/chat", token, "assistant-invalid", request)
	finishProjectText(t, s, bad["task_id"].(string), "not a JSON response", map[string]any{})
	code, _ = projectRequest(s, "GET", "/api/assistant/tasks/"+bad["task_id"].(string), token, "", nil)
	if code != 502 {
		t.Fatal("invalid assistant output faked as success")
	}
	long := map[string]any{"messages": []any{map[string]any{"role": "user", "content": strings.Repeat("x", creative.MaxField+1)}}}
	code, _ = projectRequest(s, "POST", "/api/assistant/chat", token, "assistant-long", long)
	if code != 400 {
		t.Fatal("assistant input limit not enforced")
	}
}

func TestPostgresTextSelectorsAcceptAnyMatchingFallback(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	projectProvider(t, s, u, "text", "https://example.invalid")
	secret, err := s.Vault.Encrypt(u.ID+"/responses", "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.PutProvider(context.Background(), store.Provider{ID: "responses", OwnerScope: u.ID, Name: "Second mock", Capability: "text", Protocol: string(provider.Responses), BaseURL: "https://example.invalid", Model: "second-text", Enabled: true, ExecutionMode: "http", Priority: 1, MaxConcurrency: 2, TimeoutSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, &secret)
	if err != nil {
		t.Fatal(err)
	}
	// No selection must keep the original Messages/options untouched.
	optimize := projectOK(t, s, "POST", "/api/projects/optimize-prompt", token, "heterogeneous-auto", map[string]any{"prompt": "Fixture"})
	task, err := s.Store.Task(context.Background(), u.ID, optimize["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var p jobs.Payload
	_ = json.Unmarshal(task.Request, &p)
	if p.Text.Model != "" || p.Text.Protocol != "" || len(p.Text.Messages) != 2 || len(p.Text.Input) != 0 {
		t.Fatal("handler rewrote canonical input or forced a protocol")
	}
	selected := projectOK(t, s, "POST", "/api/projects/canvas-text", token, "heterogeneous-selected", map[string]any{"model": "second-text", "protocol": string(provider.Responses), "request_body": map[string]any{"input": []any{map[string]any{"role": "user", "content": "Fixture"}}, "text": map[string]any{"format": map[string]any{"type": "json_object"}}}})
	task, err = s.Store.Task(context.Background(), u.ID, selected["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	p = jobs.Payload{}
	_ = json.Unmarshal(task.Request, &p)
	if p.Text.Model != "second-text" || p.Text.Protocol != provider.Responses || len(p.Text.Input) == 0 {
		t.Fatal("explicit selectors lost")
	}
	mismatch, _ := projectRequest(s, "POST", "/api/projects/canvas-text", token, "heterogeneous-mismatch", map[string]any{"model": "fixture-text", "protocol": string(provider.Responses), "request_body": map[string]any{"input": "Fixture"}})
	if mismatch != 409 {
		t.Fatal("selectors matched different providers independently", mismatch)
	}
	assistant := projectOK(t, s, "POST", "/api/assistant/chat", token, "heterogeneous-assistant", map[string]any{"model": "second-text", "messages": []any{map[string]any{"role": "user", "content": "Hello"}}})
	task, err = s.Store.Task(context.Background(), u.ID, assistant["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	p = jobs.Payload{}
	_ = json.Unmarshal(task.Request, &p)
	if p.Text.Model != "second-text" || p.Text.Protocol != "" {
		t.Fatal("assistant selected wrong routing scope")
	}
}
