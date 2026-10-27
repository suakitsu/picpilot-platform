package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChatPreservesToolCallsAndStructuredContent(t *testing.T) {
	tool := json.RawMessage(`[{"id":"call-1","type":"function","function":{"name":"propose_image_action","arguments":"{\"action\":\"generate\"}"},"extension":{"opaque":true}}]`)
	content := json.RawMessage(`[{"type":"text","text":"ready","annotations":[{"custom":"kept"}]}]`)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("chat endpoint/auth")
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if string(body["stream"]) != "false" || rawString(body["model"]) != "mock-text" || !nonNull(body["tools"]) || !nonNull(body["response_format"]) {
			t.Error("chat fields lost")
		}
		messages := rawArray(body["messages"])
		if len(messages) != 3 || !jsonEqual(rawObject(messages[1])["tool_calls"], tool) || rawString(rawObject(messages[2])["tool_call_id"]) != "call-1" {
			t.Error("tool history changed")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content, "tool_calls": tool}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"total_tokens": 12}, "custom": "preserved"})
	}))
	defer s.Close()
	p := testProvider(s)
	p.Model = "mock-text"
	p.BaseURL += "/v1/chat/completions/"
	result, err := testGateway(t, nil).GenerateText(context.Background(), p, TextRequest{
		Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock request"`)}, {Role: "assistant", Content: json.RawMessage(`null`), ToolCalls: tool}, {Role: "tool", ToolCallID: "call-1", Content: json.RawMessage(`"done"`)}},
		Options:  map[string]json.RawMessage{"tools": json.RawMessage(`[{"type":"function","function":{"name":"propose_image_action","parameters":{"type":"object"}}}]`), "response_format": json.RawMessage(`{"type":"json_schema","json_schema":{"name":"plan","schema":{"type":"object"}}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := rawObject(result.Payload)
	msg := rawObject(rawObject(rawArray(root["choices"])[0])["message"])
	if result.Text != "ready" || !jsonEqual(msg["tool_calls"], tool) || !jsonEqual(msg["content"], content) || rawString(root["custom"]) != "preserved" || !nonNull(root["usage"]) {
		t.Fatal("structured response was flattened/lost")
	}
}

func TestChatToolOnlyAndRefusalResponses(t *testing.T) {
	for _, message := range []string{
		`{"role":"assistant","content":null,"tool_calls":[{"id":"a","type":"function","function":{"name":"run","arguments":"{}"}}]}`,
		`{"role":"assistant","content":null,"refusal":"Cannot comply"}`,
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[{"message":`+message+`}]}`)
		}))
		res, err := testGateway(t, nil).GenerateText(context.Background(), testProvider(s), TextRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock"`)}}})
		s.Close()
		if err != nil || res.Text != "" || len(res.Payload) == 0 {
			t.Fatalf("tool/refusal discarded: %v", err)
		}
	}
}

func TestResponsesPreservesInputOutputAndTools(t *testing.T) {
	input := json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"mock"}]},{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call-1","output":"result"}]`)
	output := json.RawMessage(`[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"},{"type":"function_call","id":"fc_1","call_id":"call-2","name":"propose_image_action","arguments":"{\"prompt\":\"mock\"}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"planned","annotations":[]}]}]`)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Error("wrong responses endpoint")
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !jsonEqual(body["input"], input) || string(body["stream"]) != "false" || string(body["store"]) != "false" || nonNull(body["messages"]) || !nonNull(body["text"]) {
			t.Error("responses request changed")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp_1", "status": "completed", "output": output, "usage": map[string]int{"input_tokens": 4}})
	}))
	defer s.Close()
	p := testProvider(s)
	p.BaseURL += "/v1/responses"
	p.TextProtocol = Responses
	res, err := testGateway(t, nil).GenerateText(context.Background(), p, TextRequest{Input: input, Options: map[string]json.RawMessage{"text": json.RawMessage(`{"format":{"type":"json_object"}}`), "tools": json.RawMessage(`[{"type":"function","name":"propose_image_action","parameters":{"type":"object"}}]`)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "planned" || !jsonEqual(rawObject(res.Payload)["output"], output) {
		t.Fatal("responses structure lost")
	}
}

func TestDeepSeekJavaContract(t *testing.T) {
	for _, suffix := range []string{"", "/v1", "/chat/completions"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			want := "/chat/completions"
			if suffix == "/v1" {
				want = "/v1/chat/completions"
			}
			if r.URL.Path != want {
				t.Errorf("DeepSeek path: %s", r.URL.Path)
			}
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			if rawString(rawObject(body["thinking"])["type"]) != "disabled" {
				t.Error("DeepSeek thinking contract changed")
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
		}))
		p := testProvider(s)
		p.Model = "deepseek-mock"
		p.BaseURL += suffix
		_, err := testGateway(t, nil).GenerateText(context.Background(), p, TextRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock"`)}}})
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTextVisionInlineOnlyAndDecoded(t *testing.T) {
	data := fixture(t, "webp", 1, 1)
	uri := "data:image/webp;base64," + base64.StdEncoding.EncodeToString(data)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		msg := rawObject(rawArray(body["messages"])[0])
		parts := rawArray(msg["content"])
		if rawString(rawObject(rawObject(parts[0])["image_url"])["url"]) != uri {
			t.Error("vision data changed")
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"image seen"}}]}`)
	}))
	defer s.Close()
	g := testGateway(t, nil)
	p := testProvider(s)
	content, _ := json.Marshal([]any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": uri}}})
	r := TextRequest{Messages: []Message{{Role: "user", Content: content}}}
	if _, err := g.GenerateText(context.Background(), p, r); err != nil {
		t.Fatal(err)
	}
	p.DeepSeek = true
	_, err := g.GenerateText(context.Background(), p, r)
	assertError(t, err, Unsupported, NotSubmitted)
	p.DeepSeek = false
	for _, content := range []string{`[{"type":"image_url","image_url":{"url":"http://127.0.0.1/private"}}]`, `[{"type":"image_url","image_url":{"url":"data:image/png;base64,bm90IGFuIGltYWdl"}}]`} {
		r.Messages[0].Content = json.RawMessage(content)
		_, err := g.GenerateText(context.Background(), p, r)
		if err == nil {
			t.Fatal("accepted remote/invalid image")
		}
	}
}

func TestTextResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		protocol Protocol
		body     string
	}{
		{ChatCompletions, `{}`}, {ChatCompletions, `{"choices":[]}`}, {ChatCompletions, `{"choices":[{"message":{"content":42}}]}`},
		{ChatCompletions, `{"choices":[{"message":{"content":null,"tool_calls":[{}]}}]}`}, {ChatCompletions, `{"choices":[{"message":{"content":""}}]}`},
		{ChatCompletions, `{"choices":[{"message":{"content":[{"type":"text","text":42}]}}]}`},
		{ChatCompletions, `{"choices":[{"message":{"content":[{"type":"text","text":""}]}}]}`},
		{ChatCompletions, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"a","type":"function","function":{"name":"run","arguments":null}}]}}]}`},
		{Responses, `{"status":"in_progress","output":[{"type":"message","content":[]}]}`}, {Responses, `{"output":[]}`}, {Responses, `{"output":[{"type":"function_call","name":"missing-call-id"}]}`},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.protocol, len(tc.body)), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer s.Close()
			p := testProvider(s)
			p.TextProtocol = tc.protocol
			r := TextRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock"`)}}}
			if tc.protocol == Responses {
				r.Messages = nil
				r.Input = json.RawMessage(`"mock"`)
			}
			_, err := testGateway(t, nil).GenerateText(context.Background(), p, r)
			assertError(t, err, InvalidResponse, Uncertain)
		})
	}
}

func TestSensitiveConfigFormattingAndResponseRedaction(t *testing.T) {
	p := Provider{APIKey: Secret(testKey), BaseURL: "https://unused.test/"}
	for _, value := range []any{p, &p, p.APIKey, Request{Provider: p, Text: &TextRequest{Input: json.RawMessage(`"private mock prompt"`)}}} {
		formatted := fmt.Sprintf("%v %+v %#v %s", value, value, value, value)
		if strings.Contains(formatted, testKey) || strings.Contains(formatted, "private mock prompt") {
			t.Fatal("sensitive fmt output")
		}
		b, _ := json.Marshal(value)
		if strings.Contains(string(b), testKey) {
			t.Fatal("key serialized")
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "echo " + testKey, "tool_calls": []any{map[string]any{"id": "1", "type": "function", "function": map[string]string{"name": "call", "arguments": `{"echo":"` + testKey + `"}`}}}}}}, "echo": testKey})
	}))
	defer s.Close()
	result, err := testGateway(t, nil).GenerateText(context.Background(), testProvider(s), TextRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock"`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, testKey) || strings.Contains(string(result.Payload), testKey) || !strings.Contains(string(result.Payload), "REDACTED") {
		t.Fatal("key echoed into response")
	}
}

func TestTextRejectsOptionOverridesAndOversizeBeforeHTTP(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid text submitted") }))
	defer s.Close()
	for _, key := range []string{"stream", "model", "api_key", "unknown"} {
		_, err := testGateway(t, nil).GenerateText(context.Background(), testProvider(s), TextRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"mock"`)}}, Options: map[string]json.RawMessage{key: json.RawMessage(`true`)}})
		assertError(t, err, InvalidRequest, NotSubmitted)
	}
	content, _ := json.Marshal(strings.Repeat("x", 80_001))
	_, err := testGateway(t, nil).GenerateText(context.Background(), testProvider(s), TextRequest{Messages: []Message{{Role: "user", Content: content}}})
	assertError(t, err, LimitExceeded, NotSubmitted)
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
