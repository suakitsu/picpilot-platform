package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/risk"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func commerceFixture(t *testing.T, edition config.Edition) (*Server, string, store.User) {
	t.Helper()
	s, token, u := projectTestServer(t, edition)
	s.Config.DemoMode = true
	s.registerCommerceRoutes()
	s.registerTranslationRoutes()
	if edition == config.Platform {
		if _, err := s.Store.AdjustCredits(context.Background(), u.ID, 1000, "fixture", "commerce-seed", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	return s, token, u
}

func commerceRequestBody() map[string]any {
	return map[string]any{"platform": "tiktok_shop", "product_info": map[string]any{"product_name": "Fixture cup", "material": "steel", "selling_points": []string{"1. Verified feature"}}, "selected_slots": []string{"main_image", "detail"}, "resolution": "1K"}
}

func commerceMultipart(t *testing.T, fields map[string]string, fileField string, files [][]byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range files {
		part, err := writer.CreateFormFile(fileField, "fixture.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func commerceUploadRequest(s *Server, path, token, key, contentType string, data []byte) (int, map[string]any) {
	r := httptest.NewRequest("POST", path, bytes.NewReader(data))
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPostgresCommerceHTTPConcurrentBatchAndPersistentPlan(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	body := commerceRequestBody()
	var wg sync.WaitGroup
	results := make(chan map[string]any, 8)
	statuses := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, out := projectRequest(s, "POST", "/api/commerce-suite/generate", token, "same-http-commerce-batch", body)
			results <- out
			statuses <- code
		}()
	}
	wg.Wait()
	close(results)
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatal("concurrent generate", code)
		}
	}
	id := ""
	for out := range results {
		data := out["data"].(map[string]any)
		if id == "" {
			id = data["project_id"].(string)
		}
		if data["project_id"] != id || data["credits_cost"] != float64(12) || len(data["created_tasks"].([]any)) != 2 {
			t.Fatal("batch response mismatch")
		}
	}
	p, err := s.Store.Project(context.Background(), u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var copy map[string]any
	_ = json.Unmarshal(p.GeneratedCopy, &copy)
	if len(copy["plan"].([]any)) != 2 {
		t.Fatal("plan not durable")
	}
	// A new HTTP handler view after the submission request has ended must still
	// find and execute the queued tasks through the shared Mock runner.
	for i := 0; i < 2; i++ {
		lease, err := s.Store.ClaimTaskCapabilities(context.Background(), "commerce-mock", time.Minute, []string{"image"})
		if err != nil {
			t.Fatal(err)
		}
		s.processHTTPModelTask(context.Background(), lease, nil)
	}
	detail := projectOK(t, s, "GET", "/api/commerce-suite/projects/"+id, token, "", nil)
	project := detail["project"].(map[string]any)
	if project["total_images"] != float64(2) || project["success_images"] != float64(2) || project["running_images"] != float64(0) {
		t.Fatal("incorrect linked counts", project)
	}
	for _, value := range detail["image_tasks"].([]any) {
		task := value.(map[string]any)
		if task["status"] != "success" || task["result_url"] == "" {
			t.Fatal("mock did not complete", task["status"])
		}
	}
	var charges int
	if err = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE change_amount<0").Scan(&charges); err != nil || charges != 1 {
		t.Fatal("not one batch debit", charges, err)
	}
	status, _ := projectRequest(s, "DELETE", "/api/commerce-suite/projects/"+id, token, "", nil)
	if status != 200 {
		t.Fatal("terminal project deletion", status)
	}
	status, _ = projectRequest(s, "GET", "/api/commerce-suite/projects/"+id, token, "", nil)
	if status != 404 {
		t.Fatal("deleted project still visible")
	}
}

func TestPostgresCommerceHTTPMultipartIdempotencyCleanupAndAliases(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	data, contentType := commerceMultipart(t, map[string]string{"platform": "jd", "productInfo": `{"productName":"Fixture"}`, "selectedSlots": `["main_image"]`}, "references", [][]byte{projectPNG(t, 13, 21)})
	var wg sync.WaitGroup
	statuses := make(chan int, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := commerceUploadRequest(s, "/api/commerce-suite/generate", token, "", contentType, data)
			statuses <- code
		}()
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatal("multipart replay", code)
		}
	}
	var assets, projects, tasks int
	if err := s.Store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM assets WHERE user_id=$1),(SELECT count(*) FROM projects),(SELECT count(*) FROM tasks)`, u.ID).Scan(&assets, &projects, &tasks); err != nil {
		t.Fatal(err)
	}
	if assets != 1 || projects != 1 || tasks != 1 {
		t.Fatal("duplicate upload/batch", assets, projects, tasks)
	}
	changed, otherType := commerceMultipart(t, map[string]string{"platform": "jd", "product_info": `{"product_name":"Fixture"}`, "selected_slots": `["main_image"]`}, "references", [][]byte{projectPNG(t, 21, 13)})
	code, _ := commerceUploadRequest(s, "/api/commerce-suite/generate", token, "", otherType, changed)
	if code != 200 {
		t.Fatal("different content rejected")
	}
	if err := s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM projects").Scan(&projects); err != nil || projects != 2 {
		t.Fatal("different file contents deduplicated by filename", err)
	}
}

func TestPostgresCommerceAdminRuntimeTemplatesConfigAndAuthorization(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	ctx := context.Background()
	_, nonadmin := adminHTTPUser(t, s, "catalog-reader@example.invalid")
	status, _ := projectRequest(s, "POST", "/api/admin/commerce-suite/catalog", nonadmin, "", commerce.DefaultCatalog())
	if status != 403 {
		t.Fatal("unprivileged catalog edit", status)
	}
	updated := projectOK(t, s, "PUT", "/api/admin/commerce-suite/templates/tiktok_shop-main_image", token, "", map[string]any{"promptTemplate": "Custom fixture {{product_name}}", "providerParams": map[string]any{"quality": "high", "n": 1}})
	if updated["prompt_template"] != "Custom fixture {{product_name}}" {
		t.Fatal("partial template edit ignored")
	}
	name, scene, content := "Fixture guidance", "commerce_suite", "Trusted fixture workflow guidance"
	if _, err := s.Store.AdminSavePrompt(ctx, u.ID, "", "fixture", store.AdminPromptInput{Name: &name, Scene: &scene, Content: &content}); err != nil {
		t.Fatal(err)
	}
	base := int64(12)
	required := json.RawMessage(`"product_info.material"`)
	if _, err := s.Store.AdminSaveImageConfig(ctx, u.ID, "ic_commerce_suite", "fixture", store.AdminImageConfigInput{CreditsCost: &base, RequiredFields: required}); err != nil {
		t.Fatal(err)
	}
	body := commerceRequestBody()
	body["resolution"] = "2K"
	estimate := projectOK(t, s, "POST", "/api/commerce-suite/estimate", token, "", map[string]any{"platform": "tiktok_shop", "selectedSlots": []string{"main_image", "detail"}, "resolution": "2K"})
	generated := projectOK(t, s, "POST", "/api/commerce-suite/generate", token, "configured-commerce", body)
	if estimate["credits_cost"] != float64(96) || generated["credits_cost"] != estimate["credits_cost"] {
		t.Fatal("configured estimate differs from charge")
	}
	tasks, err := s.Store.ProjectTasks(ctx, u.ID, generated["project_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		var p commerce.Payload
		_ = json.Unmarshal(task.Request, &p)
		if !strings.Contains(p.Image.Prompt, content) {
			t.Fatal("enabled runtime prompt ignored")
		}
		if task.Kind == "main_image" && (!strings.Contains(p.Image.Prompt, "Custom fixture") || p.Image.Quality != "high") {
			t.Fatal("template/provider parameters ignored")
		}
	}
	body["product_info"] = map[string]any{"product_name": "Missing material"}
	status, _ = projectRequest(s, "POST", "/api/commerce-suite/generate", token, "missing-required-material", body)
	if status != 400 {
		t.Fatal("required config ignored", status)
	}
	enabled := false
	if _, err = s.Store.AdminSaveImageConfig(ctx, u.ID, "ic_commerce_suite", "disabled", store.AdminImageConfigInput{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	status, _ = projectRequest(s, "POST", "/api/commerce-suite/estimate", token, "", map[string]any{"platform": "jd"})
	if status != 409 {
		t.Fatal("disabled feature ignored", status)
	}
	status, _ = projectRequest(s, "PUT", "/api/admin/commerce-suite/templates/tiktok_shop-main_image", token, "", map[string]any{"provider_params": map[string]any{"api_key": "fixture-only"}})
	if status != 400 {
		t.Fatal("unknown credential config accepted", status)
	}
}

func TestPostgresCommerceRiskUsesOnlyRawUserTextAndStableOperation(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	ctx := context.Background()
	c := risk.Config{Enabled: true, BanThreshold: 100, BlockLevels: []string{"high"}, WarnLevels: []string{}, Keywords: map[string][]string{"high": {"fixture-blocked", "source of truth"}, "medium": {}, "low": {}}}
	if err := s.Store.PutSetting(ctx, "risk_control", creative.JSON(c), u.ID); err != nil {
		t.Fatal(err)
	}
	// "source of truth" is generated template text, not user input.
	projectOK(t, s, "POST", "/api/commerce-suite/generate", token, "safe-raw-user-input", commerceRequestBody())
	bad := commerceRequestBody()
	bad["product_info"] = map[string]any{"product_name": "fixture-blocked"}
	for i := 0; i < 3; i++ {
		code, _ := projectRequest(s, "POST", "/api/commerce-suite/generate", token, "blocked-stable-operation", bad)
		if code != 422 {
			t.Fatal("blocked user input reached generation", code)
		}
	}
	var events, projects int
	if err := s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_risk_events WHERE user_id=$1),(SELECT count(*) FROM projects)`, u.ID).Scan(&events, &projects); err != nil {
		t.Fatal(err)
	}
	if events != 1 || projects != 1 {
		t.Fatal("risk duplicate or charged blocked operation", events, projects)
	}
}

func TestCommerceModelRoutingPreferenceAndSnapshot(t *testing.T) {
	p := commerce.Payload{ModelPriority: []string{"preferred", "fallback"}}
	task := store.Task{Request: creative.JSON(p)}
	// Non-image documents must not change unrelated task routing.
	ps := []store.Provider{{ID: "a", Model: "unrelated"}, {ID: "b", Model: "fallback"}, {ID: "c", Model: "preferred"}}
	if len(commerceImageProviders(task, ps)) != 3 {
		t.Fatal("unrelated routing changed")
	}
	images, _ := commerce.TranslationImage(commerce.TranslationRequest{Resolution: "1K", Style: "preserve"}, commerce.Languages()[1], "asset_fixture", "source.png", 13, 21, 0, 6)
	images.Payload.ModelPriority = p.ModelPriority
	task.Request = creative.JSON(images.Payload)
	task.Status = "uncertain"
	selected := commerceImageProviders(task, ps)
	if len(selected) != 2 || selected[0].ID != "c" || selected[1].ID != "b" || commerceImageProviderAllowed(task, ps[0]) {
		t.Fatal("routing constraints ignored")
	}
	v := map[string]any{}
	decorateCommerceTaskView(task, v)
	if v["status"] != "running" || !strings.Contains(v["prompt_snapshot"].(string), "translationSourceImageId") {
		t.Fatal("legacy task snapshot lost")
	}
}
