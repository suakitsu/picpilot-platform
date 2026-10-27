package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func adminAIHTTPFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := testServer(t, config.Platform)
	if _, pattern := s.mux.Handler(httptest.NewRequest(http.MethodGet, "/api/admin/ai-config", nil)); pattern != "GET /api/admin/ai-config" {
		t.Fatal("Server.New did not mount the admin AI routes")
	}
	token := initialize(t, s)
	s.Config.AllowPrivateProviders = true
	return s, token
}

func adminAIHTTPView(t *testing.T, s *Server, token string) map[string]any {
	t.Helper()
	status, result, _ := request(t, s, "GET", "/api/admin/ai-config", token, nil)
	if status != 200 {
		t.Fatal("AI config read failed", status, result["code"])
	}
	return result["data"].(map[string]any)
}

func adminAIMockInput(base string) map[string]any {
	return map[string]any{"image_providers": []any{map[string]any{"id": "mock-http", "name": "Mock image", "enabled": true, "base_url": base + "/v1", "model": "fixture-image", "api_key": "MOCK_KEY_DO_NOT_ECHO", "provider": "openai", "execution_mode": "http", "max_concurrency": 2, "timeout_seconds": 45, "priority": 10, "actual_cost": 0.125, "supported_sizes": "1K,2K", "notes": "Fixture only"}}}
}

func TestAdminAIHTTPRegistrationRoleGuardsAndPersonalIsolation(t *testing.T) {
	s, admin := adminAIHTTPFixture(t)
	for i := 0; i < 3; i++ {
		s.registerAdminRoutes()
		s.registerMessageRoutes()
		s.registerAdminAIRoutes()
		s.registerWorkerRoutes()
	}
	_, user := adminHTTPUser(t, s, "ai-user@example.invalid")
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/admin/ai-config"}, {"PATCH", "/api/admin/ai-config"}, {"POST", "/api/admin/ai-config/validate"},
		{"GET", "/api/admin/ai-config/catalog"}, {"GET", "/api/admin/ai-config/runtime"}, {"GET", "/api/admin/ai-config/versions"},
		{"POST", "/api/admin/ai-config/rollback"}, {"POST", "/api/admin/ai-config/probe/image"},
	} {
		if status, _, _ := request(t, s, route.method, route.path, "", map[string]any{}); status != 401 {
			t.Fatal("anonymous AI route access", route.path, status)
		}
		if status, _, _ := request(t, s, route.method, route.path, user, map[string]any{}); status != 403 {
			t.Fatal("user AI route access", route.path, status)
		}
	}
	for _, path := range []string{"/api/admin/ai-config", "/api/admin/ai-config/runtime", "/api/admin/ai-config/catalog", "/api/admin/ai-config/workers", "/api/admin/ai-config/versions", "/api/admin/users", "/api/messages"} {
		if status, _, _ := request(t, s, "GET", path, admin, nil); status != 200 {
			t.Fatal("registration lost existing route", path, status)
		}
	}
	if status, _, _ := request(t, s, "GET", "/api/admin/ai-config/versions?page_size=101", admin, nil); status != 400 {
		t.Fatal("unbounded history page")
	}
	p, owner := adminHTTPFixture(t, config.Personal)
	p.registerAdminAIRoutes()
	p.registerAdminRoutes()
	p.registerMessageRoutes()
	if _, err := p.Store.Pool.Exec(context.Background(), "DROP TABLE admin_ai_config_versions"); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := request(t, p, "GET", "/api/admin/ai-config", owner, nil); status != 404 {
		t.Fatal("personal AI compatibility route exposed", status)
	}
	if status, _, _ := request(t, p, "GET", "/api/messages", owner, nil); status != 200 {
		t.Fatal("personal messages depend on admin AI tables")
	}
}

func TestAdminAIHTTPFrontendRoundtripKeysRollbackAndValidation(t *testing.T) {
	s, token := adminAIHTTPFixture(t)
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(mock.Close)
	initial := adminAIHTTPView(t, s, token)
	if status, _, _ := request(t, s, "PATCH", "/api/admin/ai-config", token, initial); status != 200 {
		t.Fatal("unchanged frontend roundtrip failed", status)
	}
	input := adminAIMockInput(mock.URL)
	input["etag"] = initial["etag"]
	if status, result, _ := request(t, s, "POST", "/api/admin/ai-config/validate", token, input); status != 200 || result["data"].(map[string]any)["network_checked"] != false || result["data"].(map[string]any)["ok"] != true {
		t.Fatal("validation failed or performed network work", status)
	}
	status, result, body := request(t, s, "PATCH", "/api/admin/ai-config", token, input)
	if status != 200 {
		t.Fatal("save failed", status, result["code"])
	}
	if strings.Contains(body, "MOCK_KEY_DO_NOT_ECHO") || strings.Contains(body, "secret_ciphertext") || calls.Load() != 0 {
		t.Fatal("save leaked credential or probed upstream")
	}
	view := result["data"].(map[string]any)
	if view["can_rollback"] != true {
		t.Fatal("rollback unavailable after save")
	}
	stored, err := s.Store.Provider(context.Background(), "platform", "mock-http")
	if err != nil || !stored.HasAPIKey || stored.Model != "fixture-image" {
		t.Fatal("compatibility save did not update canonical provider", err)
	}
	key, err := s.Vault.Decrypt("platform/mock-http", stored.SecretCiphertext)
	if err != nil || key != "MOCK_KEY_DO_NOT_ECHO" {
		t.Fatal("provider key mapping broken")
	}
	if status, _, _ := request(t, s, "PATCH", "/api/admin/ai-config", token, input); status != 409 {
		t.Fatal("stale frontend save overwrote configuration")
	}
	// The frontend posts the complete GET document with a blank API key.
	for _, item := range view["image_providers"].([]any) {
		p := item.(map[string]any)
		if p["id"] == "mock-http" {
			if p["api_key"] != "" || p["api_key_set"] != true || p["api_key_masked"] != "********" {
				t.Fatal("credential redaction invalid")
			}
			p["name"] = "Renamed"
		}
	}
	if status, _, _ := request(t, s, "PATCH", "/api/admin/ai-config", token, view); status != 200 {
		t.Fatal("frontend edit failed", status)
	}
	stored, err = s.Store.Provider(context.Background(), "platform", "mock-http")
	if err != nil || stored.Name != "Renamed" {
		t.Fatal("edit missing", err)
	}
	key, err = s.Vault.Decrypt("platform/mock-http", stored.SecretCiphertext)
	if err != nil || key != "MOCK_KEY_DO_NOT_ECHO" {
		t.Fatal("blank API key erased credential")
	}
	// The existing frontend rollback request has no body at all.
	r := httptest.NewRequest("POST", "/api/admin/ai-config/rollback", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("bodyless rollback failed", w.Code)
	}
	stored, err = s.Store.Provider(context.Background(), "platform", "mock-http")
	if err != nil || stored.Name != "Mock image" {
		t.Fatal("rollback failed to restore row", err)
	}
	for _, path := range []string{"/api/admin/ai-config/runtime", "/api/admin/ai-config/catalog", "/api/admin/ai-config/versions?page_size=1&page=2"} {
		status, _, body = request(t, s, "GET", path, token, nil)
		if status != 200 || strings.Contains(body, "MOCK_KEY_DO_NOT_ECHO") || strings.Contains(body, "ciphertext") {
			t.Fatal("read/history leaked secret or failed", path, status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("non-probe endpoint made network calls")
	}
	for _, change := range []map[string]any{{"env_key": "PATH"}, {"actual_cost": -1}, {"max_concurrency": 0}, {"model": ""}, {"protocol": "apimart_video"}, {"base_url": "https://user:password@example.invalid"}, {"base_url": "https://other-origin.example.invalid"}, {"api_key": "bad\nkey"}} {
		p := map[string]any{"id": "mock-http"}
		for k, v := range change {
			p[k] = v
		}
		if status, _, _ := request(t, s, "POST", "/api/admin/ai-config/validate", token, map[string]any{"image": p}); status != 400 {
			t.Fatal("invalid provider configuration accepted", status)
		}
	}
	if status, _, _ := request(t, s, "PATCH", "/api/admin/ai-config", token, map[string]any{"image": map[string]any{"id": "mock-http", "enabled": false, "clear_api_key": true}}); status != 200 {
		t.Fatal("explicit key clear failed", status)
	}
	stored, err = s.Store.Provider(context.Background(), "platform", "mock-http")
	if err != nil || stored.HasAPIKey {
		t.Fatal("credential was not cleared", err)
	}
}

func TestAdminAIAdapterMappingAndEchoedFields(t *testing.T) {
	s := &Server{Config: config.Config{AllowPrivateProviders: true}}
	for _, test := range []struct{ label, model, endpoint, profile, protocol string }{
		{"hfsyapi", "gpt-image-2", "https://mock.invalid", "hfsyapi", "openai_images_generations"},
		{"yunfei-hires", "gpt-image-2", "https://mock.invalid", "yunfei", "openai_images_generations"},
		{"rua.chat", "gpt-image-2", "https://mock.invalid", "rua", "openai_images_generations"},
		{"gemini", "gemini-image-fixture", "https://mock.invalid", "gemini_proxy", "gemini_generate_content"},
		{"google_gemini", "gemini-image-fixture", "https://generativelanguage.googleapis.com/v1beta", "google_gemini", "gemini_generate_content"},
	} {
		raw, _ := json.Marshal(map[string]any{"image": map[string]any{"id": "test-provider", "name": "Fixture", "provider": test.label, "model": test.model, "base_url": test.endpoint}})
		var in adminAIInput
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatal(err)
		}
		ps, err := s.adminAIMerge(in, store.AdminAIState{})
		if err != nil || len(ps) != 1 || ps[0].Profile != test.profile || ps[0].Protocol != test.protocol {
			t.Fatal("legacy adapter mapping invalid", test.label, err)
		}
	}
	p := adminAINewProvider("image", "existing")
	p.Model = "gpt-image-2"
	p.BaseURL = "https://mock.invalid"
	state := store.AdminAIState{Providers: []store.Provider{p}}
	view := adminAIView(state)
	item := view["image_providers"].([]map[string]any)[0]
	item["provider"] = "hfsyapi"
	raw, _ := json.Marshal(view)
	var in adminAIInput
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	ps, err := s.adminAIMerge(in, state)
	if err != nil || ps[0].Profile != "hfsyapi" {
		t.Fatal("unchanged echoed profile masked a provider-label edit", err)
	}
	item["provider"], item["model"] = "gemini", "gemini-image-fixture"
	raw, _ = json.Marshal(view)
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	ps, err = s.adminAIMerge(in, state)
	if err != nil || ps[0].Profile != "gemini_proxy" || ps[0].Protocol != "gemini_generate_content" {
		t.Fatal("unchanged echoed protocol masked legacy Gemini inference", err)
	}
}

func TestAdminAIHTTPGeminiProbeUsesExistingAdapterAuthentication(t *testing.T) {
	s, token := adminAIHTTPFixture(t)
	var calls atomic.Int32
	var bad atomic.Bool
	var google atomic.Bool
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1beta/models" {
			bad.Store(true)
		}
		if google.Load() {
			if r.Header.Get("X-Goog-API-Key") != "MOCK_KEY_DO_NOT_ECHO" || r.Header.Get("Authorization") != "" {
				bad.Store(true)
			}
		} else if r.Header.Get("Authorization") != "Bearer MOCK_KEY_DO_NOT_ECHO" || r.Header.Get("X-Goog-API-Key") != "" {
			bad.Store(true)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(mock.Close)
	for _, profile := range []string{"gemini_proxy", "google_gemini"} {
		google.Store(profile == "google_gemini")
		input := adminAIMockInput(mock.URL)
		p := input["image_providers"].([]any)[0].(map[string]any)
		p["base_url"], p["model"], p["provider"], p["profile"], p["protocol"] = mock.URL, "gemini-image-fixture", profile, profile, "gemini_generate_content"
		status, result, _ := request(t, s, "POST", "/api/admin/ai-config/probe/image", token, input)
		if status != 200 || result["data"].(map[string]any)["ok"] != true {
			t.Fatal("Gemini metadata probe failed", profile, status)
		}
	}
	if bad.Load() || calls.Load() != 2 {
		t.Fatal("Gemini probe did not reuse runtime authentication contract")
	}
}

func TestAdminAIHTTPProbesAreExplicitBoundedMetadataOnly(t *testing.T) {
	s, token := adminAIHTTPFixture(t)
	var calls, redirects atomic.Int32
	var badMethod, badAuth atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); w.WriteHeader(200) }))
	t.Cleanup(sink.Close)
	var mode atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			badMethod.Store(true)
		}
		if r.Header.Get("Authorization") != "Bearer MOCK_KEY_DO_NOT_ECHO" {
			badAuth.Store(true)
		}
		if mode.Load() == 1 {
			http.Redirect(w, r, sink.URL, 302)
			return
		}
		if mode.Load() == 2 {
			w.WriteHeader(401)
		}
		if mode.Load() == 3 {
			_, _ = w.Write([]byte(strings.Repeat("x", 300*1024)))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"debug":"UPSTREAM_BODY_SECRET"}`))
	}))
	t.Cleanup(mock.Close)
	input := adminAIMockInput(mock.URL)
	for _, scenario := range []struct {
		mode int32
		ok   bool
	}{{0, true}, {1, false}, {2, false}, {3, false}} {
		mode.Store(scenario.mode)
		status, result, body := request(t, s, "POST", "/api/admin/ai-config/probe/image", token, input)
		if status != 200 {
			t.Fatal("explicit probe failed", status)
		}
		data := result["data"].(map[string]any)
		if data["ok"] != scenario.ok || data["model_called"] != false || data["probe_type"] != "metadata" {
			t.Fatal("probe misreported scope", scenario.mode)
		}
		if strings.Contains(body, "MOCK_KEY_DO_NOT_ECHO") || strings.Contains(body, "UPSTREAM_BODY_SECRET") {
			t.Fatal("probe leaked credential or response body")
		}
	}
	if calls.Load() != 4 || redirects.Load() != 0 || badMethod.Load() || badAuth.Load() {
		t.Fatal("probe invoked generation, redirected credentials, or used wrong authentication")
	}
	var tasks, attempts, jobs, ledger int
	if err := s.Store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM task_attempts),(SELECT count(*) FROM worker_jobs),(SELECT count(*) FROM credit_ledger)`).Scan(&tasks, &attempts, &jobs, &ledger); err != nil {
		t.Fatal(err)
	}
	if tasks+attempts+jobs+ledger != 0 {
		t.Fatal("probe created generation or billing work")
	}
	view := adminAIHTTPView(t, s, token)
	for _, item := range view["image_providers"].([]any) {
		item.(map[string]any)["enabled"] = true
	}
	status, result, _ := request(t, s, "POST", "/api/admin/ai-config/probe/image", token, view)
	if status != 200 {
		t.Fatal("worker configuration probe failed", status)
	}
	data := result["data"].(map[string]any)
	if data["network_checked"] != false || data["model_called"] != false || data["probe_type"] != "configuration" || calls.Load() != 4 {
		t.Fatal("worker probe dispatched work")
	}
	if status, _, _ := request(t, s, "POST", "/api/admin/ai-config/probe/video", token, input); status != 400 {
		t.Fatal("unsupported probe accepted")
	}
}

func TestAdminAIHTTPTaskDetailExposesPromptOnlyToAdmin(t *testing.T) {
	s, admin := adminAIHTTPFixture(t)
	u, user := adminHTTPUser(t, s, "task-owner@example.invalid")
	var id string
	err := s.Store.Pool.QueryRow(context.Background(), `INSERT INTO tasks(id,user_id,capability,kind,status,request_json,request_hash,idempotency_key,credits_cost) VALUES('admin-input-task',$1,'image','main','queued','{"version":1,"image":{"prompt":"Submitted visible input","size":"1024x1024","api_key":"SECRET_INPUT"},"authorization":"SECRET_AUTH"}','hash','admin-input-key',0) RETURNING id`, u.ID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := request(t, s, "GET", "/api/admin/tasks/"+id, user, nil); status != 403 {
		t.Fatal("task owner accessed admin detail")
	}
	status, result, body := request(t, s, "GET", "/api/admin/tasks/"+id, admin, nil)
	if status != 200 || !strings.Contains(body, "Submitted visible input") || strings.Contains(body, "SECRET_") {
		t.Fatal("authorized prompt projection failed", status)
	}
	data, _ := json.Marshal(result["data"])
	if !strings.Contains(string(data), "prompt_details") {
		t.Fatal("legacy task detail contract missing")
	}
}
