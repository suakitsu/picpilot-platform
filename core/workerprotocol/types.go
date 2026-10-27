package workerprotocol

import (
	"encoding/json"
	"time"
)

const (
	BasePath                      = "/api/internal/picpilot-worker/v1"
	MaxRequestBytes               = 256 * 1024
	MaxInputMetadataBytes         = 256 * 1024
	MaxResultBytes                = 256 * 1024
	DefaultMaxArtifactBytes int64 = 25 * 1024 * 1024
	MaxArtifactPixels       int64 = 50_000_000
	ImageModel                    = "gpt-image-2"
)

type Capability string

const (
	Image Capability = "image"
	Text  Capability = "text"
)

type Protocol string

const (
	Responses        Protocol = "openai_responses"
	ChatCompletions  Protocol = "openai_chat_completions"
	ImageGenerations Protocol = "openai_images_generations"
	ImageEdits       Protocol = "openai_images_edits"
)

// TextModels returns a fresh copy so callers cannot change the v1 catalog.
func TextModels() []string {
	return []string{"gpt-5.6-sol", "gpt-5.5", "gpt-5.6-terra", "gpt-5.6-luna"}
}

// Message is implemented by endpoint DTOs and provider payloads. Use Decode
// instead of json.Unmarshal at trust boundaries (required/case/unknown keys).
type Message interface {
	SchemaName() string
	Validate() error
}

type HeartbeatRequest struct {
	WorkerID       string       `json:"workerId"`
	Capabilities   []Capability `json:"capabilities"`
	MaxConcurrency int          `json:"maxConcurrency"`
}
type HeartbeatResponse struct {
	WorkerID                 string    `json:"workerId"`
	ServerTime               time.Time `json:"serverTime"`
	ActiveTasks              int       `json:"activeTasks"`
	HeartbeatIntervalSeconds int       `json:"heartbeatIntervalSeconds"`
}
type ClaimRequest struct {
	WorkerID     string       `json:"workerId"`
	Capabilities []Capability `json:"capabilities"`
	MaxTasks     int          `json:"maxTasks"`
	WaitSeconds  int          `json:"waitSeconds"`
}
type InputFile struct {
	Index                 int       `json:"index"`
	URL                   string    `json:"url"`
	Filename              string    `json:"filename"`
	ContentType           string    `json:"contentType"`
	FileSize              int64     `json:"fileSize"`
	URLExpiresAt          time.Time `json:"urlExpiresAt"`
	URLChecksum           string    `json:"urlChecksum"`
	URLSignatureAlgorithm string    `json:"urlSignatureAlgorithm"`
}
type ClaimedTask struct {
	JobID      string          `json:"jobId"`
	LeaseID    string          `json:"leaseId"`
	LeaseUntil time.Time       `json:"leaseUntil"`
	RenewAfter time.Time       `json:"renewAfter"`
	GraceUntil time.Time       `json:"graceUntil"`
	Capability Capability      `json:"capability"`
	Protocol   Protocol        `json:"protocol"`
	Model      string          `json:"model"`
	Request    json.RawMessage `json:"request"`
	InputFiles []InputFile     `json:"inputFiles"`
	Attempt    int             `json:"attempt"`
}
type ClaimResponse struct {
	// Never omit task:null: no-task is HTTP 200, not 204.
	Task       *ClaimedTask `json:"task"`
	Empty      bool         `json:"empty"`
	ServerTime time.Time    `json:"serverTime"`
	WaitedMS   int64        `json:"waitedMs"`
}
type LeaseRequest struct {
	WorkerID string `json:"workerId"`
	LeaseID  string `json:"leaseId"`
}
type LeaseResponse struct {
	JobID      string    `json:"jobId"`
	LeaseUntil time.Time `json:"leaseUntil"`
	RenewAfter time.Time `json:"renewAfter"`
	GraceUntil time.Time `json:"graceUntil"`
}

// ArtifactForm models metadata only. File bytes belong to the multipart file
// part, never a JSON/base64 envelope. ValidateMultipartFields checks the exact
// multipart names; VerifyArtifactSource checks the actual bytes.
type ArtifactForm struct {
	WorkerID       string
	LeaseID        string
	IdempotencyKey string
	SHA256         string
}
type ArtifactResponse struct {
	ArtifactID   string `json:"artifactId"`
	Idempotent   bool   `json:"idempotent"`
	ContentType  string `json:"contentType"`
	FileSize     int64  `json:"fileSize"`
	SHA256       string `json:"sha256"`
	SourceSHA256 string `json:"sourceSha256"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Format       string `json:"format"`
}
type CompleteRequest struct {
	WorkerID       string          `json:"workerId"`
	LeaseID        string          `json:"leaseId"`
	IdempotencyKey string          `json:"idempotencyKey"`
	ArtifactID     string          `json:"artifactId,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}
type FailRequest struct {
	WorkerID       string `json:"workerId"`
	LeaseID        string `json:"leaseId"`
	IdempotencyKey string `json:"idempotencyKey"`
	ErrorCode      string `json:"errorCode"`
	ErrorMessage   string `json:"errorMessage"`
}
type TerminalResponse struct {
	JobID      string `json:"jobId"`
	Status     Status `json:"status"`
	Idempotent bool   `json:"idempotent"`
	Retryable  *bool  `json:"retryable,omitempty"`
}

// RawMessage is intentional for fields whose normative schema is {}.
// Protocol payload top-level fields remain closed and recursively inspected.
type ResponsesPayload struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    json.RawMessage `json:"instructions,omitempty"`
	Temperature     json.RawMessage `json:"temperature,omitempty"`
	TopP            json.RawMessage `json:"top_p,omitempty"`
	MaxOutputTokens json.RawMessage `json:"max_output_tokens,omitempty"`
	Reasoning       json.RawMessage `json:"reasoning,omitempty"`
	Text            json.RawMessage `json:"text,omitempty"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	Store           json.RawMessage `json:"store,omitempty"`
	Stream          bool            `json:"stream"`
}
type ChatCompletionsPayload struct {
	Model               string            `json:"model"`
	Messages            []json.RawMessage `json:"messages"`
	Temperature         json.RawMessage   `json:"temperature,omitempty"`
	TopP                json.RawMessage   `json:"top_p,omitempty"`
	MaxTokens           json.RawMessage   `json:"max_tokens,omitempty"`
	MaxCompletionTokens json.RawMessage   `json:"max_completion_tokens,omitempty"`
	FrequencyPenalty    json.RawMessage   `json:"frequency_penalty,omitempty"`
	PresencePenalty     json.RawMessage   `json:"presence_penalty,omitempty"`
	ResponseFormat      json.RawMessage   `json:"response_format,omitempty"`
	Stop                json.RawMessage   `json:"stop,omitempty"`
	Seed                json.RawMessage   `json:"seed,omitempty"`
	Stream              bool              `json:"stream"`
	ReasoningEffort     json.RawMessage   `json:"reasoning_effort,omitempty"`
	Thinking            json.RawMessage   `json:"thinking,omitempty"`
	Tools               json.RawMessage   `json:"tools,omitempty"`
	ToolChoice          json.RawMessage   `json:"tool_choice,omitempty"`
}
type ImageGenerationPayload struct {
	Model             string          `json:"model"`
	Prompt            string          `json:"prompt"`
	N                 int             `json:"n"`
	Size              json.RawMessage `json:"size,omitempty"`
	Quality           json.RawMessage `json:"quality,omitempty"`
	OutputFormat      json.RawMessage `json:"output_format,omitempty"`
	ResponseFormat    json.RawMessage `json:"response_format,omitempty"`
	Background        json.RawMessage `json:"background,omitempty"`
	Moderation        json.RawMessage `json:"moderation,omitempty"`
	OutputCompression json.RawMessage `json:"output_compression,omitempty"`
	User              json.RawMessage `json:"user,omitempty"`
}
type ImageEditPayload struct {
	ImageGenerationPayload
	InputFidelity json.RawMessage `json:"input_fidelity,omitempty"`
}

func (HeartbeatRequest) SchemaName() string       { return "heartbeatRequest" }
func (HeartbeatResponse) SchemaName() string      { return "heartbeatResponse" }
func (ClaimRequest) SchemaName() string           { return "claimRequest" }
func (ClaimedTask) SchemaName() string            { return "claimedTask" }
func (ClaimResponse) SchemaName() string          { return "claimResponse" }
func (LeaseRequest) SchemaName() string           { return "leaseRequest" }
func (LeaseResponse) SchemaName() string          { return "leaseResponse" }
func (InputFile) SchemaName() string              { return "inputFile" }
func (ArtifactResponse) SchemaName() string       { return "artifactResponse" }
func (CompleteRequest) SchemaName() string        { return "completeRequest" }
func (FailRequest) SchemaName() string            { return "failRequest" }
func (TerminalResponse) SchemaName() string       { return "terminalResponse" }
func (ResponsesPayload) SchemaName() string       { return "responsesPayload" }
func (ChatCompletionsPayload) SchemaName() string { return "chatCompletionsPayload" }
func (ImageGenerationPayload) SchemaName() string { return "imageGenerationPayload" }
func (ImageEditPayload) SchemaName() string       { return "imageEditPayload" }
