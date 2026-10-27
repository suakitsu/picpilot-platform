package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

var chatOptions = words("temperature top_p max_tokens max_completion_tokens frequency_penalty presence_penalty response_format stop seed reasoning_effort thinking tools tool_choice parallel_tool_calls")
var responsesOptions = words("instructions temperature top_p max_output_tokens reasoning text tools tool_choice metadata store previous_response_id parallel_tool_calls truncation include")

func words(s string) map[string]bool {
	m := make(map[string]bool)
	for _, word := range strings.Fields(s) {
		m[word] = true
	}
	return m
}

func (g *Gateway) GenerateText(ctx context.Context, p Provider, r TextRequest) (TextResponse, error) {
	ctx, cancel := g.callContext(ctx, p)
	defer cancel()
	if ctx.Err() != nil {
		return TextResponse{}, contextual(ctx.Err(), NotSubmitted)
	}
	protocol := p.TextProtocol
	if protocol == "" {
		protocol = ChatCompletions
	}
	if protocol != ChatCompletions && protocol != Responses {
		return TextResponse{}, fail(Unsupported, NotSubmitted)
	}
	p.DeepSeek = p.DeepSeek || strings.Contains(strings.ToLower(p.Model), "deepseek")
	// Text protocol auth is Bearer, independent of a shared config's image profile.
	p.ImageProfile = ""
	path, allowed := "/v1/chat/completions", chatOptions
	if protocol == Responses {
		path, allowed = "/v1/responses", responsesOptions
	}
	endpoint, err := g.endpoint(p, path)
	if err != nil {
		return TextResponse{}, err
	}
	body := map[string]any{"model": p.Model, "stream": false}
	var optionBytes int64
	for key, value := range r.Options {
		optionBytes += int64(len(value))
		if optionBytes > g.opts.MaxRequestBytes || key == "tools" && len(value) > 64_000 {
			return TextResponse{}, fail(LimitExceeded, NotSubmitted)
		}
		if !allowed[key] || !json.Valid(value) {
			return TextResponse{}, fail(InvalidRequest, NotSubmitted)
		}
		body[key] = value
	}
	if protocol == ChatCompletions {
		if len(r.Input) != 0 || len(r.Messages) == 0 || len(r.Messages) > 20 {
			return TextResponse{}, fail(InvalidRequest, NotSubmitted)
		}
		budget := contentBudget{}
		requestBytes := optionBytes
		for _, m := range r.Messages {
			requestBytes += int64(len(m.Content)) + int64(len(m.ToolCalls)) + int64(len(m.ToolCallID)) + int64(len(m.Name)) + int64(len(m.Role))
			if requestBytes > g.opts.MaxRequestBytes {
				return TextResponse{}, fail(LimitExceeded, NotSubmitted)
			}
			if !words("system developer user assistant tool")[m.Role] {
				return TextResponse{}, fail(InvalidRequest, NotSubmitted)
			}
			if m.Role == "tool" && m.ToolCallID == "" {
				return TextResponse{}, fail(InvalidRequest, NotSubmitted)
			}
			if nonNull(m.ToolCalls) && (m.Role != "assistant" || !validToolCalls(m.ToolCalls)) {
				return TextResponse{}, fail(InvalidRequest, NotSubmitted)
			}
			if !nonNull(m.Content) && !nonNull(m.ToolCalls) {
				return TextResponse{}, fail(InvalidRequest, NotSubmitted)
			}
			if err := g.validateContent(m.Content, &budget, false); err != nil {
				return TextResponse{}, err
			}
		}
		if p.DeepSeek && budget.images > 0 {
			return TextResponse{}, fail(Unsupported, NotSubmitted)
		}
		body["messages"] = r.Messages
		if p.DeepSeek {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
	} else {
		if int64(len(r.Input))+optionBytes > g.opts.MaxRequestBytes {
			return TextResponse{}, fail(LimitExceeded, NotSubmitted)
		}
		if len(r.Messages) > 0 || !nonNull(r.Input) || !json.Valid(r.Input) {
			return TextResponse{}, fail(InvalidRequest, NotSubmitted)
		}
		if err := g.validateInput(r.Input); err != nil {
			return TextResponse{}, err
		}
		body["input"] = r.Input
		if _, ok := body["store"]; !ok {
			body["store"] = false
		}
	}
	root, err := g.jsonCall(ctx, http.MethodPost, endpoint, body, p, NotSubmitted)
	if err != nil {
		return TextResponse{}, err
	}
	if responseFailure(root) {
		return TextResponse{}, fail(UpstreamFailure, Uncertain)
	}
	var text string
	if protocol == ChatCompletions {
		text, err = validateChatResponse(root)
	} else {
		text, err = validateResponsesResponse(root)
	}
	if err != nil {
		return TextResponse{}, err
	}
	raw, _ := json.Marshal(root)
	if ctx.Err() != nil {
		return TextResponse{}, contextual(ctx.Err(), Uncertain)
	}
	return TextResponse{Text: redact(text, p.APIKey), Payload: sanitizeJSON(raw, p.APIKey)}, nil
}

type contentBudget struct {
	textBytes, imageBytes int
	images                int
}

func (g *Gateway) validateContent(raw json.RawMessage, budget *contentBudget, responses bool) error {
	if !nonNull(raw) {
		return nil
	}
	if !json.Valid(raw) {
		return fail(InvalidRequest, NotSubmitted)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		budget.textBytes += len(s)
	} else {
		parts := rawArray(raw)
		if len(parts) == 0 || len(parts) > 24 {
			return fail(InvalidRequest, NotSubmitted)
		}
		for _, part := range parts {
			obj := rawObject(part)
			typ := rawString(obj["type"])
			switch typ {
			case "text", "input_text", "output_text", "refusal":
				field := "text"
				if typ == "refusal" {
					field = "refusal"
				}
				if json.Unmarshal(obj[field], &s) != nil {
					return fail(InvalidRequest, NotSubmitted)
				}
				budget.textBytes += len(s)
			case "image_url", "input_image":
				if typ == "input_image" && !responses {
					return fail(InvalidRequest, NotSubmitted)
				}
				uri := rawString(obj["image_url"])
				if uri == "" {
					uri = rawString(rawObject(obj["image_url"])["url"])
				}
				if !strings.HasPrefix(uri, "data:image/") {
					return fail(InvalidRequest, NotSubmitted)
				}
				budget.images++
				budget.imageBytes += len(uri)
				if budget.images > 4 || budget.imageBytes > 28_000_000 {
					return fail(LimitExceeded, NotSubmitted)
				}
				data, err := g.decodeBase64(uri, NotSubmitted)
				if err != nil {
					return err
				}
				if _, err := g.inspectImage(data, NotSubmitted); err != nil {
					return err
				}
			default:
				return fail(Unsupported, NotSubmitted)
			}
		}
	}
	if budget.textBytes > 80_000 {
		return fail(LimitExceeded, NotSubmitted)
	}
	return nil
}

func (g *Gateway) validateInput(raw json.RawMessage) error {
	budget := contentBudget{}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if strings.TrimSpace(text) == "" || len(text) > 80_000 {
			return fail(InvalidRequest, NotSubmitted)
		}
		return nil
	}
	items := rawArray(raw)
	if len(items) == 0 || len(items) > 100 {
		return fail(InvalidRequest, NotSubmitted)
	}
	for _, item := range items {
		obj := rawObject(item)
		switch rawString(obj["type"]) {
		case "", "message":
			if !words("system developer user assistant")[rawString(obj["role"])] || !nonNull(obj["content"]) {
				return fail(InvalidRequest, NotSubmitted)
			}
			if err := g.validateContent(obj["content"], &budget, true); err != nil {
				return err
			}
		case "function_call":
			var arguments string
			if rawString(obj["call_id"]) == "" || rawString(obj["name"]) == "" || !nonNull(obj["arguments"]) || json.Unmarshal(obj["arguments"], &arguments) != nil {
				return fail(InvalidRequest, NotSubmitted)
			}
		case "function_call_output":
			if rawString(obj["call_id"]) == "" || !nonNull(obj["output"]) {
				return fail(InvalidRequest, NotSubmitted)
			}
			if err := g.validateContent(obj["output"], &budget, true); err != nil {
				return err
			}
		case "reasoning", "item_reference":
			if rawString(obj["id"]) == "" {
				return fail(InvalidRequest, NotSubmitted)
			}
		default:
			return fail(Unsupported, NotSubmitted)
		}
	}
	return nil
}

func validToolCalls(raw json.RawMessage) bool {
	calls := rawArray(raw)
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		obj := rawObject(call)
		fn := rawObject(obj["function"])
		var args string
		if rawString(obj["id"]) == "" || rawString(obj["type"]) != "function" || rawString(fn["name"]) == "" || !nonNull(fn["arguments"]) || json.Unmarshal(fn["arguments"], &args) != nil {
			return false
		}
	}
	return true
}

func validateChatResponse(root map[string]json.RawMessage) (string, error) {
	choices := rawArray(root["choices"])
	if len(choices) == 0 {
		return "", fail(InvalidResponse, Uncertain)
	}
	var text strings.Builder
	for _, choice := range choices {
		msg := rawObject(rawObject(choice)["message"])
		if msg == nil || (rawString(msg["role"]) != "" && rawString(msg["role"]) != "assistant") {
			return "", fail(InvalidResponse, Uncertain)
		}
		content, meaningful, valid := responseContent(msg["content"])
		if !valid {
			return "", fail(InvalidResponse, Uncertain)
		}
		if nonNull(msg["tool_calls"]) {
			if !validToolCalls(msg["tool_calls"]) {
				return "", fail(InvalidResponse, Uncertain)
			}
			meaningful = true
		}
		if rawString(msg["refusal"]) != "" {
			meaningful = true
		}
		if !meaningful {
			return "", fail(InvalidResponse, Uncertain)
		}
		text.WriteString(content)
	}
	return text.String(), nil
}

func validateResponsesResponse(root map[string]json.RawMessage) (string, error) {
	status := rawString(root["status"])
	if status != "" && status != "completed" && status != "incomplete" {
		return "", fail(InvalidResponse, Uncertain)
	}
	items := rawArray(root["output"])
	if len(items) == 0 {
		return "", fail(InvalidResponse, Uncertain)
	}
	var text strings.Builder
	for _, item := range items {
		obj := rawObject(item)
		typ := rawString(obj["type"])
		if typ == "" {
			return "", fail(InvalidResponse, Uncertain)
		}
		switch typ {
		case "message":
			s, meaningful, valid := responseContent(obj["content"])
			if !valid || !meaningful {
				return "", fail(InvalidResponse, Uncertain)
			}
			text.WriteString(s)
		case "function_call":
			var args string
			if rawString(obj["call_id"]) == "" || rawString(obj["name"]) == "" || !nonNull(obj["arguments"]) || json.Unmarshal(obj["arguments"], &args) != nil {
				return "", fail(InvalidResponse, Uncertain)
			}
		default:
			// Preserve reasoning and provider-defined built-in tool results intact.
			if rawString(obj["id"]) == "" {
				return "", fail(InvalidResponse, Uncertain)
			}
		}
	}
	return text.String(), nil
}

func responseContent(raw json.RawMessage) (string, bool, bool) {
	if !nonNull(raw) {
		return "", false, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, strings.TrimSpace(s) != "", true
	}
	parts := rawArray(raw)
	if parts == nil {
		return "", false, false
	}
	var out strings.Builder
	meaningful := false
	for _, part := range parts {
		obj := rawObject(part)
		typ := rawString(obj["type"])
		if typ == "" {
			return "", false, false
		}
		switch typ {
		case "text", "output_text", "refusal":
			field := "text"
			if typ == "refusal" {
				field = "refusal"
			}
			var value string
			if !nonNull(obj[field]) || json.Unmarshal(obj[field], &value) != nil {
				return "", false, false
			}
			meaningful = meaningful || strings.TrimSpace(value) != ""
			if typ != "refusal" {
				out.WriteString(value)
			}
		default:
			if len(obj) < 2 {
				return "", false, false
			}
			meaningful = true
		}
	}
	return out.String(), meaningful, true
}
