package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func (s *Server) workflowTextProviders(ctx context.Context, user string) ([]store.Provider, error) {
	scope := "platform"
	if s.Config.Edition == config.Personal {
		if user == "" {
			return []store.Provider{}, nil
		}
		scope = user
	}
	ps, err := s.Store.Providers(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := []store.Provider{}
	for _, p := range ps {
		if p.Enabled && p.Capability == "text" {
			out = append(out, p)
		}
	}
	return out, nil
}

// Explicit selectors constrain eligible attempts, not the entire fallback chain.
func (s *Server) workflowTextSelection(ctx context.Context, user, model, protocol string) error {
	ps, err := s.workflowTextProviders(ctx, user)
	if err != nil {
		return err
	}
	if len(ps) == 0 || model == "" && protocol == "" {
		return nil
	}
	for _, p := range ps {
		effective := p.Protocol
		if effective == "" {
			effective = string(provider.ChatCompletions)
		}
		if (model == "" || model == p.Model) && (protocol == "" || protocol == effective) {
			return nil
		}
	}
	return &workflowError{409, "TEXT_ROUTING_MISMATCH", "No enabled text provider matches the requested model and protocol"}
}

func (s *Server) submitWorkflowText(w http.ResponseWriter, r *http.Request, kind string, input jobs.TextInput, identity any, userText string) {
	ctx, user := r.Context(), currentUser(r).ID
	if err := s.requireWorkflowProvider(ctx, user, "text"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	if creative.ValidateFields(input) != nil || len(input.Messages) > 20 || (len(input.Messages) == 0) == (len(input.Input) == 0) {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	key, err := workflowKey(r, kind, identity, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.screenUserInput(ctx, user, "", kind, key, userText); err != nil {
		s.workflowError(w, r, err)
		return
	}
	t, _, err := s.Store.SubmitTask(ctx, store.SubmitTask{UserID: user, Capability: "text", Kind: kind, Title: kind, Request: creative.JSON(jobs.Payload{Version: 1, Text: &input}), RequestIdentity: creative.JSON(identity), IdempotencyKey: key})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.workflowTextResult(w, r, t)
}

func (s *Server) projectsOptimize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt      string `json:"prompt"`
		Product     string `json:"product_name"`
		Marketplace string `json:"marketplace"`
		Language    string `json:"language"`
	}
	if !workflowDecode(w, r, &in, false) {
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	input := creative.Text(`Rewrite the user's image idea into one executable image prompt, in the requested language. Preserve the subject, target platform, scene, composition, camera, lighting, material and hierarchy. Use Amazon Listing/A+ rules only for Amazon; respect the actual requested platform. Do not invent measurements, certifications, efficacy or compatibility. Preserve verbatim text the user explicitly wants in the image and require readable lettering. Otherwise prohibit unnecessary text, watermarks and gibberish. Keep different product image roles distinct. Return only the final prompt, no explanation or Markdown. Maximum 32000 characters.`, in, false)
	if err := s.applyWorkflowSystemPrompt(r.Context(), &input, "prompt/optimize"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.submitWorkflowText(w, r, "prompt_optimize", input, in, workflowUserText(in))
}

func (s *Server) projectsSellingPoints(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Product  string `json:"product_name"`
		Platform string `json:"platform"`
		Usage    string `json:"usage"`
		Specs    string `json:"specs"`
		Audience string `json:"audience"`
		Language string `json:"language"`
	}
	if !workflowDecode(w, r, &in, false) {
		return
	}
	if strings.TrimSpace(in.Product) == "" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	input := creative.Text(`Suggest concise, differentiated selling points appropriate to the supplied domestic commerce platform, product, audience and language. Treat category-common ideas as suggestions for human confirmation, never verified facts. No invented specifications, medical effects, certifications, sales rankings, superlatives or numerical performance. Return JSON only: {"selling_points":["..."]}, 3 to 8 distinct suggestions, each at most 200 characters.`, in, true)
	if err := s.applyWorkflowSystemPrompt(r.Context(), &input, "copy/domestic-selling-points"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.submitWorkflowText(w, r, "domestic_selling_points", input, in, workflowUserText(in))
}

func (s *Server) projectsCanvasText(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Protocol    string                     `json:"protocol"`
		Model       string                     `json:"model"`
		RequestBody map[string]json.RawMessage `json:"request_body"`
		Feature     string                     `json:"feature"`
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(w, r, 415, "JSON_REQUIRED", "Expected application/json")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil || creative.DecodeAliases(data, &in) != nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	protocol := in.Protocol
	if protocol == "" {
		protocol = string(provider.ChatCompletions)
	}
	if protocol != string(provider.ChatCompletions) && protocol != string(provider.Responses) {
		s.workflowError(w, r, &workflowError{400, "UNSUPPORTED_PROTOCOL", "Use OpenAI chat completions or responses"})
		return
	}
	if err = s.workflowTextSelection(r.Context(), currentUser(r).ID, in.Model, in.Protocol); err != nil {
		s.workflowError(w, r, err)
		return
	}
	input := jobs.TextInput{Model: in.Model, Protocol: provider.Protocol(in.Protocol), Options: map[string]json.RawMessage{}}
	allowed := " temperature top_p max_tokens max_completion_tokens frequency_penalty presence_penalty response_format stop seed reasoning_effort thinking tools tool_choice parallel_tool_calls "
	if protocol == string(provider.Responses) {
		allowed = " instructions temperature top_p max_output_tokens reasoning text tools tool_choice metadata store previous_response_id parallel_tool_calls truncation include "
	}
	for key, value := range in.RequestBody {
		switch key {
		case "messages":
			if protocol != string(provider.ChatCompletions) || json.Unmarshal(value, &input.Messages) != nil {
				s.workflowError(w, r, invalidWorkflow())
				return
			}
		case "input":
			if protocol != string(provider.Responses) {
				s.workflowError(w, r, invalidWorkflow())
				return
			}
			input.Input = value
		case "model":
			var selected string
			if json.Unmarshal(value, &selected) != nil || in.Model != "" && selected != in.Model {
				s.workflowError(w, r, invalidWorkflow())
				return
			}
			if err = s.workflowTextSelection(r.Context(), currentUser(r).ID, selected, in.Protocol); err != nil {
				s.workflowError(w, r, err)
				return
			}
			input.Model = selected
		case "stream":
			if string(value) != "false" {
				s.workflowError(w, r, &workflowError{400, "STREAM_UNSUPPORTED", "Queued text workflows return complete responses"})
				return
			}
		default:
			if !strings.Contains(allowed, " "+key+" ") || key == "tools" && len(value) > 64000 {
				s.workflowError(w, r, invalidWorkflow())
				return
			}
			input.Options[key] = value
		}
	}
	if len(input.Messages) > 20 || (len(input.Messages) == 0) == (len(input.Input) == 0) {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	for _, m := range input.Messages {
		switch m.Role {
		case "system", "developer", "user", "assistant", "tool":
		default:
			s.workflowError(w, r, invalidWorkflow())
			return
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			s.workflowError(w, r, invalidWorkflow())
			return
		}
	}
	userText := workflowMessageUserText(input)
	key, err := workflowKey(r, "canvas_text", in, false)
	if err == nil {
		err = s.screenUserInput(r.Context(), currentUser(r).ID, "", "canvas_text", key, userText)
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	input, err = s.prepareTextInput(r.Context(), currentUser(r).ID, input)
	if err != nil {
		s.workflowError(w, r, &workflowError{400, "INVALID_TEXT_REFERENCE", "Invalid, unsafe or oversized text image reference"})
		return
	}
	if creative.ValidateFields(input) != nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	input, err = s.addCanvasSystemPrompt(r.Context(), input)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	s.submitWorkflowText(w, r, "canvas_text", input, in, userText)
}

func (s *Server) projectsTextResult(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Task(r.Context(), currentUser(r).ID, r.PathValue("taskId"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	switch t.Kind {
	case "prompt_optimize", "domestic_selling_points", "canvas_text", "assistant_chat", "listing_copy":
	default:
		s.workflowError(w, r, store.ErrNotFound)
		return
	}
	s.workflowTextResult(w, r, t)
}

func (s *Server) workflowTextResult(w http.ResponseWriter, r *http.Request, t store.Task) {
	data := map[string]any{"task_id": t.ID, "status": t.Status, "safe_error": t.ErrorCode}
	if t.Status != "succeeded" {
		ok(w, r, data)
		return
	}
	result := workflowMap(t.Result)
	text := creative.String(result["text"])
	if utf8.RuneCountInString(text) > creative.MaxField && t.Kind != "canvas_text" || creative.ValidateFields(result) != nil {
		fail(w, r, 502, "TEXT_RESULT_LIMIT", "Text output exceeds workflow limits")
		return
	}
	switch t.Kind {
	case "prompt_optimize":
		if strings.TrimSpace(text) == "" {
			fail(w, r, 502, "INVALID_TEXT_RESULT", "Text provider returned an invalid result")
			return
		}
		data["optimized_prompt"] = text
	case "domestic_selling_points":
		obj, err := creative.Object(text)
		if err != nil {
			fail(w, r, 502, "INVALID_TEXT_RESULT", "Text provider returned an invalid result")
			return
		}
		points, ok := obj["selling_points"].([]any)
		if !ok || len(points) < 3 || len(points) > 8 {
			fail(w, r, 502, "INVALID_TEXT_RESULT", "Text provider returned invalid selling points")
			return
		}
		values := []string{}
		seen := map[string]bool{}
		for _, p := range points {
			s, ok := p.(string)
			s = strings.TrimSpace(s)
			if !ok || s == "" || utf8.RuneCountInString(s) > 200 || seen[s] {
				fail(w, r, 502, "INVALID_TEXT_RESULT", "Text provider returned invalid selling points")
				return
			}
			seen[s] = true
			values = append(values, s)
		}
		data["selling_points"] = values
		data["text"] = strings.Join(values, "; ")
		data["source"] = "ai_assisted_category_common"
	case "canvas_text":
		payload, ok := result["payload"].(map[string]any)
		if !ok || len(payload) == 0 {
			fail(w, r, 502, "INVALID_TEXT_RESULT", "Text provider returned no structured payload")
			return
		}
		for k, v := range payload {
			if k != "task_id" && k != "status" && k != "safe_error" {
				data[k] = v
			}
		}
		data["text"] = text
		data["payload"] = payload
	case "assistant_chat":
		parsed, err := creative.AssistantResult(text, assistantLatest(t), creative.String(result["model"]))
		if err != nil {
			fail(w, r, 502, "INVALID_ASSISTANT_RESULT", "Assistant returned an invalid response")
			return
		}
		for k, v := range parsed {
			data[k] = v
		}
	case "listing_copy":
		p, err := s.Store.Project(r.Context(), t.UserID, t.ProjectID)
		if err == nil {
			p, err = s.reconcileProjectCopy(r.Context(), p)
		}
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		data["project_id"] = p.ID
		data["status"] = p.Status
		data["generated_copy"] = workflowMap(p.GeneratedCopy)
		data["safe_error"] = creative.String(workflowMap(p.Metadata)["copy_error"])
	}
	ok(w, r, data)
}
