package jobs

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/suakitsu/picpilot-platform-go/core/provider"
)

// Payload is an internal durable task document, not the Local Worker wire DTO.
// Reference bytes and credentials never belong in this document.
type Payload struct {
	Version int             `json:"version"`
	Image   *ImageInput     `json:"image,omitempty"`
	Text    *TextInput      `json:"text,omitempty"`
	Video   json.RawMessage `json:"video,omitempty"`
}
type ImageInput struct {
	Model             string   `json:"model,omitempty"`
	FallbackModel     string   `json:"fallback_model,omitempty"`
	Prompt            string   `json:"prompt"`
	Size              string   `json:"size"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
	Quality           string   `json:"quality,omitempty"`
	OutputFormat      string   `json:"output_format,omitempty"`
	Background        string   `json:"background,omitempty"`
	Moderation        string   `json:"moderation,omitempty"`
}
type TextInput struct {
	Model    string                     `json:"model,omitempty"`
	Protocol provider.Protocol          `json:"protocol,omitempty"`
	Messages []provider.Message         `json:"messages,omitempty"`
	Input    json.RawMessage            `json:"input,omitempty"`
	Options  map[string]json.RawMessage `json:"options,omitempty"`
}

func (p Payload) Validate(capability string) error {
	count := 0
	if p.Image != nil {
		count++
	}
	if p.Text != nil {
		count++
	}
	if len(p.Video) > 0 {
		count++
	}
	if p.Version != 1 || count != 1 {
		return errors.New("invalid task payload version or capability")
	}
	switch capability {
	case "image":
		if p.Image == nil || strings.TrimSpace(p.Image.Prompt) == "" || len(p.Image.Prompt) > 320000 || len(p.Image.ReferenceAssetIDs) > 10 {
			return errors.New("invalid image input")
		}
		for _, model := range []string{p.Image.Model, p.Image.FallbackModel} {
			if len(model) > 160 || strings.ContainsAny(model, "\r\n\x00") || model != strings.TrimSpace(model) {
				return errors.New("invalid image routing constraint")
			}
		}
	case "text":
		if p.Text == nil || (len(p.Text.Messages) == 0 && len(p.Text.Input) == 0) {
			return errors.New("invalid text input")
		}
		if len(p.Text.Model) > 200 || (p.Text.Protocol != "" && p.Text.Protocol != provider.ChatCompletions && p.Text.Protocol != provider.Responses) {
			return errors.New("invalid text routing constraint")
		}
	case "video":
		if len(p.Video) == 0 || !json.Valid(p.Video) {
			return errors.New("invalid video input")
		}
	default:
		return errors.New("unsupported task capability")
	}
	return nil
}
