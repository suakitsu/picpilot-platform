package server

import (
	"context"
	"strings"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func (s *Server) workflowImageSelection(ctx context.Context, user string, input jobs.ImageInput) error {
	for _, model := range []string{input.Model, input.FallbackModel} {
		if len(model) > 160 || strings.TrimSpace(model) != model || strings.ContainsAny(model, "\r\n\x00") {
			return invalidWorkflow()
		}
	}
	if input.Model == "" || s.Config.DemoMode {
		return nil
	}
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = user
	}
	providers, err := s.Store.Providers(ctx, scope)
	if err != nil {
		return err
	}
	for _, p := range providers {
		if p.Enabled && imageProviderMatches(p, input) {
			return nil
		}
	}
	return &workflowError{409, "IMAGE_ROUTING_MISMATCH", "No enabled image provider matches the requested model"}
}

func imageProviderMatches(p store.Provider, input jobs.ImageInput) bool {
	return p.Capability == "image" && (input.Model == "" || p.Model == input.Model || input.FallbackModel != "" && p.Model == input.FallbackModel)
}

// Explicit primary models precede the fallback regardless of provider priority.
// Providers within each group retain their configured priority ordering.
func orderedImageProviders(providers []store.Provider, input jobs.ImageInput) []store.Provider {
	primary := make([]store.Provider, 0, len(providers))
	fallback := make([]store.Provider, 0, len(providers))
	for _, p := range providers {
		if !imageProviderMatches(p, input) {
			continue
		}
		if input.FallbackModel != "" && input.FallbackModel != input.Model && p.Model == input.FallbackModel {
			fallback = append(fallback, p)
		} else {
			primary = append(primary, p)
		}
	}
	return append(primary, fallback...)
}
