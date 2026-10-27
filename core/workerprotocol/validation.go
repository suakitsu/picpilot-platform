package workerprotocol

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var workerPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{3,120}$`)
var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ValidateWorkerID(s string) error {
	if !workerPattern.MatchString(s) {
		return NewError("WORKER_ID_INVALID")
	}
	return nil
}
func ValidateLeaseID(s string) error {
	n := utf8.RuneCountInString(s)
	if n < 16 || n > 128 || strings.TrimSpace(s) == "" {
		return NewError("LEASE_ID_INVALID")
	}
	return nil
}
func ValidateIdempotencyKey(s string) error {
	if !idempotencyPattern.MatchString(s) {
		return NewError("IDEMPOTENCY_KEY_INVALID")
	}
	return nil
}
func ValidateSHA256(s string) error {
	if !shaPattern.MatchString(s) {
		return NewError("ARTIFACT_SHA256_INVALID")
	}
	return nil
}
func validateIdentity(worker, lease string) error {
	if err := ValidateWorkerID(worker); err != nil {
		return err
	}
	return ValidateLeaseID(lease)
}
func equalConstant(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func SHA256(data []byte) string      { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (v HeartbeatRequest) Validate() error {
	if err := ValidateWorkerID(v.WorkerID); err != nil {
		return err
	}
	return validateDTO(v)
}
func (v HeartbeatResponse) Validate() error {
	if v.ServerTime.IsZero() {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return validateDTO(v)
}
func (v ClaimRequest) Validate() error {
	if err := ValidateWorkerID(v.WorkerID); err != nil {
		return err
	}
	return validateDTO(v)
}
func (v LeaseRequest) Validate() error {
	if err := validateIdentity(v.WorkerID, v.LeaseID); err != nil {
		return err
	}
	return validateDTO(v)
}
func validTimes(renew, lease, grace time.Time) bool {
	return !renew.IsZero() && !lease.IsZero() && !grace.IsZero() && !renew.After(lease) && !lease.After(grace)
}
func (v LeaseResponse) Validate() error {
	if !validTimes(v.RenewAfter, v.LeaseUntil, v.GraceUntil) {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return validateDTO(v)
}
func (v ClaimResponse) Validate() error {
	if v.ServerTime.IsZero() || v.Empty != (v.Task == nil) {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	if err := validateDTO(v); err != nil {
		return err
	}
	if v.Task != nil {
		return v.Task.Validate()
	}
	return nil
}
func (v ClaimedTask) Validate() error {
	if !validTimes(v.RenewAfter, v.LeaseUntil, v.GraceUntil) {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	if err := validateDTO(v); err != nil {
		return err
	}
	if err := ValidatePayload(v.Capability, v.Protocol, v.Model, v.Request); err != nil {
		return err
	}
	return ValidateInputFiles(v.Protocol, v.JobID, v.InputFiles)
}
func (v InputFile) Validate() error {
	if err := validateDTO(v); err != nil {
		return err
	}
	if v.URLExpiresAt.IsZero() {
		return NewError("INPUT_SIGNATURE_INVALID")
	}
	if err := ValidatePublicHTTPSURL(v.URL); err != nil {
		return err
	}
	u, _ := url.Parse(v.URL)
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return NewError("INPUT_SIGNATURE_INVALID")
	}
	if len(q["signature"]) != 1 || len(q["expires"]) != 1 || len(q["workerId"]) != 1 || !equalConstant(q.Get("signature"), v.URLChecksum) {
		return NewError("INPUT_SIGNATURE_INVALID")
	}
	expires, err := strconv.ParseInt(q.Get("expires"), 10, 64)
	if err != nil || expires != v.URLExpiresAt.Unix() || ValidateWorkerID(q.Get("workerId")) != nil {
		return NewError("INPUT_SIGNATURE_INVALID")
	}
	return nil
}
func ValidateInputFiles(protocol Protocol, jobID string, files []InputFile) error {
	if protocol == ImageEdits {
		if len(files) < 1 {
			return NewError("INPUT_FILES_REQUIRED")
		}
	} else if len(files) > 0 {
		return NewError("INPUT_FILES_NOT_ALLOWED")
	}
	if len(files) > 10 {
		return NewError("INPUT_FILES_TOO_MANY")
	}
	b, err := json.Marshal(files)
	if err != nil || len(b) > MaxInputMetadataBytes {
		return NewError("WORKER_REQUEST_TOO_LARGE")
	}
	for i, f := range files {
		if f.Index != i {
			return NewError("REQUEST_SCHEMA_INVALID")
		}
		if err := f.Validate(); err != nil {
			return err
		}
		u, _ := url.Parse(f.URL)
		if u.Path != BasePath+"/inputs/"+jobID+"/"+strconv.Itoa(i) {
			return NewError("INPUT_SIGNATURE_INVALID")
		}
	}
	return nil
}

func (v ArtifactForm) Validate() error {
	if err := validateIdentity(v.WorkerID, v.LeaseID); err != nil {
		return err
	}
	if err := ValidateIdempotencyKey(v.IdempotencyKey); err != nil {
		return err
	}
	return ValidateSHA256(v.SHA256)
}

// ValidateMultipartFields rejects unexpected or repeated scalar/file fields.
// The transport supplies the names of file parts without reading file content.
func ValidateMultipartFields(fields map[string][]string, fileParts []string) (ArtifactForm, error) {
	v := ArtifactForm{}
	if len(fields) != 4 || len(fileParts) != 1 || fileParts[0] != "file" {
		return v, NewError("REQUEST_SCHEMA_INVALID")
	}
	for _, k := range []string{"workerId", "leaseId", "idempotencyKey", "sha256"} {
		if len(fields[k]) != 1 {
			return v, NewError("REQUEST_SCHEMA_INVALID")
		}
	}
	v = ArtifactForm{WorkerID: fields["workerId"][0], LeaseID: fields["leaseId"][0], IdempotencyKey: fields["idempotencyKey"][0], SHA256: fields["sha256"][0]}
	return v, v.Validate()
}
func VerifyArtifactSource(expected string, data []byte, maxBytes int64) error {
	if err := ValidateSHA256(expected); err != nil {
		return err
	}
	if len(data) == 0 {
		return NewError("ARTIFACT_EMPTY")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxArtifactBytes
	}
	if int64(len(data)) > maxBytes {
		return NewError("ARTIFACT_INVALID")
	}
	if !equalConstant(strings.ToLower(expected), SHA256(data)) {
		return NewError("ARTIFACT_SHA256_MISMATCH")
	}
	return nil
}
func (v ArtifactResponse) Validate() error {
	if err := validateDTO(v); err != nil {
		return err
	}
	if v.ContentType != "image/"+v.Format {
		return NewError("ARTIFACT_MIME_UNSUPPORTED")
	}
	if int64(v.Width) > MaxArtifactPixels/int64(v.Height) {
		return NewError("ARTIFACT_INVALID")
	}
	return nil
}

func (v CompleteRequest) Validate() error {
	if err := validateIdentity(v.WorkerID, v.LeaseID); err != nil {
		return err
	}
	if err := ValidateIdempotencyKey(v.IdempotencyKey); err != nil {
		return err
	}
	if err := validateDTO(v); err != nil {
		return err
	}
	if (strings.TrimSpace(v.ArtifactID) != "") == (len(v.Result) > 0) {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	for _, raw := range []json.RawMessage{v.Result, v.Metadata} {
		if len(raw) > 0 {
			value, err := parseJSON(raw)
			if err != nil {
				return err
			}
			if err := inspect(value, true); err != nil {
				return err
			}
		}
	}
	if len(v.Result) > 0 {
		value, _ := parseJSON(v.Result)
		if value == nil {
			return NewError("RESULT_REQUIRED")
		}
	}
	// Java limits the stored normalized result wrapper, not the transport fields.
	value := map[string]json.RawMessage{}
	if len(v.Result) > 0 {
		value["result"] = v.Result
	}
	if len(v.Metadata) > 0 {
		value["metadata"] = v.Metadata
	}
	b, err := json.Marshal(value)
	if err != nil {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	if len(b) > MaxResultBytes {
		return NewError("RESULT_TOO_LARGE")
	}
	return nil
}
func (v FailRequest) Validate() error {
	if err := validateIdentity(v.WorkerID, v.LeaseID); err != nil {
		return err
	}
	if err := ValidateIdempotencyKey(v.IdempotencyKey); err != nil {
		return err
	}
	if strings.TrimSpace(v.ErrorCode) == "" {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return validateDTO(v)
}
func (v TerminalResponse) Validate() error { return validateDTO(v) }

func (v ResponsesPayload) Validate() error {
	b, err := json.Marshal(v)
	if err != nil {
		return NewError("PAYLOAD_INVALID")
	}
	return ValidatePayload(Text, Responses, v.Model, b)
}
func (v ChatCompletionsPayload) Validate() error {
	b, err := json.Marshal(v)
	if err != nil {
		return NewError("PAYLOAD_INVALID")
	}
	return ValidatePayload(Text, ChatCompletions, v.Model, b)
}
func (v ImageGenerationPayload) Validate() error {
	b, err := json.Marshal(v)
	if err != nil {
		return NewError("PAYLOAD_INVALID")
	}
	return ValidatePayload(Image, ImageGenerations, v.Model, b)
}
func (v ImageEditPayload) Validate() error {
	b, err := json.Marshal(v)
	if err != nil {
		return NewError("PAYLOAD_INVALID")
	}
	return ValidatePayload(Image, ImageEdits, v.Model, b)
}

func ValidatePayload(capability Capability, protocol Protocol, model string, data []byte) error {
	if capability != Image && capability != Text {
		return NewError("CAPABILITY_INVALID")
	}
	def := ""
	isImage := false
	switch protocol {
	case Responses:
		def = "responsesPayload"
	case ChatCompletions:
		def = "chatCompletionsPayload"
	case ImageGenerations:
		def = "imageGenerationPayload"
		isImage = true
	case ImageEdits:
		def = "imageEditPayload"
		isImage = true
	default:
		return NewError("PROTOCOL_INVALID")
	}
	if (capability == Image) != isImage {
		return NewError("PROTOCOL_CAPABILITY_MISMATCH")
	}
	allowed := model == ImageModel && isImage
	if !isImage {
		for _, m := range TextModels() {
			allowed = allowed || model == m
		}
	}
	if !allowed {
		return NewError("MODEL_NOT_ALLOWED")
	}
	if len(data) > MaxRequestBytes {
		return NewError("WORKER_REQUEST_TOO_LARGE")
	}
	v, err := parseJSON(data)
	if err != nil {
		return err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return NewError("PAYLOAD_INVALID")
	}
	props := definitions[def].(map[string]any)["properties"].(map[string]any)
	for _, k := range sortedKeys(m) {
		if _, ok := props[k]; !ok {
			return NewError("PAYLOAD_FIELD_NOT_ALLOWED")
		}
	}
	if err := inspect(v, false); err != nil {
		return err
	}
	if m["model"] != model {
		return NewError("PAYLOAD_MODEL_MISMATCH")
	}
	if stream, exists := m["stream"]; exists && stream != false {
		return NewError("STREAM_NOT_ALLOWED")
	}
	if !matchesSchema(definitions[def].(map[string]any), v) {
		return NewError("PAYLOAD_INVALID")
	}
	if protocol == Responses {
		switch x := m["input"].(type) {
		case nil:
			return NewError("PAYLOAD_INVALID")
		case string:
			if strings.TrimSpace(x) == "" {
				return NewError("PAYLOAD_INVALID")
			}
		case []any:
			if len(x) == 0 {
				return NewError("PAYLOAD_INVALID")
			}
		}
	}
	if isImage && strings.TrimSpace(m["prompt"].(string)) == "" {
		return NewError("PAYLOAD_INVALID")
	}
	return nil
}

func inspect(v any, result bool) error {
	secretCode, binaryCode := "PAYLOAD_SECRET_NOT_ALLOWED", "BASE64_REQUEST_NOT_ALLOWED"
	if result {
		secretCode, binaryCode = "RESULT_SECRET_NOT_ALLOWED", "BASE64_RESULT_NOT_ALLOWED"
	}
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			value := x[k]
			n := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.TrimSpace(k)))
			switch n {
			case "authorization", "apikey", "accesstoken", "refreshtoken", "idtoken", "clientsecret", "cliproxyapikey", "openaioauth", "oauth", "cookie", "cookies", "baseurl", "proxyurl", "proxy", "proxyconfig", "proxyconfiguration", "localurl", "localbaseurl":
				return NewError(secretCode)
			}
			if n == "b64json" || n == "base64" {
				return NewError(binaryCode)
			}
			if err := inspect(value, result); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range x {
			if err := inspect(value, result); err != nil {
				return err
			}
		}
	case string:
		s := strings.TrimSpace(x)
		lower := strings.ToLower(s)
		if (strings.HasPrefix(lower, "data:") && strings.Contains(lower, ";base64,")) || (len(s) > 8192 && base64Pattern.MatchString(s)) {
			return NewError(binaryCode)
		}
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			u, err := url.Parse(s)
			if err == nil && (u.User != nil || localHost(u.Hostname())) {
				return NewError(secretCode)
			}
		}
	}
	return nil
}

// ValidatePublicHTTPSURL performs literal host checks only. The parent must
// also enforce DNS/redirect/egress policy before any network operation.
func ValidatePublicHTTPSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return NewError("INPUT_URL_INVALID")
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return NewError("INPUT_HTTPS_REQUIRED")
	}
	if localHost(u.Hostname()) {
		return NewError("INPUT_URL_NOT_PUBLIC")
	}
	return nil
}
func localHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if h == "" || h == "localhost" || h == "metadata" || h == "instance-data" {
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	if strings.Contains(h, "%") {
		return true
	}
	a, err := netip.ParseAddr(h)
	if err == nil {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			return true
		}
		for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "100::/64"} {
			if netip.MustParsePrefix(cidr).Contains(a) {
				return true
			}
		}
		return false
	}
	// Reject single-label/local hosts and ambiguous numeric IPv4 spellings.
	if !strings.Contains(h, ".") || regexp.MustCompile(`^[0-9.]+$`).MatchString(h) || regexp.MustCompile(`(?i)^(0x[0-9a-f]+)(\.|$)`).MatchString(h) {
		return true
	}
	return false
}

// ExtractText accepts the four documented result families. It does not log,
// store, or silently stringify arbitrary upstream objects.
func ExtractText(data json.RawMessage) (string, error) {
	v, err := parseJSON(data)
	if err != nil {
		return "", err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", NewError("RESULT_REQUIRED")
	}
	for _, key := range []string{"text", "output_text"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return s, nil
		}
	}
	if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if text, ok := message["content"].(string); ok && strings.TrimSpace(text) != "" {
					return text, nil
				}
			}
		}
	}
	texts := []string{}
	if output, ok := m["output"].([]any); ok {
		for _, item := range output {
			if item, ok := item.(map[string]any); ok {
				if content, ok := item["content"].([]any); ok {
					for _, part := range content {
						if part, ok := part.(map[string]any); ok {
							if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
								texts = append(texts, text)
							}
						}
					}
				}
			}
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}
	return "", NewError("RESULT_REQUIRED")
}
