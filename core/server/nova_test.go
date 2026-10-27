package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/nova"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/risk"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func novaTestServer(t *testing.T, edition config.Edition) (*Server, string, store.User) {
	t.Helper()
	s, token, user := projectTestServer(t, edition)
	for _, route := range []struct{ method, path, pattern string }{
		{"GET", "/api/nova/config", "GET /api/nova/config"},
		{"POST", "/api/nova/tasks", "POST /api/nova/tasks"},
		{"GET", "/api/nova/tasks/nova_fixture", "GET /api/nova/tasks/{id}"},
		{"POST", "/api/nova/proxy/text", "POST /api/nova/proxy/text"},
	} {
		if _, pattern := s.mux.Handler(httptest.NewRequest(route.method, route.path, nil)); pattern != route.pattern {
			t.Fatalf("parent did not mount Nova route %s %s", route.method, route.path)
		}
	}
	// Repeated fixture registration must remain harmless after New mounts Nova.
	s.registerNovaRoutes()
	s.registerNovaRoutes()
	if edition == config.Platform {
		if _, err := s.Store.AdjustCredits(context.Background(), user.ID, 1000, "fixture", "nova-http", "Fixture", user.ID); err != nil {
			t.Fatal(err)
		}
	}
	return s, token, user
}

func novaRequest(s *Server, method, path, token, key string, body any) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(creative.JSON(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Prefer", "respond-async")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func novaObject(t *testing.T, w *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("expected Nova object")
	}
	if w.Code != want {
		t.Fatalf("Nova status=%d want=%d code=%v", w.Code, want, out["code"])
	}
	return out
}
func novaImageFixture() nova.ImageRequest {
	return nova.ImageRequest{Mode: "text-to-image", Prompt: "Three geometric blocks", ParallelCount: 1, OutputSize: "1K", AspectRatio: "1:1"}
}

func TestPostgresNovaImageMockReloadReplayOwnershipAndAck(t *testing.T) {
	s, token, user := novaTestServer(t, config.Platform)
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/images/generations") {
			t.Error("unexpected mock route")
			w.WriteHeader(400)
			return
		}
		mockImageReply(w)
	}))
	defer mock.Close()
	projectProvider(t, s, user, "image", mock.URL)
	in := novaImageFixture()
	in.ParallelCount = 2
	w := novaRequest(s, "POST", "/api/nova/tasks", token, "test-only-image-submit", in)
	id := novaObject(t, w, 202)["taskId"].(string)
	if calls.Load() != 0 {
		t.Fatal("HTTP handler called provider directly")
	}
	if got := novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "test-only-image-submit", in), 202)["taskId"]; got != id {
		t.Fatal("replay changed Nova id")
	}
	in.Prompt = "different"
	novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "test-only-image-submit", in), 409)
	novaObject(t, novaRequest(s, "POST", "/api/nova/tasks/"+id+"/ack", token, "", nil), 200)
	q := novaObject(t, novaRequest(s, "GET", "/api/nova/queue-status", token, "", nil), 200)
	if q["queuedCount"] != float64(2) {
		t.Fatal("queue did not use durable children")
	}
	g, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		lease, err := s.Store.ClaimTask(context.Background(), "test-only-nova-runner", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		s.processHTTPModelTask(context.Background(), lease, g)
	}
	if calls.Load() != 2 {
		t.Fatal("wrong upstream call count", calls.Load())
	}
	view := novaObject(t, novaRequest(s, "GET", "/api/nova/tasks/"+id, token, "", nil), 200)
	if view["status"] != "completed" || len(view["result"].(map[string]any)["images"].([]any)) != 2 {
		t.Fatal("completed image view missing")
	}
	if !strings.HasPrefix(view["result"].(map[string]any)["images"].([]any)[0].(string), "URL:") {
		t.Fatal("legacy URL prefix lost")
	}
	image := novaRequest(s, "GET", "/api/nova/images/"+id+"/0", token, "", nil)
	if image.Code != 200 || image.Header().Get("Content-Type") != "image/png" || len(image.Body.Bytes()) == 0 {
		t.Fatal("image alias unavailable")
	}
	novaObject(t, novaRequest(s, "GET", "/api/nova/images/"+id+"/0", "", "", nil), 401)
	other, err := s.Store.CreateUser(context.Background(), "nova-other@example.invalid", "test-only-hash", "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := s.Sessions.Issue(other.ID, other.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/nova/tasks/" + id, "/api/nova/images/" + id + "/0"} {
		novaObject(t, novaRequest(s, "GET", path, otherToken, "", nil), 404)
	}
	novaObject(t, novaRequest(s, "POST", "/api/nova/tasks/"+id+"/ack", otherToken, "", nil), 404)
	restarted, err := New(s.Config, s.Store, s.Logger)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Files.Close()
	if v := novaObject(t, novaRequest(restarted, "GET", "/api/nova/tasks/"+id, token, "", nil), 200); v["status"] != "completed" {
		t.Fatal("restart lost mapping")
	}
	listed := novaObject(t, novaRequest(restarted, "GET", "/api/nova/tasks", token, "", nil), 200)
	if len(listed["tasks"].([]any)) != 1 {
		t.Fatal("reload discovery missing")
	}
	for i := 0; i < 3; i++ {
		novaObject(t, novaRequest(s, "POST", "/api/nova/tasks/"+id+"/ack", token, "", nil), 200)
	}
	var charges int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge'").Scan(&charges)
	if charges != 2 || calls.Load() != 2 {
		t.Fatal("poll/ack changed billing or execution")
	}
}

func TestPostgresNovaCentralBackgroundResumesCancellation(t *testing.T) {
	s, token, user := novaTestServer(t, config.Platform)
	ctx := context.Background()
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mock.Close()
	projectProvider(t, s, user, "image", mock.URL)
	in := novaImageFixture()
	in.ParallelCount = 2
	id := novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "background-cancel", in), 202)["taskId"].(string)
	ids, err := s.Store.NovaRunTaskIDs(ctx, user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	// Hold dispatch in the future, then simulate a crash immediately after the
	// durable cancel intent. Only the central background hook may resume it.
	if _, err = s.Store.Pool.Exec(ctx, "UPDATE tasks SET next_run_at=now()+interval '1 hour' WHERE id=ANY($1::text[])", ids); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Pool.Exec(ctx, "UPDATE nova_runs SET cancel_requested_at=now() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = s.RunBackground(runCtx)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("central background shutdown: %v", runErr)
			}
		case <-time.After(10 * time.Second):
			t.Error("central background runners did not stop")
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := s.Store.NovaRun(ctx, user.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.CancelDispatchedAt != nil {
			break
		}
		select {
		case <-done:
			t.Fatalf("central background stopped before cancellation: %v", runErr)
		case <-deadline.C:
			t.Fatal("central background did not resume Nova cancellation")
		case <-ticker.C:
		}
	}
	for _, taskID := range ids {
		task, err := s.Store.Task(ctx, user.ID, taskID)
		if err != nil || task.Status != "cancelled" {
			t.Fatal("central cancellation did not settle queued child", err)
		}
	}
	owner, err := s.Store.UserByID(ctx, user.ID)
	if err != nil || owner.Credits != 1000 || calls.Load() != 0 {
		t.Fatal("central cancellation dispatched a model or did not refund exactly once", err)
	}
}

func TestPostgresNovaImageReferencesValidationAndCancellation(t *testing.T) {
	s, token, user := novaTestServer(t, config.Platform)
	projectProvider(t, s, user, "image", "https://example.invalid")
	ref := projectAsset(t, s, user, "", "reference", 13, 17)
	in := novaImageFixture()
	in.Mode = "image-to-image"
	in.ReferenceAssetIDs = []string{ref.ID}
	in.Images = []nova.Reference{{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(projectPNG(t, 19, 23))}}
	in.Quality = "high"
	in.Background = "transparent"
	id := novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "references", in), 202)["taskId"].(string)
	ids, err := s.Store.NovaRunTaskIDs(context.Background(), user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Store.Task(context.Background(), user.ID, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload jobs.Payload
	if json.Unmarshal(task.Request, &payload) != nil || len(payload.Image.ReferenceAssetIDs) != 2 || payload.Image.ReferenceAssetIDs[0] != ref.ID || payload.Image.Quality != "high" || payload.Image.Background != "transparent" {
		t.Fatal("reference/option normalization lost data")
	}
	if strings.Contains(string(task.Request), "base64") || strings.Contains(string(task.Request), in.Images[0].Data) {
		t.Fatal("raw input bytes persisted")
	}
	cancelled := novaObject(t, novaRequest(s, "POST", "/api/nova/tasks/"+id+"/cancel", token, "", nil), 200)
	if cancelled["status"] != "cancelled" || cancelled["cancellationRequested"] != true {
		t.Fatal("queued cancellation")
	}
	novaObject(t, novaRequest(s, "POST", "/api/nova/tasks/"+id+"/cancel", token, "", nil), 200)
	owner, _ := s.Store.UserByID(context.Background(), user.ID)
	if owner.Credits != 1000 {
		t.Fatal("cancellation refund missing or duplicated")
	}
	for name, mutate := range map[string]func(*nova.ImageRequest){"credential": func(v *nova.ImageRequest) { v.APIKey = "test-only-prohibited-label" }, "url": func(v *nova.ImageRequest) { v.BaseURL = "http://127.0.0.1" }, "foreign-asset": func(v *nova.ImageRequest) {
		v.Mode = "image-to-image"
		v.ReferenceAssetIDs = []string{"asset_nonexistent"}
	}, "unsupported": func(v *nova.ImageRequest) { v.Protocol = "grok" }} {
		copy := novaImageFixture()
		mutate(&copy)
		result := novaRequest(s, "POST", "/api/nova/tasks", token, name, copy)
		if result.Code != 400 && result.Code != 404 {
			t.Fatal("invalid submission admitted", name, result.Code)
		}
	}
	novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "", novaImageFixture()), 400)
	var count int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks").Scan(&count)
	if count != 1 {
		t.Fatal("validation failure queued work")
	}
}

func TestPostgresNovaTextMockToolsAsyncReplayAndVision(t *testing.T) {
	s, token, user := novaTestServer(t, config.Personal)
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Error("unexpected text mock route")
			w.WriteHeader(400)
			return
		}
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), "data:image/png;base64,") || strings.Contains(string(data), "picpilot-asset:") {
			t.Error("vision was not materialized at execution")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test-only-text-result","model":"fixture-text","choices":[{"index":0,"message":{"role":"assistant","content":"ready","tool_calls":[{"id":"call_fixture","type":"function","function":{"name":"propose","arguments":"{\"count\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":7}}`))
	}))
	defer mock.Close()
	projectProvider(t, s, user, "text", mock.URL)
	image := base64.StdEncoding.EncodeToString(projectPNG(t, 13, 11))
	in := nova.TextRequest{Protocol: "openai-chat-completions", Model: "fixture-text", Stream: true, RequestBody: map[string]json.RawMessage{"stream": json.RawMessage("true"), "messages": creative.JSON([]any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Describe these blocks"}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + image}}}}}), "tools": json.RawMessage(`[{"type":"function","function":{"name":"propose","parameters":{"type":"object"}}}]`)}}
	w := novaRequest(s, "POST", "/api/nova/proxy/text", token, "vision-tool-test", in)
	id := novaObject(t, w, 202)["taskId"].(string)
	ids, err := s.Store.NovaRunTaskIDs(context.Background(), user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Store.Task(context.Background(), user.ID, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(task.Request), image) || !strings.Contains(string(task.Request), "picpilot-asset:") || calls.Load() != 0 {
		t.Fatal("text adapter bypassed durable normalized task")
	}
	g, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.CloseIdleConnections()
	lease, err := s.Store.ClaimTask(context.Background(), "test-only-text-runner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(context.Background(), lease, g)
	raw := novaObject(t, novaRequest(s, "GET", "/api/nova/tasks/"+id+"/response", token, "", nil), 200)
	if raw["choices"] == nil || raw["usage"] == nil {
		t.Fatal("raw compatible payload lost")
	}
	stream := novaRequest(s, "POST", "/api/nova/proxy/text", token, "vision-tool-test", in)
	if stream.Code != 200 || !strings.HasPrefix(stream.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(stream.Body.String(), `"tool_calls"`) || !strings.Contains(stream.Body.String(), "[DONE]") {
		t.Fatal("settled replay SSE missing")
	}
	if calls.Load() != 1 {
		t.Fatal("text replay executed twice")
	}
	var count int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks").Scan(&count)
	if count != 1 {
		t.Fatal("text replay queued duplicate")
	}
	bad := in
	bad.Protocol = "anthropic-messages"
	novaObject(t, novaRequest(s, "POST", "/api/nova/proxy/text", token, "bad-text", bad), 400)
	screened := directUserText(jobs.Payload{Version: 1, Text: &jobs.TextInput{Messages: []provider.Message{{Role: "user", Content: in.RequestBody["messages"]}}}})
	if strings.Contains(screened, image) || !strings.Contains(screened, "Describe these blocks") {
		t.Fatal("screening included image bytes or omitted user text")
	}
}

func TestPostgresNovaGalleryModelsAndCookieCSRF(t *testing.T) {
	s, token, user := novaTestServer(t, config.Personal)
	projectProvider(t, s, user, "image", "https://example.invalid")
	if _, err := s.Store.PutProvider(context.Background(), store.Provider{ID: "foreign", OwnerScope: "platform", Name: "Not personal", Capability: "image", Protocol: "openai_images_generations", Profile: "openai", BaseURL: "https://example.invalid", Model: "must-not-leak", Enabled: true, ExecutionMode: "http", MaxConcurrency: 1, TimeoutSeconds: 30, LeaseSeconds: 120, OfflineAction: "fallback"}, nil); err != nil {
		t.Fatal(err)
	}
	models := novaObject(t, novaRequest(s, "GET", "/api/nova/proxy/models", token, "", nil), 200)
	serialized := string(creative.JSON(models))
	if strings.Contains(serialized, "must-not-leak") || strings.Contains(serialized, "base_url") || strings.Contains(serialized, "apiKey") || !strings.Contains(serialized, "fixture-image") {
		t.Fatal("catalog scope or secret leak")
	}
	novaObject(t, novaRequest(s, "GET", "/api/nova/proxy/models?apiKey=test-only-prohibited", token, "", nil), 400)
	settings := map[string]any{"mode": "2", "prompts": []nova.Prompt{{Title: "Fixture", Content: "Three blocks", Type: 1}}, "keywords": []string{"fixture-keyword"}, "password": "test-only-gallery-password", "revision": 0}
	updated := novaObject(t, novaRequest(s, "PUT", "/api/nova/settings", token, "", settings), 200)
	if updated["revision"] != float64(1) {
		t.Fatal("settings revision")
	}
	novaObject(t, novaRequest(s, "GET", "/api/nova/prompts", token, "", nil), 403)
	verified := novaObject(t, novaRequest(s, "POST", "/api/nova/prompt-gallery/verify", token, "", map[string]string{"password": "wrong-test-only"}), 200)
	if verified["ok"] != false {
		t.Fatal("wrong gallery password")
	}
	verified = novaObject(t, novaRequest(s, "POST", "/api/nova/prompt-gallery/verify", token, "", map[string]string{"password": "test-only-gallery-password"}), 200)
	if verified["ok"] != true {
		t.Fatal("gallery verification")
	}
	prompts := novaRequest(s, "GET", "/api/nova/prompts", token, "", nil)
	var list []nova.Prompt
	if prompts.Code != 200 || json.Unmarshal(prompts.Body.Bytes(), &list) != nil || len(list) != 1 {
		t.Fatal("protected gallery source unavailable")
	}
	settings["revision"] = 1
	settings["mode"] = "3"
	delete(settings, "password")
	novaObject(t, novaRequest(s, "PUT", "/api/nova/settings", token, "", settings), 200)
	novaObject(t, novaRequest(s, "GET", "/api/nova/prompts", token, "", nil), 403)
	// Cookie-authenticated mutations still use the parent same-origin policy.
	r := httptest.NewRequest("POST", "/api/nova/tasks", bytes.NewReader(creative.JSON(novaImageFixture())))
	r.AddCookie(&http.Cookie{Name: "picpilot_session", Value: token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "cookie-spoof")
	r.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF policy bypassed")
	}
}

func TestPostgresNovaResponsesMockAndRiskBeforeCharge(t *testing.T) {
	s, token, user := novaTestServer(t, config.Platform)
	ctx := context.Background()
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Error("wrong responses mock path")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"response_fixture","object":"response","status":"completed","model":"fixture-text","output":[{"id":"msg_fixture","type":"message","role":"assistant","content":[{"type":"output_text","text":"ready"}]},{"id":"fc_fixture","type":"function_call","name":"propose","arguments":"{\"count\":1}","call_id":"call_fixture"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`))
	}))
	defer mock.Close()
	projectProvider(t, s, user, "text", mock.URL)
	p, err := s.Store.Provider(ctx, "platform", "text")
	if err != nil {
		t.Fatal(err)
	}
	p.Protocol = string(provider.Responses)
	if _, err = s.Store.PutProvider(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	in := nova.TextRequest{Protocol: "openai-responses", Model: "fixture-text", Stream: false, RequestBody: map[string]json.RawMessage{"input": json.RawMessage(`"Three blocks"`), "tools": json.RawMessage(`[{"type":"function","name":"propose","parameters":{"type":"object"}}]`)}}
	id := novaObject(t, novaRequest(s, "POST", "/api/nova/proxy/text", token, "responses", in), 202)["taskId"].(string)
	g, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.CloseIdleConnections()
	lease, err := s.Store.ClaimTask(ctx, "test-only-responses-runner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(ctx, lease, g)
	response := novaRequest(s, "GET", "/api/nova/tasks/"+id+"/response?stream=true", token, "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "response.completed") || !strings.Contains(response.Body.String(), `"function_call"`) {
		t.Fatal("responses SSE missing", response.Code)
	}
	if calls.Load() != 1 {
		t.Fatal("wrong mock responses count")
	}
	c := risk.Config{Enabled: true, BanThreshold: 100, BlockLevels: []string{"high"}, WarnLevels: []string{}, Keywords: map[string][]string{"high": {"test-only-blocked"}, "medium": {}, "low": {}}}
	if err = s.Store.PutSetting(ctx, "risk_control", creative.JSON(c), user.ID); err != nil {
		t.Fatal(err)
	}
	projectProvider(t, s, user, "image", "https://example.invalid")
	image := novaImageFixture()
	image.Prompt = "test-only-blocked"
	for i := 0; i < 3; i++ {
		novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "risk-image", image), 422)
	}
	in.RequestBody["input"] = json.RawMessage(`"test-only-blocked"`)
	for i := 0; i < 2; i++ {
		novaObject(t, novaRequest(s, "POST", "/api/nova/proxy/text", token, "risk-text", in), 422)
	}
	var events, tasks, charges int
	if err = s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_risk_events WHERE user_id=$1),(SELECT count(*) FROM tasks),(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge')`, user.ID).Scan(&events, &tasks, &charges); err != nil {
		t.Fatal(err)
	}
	if events != 2 || tasks != 1 || charges != 0 {
		t.Fatal("blocked operations queued/charged or duplicated risk events", events, tasks, charges)
	}
}

func TestNovaAdapterValidationAndWaitBounds(t *testing.T) {
	for _, value := range []string{"wait=-1", "wait=26", "wait=9999999999999999", "wait=1, respond-async", "other"} {
		r := httptest.NewRequest("POST", "/api/nova/proxy/text", nil)
		r.Header.Set("Prefer", value)
		if _, err := novaWait(r); err == nil {
			t.Fatal("unbounded Prefer value accepted")
		}
	}
	for _, raw := range []string{`[{"role":"unknown","content":"a"}]`, `[{"role":"tool","content":"a"}]`, `[{"role":"user","content":"a","apiKey":"test-only-label"}]`} {
		in := nova.TextRequest{Protocol: "openai-chat-completions", RequestBody: map[string]json.RawMessage{"messages": json.RawMessage(raw)}}
		if _, err := novaTextInput(in); err == nil {
			t.Fatal("invalid nested message accepted")
		}
	}
	for _, query := range []string{"apiKey=test-only-label", "baseUrl=https://example.invalid", "protocol=grok", "capability=video", "apiKey=x;ignored=y", "protocol=openai&protocol=openai"} {
		if _, err := parseNovaModelQuery(httptest.NewRequest("GET", "/api/nova/proxy/models?"+query, nil)); err == nil {
			t.Fatal("unsafe model query accepted")
		}
	}
}
