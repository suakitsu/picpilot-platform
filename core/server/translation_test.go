package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/commerce"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/provider"
)

func TestPostgresTranslationOwnedSourcesStyleAndLegacyMultipart(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	ctx := context.Background()
	data, contentType := commerceMultipart(t, map[string]string{"target_language": "ms-MY", "resolution": "2K", "style": "natural"}, "images", [][]byte{projectPNG(t, 9, 16), projectPNG(t, 16, 9)})
	code, out := commerceUploadRequest(s, "/api/image-translation/generate", token, "translation-source-batch", contentType, data)
	if code != 200 {
		t.Fatal("translation generation", code, out["code"])
	}
	res := out["data"].(map[string]any)
	if res["credits_cost"] != float64(48) || res["target_language"].(map[string]any)["code"] != "ms-MY" {
		t.Fatal("translation contract")
	}
	tasks, err := s.Store.ProjectTasks(ctx, u.ID, res["project_id"].(string))
	if err != nil || len(tasks) != 2 {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	for _, task := range tasks {
		var p commerce.Payload
		if json.Unmarshal(task.Request, &p) != nil {
			t.Fatal("payload")
		}
		if len(p.Image.ReferenceAssetIDs) != 1 || p.Snapshot["translationSourceImageId"] != p.Image.ReferenceAssetIDs[0] || p.Snapshot["style"] != "natural" {
			t.Fatal("task mixed source images or lost style")
		}
		if sources[p.Image.ReferenceAssetIDs[0]] {
			t.Fatal("duplicate source")
		}
		sources[p.Image.ReferenceAssetIDs[0]] = true
		if p.Image.Size != "1152x2048" && p.Image.Size != "2048x1152" {
			t.Fatal("aspect size changed", p.Image.Size)
		}
	}
	_, otherToken := adminHTTPUser(t, s, "translation-intruder@example.invalid")
	for id := range sources {
		status, _ := projectRequest(s, "POST", "/api/image-translation/generate", otherToken, "foreign-translation-source", map[string]any{"target_language": "en-US", "reference_image_ids": []string{id}})
		if status != 404 {
			t.Fatal("foreign source accepted", status)
		}
		break
	}
	status, _ := projectRequest(s, "GET", "/api/image-translation/projects/"+res["project_id"].(string), otherToken, "", nil)
	if status != 404 {
		t.Fatal("foreign project exposed")
	}
	status, _ = projectRequest(s, "POST", "/api/image-translation/generate", token, "remote-reference-rejected", map[string]any{"target_language": "en-US", "reference_url": "https://example.invalid/source.png"})
	if status != 400 {
		t.Fatal("translation remote URL accepted")
	}
	status, _ = projectRequest(s, "POST", "/api/image-translation/tasks/"+tasks[0].ID+"/retry", token, "active-task-retry", nil)
	if status != 409 {
		t.Fatal("active task retried", status)
	}
	status, _ = projectRequest(s, "DELETE", "/api/image-translation/projects/"+res["project_id"].(string), token, "", nil)
	if status != 409 {
		t.Fatal("active project deleted", status)
	}
}

func TestPostgresTranslationPersonalMockHTTPPreservesActualPixels(t *testing.T) {
	s, token, u := commerceFixture(t, config.Personal)
	s.Config.DemoMode = false
	ctx := context.Background()
	a := projectAsset(t, s, u, "", "reference", 13, 21)
	body := map[string]any{"targetLanguage": "en-US", "referenceImageIds": []string{a.ID}, "style": "literal"}
	status, _ := projectRequest(s, "POST", "/api/image-translation/generate", token, "personal-no-provider", body)
	if status != 503 {
		t.Fatal("missing own provider ignored", status)
	}
	var calls atomic.Int32
	output := projectPNG(t, 17, 29)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/images/edits" {
			t.Error("translation was not an edit", r.URL.Path)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error("missing multipart edit")
			w.WriteHeader(400)
			return
		}
		images := 0
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				break
			}
			if strings.HasPrefix(part.FormName(), "image") {
				c, _, err := image.DecodeConfig(part)
				if err != nil || c.Width != 13 || c.Height != 21 {
					t.Error("reference cropped/upscaled", c.Width, c.Height)
				}
				images++
			}
			part.Close()
		}
		if images != 1 {
			t.Error("wrong reference count", images)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer upstream.Close()
	projectProvider(t, s, u, "image", upstream.URL)
	res := projectOK(t, s, "POST", "/api/image-translation/generate", token, "personal-translation", body)
	if res["credits_cost"] != float64(0) || calls.Load() != 0 {
		t.Fatal("personal charged or HTTP handler called model")
	}
	// Independent lease execution after the submit request has returned.
	lease, err := s.Store.ClaimTaskCapabilities(ctx, "translation-local-mock", time.Minute, []string{"image"})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.CloseIdleConnections()
	s.processHTTPModelTask(ctx, lease, gateway)
	final := projectOK(t, s, "GET", "/api/image-translation/tasks/"+lease.ID, token, "", nil)
	if final["status"] != "success" || calls.Load() != 1 {
		t.Fatal("translation did not complete", final["status"], final["safe_error"])
	}
	artifact := final["artifact"].(map[string]any)
	if artifact["width"] != float64(17) || artifact["height"] != float64(29) {
		t.Fatal("output resized to requested generation size")
	}
	var count int
	if err = s.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger").Scan(&count); err != nil || count != 0 {
		t.Fatal("personal ledger", err)
	}
	status, _ = projectRequest(s, "GET", "/api/admin/commerce-suite/catalog", token, "", nil)
	if status != 404 {
		t.Fatal("personal commerce admin exposed", status)
	}
}

func TestPostgresTranslationGenericProjectPollingMetadata(t *testing.T) {
	s, token, u := commerceFixture(t, config.Personal)
	ctx := context.Background()
	a := projectAsset(t, s, u, "", "reference", 13, 21)
	res := projectOK(t, s, "POST", "/api/image-translation/generate", token, "generic-polling-translation", map[string]any{"target_language": "en-US", "reference_image_ids": []string{a.ID}})
	lease, err := s.Store.ClaimTaskCapabilities(ctx, "generic-polling-mock", time.Minute, []string{"image"})
	if err != nil {
		t.Fatal(err)
	}
	s.processHTTPModelTask(ctx, lease, nil)
	detail := projectOK(t, s, "GET", "/api/projects/"+res["project_id"].(string), token, "", nil)
	view := detail["image_tasks"].([]any)[0].(map[string]any)
	if view["status"] != "success" || !strings.Contains(view["prompt_snapshot"].(string), "translationSourceImageId") {
		t.Fatal("generic project polling lacks decorateCommerceTaskView hook")
	}
}

func TestPostgresTranslationRetryRefundAndDelete(t *testing.T) {
	s, token, u := commerceFixture(t, config.Platform)
	ctx := context.Background()
	a := projectAsset(t, s, u, "", "reference", 19, 11)
	res := projectOK(t, s, "POST", "/api/image-translation/generate", token, "translation-retry-fixture", map[string]any{"target_language": "de-DE", "reference_asset_ids": []string{a.ID}})
	lease, err := s.Store.ClaimTask(ctx, "translation-failure", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Status != "running" || lease.ProjectID != res["project_id"] {
		t.Fatal("translation claim returned the wrong task or state")
	}
	var batchDebit int64
	if err = s.Store.Pool.QueryRow(ctx, `SELECT change_amount FROM credit_ledger WHERE user_id=$1 AND biz_type='image_translation_generate' AND biz_id=$2`, u.ID, lease.ProjectID).Scan(&batchDebit); err != nil || batchDebit != -lease.CreditsCost {
		t.Fatalf("translation aggregate debit missing before failure: debit=%d allocated=%d err=%v", batchDebit, lease.CreditsCost, err)
	}
	if _, err = s.Store.FailTask(ctx, lease, "MOCK_FAILURE"); err != nil {
		t.Fatalf("FailTask after successful claim and verified aggregate debit: %v", err)
	}
	retry := projectOK(t, s, "POST", "/api/image-translation/tasks/"+lease.ID+"/retry", token, "translation-idempotent-retry", map[string]any{"style": "natural"})
	replayed := projectOK(t, s, "POST", "/api/image-translation/tasks/"+lease.ID+"/retry", token, "translation-idempotent-retry", map[string]any{"style": "natural"})
	if retry["id"] != replayed["id"] {
		t.Fatal("duplicate retry")
	}
	detail := projectOK(t, s, "GET", "/api/image-translation/projects/"+res["project_id"].(string), token, "", nil)
	if detail["project"].(map[string]any)["total_images"] != float64(1) || len(detail["image_tasks"].([]any)) != 1 {
		t.Fatal("retry inflated linked outputs")
	}
	lease, err = s.Store.ClaimTask(ctx, "translation-retry-failure", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.FailTask(ctx, lease, "MOCK_FAILURE"); err != nil {
		t.Fatal(err)
	}
	status, _ := projectRequest(s, "DELETE", "/api/image-translation/tasks/"+lease.ID, token, "", nil)
	if status != 200 {
		t.Fatal("failed translation delete", status)
	}
	user, err := s.Store.UserByID(ctx, u.ID)
	if err != nil || user.Credits != 1000 {
		t.Fatal("failed translations did not refund exact costs", err)
	}
}
