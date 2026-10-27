package nova

import (
	"encoding/json"
)

// Events synthesizes compatible SSE only from a settled, bounded result. It
// preserves full choices, tool calls and response output, not just plain text.
func Events(protocol string, payload json.RawMessage) ([]byte, error) {
	var root map[string]json.RawMessage
	if len(payload) > 1<<20 || Decode(payload, &root) != nil || len(root) == 0 {
		return nil, ErrInvalid
	}
	var out []byte
	add := func(event string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if event != "" {
			out = append(out, []byte("event: "+event+"\n")...)
		}
		out = append(out, []byte("data: ")...)
		out = append(out, data...)
		out = append(out, '\n', '\n')
		return nil
	}
	switch protocol {
	case "openai-chat-completions", "openai_chat_completions":
		var choices []map[string]json.RawMessage
		if json.Unmarshal(root["choices"], &choices) != nil || len(choices) == 0 {
			return nil, ErrInvalid
		}
		for i, c := range choices {
			var message map[string]json.RawMessage
			if json.Unmarshal(c["message"], &message) != nil || len(message) == 0 {
				return nil, ErrInvalid
			}
			if calls, ok := message["tool_calls"]; ok {
				var list []map[string]json.RawMessage
				if json.Unmarshal(calls, &list) != nil {
					return nil, ErrInvalid
				}
				for j, call := range list {
					call["index"], _ = json.Marshal(j)
				}
				message["tool_calls"], _ = json.Marshal(list)
			}
			c["delta"], _ = json.Marshal(message)
			delete(c, "message")
			if c["index"] == nil {
				c["index"], _ = json.Marshal(i)
			}
		}
		root["choices"], _ = json.Marshal(choices)
		root["object"] = json.RawMessage(`"chat.completion.chunk"`)
		if err := add("", root); err != nil {
			return nil, err
		}
	case "openai-responses", "openai_responses":
		var output []map[string]json.RawMessage
		if json.Unmarshal(root["output"], &output) != nil {
			return nil, ErrInvalid
		}
		for i, item := range output {
			var typ string
			_ = json.Unmarshal(item["type"], &typ)
			if typ == "message" {
				var content []map[string]json.RawMessage
				_ = json.Unmarshal(item["content"], &content)
				for j, part := range content {
					var text, kind string
					_ = json.Unmarshal(part["text"], &text)
					_ = json.Unmarshal(part["type"], &kind)
					if kind == "output_text" {
						_ = add("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": i, "content_index": j, "delta": text})
					}
				}
			}
			_ = add("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		_ = add("response.completed", map[string]any{"type": "response.completed", "response": root})
	default:
		return nil, ErrProtocol
	}
	out = append(out, []byte("data: [DONE]\n\n")...)
	return out, nil
}
