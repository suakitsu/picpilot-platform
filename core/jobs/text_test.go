package jobs

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/provider"
)

func TestTextAdapterPreservesContentImagesAndOptions(t *testing.T) {
	original := TextInput{Messages: []provider.Message{{Role: "system", Content: textJSON("Preserve exact instructions.")}, {Role: "user", Content: json.RawMessage(`[{"type":"text","text":"Original wording 1. and 2. must not change"},{"type":"image_url","image_url":{"url":"picpilot-asset:asset_fixture","detail":"high"}}]`)}}, Options: map[string]json.RawMessage{"temperature": json.RawMessage("0.4"), "max_completion_tokens": json.RawMessage("1500"), "reasoning_effort": textJSON("low"), "response_format": json.RawMessage(`{"type":"json_schema","json_schema":{"name":"result","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}`)}}
	before, _ := json.Marshal(original)
	converted, err := AdaptTextInput(original, provider.Responses)
	if err != nil || len(converted.Messages) != 0 || len(converted.Input) == 0 {
		t.Fatal("chat to responses failed", err)
	}
	restored, err := AdaptTextInput(converted, provider.ChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("conversion mutated caller input")
	}
	var a, b any
	_ = json.Unmarshal(before, &a)
	raw, _ := json.Marshal(restored)
	_ = json.Unmarshal(raw, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("roundtrip lost message or parameter semantics")
	}
}

func TestTextAdapterPreservesToolCallRoundTrip(t *testing.T) {
	in := TextInput{Messages: []provider.Message{{Role: "user", Content: textJSON("Compute fixture")}, {Role: "assistant", ToolCalls: json.RawMessage(`[{"id":"call_fixture","type":"function","function":{"name":"calc","arguments":"{\"x\":1}"}}]`)}, {Role: "tool", ToolCallID: "call_fixture", Content: textJSON("2")}}, Options: map[string]json.RawMessage{"tools": json.RawMessage(`[{"type":"function","function":{"name":"calc","description":"Fixture calculator","parameters":{"type":"object","properties":{"x":{"type":"number"}}}}}]`), "tool_choice": json.RawMessage(`{"type":"function","function":{"name":"calc"}}`)}}
	responses, err := AdaptTextInput(in, provider.Responses)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := AdaptTextInput(responses, provider.ChatCompletions)
	if err != nil || len(chat.Messages) != 3 || chat.Messages[2].ToolCallID != "call_fixture" {
		t.Fatal("tool conversation lost", err)
	}
	var expected, actual any
	b, _ := json.Marshal(in)
	_ = json.Unmarshal(b, &expected)
	b, _ = json.Marshal(chat)
	_ = json.Unmarshal(b, &actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("tool schema changed during roundtrip")
	}
}

func TestTextAdapterRejectsLossyProtocolChanges(t *testing.T) {
	for _, input := range []TextInput{
		{Messages: []provider.Message{{Role: "user", Content: textJSON("fixture")}}, Options: map[string]json.RawMessage{"stop": textJSON("END")}},
		{Messages: []provider.Message{{Role: "user", Content: json.RawMessage(`[{"type":"input_audio","input_audio":{"data":"invalid","format":"wav"}}]`)}}},
		{Messages: []provider.Message{{Role: "user", Content: textJSON("fixture")}}, Options: map[string]json.RawMessage{"max_tokens": textJSON(100), "max_completion_tokens": textJSON(200)}},
	} {
		if _, err := AdaptTextInput(input, provider.Responses); err == nil {
			t.Fatal("unsupported field silently dropped")
		}
	}
	if _, err := AdaptTextInput(TextInput{Input: textJSON("fixture"), Options: map[string]json.RawMessage{"previous_response_id": textJSON("resp_fixture")}}, provider.ChatCompletions); err == nil {
		t.Fatal("remote conversation dependency dropped")
	}
}
