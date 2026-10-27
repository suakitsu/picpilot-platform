package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func commerceRouteURL(path string) *url.URL { return &url.URL{Path: path} }

func (s *Server) registerCommerceRoutes() {
	if _, pattern := s.mux.Handler(&http.Request{Method: "GET", URL: commerceRouteURL("/api/commerce-suite/catalog")}); pattern == "GET /api/commerce-suite/catalog" {
		return
	}
	s.mux.HandleFunc("GET /api/commerce-suite/catalog", s.auth(s.commerceCatalog))
	s.mux.HandleFunc("GET /api/commerce-suite/platforms", s.auth(s.commercePlatforms))
	s.mux.HandleFunc("GET /api/commerce-suite/templates", s.auth(s.commerceTemplates))
	s.mux.HandleFunc("POST /api/commerce-suite/estimate", s.auth(s.commerceEstimate))
	s.mux.HandleFunc("POST /api/commerce-suite/generate", s.auth(s.commerceGenerate))
	s.mux.HandleFunc("POST /api/commerce-suite/projects", s.auth(s.commerceGenerate))
	s.registerCommerceObjectRoutes("/api/commerce-suite", "commerce_suite")
	s.registerAdminCommerceRoutes()
}

func (s *Server) registerCommerceObjectRoutes(base, kind string) {
	s.mux.HandleFunc("GET "+base+"/projects/{projectId}", s.auth(s.commerceGetProject(kind)))
	s.mux.HandleFunc("DELETE "+base+"/projects/{projectId}", s.auth(s.commerceDeleteProject(kind)))
	s.mux.HandleFunc("GET "+base+"/tasks/{taskId}", s.auth(s.commerceGetTask(kind)))
	s.mux.HandleFunc("POST "+base+"/tasks/{taskId}/retry", s.auth(s.commerceRetry(kind)))
	s.mux.HandleFunc("POST "+base+"/tasks/{taskId}/cancel", s.auth(s.commerceCancel(kind)))
	s.mux.HandleFunc("DELETE "+base+"/tasks/{taskId}", s.auth(s.commerceDeleteTask(kind)))
}

func (s *Server) commerceCatalog(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CommerceCatalog(r.Context())
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, c)
}
func (s *Server) commercePlatforms(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CommerceCatalog(r.Context())
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []commerce.Platform{}
	for _, p := range c.Platforms {
		if p.Enabled {
			out = append(out, p)
		}
	}
	ok(w, r, out)
}
func (s *Server) commerceTemplates(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CommerceCatalog(r.Context())
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []commerce.Template{}
	platform := r.URL.Query().Get("platform")
	for _, t := range c.Templates {
		if t.Enabled && (platform == "" || platform == t.Platform) {
			out = append(out, t)
		}
	}
	ok(w, r, out)
}

func (s *Server) commerceSelection(ctx context.Context, in commerce.EstimateRequest) (commerce.Catalog, commerce.Platform, []commerce.Template, commerce.Estimate, commerceRuntime, error) {
	c, err := s.Store.CommerceCatalog(ctx)
	if err != nil {
		return c, commerce.Platform{}, nil, commerce.Estimate{}, commerceRuntime{}, err
	}
	runtime, err := s.commerceRuntime(ctx, "commerce_suite")
	if err != nil {
		return c, commerce.Platform{}, nil, commerce.Estimate{}, runtime, err
	}
	if in.Resolution == "" && runtime.config.DefaultSize != "1024x1024" {
		in.Resolution = runtime.defaultResolution()
	}
	p, templates, estimate, err := c.Select(in, s.Config.Edition == config.Personal)
	if err != nil {
		return c, p, templates, estimate, runtime, err
	}
	estimate.CreditsCost = 0
	for i := range estimate.SlotCosts {
		cost := runtime.cost(estimate.SlotCosts[i].CreditsCost, s.Config.Edition == config.Personal)
		if cost > 1000000 {
			return c, p, nil, estimate, runtime, invalidWorkflow()
		}
		estimate.SlotCosts[i].CreditsCost = cost
		estimate.CreditsCost += cost
	}
	return c, p, templates, estimate, runtime, nil
}

func (s *Server) commerceEstimate(w http.ResponseWriter, r *http.Request) {
	var in commerce.EstimateRequest
	if !commerceDecode(w, r, &in, false) {
		return
	}
	_, _, _, estimate, _, err := s.commerceSelection(r.Context(), in)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, estimate)
}

func (s *Server) commerceGenerate(w http.ResponseWriter, r *http.Request) {
	var in commerce.GenerateRequest
	uploads, identity, err := readCommerceInput(w, r, &in, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ctx, user := r.Context(), currentUser(r).ID
	key, err := workflowKey(r, "commerce-suite", identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if s.commerceReplay(w, r, user, key, identity) {
		return
	}
	if err = s.screenUserInput(ctx, user, "", "commerce_suite", key, commerceUserText(map[string]any{"product": in.Product, "overlay": in.OverlayOptions})); err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = in.Product.Normalize(); err != nil {
		s.workflowError(w, r, err)
		return
	}
	c, p, templates, estimate, runtime, err := s.commerceSelection(ctx, commerce.EstimateRequest{Platform: in.Platform, SelectedSlots: in.SelectedSlots, Resolution: in.Resolution})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = runtime.validateRequired(in); err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.requireWorkflowProvider(ctx, user, "image"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	refs, created, err := s.commerceReferences(ctx, user, append(in.ReferenceImageIDs, in.ReferenceAssetIDs...), in.ReferenceURL, uploads)
	defer s.discardCommerceUploads(user, created)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ids := workflowIDs(refs)
	images, err := c.Build(p, templates, estimate, in, ids)
	if err == nil {
		err = runtime.apply(images)
	}
	if err == nil {
		err = s.commerceProviders(ctx, user, images)
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	slots := []string{}
	plan := []map[string]any{}
	for _, im := range images {
		slots = append(slots, im.Slot)
		plan = append(plan, map[string]any{"slot": im.Slot, "name": im.Name, "size": im.Payload.Image.Size, "prompt": im.Payload.Image.Prompt, "visible_text_plan": im.Payload.Snapshot["visibleTextPlan"]})
	}
	original := map[string]any{"source_type": "commerce_suite", "platform": p.Code, "platform_name": p.Name, "product_info": in.Product, "selected_slots": slots, "resolution": estimate.Resolution, "reference_image_ids": ids, "description": strings.Join(in.Product.SellingPoints, "; "), "category": in.Product.Category, "material": in.Product.Material, "dimensions": in.Product.Specs, "keywords": in.Product.SellingPoints}
	facts := map[string]any{"mode": "commerce_suite", "platform": p.Code, "platform_name": p.Name, "product_name": in.Product.ProductName, "brand": in.Product.Brand, "category": in.Product.Category, "material": in.Product.Material, "specs": in.Product.Specs, "selling_points": in.Product.SellingPoints, "selected_slots": slots, "resolution": estimate.Resolution, "credits_cost": estimate.CreditsCost, "overlay_editable": false, "visible_text_rendered_by": "ai_image_model"}
	project := store.Project{UserID: user, SourceType: "commerce_suite", ProductName: in.Product.ProductName, Brand: in.Product.Brand, Country: in.Product.Country, Language: creative.Default(in.Product.Language, p.Language), Status: "images_queued", OriginalInfo: creative.JSON(original), ConfirmedFacts: creative.JSON(facts), GeneratedCopy: creative.JSON(map[string]any{"plan": plan, "plan_source": "catalog_templates"})}
	result, _, err := s.Store.SubmitCommerceBatch(ctx, store.CommerceBatch{Project: project, References: ids, Images: images, Key: key, Identity: identity})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.commerceGenerated(w, r, result)
}

func (s *Server) commerceReplay(w http.ResponseWriter, r *http.Request, user, key string, identity json.RawMessage) bool {
	result, found, err := s.Store.CreativeReplay(r.Context(), user, key, identity)
	if err != nil {
		s.workflowError(w, r, err)
		return true
	}
	if found {
		s.commerceGenerated(w, r, result)
		return true
	}
	return false
}

func (s *Server) commerceTaskView(ctx context.Context, t store.Task) map[string]any {
	v := s.projectTaskView(ctx, t)
	decorateCommerceTaskView(t, v)
	return v
}

func (s *Server) commerceProjectDetail(ctx context.Context, user, id, kind string) (map[string]any, error) {
	p, err := s.Store.Project(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if kind != "" && p.SourceType != kind {
		return nil, store.ErrNotFound
	}
	detail, err := s.projectDetail(ctx, user, id)
	if err != nil {
		return nil, err
	}
	tasks, err := s.Store.ProjectTasks(ctx, user, id)
	if err != nil {
		return nil, err
	}
	views := []any{}
	for _, t := range tasks {
		if t.Capability == "image" {
			views = append(views, s.commerceTaskView(ctx, t))
		}
	}
	detail["image_tasks"] = views
	return detail, nil
}

func (s *Server) commerceGenerated(w http.ResponseWriter, r *http.Request, result store.WorkflowResult) {
	user, ctx := currentUser(r).ID, r.Context()
	detail, err := s.commerceProjectDetail(ctx, user, result.ProjectID, "")
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	// Replay after retries/deletion returns the current linked outputs, not stale
	// hidden task IDs from the original creation receipt.
	cost, err := s.Store.CommerceBatchCost(ctx, user, result.ProjectID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := map[string]any{"project_id": result.ProjectID, "project": detail, "created_tasks": detail["image_tasks"], "credits_cost": cost, "mock_mode": s.Config.DemoMode}
	original := detail["original_info"].(map[string]any)
	if original["source_type"] == "image_translation" {
		out["resolution"] = original["resolution"]
		for _, l := range commerce.Languages() {
			if l.Code == original["target_language"] {
				out["target_language"] = l
				break
			}
		}
	}
	ok(w, r, out)
}

func (s *Server) commerceTask(ctx context.Context, user, id, kind string) (store.Task, error) {
	t, err := s.Store.Task(ctx, user, id)
	if err != nil {
		return t, err
	}
	p, err := s.Store.Project(ctx, user, t.ProjectID)
	if err != nil {
		return t, err
	}
	if p.SourceType != kind || t.Capability != "image" {
		return t, store.ErrNotFound
	}
	return t, nil
}
func (s *Server) commerceGetProject(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.commerceProjectDetail(r.Context(), currentUser(r).ID, r.PathValue("projectId"), kind)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, v)
	}
}
func (s *Server) commerceGetTask(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, err := s.commerceTask(r.Context(), currentUser(r).ID, r.PathValue("taskId"), kind)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, s.commerceTaskView(r.Context(), t))
	}
}
func (s *Server) commerceDeleteProject(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, user, id := r.Context(), currentUser(r).ID, r.PathValue("projectId")
		p, err := s.Store.Project(ctx, user, id)
		if err == nil && p.SourceType != kind {
			err = store.ErrNotFound
		}
		if err == nil {
			err = s.Store.DeleteProject(ctx, user, id)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, map[string]any{"deleted": true})
	}
}
func (s *Server) commerceDeleteTask(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, user := r.Context(), currentUser(r).ID
		t, err := s.commerceTask(ctx, user, r.PathValue("taskId"), kind)
		if err == nil {
			err = s.Store.DeleteProjectFailedTask(ctx, user, t.ProjectID, t.ID)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, map[string]any{"deleted": true})
	}
}
func (s *Server) commerceCancel(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, user := r.Context(), currentUser(r).ID
		t, err := s.commerceTask(ctx, user, r.PathValue("taskId"), kind)
		if err == nil {
			t, err = s.Store.CancelTask(ctx, user, t.ID)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, s.commerceTaskView(ctx, t))
	}
}

func (s *Server) commerceRetry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ReferenceText string `json:"reference_text"`
			Style         string `json:"style"`
		}
		if !commerceDecode(w, r, &in, true) {
			return
		}
		ctx, user, id := r.Context(), currentUser(r).ID, r.PathValue("taskId")
		identity := creative.JSON(in)
		key, err := workflowKey(r, kind+"/retry/"+id, identity, false)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		res, found, err := s.Store.CreativeReplay(ctx, user, key, identity)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		if found {
			if project := r.PathValue("id"); project != "" && project != res.ProjectID {
				s.workflowError(w, r, store.ErrNotFound)
				return
			}
			if len(res.TaskIDs) != 1 {
				s.workflowError(w, r, store.ErrConflict)
				return
			}
			t, err := s.commerceTask(ctx, user, res.TaskIDs[0], kind)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			ok(w, r, s.commerceTaskView(ctx, t))
			return
		}
		old, err := s.commerceTask(ctx, user, id, kind)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		if project := r.PathValue("id"); project != "" && project != old.ProjectID {
			s.workflowError(w, r, store.ErrNotFound)
			return
		}
		if old.Status != "failed" && old.Status != "cancelled" && old.Status != "succeeded" {
			s.workflowError(w, r, &workflowError{409, "TASK_NOT_RETRYABLE", "Resolve or finish the existing task before retrying"})
			return
		}
		if err = s.screenUserInput(ctx, user, old.ProjectID, kind, key, commerceUserText(in)); err != nil {
			s.workflowError(w, r, err)
			return
		}
		var payload commerce.Payload
		if json.Unmarshal(old.Request, &payload) != nil || payload.Validate("image") != nil {
			s.workflowError(w, r, invalidWorkflow())
			return
		}
		if in.Style != "" && kind == "image_translation" {
			if len([]rune(in.Style)) > 1000 {
				s.workflowError(w, r, invalidWorkflow())
				return
			}
			payload.Image.Prompt += "\nRevised translation style (text only, retain non-text pixels): " + in.Style
			payload.Snapshot["style"] = in.Style
		}
		if in.ReferenceText != "" {
			payload.Image.Prompt += "\nRevision instructions: " + in.ReferenceText
		}
		runtime, err := s.commerceRuntime(ctx, kind)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		catalog, err := s.Store.CommerceCatalog(ctx)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		resolution := commerce.Resolution(creative.String(payload.Snapshot["resolution"]), "1K")
		cost := runtime.cost(catalog.CostRule[resolution], s.Config.Edition == config.Personal)
		images := []commerce.PlannedImage{{Slot: old.Kind, Name: old.Title, CreditsCost: cost, Payload: payload}}
		if err = runtime.apply(images); err == nil {
			err = s.commerceProviders(ctx, user, images)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		res, _, err = s.Store.RetryCommerceTask(ctx, user, id, key, identity, images[0].Payload, cost)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		t, err := s.Store.Task(ctx, user, res.TaskIDs[0])
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, s.commerceTaskView(ctx, t))
	}
}

// Generic project retry must use the feature's current enabled/price policy and
// retain its snapshot. Call before the generic handler consumes the request body.
func (s *Server) routeCommerceProjectRetry(w http.ResponseWriter, r *http.Request) bool {
	p, err := s.Store.Project(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil || p.SourceType != "commerce_suite" && p.SourceType != "image_translation" {
		return false
	}
	s.commerceRetry(p.SourceType)(w, r)
	return true
}
