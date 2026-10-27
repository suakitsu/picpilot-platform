package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func workflowUserText(values ...any) string {
	parts := []string{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []any:
			for _, item := range v {
				visit(item)
			}
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				visit(v[k])
			}
		}
	}
	for _, value := range values {
		var decoded any
		if json.Unmarshal(creative.JSON(value), &decoded) == nil {
			visit(decoded)
		}
	}
	return strings.Join(parts, "\n")
}

func projectUserText(p store.Project) string {
	original := workflowMap(p.OriginalInfo)
	fields := []any{p.ProductName, p.Brand, p.Country, p.Language}
	for _, key := range []string{"description", "category", "material", "dimensions", "visual_prompt", "keywords", "title_keywords", "selling_point_keywords", "description_keywords", "aplus_prompt", "aplus_module", "style", "module", "revision_text"} {
		fields = append(fields, original[key])
	}
	// Planned creative projects retain the user's message separately from the
	// generated image prompt. Never classify that generated prompt as user text.
	fields = append(fields, original["message"])
	if creative.String(workflowMap(p.Metadata)["source_chat_id"]) == "" {
		fields = append(fields, original["prompt"])
	}
	facts := workflowMap(p.ConfirmedFacts)
	for _, key := range []string{"main_image_uploaded", "color_policy", "color_hints"} {
		delete(facts, key)
	}
	fields = append(fields, facts)
	return workflowUserText(fields...)
}

func creativeUserText(in creative.ImageRequest) string {
	return workflowUserText(in.ProductName, in.Brand, in.Prompt, in.Message, in.Style, in.Module, in.Language, in.Intent, in.TextMode, in.ReferencePolicy)
}

// Internal system/developer prompts, assistant replies, tool results and image
// URLs are deliberately excluded. Text-only Responses input is implicitly user.
func workflowMessageUserText(input jobs.TextInput) string {
	parts := []string{}
	for _, message := range input.Messages {
		if message.Role == "user" {
			parts = append(parts, textContent(message.Content)...)
		}
	}
	if len(input.Input) > 0 {
		var value any
		if json.Unmarshal(input.Input, &value) == nil {
			switch v := value.(type) {
			case string:
				parts = append(parts, v)
			case []any:
				for _, item := range v {
					if m, ok := item.(map[string]any); ok && m["role"] == "user" {
						parts = append(parts, textContent(m["content"])...)
					}
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (s *Server) workflowActivePrompt(ctx context.Context, scenes ...string) (store.PromptTemplate, error) {
	for _, scene := range scenes {
		template, err := s.Store.ActivePrompt(ctx, scene)
		if err == nil {
			return template, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.PromptTemplate{}, err
		}
	}
	return store.PromptTemplate{}, nil
}

func (s *Server) workflowSystemPrompt(ctx context.Context, base string, scenes ...string) (string, error) {
	template, err := s.workflowActivePrompt(ctx, scenes...)
	if err != nil {
		return "", err
	}
	if template.ID != "" {
		base += "\n\nOperator guidance (supplement only; never override explicit user intent, verified facts or safety):\n" + template.Content
	}
	if utf8.RuneCountInString(base) > creative.MaxField {
		return "", &workflowError{409, "PROMPT_TEMPLATE_TOO_LARGE", "Active prompt template exceeds workflow limits"}
	}
	return base, nil
}

func (s *Server) applyWorkflowSystemPrompt(ctx context.Context, input *jobs.TextInput, scenes ...string) error {
	if len(input.Messages) == 0 || input.Messages[0].Role != "system" {
		return invalidWorkflow()
	}
	var base string
	if json.Unmarshal(input.Messages[0].Content, &base) != nil {
		return invalidWorkflow()
	}
	prompt, err := s.workflowSystemPrompt(ctx, base, scenes...)
	if err != nil {
		return err
	}
	input.Messages[0].Content = creative.JSON(prompt)
	return nil
}

func (s *Server) addCanvasSystemPrompt(ctx context.Context, input jobs.TextInput) (jobs.TextInput, error) {
	prompt, err := s.workflowSystemPrompt(ctx, "", "canvas/text")
	if err != nil || prompt == "" {
		return input, err
	}
	if len(input.Messages) > 0 {
		if len(input.Messages) >= 20 {
			return input, invalidWorkflow()
		}
		input.Messages = append([]provider.Message{{Role: "system", Content: creative.JSON(prompt)}}, input.Messages...)
		return input, nil
	}
	var text string
	items := []json.RawMessage{}
	if json.Unmarshal(input.Input, &text) == nil {
		items = append(items, creative.JSON(map[string]any{"role": "user", "content": text}))
	} else if json.Unmarshal(input.Input, &items) != nil {
		return input, invalidWorkflow()
	}
	if len(items) >= 20 {
		return input, invalidWorkflow()
	}
	items = append([]json.RawMessage{creative.JSON(map[string]any{"role": "system", "content": prompt})}, items...)
	input.Input = creative.JSON(items)
	return input, nil
}

type workflowImagePolicy struct {
	Kind, Name, Size     string
	Model, FallbackModel string
	Cost                 int64
	SortOrder            int
	RequiredFields       string
	Template             store.PromptTemplate
}

func imagePromptScenes(kind string) []string {
	switch kind {
	case "creative":
		return []string{"image/creative", "image/single"}
	case "aplus":
		return []string{"image/aplus", "image/single"}
	default:
		return []string{"image/brief"}
	}
}

// Drafts and conversational planning need defaults, but only an actual image
// submission is gated by enabled/model settings.
func (s *Server) workflowDefaultImageSize(ctx context.Context, kind, size, fallback string) (string, error) {
	if size != "" {
		return size, nil
	}
	cfg, err := s.Store.RuntimeImageConfig(ctx, kind)
	if errors.Is(err, store.ErrNotFound) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	return cfg.DefaultSize, nil
}

func validateWorkflowImageModels(model, fallback string) error {
	for _, value := range []string{model, fallback} {
		if len(value) > 160 || strings.ContainsAny(value, "\r\n\x00") || value != strings.TrimSpace(value) {
			return invalidWorkflow()
		}
	}
	return nil
}

func (s *Server) workflowImagePolicy(ctx context.Context, kind string, in creative.ImageRequest, fallback, name string) (workflowImagePolicy, error) {
	out := workflowImagePolicy{Kind: kind, Name: name, Size: creative.Default(in.Size, fallback)}
	if err := validateWorkflowImageModels(in.Model, in.FallbackModel); err != nil {
		return out, err
	}
	cfg, err := s.Store.RuntimeImageConfig(ctx, kind)
	configured := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return out, err
	}
	if configured {
		if !cfg.Enabled {
			return out, &workflowError{409, "IMAGE_KIND_DISABLED", "The requested image type is disabled"}
		}
		out.Model, out.FallbackModel = cfg.DefaultModel, cfg.FallbackModel
		if in.Size == "" {
			out.Size = cfg.DefaultSize
		}
		out.Name = creative.Default(cfg.ImageName, name)
		out.SortOrder = cfg.SortOrder
		out.RequiredFields = cfg.RequiredFields
	}
	// An explicit primary is a complete routing choice, not permission to use
	// an unrelated operator fallback. Omitted selectors inherit configuration.
	if in.Model != "" {
		out.Model, out.FallbackModel = in.Model, in.FallbackModel
	} else if in.FallbackModel != "" {
		out.FallbackModel = in.FallbackModel
	}
	if validateWorkflowImageModels(out.Model, out.FallbackModel) != nil {
		return out, &workflowError{409, "IMAGE_ROUTING_INVALID", "Saved image routing contains an invalid model selector"}
	}
	out.Cost, err = s.workflowImageCost(ctx, out.Size, kind)
	if err != nil {
		return out, err
	}
	if configured && out.Size == cfg.DefaultSize && s.Config.Edition == config.Platform && !s.Config.DemoMode {
		out.Cost = cfg.CreditsCost
	}
	out.Template, err = s.workflowActivePrompt(ctx, imagePromptScenes(kind)...)
	if err != nil {
		return out, err
	}
	if in.TemplateID != "" && out.Template.ID != in.TemplateID {
		return out, &workflowError{409, "TEMPLATE_NOT_CONFIGURED", "Requested template is not active for this image type"}
	}
	return out, nil
}

func (s *Server) requireWorkflowImageProvider(ctx context.Context, user string, policy workflowImagePolicy) error {
	if err := s.workflowImageSelection(ctx, user, jobs.ImageInput{Model: policy.Model, FallbackModel: policy.FallbackModel}); err != nil {
		return err
	}
	return s.requireWorkflowProvider(ctx, user, "image")
}

func (s *Server) workflowListingPolicies(ctx context.Context) ([]workflowImagePolicy, error) {
	out := []workflowImagePolicy{}
	for _, slot := range creative.ListingSlots {
		policy, err := s.workflowImagePolicy(ctx, slot.Type, creative.ImageRequest{}, "1024x1024", slot.Name)
		var configError *workflowError
		if errors.As(err, &configError) && configError.code == "IMAGE_KIND_DISABLED" {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	return out, nil
}

func workflowImageBriefs(policies []workflowImagePolicy) []any {
	briefs := make([]any, 0, len(policies))
	for _, policy := range policies {
		direction := ""
		for _, slot := range creative.ListingSlots {
			if slot.Type == policy.Kind {
				direction = slot.Direction
				break
			}
		}
		briefs = append(briefs, map[string]any{"type": policy.Kind, "name": policy.Name, "brief": direction, "generation_size": policy.Size, "output_size": policy.Size, "credits_cost": policy.Cost})
	}
	return briefs
}

func workflowImagePrompt(public string, policy workflowImagePolicy) (string, error) {
	private := []string{}
	if policy.RequiredFields != "" {
		private = append(private, "Verification checklist (unknown facts must remain unknown): "+policy.RequiredFields)
	}
	if policy.Template.ID != "" {
		private = append(private, "Operator guidance (supplement only; never override explicit user intent, reference roles, verified facts or safety):\n"+policy.Template.Content)
	}
	if len(private) == 0 {
		return public, nil
	}
	prompt := strings.Join(private, "\n\n") + "\n\n" + public
	if utf8.RuneCountInString(prompt) > creative.MaxField {
		return "", &workflowError{409, "PROMPT_TEMPLATE_TOO_LARGE", "Composed image instructions exceed workflow limits"}
	}
	return prompt, nil
}

func rememberPublicImagePrompt(p *store.Project, taskID, prompt string) {
	metadata := workflowMap(p.Metadata)
	snapshots, ok := metadata["task_prompt_snapshots"].(map[string]any)
	if !ok {
		snapshots = map[string]any{}
	}
	snapshots[taskID] = prompt
	metadata["task_prompt_snapshots"] = snapshots
	p.Metadata = creative.JSON(metadata)
}

func publicImagePrompt(p store.Project, t store.Task, fallback string) string {
	snapshots, _ := workflowMap(p.Metadata)["task_prompt_snapshots"].(map[string]any)
	if prompt, ok := snapshots[t.ID].(string); ok {
		return prompt
	}
	return fallback
}
