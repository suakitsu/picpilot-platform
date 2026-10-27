// Package video implements PicPilot's single-provider video protocol adapters.
// It does not select routes, retry submissions, persist tasks, or charge users.
package video

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Provider string

const (
	Gemini   Provider = "gemini"
	Seedance Provider = "seedance"
	HFSY     Provider = "hfsy"
	Agnes    Provider = "agnes"
)

type Status string

const (
	Submitted  Status = "submitted"
	Generating Status = "generating"
	Completed  Status = "completed"
	Failed     Status = "failed"
)

// Config binds a gateway to one provider and credential. Client and its Transport
// are required: the parent must supply DNS/IP/egress policy, bounded dialing/TLS,
// and a transport that does not retry POSTs or inject credentials into downloads.
// The gateway copies Client, removes its cookie jar, and disables all redirects.
type Config struct {
	Provider         Provider
	APIKey           string `json:"-"`
	BaseURL          string
	Client           *http.Client `json:"-"`
	PublicBaseURL    string
	WebhookURL       string        `json:"-"` // APIMart base URL, before its appended /callback.
	AgnesModel       string        // Optional explicitly trusted custom model.
	AllowHTTP        bool          // Only for isolated local HTTP mocks.
	SubmitTimeout    time.Duration // Default and maximum: 45 seconds.
	PollTimeout      time.Duration // Default and maximum: 30 seconds.
	FetchTimeout     time.Duration // Default and maximum: 5 minutes.
	MaxResponseBytes int64         // Default 1 MiB; maximum 4 MiB.
	MaxResultBytes   int64         // Default and maximum: 500 MiB.
}

// Request contains only the inputs supported by the Java video gateway. Zero
// duration/resolution/aspect ratio select documented defaults. Options belonging
// to another provider are rejected instead of being silently dropped.
type Request struct {
	Model           string
	Prompt          string   `json:"-"`
	SourceURLs      []string `json:"-"`
	AspectRatio     string
	DurationSeconds int
	Resolution      string
	GenerateAudio   *bool  // APIMart Seedance only; nil means true.
	Seed            *int64 // APIMart Seedance only; 0..2147483647.
	Watermark       *bool  // HFSY only; nil means false.
}

// Submission is safe to persist; it contains no credential, prompt, or body.
// Persist the chosen credential identity separately so polling stays pinned.
type Submission struct {
	UpstreamTaskID string   `json:"upstream_task_id"`
	Provider       Provider `json:"provider"`
	Model          string   `json:"model"`
}

// PollResult contains allowlisted, validated metadata only. Seconds and monetary
// metadata are decimal strings to avoid losing upstream precision. Error is a
// fixed safe message, never the upstream failure text (which can echo prompts).
type PollResult struct {
	Status    Status            `json:"status"`
	Progress  int               `json:"progress"`
	ResultURL string            `json:"result_url,omitempty"`
	Width     *int              `json:"width,omitempty"`
	Height    *int              `json:"height,omitempty"`
	Seconds   string            `json:"seconds,omitempty"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Callback must be correlated with a persisted task after the parent's webhook
// authentication. Parsing a callback does not authenticate or settle it.
type Callback struct {
	UpstreamTaskID string     `json:"upstream_task_id"`
	Result         PollResult `json:"result"`
}

type ErrorKind string

const (
	InvalidRequest ErrorKind = "invalid_request"
	Rejected       ErrorKind = "rejected"
	Ambiguous      ErrorKind = "submission_unknown"
	TransportError ErrorKind = "transport"
	ProtocolError  ErrorKind = "protocol"
	LimitExceeded  ErrorKind = "limit_exceeded"
)

var (
	ErrInvalidRequest = errors.New("video: invalid request")
	ErrRejected       = errors.New("video: upstream rejected request")
	ErrAmbiguous      = errors.New("video: submission outcome unknown")
	ErrTransport      = errors.New("video: transport failure")
	ErrProtocol       = errors.New("video: invalid upstream response")
	ErrLimit          = errors.New("video: configured limit exceeded")
)

// Error deliberately excludes upstream response bodies, URL errors, and request
// headers. RetryAfter is advisory, never permission to resubmit an unknown task.
type Error struct {
	Kind       ErrorKind
	Operation  string
	Code       string
	HTTPStatus int
	RetryAfter time.Duration
	cause      error
}

func (e *Error) Error() string {
	return fmt.Sprintf("video: %s: %s (%s, HTTP %d)", e.Operation, e.Kind, e.Code, e.HTTPStatus)
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Is(target error) bool {
	switch e.Kind {
	case InvalidRequest:
		return target == ErrInvalidRequest
	case Rejected:
		return target == ErrRejected
	case Ambiguous:
		return target == ErrAmbiguous
	case TransportError:
		return target == ErrTransport
	case ProtocolError:
		return target == ErrProtocol
	case LimitExceeded:
		return target == ErrLimit
	}
	return false
}

func failure(kind ErrorKind, op, code string) *Error {
	return &Error{Kind: kind, Operation: op, Code: code}
}

func contextFailure(kind ErrorKind, op string, err error) *Error {
	e := failure(kind, op, "request_interrupted")
	if errors.Is(err, context.Canceled) {
		e.cause = context.Canceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		e.cause = context.DeadlineExceeded
	}
	return e
}

// Gateway is immutable after construction and supports concurrent calls if the
// injected RoundTripper does. No method retries, falls back, or starts a goroutine.
type Gateway struct {
	config Config
	client http.Client
	base   *url.URL
}

// Formatting potentially sensitive inputs never prints their contents.
func (Config) Format(s fmt.State, _ rune)  { _, _ = s.Write([]byte("video.Config{redacted}")) }
func (Request) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("video.Request{redacted}")) }
func (*Gateway) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("video.Gateway{redacted}"))
}
