package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func projectRoutingProvider(t *testing.T, s *Server, u store.User, id, model, base string, priority int) {
	t.Helper()
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = u.ID
	}
	secret, err := s.Vault.Encrypt(scope+"/"+id, "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.PutProvider(context.Background(), store.Provider{ID: id, OwnerScope: scope, Name: "Routing mock", Capability: "image", Protocol: string(provider.ImageGenerations), Profile: "openai", BaseURL: base, Model: model, Enabled: true, ExecutionMode: "http", Priority: priority, MaxConcurrency: 2, TimeoutSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, &secret)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresProjectImageRoutingQuoteExplicitAndFree(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectRoutingProvider(t, s, u, "route-primary", "configured-image", "https://example.invalid", 10)
	projectRoutingProvider(t, s, u, "route-explicit", "explicit-image", "https://example.invalid", 0)
	cfg := store.ImageConfig{ImageType: "creative", ImageName: "Free configured image", Enabled: true, CreditsCost: 0, DefaultSize: "1040x832", DefaultModel: "configured-image", FallbackModel: "configured-backup"}
	workflowConfigFixture(t, s, cfg)
	for i, tc := range []struct{ model, fallback, wantModel, wantFallback string }{
		{"", "", "configured-image", "configured-backup"},
		{"explicit-image", "", "explicit-image", ""},
		{"explicit-image", "configured-image", "explicit-image", "configured-image"},
		{"", "explicit-image", "configured-image", "explicit-image"},
	} {
		in := map[string]any{"prompt": "Keep the exact visible text BLUE 23", "model": tc.model, "fallbackModel": tc.fallback}
		quote := projectOK(t, s, "POST", "/api/projects/creative-chats/estimate", token, "", in)
		if quote["credits_cost"] != float64(0) || quote["size"] != "1040x832" || quote["model"] != tc.wantModel || quote["fallback_model"] != tc.wantFallback {
			t.Fatal("quote lost model selection, exact size or free config")
		}
		before := workflowCounts(t, s, u.ID)
		detail := projectOK(t, s, "POST", "/api/projects/creative-images", token, fmt.Sprintf("routing-free-%d", i), in)
		tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, detail["project"].(map[string]any)["id"].(string))
		if err != nil || len(tasks) != 1 {
			t.Fatal("missing image task", err)
		}
		task, payload := workflowTaskPayload(t, s, u.ID, tasks[0].ID)
		if payload.Image.Model != tc.wantModel || payload.Image.FallbackModel != tc.wantFallback || payload.Image.Size != "1040x832" || task.CreditsCost != 0 || !strings.Contains(payload.Image.Prompt, "Keep the exact visible text BLUE 23") {
			t.Fatal("image payload differs from quote or user intent")
		}
		after := workflowCounts(t, s, u.ID)
		if before[1] != after[1] || before[3] != after[3] {
			t.Fatal("free configured image charged credits")
		}
	}
	before := workflowCounts(t, s, u.ID)
	for _, path := range []string{"/api/projects/creative-images", "/api/projects/creative-chats/estimate"} {
		status, out := projectRequest(s, "POST", path, token, "routing-not-configured", map[string]any{"prompt": "A rectangle", "model": "unconfigured-image"})
		if status != 409 || out["code"] != "IMAGE_ROUTING_MISMATCH" {
			t.Fatalf("selector silently ignored at %s: %d %v", path, status, out["code"])
		}
	}
	if before != workflowCounts(t, s, u.ID) {
		t.Fatal("unmatched model reserved credits or submitted task")
	}
	cfg.DefaultModel, cfg.FallbackModel = "", ""
	workflowConfigFixture(t, s, cfg)
	normal := projectOK(t, s, "POST", "/api/projects/creative-images", token, "routing-normal-chain", map[string]any{"prompt": "A rectangle"})
	tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, normal["project"].(map[string]any)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	_, payload := workflowTaskPayload(t, s, u.ID, tasks[0].ID)
	if payload.Image.Model != "" || payload.Image.FallbackModel != "" {
		t.Fatal("empty selection pinned a provider")
	}
	cfg.FallbackModel = "configured-image"
	workflowConfigFixture(t, s, cfg)
	quote := projectOK(t, s, "POST", "/api/projects/creative-chats/estimate", token, "", map[string]any{})
	if quote["model"] != "" || quote["fallback_model"] != "configured-image" {
		t.Fatal("empty primary did not retain shared normal-route semantics")
	}
}

func TestPostgresProjectImageRoutingPlannerRetainsExplicitChoice(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectRoutingProvider(t, s, u, "route-explicit", "explicit-image", "https://example.invalid", 10)
	cfg := store.ImageConfig{ImageType: "creative", ImageName: "Planner model fixture", Enabled: true, CreditsCost: 0, DefaultSize: "1040x832", DefaultModel: "configured-image", FallbackModel: "configured-backup"}
	workflowConfigFixture(t, s, cfg)
	queued := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "planner-routing-fixture", map[string]any{"message": "Draw a rectangle", "model": "explicit-image", "fallback_model": "explicit-backup"})
	task, payload := workflowTaskPayload(t, s, u.ID, queued["planning_task_id"].(string))
	if payload.Text == nil || payload.Text.Model != "" {
		t.Fatal("image selector was applied to text planner")
	}
	cfg.DefaultModel, cfg.FallbackModel = "changed-default", "changed-backup"
	workflowConfigFixture(t, s, cfg)
	finishProjectText(t, s, task.ID, `{"intent":"generate_image","reply":"Ready","final_prompt":"A rectangle","confidence":0.99}`, map[string]any{})
	chatID := queued["chat"].(map[string]any)["chat"].(map[string]any)["id"].(string)
	view := projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	versions := view["versions"].([]any)
	if len(versions) != 1 {
		t.Fatal("planner failed to produce an image task")
	}
	_, image := workflowTaskPayload(t, s, u.ID, versions[0].(map[string]any)["task_id"].(string))
	if image.Image.Model != "explicit-image" || image.Image.FallbackModel != "explicit-backup" {
		t.Fatal("delayed image submission lost explicit routing")
	}
}

func TestPostgresProjectImageRoutingMockHTTPOrderAndUncertainStop(t *testing.T) {
	for _, mode := range []string{"success", "rejected", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			s, token, u := projectTestServer(t, config.Platform)
			s.Config.AllowPrivateProviders = true
			output := base64.StdEncoding.EncodeToString(projectPNG(t, 17, 29))
			var mu sync.Mutex
			calls := []string{}
			mock := func(model string) *httptest.Server {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					calls = append(calls, model)
					mu.Unlock()
					var body map[string]any
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != model {
						t.Error("incorrect model wire request")
					}
					w.Header().Set("Content-Type", "application/json")
					if model == "primary-image" && mode == "rejected" {
						w.WriteHeader(401)
						_, _ = w.Write([]byte(`{"error":{"message":"mock definitive rejection"}}`))
						return
					}
					if model == "primary-image" && mode == "uncertain" {
						_, _ = w.Write([]byte(`{"data":[]}`))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": output}}})
				}))
				t.Cleanup(ts.Close)
				return ts
			}
			primary, backup, unrelated := mock("primary-image"), mock("backup-image"), mock("unrelated-image")
			projectRoutingProvider(t, s, u, "route-primary", "primary-image", primary.URL, 30)
			projectRoutingProvider(t, s, u, "route-backup", "backup-image", backup.URL, 10)
			projectRoutingProvider(t, s, u, "route-unrelated", "unrelated-image", unrelated.URL, 0)
			workflowConfigFixture(t, s, store.ImageConfig{ImageType: "creative", ImageName: "Model order fixture", Enabled: true, CreditsCost: 0, DefaultSize: "1024x1024", DefaultModel: "primary-image", FallbackModel: "backup-image"})
			detail := projectOK(t, s, "POST", "/api/projects/creative-images", token, "http-routing-"+mode, map[string]any{"prompt": "A rectangle"})
			mu.Lock()
			count := len(calls)
			mu.Unlock()
			if count != 0 {
				t.Fatal("handler submitted provider HTTP synchronously")
			}
			lease, err := s.Store.ClaimTaskCapabilities(context.Background(), "routing-mock", time.Minute, []string{"image"})
			if err != nil {
				t.Fatal(err)
			}
			gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
			if err != nil {
				t.Fatal(err)
			}
			defer gateway.CloseIdleConnections()
			s.processHTTPModelTask(context.Background(), lease, gateway)
			final, err := s.Store.Task(context.Background(), u.ID, lease.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := []string{"primary-image"}
			if mode == "rejected" {
				expected = append(expected, "backup-image")
			}
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("primary order/fallback boundary violated: %v", got)
			}
			if mode == "uncertain" {
				if final.Status != "uncertain" {
					t.Fatal("ambiguous submission was not marked uncertain", final.Status)
				}
				project := detail["project"].(map[string]any)["id"].(string)
				before := workflowCounts(t, s, u.ID)
				status, _ := projectRequest(s, "POST", "/api/projects/"+project+"/images/"+lease.ID+"/retry", token, "uncertain-image-retry", map[string]any{})
				if status != 409 || workflowCounts(t, s, u.ID) != before {
					t.Fatal("uncertain generation permitted duplicate submission")
				}
			} else if final.Status != "succeeded" {
				t.Fatal("mock image failed", final.Status, final.ErrorCode)
			}
		})
	}
}
