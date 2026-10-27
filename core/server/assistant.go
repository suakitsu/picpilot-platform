package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func (s *Server) registerAssistantRoutes() {
	probe, _ := http.NewRequest(http.MethodGet, "/api/assistant/status", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/assistant/status" {
		return
	}
	s.mux.HandleFunc("GET /api/assistant/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || hasSessionCookie(r) {
			s.auth(s.assistantStatus)(w, r)
			return
		}
		s.assistantStatus(w, r)
	})
	s.mux.HandleFunc("POST /api/assistant/chat", s.auth(s.assistantChat))
	s.mux.HandleFunc("GET /api/assistant/tasks/{taskId}", s.auth(s.assistantResult))
}
func hasSessionCookie(r *http.Request) bool {
	_, err := r.Cookie("picpilot_session")
	return err == nil
}
func (s *Server) assistantStatus(w http.ResponseWriter, r *http.Request) {
	user := ""
	if r.Header.Get("Authorization") != "" || hasSessionCookie(r) {
		user = currentUser(r).ID
	}
	ps, err := s.workflowTextProviders(r.Context(), user)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	models := []string{}
	seen := map[string]bool{}
	for _, p := range ps {
		if p.Model != "" && !seen[p.Model] {
			seen[p.Model] = true
			models = append(models, p.Model)
		}
	}
	model := ""
	if len(models) > 0 {
		model = models[0]
	}
	ok(w, r, map[string]any{"enabled": len(ps) > 0 || s.Config.DemoMode, "model": model, "models": models})
}
func (s *Server) assistantChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		PageContext map[string]any `json:"page_context"`
		Locale      string         `json:"locale"`
		Model       string         `json:"model"`
	}
	if !workflowDecode(w, r, &in, false) {
		return
	}
	if len(in.Messages) == 0 || len(in.Messages) > 19 {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	if err := s.workflowTextSelection(r.Context(), currentUser(r).ID, in.Model, ""); err != nil {
		s.workflowError(w, r, err)
		return
	}
	messages := []provider.Message{{Role: "system", Content: creative.JSON(creative.AssistantPrompt + "\nLocale: " + creative.Default(in.Locale, "zh-CN") + "\nUntrusted page context: " + string(creative.JSON(in.PageContext)))}}
	for _, m := range in.Messages {
		if m.Role != "user" && m.Role != "assistant" || strings.TrimSpace(m.Content) == "" {
			s.workflowError(w, r, invalidWorkflow())
			return
		}
		messages = append(messages, provider.Message{Role: m.Role, Content: creative.JSON(m.Content)})
	}
	if in.Messages[len(in.Messages)-1].Role != "user" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	input := jobs.TextInput{Model: in.Model, Messages: messages, Options: map[string]json.RawMessage{"response_format": json.RawMessage(`{"type":"json_object"}`)}}
	if err := s.applyWorkflowSystemPrompt(r.Context(), &input, "assistant/global"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.submitWorkflowText(w, r, "assistant_chat", input, in, workflowMessageUserText(input))
}
func assistantLatest(t store.Task) string {
	var p jobs.Payload
	if json.Unmarshal(t.Request, &p) != nil || p.Text == nil {
		return ""
	}
	if len(p.Text.Messages) == 0 && len(p.Text.Input) > 0 {
		_ = json.Unmarshal(p.Text.Input, &p.Text.Messages)
	}
	for i := len(p.Text.Messages) - 1; i >= 0; i-- {
		if p.Text.Messages[i].Role == "user" {
			var text string
			_ = json.Unmarshal(p.Text.Messages[i].Content, &text)
			return text
		}
	}
	return ""
}
func (s *Server) assistantResult(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Task(r.Context(), currentUser(r).ID, r.PathValue("taskId"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if t.Kind != "assistant_chat" {
		s.workflowError(w, r, store.ErrNotFound)
		return
	}
	s.workflowTextResult(w, r, t)
}
