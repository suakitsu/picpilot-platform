package provider

import (
	"context"
	"errors"
	"fmt"
)

type ErrorKind string

const (
	InvalidRequest   ErrorKind = "invalid_request"
	Unsupported      ErrorKind = "unsupported"
	BlockedAddress   ErrorKind = "blocked_address"
	Authentication   ErrorKind = "authentication"
	RateLimited      ErrorKind = "rate_limited"
	UpstreamHTTP     ErrorKind = "upstream_http"
	UpstreamFailure  ErrorKind = "upstream_failure"
	InvalidResponse  ErrorKind = "invalid_response"
	LimitExceeded    ErrorKind = "limit_exceeded"
	TransportFailure ErrorKind = "transport_failure"
	Canceled         ErrorKind = "canceled"
	TimedOut         ErrorKind = "timed_out"
)

type Submission string

const (
	NotSubmitted Submission = "not_submitted"
	Rejected     Submission = "rejected"
	Uncertain    Submission = "uncertain"
)

// Error contains only bounded, local diagnostic enums and an HTTP status. No
// URLs, upstream messages, prompts, headers, keys, or underlying network errors
// are retained. Uncertain also covers a completed generation whose output could
// not be retrieved/validated: resubmitting could charge the caller twice.
type Error struct {
	Kind       ErrorKind
	Submission Submission
	StatusCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("provider: %s (submission=%s, http=%d)", e.Kind, e.Submission, e.StatusCode)
}

// Definitive means no generation was submitted, or the upstream explicitly
// rejected it. It is not a retry recommendation (auth/validation will still fail).
func (e *Error) Definitive() bool { return e.Submission == NotSubmitted || e.Submission == Rejected }

func (e *Error) Is(target error) bool {
	return e.Kind == Canceled && target == context.Canceled || e.Kind == TimedOut && target == context.DeadlineExceeded
}

func fail(kind ErrorKind, submission Submission) *Error {
	return &Error{Kind: kind, Submission: submission}
}

func contextual(err error, submission Submission) *Error {
	if errors.Is(err, context.Canceled) {
		return fail(Canceled, submission)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(TimedOut, submission)
	}
	var pe *Error
	if errors.As(err, &pe) {
		return &Error{Kind: pe.Kind, Submission: submission, StatusCode: pe.StatusCode}
	}
	return fail(TransportFailure, submission)
}
