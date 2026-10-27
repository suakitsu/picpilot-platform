// Package provider implements single-submission HTTP adapters. It does not select
// credentials, compile prompts, persist requests, log payloads, or retry jobs.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Protocol string

const (
	ChatCompletions       Protocol = "openai_chat_completions"
	Responses             Protocol = "openai_responses"
	ImageGenerations      Protocol = "openai_images_generations"
	ImageEdits            Protocol = "openai_images_edits"
	GeminiGenerateContent Protocol = "gemini_generate_content"
)

// ImageProfile selects a wire contract, never inferred from an untrusted URL.
type ImageProfile string

const (
	OpenAI       ImageProfile = "openai"
	LegacyOpenAI ImageProfile = "legacy_openai"
	Hfsy         ImageProfile = "hfsyapi"
	Yunfei       ImageProfile = "yunfei"
	Rua          ImageProfile = "rua"
	XinZhiZhu    ImageProfile = "xin_zhi_zhu"
	Aishuch      ImageProfile = "aishuch"
	GeminiProxy  ImageProfile = "gemini_proxy"
	GoogleGemini ImageProfile = "google_gemini"
)

// Secret is an in-memory credential. Formatting and JSON never disclose it.
// Explicit conversion to string is intentionally required for use as a key.
type Secret string

func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[REDACTED]") }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

// Provider belongs to a single call. Gateway never retains its credentials.
// BaseURL may end in /v1, /v1beta, or a full chat/responses endpoint.
type Provider struct {
	BaseURL      string
	APIKey       Secret `json:"-"`
	Model        string
	ImageProfile ImageProfile
	TextProtocol Protocol
	DeepSeek     bool
	Timeout      time.Duration
}

func (Provider) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "provider.Provider{[REDACTED]}")
}

// Reference contains exactly one of Data and URL. Input order is preserved.
// URLs must be absolute HTTP(S); filesystem and storage-key resolution belongs
// to the caller. Original bytes are sent, without resizing or cropping.
type Reference struct {
	Data []byte
	URL  string
}

type ImageRequest struct {
	Prompt         string
	Size           string
	ResponseFormat string
	Quality        string
	OutputFormat   string
	Background     string
	Moderation     string
	References     []Reference
}

type Image struct {
	Bytes    []byte
	MIMEType string
	Width    int
	Height   int
}

type ImageResponse struct {
	Image               Image
	Model               string
	RequestedSize       string
	SubmittedSize       string
	ReferencesUsed      int
	ReferencesTruncated bool
}

// Message preserves string, structured-array, and null content. ToolCalls keeps
// the original protocol objects, including function arguments and extension fields.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

// TextRequest uses Messages for chat and Input for Responses. Options is a
// protocol-specific allowlist (tools, tool_choice, response_format/text, reasoning,
// token limits, etc.). Model and stream cannot be overridden. Streaming is disabled.
type TextRequest struct {
	Messages []Message
	Input    json.RawMessage
	Options  map[string]json.RawMessage
}

// Payload preserves tool calls, structured content, usage, status, and refusal
// information. Text is only a convenience projection. Never log Payload or Text.
type TextResponse struct {
	Text    string
	Payload json.RawMessage
}

type Request struct {
	Provider Provider `json:"-"`
	Protocol Protocol
	Image    *ImageRequest
	Text     *TextRequest
}

func (Request) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "provider.Request{[REDACTED]}") }

type Response struct {
	Image *ImageResponse
	Text  *TextResponse
}

// Do dispatches one operation. It never executes fallback providers or retries.
func (g *Gateway) Do(ctx context.Context, r Request) (Response, error) {
	if r.Image != nil && r.Text == nil {
		if r.Protocol != ImageGenerations && r.Protocol != ImageEdits && r.Protocol != GeminiGenerateContent {
			return Response{}, fail(InvalidRequest, NotSubmitted)
		}
		v, err := g.image(ctx, r.Provider, *r.Image, r.Protocol)
		if err != nil {
			return Response{}, err
		}
		return Response{Image: &v}, nil
	}
	if r.Text != nil && r.Image == nil && (r.Protocol == ChatCompletions || r.Protocol == Responses) {
		r.Provider.TextProtocol = r.Protocol
		v, err := g.GenerateText(ctx, r.Provider, *r.Text)
		if err != nil {
			return Response{}, err
		}
		return Response{Text: &v}, nil
	}
	return Response{}, fail(InvalidRequest, NotSubmitted)
}
