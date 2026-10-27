package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
)

const generatePlanFixture = `{"intent":"generate_image","reply":"","final_prompt":"Three geometric blocks, preserve all edges and the supplied reference identities.","confidence":0.98}`

func TestPostgresCreativeChatQueuedPlanningAndReferenceContinuity(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	a := projectAsset(t, s, u, "", "reference", 11, 17)
	b := projectAsset(t, s, u, "", "reference", 19, 7)
	request := map[string]any{"message": "Generate three geometric blocks", "reference_asset_ids": []string{b.ID, a.ID}, "size": "1024x1024", "style": "clean"}
	queued := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "chat-turn-one", request)
	if queued["status"] != "queued" || queued["planning_task_id"] != queued["task_id"] {
		t.Fatal("creative submission returned a non-task status")
	}
	chat := queued["chat"].(map[string]any)["chat"].(map[string]any)
	chatID := chat["id"].(string)
	if queued["project"] != nil || len(queued["chat"].(map[string]any)["versions"].([]any)) != 0 {
		t.Fatal("planner pretended image was generated")
	}
	var count int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&count)
	if count != 0 {
		t.Fatal("image charged before planning")
	}
	finishProjectText(t, s, queued["task_id"].(string), generatePlanFixture, map[string]any{})
	view := projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	versions := view["versions"].([]any)
	if len(versions) != 1 || len(view["messages"].([]any)) != 2 {
		t.Fatal("planning did not materialize one version and message pair")
	}
	v := versions[0].(map[string]any)
	task, err := s.Store.Task(context.Background(), u.ID, v["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var payload jobs.Payload
	_ = json.Unmarshal(task.Request, &payload)
	if len(payload.Image.ReferenceAssetIDs) != 2 || payload.Image.ReferenceAssetIDs[0] != b.ID || payload.Image.ReferenceAssetIDs[1] != a.ID {
		t.Fatal("reference composer order lost")
	}
	// Completion uses the real asset normalizer and durable task lease, never an API call.
	lease, err := s.Store.ClaimTaskCapabilities(context.Background(), "image-fixture", time.Minute, []string{"image"})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := s.saveImage(context.Background(), u.ID, task.ProjectID, task.ID, "generated", "result.png", bytes.NewReader(projectPNG(t, 1024, 1024)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.CompleteTask(context.Background(), lease, creative.JSON(map[string]any{"artifact_id": generated.ID})); err != nil {
		t.Fatal(err)
	}
	next := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "chat-turn-two", map[string]any{"chat_id": chatID, "message": "Make the background gray"})
	planned, err := s.Store.Task(context.Background(), u.ID, next["task_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(planned.Request)
	if !strings.Contains(raw, "picpilot-asset:"+generated.ID) || strings.Index(raw, generated.ID) > strings.Index(raw, b.ID) || strings.Contains(raw, "data:image") {
		t.Fatal("revision did not inherit result and ordered references")
	}
	finishProjectText(t, s, planned.ID, generatePlanFixture, map[string]any{})
	view = projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	if len(view["versions"].([]any)) != 2 {
		t.Fatal("second version missing")
	}
	projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&count)
	if count != 2 {
		t.Fatal("reconciliation created duplicate images")
	}
	historyStatus, history := projectRequest(s, "GET", "/api/projects", token, "", nil)
	if historyStatus != 200 || len(history["data"].([]any)) != 1 {
		t.Fatal("chat versions were not grouped in history")
	}
	summary := history["data"].([]any)[0].(map[string]any)
	if summary["source_type"] != "creative_chat" || summary["version_count"] != float64(2) || summary["total_images"] != float64(2) {
		t.Fatal("chat aggregate metadata incorrect")
	}
	last := view["versions"].([]any)[1].(map[string]any)
	if _, err = s.Store.CancelTask(context.Background(), u.ID, last["task_id"].(string)); err != nil {
		t.Fatal(err)
	}
	projectOK(t, s, "DELETE", "/api/projects/"+last["project_id"].(string), token, "", nil)
	if code, _ := projectRequest(s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil); code != 404 {
		t.Fatal("chat project deletion left related history visible")
	}
	// Same owned asset cannot be quoted as history from a different chat.
	other := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "different-chat", map[string]any{"message": "Hello"})
	otherID := other["chat"].(map[string]any)["chat"].(map[string]any)["id"].(string)
	status, _ := projectRequest(s, "POST", "/api/projects/creative-chats/generate", token, "forged-chat-history", map[string]any{"chat_id": otherID, "message": "Generate blocks", "reference_urls": []string{"picpilot-asset:" + a.ID}})
	if status != 404 {
		t.Fatal("cross-chat history accepted", status)
	}
}

func TestPostgresCreativeReconcilerAndRegistrationGuards(t *testing.T) {
	s, token, u := projectTestServer(t, config.Personal)
	// Parent registration plus explicit test registration must not panic.
	s.registerProjectRoutes()
	s.registerAssistantRoutes()
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	p := projectOK(t, s, "POST", "/api/projects", token, "background-copy-project", map[string]any{"product_name": "Fixture", "include_aplus": true})
	id := p["id"].(string)
	copy := projectOK(t, s, "POST", "/api/projects/"+id+"/generate-copy", token, "background-copy", nil)
	finishProjectText(t, s, copy["task_id"].(string), projectCopyFixture, map[string]any{})
	chat := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "background-chat", map[string]any{"message": "Draw three blocks"})
	finishProjectText(t, s, chat["task_id"].(string), generatePlanFixture, map[string]any{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.RunCreativeWorkflows(ctx) }()
	defer cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := s.Store.Project(context.Background(), u.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM creative_versions").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if current.Status == "copy_generated" && count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background workflows required browser polling")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal("unexpected shutdown error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reconciler ignored shutdown")
	}
}

func TestPostgresCreativeChatConversationInvalidPlanAndVersionLimit(t *testing.T) {
	s, token, u := projectTestServer(t, config.Platform)
	projectProvider(t, s, u, "text", "https://example.invalid")
	projectProvider(t, s, u, "image", "https://example.invalid")
	_, _ = s.Store.Pool.Exec(context.Background(), "UPDATE users SET credits=60 WHERE id=$1", u.ID)
	queued := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "chat-question", map[string]any{"message": "What can you do?"})
	chatID := queued["chat"].(map[string]any)["chat"].(map[string]any)["id"].(string)
	finishProjectText(t, s, queued["task_id"].(string), `{"intent":"chat","reply":"I can help prepare images and prompts.","final_prompt":"","confidence":0.99}`, map[string]any{})
	view := projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	if len(view["versions"].([]any)) != 0 {
		t.Fatal("conversation generated an image")
	}
	messages := view["messages"].([]any)
	if messages[len(messages)-1].(map[string]any)["content"] != "I can help prepare images and prompts." {
		t.Fatal("conversation reply not persisted")
	}
	invalid := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "invalid-plan", map[string]any{"chat_id": chatID, "message": "Draw a box"})
	finishProjectText(t, s, invalid["task_id"].(string), `{"intent":"generate_image","final_prompt":"","confidence":1}`, map[string]any{})
	view = projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	messages = view["messages"].([]any)
	if messages[len(messages)-1].(map[string]any)["status"] != "failed" {
		t.Fatal("invalid plan hidden as success")
	}
	good := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "valid-plan", map[string]any{"chat_id": chatID, "message": "Draw a box"})
	finishProjectText(t, s, good["task_id"].(string), generatePlanFixture, map[string]any{})
	view = projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	first := view["versions"].([]any)[0].(map[string]any)
	for n := 2; n <= 10; n++ {
		_, err := s.Store.Pool.Exec(context.Background(), `INSERT INTO creative_versions(id,chat_id,version_no,project_id,task_id,user_request,final_prompt,style,size) VALUES($1,$2,$3,$4,$5,'fixture','fixture','','1024x1024')`, "cver_limit_"+string(rune('a'+n)), chatID, n, first["project_id"], first["task_id"])
		if err != nil {
			t.Fatal(err)
		}
	}
	limit := projectOK(t, s, "POST", "/api/projects/creative-chats/generate", token, "limit-plan", map[string]any{"chat_id": chatID, "message": "Draw one more box"})
	finishProjectText(t, s, limit["task_id"].(string), generatePlanFixture, map[string]any{})
	view = projectOK(t, s, "GET", "/api/projects/creative-chats/"+chatID, token, "", nil)
	if len(view["versions"].([]any)) != 10 {
		t.Fatal("version limit violated")
	}
	var count int
	_ = s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tasks WHERE capability='image'").Scan(&count)
	user, _ := s.Store.UserByID(context.Background(), u.ID)
	if count != 1 || user.Credits != 54 {
		t.Fatal("failed planning or version-limit charge leaked", count, user.Credits)
	}
}

func TestCreativeMultipartOrderingAndLimits(t *testing.T) {
	// The streaming reader does not create temporary files and bounds uploads.
	body := "--fixture\r\nContent-Disposition: form-data; name=\"message\"\r\n\r\nDraw blocks\r\n--fixture\r\nContent-Disposition: form-data; name=\"referenceUrls\"\r\n\r\npicpilot-asset:asset_b\r\n--fixture\r\nContent-Disposition: form-data; name=\"referenceUrls\"\r\n\r\npicpilot-asset:asset_a\r\n--fixture--\r\n"
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	in, uploads, identity, err := readCreativeInput(httptest.NewRecorder(), r, false)
	if err != nil || len(uploads) != 0 || len(in.ReferenceURLs) != 2 || in.ReferenceURLs[0] != "picpilot-asset:asset_b" || !json.Valid(identity) {
		t.Fatal("multipart composer order changed", err)
	}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"message":"x","credits":1}`))
	r.Header.Set("Content-Type", "application/json")
	if _, _, _, err = readCreativeInput(httptest.NewRecorder(), r, false); err == nil {
		t.Fatal("unknown field accepted")
	}
}
