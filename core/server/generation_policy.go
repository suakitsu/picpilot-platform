package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/risk"
)

func (s *Server) screenUserInput(ctx context.Context, userID, projectID, feature, key, text string) error {
	// Hash long composite keys without persisting their contents. The same user
	// action keeps one moderation event even when a browser retries the request.
	digest := sha256.Sum256([]byte(feature + "\x00" + key))
	_, err := (risk.Service{Backend: s.Store}).Check(ctx, risk.Request{UserID: userID, ProjectID: projectID, Feature: feature, RequestKey: hex.EncodeToString(digest[:]), Source: "user_input", Text: text})
	return err
}

func directUserText(payload jobs.Payload) string {
	if payload.Image != nil {
		return payload.Image.Prompt
	}
	if payload.Text == nil {
		return ""
	}
	parts := []string{}
	if instructions, ok := payload.Text.Options["instructions"]; ok {
		parts = append(parts, textContent(instructions)...)
	}
	for _, message := range payload.Text.Messages {
		// Direct API system/developer messages are user-supplied too; only internal
		// workflow composers are exempt from user-input classification.
		parts = append(parts, textContent(message.Content)...)
	}
	if len(payload.Text.Input) > 0 {
		var input any
		if json.Unmarshal(payload.Text.Input, &input) == nil {
			parts = append(parts, textContent(input)...)
		}
	}
	return strings.Join(parts, "\n")
}

func textContent(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case json.RawMessage:
		var parsed any
		if json.Unmarshal(v, &parsed) == nil {
			return textContent(parsed)
		}
	case []any:
		out := []string{}
		for _, item := range v {
			out = append(out, textContent(item)...)
		}
		return out
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return []string{text}
		}
		if content, ok := v["content"]; ok {
			return textContent(content)
		}
	}
	return nil
}
