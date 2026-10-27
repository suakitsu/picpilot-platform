package workerprotocol

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// CanonicalHash recursively sorts object keys, preserves array order and exact
// JSON number values (no float64 round trip), and ignores insignificant spacing.
// Hashes are persistence-internal, not extra wire fields.
func CanonicalHash(data []byte) (string, error) {
	v, err := parseJSON(data)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err = e.Encode(v); err != nil {
		return "", NewError("REQUEST_SCHEMA_INVALID")
	}
	return SHA256(bytes.TrimSuffix(b.Bytes(), []byte{'\n'})), nil
}
func CompleteFingerprint(v CompleteRequest) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	metadata := v.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	result := v.Result
	if len(result) == 0 {
		result = json.RawMessage(`null`)
	}
	b, err := json.Marshal(map[string]any{"action": "complete", "artifact_id": v.ArtifactID, "result": result, "metadata": metadata})
	if err != nil {
		return "", NewError("REQUEST_SCHEMA_INVALID")
	}
	return CanonicalHash(b)
}
func FailFingerprint(v FailRequest) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	// Hash the validated original content, not its lossy display redaction.
	// Distinct sensitive messages must not collide into the same receipt. Only
	// the digest is retained; persistence uses SanitizeErrorMessage separately.
	b, _ := json.Marshal(map[string]any{"action": "fail", "error_code": NormalizeErrorCode(v.ErrorCode), "error_message": v.ErrorMessage})
	return CanonicalHash(b)
}
func cleanText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}
func NormalizeErrorCode(s string) string {
	s = regexp.MustCompile(`[^A-Z0-9_:-]`).ReplaceAllString(strings.ToUpper(cleanText(s, 80)), "_")
	if s == "" {
		return "WORKER_FAILED"
	}
	return s
}
func SanitizeErrorMessage(s string) string {
	// Paths can contain spaces, quotes and credentials. Removing a single word
	// can leave the rest of a path behind, so omit the entire detail in this case.
	if errorLocationPattern.MatchString(s) {
		return "Worker error detail redacted"
	}
	s = cleanText(s, 500)
	s = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._-]+`).ReplaceAllString(s, "Bearer ***")
	s = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9_-]{8,}`).ReplaceAllString(s, "sk-***")
	s = regexp.MustCompile(`(?i)\bppw_[A-Za-z0-9_-]{8,}`).ReplaceAllString(s, "ppw_***")
	s = regexp.MustCompile(`(?i)(["']?(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|client[_ -]?secret)["']?\s*[:=]\s*["']?)[^\s,"'}]+`).ReplaceAllString(s, "${1}***")
	return s
}

var errorLocationPattern = regexp.MustCompile(`(?i)(\b[a-z]:[\\/]|\\\\|\b(?:https?|file)://|(?:^|[\s"'(=:])(?:/|\.{1,2}/|~/))`)

// LeaseDigest matches the Java SHA256(signingSecret + "\n" + leaseId).
// The caller supplies an independent secret; this package never generates one.
func LeaseDigest(signingSecret []byte, leaseID string) (string, error) {
	if len(signingSecret) < 32 {
		return "", NewError("WORKER_INTERNAL_ERROR")
	}
	if err := ValidateLeaseID(leaseID); err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(signingSecret)
	h.Write([]byte{'\n'})
	h.Write([]byte(leaseID))
	return hex.EncodeToString(h.Sum(nil)), nil
}
func InputSignature(signingSecret []byte, jobID string, index int, workerID string, expires int64, sourceHTTPSURL string) (string, error) {
	if len(signingSecret) < 32 {
		return "", NewError("WORKER_INTERNAL_ERROR")
	}
	if ValidateWorkerID(workerID) != nil || index < 0 || strings.ContainsAny(jobID, "\r\n") || jobID == "" {
		return "", NewError("INPUT_SIGNATURE_INVALID")
	}
	if err := ValidatePublicHTTPSURL(sourceHTTPSURL); err != nil {
		return "", err
	}
	value := jobID + "\n" + strconv.Itoa(index) + "\n" + workerID + "\n" + strconv.FormatInt(expires, 10) + "\n" + sourceHTTPSURL
	h := hmac.New(sha256.New, signingSecret)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil)), nil
}
