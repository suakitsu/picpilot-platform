package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func (s *Server) workflowImageCost(ctx context.Context, size, kind string) (int64, error) {
	if kind == "aplus" {
		switch size {
		case "970x600", "300x300", "600x180", "300x400", "350x135", "135x135", "220x220", "970x300", "150x300":
			return s.imageCost(ctx, "1024x1024")
		}
	}
	cost, err := s.imageCost(ctx, size)
	if err != nil {
		return 0, &workflowError{400, "INVALID_IMAGE_SIZE", "Unsupported image size"}
	}
	return cost, nil
}
func (s *Server) projectsGenerateImages(w http.ResponseWriter, r *http.Request) {
	s.projectsEnqueuePackage(w, r, creative.PackageRequest{}, false, false)
}
func (s *Server) projectsPackage(w http.ResponseWriter, r *http.Request) {
	var in creative.PackageRequest
	if !workflowDecode(w, r, &in, true) {
		return
	}
	s.projectsEnqueuePackage(w, r, in, true, false)
}
func (s *Server) projectsLinkedAplus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt   string `json:"prompt"`
		Size     string `json:"size"`
		Language string `json:"language"`
		Module   string `json:"module"`
	}
	if !workflowDecode(w, r, &in, true) {
		return
	}
	yes := true
	s.projectsEnqueuePackage(w, r, creative.PackageRequest{IncludeAplus: &yes, AplusPrompt: in.Prompt, AplusSize: in.Size, AplusModule: in.Module, Language: in.Language}, true, true)
}
func (s *Server) projectsEnqueuePackage(w http.ResponseWriter, r *http.Request, in creative.PackageRequest, packageView, onlyAplus bool) {
	ctx, user, id := r.Context(), currentUser(r).ID, r.PathValue("id")
	p, err := s.Store.Project(ctx, user, id)
	if err == nil {
		p, err = s.reconcileProjectCopy(ctx, p)
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	assets, err := s.Store.ProjectAssets(ctx, user, id)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if len(assets) > 10 {
		s.workflowError(w, r, &workflowError{400, "TOO_MANY_REFERENCES", "Choose at most ten product images"})
		return
	}
	if err = s.requireWorkflowProvider(ctx, user, "image"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	original := workflowMap(p.OriginalInfo)
	include, _ := original["include_aplus"].(bool)
	if in.IncludeAplus != nil {
		include = *in.IncludeAplus
	}
	if !packageView {
		include = false
	}
	identity := map[string]any{"project_id": id, "input": in, "only_aplus": onlyAplus, "package": packageView}
	key, err := workflowKey(r, "listing-package/"+id, identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.screenUserInput(ctx, user, id, "listing_package", key, workflowUserText(projectUserText(p), in.AplusPrompt, in.AplusModule, in.Language)); err != nil {
		s.workflowError(w, r, err)
		return
	}
	var policies []workflowImagePolicy
	if !onlyAplus {
		policies, err = s.workflowListingPolicies(ctx)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		for _, policy := range policies {
			if err = s.requireWorkflowImageProvider(ctx, user, policy); err != nil {
				s.workflowError(w, r, err)
				return
			}
		}
	}
	var aplusPolicy workflowImagePolicy
	if include {
		aplusPolicy, err = s.workflowImagePolicy(ctx, "aplus", creative.ImageRequest{Size: creative.Default(in.AplusSize, creative.String(original["aplus_size"]))}, "970x600", "A+ module")
		if err == nil {
			err = s.requireWorkflowImageProvider(ctx, user, aplusPolicy)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
	}
	result, _, err := s.Store.CreativeWrite(ctx, user, key, creative.JSON(identity), func(tx pgx.Tx) (store.WorkflowResult, error) {
		locked, err := s.Store.ProjectTx(ctx, tx, user, id)
		if err != nil {
			return store.WorkflowResult{}, err
		}
		if !locked.UpdatedAt.Equal(p.UpdatedAt) {
			return store.WorkflowResult{}, store.ErrConflict
		}
		original = workflowMap(locked.OriginalInfo)
		// A missing flag means retain the user's project-level selection.
		include, _ = original["include_aplus"].(bool)
		if in.IncludeAplus != nil {
			include = *in.IncludeAplus
		}
		if !packageView {
			include = false
		}
		res := store.WorkflowResult{ProjectID: id, TaskIDs: []string{}}
		tasks, err := s.Store.ProjectTasksTx(ctx, tx, user, id)
		if err != nil {
			return res, err
		}
		for _, t := range tasks {
			if t.Capability == "image" {
				res.TaskIDs = append(res.TaskIDs, t.ID)
			}
		}
		if !onlyAplus && len(res.TaskIDs) == 0 {
			if len(policies) == 0 {
				return res, &workflowError{409, "IMAGE_TYPES_DISABLED", "All listing image types are disabled"}
			}
			if len(workflowMap(locked.GeneratedCopy)) == 0 {
				return res, &workflowError{409, "COPY_REQUIRED", "Generate and review listing copy first"}
			}
			if !workflowMain(assets) {
				return res, &workflowError{409, "MAIN_IMAGE_REQUIRED", "Upload a main product image first"}
			}
			facts := creative.Facts(locked.ProductName, locked.Brand, locked.Country, locked.Language, original, workflowMap(locked.ConfirmedFacts), true)
			locked.ConfirmedFacts = creative.JSON(facts)
			for _, policy := range policies {
				publicPrompt, err := creative.ImagePrompt(policy.Kind, creative.ImageRequest{Prompt: creative.String(original["visual_prompt"]), Size: policy.Size, Language: locked.Language}, facts, workflowMap(locked.GeneratedCopy), nil)
				if err != nil {
					return res, err
				}
				prompt, err := workflowImagePrompt(publicPrompt, policy)
				if err != nil {
					return res, err
				}
				payload := jobs.Payload{Version: 1, Image: &jobs.ImageInput{Model: policy.Model, FallbackModel: policy.FallbackModel, Prompt: prompt, Size: policy.Size, ReferenceAssetIDs: workflowIDs(assets)}}
				t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, ProjectID: id, Capability: "image", Kind: policy.Kind, Title: policy.Name, Request: creative.JSON(payload), IdempotencyKey: key + ":" + policy.Kind, CreditsCost: policy.Cost})
				if err != nil {
					return res, err
				}
				res.TaskIDs = append(res.TaskIDs, t.ID)
				rememberPublicImagePrompt(&locked, t.ID, publicPrompt)
			}
			copy := workflowMap(locked.GeneratedCopy)
			copy["image_briefs"] = workflowImageBriefs(policies)
			locked.GeneratedCopy = creative.JSON(copy)
			locked.Status = "images_queued"
		}
		if packageView {
			original["include_aplus"] = include
		}
		if include {
			linkedID := creative.String(original["linked_aplus_project_id"])
			if linkedID != "" {
				if linked, err := s.Store.ProjectTx(ctx, tx, user, linkedID); err == nil {
					res.AplusID = linked.ID
				} else if err != store.ErrNotFound {
					return res, err
				}
			}
			if res.AplusID == "" {
				prompt := creative.Default(in.AplusPrompt, creative.Default(creative.String(original["aplus_prompt"]), locked.ProductName+" Amazon A+ module"))
				module := creative.Default(in.AplusModule, creative.Default(creative.String(original["aplus_module"]), "Core benefits"))
				req := creative.ImageRequest{ProductName: locked.ProductName + " A+", Brand: locked.Brand, Prompt: prompt, Size: aplusPolicy.Size, Language: creative.Default(in.Language, locked.Language), Module: module}
				linked, task, err := s.createImageProjectTx(ctx, tx, user, key+":aplus", req, aplusPolicy, workflowIDs(assets), creative.Facts(locked.ProductName, locked.Brand, locked.Country, locked.Language, original, workflowMap(locked.ConfirmedFacts), workflowMain(assets)), nil)
				if err != nil {
					return res, err
				}
				_ = task
				linkedOriginal := workflowMap(linked.OriginalInfo)
				linkedOriginal["linked_from_project_id"] = id
				linkedOriginal["linked_from_product_name"] = locked.ProductName
				linked.OriginalInfo = creative.JSON(linkedOriginal)
				if err = s.Store.UpdateProjectTx(ctx, tx, linked); err != nil {
					return res, err
				}
				res.AplusID = linked.ID
				original["linked_aplus_project_id"] = linked.ID
				original["linked_aplus_product_name"] = linked.ProductName
				original["aplus_size"] = aplusPolicy.Size
				original["aplus_module"] = module
				original["aplus_prompt"] = prompt
			}
		}
		locked.OriginalInfo = creative.JSON(original)
		if err = s.Store.UpdateProjectTx(ctx, tx, locked); err != nil {
			return res, err
		}
		return res, nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	var aplus any
	if result.AplusID != "" {
		aplus, err = s.projectDetail(ctx, user, result.AplusID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
	}
	if onlyAplus {
		ok(w, r, aplus)
		return
	}
	views := []any{}
	for _, id := range result.TaskIDs {
		t, err := s.Store.Task(ctx, user, id)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		views = append(views, s.projectTaskView(ctx, t))
	}
	if !packageView {
		ok(w, r, views)
		return
	}
	status := "not_requested"
	if result.AplusID != "" {
		status = "queued"
		if detail, ok := aplus.(map[string]any); ok {
			if summary, ok := detail["project"].(map[string]any); ok {
				status = creative.String(summary["status"])
				switch status {
				case "images_queued":
					status = "queued"
				case "images_completed":
					status = "succeeded"
				case "images_failed":
					status = "failed"
				case "images_partial":
					status = "partial"
				}
			}
		}
	}
	ok(w, r, map[string]any{"image_tasks": views, "aplus_project": aplus, "aplus_requested": result.AplusID != "", "aplus_status": status})
}

func (s *Server) createImageProjectTx(ctx context.Context, tx pgx.Tx, user, key string, in creative.ImageRequest, policy workflowImagePolicy, refs []string, facts map[string]any, history []string) (store.Project, store.Task, error) {
	kind := policy.Kind
	in.Size = policy.Size
	publicPrompt, err := creative.ImagePrompt(kind, in, facts, nil, history)
	if err != nil {
		return store.Project{}, store.Task{}, err
	}
	prompt, err := workflowImagePrompt(publicPrompt, policy)
	if err != nil {
		return store.Project{}, store.Task{}, err
	}
	original := workflowMap(creative.JSON(in))
	delete(original, "reference_url")
	delete(original, "reference_urls")
	original["reference_asset_ids"] = refs
	p, err := s.Store.CreateProjectTx(ctx, tx, store.Project{UserID: user, SourceType: kind, ProductName: creative.Default(in.ProductName, kind), Brand: in.Brand, Language: creative.Default(in.Language, "zh-CN"), Status: "images_queued", OriginalInfo: creative.JSON(original), ConfirmedFacts: creative.JSON(facts)})
	if err != nil {
		return p, store.Task{}, err
	}
	if err = s.Store.LinkProjectAssetsTx(ctx, tx, user, p.ID, refs, "reference"); err != nil {
		return p, store.Task{}, err
	}
	payload := jobs.Payload{Version: 1, Image: &jobs.ImageInput{Model: policy.Model, FallbackModel: policy.FallbackModel, Prompt: prompt, Size: in.Size, ReferenceAssetIDs: refs}}
	t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, ProjectID: p.ID, Capability: "image", Kind: kind, Title: policy.Name, Request: creative.JSON(payload), IdempotencyKey: key, CreditsCost: policy.Cost})
	if err == nil {
		rememberPublicImagePrompt(&p, t.ID, publicPrompt)
		err = s.Store.UpdateProjectTx(ctx, tx, p)
	}
	return p, t, err
}

func (s *Server) projectsLinkImage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string `json:"image_type"`
		URL  string `json:"url"`
	}
	if !workflowDecode(w, r, &in, false) {
		return
	}
	user, id := currentUser(r).ID, r.PathValue("id")
	if _, err := s.Store.Project(r.Context(), user, id); err != nil {
		s.workflowError(w, r, err)
		return
	}
	asset, err := s.resolveCreativeReference(r.Context(), user, in.URL)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	kind := creative.Default(in.Kind, "main")
	if kind != "main" && kind != "reference" && kind != "detail" && kind != "package" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	key, err := workflowKey(r, "link-image/"+id, in, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	_, _, err = s.Store.CreativeWrite(r.Context(), user, key, creative.JSON(in), func(tx pgx.Tx) (store.WorkflowResult, error) {
		return store.WorkflowResult{ProjectID: id}, s.Store.LinkProjectAssetsTx(r.Context(), tx, user, id, []string{asset.ID}, kind)
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	asset.ImageType = kind
	ok(w, r, s.assetView(asset))
}

func (s *Server) projectsRetryTask(w http.ResponseWriter, r *http.Request) {
	in, uploads, identity, err := readCreativeInput(w, r, true)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ctx, user, project, id := r.Context(), currentUser(r).ID, r.PathValue("id"), r.PathValue("taskId")
	key, err := workflowKey(r, "retry/"+id, identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if replay, found, err := s.Store.CreativeReplay(ctx, user, key, identity); err != nil {
		s.workflowError(w, r, err)
		return
	} else if found {
		if len(replay.TaskIDs) != 1 {
			s.workflowError(w, r, store.ErrConflict)
			return
		}
		task, err := s.Store.Task(ctx, user, replay.TaskIDs[0])
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		ok(w, r, s.projectTaskView(ctx, task))
		return
	}
	old, err := s.Store.Task(ctx, user, id)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if old.ProjectID != project || old.Capability != "image" {
		s.workflowError(w, r, store.ErrNotFound)
		return
	}
	if old.Status != "failed" && old.Status != "cancelled" && old.Status != "succeeded" {
		s.workflowError(w, r, &workflowError{409, "TASK_NOT_RETRYABLE", "Resolve or finish the existing task before submitting another generation"})
		return
	}
	var payload jobs.Payload
	if json.Unmarshal(old.Request, &payload) != nil || payload.Image == nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	p, err := s.Store.Project(ctx, user, project)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if p.SourceType == "commerce_suite" || p.SourceType == "image_translation" {
		s.workflowError(w, r, &workflowError{409, "WORKFLOW_RETRY_REQUIRED", "Retry this task through its source workflow endpoint"})
		return
	}
	if err = s.screenUserInput(ctx, user, project, "image_retry", key, workflowUserText(projectUserText(p), in.Prompt)); err != nil {
		s.workflowError(w, r, err)
		return
	}
	policy, err := s.workflowImagePolicy(ctx, old.Kind, creative.ImageRequest{Size: payload.Image.Size, Model: payload.Image.Model, FallbackModel: payload.Image.FallbackModel}, payload.Image.Size, old.Title)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	payload.Image.Model, payload.Image.FallbackModel = policy.Model, policy.FallbackModel
	refs, err := s.resolveCreativeInputs(ctx, user, in, uploads)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if len(refs) > 0 {
		payload.Image.ReferenceAssetIDs = refs
	}
	publicPrompt := publicImagePrompt(p, old, payload.Image.Prompt)
	if in.Prompt != "" {
		publicPrompt += "\nRevision instructions: " + in.Prompt
	}
	payload.Image.Prompt, err = workflowImagePrompt(publicPrompt, policy)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if creative.ValidateFields(payload) != nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	if err = s.requireWorkflowImageProvider(ctx, user, policy); err != nil {
		s.workflowError(w, r, err)
		return
	}
	result, _, err := s.Store.CreativeWrite(ctx, user, key, identity, func(tx pgx.Tx) (store.WorkflowResult, error) {
		locked, err := s.Store.ProjectTx(ctx, tx, user, project)
		if err != nil {
			return store.WorkflowResult{}, err
		}
		if !locked.UpdatedAt.Equal(p.UpdatedAt) {
			return store.WorkflowResult{}, store.ErrConflict
		}
		current, err := s.Store.ProjectTasksTx(ctx, tx, user, project)
		if err != nil {
			return store.WorkflowResult{}, err
		}
		found := false
		for _, candidate := range current {
			if candidate.ID == id && (candidate.Status == "failed" || candidate.Status == "cancelled" || candidate.Status == "succeeded") {
				found = true
			}
		}
		if !found {
			return store.WorkflowResult{}, store.ErrConflict
		}
		t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, ProjectID: project, Capability: "image", Kind: old.Kind, Title: policy.Name, Request: creative.JSON(payload), RequestIdentity: identity, IdempotencyKey: key, CreditsCost: policy.Cost})
		if err != nil {
			return store.WorkflowResult{}, err
		}
		if err = s.Store.ReplaceCreativeTaskTx(ctx, tx, user, project, id, t.ID, payload.Image.ReferenceAssetIDs, publicPrompt); err != nil {
			return store.WorkflowResult{}, err
		}
		rememberPublicImagePrompt(&locked, t.ID, publicPrompt)
		original := workflowMap(locked.OriginalInfo)
		if in.Prompt != "" {
			original["revision_text"] = creative.String(original["revision_text"]) + "\n" + in.Prompt
		}
		locked.OriginalInfo = creative.JSON(original)
		locked.Status = "images_queued"
		if err = s.Store.UpdateProjectTx(ctx, tx, locked); err != nil {
			return store.WorkflowResult{}, err
		}
		return store.WorkflowResult{ProjectID: project, TaskIDs: []string{t.ID}}, nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	t, err := s.Store.Task(ctx, user, result.TaskIDs[0])
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, s.projectTaskView(ctx, t))
}

func requestedCreativeSize(size, message string) string {
	switch strings.ToUpper(strings.TrimSpace(size)) {
	case "1K":
		return "1024x1024"
	case "2K", "HD":
		return "2048x2048"
	case "4K", "8K", "UHD":
		return "4096x4096"
	}
	if size != "" {
		return size
	}
	text := strings.ToUpper(message)
	if strings.Contains(text, "4K") || strings.Contains(text, "8K") {
		return "4096x4096"
	}
	if strings.Contains(text, "2K") || strings.Contains(text, "HD") {
		return "2048x2048"
	}
	return ""
}
