package nova

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictDecodeAndSubmissionBounds(t *testing.T) {
	valid := `{"mode":"text-to-image","prompt":"three blocks","parallelCount":1,"outputSize":"1K","aspectRatio":"16:9"}`
	var in ImageRequest
	if Decode([]byte(valid), &in) != nil || in.Validate() != nil {
		t.Fatal("valid image rejected")
	}
	if got, _ := in.Size(); got != "1280x720" {
		t.Fatal(got)
	}
	for _, raw := range []string{`null`, `[]`, valid + ` {}`, `{"prompt":"a","prompt":"b"}`, `{"unknown":1}`, `{"images":[{"data":"a","mimeType":"image/png","extra":1}]}`, `{"apiKey":"a","apiKey":"b"}`} {
		if Decode([]byte(raw), &ImageRequest{}) == nil {
			t.Errorf("accepted malformed fixture %s", raw)
		}
	}
	for _, edit := range []func(*ImageRequest){
		func(v *ImageRequest) { v.APIKey = "test-only-label" }, func(v *ImageRequest) { v.BaseURL = "https://example.invalid" },
		func(v *ImageRequest) { v.Protocol = "google" }, func(v *ImageRequest) { v.Model = strings.Repeat("a", 201) },
		func(v *ImageRequest) { v.Mode = "image-to-image" }, func(v *ImageRequest) { v.ParallelCount = 5 },
		func(v *ImageRequest) { v.ParallelCount = 0 }, func(v *ImageRequest) { v.Prompt = strings.Repeat("x", 32001) },
		func(v *ImageRequest) { v.Background = "secret" }, func(v *ImageRequest) { v.Quality = "invalid" },
		func(v *ImageRequest) { v.OutputSize = "8K" }, func(v *ImageRequest) { v.AspectRatio = "0:0" },
		func(v *ImageRequest) { v.CustomSize = "999999x999999" }, func(v *ImageRequest) { v.Images = []Reference{{Data: "YQ==", MIMEType: "image/png"}} },
	} {
		copy := in
		edit(&copy)
		if copy.Validate() == nil {
			t.Fatal("accepted invalid submission")
		}
	}
	for _, key := range []string{"", "two keys", strings.Repeat("x", 129), "line\nbreak", "nonascii\u00e9"} {
		if ValidateKey(key) == nil {
			t.Fatal("accepted bad key")
		}
	}
	if ValidateKey("test-only-request:1") != nil {
		t.Fatal("valid key rejected")
	}
}

func TestReferenceAndSizeNormalization(t *testing.T) {
	for _, ref := range []Reference{{"", "image/png"}, {"YWJj", "text/plain"}, {"data:image/jpeg;base64,YQ==", "image/png"}, {"YQ==\n", "image/png"}, {"not base64", "image/png"}} {
		if _, err := ref.Bytes(); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	for _, data := range []string{"YQ==", "data:image/png;base64,YQ=="} {
		b, err := (Reference{data, "image/png"}).Bytes()
		if err != nil || string(b) != "a" {
			t.Fatal("valid reference rejected")
		}
	}
	for _, tc := range []struct{ out, ratio, custom, want string }{
		{"auto", "auto", "", "1024x1024"}, {"512", "1:1", "", "1024x1024"}, {"2K", "9:16", "", "1152x2048"},
		{"4K", "1:1", "", "2880x2880"}, {"1K", "1:1", "3840x2160", "3840x2160"}, {"1K", "1:8", "", "720x1280"},
	} {
		got, err := (ImageRequest{OutputSize: tc.out, AspectRatio: tc.ratio, CustomSize: tc.custom}).Size()
		if err != nil || got != tc.want {
			t.Fatalf("size %s %s got %s", tc.out, tc.ratio, got)
		}
	}
}

func TestTextTransportAndNestedDuplicates(t *testing.T) {
	in := TextRequest{Protocol: "openai-chat-completions", Stream: true, RequestBody: map[string]json.RawMessage{"stream": json.RawMessage("true"), "messages": json.RawMessage(`[{"role":"user","content":"hello"}]`)}}
	body, err := in.CanvasBody()
	if err != nil || body["protocol"] != "openai_chat_completions" {
		t.Fatal("text mapping")
	}
	if _, ok := body["request_body"].(map[string]json.RawMessage)["stream"]; ok {
		t.Fatal("stream entered durable request")
	}
	in.Stream = false
	if _, err = in.CanvasBody(); err == nil {
		t.Fatal("stream mismatch")
	}
	in.Protocol = "anthropic-messages"
	if _, err = in.CanvasBody(); err != ErrProtocol {
		t.Fatal("unsupported text protocol")
	}
	if Decode([]byte(`{"requestBody":{"input":"first","input":"second"}}`), &TextRequest{}) == nil {
		t.Fatal("nested duplicate accepted")
	}
	in = TextRequest{Protocol: "openai-responses", RequestBody: map[string]json.RawMessage{"input": json.RawMessage(`"hello"`)}}
	if body, err = in.CanvasBody(); err != nil || body["protocol"] != "openai_responses" {
		t.Fatal("responses mapping")
	}
	in.APIKey = "test-only-label"
	if _, err = in.CanvasBody(); err != ErrCredentials {
		t.Fatal("iframe credential accepted")
	}
}

func TestAggregatePreservesUncertaintyAndPartialSuccess(t *testing.T) {
	for _, tc := range []struct {
		states  []string
		want    string
		warning bool
	}{
		{nil, "failed", true}, {[]string{"queued", "queued"}, "queued", false},
		{[]string{"waiting_worker", "queued"}, "processing", false}, {[]string{"uncertain"}, "processing", false},
		{[]string{"succeeded", "queued"}, "processing", false}, {[]string{"succeeded", "failed"}, "completed", true},
		{[]string{"cancelled", "succeeded"}, "completed", true}, {[]string{"cancelled", "cancelled"}, "cancelled", false},
		{[]string{"failed", "deleted"}, "failed", false}, {[]string{"succeeded"}, "completed", false},
	} {
		got, warning := Aggregate(tc.states)
		if got != tc.want || (warning != "") != tc.warning {
			t.Fatalf("%v => %s %s", tc.states, got, warning)
		}
	}
}

func TestSettingsAndSSEToolCalls(t *testing.T) {
	s := DefaultSettings()
	if s.Validate() != nil || s.Mode != "2" || s.Prompts == nil || s.Keywords == nil {
		t.Fatal("settings defaults")
	}
	s.Prompts = []Prompt{{Title: "test", Content: "test", Type: 3}}
	if s.Validate() == nil {
		t.Fatal("invalid prompt type")
	}
	chat := json.RawMessage(`{"id":"test-only-response","choices":[{"index":0,"message":{"role":"assistant","content":"ready","tool_calls":[{"id":"call_fixture","type":"function","function":{"name":"propose","arguments":"{\"count\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":9}}`)
	stream, err := Events("openai_chat_completions", chat)
	if err != nil || !strings.Contains(string(stream), `"delta"`) || !strings.Contains(string(stream), `"tool_calls"`) || !strings.Contains(string(stream), `"total_tokens":9`) || !strings.HasSuffix(string(stream), "data: [DONE]\n\n") {
		t.Fatal("chat SSE lost payload")
	}
	response := json.RawMessage(`{"id":"test-only-response","output":[{"type":"message","content":[{"type":"output_text","text":"ready"}]},{"type":"function_call","name":"propose","arguments":"{\"count\":1}","call_id":"call_fixture"}],"usage":{"total_tokens":9}}`)
	stream, err = Events("openai_responses", response)
	if err != nil || !strings.Contains(string(stream), "event: response.completed") || !strings.Contains(string(stream), `"arguments"`) || !strings.Contains(string(stream), "response.output_text.delta") {
		t.Fatal("responses SSE lost payload")
	}
	for _, raw := range []string{`null`, `{}`, `{"choices":[]}`, `{"choices":[{"message":null}]}`, `{"choices":[],"choices":[{}]}`} {
		if _, err = Events("openai_chat_completions", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid result streamed")
		}
	}
}

func FuzzDecodeNova(f *testing.F) {
	f.Add(`{"prompt":"fixture","mode":"text-to-image","parallelCount":1}`)
	f.Add(`{"requestBody":{"input":[{"role":"user","content":"hello"}]}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		var in ImageRequest
		if Decode([]byte(raw), &in) == nil {
			_ = in.Validate()
		}
		var text TextRequest
		if Decode([]byte(raw), &text) == nil {
			_, _ = text.CanvasBody()
		}
	})
}
