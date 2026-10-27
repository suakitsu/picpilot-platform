package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/risk"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

const workflowBlockedFixture = "workflowblockedfixture"

func workflowRiskFixture(t *testing.T, s *Server) {
	t.Helper()
	cfg := risk.Config{Enabled: true, BanThreshold: 100, BlockLevels: []string{"high"}, WarnLevels: []string{}, Keywords: map[string][]string{"high": {workflowBlockedFixture}, "medium": {}, "low": {}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Store.Pool.Exec(context.Background(), `INSERT INTO app_settings(key,value) VALUES('risk_control',$1) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, creative.JSON(cfg))
	if err != nil {
		t.Fatal(err)
	}
}

func workflowTemplateFixture(t *testing.T, s *Server, u store.User, scene, content string) store.PromptTemplate {
	t.Helper()
	name := "Workflow fixture"
	p, err := s.Store.AdminSavePrompt(context.Background(), u.ID, "", "workflow-template-fixture", store.AdminPromptInput{Name: &name, Scene: &scene, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func workflowConfigFixture(t *testing.T, s *Server, c store.ImageConfig) {
	t.Helper()
	_, err := s.Store.Pool.Exec(context.Background(), `INSERT INTO admin_image_configs(id,image_type,image_name,enabled,credits_cost,default_model,fallback_model,default_size,required_fields,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(image_type) DO UPDATE SET image_name=EXCLUDED.image_name,enabled=EXCLUDED.enabled,credits_cost=EXCLUDED.credits_cost,default_model=EXCLUDED.default_model,fallback_model=EXCLUDED.fallback_model,default_size=EXCLUDED.default_size,required_fields=EXCLUDED.required_fields,sort_order=EXCLUDED.sort_order`, "fixture_"+c.ImageType, c.ImageType, c.ImageName, c.Enabled, c.CreditsCost, c.DefaultModel, c.FallbackModel, c.DefaultSize, c.RequiredFields, c.SortOrder)
	if err != nil {
		t.Fatal(err)
	}
}

func workflowCounts(t *testing.T, s *Server, user string) [4]int64 {
	t.Helper()
	var counts [4]int64
	err := s.Store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM tasks WHERE user_id=$1),(SELECT count(*) FROM credit_ledger WHERE user_id=$1 AND biz_type='generation_charge'),(SELECT count(*) FROM assets WHERE user_id=$1),credits FROM users WHERE id=$1`, user).Scan(&counts[0], &counts[1], &counts[2], &counts[3])
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func workflowTaskPayload(t *testing.T, s *Server, user, id string) (store.Task, jobs.Payload) {
	t.Helper()
	task, err := s.Store.Task(context.Background(), user, id)
	if err != nil {
		t.Fatal(err)
	}
	var payload jobs.Payload
	if err = json.Unmarshal(task.Request, &payload); err != nil {
		t.Fatal(err)
	}
	return task, payload
}

func TestPostgresProjectPolicyScreensEveryOriginalEntrypoint(t *testing.T) {
	for _, kind := range []string{"creative", "aplus", "planner", "optimize", "selling", "assistant", "canvas-chat", "canvas-responses", "copy", "listing", "package", "linked", "retry"} {
		t.Run(kind, func(t *testing.T) {
			s, token, u := projectTestServer(t, config.Platform)
			projectProvider(t, s, u, "text", "https://example.invalid")
			projectProvider(t, s, u, "image", "https://example.invalid")
			path := ""
			var body any
			switch kind {
			case "creative", "aplus":
				path = "/api/projects/" + kind + "-images"
				body = map[string]any{"prompt": workflowBlockedFixture, "reference_url": "http://127.0.0.1:1/must-not-fetch"}
			case "planner":
				path = "/api/projects/creative-chats/generate"
				body = map[string]any{"message": workflowBlockedFixture}
			case "optimize":
				path = "/api/projects/optimize-prompt"
				body = map[string]any{"prompt": workflowBlockedFixture}
			case "selling":
				path = "/api/projects/domestic-selling-points/suggest"
				body = map[string]any{"product_name": workflowBlockedFixture}
			case "assistant":
				path = "/api/assistant/chat"
				body = map[string]any{"messages": []any{map[string]any{"role": "user", "content": workflowBlockedFixture}}}
			case "canvas-chat":
				path = "/api/projects/canvas-text"
				body = map[string]any{"request_body": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": workflowBlockedFixture}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "http://127.0.0.1:1/must-not-fetch"}}}}}}}
			case "canvas-responses":
				if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE providers SET protocol='openai_responses' WHERE capability='text'`); err != nil {
					t.Fatal(err)
				}
				path = "/api/projects/canvas-text"
				body = map[string]any{"protocol": "openai_responses", "request_body": map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": workflowBlockedFixture}}}}}}
			case "copy":
				p := projectOK(t, s, "POST", "/api/projects", token, "risk-project-fixture", map[string]any{"product_name": workflowBlockedFixture})
				path = "/api/projects/" + p["id"].(string) + "/generate-copy"
			case "listing", "package", "linked":
				id, _ := prepareListing(t, s, token, u, true)
				path = "/api/projects/" + id
				switch kind {
				case "listing":
					if err := s.Store.SetProjectFacts(context.Background(), u.ID, id, creative.JSON(map[string]any{"material": workflowBlockedFixture})); err != nil {
						t.Fatal(err)
					}
					path += "/images"
				case "package":
					path += "/listing-package"
					body = map[string]any{"aplus_prompt": workflowBlockedFixture}
				case "linked":
					path += "/linked-aplus"
					body = map[string]any{"module": workflowBlockedFixture}
				}
			case "retry":
				if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE users SET credits=100 WHERE id=$1`, u.ID); err != nil {
					t.Fatal(err)
				}
				detail := projectOK(t, s, "POST", "/api/projects/creative-images", token, "risk-retry-initial", map[string]any{"prompt": "A simple rectangle"})
				id := detail["project"].(map[string]any)["id"].(string)
				tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, id)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Store.Pool.Exec(context.Background(), `UPDATE tasks SET status='failed' WHERE id=$1`, tasks[0].ID); err != nil {
					t.Fatal(err)
				}
				path = "/api/projects/" + id + "/images/" + tasks[0].ID + "/retry"
				body = map[string]any{"reference_text": workflowBlockedFixture}
			}
			workflowRiskFixture(t, s)
			before := workflowCounts(t, s, u.ID)
			for range 2 {
				status, out := projectRequest(s, "POST", path, token, "same-original-risk-request", body)
				if status != 422 || out["code"] != "CONTENT_BLOCKED" {
					t.Fatalf("screening status=%d code=%v", status, out["code"])
				}
			}
			if after := workflowCounts(t, s, u.ID); after != before {
				t.Fatalf("blocked request changed tasks/charges/assets/balance: %v -> %v", before, after)
			}
			var hits int
			if err := s.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_risk_events WHERE user_id=$1`, u.ID).Scan(&hits); err != nil {
				t.Fatal(err)
			}
			if hits != 1 {
				t.Fatalf("replayed original input counted %d times", hits)
			}
		})
	}
}

func TestWorkflowUserTextExcludesGeneratedAndSystemContent(t *testing.T) {
	input := jobs.TextInput{Messages: []provider.Message{
		{Role: "system", Content: creative.JSON("system-only")},
		{Role: "developer", Content: creative.JSON("developer-only")},
		{Role: "assistant", Content: creative.JSON("assistant-only")},
		{Role: "tool", Content: creative.JSON("tool-only"), ToolCallID: "call-1"},
		{Role: "user", Content: creative.JSON([]any{map[string]any{"type": "text", "text": "original user"}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "image-only"}}})},
	}}
	if got := workflowMessageUserText(input); got != "original user" {
		t.Fatalf("wrong screening input: %q", got)
	}
	input = jobs.TextInput{Input: creative.JSON([]any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "responses user"}}}, map[string]any{"type": "function_call_output", "output": "tool-only"}, map[string]any{"role": "assistant", "content": "assistant-only"}})}
	if got := workflowMessageUserText(input); got != "responses user" {
		t.Fatalf("wrong Responses screening input: %q", got)
	}
	input.Input = creative.JSON("raw Responses user")
	if got := workflowMessageUserText(input); got != "raw Responses user" {
		t.Fatal(got)
	}
	p := store.Project{OriginalInfo: creative.JSON(map[string]any{"message": "original request", "prompt": "generated-only"}), Metadata: creative.JSON(map[string]any{"source_chat_id": "chat_fixture"})}
	if got := projectUserText(p); got != "original request" {
		t.Fatalf("planner output screened: %q", got)
	}
}

func TestPostgresProjectPolicyTextTemplatesKeepOriginalMessages(t *testing.T) {
	for _, scene := range []string{"prompt/optimize", "copy/domestic-selling-points", "assistant/global", "canvas/text"} {
		t.Run(scene, func(t *testing.T) {
			s, token, u := projectTestServer(t, config.Platform)
			projectProvider(t, s, u, "text", "https://example.invalid")
			workflowRiskFixture(t, s)
			marker := "operator-only " + workflowBlockedFixture + " " + scene
			workflowTemplateFixture(t, s, u, scene, marker)
			original := "Keep exact TEXT 34.1"
			path := ""
			var body any
			switch scene {
			case "prompt/optimize":
				path = "/api/projects/optimize-prompt"
				body = map[string]any{"prompt": original}
			case "copy/domestic-selling-points":
				path = "/api/projects/domestic-selling-points/suggest"
				body = map[string]any{"product_name": original}
			case "assistant/global":
				path = "/api/assistant/chat"
				body = map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": workflowBlockedFixture}, map[string]any{"role": "user", "content": original}}, "page_context": map[string]any{"generated": workflowBlockedFixture}}
			case "canvas/text":
				path = "/api/projects/canvas-text"
				body = map[string]any{"request_body": map[string]any{"messages": []any{map[string]any{"role": "developer", "content": workflowBlockedFixture}, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}, map[string]any{"role": "tool", "tool_call_id": "call-1", "content": workflowBlockedFixture}, map[string]any{"role": "user", "content": original}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}}}}
			}
			out := projectOK(t, s, "POST", path, token, "template-original-fixture", body)
			task, payload := workflowTaskPayload(t, s, u.ID, out["task_id"].(string))
			if payload.Text == nil || payload.Text.Messages[0].Role != "system" || !strings.Contains(string(task.Request), marker) || !strings.Contains(workflowMessageUserText(*payload.Text), original) {
				t.Fatal("template or original input missing")
			}
			if scene == "canvas/text" && (len(payload.Text.Messages) != 5 || len(payload.Text.Messages[2].ToolCalls) == 0 || payload.Text.Messages[3].ToolCallID != "call-1" || len(payload.Text.Options["tools"]) == 0) {
				t.Fatal("canvas structured/tool semantics changed")
			}
			var hits int
			if err := s.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_risk_events`).Scan(&hits); err != nil {
				t.Fatal(err)
			}
			if hits != 0 {
				t.Fatal("internal template/history/system output was screened")
			}
		})
	}
}

func TestPostgresProjectPolicyListingConfigAtomicPricingAndCopyBriefs(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	for _, slot := range creative.ListingSlots {
		workflowConfigFixture(t, s, store.ImageConfig{ImageType: slot.Type, ImageName: slot.Name, DefaultSize: "1024x1024"})
	}
	checklist := "operator-only checklist"
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "scenario", ImageName: "Configured scene", Enabled: true, CreditsCost: 7, DefaultSize: "720x1280", SortOrder: 1, DefaultModel: "fixture-image", FallbackModel: "fixture-backup"})
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "main", ImageName: "Configured main", Enabled: true, CreditsCost: 9, DefaultSize: "1040x832", RequiredFields: checklist, SortOrder: 2, DefaultModel: "fixture-image", FallbackModel: "fixture-backup"})
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "aplus", ImageName: "Configured A+", Enabled: true, CreditsCost: 6, DefaultSize: "1024x1024", DefaultModel: "fixture-image", FallbackModel: "fixture-backup"})
	marker := "private listing image instructions"
	workflowTemplateFixture(t, s, u, "image/brief", marker)
	workflowTemplateFixture(t, s, u, "copy/title", "copy fallback fixture")
	id, assets := prepareListing(t, s, token, u, true)
	detail := projectOK(t, s, "GET", "/api/projects/"+id, token, "", nil)
	briefs := detail["generated_copy"].(map[string]any)["image_briefs"].([]any)
	if len(briefs) != 2 || briefs[0].(map[string]any)["type"] != "scenario" || briefs[1].(map[string]any)["generation_size"] != "1040x832" || briefs[1].(map[string]any)["credits_cost"] != float64(9) {
		t.Fatal("copy briefs ignore runtime configuration")
	}
	copyID := detail["copy_task_id"].(string)
	copyTask, _ := workflowTaskPayload(t, s, u.ID, copyID)
	if !strings.Contains(string(copyTask.Request), "copy fallback fixture") {
		t.Fatal("copy/title fallback not consumed")
	}
	ctx := context.Background()
	if _, err := s.Store.Pool.Exec(ctx, `UPDATE users SET credits=21 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	before := workflowCounts(t, s, u.ID)
	status, out := projectRequest(s, "POST", "/api/projects/"+id+"/listing-package", token, "configured-package", map[string]any{})
	if status != 402 {
		t.Fatalf("partial package accepted: %d %v", status, out["code"])
	}
	if workflowCounts(t, s, u.ID) != before {
		t.Fatal("failed linked package was not atomic")
	}
	if _, err := s.Store.Pool.Exec(ctx, `UPDATE users SET credits=22 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	result := projectOK(t, s, "POST", "/api/projects/"+id+"/listing-package", token, "configured-package", map[string]any{})
	views := result["image_tasks"].([]any)
	if len(views) != 2 || views[0].(map[string]any)["image_type"] != "scenario" || views[1].(map[string]any)["image_name"] != "Configured main" {
		t.Fatal("disabled slots, names or ordering ignored")
	}
	tasks, err := s.Store.ProjectTasks(ctx, u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Capability != "image" {
			continue
		}
		_, payload := workflowTaskPayload(t, s, u.ID, task.ID)
		expected := "720x1280"
		cost := int64(7)
		if task.Kind == "main" {
			expected = "1040x832"
			cost = 9
		}
		if payload.Image.Size != expected || task.CreditsCost != cost || len(payload.Image.ReferenceAssetIDs) != 2 || payload.Image.ReferenceAssetIDs[0] != assets[0].ID || payload.Image.ReferenceAssetIDs[1] != assets[1].ID || payload.Image.Model != "fixture-image" || payload.Image.FallbackModel != "fixture-backup" {
			t.Fatal("exact canvas/price/reference order changed")
		}
		if !strings.Contains(payload.Image.Prompt, marker) {
			t.Fatal("image template ignored")
		}
		if task.Kind == "main" && !strings.Contains(payload.Image.Prompt, checklist) {
			t.Fatal("required_fields ignored")
		}
		public := fmt.Sprint(s.projectTaskView(ctx, task)["prompt_snapshot"])
		if strings.Contains(public, marker) || strings.Contains(public, checklist) {
			t.Fatal("operator template leaked through task view")
		}
	}
	if balance := workflowCounts(t, s, u.ID)[3]; balance != 0 {
		t.Fatal("configured linked price was not charged exactly", balance)
	}
	aplusID := result["aplus_project"].(map[string]any)["project"].(map[string]any)["id"].(string)
	aplusTasks, err := s.Store.ProjectTasks(ctx, u.ID, aplusID)
	if err != nil || len(aplusTasks) != 1 {
		t.Fatal("missing A+ task", err)
	}
	_, aplusPayload := workflowTaskPayload(t, s, u.ID, aplusTasks[0].ID)
	if aplusPayload.Image.Model != "fixture-image" || aplusPayload.Image.FallbackModel != "fixture-backup" || aplusPayload.Image.Size != "970x600" {
		t.Fatal("linked A+ lost model selection or exact legacy canvas")
	}
}

func TestPostgresProjectPolicySingleDefaultsExplicitOverridesAndFailures(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "image", "https://example.invalid")
	if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE users SET credits=100 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	cfg := store.ImageConfig{ImageType: "creative", ImageName: "Configured creative", Enabled: true, CreditsCost: 9, DefaultSize: "1040x832", RequiredFields: "private checklist"}
	workflowConfigFixture(t, s, cfg)
	tpl := workflowTemplateFixture(t, s, u, "image/single", "private single fallback")
	for _, tc := range []struct {
		size, want string
		cost       int64
	}{{"", "1040x832", 9}, {"4096x4096", "4096x4096", 30}} {
		in := map[string]any{"prompt": "Display the exact words KEEP IT", "size": tc.size, "template_id": tpl.ID}
		detail := projectOK(t, s, "POST", "/api/projects/creative-images", token, "single-config-"+tc.want, in)
		tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, detail["project"].(map[string]any)["id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		_, p := workflowTaskPayload(t, s, u.ID, tasks[0].ID)
		if p.Image.Size != tc.want || tasks[0].CreditsCost != tc.cost || tasks[0].Title != cfg.ImageName || !strings.Contains(p.Image.Prompt, "Display the exact words KEEP IT") || !strings.Contains(p.Image.Prompt, tpl.Content) {
			t.Fatal("default, explicit size, price or user intent lost")
		}
		if strings.Contains(string(creative.JSON(detail)), tpl.Content) || strings.Contains(string(creative.JSON(detail)), cfg.RequiredFields) {
			t.Fatal("private instructions exposed")
		}
	}
	before := workflowCounts(t, s, u.ID)
	for _, tc := range []struct{ kind, code string }{{"disabled", "IMAGE_KIND_DISABLED"}, {"default_model", "IMAGE_ROUTING_MISMATCH"}, {"fallback_model", "IMAGE_ROUTING_INVALID"}, {"wrong_template", "TEMPLATE_NOT_CONFIGURED"}} {
		next := cfg
		templateID := tpl.ID
		switch tc.kind {
		case "disabled":
			next.Enabled = false
		case "default_model":
			next.DefaultModel = "unconfigured-image"
		case "fallback_model":
			next.FallbackModel = "invalid\nmodel"
		case "wrong_template":
			templateID = "not-active"
		}
		workflowConfigFixture(t, s, next)
		status, out := projectRequest(s, "POST", "/api/projects/creative-images", token, "failed-config-"+tc.kind, map[string]any{"prompt": "A rectangle", "template_id": templateID})
		if status != 409 || out["code"] != tc.code {
			t.Fatalf("configuration silently ignored: %d %v", status, out["code"])
		}
		if workflowCounts(t, s, u.ID) != before {
			t.Fatal("invalid configuration reserved credits or queued work")
		}
	}
	workflowConfigFixture(t, s, cfg)
	workflowTemplateFixture(t, s, u, "image/creative", strings.Repeat("x", 32000))
	status, out := projectRequest(s, "POST", "/api/projects/creative-images", token, "too-large-template", map[string]any{"prompt": "A rectangle"})
	if status != 409 || out["code"] != "PROMPT_TEMPLATE_TOO_LARGE" || workflowCounts(t, s, u.ID) != before {
		t.Fatal("oversized template was truncated or charged")
	}
}

func TestPostgresProjectPolicyAllDisabledAndDemoPricing(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE admin_image_configs SET enabled=false`); err != nil {
		t.Fatal(err)
	}
	id, _ := prepareListing(t, s, token, u, false)
	detail := projectOK(t, s, "GET", "/api/projects/"+id, token, "", nil)
	if len(detail["generated_copy"].(map[string]any)["image_briefs"].([]any)) != 0 {
		t.Fatal("disabled image slots resurrected in copy")
	}
	before := workflowCounts(t, s, u.ID)
	status, out := projectRequest(s, "POST", "/api/projects/"+id+"/images", token, "all-disabled-listing", nil)
	if status != 409 || out["code"] != "IMAGE_TYPES_DISABLED" || workflowCounts(t, s, u.ID) != before {
		t.Fatal("disabled listing kinds were submitted")
	}
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "creative", ImageName: "Demo creative", Enabled: true, DefaultSize: "2048x2048", CreditsCost: 90})
	s.Config.DemoMode = true
	estimate := projectOK(t, s, "POST", "/api/projects/creative-chats/estimate", token, "", map[string]any{})
	if estimate["size"] != "2048x2048" || estimate["credits_cost"] != float64(0) {
		t.Fatal("demo ignored configured size or was priced as paid")
	}
}

func TestPostgresProjectPolicyResponsesTemplatePreservesItems(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE providers SET protocol='openai_responses' WHERE capability='text'`); err != nil {
		t.Fatal(err)
	}
	workflowRiskFixture(t, s)
	workflowTemplateFixture(t, s, u, "canvas/text", "system-only "+workflowBlockedFixture)
	items := []any{
		map[string]any{"role": "developer", "content": workflowBlockedFixture},
		map[string]any{"type": "function_call", "call_id": "call1", "name": "lookup", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "call1", "output": workflowBlockedFixture},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Original visible text"}}},
	}
	tools := []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}}
	in := map[string]any{"protocol": "openai_responses", "request_body": map[string]any{"input": items, "tools": tools, "reasoning": map[string]any{"effort": "high"}, "instructions": "caller-system-only"}}
	result := projectOK(t, s, "POST", "/api/projects/canvas-text", token, "responses-template-original", in)
	_, payload := workflowTaskPayload(t, s, u.ID, result["task_id"].(string))
	var persisted []any
	if payload.Text == nil || json.Unmarshal(payload.Text.Input, &persisted) != nil || len(persisted) != 5 {
		t.Fatal("Responses items missing")
	}
	var persistedTools any
	if json.Unmarshal(payload.Text.Options["tools"], &persistedTools) != nil {
		t.Fatal("invalid persisted tools")
	}
	if !reflect.DeepEqual(persisted[1:], items) || !reflect.DeepEqual(persistedTools, tools) || string(payload.Text.Options["instructions"]) != `"caller-system-only"` || !strings.Contains(string(payload.Text.Options["reasoning"]), "high") {
		t.Fatal("Responses template rewrote existing message/tool/option semantics")
	}
}

func TestPostgresProjectPolicyPlannerDefaultsAndInternalOutput(t *testing.T) {
	for _, mode := range []string{"generate", "disabled", "conversation"} {
		t.Run(mode, func(t *testing.T) {
			s, token, u := projectTestServer(t, config.Platform)
			projectProvider(t, s, u, "text", "https://example.invalid")
			projectProvider(t, s, u, "image", "https://example.invalid")
			if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE users SET credits=20 WHERE id=$1`, u.ID); err != nil {
				t.Fatal(err)
			}
			cfg := store.ImageConfig{ImageType: "creative", ImageName: "Configured creative", Enabled: mode != "conversation", DefaultSize: "1040x832", CreditsCost: 9}
			workflowConfigFixture(t, s, cfg)
			workflowRiskFixture(t, s)
			workflowTemplateFixture(t, s, u, "prompt/optimize", "planner fallback "+workflowBlockedFixture)
			workflowTemplateFixture(t, s, u, "image/creative", "private image instructions "+workflowBlockedFixture)
			queued := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "planner-config-original", map[string]any{"message": "Draw a rectangle"})
			task, payload := workflowTaskPayload(t, s, u.ID, queued["task_id"].(string))
			if !strings.Contains(string(task.Request), "planner fallback") || !strings.Contains(string(task.Request), "1040x832") || !strings.Contains(workflowMessageUserText(*payload.Text), "Draw a rectangle") {
				t.Fatal("planner ignored configured defaults/template or changed request")
			}
			chatID := queued["chat"].(map[string]any)["chat"].(map[string]any)["id"].(string)
			if mode == "disabled" {
				cfg.Enabled = false
				workflowConfigFixture(t, s, cfg)
			}
			intent := "generate_image"
			if mode == "conversation" {
				intent = "chat"
			}
			finishProjectText(t, s, task.ID, string(creative.JSON(map[string]any{"intent": intent, "reply": "Ready to help", "final_prompt": "Generated-only " + workflowBlockedFixture, "confidence": 0.99})), map[string]any{})
			view := projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
			versions := view["versions"].([]any)
			expectedBalance := int64(20)
			if mode == "generate" {
				expectedBalance = 11
				if len(versions) != 1 {
					t.Fatal("generated-only content was blocked or image was not queued")
				}
				version := versions[0].(map[string]any)
				imageTask, imagePayload := workflowTaskPayload(t, s, u.ID, version["task_id"].(string))
				if imagePayload.Image.Size != "1040x832" || imageTask.CreditsCost != 9 {
					t.Fatal("planner image ignored configured exact size or price")
				}
				if strings.Contains(fmt.Sprint(s.projectTaskView(context.Background(), imageTask)["prompt_snapshot"]), "private image instructions") {
					t.Fatal("planner exposed active template")
				}
			} else if len(versions) != 0 {
				t.Fatal("conversation/disabled kind queued an image")
			}
			if mode == "disabled" {
				found := false
				for _, v := range view["messages"].([]any) {
					m := v.(map[string]any)
					if m["safe_error"] == "IMAGE_KIND_DISABLED" && m["status"] == "failed" {
						found = true
					}
				}
				if !found {
					t.Fatal("disabled planner image did not publish terminal configuration failure")
				}
			}
			if workflowCounts(t, s, u.ID)[3] != expectedBalance {
				t.Fatal("wrong planner charge")
			}
			var hits int
			if err := s.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_risk_events`).Scan(&hits); err != nil {
				t.Fatal(err)
			}
			if hits != 0 {
				t.Fatal("generated or operator text classified as original user input")
			}
		})
	}
}

func TestPostgresProjectPolicyRetryKeepsPrivatePromptsPrivate(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "image", "https://example.invalid")
	if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE users SET credits=30 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "creative", ImageName: "Configured retry", Enabled: true, DefaultSize: "1040x832", CreditsCost: 9, DefaultModel: "fixture-image", FallbackModel: "fixture-backup"})
	workflowRiskFixture(t, s)
	marker := "private retry template " + workflowBlockedFixture
	workflowTemplateFixture(t, s, u, "image/creative", marker)
	detail := projectOK(t, s, "POST", "/api/projects/creative-images", token, "policy-retry-initial", map[string]any{"prompt": "A full rectangle"})
	id := detail["project"].(map[string]any)["id"].(string)
	tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Pool.Exec(context.Background(), `UPDATE tasks SET status='failed' WHERE id=$1`, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	workflowConfigFixture(t, s, store.ImageConfig{ImageType: "creative", ImageName: "Configured retry", Enabled: true, DefaultSize: "1040x832", CreditsCost: 9, DefaultModel: "new-default", FallbackModel: "new-fallback"})
	result := projectOK(t, s, "POST", "/api/projects/"+id+"/images/"+tasks[0].ID+"/retry", token, "policy-retry-revision", map[string]any{"reference_text": "Keep all four edges visible"})
	next := result["id"].(string)
	task, payload := workflowTaskPayload(t, s, u.ID, next)
	if strings.Count(payload.Image.Prompt, marker) != 1 || !strings.Contains(payload.Image.Prompt, "Keep all four edges visible") || payload.Image.Size != "1040x832" || task.CreditsCost != 9 || payload.Image.Model != "fixture-image" || payload.Image.FallbackModel != "fixture-backup" {
		t.Fatal("retry duplicated private prompt or lost canvas/revision/price")
	}
	if strings.Contains(fmt.Sprint(result["prompt_snapshot"]), marker) {
		t.Fatal("retry published operator instructions")
	}
}

func TestPostgresProjectRetryRequiresSourceWorkflow(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	asset := projectAsset(t, s, u, "", "reference", 19, 11)
	res := projectOK(t, s, "POST", "/api/image-translation/generate", token, "project-view-retry-source", map[string]any{"target_language": "de-DE", "reference_asset_ids": []string{asset.ID}})
	project := res["project_id"].(string)
	tasks, err := s.Store.ProjectTasks(context.Background(), u.ID, project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Pool.Exec(context.Background(), `UPDATE tasks SET status='failed' WHERE id=$1`, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	before := workflowCounts(t, s, u.ID)
	status, out := projectRequest(s, "POST", "/api/projects/"+project+"/images/"+tasks[0].ID+"/retry", token, "project-view-retry-guard", map[string]any{})
	if status != 409 || out["code"] != "WORKFLOW_RETRY_REQUIRED" || workflowCounts(t, s, u.ID) != before {
		t.Fatal("generic retry dropped business payload fields or charged")
	}
}
