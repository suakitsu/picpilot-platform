package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
)

func boundedJSON(b []byte) bool {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	depth := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			return depth == 0
		}
		if err != nil {
			return false
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter == '{' || delimiter == '[' {
				depth++
			} else {
				depth--
			}
			if depth > 64 {
				return false
			}
		}
	}
}

func decodeObject(b []byte) (map[string]json.RawMessage, error) {
	if !boundedJSON(b) {
		return nil, fail(InvalidResponse, Uncertain)
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil || root == nil {
		return nil, fail(InvalidResponse, Uncertain)
	}
	return root, nil
}

func rawString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	return obj
}

func rawArray(raw json.RawMessage) []json.RawMessage {
	var arr []json.RawMessage
	_ = json.Unmarshal(raw, &arr)
	return arr
}

func nonNull(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}

func responseFailure(root map[string]json.RawMessage) bool {
	if nonNull(root["error"]) {
		return true
	}
	if raw := root["code"]; len(raw) > 0 {
		var code int
		if json.Unmarshal(raw, &code) == nil && code >= 400 {
			return true
		}
	}
	switch imageStatus(root) {
	case "failure", "failed", "error", "errored", "cancelled", "canceled", "timeout", "expired":
		return true
	}
	return false
}

func pendingStatus(s string) bool {
	switch s {
	case "submitted", "pending", "queued", "in_progress", "inprogress", "processing", "running", "starting", "waiting", "created":
		return true
	}
	return false
}

func imageStatus(root map[string]json.RawMessage) string {
	return strings.ToLower(envelopeString(root, []string{"status"}, 0))
}

func imageTaskID(root map[string]json.RawMessage) string {
	return envelopeString(root, []string{"task_id", "taskId", "job_id", "jobId", "generation_id", "prediction_id", "request_id"}, 0)
}

func envelopeString(root map[string]json.RawMessage, fields []string, depth int) string {
	if depth > 16 {
		return ""
	}
	for _, field := range fields {
		if s := rawString(root[field]); s != "" {
			return s
		}
	}
	for _, key := range []string{"data", "result"} {
		if obj := rawObject(root[key]); obj != nil {
			if s := envelopeString(obj, fields, depth+1); s != "" {
				return s
			}
		}
		for _, item := range rawArray(root[key]) {
			if s := envelopeString(rawObject(item), fields, depth+1); s != "" {
				return s
			}
		}
	}
	return ""
}

func imagePayload(root map[string]json.RawMessage, gemini bool) (string, bool, error) {
	if gemini {
		if feedback := rawObject(root["promptFeedback"]); rawString(feedback["blockReason"]) != "" {
			return "", false, fail(UpstreamFailure, Uncertain)
		}
		for _, candidate := range rawArray(root["candidates"]) {
			c := rawObject(candidate)
			reason := rawString(c["finishReason"])
			if reason != "" && reason != "STOP" {
				return "", false, fail(UpstreamFailure, Uncertain)
			}
			content := rawObject(c["content"])
			for _, part := range rawArray(content["parts"]) {
				obj := rawObject(part)
				for _, key := range []string{"inlineData", "inline_data"} {
					inline := rawObject(obj[key])
					mime := rawString(inline["mimeType"])
					if mime == "" {
						mime = rawString(inline["mime_type"])
					}
					if data := rawString(inline["data"]); data != "" {
						if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
							return "", false, fail(InvalidResponse, Uncertain)
						}
						return data, true, nil
					}
				}
			}
		}
	}
	// Only known image fields/envelopes are searched. Never fetch an arbitrary
	// URL found in an error, prompt echo, usage record, or unrelated metadata.
	if s, b64 := findImage(root, 0); s != "" {
		return s, b64, nil
	}
	return "", false, fail(InvalidResponse, Uncertain)
}

func findImage(root map[string]json.RawMessage, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	for _, key := range []string{"b64_json", "b64_data", "base64", "image_base64", "base64_image", "image_b64"} {
		if s := rawString(root[key]); s != "" {
			return s, true
		}
	}
	for _, key := range []string{"url", "urls", "uri", "image_url", "image_urls", "output_url", "result_url", "download_url", "signed_url", "file_url"} {
		if s := imageString(root[key]); s != "" {
			return s, strings.HasPrefix(s, "data:")
		}
	}
	for _, key := range []string{"data", "result", "results", "image", "images", "output", "outputs"} {
		if s := imageString(root[key]); s != "" {
			return s, strings.HasPrefix(s, "data:")
		}
		if obj := rawObject(root[key]); obj != nil {
			if s, b := findImage(obj, depth+1); s != "" {
				return s, b
			}
		}
		for _, item := range rawArray(root[key]) {
			if s, b := findImage(rawObject(item), depth+1); s != "" {
				return s, b
			}
		}
	}
	return "", false
}

func imageString(raw json.RawMessage) string {
	if s := rawString(raw); strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "data:image/") {
		return s
	}
	for _, item := range rawArray(raw) {
		if s := rawString(item); strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "data:image/") {
			return s
		}
	}
	return ""
}

func redact(s string, secret Secret) string {
	key := string(secret)
	if key == "" {
		return s
	}
	for _, v := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	return s
}

func containsSecret(s string, secret Secret) bool { return redact(s, secret) != s }

// Redact string values recursively, including JSON-escaped occurrences in tool
// arguments, while leaving their protocol structure and numeric precision intact.
func sanitizeJSON(raw json.RawMessage, secret Secret) json.RawMessage {
	if secret == "" {
		return raw
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		clean := make(map[string]json.RawMessage, len(obj))
		for key, value := range obj {
			clean[redact(key, secret)] = sanitizeJSON(value, secret)
		}
		b, _ := json.Marshal(clean)
		return b
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil && arr != nil {
		for i := range arr {
			arr[i] = sanitizeJSON(arr[i], secret)
		}
		b, _ := json.Marshal(arr)
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		b, _ := json.Marshal(redact(s, secret))
		return b
	}
	return raw
}
