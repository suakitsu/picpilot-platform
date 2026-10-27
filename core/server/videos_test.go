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

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
	"github.com/suakitsu/picpilot-platform-go/core/video"
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
	if w := post("/api/public/video-webhooks/apimart", "forged", callback); w.Code != 401 {
		t.Fatal("forged callback accepted")
	}
	if w := post("/api/public/video-webhooks/apimart", s.videoWebhookSecret(), strings.Repeat(" ", (1<<20)+1)); w.Code != 413 {
		t.Fatal("oversized callback accepted", w.Code)
	}
	if w := post("/api/public/video-webhooks/apimart", s.videoWebhookSecret(), `{}`); w.Code != 400 {
		t.Fatal("malformed callback accepted", w.Code)
	}
	url, _ := url.Parse(s.VideoWebhookURL() + "/callback")
	if data := videoResponseData(t, post(url.Path, "", callback)); data["applied"] != false {
		t.Fatal("early callback must wait for acknowledgement")
	}
	if _, err := s.Store.RecordVideoSubmission(ctx, lease, a.ID, video.Submission{Provider: video.Gemini, Model: a.Model, UpstreamTaskID: "mock-upstream"}, 0); err != nil {
		t.Fatal(err)
	}
	if data := videoResponseData(t, post("/api/public/video-webhooks/apimart", s.videoWebhookSecret(), callback)); data["applied"] != true {
		t.Fatal("known callback not reconciled")
	}
	status, result, body := request(t, s, "GET", "/api/videos/tasks/"+id, token, nil)
	if status != 200 || result["data"].(map[string]any)["result_url"] != "" || strings.Contains(body, "cdn.example.invalid") {
		t.Fatal("unpersisted callback URL published")
	}
	if status, _, _ := request(t, s, "DELETE", "/api/videos/tasks/"+id, token, nil); status != 409 {
		t.Fatal("active task hidden")
	}
	if status, _, _ := request(t, s, "POST", "/api/videos/tasks/"+id+"/cancel", token, map[string]any{}); status != 200 {
		t.Fatal("cancel rejected")
	}
	v, err := s.Store.VideoTask(ctx, u.ID, id)
	if err != nil || v.Status != "polling" || v.CancelRequestedAt == nil || v.RefundedAt != nil {
		t.Fatal("inflight cancellation settled incorrectly", err)
	}
	queued := videoResponseData(t, videoJSONRequest(t, s, token, "delete-video-key-1", videoInputFixture()))["id"].(string)
	request(t, s, "POST", "/api/videos/tasks/"+queued+"/cancel", token, map[string]any{})
	if status, _, _ := request(t, s, "DELETE", "/api/videos/tasks/"+queued, token, nil); status != 200 {
		t.Fatal("terminal hide failed")
	}
	if status, _, _ := request(t, s, "GET", "/api/videos/tasks/"+queued, token, nil); status != 404 {
		t.Fatal("hidden task visible")
	}
	status, result, _ = request(t, s, "GET", "/api/admin/video-tasks?keyword="+queued+"&status=cancelled&page=1&page_size=1", token, nil)
	if status != 200 || result["data"].(map[string]any)["total"] != float64(1) {
		t.Fatal("admin filtered history failed")
	}
	if status, _, _ := request(t, s, "DELETE", "/api/admin/video-tasks/"+queued, token, nil); status != 200 {
		t.Fatal("admin soft deletion failed")
	}
	if replay := videoResponseData(t, videoJSONRequest(t, s, token, "delete-video-key-1", videoInputFixture())); replay["id"] != queued {
		t.Fatal("deleted task resubmitted")
	}
}

func videoMultipartFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	var pngBytes, body bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	m := multipart.NewWriter(&body)
	for key, value := range map[string]string{"prompt": "Slowly rotate the uploaded product", "model_code": "gemini-omni", "duration_seconds": "10", "resolution": "720p"} {
		if err := m.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := m.CreateFormFile("images", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), m.FormDataContentType()
}

func TestPostgresVideoHTTPConcurrentMultipartReferencesAndCleanup(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	body, contentType := videoMultipartFixture(t)
	var failures atomic.Int32
	var wg sync.WaitGroup
	ids := make(chan string, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/videos/tasks", bytes.NewReader(body))
			r.Header.Set("Content-Type", contentType)
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Idempotency-Key", "concurrent-multipart-key")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			var envelope struct{ Data struct{ ID string } }
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
				failures.Add(1)
				return
			}
			ids <- envelope.Data.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("concurrent upload created duplicate task")
		}
		id = got
	}
	if failures.Load() != 0 || id == "" {
		t.Fatal("concurrent multipart rejected", failures.Load())
	}
	task, err := s.Store.Task(ctx, u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	compiled, _, err := s.CompileVideoTask(ctx, task)
	if err != nil || len(compiled.SourceURLs) != 1 || !strings.Contains(compiled.SourceURLs[0], "reference.png?") {
		t.Fatal("owned reference not compiled for Gemini", err)
	}
	r := httptest.NewRequest("GET", compiled.SourceURLs[0], nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/png") {
		t.Fatal("signed video input alias failed", w.Code)
	}
	var assets, charges int
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM assets WHERE user_id=$1", u.ID).Scan(&assets); err != nil || assets != 1 {
		t.Fatal("concurrent upload left orphan assets", assets, err)
	}
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE biz_id=$1 AND biz_type='generation_charge'", id).Scan(&charges); err != nil || charges != 1 {
		t.Fatal("concurrent upload charged twice", err)
	}
	if _, err := s.Store.Pool.Exec(ctx, "UPDATE users SET credits=0 WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "/api/videos/tasks", bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", "insufficient-upload-key")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 402 {
		t.Fatal("insufficient balance accepted", w.Code)
	}
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM assets WHERE user_id=$1", u.ID).Scan(&assets); err != nil || assets != 1 {
		t.Fatal("rejected upload left orphan asset", assets, err)
	}
}

func TestVideoCatalogExactModelRouting(t *testing.T) {
	c := defaultVideoConfig()
	c.Enabled, c.Paid, c.Gemini, c.Seedance, c.HFSY, c.Agnes = true, true, true, true, true, true
	providers := managedVideoProviders(c)
	for i := range providers {
		providers[i].HasAPIKey = true
	}
	s := &Server{Config: config.Config{Edition: config.Platform}}
	for _, model := range videoCatalog(c) {
		for _, variant := range model.Variants {
			selection, err := s.selectVideo(c, providers, videoCreateInput{ModelCode: model.Code, Resolution: variant.Resolution, Duration: variant.Duration, AspectRatio: "9:16"})
			if err != nil || selection.Native == "" || selection.Variant.Price < variant.Price {
				t.Fatal("catalog variant unavailable", model.Code, variant.Resolution, variant.Duration, err)
			}
			if model.Code == "seedance-mini" && (selection.Family != video.HFSY || selection.Native != "sd-2.0-mini") {
				t.Fatal("mini480/720 did not map to same HFSY model")
			}
			if model.Provider == video.Seedance {
				selection, err = s.selectVideo(c, providers, videoCreateInput{ModelCode: model.Code, Resolution: variant.Resolution, Duration: variant.Duration, AspectRatio: "adaptive"})
				if err != nil || selection.Family != video.Seedance || selection.Native != model.Model {
					t.Fatal("unsupported HFSY ratio failed to select exact APIMart model", err)
				}
			}
		}
	}
}

func TestPostgresVideoHTTPAudioSeedAndWatermarkDefaults(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	in := videoInputFixture()
	in["model_code"], in["duration_seconds"], in["resolution"] = "seedance-mini", 5, "480p"
	in["generate_audio"], in["seed"] = false, 42
	data := videoResponseData(t, videoJSONRequest(t, s, token, "audio-seed-video-key", in))
	task, err := s.Store.Task(context.Background(), u.ID, data["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := s.CompileVideoTask(context.Background(), task)
	if err != nil || r.GenerateAudio == nil || *r.GenerateAudio || r.Seed == nil || *r.Seed != 42 {
		t.Fatal("APIMart audio or seed option lost", err)
	}
	if status, _, _ := request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"hfsy_enabled": true, "hfsy_api_key": "fixture-hfsy-key"}); status != 200 {
		t.Fatal("HFSY config rejected")
	}
	data = videoResponseData(t, videoJSONRequest(t, s, token, "watermark-video-key", in))
	task, err = s.Store.Task(context.Background(), u.ID, data["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	r, p, err := s.CompileVideoTask(context.Background(), task)
	if err != nil || r.Model != "sd-2.0-mini" || p.Protocol != "hfsy_video" || r.GenerateAudio != nil || r.Seed != nil || r.Watermark != nil {
		t.Fatal("HFSY did not preserve legacy no-watermark/default option contract", err)
	}
}

func videoRuntimeClient(t *testing.T) *http.Client {
	t.Helper()
	c, err := provider.NewHTTPClient(provider.Options{AllowLocalDevelopment: true, Timeout: 5 * time.Second, MaxResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func videoMockMP4() []byte {
	// Synthetic ISO BMFF signature, no real video or user fixture.
	head := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	return append(head, make([]byte, 1024)...)
}

func videoConfigureEndpoint(t *testing.T, s *Server, u store.User, taskID, endpoint string) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := s.Store.Task(ctx, u.ID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := s.CompileVideoTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	p.BaseURL, p.TimeoutSeconds = endpoint, 3
	if _, err := s.Store.PutProvider(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	return task
}

func videoRuntimeClaim(t *testing.T, s *Server, taskID string) store.Task {
	t.Helper()
	lease, err := s.Store.ClaimTaskCapabilities(context.Background(), "mock-video-executor", time.Minute, []string{"video"})
	if err != nil || lease.ID != taskID {
		t.Fatal("runtime claim", err)
	}
	return lease
}

func videoRuntimePoll(t *testing.T, s *Server, taskID string) store.VideoPollClaim {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Store.Pool.Exec(ctx, "UPDATE tasks SET next_run_at=now() WHERE id=$1", taskID); err != nil {
		t.Fatal(err)
	}
	claim, err := s.Store.ClaimVideoPoll(ctx, "mock-video-poller", time.Minute)
	if err != nil || claim.Task.ID != taskID {
		t.Fatal("runtime poll claim", err)
	}
	return claim
}

func TestPostgresVideoRuntimeProviderFamilies(t *testing.T) {
	for _, tc := range []struct {
		code    string
		family  video.Provider
		edition config.Edition
	}{{"gemini-omni", video.Gemini, config.Platform}, {"seedance-mini", video.Seedance, config.Platform}, {"seedance-mini", video.HFSY, config.Platform}, {"agnes-v2", video.Agnes, config.Platform}, {"gemini-omni", video.Gemini, config.Personal}} {
		t.Run(string(tc.edition)+"/"+string(tc.family), func(t *testing.T) {
			s, token, u := videoHTTPFixture(t, tc.edition)
			ctx := context.Background()
			if tc.edition == config.Personal {
				var settings int
				if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM app_settings WHERE key='video_runtime_config'").Scan(&settings); err != nil || settings != 0 {
					t.Fatal("personal fixture depended on platform settings", err)
				}
			}
			if tc.family == video.HFSY {
				if status, _, _ := request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"hfsy_enabled": true, "hfsy_api_key": "fixture-hfsy-runtime"}); status != 200 {
					t.Fatal("HFSY setup")
				}
			}
			var creates, polls, downloads atomic.Int32
			media := videoMockMP4()
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downloads.Add(1)
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("credentials forwarded to result origin")
				}
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = w.Write(media)
			}))
			defer cdn.Close()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer fixture-") {
					t.Error("mock API credential not applied")
				}
				if r.Method == "POST" {
					creates.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					switch tc.family {
					case video.Agnes:
						_, _ = w.Write([]byte(`{"video_id":"runtime-id"}`))
					case video.HFSY:
						_, _ = w.Write([]byte(`{"data":{"task_id":"runtime-id"}}`))
					default:
						_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"runtime-id"}]}`))
					}
					return
				}
				polls.Add(1)
				url := cdn.URL + "/result.mp4"
				switch tc.family {
				case video.Agnes:
					_ = json.NewEncoder(w).Encode(map[string]any{"video_id": "runtime-id", "status": "success", "metadata": map[string]any{"url": url, "size_mapping": map[string]int{"width": 1280, "height": 720}}})
				case video.HFSY:
					_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "runtime-id", "status": "completed", "result_url": url})
				default:
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "runtime-id", "status": "completed", "result": map[string]any{"videos": []any{map[string]any{"url": []string{url}}}}}})
				}
			}))
			defer api.Close()
			input := videoInputFixture()
			input["model_code"] = tc.code
			id := videoResponseData(t, videoJSONRequest(t, s, token, "runtime-provider-key", input))["id"].(string)
			videoConfigureEndpoint(t, s, u, id, api.URL)
			client := videoRuntimeClient(t)
			lease := videoRuntimeClaim(t, s, id)
			s.executeVideoSubmission(ctx, lease, client)
			claim := videoRuntimePoll(t, s, id)
			if claim.Provider.ID == "" || claim.Submission.Provider != tc.family {
				t.Fatal("durable provider identity lost")
			}
			s.executeVideoPollLease(ctx, claim, client)
			v, err := s.Store.VideoTask(ctx, u.ID, id)
			if err != nil || v.Status != "succeeded" || v.Attempt != 1 || v.RefundedAt != nil || strings.Contains(string(v.Result), cdn.URL) {
				t.Fatal("runtime did not safely complete", err, v.Status)
			}
			if creates.Load() != 1 || polls.Load() != 1 || downloads.Load() != 1 {
				t.Fatal("unexpected runtime network counts", creates.Load(), polls.Load(), downloads.Load())
			}
			if tc.edition == config.Personal {
				var ledger int
				if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger").Scan(&ledger); err != nil || ledger != 0 || v.CreditsCost != 0 || claim.Provider.OwnerScope != u.ID {
					t.Fatal("personal runtime touched platform billing or credentials", err)
				}
			}
			view := s.videoTaskView(ctx, v, false)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", view["result_url"].(string), nil))
			if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), media) {
				t.Fatal("stored video did not round trip through signed serving", w.Code)
			}
		})
	}
}

func TestPostgresVideoRuntimeAmbiguityAndDefinitiveRejection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
		refund int
	}{
		{"server-error", 503, `{"error":"fixture upstream unavailable"}`, "uncertain", 0},
		{"missing-id", 200, `{}`, "uncertain", 0},
		{"authentication", 403, `{"error":"fixture denial"}`, "failed", 1},
		{"contradictory-known-id", 200, `{"code":500,"data":[{"task_id":"known-despite-error"}]}`, "polling", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, token, u := videoHTTPFixture(t, config.Platform)
			var calls atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer api.Close()
			id := videoResponseData(t, videoJSONRequest(t, s, token, "runtime-ambiguity-key", videoInputFixture()))["id"].(string)
			videoConfigureEndpoint(t, s, u, id, api.URL)
			lease := videoRuntimeClaim(t, s, id)
			client := videoRuntimeClient(t)
			s.executeVideoSubmission(context.Background(), lease, client)
			s.executeVideoSubmission(context.Background(), lease, client)
			v, err := s.Store.VideoTask(context.Background(), u.ID, id)
			if err != nil || v.Status != tc.want || calls.Load() != 1 {
				t.Fatal("submission outcome/replay", err, v.Status, calls.Load())
			}
			var refunds int
			if err := s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE biz_id=$1 AND biz_type='generation_refund'", id).Scan(&refunds); err != nil || refunds != tc.refund {
				t.Fatal("ambiguous/rejected settlement", err, refunds)
			}
			if _, err := s.Store.ClaimTaskCapabilities(context.Background(), "duplicate", time.Minute, []string{"video"}); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("paid task returned to submission queue", err)
			}
		})
	}
}

func TestPostgresVideoRuntimePollStorageAndRestartRecovery(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	var creates, polls, downloads atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := downloads.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		if n == 1 {
			_, _ = w.Write([]byte("<html>not a video</html>"))
			return
		}
		_, _ = w.Write(videoMockMP4())
	}))
	defer cdn.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			creates.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"task_id":"restart-id"}]}`))
			return
		}
		if polls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "restart-id", "status": "completed", "url": cdn.URL + "/result.mp4"}})
	}))
	defer api.Close()
	id := videoResponseData(t, videoJSONRequest(t, s, token, "runtime-recovery-key", videoInputFixture()))["id"].(string)
	videoConfigureEndpoint(t, s, u, id, api.URL)
	client := videoRuntimeClient(t)
	s.executeVideoSubmission(ctx, videoRuntimeClaim(t, s, id), client)
	s.executeVideoPoll(ctx, videoRuntimePoll(t, s, id), client)
	v, err := s.Store.VideoTask(ctx, u.ID, id)
	if err != nil || v.Status != "polling" || v.RefundedAt != nil || !v.NextRunAt.After(time.Now()) {
		t.Fatal("poll transport error lost known task/backoff", err)
	}
	s.executeVideoPoll(ctx, videoRuntimePoll(t, s, id), client)
	v, err = s.Store.VideoTask(ctx, u.ID, id)
	if err != nil || v.Status != "polling" || len(v.PendingResult) == 0 || !v.NextRunAt.After(time.Now()) {
		t.Fatal("invalid result body erased completion or spun without backoff", err)
	}
	claim := videoRuntimePoll(t, s, id)
	g, err := s.videoGateway(claim.Provider, claim.Submission.Provider, client)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := s.downloadVideoAsset(ctx, claim.Task, g, claim.Pending.ResultURL)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate shutdown after durable storage but before terminal settlement.
	if _, err := s.Store.Pool.Exec(ctx, "UPDATE tasks SET lease_until=now()-interval '1 second',next_run_at=now() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	s.executeVideoPoll(ctx, videoRuntimePoll(t, s, id), client)
	v, err = s.Store.VideoTask(ctx, u.ID, id)
	if err != nil || v.Status != "succeeded" || !strings.Contains(string(v.Result), asset.ID) || creates.Load() != 1 || polls.Load() != 2 || downloads.Load() != 2 {
		t.Fatal("restart failed to reuse verified result", err, v.Status, creates.Load(), polls.Load(), downloads.Load())
	}
	var assets int
	if err := s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM assets WHERE task_id=$1", id).Scan(&assets); err != nil || assets != 1 {
		t.Fatal("recovery duplicated result assets", err, assets)
	}
}

func TestPostgresVideoRunTasksShutdownJoinsStreamingDownload(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	if status, _, _ := request(t, s, "PATCH", "/api/admin/video-config", token, map[string]any{"poll_seconds": 2}); status != 200 {
		t.Fatal("poll config")
	}
	started := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(videoMockMP4())
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
		closed <- struct{}{}
	}))
	defer cdn.Close()
	var creates atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			creates.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"task_id":"shutdown-id"}]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "shutdown-id", "status": "completed", "url": cdn.URL + "/result.mp4"}})
	}))
	defer api.Close()
	id := videoResponseData(t, videoJSONRequest(t, s, token, "runtime-shutdown-key", videoInputFixture()))["id"].(string)
	videoConfigureEndpoint(t, s, u, id, api.URL)
	imageTask, _, err := s.Store.SubmitTask(context.Background(), store.SubmitTask{UserID: u.ID, Capability: "image", Kind: "fixture", IdempotencyKey: "other-capability", Request: json.RawMessage(`{"version":1,"image":{"prompt":"fixture"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.RunVideoTasks(ctx) }()
	defer cancel()
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		cancel()
		<-done
		t.Fatal("runtime did not reach mock download")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("video runtime did not join on shutdown")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("streaming result request outlived shutdown")
	}
	v, err := s.Store.VideoTask(context.Background(), u.ID, id)
	if err != nil || v.Status != "polling" || v.RefundedAt != nil || creates.Load() != 1 {
		t.Fatal("shutdown changed paid settlement", err, v.Status)
	}
	untouched, err := s.Store.Task(context.Background(), u.ID, imageTask.ID)
	if err != nil || untouched.Status != "queued" || untouched.Attempt != 0 {
		t.Fatal("video runtime claimed image work", err)
	}
	var assets int
	if err := s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM assets WHERE task_id=$1", id).Scan(&assets); err != nil || assets != 0 {
		t.Fatal("partial download published an asset", err, assets)
	}
}

func TestPostgresVideoRuntimeCircuitCapacityAndDemoGuards(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Platform)
	ctx := context.Background()
	var creates atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"task_id":"capacity-id"}]}`))
	}))
	defer api.Close()
	first := videoResponseData(t, videoJSONRequest(t, s, token, "capacity-first-key", videoInputFixture()))["id"].(string)
	task := videoConfigureEndpoint(t, s, u, first, api.URL)
	_, p, err := s.CompileVideoTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	p.MaxConcurrency = 1
	p, err = s.Store.PutProvider(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := videoRuntimeClient(t)
	if err := s.Store.RecordProviderOutcome(ctx, p, "MOCK_AUTH_FAILURE", true); err != nil {
		t.Fatal(err)
	}
	s.executeVideoSubmission(ctx, videoRuntimeClaim(t, s, first), client)
	v, err := s.Store.VideoTask(ctx, u.ID, first)
	if err != nil || v.Status != "queued" || v.Attempt != 0 || creates.Load() != 0 {
		t.Fatal("open circuit made a paid request or discarded queue", err)
	}
	if err := s.Store.RecordProviderOutcome(ctx, p, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Pool.Exec(ctx, "UPDATE tasks SET next_run_at=now() WHERE id=$1", first); err != nil {
		t.Fatal(err)
	}
	s.executeVideoSubmission(ctx, videoRuntimeClaim(t, s, first), client)
	second := videoResponseData(t, videoJSONRequest(t, s, token, "capacity-second-key", videoInputFixture()))["id"].(string)
	s.executeVideoSubmission(ctx, videoRuntimeClaim(t, s, second), client)
	v, err = s.Store.VideoTask(ctx, u.ID, second)
	if err != nil || v.Status != "queued" || v.Attempt != 0 || creates.Load() != 1 {
		t.Fatal("known-id attempt did not count against provider capacity", err)
	}
	if _, err := s.Store.CancelVideoTask(ctx, u.ID, second); err != nil {
		t.Fatal(err)
	}
	s.Config.DemoMode = true
	demo := videoResponseData(t, videoJSONRequest(t, s, token, "demo-no-paid-request", videoInputFixture()))["id"].(string)
	// Toggling demo off before execution must not turn a free queued job paid.
	s.Config.DemoMode = false
	s.executeVideoSubmission(ctx, videoRuntimeClaim(t, s, demo), client)
	v, err = s.Store.VideoTask(ctx, u.ID, demo)
	if err != nil || v.Status != "failed" || v.CreditsCost != 0 || creates.Load() != 1 || v.ErrorCode != "VIDEO_DEMO_EXECUTION_UNAVAILABLE" {
		t.Fatal("demo task became paid after restart/config change", err)
	}
}

func TestPostgresVideoPersonalLocalOriginUsesHTTPSProviderWithoutAdminConfig(t *testing.T) {
	s, token, u := videoHTTPFixture(t, config.Personal)
	s.Config.AllowPrivateProviders = false
	var calls atomic.Int32
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input map[string]any
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Error("invalid mock request")
		}
		if input["webhook"] != nil || input["callback_url"] != nil || input["webhook_url"] != nil {
			t.Error("local personal origin advertised as a public callback")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"task_id":"personal-https-id"}]}`))
	}))
	defer api.Close()
	roots := x509.NewCertPool()
	roots.AddCert(api.Certificate())
	// Only the local test transport allows loopback; the gateway keeps its
	// production HTTPS requirement and the personal app still uses localhost.
	client, err := provider.NewHTTPClient(provider.Options{AllowLocalDevelopment: true, TrustedRoots: roots, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	id := videoResponseData(t, videoJSONRequest(t, s, token, "personal-local-https", videoInputFixture()))["id"].(string)
	videoConfigureEndpoint(t, s, u, id, api.URL)
	s.executeVideoSubmission(context.Background(), videoRuntimeClaim(t, s, id), client)
	v, err := s.Store.VideoTask(context.Background(), u.ID, id)
	if err != nil || v.Status != "polling" || calls.Load() != 1 || v.CreditsCost != 0 {
		t.Fatal("local personal origin blocked owner-configured HTTPS provider", err, v.Status)
	}
	var configRows int
	if err := s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM app_settings WHERE key='video_runtime_config'").Scan(&configRows); err != nil || configRows != 0 {
		t.Fatal("personal path used platform-global configuration", err)
	}
}
