package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) registerProjectRoutes() {
	probe, _ := http.NewRequest(http.MethodGet, "/api/projects", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/projects" {
		return
	}
	routes := map[string]http.HandlerFunc{
		"GET /api/projects": s.projectsList, "POST /api/projects": s.projectsCreate,
		"GET /api/projects/{id}": s.projectsDetail, "DELETE /api/projects/{id}": s.projectsDelete,
		"POST /api/projects/{id}/generate-copy":              s.projectsGenerateCopy,
		"POST /api/projects/{id}/confirm-facts":              s.projectsConfirmFacts,
		"GET /api/projects/{id}/product-images":              s.projectsImages,
		"POST /api/projects/{id}/product-images/link":        s.projectsLinkImage,
		"GET /api/projects/{id}/images":                      s.projectsTasks,
		"POST /api/projects/{id}/images":                     s.projectsGenerateImages,
		"POST /api/projects/{id}/listing-package":            s.projectsPackage,
		"POST /api/projects/{id}/linked-aplus":               s.projectsLinkedAplus,
		"DELETE /api/projects/{id}/images/{taskId}":          s.projectsDeleteTask,
		"POST /api/projects/{id}/images/{taskId}/retry":      s.projectsRetryTask,
		"POST /api/projects/aplus-images":                    s.creativeSingle,
		"POST /api/projects/creative-images":                 s.creativeSingle,
		"GET /api/projects/creative-chats":                   s.creativeChats,
		"GET /api/projects/creative-chats/{chatId}":          s.creativeChat,
		"POST /api/projects/creative-chats/generate":         s.creativeGenerate,
		"POST /api/projects/creative-chats/estimate":         s.creativeEstimate,
		"POST /api/projects/optimize-prompt":                 s.projectsOptimize,
		"POST /api/projects/canvas-text":                     s.projectsCanvasText,
		"POST /api/projects/domestic-selling-points/suggest": s.projectsSellingPoints,
		"GET /api/projects/text-tasks/{taskId}":              s.projectsTextResult,
	}
	for route, handler := range routes {
		if route == "GET /api/projects" || route == "POST /api/projects" {
			s.mux.HandleFunc(route, s.auth(handler))
		}
	}
	// Legacy static subresources cross the {id}/suffix routes. Dispatching this
	// prefix avoids ambiguous ServeMux wildcard patterns; the existing upload
	// route in server.go remains more specific and retains its own handler.
	s.mux.HandleFunc("/api/projects/", s.auth(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		best := -1
		var chosen http.HandlerFunc
		var params map[string]string
		pathMatched := false
		for pattern, handler := range routes {
			method, path, _ := strings.Cut(pattern, " ")
			segments := strings.Split(strings.Trim(path, "/"), "/")
			if len(segments) != len(parts) {
				continue
			}
			score := 0
			values := map[string]string{}
			match := true
			for i, segment := range segments {
				if strings.HasPrefix(segment, "{") {
					if parts[i] == "" {
						match = false
						break
					}
					values[strings.Trim(segment, "{}")] = parts[i]
				} else if segment == parts[i] {
					score |= 1 << (16 - i)
				} else {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			pathMatched = true
			if method != r.Method {
				continue
			}
			if score > best {
				best = score
				chosen = handler
				params = values
			}
		}
		if chosen == nil {
			if pathMatched {
				fail(w, r, 405, "METHOD_NOT_ALLOWED", "Method not allowed")
			} else {
				fail(w, r, 404, "NOT_FOUND", "Route not found")
			}
			return
		}
		for k, v := range params {
			r.SetPathValue(k, v)
		}
		chosen(w, r)
	}))
}

type workflowError struct {
	status        int
	code, message string
}

func (e *workflowError) Error() string { return e.code }
func invalidWorkflow() error {
	return &workflowError{400, "INVALID_REQUEST", "Invalid or oversized workflow input"}
}
func (s *Server) workflowError(w http.ResponseWriter, r *http.Request, err error) {
	var e *workflowError
	if errors.As(err, &e) {
		fail(w, r, e.status, e.code, e.message)
		return
	}
	if errors.Is(err, creative.ErrInvalid) {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid or oversized workflow input")
		return
	}
	s.databaseError(w, r, err)
}
func workflowDecode(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	if optional && (r.Body == nil || r.ContentLength == 0) {
		return true
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(w, r, 415, "JSON_REQUIRED", "Expected application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || creative.DecodeAliases(data, dst) != nil || creative.ValidateFields(dst) != nil {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid or oversized workflow input")
		return false
	}
	return true
}
func workflowMap(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}
func workflowIDs(assets []store.Asset) []string {
	ids := []string{}
	for _, a := range assets {
		if len(ids) == 10 {
			break
		}
		ids = append(ids, a.ID)
	}
	return ids
}
func workflowMain(assets []store.Asset) bool {
	for _, a := range assets {
		if a.ImageType == "main" || a.ImageType == "product_main" {
			return true
		}
	}
	return false
}
func workflowKey(r *http.Request, scope string, identity any, unique bool) (string, error) {
	value := r.Header.Get("Idempotency-Key")
	if value != "" && (len(value) < 8 || len(value) > 128) {
		return "", invalidWorkflow()
	}
	if value == "" {
		if unique {
			value = security.NewID("request")
		} else {
			value = string(creative.JSON(identity))
		}
	}
	sum := sha256.Sum256([]byte(scope + "\x00" + value))
	return "creative:" + hex.EncodeToString(sum[:]), nil
}
func (s *Server) requireWorkflowProvider(ctx context.Context, user, capability string) error {
	if s.Config.DemoMode {
		return nil
	}
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = user
	}
	ps, err := s.Store.Providers(ctx, scope)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Enabled && p.Capability == capability {
			return nil
		}
	}
	return &workflowError{503, "NO_PROVIDER_CONFIGURED", "No enabled provider is configured for this capability"}
}
func (s *Server) projectsCreate(w http.ResponseWriter, r *http.Request) {
	var in creative.CreateProject
	if !workflowDecode(w, r, &in, false) {
		return
	}
	if strings.TrimSpace(in.ProductName) == "" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	key, err := workflowKey(r, "create-project", in, true)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	original := workflowMap(creative.JSON(in))
	delete(original, "product_name")
	delete(original, "brand")
	delete(original, "country")
	delete(original, "language")
	if in.TitleKeywords == nil {
		original["title_keywords"] = in.Keywords
	}
	original["include_aplus"] = in.IncludeAplus != nil && *in.IncludeAplus
	original["aplus_size"], err = s.workflowDefaultImageSize(r.Context(), "aplus", in.AplusSize, "970x600")
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if _, err = s.workflowImageCost(r.Context(), creative.String(original["aplus_size"]), "aplus"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	user := currentUser(r).ID
	res, _, err := s.Store.CreativeWrite(r.Context(), user, key, creative.JSON(in), func(tx pgx.Tx) (store.WorkflowResult, error) {
		p, err := s.Store.CreateProjectTx(r.Context(), tx, store.Project{UserID: user, SourceType: "listing", ProductName: in.ProductName, Brand: in.Brand, Country: creative.Default(in.Country, "US"), Language: creative.Default(in.Language, "en-US"), OriginalInfo: creative.JSON(original)})
		return store.WorkflowResult{ProjectID: p.ID}, err
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	p, err := s.Store.Project(r.Context(), user, res.ProjectID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	summary, err := s.projectSummary(r.Context(), p)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, summary)
}
func (s *Server) projectsList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.Projects(r.Context(), currentUser(r).ID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []any{}
	seenChats := map[string]bool{}
	for _, p := range ps {
		if chat := creative.String(workflowMap(p.Metadata)["source_chat_id"]); chat != "" {
			if seenChats[chat] {
				continue
			}
			seenChats[chat] = true
			summary, err := s.creativeHistorySummary(r.Context(), p.UserID, chat)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			if summary != nil {
				out = append(out, summary)
			}
			continue
		}
		p, err = s.reconcileProjectCopy(r.Context(), p)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		v, err := s.projectSummary(r.Context(), p)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].(map[string]any)["updated_at"].(time.Time).After(out[j].(map[string]any)["updated_at"].(time.Time))
	})
	ok(w, r, out)
}

func (s *Server) creativeHistorySummary(ctx context.Context, user, chatID string) (map[string]any, error) {
	chat, err := s.Store.CreativeChat(ctx, user, chatID)
	if err != nil {
		return nil, err
	}
	versions, err := s.Store.CreativeVersions(ctx, user, chatID)
	if err != nil {
		return nil, err
	}
	summaries := []map[string]any{}
	for _, v := range versions {
		p, err := s.Store.Project(ctx, user, v.ProjectID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		summary, err := s.projectSummary(ctx, p)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	result := map[string]any{}
	for k, v := range summaries[len(summaries)-1] {
		result[k] = v
	}
	for _, field := range []string{"success_images", "failed_images", "running_images", "total_images"} {
		total := 0
		for _, s := range summaries {
			total += s[field].(int)
		}
		result[field] = total
	}
	var cost int64
	updated := chat.UpdatedAt
	for _, s := range summaries {
		cost += s["credits_cost"].(int64)
		if at := s["updated_at"].(time.Time); at.After(updated) {
			updated = at
		}
	}
	previews := []string{}
	reason, failedID := "", ""
	for i := len(summaries) - 1; i >= 0; i-- {
		s := summaries[i]
		if reason == "" {
			reason = creative.String(s["latest_failure_reason"])
			failedID = creative.String(s["latest_failed_task_id"])
		}
		for _, u := range s["preview_urls"].([]string) {
			if len(previews) < 4 {
				previews = append(previews, u)
			}
		}
	}
	result["preview_urls"] = previews
	result["cover_url"] = ""
	if len(previews) > 0 {
		result["cover_url"] = previews[0]
	}
	result["latest_failure_reason"] = reason
	result["latest_failed_task_id"] = failedID
	result["credits_cost"] = cost
	result["product_name"] = chat.Title
	result["language"] = chat.Language
	result["created_at"] = chat.CreatedAt
	result["updated_at"] = updated
	result["source_type"] = "creative_chat"
	result["source_chat_id"] = chat.ID
	result["version_count"] = len(summaries)
	switch {
	case result["running_images"].(int) > 0:
		result["status"] = "images_queued"
	case result["success_images"] == result["total_images"]:
		result["status"] = "images_completed"
	case result["failed_images"] == result["total_images"]:
		result["status"] = "images_failed"
	default:
		result["status"] = "images_partial"
	}
	return result, nil
}
func (s *Server) projectsDetail(w http.ResponseWriter, r *http.Request) {
	v, err := s.projectDetail(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, v)
}
func (s *Server) projectsDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProject(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"deleted": true, "project_id": r.PathValue("id")})
}
func (s *Server) projectsDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProjectFailedTask(r.Context(), currentUser(r).ID, r.PathValue("id"), r.PathValue("taskId")); err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"deleted": true, "hidden": true, "project_id": r.PathValue("id"), "task_id": r.PathValue("taskId")})
}
func (s *Server) projectsConfirmFacts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Facts map[string]any `json:"confirmed_facts"`
	}
	if !workflowDecode(w, r, &in, false) {
		return
	}
	if in.Facts == nil {
		in.Facts = map[string]any{}
	}
	if err := s.Store.SetProjectFacts(r.Context(), currentUser(r).ID, r.PathValue("id"), creative.JSON(in.Facts)); err != nil {
		s.workflowError(w, r, err)
		return
	}
	p, err := s.Store.Project(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	view, err := s.projectSummary(r.Context(), p)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, view)
}
func (s *Server) projectsImages(w http.ResponseWriter, r *http.Request) {
	assets, err := s.Store.ProjectAssets(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []any{}
	for _, a := range assets {
		out = append(out, s.assetView(a))
	}
	ok(w, r, out)
}
func (s *Server) projectsTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.Store.ProjectTasks(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []any{}
	for _, t := range tasks {
		if t.Capability == "image" {
			out = append(out, s.projectTaskView(r.Context(), t))
		}
	}
	ok(w, r, out)
}
func (s *Server) projectTaskView(ctx context.Context, t store.Task) map[string]any {
	v := s.taskView(ctx, t)
	// Project workflows use the legacy image-task status contract.
	// Generic task endpoints retain the internal state names.
	switch t.Status {
	case "succeeded":
		v["status"] = "success"
	case "cancelled":
		v["status"] = "failed"
	case "queued", "running", "failed":
	default:
		v["status"] = "running"
	}
	var payload jobs.Payload
	_ = json.Unmarshal(t.Request, &payload)
	prompt := ""
	if payload.Image != nil {
		if p, err := s.Store.Project(ctx, t.UserID, t.ProjectID); err == nil {
			prompt = publicImagePrompt(p, t, payload.Image.Prompt)
		}
	}
	if len([]rune(prompt)) > creative.MaxField {
		prompt = string([]rune(prompt)[:creative.MaxField])
	}
	v["image_type"] = t.Kind
	v["image_name"] = t.Title
	v["safe_error"] = t.ErrorCode
	snapshot := string(creative.JSON(map[string]any{"prompt": prompt}))
	for len([]rune(snapshot)) > creative.MaxField {
		prompt = string([]rune(prompt)[:len([]rune(prompt))/2])
		snapshot = string(creative.JSON(map[string]any{"prompt": prompt}))
	}
	v["prompt_snapshot"] = snapshot
	delete(v, "provider")
	delete(v, "model")
	if v["result_url"] == nil {
		v["result_url"] = ""
	}
	decorateCommerceTaskView(t, v)
	return v
}
func (s *Server) projectSummary(ctx context.Context, p store.Project) (map[string]any, error) {
	tasks, err := s.Store.ProjectTasks(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	success, failed, running, total := 0, 0, 0, 0
	var cost int64
	latestFailure, latestID := "", ""
	previews := []string{}
	updated := p.UpdatedAt
	for _, t := range tasks {
		if t.Capability != "image" {
			continue
		}
		if t.UpdatedAt.After(updated) {
			updated = t.UpdatedAt
		}
		total++
		cost += t.CreditsCost
		switch t.Status {
		case "succeeded":
			success++
			v := s.projectTaskView(ctx, t)
			if u, ok := v["result_url"].(string); ok && u != "" {
				previews = append(previews, u)
			}
		case "failed", "cancelled":
			failed++
			latestFailure = t.ErrorCode
			latestID = t.ID
		default:
			running++
		}
	}
	status := p.Status
	if total > 0 {
		switch {
		case running > 0:
			status = "images_queued"
		case success == total:
			status = "images_completed"
		case failed == total:
			status = "images_failed"
		default:
			status = "images_partial"
		}
	}
	cover := ""
	if len(previews) > 0 {
		cover = previews[0]
	}
	meta := workflowMap(p.Metadata)
	if meta["version_count"] == nil {
		meta["version_count"] = 0
	}
	return map[string]any{"id": p.ID, "brand": p.Brand, "product_name": p.ProductName, "country": p.Country, "language": p.Language, "status": status, "success_images": success, "failed_images": failed, "running_images": running, "total_images": total, "credits_cost": cost, "latest_failure_reason": latestFailure, "latest_failed_task_id": latestID, "cover_url": cover, "preview_urls": previews, "created_at": p.CreatedAt, "updated_at": updated, "source_type": p.SourceType, "source_chat_id": creative.String(meta["source_chat_id"]), "version_count": meta["version_count"]}, nil
}
func (s *Server) projectDetail(ctx context.Context, user, id string) (map[string]any, error) {
	p, err := s.Store.Project(ctx, user, id)
	if err != nil {
		return nil, err
	}
	p, err = s.reconcileProjectCopy(ctx, p)
	if err != nil {
		return nil, err
	}
	summary, err := s.projectSummary(ctx, p)
	if err != nil {
		return nil, err
	}
	assets, err := s.Store.ProjectAssets(ctx, user, id)
	if err != nil {
		return nil, err
	}
	tasks, err := s.Store.ProjectTasks(ctx, user, id)
	if err != nil {
		return nil, err
	}
	images := []any{}
	for _, a := range assets {
		images = append(images, s.assetView(a))
	}
	views := []any{}
	for _, t := range tasks {
		if t.Capability == "image" {
			views = append(views, s.projectTaskView(ctx, t))
		}
	}
	copyID := creative.String(workflowMap(p.Metadata)["copy_task_id"])
	copyStatus := ""
	copyError := creative.String(workflowMap(p.Metadata)["copy_error"])
	for _, t := range tasks {
		if t.ID == copyID {
			copyStatus = t.Status
			if copyError == "" {
				copyError = t.ErrorCode
			}
		}
	}
	return map[string]any{"project": summary, "original_info": workflowMap(p.OriginalInfo), "generated_copy": workflowMap(p.GeneratedCopy), "confirmed_facts": workflowMap(p.ConfirmedFacts), "product_images": images, "image_tasks": views, "copy_task_id": copyID, "copy_task_status": copyStatus, "copy_error": copyError}, nil
}
func (s *Server) projectsGenerateCopy(w http.ResponseWriter, r *http.Request) {
	ctx, user, id := r.Context(), currentUser(r).ID, r.PathValue("id")
	p, err := s.Store.Project(ctx, user, id)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	assets, err := s.Store.ProjectAssets(ctx, user, id)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	facts := creative.Facts(p.ProductName, p.Brand, p.Country, p.Language, workflowMap(p.OriginalInfo), workflowMap(p.ConfirmedFacts), workflowMain(assets))
	input := creative.Text(creative.ListingPrompt, facts, true)
	if err = s.applyWorkflowSystemPrompt(ctx, &input, "copy/listing", "copy/title"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	creative.AddVision(&input, workflowIDs(assets))
	if creative.ValidateFields(input) != nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	identity := map[string]any{"project_id": id, "facts": facts, "references": workflowIDs(assets)}
	key, err := workflowKey(r, "listing-copy/"+id, identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.screenUserInput(ctx, user, id, "listing_copy", key, projectUserText(p)); err != nil {
		s.workflowError(w, r, err)
		return
	}
	policies, err := s.workflowListingPolicies(ctx)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.requireWorkflowProvider(ctx, user, "text"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	res, _, err := s.Store.CreativeWrite(ctx, user, key, creative.JSON(identity), func(tx pgx.Tx) (store.WorkflowResult, error) {
		locked, err := s.Store.ProjectTx(ctx, tx, user, id)
		if err != nil {
			return store.WorkflowResult{}, err
		}
		t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, ProjectID: id, Capability: "text", Kind: "listing_copy", Title: "Listing copy", Request: creative.JSON(jobs.Payload{Version: 1, Text: &input}), IdempotencyKey: key + ":copy"})
		if err != nil {
			return store.WorkflowResult{}, err
		}
		meta := workflowMap(locked.Metadata)
		meta["copy_task_id"] = t.ID
		meta["copy_error"] = ""
		meta["copy_image_briefs"] = workflowImageBriefs(policies)
		locked.Metadata = creative.JSON(meta)
		locked.Status = "copy_queued"
		if err = s.Store.UpdateProjectTx(ctx, tx, locked); err != nil {
			return store.WorkflowResult{}, err
		}
		return store.WorkflowResult{ProjectID: id, TaskIDs: []string{t.ID}}, nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	p, err = s.Store.Project(ctx, user, id)
	if err == nil {
		p, err = s.reconcileProjectCopy(ctx, p)
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"project_id": id, "status": p.Status, "task_id": res.TaskIDs[0], "generated_copy": workflowMap(p.GeneratedCopy), "safe_error": creative.String(workflowMap(p.Metadata)["copy_error"])})
}
func (s *Server) reconcileProjectCopy(ctx context.Context, p store.Project) (store.Project, error) {
	meta := workflowMap(p.Metadata)
	id := creative.String(meta["copy_task_id"])
	if id == "" || id == creative.String(meta["copy_applied_task_id"]) {
		return p, nil
	}
	t, err := s.Store.Task(ctx, p.UserID, id)
	if err != nil {
		return p, err
	}
	if t.Status != "succeeded" && t.Status != "failed" && t.Status != "cancelled" {
		return p, nil
	}
	assets, err := s.Store.ProjectAssets(ctx, p.UserID, p.ID)
	if err != nil {
		return p, err
	}
	facts := creative.Facts(p.ProductName, p.Brand, p.Country, p.Language, workflowMap(p.OriginalInfo), workflowMap(p.ConfirmedFacts), workflowMain(assets))
	text := creative.String(workflowMap(t.Result)["text"])
	copy, parseErr := creative.ListingCopy(text, facts)
	if copy == nil {
		copy = map[string]any{}
	}
	if briefs, exists := meta["copy_image_briefs"]; exists && parseErr == nil {
		copy["image_briefs"] = briefs
	}
	code := "INVALID_COPY_RESULT"
	if t.Status != "succeeded" {
		code = creative.Default(t.ErrorCode, "TEXT_TASK_FAILED")
	}
	if err = s.Store.ApplyProjectCopy(ctx, p.UserID, p.ID, id, creative.JSON(copy), t.Status == "succeeded" && parseErr == nil, code); err != nil {
		return p, err
	}
	return s.Store.Project(ctx, p.UserID, p.ID)
}
