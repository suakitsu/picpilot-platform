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
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func projectTestServer(t *testing.T, edition config.Edition) (*Server, string, store.User) {
	t.Helper()
	s := testServer(t, edition)
	if _, pattern := s.mux.Handler(httptest.NewRequest("GET", "/api/projects", nil)); pattern != "GET /api/projects" {
		s.registerProjectRoutes()
	}
	if _, pattern := s.mux.Handler(httptest.NewRequest("GET", "/api/assistant/status", nil)); pattern != "GET /api/assistant/status" {
		s.registerAssistantRoutes()
	}
	token := initialize(t, s)
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	return s, token, u
}
func projectRequest(s *Server, method, path, token, key string, body any) (int, map[string]any) {
	r := httptest.NewRequest(method, path, bytes.NewReader(creative.JSON(body)))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}
func projectOK(t *testing.T, s *Server, method, path, token, key string, body any) map[string]any {
	t.Helper()
	status, out := projectRequest(s, method, path, token, key, body)
	if status != 200 {
		t.Fatalf("%s %s: status=%d code=%v", method, path, status, out["code"])
	}
	value, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatal("expected object response")
	}
	return value
}
func projectProvider(t *testing.T, s *Server, u store.User, capability, base string) {
	t.Helper()
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = u.ID
	}
	secret, err := s.Vault.Encrypt(scope+"/"+capability, "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	protocol := "openai_chat_completions"
	model := "fixture-text"
	if capability == "image" {
		protocol = "openai_images_generations"
		model = "fixture-image"
	}
	_, err = s.Store.PutProvider(context.Background(), store.Provider{ID: capability, OwnerScope: scope, Name: "Isolated mock", Capability: capability, Protocol: protocol, Profile: "openai", BaseURL: base, Model: model, Enabled: true, ExecutionMode: "http", MaxConcurrency: 2, TimeoutSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, &secret)
	if err != nil {
		t.Fatal(err)
	}
}
func projectPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func projectAsset(t *testing.T, s *Server, u store.User, project, kind string, w, h int) store.Asset {
	t.Helper()
	a, err := s.saveImage(context.Background(), u.ID, project, "", kind, "fixture.png", bytes.NewReader(projectPNG(t, w, h)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func finishProjectText(t *testing.T, s *Server, id, text string, payload any) {
	t.Helper()
	task, err := s.Store.ClaimTaskCapabilities(context.Background(), "creative-test", time.Minute, []string{"text"})
	if err != nil || task.ID != id {
		t.Fatal("claim mismatch", err)
	}
	if _, err = s.Store.CompleteTask(context.Background(), task, creative.JSON(map[string]any{"text": text, "payload": payload, "model": "fixture-text"})); err != nil {
		t.Fatal(err)
	}
}

const projectCopyFixture = `{"title":"Fixture Cup","selling_point":["One","Two","Three","Four","Five"],"product_description":{"Feature":["Known use"],"Material":"Confirmed steel"}}`

func prepareListing(t *testing.T, s *Server, token string, u store.User, include bool) (string, []store.Asset) {
	t.Helper()
	p := projectOK(t, s, "POST", "/api/projects", token, "create-project-fixture", map[string]any{"productName": "Fixture Cup", "includeAplus": include, "aplus_size": "970x600", "material": "Confirmed steel"})
	id := p["id"].(string)
	assets := []store.Asset{projectAsset(t, s, u, id, "main", 13, 21), projectAsset(t, s, u, id, "detail", 17, 9)}
	copy := projectOK(t, s, "POST", "/api/projects/"+id+"/generate-copy", token, "listing-copy-fixture", nil)
	task, err := s.Store.Task(context.Background(), u.ID, copy["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(task.Request), "base64") || !strings.Contains(string(task.Request), "picpilot-asset:"+assets[0].ID) {
		t.Fatal("copy vision is not durable asset references")
	}
	finishProjectText(t, s, task.ID, projectCopyFixture, map[string]any{})
	detail := projectOK(t, s, "GET", "/api/projects/"+id, token, "", nil)
	if detail["project"].(map[string]any)["status"] != "copy_generated" || detail["original_info"].(map[string]any)["include_aplus"] != include {
		t.Fatal("copy erased A+ selection")
	}
	return id, assets
}

func TestPostgresProjectsAtomicListingAplusRollbackReplay(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	id, assets := prepareListing(t, s, token, u, true)
	ctx := context.Background()
	if _, err := s.Store.Pool.Exec(ctx, "UPDATE users SET credits=45 WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	status, out := projectRequest(s, "POST", "/api/projects/"+id+"/listing-package", token, "atomic-package-fixture", map[string]any{})
	if status != 402 {
		t.Fatal("partial affordable package was accepted", status, out["code"])
	}
	var count, credits, charges int
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = s.Store.Pool.QueryRow(ctx, "SELECT credits FROM users WHERE id=$1", u.ID).Scan(&credits)
	_ = s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge'").Scan(&charges)
	if count != 0 || charges != 0 || credits != 45 {
		t.Fatal("seven-image/42-credit partial commit", count, charges, credits)
	}
	_, err := s.Store.Pool.Exec(ctx, "UPDATE users SET credits=48 WHERE id=$1", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := projectOK(t, s, "POST", "/api/projects/"+id+"/listing-package", token, "atomic-package-fixture", map[string]any{})
	if result["aplus_requested"] != true || len(result["image_tasks"].([]any)) != 7 {
		t.Fatal("A+ omitted or missing listing slots")
	}
	aplus := result["aplus_project"].(map[string]any)
	aid := aplus["project"].(map[string]any)["id"].(string)
	tasks, err := s.Store.ProjectTasks(ctx, u.ID, aid)
	if err != nil || len(tasks) != 1 {
		t.Fatal("A+ task missing", err)
	}
	var payload jobs.Payload
	_ = json.Unmarshal(tasks[0].Request, &payload)
	if payload.Image.Size != "970x600" || len(payload.Image.ReferenceAssetIDs) != 2 || payload.Image.ReferenceAssetIDs[0] != assets[0].ID || payload.Image.ReferenceAssetIDs[1] != assets[1].ID {
		t.Fatal("canvas or reference order changed")
	}
	var wg sync.WaitGroup
	results := make(chan int, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := projectRequest(s, "POST", "/api/projects/"+id+"/listing-package", token, "atomic-package-fixture", map[string]any{})
			results <- status
		}()
	}
	wg.Wait()
	close(results)
	for status := range results {
		if status != 200 {
			t.Fatal("concurrent replay failed", status)
		}
	}
	_ = s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&count)
	_ = s.Store.Pool.QueryRow(ctx, "SELECT credits FROM users WHERE id=$1", u.ID).Scan(&credits)
	if count != 8 || credits != 0 {
		t.Fatal("duplicate charge or generation", count, credits)
	}
	status, _ = projectRequest(s, "POST", "/api/projects/"+id+"/listing-package", token, "atomic-package-fixture", map[string]any{"include_aplus": false})
	if status != 409 {
		t.Fatal("idempotency conflict not detected", status)
	}
}

func TestPostgresProjectsSeedlessAuthFactsAndDeletion(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	status, out := projectRequest(s, "GET", "/api/projects", "", "", nil)
	if status != 401 {
		t.Fatal("auth missing", status)
	}
	status, out = projectRequest(s, "GET", "/api/projects", token, "", nil)
	if status != 200 || len(out["data"].([]any)) != 0 {
		t.Fatal("seedless history incorrect")
	}
	p := projectOK(t, s, "POST", "/api/projects", token, "project-empty-fixture", map[string]any{"product_name": "Fixture"})
	id := p["id"].(string)
	status, out = projectRequest(s, "POST", "/api/projects/"+id+"/generate-copy", token, "copy-unavailable", nil)
	if status != 503 || out["code"] != "NO_PROVIDER_CONFIGURED" {
		t.Fatal("fake copy success without provider")
	}
	facts := map[string]any{"confirmed_facts": map[string]any{"dimensions": "10 x 20 cm", "material": "steel"}}
	result := projectOK(t, s, "POST", "/api/projects/"+id+"/confirm-facts", token, "", facts)
	if result["status"] != "facts_confirmed" {
		t.Fatal("facts did not persist")
	}
	other, err := s.Store.CreateUser(context.Background(), "another@example.invalid", "fixture-hash", "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := s.Sessions.Issue(other.ID, other.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = projectRequest(s, "GET", "/api/projects/"+id, otherToken, "", nil)
	if status != 404 {
		t.Fatal("cross-owner project visible", status)
	}
	foreign := projectAsset(t, s, other, "", "reference", 3, 5)
	status, _ = projectRequest(s, "POST", "/api/projects/"+id+"/product-images/link", token, "link-foreign", map[string]any{"url": "picpilot-asset:" + foreign.ID})
	if status != 404 {
		t.Fatal("foreign image linked", status)
	}
	status, _ = projectRequest(s, "POST", "/api/projects/"+id+"/product-images/link", token, "link-loopback", map[string]any{"url": "http://127.0.0.1:1/secret"})
	if status != 400 {
		t.Fatal("unsafe URL accepted", status)
	}
	projectOK(t, s, "DELETE", "/api/projects/"+id, token, "", nil)
	if _, err = s.Store.Project(context.Background(), u.ID, id); err != store.ErrNotFound {
		t.Fatal("project not deleted")
	}
}

func TestPostgresProjectsRetryUncertainAndIdempotency(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "image", "https://example.invalid")
	_, _ = s.Store.Pool.Exec(context.Background(), "UPDATE users SET credits=30 WHERE id=$1", u.ID)
	created := projectOK(t, s, "POST", "/api/projects/creative-images", token, "single-fixture", map[string]any{"prompt": "Three simple blocks", "size": "1024x1024"})
	id := created["project"].(map[string]any)["id"].(string)
	taskID := created["image_tasks"].([]any)[0].(map[string]any)["id"].(string)
	_, _ = s.Store.Pool.Exec(context.Background(), "UPDATE tasks SET status='uncertain' WHERE id=$1", taskID)
	status, _ := projectRequest(s, "POST", "/api/projects/"+id+"/images/"+taskID+"/retry", token, "retry-fixture", map[string]any{})
	if status != 409 {
		t.Fatal("ambiguous paid task retried", status)
	}
	status, _ = projectRequest(s, "DELETE", "/api/projects/"+id, token, "", nil)
	if status != 409 {
		t.Fatal("uncertain project hidden")
	}
	_, _ = s.Store.Pool.Exec(context.Background(), "UPDATE tasks SET status='queued' WHERE id=$1", taskID)
	lease, err := s.Store.ClaimTask(context.Background(), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.FailTask(context.Background(), lease, "FIXTURE_REJECTED"); err != nil {
		t.Fatal(err)
	}
	retry := projectOK(t, s, "POST", "/api/projects/"+id+"/images/"+taskID+"/retry", token, "retry-fixture", map[string]any{"reference_text": "Keep every edge visible"})
	replay := projectOK(t, s, "POST", "/api/projects/"+id+"/images/"+taskID+"/retry", token, "retry-fixture", map[string]any{"reference_text": "Keep every edge visible"})
	if retry["id"] != replay["id"] {
		t.Fatal("retry duplicated")
	}
	user, err := s.Store.UserByID(context.Background(), u.ID)
	if err != nil || user.Credits != 24 {
		t.Fatal("retry refund/charge incorrect", user.Credits, err)
	}
}

func TestPostgresCanvasTextVisionToolCallsAndLimits(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	projectProvider(t, s, u, "text", "https://example.invalid")
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(projectPNG(t, 9, 13))
	input := map[string]any{"protocol": "openai_chat_completions", "model": "fixture-text", "request_body": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Inspect"}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}}}}, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}, map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "Known fact"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}}}}
	result := projectOK(t, s, "POST", "/api/projects/canvas-text", token, "canvas-fixture", input)
	id := result["task_id"].(string)
	task, err := s.Store.Task(context.Background(), u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(task.Request)
	if strings.Contains(raw, "data:image") || !strings.Contains(raw, "picpilot-asset:") || !strings.Contains(raw, "call_1") {
		t.Fatal("vision/tool persistence contract violated")
	}
	replay := projectOK(t, s, "POST", "/api/projects/canvas-text", token, "canvas-fixture", input)
	if replay["task_id"] != id {
		t.Fatal("normalized image IDs broke replay")
	}
	payload := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}}}}
	finishProjectText(t, s, id, "", payload)
	output := projectOK(t, s, "GET", "/api/projects/text-tasks/"+id, token, "", nil)
	if output["choices"] == nil {
		t.Fatal("structured tool-only response lost")
	}
	oversized := map[string]any{"prompt": strings.Repeat("x", creative.MaxField+1)}
	status, _ := projectRequest(s, "POST", "/api/projects/optimize-prompt", token, "long-input", oversized)
	if status != 400 {
		t.Fatal("input field limit missing")
	}
	req := projectOK(t, s, "POST", "/api/projects/optimize-prompt", token, "long-output", map[string]any{"prompt": "Fixture"})
	finishProjectText(t, s, req["task_id"].(string), strings.Repeat("x", creative.MaxField+1), map[string]any{})
	status, _ = projectRequest(s, "GET", "/api/projects/text-tasks/"+req["task_id"].(string), token, "", nil)
	if status != 502 {
		t.Fatal("oversized output accepted", status)
	}
}

func TestPostgresProjectTextExecutesThroughMockHTTP(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	var seen bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-only-key" {
			t.Error("unexpected provider request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "A sharp product photograph with every edge visible."}, "finish_reason": "stop"}}})
	}))
	defer upstream.Close()
	projectProvider(t, s, u, "text", upstream.URL)
	response := projectOK(t, s, "POST", "/api/projects/optimize-prompt", token, "http-fixture", map[string]any{"prompt": "Photograph a cup"})
	if seen {
		t.Fatal("handler called upstream directly")
	}
	gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.CloseIdleConnections()
	lease, err := s.Store.ClaimTaskCapabilities(context.Background(), "http-fixture", time.Minute, []string{"text"})
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(context.Background(), lease, gateway)
	final := projectOK(t, s, "GET", "/api/projects/text-tasks/"+response["task_id"].(string), token, "", nil)
	if !seen || final["optimized_prompt"] == nil {
		t.Fatal("queued HTTP text did not finish", final["status"])
	}
}
