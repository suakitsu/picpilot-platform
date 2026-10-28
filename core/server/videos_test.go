package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
	"github.com/suakitsu/picpilot-platform/core/video"
)

func videoHTTPFixture(t *testing.T, edition config.Edition) (*Server, string, store.User) {
	t.Helper()
	s := testServer(t, edition) // Uses internal/testpg.Open and an isolated schema.
	s.registerVideoRoutes()
	token := initialize(t, s)
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if edition == config.Platform {
		if _, err := s.Store.AdjustCredits(context.Background(), u.ID, 100000, "fixture", "video-funding", "Fixture", u.ID); err != nil {
			t.Fatal(err)
		}
		status, _, body := request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"enabled": true, "paid_provider_requested": true, "gemini_enabled": true, "seedance_enabled": true, "agnes_route_enabled": true, "apimart_api_key": "fixture-video-key", "agnes_api_key": "fixture-agnes-key"})
		if status != 200 {
			t.Fatal("video config fixture", status, body)
		}
	} else {
		cipher, err := s.Vault.Encrypt(u.ID+"/personal-video", "fixture-personal-key")
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Store.PutProvider(context.Background(), store.Provider{ID: "personal-video", OwnerScope: u.ID, Name: "Personal video", Capability: "video", Protocol: "apimart_video", Profile: "apimart", BaseURL: "https://example.invalid", Model: "gemini-omni-flash-preview", Enabled: true, MaxConcurrency: 2, ExecutionMode: "http", TimeoutSeconds: 45, OfflineAction: "queue", LeaseSeconds: 120}, &cipher)
		if err != nil {
			t.Fatal(err)
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an API handler contacted the mock upstream")
		w.WriteHeader(500)
	}))
	t.Cleanup(upstream.Close)
	s.Config.AllowPrivateProviders = true
	scope := "platform"
	if edition == config.Personal {
		scope = u.ID
	}
	providers, err := s.Store.Providers(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range providers {
		p.BaseURL = upstream.URL
		if _, err := s.Store.PutProvider(context.Background(), p, nil); err != nil {
			t.Fatal(err)
		}
	}
	return s, token, u
}

func TestVideoRoutesIdempotentRegistration(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.registerVideoRoutes()
	s.registerVideoRoutes()
	for _, route := range []struct{ method, path, pattern string }{
		{"GET", "/api/videos/config", "GET /api/videos/config"},
		{"POST", "/api/videos/tasks", "POST /api/videos/tasks"},
		{"GET", "/api/videos/tasks", "GET /api/videos/tasks"},
		{"GET", "/api/videos/tasks/task-1", "GET /api/videos/tasks/{id}"},
		{"DELETE", "/api/videos/tasks/task-1", "DELETE /api/videos/tasks/{id}"},
		{"POST", "/api/videos/tasks/task-1/cancel", "POST /api/videos/tasks/{id}/cancel"},
		{"POST", "/api/videos/script-preview", "POST /api/videos/script-preview"},
		{"GET", "/api/admin/video-config", "GET /api/admin/video-config"},
		{"PATCH", "/api/admin/video-config", "PATCH /api/admin/video-config"},
		{"GET", "/api/admin/video-tasks", "GET /api/admin/video-tasks"},
		{"DELETE", "/api/admin/video-tasks/task-1", "DELETE /api/admin/video-tasks/{id}"},
		{"POST", "/api/public/video-webhooks/apimart", "POST /api/public/video-webhooks/apimart"},
		{"POST", "/api/public/video-webhooks/apimart/fixture/callback", "POST /api/public/video-webhooks/apimart/{secret}/callback"},
		{"GET", "/api/videos/references/asset-1/reference.png", "GET /api/videos/references/{id}/{filename}"},
	} {
		if _, pattern := s.mux.Handler(httptest.NewRequest(route.method, route.path, nil)); pattern != route.pattern {
			t.Fatal("video route missing after repeated registration", route.pattern, pattern)
		}
	}
}

func videoJSONRequest(t *testing.T, s *Server, token, key string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/videos/tasks", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func videoResponseData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatal("video response", w.Code, w.Body.String())
	}
	var envelope struct{ Data map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func videoInputFixture() map[string]any {
	return map[string]any{"prompt": "Show the reference product rotating slowly", "model_code": "gemini-omni", "aspect_ratio": "9:16", "resolution": "720p", "duration_seconds": 10, "image_urls": []string{"https://images.example.invalid/reference.png"}}
}

func videoScriptRequest(t *testing.T, s *Server, token, key string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/videos/script-preview", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func videoScriptFixture(prompt string) videoScriptInput {
	return videoScriptInput{Prompt: prompt, ModelCode: "seedance-mini", Duration: 8, Resolution: "480p", Ratio: "16:9", References: 2}
}

func TestPostgresVideoHTTPRiskBeforeAssetsChargesAndPreviewTasks(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	for _, keyword := range []string{"video-fixture-block", "Slowly rotate the uploaded product"} {
		if _, err := s.Store.AdminRiskKeyword(ctx, u.ID, "video-risk-fixture", "high", keyword, false); err != nil {
			t.Fatal(err)
		}
	}
	// A configured text provider must not cause a blocked preview to create work.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("risk-gated handler contacted a provider")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	projectProvider(t, s, u, "text", upstream.URL)
	for i, field := range []string{"prompt", "original_prompt"} {
		in := videoInputFixture()
		in["original_prompt"] = "Show the product in its original packaging"
		in[field] = "Show video-fixture-block content"
		for range 2 {
			w := videoJSONRequest(t, s, token, fmt.Sprintf("video-risk-field-%d", i), in)
			if w.Code != 422 || !strings.Contains(w.Body.String(), "CONTENT_BLOCKED") || strings.Contains(w.Body.String(), "video-fixture-block") {
				t.Fatal("original description or edited script bypassed screening", field, w.Code)
			}
		}
	}
	body, contentType := videoMultipartFixture(t)
	for range 2 {
		r := httptest.NewRequest("POST", "/api/videos/tasks", bytes.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", "video-risk-multipart")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatal("uploaded video reference escaped risk screening", w.Code)
		}
	}
	for _, key := range []string{"", "video-preview-risk-key"} {
		for range 2 {
			w := videoScriptRequest(t, s, token, key, videoScriptFixture("Show video-fixture-block content"))
			if w.Code != 422 || !strings.Contains(w.Body.String(), "CONTENT_BLOCKED") {
				t.Fatal("preview screening missing", w.Code)
			}
		}
	}
	var tasks, assets, charges, events, credits int
	if err := s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM assets),(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge'),(SELECT count(*) FROM admin_risk_events),(SELECT credits FROM users WHERE id=$1)`, u.ID).Scan(&tasks, &assets, &charges, &events, &credits); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || assets != 0 || charges != 0 || events != 5 || credits != 100000 {
		t.Fatal("risk gate persisted work, charged or duplicated events", tasks, assets, charges, events, credits)
	}
}

func TestPostgresVideoHTTPRiskRestrictionsAndWarningReplay(t *testing.T) {
	s, token, admin := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	threshold := 2
	if _, err := s.Store.AdminUpdateRiskConfig(ctx, admin.ID, "video-risk-threshold", store.AdminRiskPatch{BanThreshold: &threshold}); err != nil {
		t.Fatal(err)
	}
	for level, word := range map[string]string{"high": "video-fixture-block", "low": "video-fixture-warn"} {
		if _, err := s.Store.AdminRiskKeyword(ctx, admin.ID, "video-risk-keyword", level, word, false); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.Store.CreateUser(ctx, "video-risk-user@example.invalid", "fixture", "Video risk fixture")
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	in := videoInputFixture()
	in["prompt"] = "Show video-fixture-block content"
	for range 2 {
		if w := videoJSONRequest(t, s, userToken, "video-risk-user-create", in); w.Code != 422 {
			t.Fatal("risk gate must precede insufficient-credit rejection", w.Code)
		}
	}
	w := videoScriptRequest(t, s, userToken, "video-risk-user-preview", videoScriptFixture(in["prompt"].(string)))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "ACCOUNT_RESTRICTED") {
		t.Fatal("restriction did not propagate through the shared error contract", w.Code)
	}
	u, err = s.Store.UserByID(ctx, u.ID)
	if err != nil || u.Status != "disabled" {
		t.Fatal("automatic restriction not persisted", err)
	}
	in["prompt"] = "Show video-fixture-warn content"
	first := videoResponseData(t, videoJSONRequest(t, s, token, "video-warning-replay", in))
	if next := videoResponseData(t, videoJSONRequest(t, s, token, "video-warning-replay", in)); next["id"] != first["id"] {
		t.Fatal("warning retry created another video")
	}
	var events, charges int
	if err = s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_risk_events),(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge')`).Scan(&events, &charges); err != nil || events != 3 || charges != 1 {
		t.Fatal("warning/restriction retries duplicated events or credits", events, charges, err)
	}
}

func TestPostgresVideoScriptTemplateQueueAndMockExecution(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	if _, err := s.Store.AdminRiskKeyword(ctx, u.ID, "video-template-keyword", "high", "fixture-template-block", false); err != nil {
		t.Fatal(err)
	}
	name, scene, content := "Video script fixture", videoScriptScene, "fixture-template-block is an internal policy marker. Use gentle natural movement."
	template, err := s.Store.AdminSavePrompt(ctx, u.ID, "", "video-template-create", store.AdminPromptInput{Name: &name, Scene: &scene, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Messages []provider.Message }
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/chat/completions") || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 {
			t.Error("unexpected mock text request")
			w.WriteHeader(400)
			return
		}
		var system, user string
		_ = json.Unmarshal(request.Messages[0].Content, &system)
		_ = json.Unmarshal(request.Messages[1].Content, &user)
		var input map[string]any
		_ = json.Unmarshal([]byte(user), &input)
		if !strings.HasPrefix(system, content) || !strings.Contains(system, "Preserve the user's intent") || input["prompt"] != "Single shot, no cuts. Keep the exact blue packaging and quoted label." || input["mode"] != "single_shot" || input["duration_seconds"] != float64(8) || !strings.Contains(input["audio"].(string), "Do not generate") {
			t.Error("template or original user intent changed in the queued model input")
		}
		plan, _ := json.Marshal(map[string]any{"optimized_prompt": "Single continuous eight-second product shot preserving the exact blue packaging and quoted label.", "summary": strings.Repeat("x", 40), "mode": "storyboard"})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": string(plan)}}}})
	}))
	defer upstream.Close()
	projectProvider(t, s, u, "text", upstream.URL)
	in := videoScriptFixture("Single shot, no cuts. Keep the exact blue packaging and quoted label.")
	audio := false
	in.Audio = &audio
	firstResponse := videoScriptRequest(t, s, token, "", in)
	first := videoResponseData(t, firstResponse)
	id := first["task_id"].(string)
	if first["source"] != "fallback" || first["status"] != "queued" || calls.Load() != 0 || strings.Contains(firstResponse.Body.String(), "fixture-template-block") {
		t.Fatal("preview handler executed a model or exposed the system template")
	}
	changed := "Replacement internal template"
	if _, err = s.Store.AdminSavePrompt(ctx, u.ID, template.ID, "video-template-update", store.AdminPromptInput{Content: &changed}); err != nil {
		t.Fatal(err)
	}
	if replay := videoResponseData(t, videoScriptRequest(t, s, token, "", in)); replay["task_id"] != id {
		t.Fatal("template update changed the stable preview action")
	}
	task, err := s.Store.ClaimTaskCapabilities(ctx, "video-script-test", time.Minute, []string{"text"})
	if err != nil || task.ID != id || task.Kind != videoScriptScene || task.CreditsCost != 0 {
		t.Fatal("preview was not a zero-credit generic text task", err)
	}
	gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.CloseIdleConnections()
	s.processHTTPModelTask(ctx, task, gateway)
	for range 2 {
		result := videoResponseData(t, videoScriptRequest(t, s, token, "", in))
		if result["task_id"] != id || result["source"] != "ai" || result["status"] != "succeeded" || result["mode"] != "single_shot" || len(result["summary"].(string)) != 30 {
			t.Fatal("completed preview did not normalize the result")
		}
	}
	var tasks, events, charges int
	if err = s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM admin_risk_events),(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge')`).Scan(&tasks, &events, &charges); err != nil || tasks != 1 || events != 0 || charges != 0 || calls.Load() != 1 {
		t.Fatal("system template screened or preview resubmitted/charged", tasks, events, charges, calls.Load(), err)
	}
	disabled := false
	if _, err = s.Store.AdminSavePrompt(ctx, u.ID, template.ID, "video-template-disable", store.AdminPromptInput{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	result := videoResponseData(t, videoScriptRequest(t, s, token, "video-template-disabled", in))
	task, err = s.Store.Task(ctx, u.ID, result["task_id"].(string))
	if err != nil || !strings.Contains(string(task.Request), videoScriptSystem) || strings.Contains(string(task.Request), changed) {
		t.Fatal("disabled template did not restore the default", err)
	}
	in.Prompt = "Keep the user's distinct new camera direction"
	if w := videoScriptRequest(t, s, token, "video-template-disabled", in); w.Code != 409 {
		t.Fatal("changed preview input reused an existing action", w.Code)
	}
}

func TestPostgresVideoScriptFallbackPreservesLongInputAndPersonalScope(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Personal)
	ctx := context.Background()
	for _, prompt := range []string{strings.Repeat("x", 3996) + "tail", strings.Repeat("\U0001F600", 1998) + "tail"} {
		in := videoScriptInput{Prompt: prompt, ModelCode: "gemini-omni", Duration: 10, Resolution: "720p", Ratio: "9:16", References: 1}
		result := videoResponseData(t, videoScriptRequest(t, s, token, "", in))
		if result["optimized_prompt"] != prompt || len(utf16.Encode([]rune(result["optimized_prompt"].(string)))) != 4000 || result["source"] != "fallback" || result["task_id"] != nil {
			t.Fatal("fallback discarded original intent to fit generated instructions")
		}
	}
	// Platform risk/template rows are not a dependency of a personal instance.
	if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES('risk_control','{"enabled":true,"invalid":true}') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO admin_prompt_templates(id,name,scene,content,enabled) VALUES('video-personal-template','Fixture',$1,'platform-only template must not be used',true)`, videoScriptScene); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("personal preview handler called a provider")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	projectProvider(t, s, u, "text", upstream.URL)
	in := videoScriptInput{Prompt: "Preserve the original product and presentation", ModelCode: "gemini-omni", Duration: 10, Resolution: "720p", Ratio: "9:16", References: 1}
	result := videoResponseData(t, videoScriptRequest(t, s, token, "personal-script-key", in))
	task, err := s.Store.Task(ctx, u.ID, result["task_id"].(string))
	if err != nil || !strings.Contains(string(task.Request), videoScriptSystem) || strings.Contains(string(task.Request), "platform-only") || task.UserID != u.ID || task.CreditsCost != 0 {
		t.Fatal("personal preview inherited platform policy/template or cost", err)
	}
	videoInput := videoInputFixture()
	videoInput["prompt"] = "Show the product without changing it"
	if w := videoJSONRequest(t, s, token, "personal-risk-independent", videoInput); w.Code != 200 {
		t.Fatal("personal video depended on platform risk settings", w.Code)
	}
}

func TestPostgresVideoScriptInvalidAndUncertainOutputNeverResubmits(t *testing.T) {
	for _, test := range []struct {
		name, output string
		status       int
	}{
		{"malformed", "not JSON", 200},
		{"oversized", `{"optimized_prompt":"` + strings.Repeat("x", 4001) + `"}`, 200},
		{"ambiguous", "", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, token, u := videoHTTPFixture(t, config.Platform)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				if test.status == 200 {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": test.output}}}})
				}
			}))
			defer upstream.Close()
			projectProvider(t, s, u, "text", upstream.URL)
			in := videoScriptFixture("Show the same product rotating slowly")
			first := videoResponseData(t, videoScriptRequest(t, s, token, "video-script-output-key", in))
			ctx := context.Background()
			lease, err := s.Store.ClaimTaskCapabilities(ctx, "script-output", time.Minute, []string{"text"})
			if err != nil {
				t.Fatal(err)
			}
			gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
			if err != nil {
				t.Fatal(err)
			}
			defer gateway.CloseIdleConnections()
			s.processHTTPModelTask(ctx, lease, gateway)
			for range 2 {
				result := videoResponseData(t, videoScriptRequest(t, s, token, "video-script-output-key", in))
				if result["task_id"] != first["task_id"] || result["source"] != "fallback" || result["optimized_prompt"] != first["optimized_prompt"] || test.status == 503 && result["status"] != "uncertain" {
					t.Fatal("invalid/uncertain model output did not retain the original safe preview")
				}
			}
			if _, err := s.Store.ClaimTaskCapabilities(ctx, "script-repeat", time.Minute, []string{"text"}); !errors.Is(err, store.ErrNotFound) || calls.Load() != 1 {
				t.Fatal("preview replay resubmitted uncertain or malformed output", err, calls.Load())
			}
		})
	}
}

func TestPostgresVideoHTTPSubmitReplayCompileAndScopes(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	in := videoInputFixture()
	first := videoResponseData(t, videoJSONRequest(t, s, token, "video-json-key-0001", in))
	id := first["id"].(string)
	if first["credits_cost"] != float64(950) || first["status"] != "queued" {
		t.Fatal("server-side video pricing or status")
	}
	task, err := s.Store.Task(ctx, u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var payload jobs.Payload
	if json.Unmarshal(task.Request, &payload) != nil || payload.Validate("video") != nil || payload.Version != 1 || len(payload.Video) == 0 || strings.Contains(string(task.Request), "fixture-video-key") {
		t.Fatal("invalid internal job envelope")
	}
	req, p, err := s.CompileVideoTask(ctx, task)
	if err != nil || req.Prompt != in["prompt"] || len(req.SourceURLs) != 1 || req.Model != "gemini-omni-flash-preview" || p.OwnerScope != "platform" {
		t.Fatal("compiler lost input or scope", err)
	}
	status, _, _ := request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"model_configs": []any{map[string]any{"code": "gemini-omni", "enabled": true, "variants": []any{map[string]any{"resolution": "720p", "duration_seconds": 10, "price": 1000, "enabled": true}}}}})
	if status != 200 {
		t.Fatal("price change failed")
	}
	replay := videoResponseData(t, videoJSONRequest(t, s, token, "video-json-key-0001", in))
	if replay["id"] != id || replay["credits_cost"] != float64(950) {
		t.Fatal("replay changed task or charge")
	}
	in["prompt"] = "Different product movement"
	if w := videoJSONRequest(t, s, token, "video-json-key-0001", in); w.Code != 409 {
		t.Fatal("changed request replay accepted", w.Code)
	}
	var charges int
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE biz_id=$1 AND biz_type='generation_charge'", id).Scan(&charges); err != nil || charges != 1 {
		t.Fatal("duplicate charge", err)
	}
	other, err := s.Store.CreateUser(ctx, "video-other@example.invalid", "fixture", "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := s.Sessions.Issue(other.ID, other.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/videos/tasks/" + id, "/api/admin/video-config", "/api/admin/video-tasks"} {
		status, _, _ := request(t, s, "GET", endpoint, otherToken, nil)
		if status != 404 && status != 403 {
			t.Fatal("scope not enforced", endpoint, status)
		}
	}
	for _, key := range []string{"actual_cost", "actual_width", "cost_details", "secret_ciphertext"} {
		if _, exists := first[key]; exists {
			t.Fatal("private task field exposed", key)
		}
	}
	lease, err := s.Store.ClaimTaskCapabilities(ctx, "executor", time.Minute, []string{"video"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Store.ReserveAttempt(ctx, lease, "platform", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	newProvider := a.Provider
	newProvider.Model = "doubao-seedance-2.0"
	if _, err := s.Store.PutProvider(ctx, newProvider, nil); err != nil {
		t.Fatal(err)
	}
	if req, err := s.CompileVideoAttempt(ctx, lease, a); err != nil || req.Model != a.Provider.Model {
		t.Fatal("reservation snapshot not used", err)
	}
	if _, _, err := s.CompileVideoTask(ctx, lease); !errors.Is(err, store.ErrConflict) {
		t.Fatal("mutable lookup silently changed pinned model", err)
	}
}

func TestPostgresVideoHTTPConfigurationPreviewAndValidation(t *testing.T) {
	s, token, _ := videoHTTPFixture(t, config.Platform)
	status, envelope, body := request(t, s, "GET", "/api/admin/video-config", token, nil)
	if status != 200 || strings.Contains(body, "fixture-video-key") || strings.Contains(body, "fixture-agnes-key") || strings.Contains(body, s.videoWebhookSecret()) {
		t.Fatal("config exposes secrets")
	}
	data := envelope["data"].(map[string]any)
	if data["apimart_key_source"] != "admin" || data["apimart_key_error"] != false {
		t.Fatal("admin secret-state enum contract")
	}
	for _, key := range []string{"model_configs", "agnes_keys", "hfsy_mappings", "metrics", "recent_tasks"} {
		if data[key] == nil {
			t.Fatal("missing admin contract", key)
		}
	}
	status, envelope, _ = request(t, s, "GET", "/api/videos/config", token, nil)
	if status != 200 || len(envelope["data"].(map[string]any)["model_configs"].([]any)) != 5 {
		t.Fatal("public catalog contract")
	}
	for i, patch := range []map[string]any{
		{"unknown": true}, {"standard_price": 0}, {"agnes_route_percent": 26}, {"poll_seconds": 0},
		{"model_configs": []any{map[string]any{"code": "invented", "enabled": true}}},
	} {
		if status, _, _ := request(t, s, "PATCH", "/api/admin/video-config", token, patch); status != 400 {
			t.Fatal("invalid config accepted", i, status)
		}
	}
	status, envelope, body = request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"agnes_keys": []any{map[string]any{"id": "secondary", "label": "Secondary", "enabled": true, "concurrency": 2, "api_key": "fixture-pool-key"}}})
	if status != 200 || strings.Contains(body, "fixture-pool-key") {
		t.Fatal("key pool update", status)
	}
	p, err := s.Store.Provider(context.Background(), "platform", "video-agnes-secondary")
	if err != nil || !p.HasAPIKey || p.MaxConcurrency != 2 {
		t.Fatal("key pool not persisted", err)
	}
	secret, err := s.Vault.Decrypt("platform/video-agnes-secondary", p.SecretCiphertext)
	if err != nil || secret != "fixture-pool-key" {
		t.Fatal("key pool encryption binding", err)
	}
	for i, change := range []func(map[string]any){
		func(m map[string]any) { m["credits_cost"] = 1 },
		func(m map[string]any) { m["duration_seconds"] = 15 },
		func(m map[string]any) { m["resolution"] = "4k" },
		func(m map[string]any) { m["aspect_ratio"] = "1:1" },
		func(m map[string]any) { m["model_code"] = "invented" },
		func(m map[string]any) { m["image_urls"] = []string{} },
		func(m map[string]any) { m["image_urls"] = []string{"https://images.example.invalid/ref.webp"} },
		func(m map[string]any) { m["seed"] = -1 },
		func(m map[string]any) { m["prompt"] = "abc" },
	} {
		in := videoInputFixture()
		change(in)
		if w := videoJSONRequest(t, s, token, fmt.Sprintf("invalid-video-%04d", i), in); w.Code != 400 {
			t.Fatal("invalid request accepted", i, w.Code)
		}
	}
	if w := videoJSONRequest(t, s, token, "short", videoInputFixture()); w.Code != 400 {
		t.Fatal("invalid idempotency key")
	}
	preview := map[string]any{"prompt": "Shot 1: show the product. Shot 2: rotate it.", "model_code": "seedance-mini", "duration_seconds": 8, "resolution": "480p", "aspect_ratio": "16:9", "generate_audio": false, "reference_count": 2}
	status, envelope, _ = request(t, s, "POST", "/api/videos/script-preview", token, preview)
	if status != 200 {
		t.Fatal("preview rejected", status)
	}
	result := envelope["data"].(map[string]any)
	if result["source"] != "fallback" || result["mode"] != "storyboard" || !strings.Contains(result["optimized_prompt"].(string), "8 seconds") {
		t.Fatal("preview contract")
	}
	preview["prompt"] = "Single shot, no cuts. Shot 1: rotate the product."
	status, envelope, _ = request(t, s, "POST", "/api/videos/script-preview", token, preview)
	if status != 200 || envelope["data"].(map[string]any)["mode"] != "single_shot" {
		t.Fatal("single shot precedence")
	}
	var count int
	if err := s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks").Scan(&count); err != nil || count != 0 {
		t.Fatal("preview or validation created a task", err)
	}
}

func TestPostgresVideoHTTPPersonalPricingAndEnvelope(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Personal)
	data := videoResponseData(t, videoJSONRequest(t, s, token, "personal-video-key", videoInputFixture()))
	if data["credits_cost"] != float64(0) {
		t.Fatal("personal video charged")
	}
	task, err := s.Store.Task(context.Background(), u.ID, data["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := s.CompileVideoTask(context.Background(), task)
	if err != nil || p.OwnerScope != u.ID {
		t.Fatal("personal provider scope", err)
	}
	d, _ := decodeVideoTask(task)
	d.ProviderScope = "platform"
	raw, _ := json.Marshal(d)
	task.Request, _ = json.Marshal(jobs.Payload{Version: 1, Video: raw})
	if _, _, err := s.CompileVideoTask(context.Background(), task); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("personal compiler accepted platform credential scope", err)
	}
	if status, _, _ := request(t, s, "GET", "/api/admin/video-config", token, nil); status != 403 {
		t.Fatal("personal platform administration allowed")
	}
}

func TestPostgresVideoHTTPWebhookAuthenticationReconciliationAndDelete(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	data := videoResponseData(t, videoJSONRequest(t, s, token, "webhook-video-key", videoInputFixture()))
	id := data["id"].(string)
	lease, err := s.Store.ClaimTaskCapabilities(ctx, "executor", time.Minute, []string{"video"})
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := s.CompileVideoTask(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Store.ReserveAttempt(ctx, lease, p.OwnerScope, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.BeginVideoSubmission(ctx, lease, a); err != nil {
		t.Fatal(err)
	}
	callback := `{"data":[{"id":"mock-upstream","status":"completed","result":{"videos":[{"url":"https://cdn.example.invalid/video.mp4"}]}}]}`
	post := func(path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("X-Webhook-Secret", key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := post("/api/public/video-webhooks/apimart", "forged", callback=÷m¢G§²ÚîÆ­yÜ($$%™…¥°¡Ü°È°€ĞÀÌ°€‰=I	%8ˆ°€‰=İ¹•È…•ÍÌ¥ÌÉ•ÅÕ¥É•ˆ¤($$%É•ÑÕÉ¸($%ô($%¹•áĞ¡Ü°È¤(%ô¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É=ÁÑ¥½¹Ì ¤ÍÑ½É”¹]½É­•É=ÁÑ¥½¹Ìì(%É•ÑÕÉ¸ÍÑ½É”¹]½É­•É=ÁÑ¥½¹ÍíAÕ‰±¥	…Í•UI0èÌ¹½¹™¥œ¹AÕ‰±¥	…Í•UI0°M¥¹¥¹-•äèÌ¹½¹™¥œ¹]½É­•ÉM¥¹¥¹-•ä°É…•M•½¹‘ÌèÌ¹½¹™¥œ¹]½É­•ÉÉ…•M•½¹‘Ì°%¹ÁÕÑQQ1M•½¹‘ÌèÌ¹½¹™¥œ¹]½É­•É%¹ÁÕÑQQ1M•½¹‘Íô)ô)™Õ¹Œİ½É­•ÉAÉ¥¹¥Á…°¡È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ÍÑ½É”¹]½É­•ÉAÉ¥¹¥Á…°ì(%À°|€èôÈ¹½¹Ñ•áĞ ¤¹Y…±Õ”¡İ½É­•ÉAÉ¥¹¥Á…±-•åíô¤¸¡ÍÑ½É”¹]½É­•ÉAÉ¥¹¥Á…°¤(%É•ÑÕÉ¸À)ô((¼¼Q¡”Í¡…É•Á½±¥ä…•ÁÑÌQ1L½È…¸•áÁ±¥¥Ñ±äÑÉÕÍÑ•¥µµ•‘¥…Ñ”ÁÉ½áäÁ••È¸)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É!QQAL¡È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤‰½½°ìÉ•ÑÕÉ¸Ì¹Í•ÕÉ•I•ÅÕ•ÍĞ¡È¤ô()™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉÕÑ ¡¹•áĞ¡ÑÑÀ¹!…¹‘±•ÉÕ¹Œ¤¡ÑÑÀ¹!…¹‘±•ÉÕ¹Œì(%É•ÑÕÉ¸™Õ¹Œ¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì($%Ü¹!•…‘•È ¤¹M•Ğ ‰…¡”µ½¹ÑÉ½°ˆ°€‰¹¼µÍÑ½É”ˆ¤($%‘•™•È™Õ¹Œ ¤ì($$%¥˜É•½Ù•È ¤€„ô¹¥°ì($$$%Ì¹1½•È¹ÉÉ½È ‰İ½É­•ÈÉ•ÅÕ•ÍĞÁ…¹¥Œˆ°€‰É•ÅÕ•ÍÑ}¥ˆ°É•ÅÕ•ÍÑ%¡È¤¤($$$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰]=I-I}%9QI91}II=Hˆ¤¤($$%ô($%ô ¤($%¥˜€…Ì¹İ½É­•É!QQAL¡È¤ì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰!QQAM}IEU%Iˆ¤¤($$%É•ÑÕÉ¸($%ô($%¡•…‘•ÉÌ€èôÈ¹!•…‘•È¹Y…±Õ•Ì ‰ÕÑ¡½É¥é…Ñ¥½¸ˆ¤($%¥˜±•¸¡¡•…‘•ÉÌ¤€ôô€Àì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰]=I-I}Q=-9}5%MM%9ˆ¤¤($$%É•ÑÕÉ¸($%ô($%¥˜±•¸¡¡•…‘•ÉÌ¤€„ô€Äì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰]=I-I}Q=-9}%9Y1%ˆ¤¤($$%É•ÑÕÉ¸($%ô($%Í¡•µ”°Ñ½­•¸°½¬€èôÍÑÉ¥¹Ì¹ÕĞ¡¡•…‘•ÉÍlÁt°€ˆ€ˆ¤($%¥˜€…½¬ñğ€…ÍÑÉ¥¹Ì¹ÅÕ…±½±¡Í¡•µ”°€‰	•…É•Èˆ¤ñğÑ½­•¸€ôô€ˆˆñğÍÑÉ¥¹Ì¹½¹Ñ…¥¹Í¹ä¡Ñ½­•¸°€ˆqÑqÉq¸°ˆ¤ì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰]=I-I}Q=-9}%9Y1%ˆ¤¤($$%É•ÑÕÉ¸($%ô($%À°•ÉÈ€èôÌ¹MÑ½É”¹ÕÑ¡•¹Ñ¥…Ñ•]½É­•ÉQ½­•¸¡È¹½¹Ñ•áĞ ¤°Ñ½­•¸°Ì¹½¹™¥œ¹]½É­•ÉA•ÁÁ•È¤($%¥˜•ÉÈ€„ô¹¥°ì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($$%É•ÑÕÉ¸($%ô($%¹•áĞ¡Ü°È¹]¥Ñ¡½¹Ñ•áĞ¡½¹Ñ•áĞ¹]¥Ñ¡Y…±Õ”¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…±-•åíô°À¤¤¤(%ô)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉÉÉ½È¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ°•ÉÈ•ÉÉ½È¤ì(%Ù…ÈÁÉ½Ñ½½°€©İÀ¹AÉ½Ñ½½±ÉÉ½È(%¥˜€…•ÉÉ½ÉÌ¹Ì¡•ÉÈ°€™ÁÉ½Ñ½½°¤ì($%Íİ¥Ñ ì($%…Í”•ÉÉ½ÉÌ¹%Ì¡•ÉÈ°ÍÑ½É…”¹ÉÉ%¹Ù…±¥‘%µ…”¤è($$%ÁÉ½Ñ½½°€ôİÀ¹9•İÉÉ½È ‰IQ%Q}%9Y1%ˆ¤($%…Í”•ÉÉ½ÉÌ¹%Ì¡•ÉÈ°ÍÑ½É…”¹ÉÉ%µ…•Q½½1…É”¤è($$%ÁÉ½Ñ½½°€ôİÀ¹9•İÉÉ½È ‰IQ%Q}%9Y1%ˆ¤($%‘•™…Õ±Ğè($$%Ì¹1½•È¹ÉÉ½È ‰İ½É­•È½Á•É…Ñ¥½¸™…¥±•ˆ°€‰É•ÅÕ•ÍÑ}¥ˆ°É•ÅÕ•ÍÑ%¡È¤°€‰ÍÅ±ÍÑ…Ñ”ˆ°ÍÑ½É”¹ME1MÑ…Ñ”¡•ÉÈ¤¤($$%ÁÉ½Ñ½½°€ôİÀ¹9•İÉÉ½È ‰]=I-I}%9QI91}II=Hˆ¤($%ô(%ô(%İÉ¥Ñ•)M=8¡Ü°ÁÉ½Ñ½½°¹!QQAMÑ…ÑÕÌ°ÁÉ½Ñ½½°¹I•ÍÁ½¹Í” ¤¤)ô)™Õ¹ŒÉ•…‘]½É­•É)M=9mPİÀ¹5•ÍÍ…•t¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤€¡P°•ÉÉ½È¤ì(%Ù…Èé•É¼P(%µ•‘¥„°|°•ÉÈ€èôµ¥µ”¹A…ÉÍ•5•‘¥…QåÁ”¡È¹!•…‘•È¹•Ğ ‰½¹Ñ•¹ĞµQåÁ”ˆ¤¤(%¥˜•ÉÈ€„ô¹¥°ñğµ•‘¥„€„ô€‰…ÁÁ±¥…Ñ¥½¸½©Í½¸ˆì($%É•ÑÕÉ¸é•É¼°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤(%ô(%È¹	½‘ä€ô¡ÑÑÀ¹5…á	åÑ•ÍI•…‘•È¡Ü°È¹	½‘ä°İÀ¹5…áI•ÅÕ•ÍÑ	åÑ•Ì¬àÄäÈ¤(%É…Ü°•ÉÈ€èô¥¼¹I•…‘±°¡È¹	½‘ä¤(%¥˜•ÉÈ€„ô¹¥°ì($%É•ÑÕÉ¸é•É¼°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤(%ô(%É•ÑÕÉ¸İÀ¹•½‘•mQt¡É…Ü¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É!•…ÉÑ‰•…Ğ¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥¸°•ÉÈ€èôÉ•…‘]½É­•É)M=9mİÀ¹!•…ÉÑ‰•…ÑI•ÅÕ•ÍÑt¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹]½É­•É!•…ÉÑ‰•…Ğ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°¥¸¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É±…¥´¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥¸°•ÉÈ€èôÉ•…‘]½É­•É)M=9mİÀ¹±…¥µI•ÅÕ•ÍÑt¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%ÍÑ…ÉĞ€èôÑ¥µ”¹9½Ü ¤(%‘•…‘±¥¹”€èôÍÑ…ÉĞ¹‘¡Ñ¥µ”¹ÕÉ…Ñ¥½¸¡¥¸¹]…¥ÑM•½¹‘Ì¤€¨Ñ¥µ”¹M•½¹¤(%™½Èì($%Ñ…Í¬°•ÉÈ€èôÌ¹MÑ½É”¹±…¥µ]½É­•É)½ˆ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°¥¸°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤¤($%¥˜•ÉÈ€„ô¹¥°ì($$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($$%É•ÑÕÉ¸($%ô($%¹½Ü€èôÑ¥µ”¹9½Ü ¤($%¥˜Ñ…Í¬€„ô¹¥°ñğ€…¹½Ü¹	•™½É”¡‘•…‘±¥¹”¤ì($$%É•ÍÁ½¹Í”°•ÉÈ€èôİÀ¹9•İ±…¥µI•ÍÁ½¹Í”¡Ñ…Í¬°¹½Ü¹UQ ¤°¹½Ü¹MÕˆ¡ÍÑ…ÉĞ¤¹5¥±±¥Í•½¹‘Ì ¤¤($$%¥˜•ÉÈ€„ô¹¥°ì($$$%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($$$%É•ÑÕÉ¸($$%ô($$%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°É•ÍÁ½¹Í”¤($$%É•ÑÕÉ¸($%ô($%İ…¥Ğ€èôµ¥¸ ÔÀÀ©Ñ¥µ”¹5¥±±¥Í•½¹°Ñ¥µ”¹U¹Ñ¥°¡‘•…‘±¥¹”¤¤($%Ñ¥µ•È€èôÑ¥µ”¹9•İQ¥µ•È¡İ…¥Ğ¤($%Í•±•Ğì($%…Í”€ğµÈ¹½¹Ñ•áĞ ¤¹½¹” ¤è($$%Ñ¥µ•È¹MÑ½À ¤($$%É•ÑÕÉ¸($%…Í”€ğµÑ¥µ•È¹è($%ô(%ô)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉI•¹•Ü¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥¸°•ÉÈ€èôÉ•…‘]½É­•É)M=9mİÀ¹1•…Í•I•ÅÕ•ÍÑt¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹I•¹•İ]½É­•É)½ˆ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°È¹A…Ñ¡Y…±Õ” ‰©½‰%ˆ¤°¥¸°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É½µÁ±•Ñ”¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥¸°•ÉÈ€èôÉ•…‘]½É­•É)M=9mİÀ¹½µÁ±•Ñ•I•ÅÕ•ÍÑt¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹½µÁ±•Ñ•]½É­•É)½ˆ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°È¹A…Ñ¡Y…±Õ” ‰©½‰%ˆ¤°¥¸°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É…¥°¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥¸°•ÉÈ€èôÉ•…‘]½É­•É)M=9mİÀ¹…¥±I•ÅÕ•ÍÑt¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹…¥±]½É­•É)½ˆ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°È¹A…Ñ¡Y…±Õ” ‰©½‰%ˆ¤°¥¸°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°½ÕĞ¤)ô()™Õ¹ŒÉ•…‘]½É­•ÉÉÑ¥™…Ğ¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤€¡İÀ¹ÉÑ¥™…Ñ½É´°mu‰åÑ”°ÍÑÉ¥¹œ°•ÉÉ½È¤ì(%é•É¼€èôİÀ¹ÉÑ¥™…Ñ½Éµíô(%µ•‘¥„°|°•ÉÈ€èôµ¥µ”¹A…ÉÍ•5•‘¥…QåÁ”¡È¹!•…‘•È¹•Ğ ‰½¹Ñ•¹ĞµQåÁ”ˆ¤¤(%¥˜•ÉÈ€„ô¹¥°ñğµ•‘¥„€„ô€‰µÕ±Ñ¥Á…ÉĞ½™½É´µ‘…Ñ„ˆì($%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤(%ô(%È¹	½‘ä€ô¡ÑÑÀ¹5…á	åÑ•ÍI•…‘•È¡Ü°È¹	½‘ä°İÀ¹•™…Õ±Ñ5…áÉÑ¥™…Ñ	åÑ•Ì¬ØĞ¨ÄÀÈĞ¤(%É•…‘•È°•ÉÈ€èôÈ¹5Õ±Ñ¥Á…ÉÑI•…‘•È ¤(%¥˜•ÉÈ€„ô¹¥°ì($%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤(%ô(%™¥•±‘Ì€èôµ…ÁmÍÑÉ¥¹umuÍÑÉ¥¹íô(%™¥±•Ì€èômuÍÑÉ¥¹íô(%Ù…È‘…Ñ„mu‰åÑ”(%™¥±•¹…µ”€èô€ˆˆ(%½¹Ñ•¹ÑQåÁ”€èô€ˆˆ(%Á…ÉÑÌ€èô€À(%™½Èì($%Á…ÉĞ°•ÉÈ€èôÉ•…‘•È¹9•áÑA…ÉĞ ¤($%¥˜•ÉÉ½ÉÌ¹%Ì¡•ÉÈ°¥¼¹=¤ì($$%‰É•…¬($%ô($%¥˜•ÉÈ€„ô¹¥°ì($$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IQ%Q}I}%1ˆ¤($%ô($%Á…ÉÑÌ¬¬($%¥˜Á…ÉÑÌ€ø€Ôì($$%Á…ÉĞ¹±½Í” ¤($$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤($%ô($%¹…µ”€èôÁ…ÉĞ¹½Éµ9…µ” ¤($%¥˜Á…ÉĞ¹¥±•9…µ” ¤€„ô€ˆˆì($$%¥˜¹…µ”€„ô€‰™¥±”ˆñğ±•¸¡™¥±•Ì¤€ø€Àì($$$%Á…ÉĞ¹±½Í” ¤($$$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤($$%ô($$%™¥±•Ì€ô…ÁÁ•¹¡™¥±•Ì°¹…µ”¤($$%™¥±•¹…µ”€ôÁ…ÉĞ¹¥±•9…µ” ¤($$%½¹Ñ•¹ÑQåÁ”€ôÁ…ÉĞ¹!•…‘•È¹•Ğ ‰½¹Ñ•¹ĞµQåÁ”ˆ¤($$%‘…Ñ„°•ÉÈ€ô¥¼¹I•…‘±°¡¥¼¹1¥µ¥ÑI•…‘•È¡Á…ÉĞ°İÀ¹•™…Õ±Ñ5…áÉÑ¥™…Ñ	åÑ•Ì¬Ä¤¤($$%Á…ÉĞ¹±½Í” ¤($$%¥˜•ÉÈ€„ô¹¥°ì($$$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IQ%Q}I}%1ˆ¤($$%ô($$%¥˜¥¹ĞØĞ¡±•¸¡‘…Ñ„¤¤€øİÀ¹•™…Õ±Ñ5…áÉÑ¥™…Ñ	åÑ•Ìì($$$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IQ%Q}%9Y1%ˆ¤($$%ô($%ô•±Í”ì($$%Íİ¥Ñ ¹…µ”ì($$%…Í”€‰İ½É­•É%ˆ°€‰±•…Í•%ˆ°€‰¥‘•µÁ½Ñ•¹å-•äˆ°€‰Í¡„ÈÔØˆè($$%‘•™…Õ±Ğè($$$%Á…ÉĞ¹±½Í” ¤($$$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤($$%ô($$%Ø°•ÉÈ€èô¥¼¹I•…‘±°¡¥¼¹1¥µ¥ÑI•…‘•È¡Á…ÉĞ°€ÔÄÌ¤¤($$%Á…ÉĞ¹±½Í” ¤($$%¥˜•ÉÈ€„ô¹¥°ñğ±•¸¡Ø¤€ø€ÔÄÈì($$$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IEUMQ}M!5}%9Y1%ˆ¤($$%ô($$%™¥•±‘Ím¹…µ•t€ô…ÁÁ•¹¡™¥•±‘Ím¹…µ•t°ÍÑÉ¥¹œ¡Ø¤¤($%ô(%ô(%™½É´°•ÉÈ€èôİÀ¹Y…±¥‘…Ñ•5Õ±Ñ¥Á…ÉÑ¥•±‘Ì¡™¥•±‘Ì°™¥±•Ì¤(%¥˜•ÉÈ€„ô¹¥°ì($%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°•ÉÈ(%ô(%¥˜•ÉÈ€ôİÀ¹Y•É¥™åÉÑ¥™…ÑM½ÕÉ”¡™½É´¹M!ÈÔØ°‘…Ñ„°İÀ¹•™…Õ±Ñ5…áÉÑ¥™…Ñ	åÑ•Ì¤ì•ÉÈ€„ô¹¥°ì($%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°•ÉÈ(%ô(%‘•Ñ•Ñ•€èô¡ÑÑÀ¹•Ñ•Ñ½¹Ñ•¹ÑQåÁ”¡‘…Ñ„¤(%¥˜‘•Ñ•Ñ•€„ô€‰¥µ…”½Á¹œˆ€˜˜‘•Ñ•Ñ•€„ô€‰¥µ…”½©Á•œˆ€˜˜‘•Ñ•Ñ•€„ô€‰¥µ…”½İ•‰Àˆì($%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IQ%Q}5%5}U9MUAA=IQˆ¤(%ô(%¥˜½¹Ñ•¹ÑQåÁ”€„ô€ˆˆì($%µ•‘¥„°|°•ÉÈ€èôµ¥µ”¹A…ÉÍ•5•‘¥…QåÁ”¡½¹Ñ•¹ÑQåÁ”¤($%¥˜•ÉÈ€„ô¹¥°ñğ€¡µ•‘¥„€„ô€‰…ÁÁ±¥…Ñ¥½¸½½Ñ•ĞµÍÑÉ•…´ˆ€˜˜µ•‘¥„€„ô‘•Ñ•Ñ•¤ì($$%É•ÑÕÉ¸é•É¼°¹¥°°€ˆˆ°İÀ¹9•İÉÉ½È ‰IQ%Q}5%5}U9MUAA=IQˆ¤($%ô(%ô(%É•ÑÕÉ¸™½É´°‘…Ñ„°™¥±•¹…µ”°¹¥°)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉÉÑ¥™…Ğ¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%™½É´°Í½ÕÉ”°™¥±•¹…µ”°•ÉÈ€èôÉ•…‘]½É­•ÉÉÑ¥™…Ğ¡Ü°È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%Ù…ÈÉ•…Ñ•ÍÑ½É”¹ÍÍ•Ğ(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹M…Ù•]½É­•ÉÉÑ¥™…Ğ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°È¹A…Ñ¡Y…±Õ” ‰©½‰%ˆ¤°™½É´°Í½ÕÉ”°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤°™Õ¹Œ¡©½ˆÍÑ½É”¹]½É­•É)½ˆ¤€¡ÍÑ½É”¹ÍÍ•Ğ°•ÉÉ½È¤ì($%Ù…È•ÉÈ•ÉÉ½È($%É•…Ñ•°•ÉÈ€ôÌ¹Í…Ù•%µ…”¡È¹½¹Ñ•áĞ ¤°©½ˆ¹UÍ•É%°©½ˆ¹AÉ½©•Ñ%°©½ˆ¹Q…Í­%°€‰İ½É­•Èµ…¹‘¥‘…Ñ”ˆ°™¥±•¹…µ”°‰åÑ•Ì¹9•İI•…‘•È¡Í½ÕÉ”¤¤($%É•ÑÕÉ¸É•…Ñ•°•ÉÈ(%ô¤(%¥˜É•…Ñ•¹%€„ô€ˆˆ€˜˜€¡•ÉÈ€„ô¹¥°ñğ½ÕĞ¹%‘•µÁ½Ñ•¹Ğ¤ì($%Ñà°…¹•°€èô½¹Ñ•áĞ¹]¥Ñ¡Q¥µ•½ÕĞ¡½¹Ñ•áĞ¹	…­É½Õ¹ ¤°€Ô©Ñ¥µ”¹M•½¹¤($%É•µ½Ù•°±•…¹ÕÁÉÈ€èôÌ¹MÑ½É”¹¥Í…É‘]½É­•ÉÉÑ¥™…ÑÍÍ•Ğ¡Ñà°É•…Ñ•¹%¤($%…¹•° ¤($%¥˜±•…¹ÕÁÉÈ€ôô¹¥°€˜˜É•µ½Ù•ì($$%|€ôÌ¹¥±•Ì¹I•µ½Ù”¡É•…Ñ•¹MÑ½É…•-•ä¤($%ô(%ô(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%İÉ¥Ñ•)M=8¡Ü°€ÈÀÀ°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É%¹ÁÕĞ¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%Ä°•ÉÈ€èôÕÉ°¹A…ÉÍ•EÕ•Éä¡È¹UI0¹I…İEÕ•Éä¤(%¥˜•ÉÈ€„ô¹¥°ñğ±•¸¡Ä¤€„ô€Ìñğ±•¸¡Ål‰İ½É­•É%‰t¤€„ô€Äñğ±•¸¡Ål‰•áÁ¥É•Ì‰t¤€„ô€Äñğ±•¸¡Ål‰Í¥¹…ÑÕÉ”‰t¤€„ô€Äì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰%9AUQ}M%9QUI}%9Y1%ˆ¤¤($%É•ÑÕÉ¸(%ô(%¥¹‘•à°•ÉÈ€èôÍÑÉ½¹Ø¹Ñ½¤¡È¹A…Ñ¡Y…±Õ” ‰¥¹‘•àˆ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰%9AUQ}9=Q}=U9ˆ¤¤($%É•ÑÕÉ¸(%ô(%•áÁ¥É•Ì°•ÉÈ€èôÍÑÉ½¹Ø¹A…ÉÍ•%¹Ğ¡Ä¹•Ğ ‰•áÁ¥É•Ìˆ¤°€ÄÀ°€ØĞ¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰%9AUQ}M%9QUI}%9Y1%ˆ¤¤($%É•ÑÕÉ¸(%ô(%„°•ÉÈ€èôÌ¹MÑ½É”¹]½É­•É%¹ÁÕÑÍÍ•Ğ¡È¹½¹Ñ•áĞ ¤°İ½É­•ÉAÉ¥¹¥Á…°¡È¤°È¹A…Ñ¡Y…±Õ” ‰©½‰%ˆ¤°Ä¹•Ğ ‰İ½É­•É%ˆ¤°¥¹‘•à°•áÁ¥É•Ì°Ä¹•Ğ ‰Í¥¹…ÑÕÉ”ˆ¤°Ì¹İ½É­•É=ÁÑ¥½¹Ì ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%˜°•ÉÈ€èôÌ¹¥±•Ì¹=Á•¸¡„¹MÑ½É…•-•ä¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•ÉÉÉ½È¡Ü°È°İÀ¹9•İÉÉ½È ‰%9AUQ}9=Q}=U9ˆ¤¤($%É•ÑÕÉ¸(%ô(%‘•™•È˜¹±½Í” ¤(%Ü¹!•…‘•È ¤¹M•Ğ ‰½¹Ñ•¹ĞµQåÁ”ˆ°„¹½¹Ñ•¹ÑQåÁ”¤(%Ü¹!•…‘•È ¤¹M•Ğ ‰½¹Ñ•¹Ğµ¥ÍÁ½Í¥Ñ¥½¸ˆ°µ¥µ”¹½Éµ…Ñ5•‘¥…QåÁ” ‰¥¹±¥¹”ˆ°µ…ÁmÍÑÉ¥¹uÍÑÉ¥¹ì‰™¥±•¹…µ”ˆè„¹=É¥¥¹…±¥±•¹…µ•ô¤¤(%Ü¹!•…‘•È ¤¹M•Ğ ‰½¹Ñ•¹ĞµM•ÕÉ¥ÑäµA½±¥äˆ°€‰‘•™…Õ±ĞµÍÉŒ€¹½¹”œìÍ…¹‘‰½àˆ¤(%Ü¹!•…‘•È ¤¹M•Ğ ‰…¡”µ½¹ÑÉ½°ˆ°€‰¹¼µÍÑ½É”ˆ¤(%¡ÑÑÀ¹M•ÉÙ•½¹Ñ•¹Ğ¡Ü°È°„¹=É¥¥¹…±¥±•¹…µ”°„¹É•…Ñ•‘Ğ°˜¤)ô()™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉM½Á”¡È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ÍÑÉ¥¹œì(%¥˜Ì¹½¹™¥œ¹‘¥Ñ¥½¸€ôô½¹™¥œ¹A•ÉÍ½¹…°ì($%É•ÑÕÉ¸ÕÉÉ•¹ÑUÍ•È¡È¤¹%(%ô(%É•ÑÕÉ¸€‰Á±…Ñ™½É´ˆ)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É‘µ¥¹ÉÉ½È¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ°•ÉÈ•ÉÉ½È¤ì(%Ù…ÈÀ€©İÀ¹AÉ½Ñ½½±ÉÉ½È(%¥˜•ÉÉ½ÉÌ¹Ì¡•ÉÈ°€™À¤ì($%™…¥°¡Ü°È°À¹!QQAMÑ…ÑÕÌ°À¹•Ñ…¥°¹½‘”°À¹•Ñ…¥°¹5•ÍÍ…”¤($%É•ÑÕÉ¸(%ô(%Ì¹‘…Ñ…‰…Í•ÉÉ½È¡Ü°È°•ÉÈ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•É‘µ¥¹MÑ…Ñ”¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%ÍÑ…Ñ”°•ÉÈ€èôÌ¹MÑ½É”¹]½É­•É‘µ¥¹MÑ…Ñ”¡È¹½¹Ñ•áĞ ¤°Ì¹İ½É­•ÉM½Á”¡È¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•É‘µ¥¹ÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%ÍÑ…Ñ•l‰Ñ½­•¹}Á•ÁÁ•É}½¹™¥ÕÉ•‰t€ô±•¸¡Ì¹½¹™¥œ¹]½É­•ÉA•ÁÁ•È¤€øô€ÌÈ(%ÍÑ…Ñ•l‰Í¥¹¥¹}Í•É•Ñ}½¹™¥ÕÉ•‰t€ô±•¸¡Ì¹½¹™¥œ¹]½É­•ÉM¥¹¥¹-•ä¤€øô€ÌÈ(%½¬¡Ü°È°ÍÑ…Ñ”¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉQ½­•¹É•…Ñ”¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥˜€…Ì¹İ½É­•É!QQAL¡È¤ì($%™…¥°¡Ü°È°€ĞÈØ°€‰!QQAM}IEU%Iˆ°€‰]½É­•ÈÑ½­•¸¥ÍÍÕ…¹”É•ÅÕ¥É•Ì!QQALˆ¤($%É•ÑÕÉ¸(%ô(%Ù…È¥¹ÁÕĞÍÑÉÕĞì($%9…µ”ÍÑÉ¥¹œ©Í½¸è‰¹…µ”‰€(%ô(%¥˜€…‘•½‘”¡Ü°È°€™¥¹ÁÕĞ¤ì($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹É•…Ñ•]½É­•ÉQ½­•¸¡È¹½¹Ñ•áĞ ¤°Ì¹İ½É­•ÉM½Á”¡È¤°ÕÉÉ•¹ÑUÍ•È¡È¤¹%°¥¹ÁÕĞ¹9…µ”°Ì¹½¹™¥œ¹]½É­•ÉA•ÁÁ•È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•É‘µ¥¹ÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½¬¡Ü°È°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉQ½­•¹I••¹•É…Ñ”¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%¥˜€…Ì¹İ½É­•É!QQAL¡È¤ì($%™…¥°¡Ü°È°€ĞÈØ°€‰!QQAM}IEU%Iˆ°€‰]½É­•ÈÑ½­•¸¥ÍÍÕ…¹”É•ÅÕ¥É•Ì!QQALˆ¤($%É•ÑÕÉ¸(%ô(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹I••¹•É…Ñ•]½É­•ÉQ½­•¸¡È¹½¹Ñ•áĞ ¤°Ì¹İ½É­•ÉM½Á”¡È¤°È¹A…Ñ¡Y…±Õ” ‰Ñ½­•¹%ˆ¤°Ì¹½¹™¥œ¹]½É­•ÉA•ÁÁ•È¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•É‘µ¥¹ÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½¬¡Ü°È°½ÕĞ¤)ô)™Õ¹Œ€¡Ì€©M•ÉÙ•È¤İ½É­•ÉQ½­•¹I•Ù½­”¡Ü¡ÑÑÀ¹I•ÍÁ½¹Í•]É¥Ñ•È°È€©¡ÑÑÀ¹I•ÅÕ•ÍĞ¤ì(%½ÕĞ°•ÉÈ€èôÌ¹MÑ½É”¹I•Ù½­•]½É­•ÉQ½­•¸¡È¹½¹Ñ•áĞ ¤°Ì¹İ½É­•ÉM½Á”¡È¤°È¹A…Ñ¡Y…±Õ” ‰Ñ½­•¹%ˆ¤¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ì¹İ½É­•É‘µ¥¹ÉÉ½È¡Ü°È°•ÉÈ¤($%É•ÑÕÉ¸(%ô(%½¬¡Ü°È°½ÕĞ¤)ô(