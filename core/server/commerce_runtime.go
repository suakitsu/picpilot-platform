package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/commerce"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/store"
)

type commerceRuntime struct {
	config store.ImageConfig
	prompt string
}

func (s *Server) commerceRuntime(ctx context.Context, kind string) (commerceRuntime, error) {
	r := commerceRuntime{config: store.ImageConfig{Enabled: true, CreditsCost: 6, DefaultSize: "1024x1024"}}
	c, err := s.Store.RuntimeImageConfig(ctx, kind)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return r, err
	}
	if err == nil {
		r.config = c
	}
	if !r.config.Enabled {
		return r, &workflowError{409, "FEATURE_DISABLED", "This image workflow is disabled"}
	}
	p, err := s.Store.ActivePrompt(ctx, kind)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return r, err
	}
	if err == nil {
		r.prompt = p.Content
	}
	return r, nil
}

func (r commerceRuntime) defaultResolution() string {
	var w, h int
	_, _ = fmt.Sscanf(r.config.DefaultSize, "%dx%d", &w, &h)
	if max(w, h) >= 2800 {
		return "4K"
	}
	if max(w, h) >= 1800 {
		return "2K"
	}
	return "1K"
}

func (r commerceRuntime) cost(base int64, personal bool) int64 {
	if personal {
		return 0
	}
	// The feature base is one legacy 1K unit; catalog resolution weights are
	// authoritative. Round up, never lose a fraction of a configured credit.
	return (base*r.config.CreditsCost + 5) / 6
}

func (r commerceRuntime) validateRequired(value any) error {
	if strings.TrimSpace(r.config.RequiredFields) == "" {
		return nil
	}
	var fields []string
	if json.Unmarshal([]byte(r.config.RequiredFields), &fields) != nil {
		fields = strings.FieldsFunc(r.config.RequiredFields, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' })
	}
	var obj map[string]any
	if json.Unmarshal(creative.JSON(value), &obj) != nil {
		return invalidWorkflow()
	}
	for _, path := range fields {
		var v any = obj
		for _, key := range strings.Split(path, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				v = nil
				break
			}
			v = m[key]
		}
		missing := v == nil
		switch x := v.(type) {
		case string:
			missing = strings.TrimSpace(x) == ""
		case []any:
			missing = len(x) == 0
		}
		if missing {
			return &workflowError{400, "MISSING_REQUIRED_FIELDS", "A configured required product field is missing"}
		}
	}
	return nil
}

func (r commerceRuntime) apply(images []commerce.PlannedImage) error {
	for i := range images {
		p := &images[i].Payload
		if r.prompt != "" {
			p.Image.Prompt += "\nConfigured workflow guidance:\n" + r.prompt
		}
		p.ModelPriority = nil
		for _, m := range []string{r.config.DefaultModel, r.config.FallbackModel} {
			if m != "" && (len(p.ModelPriority) == 0 || p.ModelPriority[0] != m) {
				p.ModelPriority = append(p.ModelPriority, m)
			}
		}
		if p.Validate("image") != nil || creative.ValidateFields(p) != nil {
			return invalidWorkflow()
		}
	}
	return nil
}

// Shared execution calls this immediately after loading providers. It keeps
// attempts, circuit limits, workers and fallback behavior in the common runner.
func commerceImageProviders(t store.Task, providers []store.Provider) []store.Provider {
	var p commerce.Payload
	if json.Unmarshal(t.Request, &p) != nil || p.Image == nil || len(p.ModelPriority) == 0 {
		return providers
	}
	out := []store.Provider{}
	for _, model := range p.ModelPriority {
		for _, candidate := range providers {
			if candidate.Model == model {
				out = append(out, candidate)
			}
		}
	}
	return out
}

func commerceImageProviderAllowed(t store.Task, p store.Provider) bool {
	return len(commerceImageProviders(t, []store.Provider{p})) > 0
}

func (s *Server) commerceProviders(ctx context.Context, user string, images []commerce.PlannedImage) error {
	if err := s.requireWorkflowProvider(ctx, user, "image"); err != nil {
		return err
	}
	if s.Config.DemoMode || len(images) == 0 || len(images[0].Payload.ModelPriority) == 0 {
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
	for _, p := range commerceImageProviders(store.Task{Request: creative.JSON(images[0].Payload)}, ps) {
		if p.Enabled && p.Capability == "image" {
			return nil
		}
	}
	return &workflowError{409, "IMAGE_ROUTING_MISMATCH", "No enabled image provider matches the configured models"}
}

// Shared projectTaskView calls this last, preserving legacy polling metadata.
// Internal task states remain unchanged; only the legacy presentation is mapped.
func decorateCommerceTaskView(t store.Task, v map[string]any) {
	var p commerce.Payload
	if json.Unmarshal(t.Request, &p) != nil || p.Image == nil || p.Snapshot == nil {
		return
	}
	kind, _ := p.Snapshot["taskKind"].(string)
	if kind != "commerce_suite" && kind != "image_translation" {
		return
	}
	snapshot := commerce.CopyObject(p.Snapshot)
	prompt := p.Image.Prompt
	if len([]rune(prompt)) > 8000 {
		prompt = string([]rune(prompt)[:8000])
	}
	snapshot["prompt"], snapshot["modelPrompt"] = prompt, prompt
	raw := creative.JSON(snapshot)
	if len([]rune(string(raw))) > creative.MaxField {
		delete(snapshot, "productInfo")
		delete(snapshot, "visibleTextPlan")
		raw = creative.JSON(snapshot)
	}
	// Preserve source IDs/indexes even with unusually long admin-supplied labels.
	if len([]rune(string(raw))) > creative.MaxField {
		for key, value := range snapshot {
			if text, ok := value.(string); ok && len([]rune(text)) > 1000 {
				snapshot[key] = string([]rune(text)[:1000])
			}
		}
		raw = creative.JSON(snapshot)
	}
	v["prompt_snapshot"] = string(raw)
	switch t.Status {
	case "succeeded":
		v["status"] = "success"
	case "cancelled":
		v["status"] = "failed"
	case "queued", "running", "failed":
	default:
		v["status"] = "running"
	}
}
