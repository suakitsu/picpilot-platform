package jobs

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/suakitsu/picpilot-platform/core/provider"
)

var ErrTextProtocol = errors.New("text request cannot be represented by this protocol without loss")

// AdaptTextInput converts the common chat/responses subset for a fallback
// attempt. Unsupported fields are rejected rather than silently discarded.
func AdaptTextInput(in TextInput, target provider.Protocol) (TextInput, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return in, ErrTextProtocol
	}
	var out TextInput
	if json.Unmarshal(data, &out) != nil {
		return in, ErrTextProtocol
	}
	if target == "" {
		target = provider.ChatCompletions
	}
	if target != provider.ChatCompletions && target != provider.Responses {
		return in, ErrTextProtocol
	}
	if (len(in.Messages) > 0) == (len(in.Input) > 0) {
		return in, ErrTextProtocol
	}
	if target == provider.ChatCompletions && len(in.Messages) > 0 || target == provider.Responses && len(in.Input) > 0 {
		return out, nil
	}
	toResponses := target == provider.Responses
	if toResponses {
		items := []json.RawMessage{}
		for _, m := range out.Messages {
			if m.Name != "" {
				return in, ErrTextProtocol
			}
			if m.Role == "tool" {
				var output string
				if m.ToolCallID == "" || json.Unmarshal(m.Content, &output) != nil {
					return in, ErrTextProtocol
				}
				items = append(items, textJSON(map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": output}))
				continue
			}
			if len(m.Content) > 0 && !bytes.Equal(m.Content, []byte("null")) {
				content, err := adaptContent(m.Content, true)
				if err != nil {
					return in, err
				}
				items = append(items, textJSON(map[string]any{"role": m.Role, "content": content}))
			}
			if len(m.ToolCalls) > 0 && !bytes.Equal(m.ToolCalls, []byte("null")) {
				if m.Role != "assistant" {
					return in, ErrTextProtocol
				}
				var calls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				}
				decoder := json.NewDecoder(bytes.NewReader(m.ToolCalls))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&calls) != nil {
					return in, ErrTextProtocol
				}
				for _, c := range calls {
					if c.Type != "function" || c.ID == "" || c.Function.Name == "" {
						return in, ErrTextProtocol
					}
					items = append(items, textJSON(map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Function.Name, "arguments": c.Function.Arguments}))
				}
			}
		}
		if len(items) == 0 {
			return in, ErrTextProtocol
		}
		out.Input = textJSON(items)
		out.Messages = nil
	} else {
		var text string
		if json.Unmarshal(out.Input, &text) == nil {
			out.Messages = []provider.Message{{Role: "user", Content: textJSON(text)}}
		} else {
			var items []map[string]json.RawMessage
			if json.Unmarshal(out.Input, &items) != nil {
				return in, ErrTextProtocol
			}
			for _, item := range items {
				var typ, role string
				_ = json.Unmarshal(item["type"], &typ)
				_ = json.Unmarshal(item["role"], &role)
				switch typ {
				case "", "message":
					if role == "" {
						return in, ErrTextProtocol
					}
					for k := range item {
						if k != "type" && k != "role" && k != "content" {
							return in, ErrTextProtocol
						}
					}
					content, err := adaptContent(item["content"], false)
					if err != nil {
						return in, err
					}
					out.Messages = append(out.Messages, provider.Message{Role: role, Content: content})
				case "function_call":
					var id, name, args string
					if json.Unmarshal(item["call_id"], &id) != nil || json.Unmarshal(item["name"], &name) != nil || json.Unmarshal(item["arguments"], &args) != nil || id == "" || name == "" {
						return in, ErrTextProtocol
					}
					for k := range item {
						if k != "type" && k != "call_id" && k != "name" && k != "arguments" {
							return in, ErrTextProtocol
						}
					}
					out.Messages = append(out.Messages, provider.Message{Role: "assistant", ToolCalls: textJSON([]any{map[string]any{"id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}}})})
				case "function_call_output":
					var id, output string
					if json.Unmarshal(item["call_id"], &id) != nil || json.Unmarshal(item["output"], &output) != nil || id == "" {
						return in, ErrTextProtocol
					}
					for k := range item {
						if k != "type" && k != "call_id" && k != "output" {
							return in, ErrTextProtocol
						}
					}
					out.Messages = append(out.Messages, provider.Message{Role: "tool", ToolCallID: id, Content: textJSON(output)})
				default:
					return in, ErrTextProtocol
				}
			}
		}
		out.Input = nil
	}
	out.Options = map[string]json.RawMessage{}
	for k, v := range in.Options {
		switch k {
		case "temperature", "top_p":
			out.Options[k] = bytes.Clone(v)
		case "max_tokens", "max_completion_tokens":
			if !toResponses {
				return in, ErrTextProtocol
			}
			if old, ok := out.Options["max_output_tokens"]; ok && !bytes.Equal(old, v) {
				return in, ErrTextProtocol
			}
			out.Options["max_output_tokens"] = bytes.Clone(v)
		case "max_output_tokens":
			if toResponses {
				return in, ErrTextProtocol
			}
			out.Options["max_completion_tokens"] = bytes.Clone(v)
		case "reasoning_effort":
			if !toResponses {
				return in, ErrTextProtocol
			}
			out.Options["reasoning"] = textJSON(map[string]json.RawMessage{"effort": v})
		case "reasoning":
			if toResponses {
				return in, ErrTextProtocol
			}
			var r map[string]json.RawMessage
			if json.Unmarshal(v, &r) != nil || len(r) != 1 || r["effort"] == nil {
				return in, ErrTextProtocol
			}
			out.Options["reasoning_effort"] = r["effort"]
		case "response_format":
			if !toResponses {
				return in, ErrTextProtocol
			}
			format, err := adaptFormat(v, true)
			if err != nil {
				return in, err
			}
			out.Options["text"] = textJSON(map[string]json.RawMessage{"format": format})
		case "text":
			if toResponses {
				return in, ErrTextProtocol
			}
			var obj map[string]json.RawMessage
			if json.Unmarshal(v, &obj) != nil || len(obj) != 1 || obj["format"] == nil {
				return in, ErrTextProtocol
			}
			format, err := adaptFormat(obj["format"], false)
			if err != nil {
				return in, err
			}
			out.Options["response_format"] = format
		case "instructions":
			if toResponses {
				return in, ErrTextProtocol
			}
			var content string
			if json.Unmarshal(v, &content) != nil {
				return in, ErrTextProtocol
			}
			out.Messages = append([]provider.Message{{Role: "system", Content: v}}, out.Messages...)
		case "store":
			if toResponses || !bytes.Equal(v, []byte("false")) {
				return in, ErrTextProtocol
			}
		case "tools":
			var list []json.RawMessage
			if json.Unmarshal(v, &list) != nil {
				return in, ErrTextProtocol
			}
			converted := make([]json.RawMessage, 0, len(list))
			for _, tool := range list {
				c, err := adaptFunction(tool, toResponses)
				if err != nil {
					return in, err
				}
				converted = append(converted, c)
			}
			out.Options[k] = textJSON(converted)
		case "tool_choice":
			var choice string
			if json.Unmarshal(v, &choice) == nil {
				out.Options[k] = bytes.Clone(v)
			} else {
				c, err := adaptFunction(v, toResponses)
				if err != nil {
					return in, err
				}
				out.Options[k] = c
			}
		default:
			return in, ErrTextProtocol
		}
	}
	return out, nil
}

func adaptContent(raw json.RawMessage, toResponses bool) (json.RawMessage, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return bytes.Clone(raw), nil
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil, ErrTextProtocol
	}
	for _, p := range parts {
		var typ string
		_ = json.Unmarshal(p["type"], &typ)
		if toResponses {
			switch typ {
			case "text":
				if len(p) != 2 || p["text"] == nil {
					return nil, ErrTextProtocol
				}
				p["type"] = textJSON("input_text")
			case "image_url":
				if len(p) != 2 {
					return nil, ErrTextProtocol
				}
				var image map[string]json.RawMessage
				if json.Unmarshal(p["image_url"], &image) != nil || image["url"] == nil {
					return nil, ErrTextProtocol
				}
				for k := range image {
					if k != "url" && k != "detail" {
						return nil, ErrTextProtocol
					}
				}
				p["type"] = textJSON("input_image")
				p["image_url"] = image["url"]
				if image["detail"] != nil {
					p["detail"] = image["detail"]
				}
			default:
				return nil, ErrTextProtocol
			}
		} else {
			switch typ {
			case "input_text", "output_text":
				if len(p) != 2 || p["text"] == nil {
					return nil, ErrTextProtocol
				}
				p["type"] = textJSON("text")
			case "input_image":
				if p["image_url"] == nil {
					return nil, ErrTextProtocol
				}
				for k := range p {
					if k != "type" && k != "image_url" && k != "detail" {
						return nil, ErrTextProtocol
					}
				}
				image := map[string]json.RawMessage{"url": p["image_url"]}
				if p["detail"] != nil {
					image["detail"] = p["detail"]
					delete(p, "detail")
				}
				p["type"] = textJSON("image_url")
				p["image_url"] = textJSON(image)
			default:
				return nil, ErrTextProtocol
			}
		}
	}
	return textJSON(parts), nil
}

func adaptFormat(raw json.RawMessage, toResponses bool) (json.RawMessage, error) {
	var format map[string]json.RawMessage
	if json.Unmarshal(raw, &format) != nil {
		return nil, ErrTextProtocol
	}
	var typ string
	if json.Unmarshal(format["type"], &typ) != nil {
		return nil, ErrTextProtocol
	}
	if typ == "text" || typ == "json_object" {
		if len(format) != 1 {
			return nil, ErrTextProtocol
		}
		return bytes.Clone(raw), nil
	}
	if typ != "json_schema" {
		return nil, ErrTextProtocol
	}
	if toResponses {
		if len(format) != 2 {
			return nil, ErrTextProtocol
		}
		var schema map[string]json.RawMessage
		if json.Unmarshal(format["json_schema"], &schema) != nil {
			return nil, ErrTextProtocol
		}
		for k := range schema {
			if k != "name" && k != "schema" && k != "strict" && k != "description" {
				return nil, ErrTextProtocol
			}
		}
		schema["type"] = textJSON("json_schema")
		return textJSON(schema), nil
	}
	delete(format, "type")
	for k := range format {
		if k != "name" && k != "schema" && k != "strict" && k != "description" {
			return nil, ErrTextProtocol
		}
	}
	return textJSON(map[string]any{"type": "json_schema", "json_schema": format}), nil
}

func adaptFunction(raw json.RawMessage, toResponses bool) (json.RawMessage, error) {
	var tool map[string]json.RawMessage
	if json.Unmarshal(raw, &tool) != nil || !bytes.Equal(tool["type"], textJSON("function")) {
		return nil, ErrTextProtocol
	}
	if toResponses {
		if len(tool) != 2 {
			return nil, ErrTextProtocol
		}
		var fn map[string]json.RawMessage
		if json.Unmarshal(tool["function"], &fn) != nil {
			return nil, ErrTextProtocol
		}
		for k := range fn {
			if k != "name" && k != "description" && k != "parameters" && k != "strict" {
				return nil, ErrTextProtocol
			}
		}
		fn["type"] = textJSON("function")
		return textJSON(fn), nil
	}
	delete(tool, "type")
	for k := range tool {
		if k != "name" && k != "description" && k != "parameters" && k != "strict" {
			return nil, ErrTextProtocol
		}
	}
	return textJSON(map[string]any{"type": "function", "function": tool}), nil
}

func textJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
