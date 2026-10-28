package server

import (
	"net/http"

	"github.com/suakitsu/picpilot-platform/core/commerce"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) registerTranslationRoutes() {
	if _, pattern := s.mux.Handler(&http.Request{Method: "GET", URL: commerceRouteURL("/api/image-translation/languages")}); pattern == "GET /api/image-translation/languages" {
		return
	}
	s.mux.HandleFunc("GET /api/image-translation/languages", s.auth(func(w http.ResponseWriter, r *http.Request) { ok(w, r, commerce.Languages()) }))
	s.mux.HandleFunc("POST /api/image-translation/generate", s.auth(s.translationGenerate))
	s.registerCommerceObjectRoutes("/api/image-translation", "image_translation")
}

func (s *Server) translationGenerate(w http.ResponseWriter, r *http.Request) {
	var in commerce.TranslationRequest
	uploads, identity, err := readCommerceInput(w, r, &in, true)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ctx, user := r.Context(), currentUser(r).ID
	key, err := workflowKey(r, "image-translation", identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if s.commerceReplay(w, r, user, key, identity) {
		return
	}
	if err = s.screenUserInput(ctx, user, "", "image_translation", key, commerceUserText(map[string]any{"target_language": in.TargetLanguage, "style": in.Style})); err != nil {
		s.workflowError(w, r, err)
		return
	}
	runtime, err := s.commerceRuntime(ctx, "image_translation")
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if in.Resolution == "" {
		in.Resolution = runtime.defaultResolution()
	}
	language, err := in.Normalize()
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
	refs, created, err := s.commerceReferences(ctx, user, append(in.ReferenceImageIDs, in.ReferenceAssetIDs...), "", uploads)
	defer s.discardCommerceUploads(user, created)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if len(refs) == 0 {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	catalog, err := s.Store.CommerceCatalog(ctx)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	cost := runtime.cost(catalog.CostRule[in.Resolution], s.Config.Edition == config.Personal)
	if cost > 1000000 {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	images := []commerce.PlannedImage{}
	for i, a := range refs {
		im, err := commerce.TranslationImage(in, language, a.ID, a.OriginalFilename, a.Width, a.Height, i, cost)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		images = append(images, im)
	}
	if err = runtime.apply(images); err == nil {
		err = s.commerceProviders(ctx, user, images)
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ids := workflowIDs(refs)
	original := map[string]any{"source_type": "image_translation", "target_language": language.Code, "target_language_name": language.Name, "resolution": in.Resolution, "style": in.Style, "source_count": len(refs), "source_language": "auto", "translation_engine": "direct_ai_image_edit", "edit_scope": "existing_text_only", "reference_image_ids": ids}
	facts := commerce.CopyObject(original)
	facts["credits_cost"] = cost * int64(len(refs))
	facts["mode"] = "image_translation"
	plan := []map[string]any{}
	for _, im := range images {
		plan = append(plan, map[string]any{"image_type": im.Slot, "source_image_id": im.Payload.Snapshot["translationSourceImageId"], "prompt": im.Payload.Image.Prompt})
	}
	project := store.Project{UserID: user, SourceType: "image_translation", ProductName: "Image translation - " + language.Name, Language: language.Code, Status: "images_queued", OriginalInfo: creative.JSON(original), ConfirmedFacts: creative.JSON(facts), GeneratedCopy: creative.JSON(map[string]any{"plan": plan})}
	res, _, err := s.Store.SubmitCommerceBatch(ctx, store.CommerceBatch{Project: project, References: ids, Images: images, Key: key, Identity: identity})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.commerceGenerated(w, r, res)
}
