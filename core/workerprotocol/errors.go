package workerprotocol

import "strings"

type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

func (ErrorResponse) SchemaName() string { return "errorResponse" }
func (v ErrorResponse) Validate() error  { return validateDTO(v) }

// ProtocolError contains only safe, fixed text; it never embeds a payload,
// raw lease, signing key, request URL, or worker-reported text.
type ProtocolError struct {
	HTTPStatus int
	Detail     ErrorDetail
}

func (e *ProtocolError) Error() string           { return e.Detail.Code + ": " + e.Detail.Message }
func (e *ProtocolError) Response() ErrorResponse { return ErrorResponse{Error: e.Detail} }

type errorSpec struct {
	status  int
	message string
}

var errorSpecs = map[string]errorSpec{
	"REQUEST_SCHEMA_INVALID":       {400, "Request body does not match Local Worker Protocol v1"},
	"WORKER_ID_INVALID":            {400, "workerId must be 3-120 safe characters"},
	"CAPABILITIES_REQUIRED":        {400, "At least one supported capability is required"},
	"IDEMPOTENCY_KEY_INVALID":      {400, "idempotencyKey must be 8-128 safe characters"},
	"ARTIFACT_EMPTY":               {400, "Artifact file is required"},
	"ARTIFACT_INVALID":             {400, "Artifact is not a valid supported image"},
	"ARTIFACT_SHA256_INVALID":      {400, "sha256 must be a 64-character hexadecimal SHA-256 digest"},
	"ARTIFACT_SHA256_MISMATCH":     {400, "Artifact SHA-256 does not match the uploaded bytes"},
	"ARTIFACT_MIME_UNSUPPORTED":    {400, "Artifact must normalize to JPEG, PNG, or WebP"},
	"ARTIFACT_REQUIRED":            {400, "A valid artifactId is required for image completion"},
	"ARTIFACT_NOT_ALLOWED":         {400, "Artifacts are only accepted for image jobs"},
	"ARTIFACT_READ_FAILED":         {400, "Artifact could not be read"},
	"RESULT_REQUIRED":              {400, "Text completion requires result"},
	"RESULT_TOO_LARGE":             {400, "Completion metadata is too large"},
	"RESULT_SECRET_NOT_ALLOWED":    {400, "Worker results cannot contain credentials or local connection configuration"},
	"BASE64_RESULT_NOT_ALLOWED":    {400, "Upload binary output through the artifacts endpoint"},
	"WORKER_TOKEN_MISSING":         {401, "Worker token is required"},
	"WORKER_TOKEN_INVALID":         {401, "Worker token is invalid or revoked"},
	"WORKER_TOKEN_MISMATCH":        {403, "Worker belongs to another token"},
	"WORKER_MISMATCH":              {403, "Worker does not own this lease"},
	"LEASE_ID_INVALID":             {403, "leaseId is invalid"},
	"ARTIFACT_WORKER_MISMATCH":     {403, "Artifact belongs to another Worker"},
	"INPUT_SIGNATURE_INVALID":      {403, "Input signature is invalid"},
	"JOB_NOT_FOUND":                {404, "Worker job does not exist"},
	"INPUT_NOT_FOUND":              {404, "Input file does not exist"},
	"WORKER_ID_IN_USE":             {409, "workerId is already registered to another token"},
	"JOB_NOT_LEASED":               {409, "Worker job is not leased"},
	"LEASE_EXPIRED":                {409, "Worker lease has expired"},
	"IDEMPOTENCY_CONFLICT":         {409, "The idempotency key was already used with different content"},
	"JOB_TERMINAL_CONFLICT":        {409, "Worker job already has a different terminal result"},
	"JOB_CANCELLED":                {410, "Worker job was cancelled"},
	"INPUT_URL_EXPIRED":            {410, "Input URL has expired"},
	"HTTPS_REQUIRED":               {426, "Worker requests require HTTPS"},
	"WORKER_INTERNAL_ERROR":        {500, "Worker internal error"},
	"WORKER_HEARTBEAT_REQUIRED":    {400, "Worker must send heartbeat before this request"},
	"WORKER_WAIT_INTERRUPTED":      {500, "Worker wait was interrupted"},
	"LOCAL_WORKER_OFFLINE":         {409, "No Worker claimed the job"},
	"CAPABILITY_INVALID":           {400, "Worker capability is invalid"},
	"PROTOCOL_INVALID":             {400, "Worker protocol is invalid"},
	"MODEL_NOT_ALLOWED":            {400, "Worker model is not allowed for capability"},
	"PROTOCOL_CAPABILITY_MISMATCH": {400, "Worker capability and protocol do not match"},
	"PAYLOAD_FIELD_NOT_ALLOWED":    {400, "Worker request field is not allowed"},
	"PAYLOAD_MODEL_MISMATCH":       {400, "Worker request model must match the configured Provider model"},
	"PAYLOAD_SECRET_NOT_ALLOWED":   {400, "Worker jobs cannot contain credentials or local connection configuration"},
	"PAYLOAD_INVALID":              {400, "Worker request payload is invalid"},
	"WORKER_REQUEST_TOO_LARGE":     {400, "Worker request payload is too large"},
	"STREAM_NOT_ALLOWED":           {400, "Local Worker Protocol v1 does not allow streaming responses"},
	"BASE64_REQUEST_NOT_ALLOWED":   {400, "Worker input images must use signed HTTPS URLs"},
	"INPUT_HTTPS_REQUIRED":         {400, "Input source must use HTTPS"},
	"INPUT_URL_INVALID":            {400, "Worker input URL is invalid"},
	"INPUT_URL_NOT_PUBLIC":         {400, "Worker input URL must be public"},
	"INPUT_FILES_REQUIRED":         {400, "Image edit jobs require at least one signed input image"},
	"INPUT_FILES_NOT_ALLOWED":      {400, "This protocol does not accept input files"},
	"INPUT_FILES_TOO_MANY":         {400, "Worker jobs accept at most 10 input files"},
}

// NewError constructs a safe protocol error. Unknown server codes fail closed
// as WORKER_INTERNAL_ERROR, rather than echoing an arbitrary string.
func NewError(code string) *ProtocolError {
	s, ok := errorSpecs[code]
	if !ok {
		code = "WORKER_INTERNAL_ERROR"
		s = errorSpecs[code]
	}
	return &ProtocolError{HTTPStatus: s.status, Detail: ErrorDetail{Code: code, Message: s.message, Retryable: Retryable(s.status, code)}}
}

// Retryable mirrors the Java error policy, including the two output-size
// categories explicitly covered by AiWorkerErrorPolicyTest.
func Retryable(status int, code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch code {
	case "WORKER_HEARTBEAT_REQUIRED", "WORKER_INTERNAL_ERROR", "WORKER_WAIT_INTERRUPTED", "LOCAL_WORKER_OFFLINE", "ARTIFACT_READ_FAILED", "NETWORK_ERROR", "UPSTREAM_TIMEOUT", "UPSTREAM_RATE_LIMITED", "UPSTREAM_UNAVAILABLE", "WORKER_RUNTIME_ERROR", "OUTPUT_DIMENSION_MISMATCH", "OUTPUT_ASPECT_RATIO_MISMATCH":
		return true
	}
	return strings.HasPrefix(code, "NETWORK_") || strings.HasPrefix(code, "UPSTREAM_5") || status == 408 || status == 429 || (status >= 500 && status <= 599)
}

func MustStop(code string) bool {
	switch code {
	case "JOB_CANCELLED", "LEASE_EXPIRED", "WORKER_MISMATCH", "LEASE_ID_INVALID", "JOB_TERMINAL_CONFLICT":
		return true
	}
	return false
}

// ShouldRetryTransport honors the envelope, not just an HTTP status.
func ShouldRetryTransport(response ErrorResponse) bool {
	return !MustStop(response.Error.Code) && response.Error.Retryable
}
