// Package nova contains the retained canvas runtime's wire contracts. It does
// not call providers, own a queue, or settle business tasks.
package nova

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxImageBody  = 12 << 20
	MaxTextBody   = 32 << 20
	MaxReferences = 10
	MaxParallel   = 4
	MaxPending    = 64
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *Error) Error() string { return e.Code }

var (
	ErrInvalid     = &Error{"INVALID_REQUEST", "Invalid, unsupported or oversized Nova request"}
	ErrCredentials = &Error{"IFRAME_CREDENTIALS_FORBIDDEN", "Configure providers in PicPilot; do not send provider credentials or URLs"}
	ErrProtocol    = &Error{"UNSUPPORTED_PROTOCOL", "This provider protocol is not supported by the configured Go task runner"}
	ErrIdempotency = &Error{"IDEMPOTENCY_KEY_REQUIRED", "Send one stable Idempotency-Key for each logical submission"}
)

// Decode rejects unknown fields, duplicate JSON keys, trailing values and deep
// documents, including duplicates inside the otherwise opaque requestBody.
func Decode(data []byte, dst any) error {
	if !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := checkJSON(d, 0); err != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return ErrInvalid
	}
	return nil
}

func checkJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || len(s) > 256 || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if err := checkJSON(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
	case json.Delim('['):
		count := 0
		for d.More() {
			count++
			if count > 1024 {
				return ErrInvalid
			}
			if err := checkJSON(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
	}
	return err
}

func ValidateKey(key string) error {
	if len(key) < 1 || len(key) > 128 {
		return ErrIdempotency
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return ErrIdempotency
		}
	}
	return nil
}

type Reference struct {
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

func (r Reference) Bytes() ([]byte, error) {
	if r.MIMEType != "image/png" && r.MIMEType != "image/jpeg" && r.MIMEType != "image/webp" {
		return nil, ErrInvalid
	}
	data := r.Data
	if strings.HasPrefix(data, "data:") {
		prefix := "data:" + r.MIMEType + ";base64,"
		if !strings.HasPrefix(data, prefix) {
			return nil, ErrInvalid
		}
		data = strings.TrimPrefix(data, prefix)
	}
	if len(data) == 0 || len(data) > MaxImageBody || strings.ContainsAny(data, "\r\n \t") {
		return nil, ErrInvalid
	}
	b, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || len(b) == 0 || len(b) > 8<<20 {
		return nil, ErrInvalid
	}
	return b, nil
}

type ImageRequest struct {
	APIKey            string      `json:"apiKey,omitempty"`
	BaseURL           string      `json:"baseUrl,omitempty"`
	Protocol          string      `json:"protocol,omitempty"`
	Mode              string      `json:"mode"`
	Prompt            string      `json:"prompt"`
	OutputSize        string      `json:"outputSize,omitempty"`
	CustomSize        string      `json:"customSize,omitempty"`
	AspectRatio       string      `json:"aspectRatio,omitempty"`
	Temperature       *float64    `json:"temperature,omitempty"`
	Model             string      `json:"model,omitempty"`
	Quality           string      `json:"gptImageQuality,omitempty"`
	Style             string      `json:"gptImageStyle,omitempty"`
	Background        string      `json:"gptImageBackground,omitempty"`
	ParallelCount     int         `json:"parallelCount"`
	Images            []Reference `json:"images,omitempty"`
	ReferenceAssetIDs []string    `json:"referenceAssetIds,omitempty"`
}

func (in ImageRequest) Validate() error {
	if in.APIKey != "" || in.BaseURL != "" {
		return ErrCredentials
	}
	if in.Protocol != "" && in.Protocol != "openai" {
		return ErrProtocol
	}
	if in.Mode != "text-to-image" && in.Mode != "image-to-image" || strings.TrimSpace(in.Prompt) == "" || utf8.RuneCountInString(in.Prompt) > 32000 || len(in.Model) > 200 {
		return ErrInvalid
	}
	if in.ParallelCount < 1 || in.ParallelCount > MaxParallel || len(in.Images)+len(in.ReferenceAssetIDs) > MaxReferences {
		return ErrInvalid
	}
	if in.Mode == "image-to-image" && len(in.Images)+len(in.ReferenceAssetIDs) == 0 || in.Mode == "text-to-image" && len(in.Images)+len(in.ReferenceAssetIDs) > 0 {
		return ErrInvalid
	}
	if in.Temperature != nil && (math.IsNaN(*in.Temperature) || *in.Temperature < 0 || *in.Temperature > 2) {
		return ErrInvalid
	}
	if !oneOf(in.Quality, "", "auto", "low", "medium", "high") || !oneOf(in.Background, "", "auto", "opaque", "transparent") || !oneOf(in.Style, "", "natural", "vivid") {
		return ErrInvalid
	}
	for _, id := range in.ReferenceAssetIDs {
		if !strings.HasPrefix(id, "asset_") || len(id) > 100 {
			return ErrInvalid
		}
	}
	_, err := in.Size()
	return err
}

// Size preserves the embedded bridge's nearest billed-size mapping. The
// returned dimensions, not the UI label, are charged and dispatched.
func (in ImageRequest) Size() (string, error) {
	tier := in.OutputSize
	if tier == "" || tier == "auto" || tier == "512" {
		tier = "1K"
	}
	if !oneOf(tier, "1K", "2K", "4K") {
		return "", ErrInvalid
	}
	ratio := 1.0
	aspect := in.AspectRatio
	if aspect != "" && aspect != "auto" {
		if !oneOf(aspect, "1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5", "5:4", "8:1", "9:16", "16:9", "21:9") {
			return "", ErrInvalid
		}
		parts := strings.Split(aspect, ":")
		w, _ := strconv.Atoi(parts[0])
		h, _ := strconv.Atoi(parts[1])
		ratio = float64(w) / float64(h)
	}
	if in.CustomSize != "" {
		parts := strings.Split(in.CustomSize, "x")
		if len(parts) != 2 {
			return "", ErrInvalid
		}
		w, ew := strconv.Atoi(parts[0])
		h, eh := strconv.Atoi(parts[1])
		if ew != nil || eh != nil || w < 256 || h < 256 || w > 4096 || h > 4096 {
			return "", ErrInvalid
		}
		ratio = float64(w) / float64(h)
		if ratio < 0.125 || ratio > 8 {
			return "", ErrInvalid
		}
		tier = "1K"
		if max(w, h) >= 1800 {
			tier = "2K"
		}
		if max(w, h) >= 2800 {
			tier = "4K"
		}
	}
	ratios := []float64{1, 5.0 / 4, 9.0 / 16, 16.0 / 9, 4.0 / 3, 3.0 / 2, 4.0 / 5, 3.0 / 4, 2.0 / 3, 21.0 / 9}
	sizes := map[string][]string{
		"1K": {"1024x1024", "1040x832", "720x1280", "1280x720", "1024x768", "1008x672", "832x1040", "768x1024", "672x1008", "1344x576"},
		"2K": {"2048x2048", "2080x1664", "1152x2048", "2048x1152", "2048x1536", "2016x1344", "1664x2080", "1536x2048", "1344x2016", "2016x864"},
		"4K": {"2880x2880", "3200x2560", "2160x3840", "3840x2160", "3264x2448", "3504x2336", "2560x3200", "2448x3264", "2336x3504", "3696x1584"},
	}
	best := 0
	for i := range ratios {
		if math.Abs(math.Log(ratios[i]/ratio)) < math.Abs(math.Log(ratios[best]/ratio)) {
			best = i
		}
	}
	return sizes[tier][best], nil
}

type TextRequest struct {
	APIKey      string                     `json:"apiKey,omitempty"`
	BaseURL     string                     `json:"baseUrl,omitempty"`
	Protocol    string                     `json:"protocol"`
	Model       string                     `json:"model,omitempty"`
	RequestBody map[string]json.RawMessage `json:"requestBody"`
	Stream      bool                       `json:"stream"`
}

// CanvasBody adapts only transport names. The creative canvas API remains the
// authority for model selection, options, messages and owned vision inputs.
func (in TextRequest) CanvasBody() (map[string]any, error) {
	if in.APIKey != "" || in.BaseURL != "" {
		return nil, ErrCredentials
	}
	protocol := ""
	switch in.Protocol {
	case "openai-chat-completions", "openai_chat_completions":
		protocol = "openai_chat_completions"
	case "openai-responses", "openai_responses":
		protocol = "openai_responses"
	default:
		return nil, ErrProtocol
	}
	if len(in.Model) > 200 || len(in.RequestBody) == 0 || len(in.RequestBody) > 40 {
		return nil, ErrInvalid
	}
	body := make(map[string]json.RawMessage, len(in.RequestBody))
	for k, v := range in.RequestBody {
		if k == "stream" {
			var flag bool
			if json.Unmarshal(v, &flag) != nil || flag != in.Stream {
				return nil, ErrInvalid
			}
			continue
		}
		body[k] = v
	}
	return map[string]any{"protocol": protocol, "model": in.Model, "request_body": body, "feature": "nova"}, nil
}

type Prompt struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Type    int    `json:"type"`
}
type Config struct {
	PromptGalleryMode            string `json:"promptGalleryMode"`
	PromptGalleryPasswordEnabled bool   `json:"promptGalleryPasswordEnabled"`
}
type Settings struct {
	Mode     string   `json:"mode"`
	Prompts  []Prompt `json:"prompts"`
	Keywords []string `json:"keywords"`
}

// Upstream source data only; never a provider credential or runtime database.
//
//go:embed defaults/prompts.json
var defaultPrompts []byte

//go:embed defaults/blacklist.json
var defaultBlacklist []byte

func DefaultSettings() Settings {
	s := Settings{Mode: "2", Prompts: []Prompt{}, Keywords: []string{}}
	var blacklist struct {
		Keywords []string `json:"keywords"`
	}
	_ = json.Unmarshal(defaultPrompts, &s.Prompts)
	_ = json.Unmarshal(defaultBlacklist, &blacklist)
	s.Keywords = blacklist.Keywords
	return s
}
func (s Settings) Validate() error {
	if !oneOf(s.Mode, "1", "2", "3") || len(s.Prompts) > 256 || len(s.Keywords) > 512 {
		return ErrInvalid
	}
	total := 0
	for _, p := range s.Prompts {
		if strings.TrimSpace(p.Title) == "" || len(p.Title) > 512 || strings.TrimSpace(p.Content) == "" || len(p.Content) > 32000 || p.Type < 1 || p.Type > 2 {
			return ErrInvalid
		}
		total += len(p.Title) + len(p.Content)
	}
	for _, k := range s.Keywords {
		if strings.TrimSpace(k) == "" || len(k) > 256 {
			return ErrInvalid
		}
		total += len(k)
	}
	if total > 1<<20 {
		return ErrInvalid
	}
	return nil
}

type TaskResult struct {
	Images  []string        `json:"images,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type TaskView struct {
	ID                    string      `json:"id"`
	Kind                  string      `json:"kind"`
	Mode                  string      `json:"mode,omitempty"`
	Status                string      `json:"status"`
	Result                *TaskResult `json:"result,omitempty"`
	Warning               string      `json:"warning,omitempty"`
	Error                 string      `json:"error,omitempty"`
	CancellationRequested bool        `json:"cancellationRequested"`
	CreatedAt             time.Time   `json:"createdAt"`
	CompletedAt           *time.Time  `json:"completedAt,omitempty"`
	AcknowledgedAt        *time.Time  `json:"acknowledgedAt,omitempty"`
}

// Aggregate treats an uncertain provider submission as processing, never as a
// retryable failure. Cancellation is final only when the business task says so.
func Aggregate(states []string) (status, warning string) {
	if len(states) == 0 {
		return "failed", "Task mapping is unavailable"
	}
	success, terminal, cancelled, active := 0, 0, 0, false
	for _, s := range states {
		switch s {
		case "succeeded":
			success++
			terminal++
		case "failed", "deleted":
			terminal++
		case "cancelled":
			terminal++
			cancelled++
		case "queued":
		default:
			active = true
		}
	}
	if terminal < len(states) {
		if active || terminal > 0 {
			return "processing", ""
		}
		return "queued", ""
	}
	if success > 0 {
		if success < len(states) {
			return "completed", fmt.Sprintf("%d of %d outputs completed", success, len(states))
		}
		return "completed", ""
	}
	if cancelled == len(states) {
		return "cancelled", ""
	}
	return "failed", ""
}

type QueueStatus struct {
	ConcurrencyLimit      int    `json:"concurrencyLimit"`
	ConfiguredConcurrency int    `json:"configuredConcurrency"`
	ProcessingCount       int    `json:"processingCount"`
	QueuedCount           int    `json:"queuedCount"`
	PendingCount          int    `json:"pendingCount"`
	MaxQueueSize          int    `json:"maxQueueSize"`
	RemainingQueueSlots   int    `json:"remainingQueueSlots"`
	DisplayConcurrency    int    `json:"displayConcurrency"`
	DisplayQueued         int    `json:"displayQueued"`
	AcceptingNewTasks     bool   `json:"acceptingNewTasks"`
	RetryAfterSeconds     int    `json:"retryAfterSeconds,omitempty"`
	ServerMessage         string `json:"serverMessage,omitempty"`
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
