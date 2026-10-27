package workerprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Fixed labels only. These are never issued, persisted by a server, or sent.
const testLease = "awl_test_fixture_v1_not_a_production_lease"
const testWorker = "fixture-worker-01"
const testTokenID = "awt_test_fixture_identity_only"

var testKey = []byte("test-only-signing-label-not-production-0001")
var testNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var e *ProtocolError
	if !errors.As(err, &e) || e.Detail.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func queued(cap Capability) Job {
	j := Job{JobID: "awj_test_fixture", ProviderID: "local-cliproxy-text", Status: Queued, Capability: cap, Protocol: ChatCompletions, Model: "gpt-5.5", Request: json.RawMessage(`{"model":"gpt-5.5","messages":[{"role":"user","content":"fixture"}],"stream":false}`), InputFiles: []InputFile{}, OfflineAction: Queue, QueuedAt: testNow.Add(-time.Hour)}
	if cap == Image {
		j.ProviderID = "local-cliproxy-image"
		j.Protocol = ImageGenerations
		j.Model = ImageModel
		j.Request = json.RawMessage(`{"model":"gpt-image-2","prompt":"fixture","n":1}`)
	}
	return j
}
func leased(t *testing.T, cap Capability) Job {
	t.Helper()
	j, task, err := Acquire(queued(cap), LeaseRequest{testWorker, testLease}, testKey, testNow, DefaultLeasePolicy())
	requireCode(t, err, "")
	requireCode(t, task.Validate(), "")
	return j
}
func completion() CompleteRequest {
	return CompleteRequest{WorkerID: testWorker, LeaseID: testLease, IdempotencyKey: "test-complete-0001", Result: json.RawMessage(`{"text":"fixture result"}`), Metadata: json.RawMessage(`{"model":"gpt-5.5"}`)}
}
func failure() FailRequest {
	return FailRequest{WorkerID: testWorker, LeaseID: testLease, IdempotencyKey: "test-fail-0001", ErrorCode: "UPSTREAM_TIMEOUT", ErrorMessage: "Local model request timed out"}
}

func TestAuthoritySchemaSnapshot(t *testing.T) {
	b, err := os.ReadFile("../../docs/local-worker-protocol-v1.md")
	if os.IsNotExist(err) {
		t.Skip("standalone core snapshot has no migration docs")
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := SHA256(b); got != "4c4bc9f674452dcee04e62d5f7a0a30389d51ef3125d79e1e0ac04514edca97b" {
		t.Fatalf("authority copy changed: %s", got)
	}
	blocks := regexp.MustCompile("(?s)```json\\r?\\n(.*?)\\r?\\n```").FindAllSubmatch(b, -1)
	for _, block := range blocks {
		if bytes.Contains(block[1], []byte(`"$schema"`)) {
			a, e := parseJSON(block[1])
			if e != nil {
				t.Fatal(e)
			}
			b, e := parseJSON(JSONSchema())
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatal("embedded schema differs from authority")
			}
			return
		}
	}
	t.Fatal("authority schema missing")
}

func TestWireFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/wire-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		Schema string
		Body   json.RawMessage
	}
	var fixtures struct{ Valid, Invalid []fixture }
	if err := json.Unmarshal(b, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures.Valid {
		requireCode(t, ValidateWire(f.Schema, f.Body), "")
	}
	for _, f := range fixtures.Invalid {
		requireCode(t, ValidateWire(f.Schema, f.Body), "REQUEST_SCHEMA_INVALID")
	}
}
func TestOriginalFixtureAndEmptyEnvelope(t *testing.T) {
	b, err := os.ReadFile("testdata/original-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		WorkerToken string
		Claim       json.RawMessage
		EmptyClaim  json.RawMessage
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.WorkerToken, "ppw_test_") || !strings.Contains(f.WorkerToken, "not_a_production_token") {
		t.Fatal("fixture must use test-only token label")
	}
	request, err := Decode[ClaimRequest](f.Claim)
	requireCode(t, err, "")
	if request.WaitSeconds != 25 || request.MaxTasks != 1 {
		t.Fatal(request)
	}
	empty, err := Decode[ClaimResponse](f.EmptyClaim)
	requireCode(t, err, "")
	if !empty.Empty || empty.Task != nil || empty.WaitedMS != 25000 {
		t.Fatal(empty)
	}
	b = encode(t, empty)
	if !bytes.Contains(b, []byte(`"task":null`)) {
		t.Fatal("task:null was omitted")
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	if len(m) != 4 {
		t.Fatal("claim envelope must have exactly four keys")
	}
	_, err = NewClaimResponse(nil, testNow, 0)
	requireCode(t, err, "")
}
func TestStrictClaimDecoding(t *testing.T) {
	valid := `{"workerId":"fixture-worker-01","capabilities":["image","text"],"maxTasks":1,"waitSeconds":25}`
	cases := []struct{ name, body string }{
		{"unknown", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":1,"localApiKey":"test-only"`, 1)},
		{"legacy", strings.ReplaceAll(valid, "workerId", "worker_id")},
		{"case", strings.ReplaceAll(valid, "workerId", "WorkerId")},
		{"duplicate-key", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":1,"maxTasks":1`, 1)},
		{"duplicate-capability", strings.Replace(valid, `"image","text"`, `"image","image"`, 1)},
		{"missing", strings.Replace(valid, `,"waitSeconds":25`, "", 1)},
		{"null", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":null`, 1)},
		{"string-number", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":"1"`, 1)},
		{"fraction", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":1.2`, 1)},
		{"multiple", valid + valid}, {"array", "[]"}, {"null-object", "null"},
		{"wait-low", strings.Replace(valid, "25", "19", 1)}, {"wait-high", strings.Replace(valid, "25", "31", 1)},
		{"batch", strings.Replace(valid, `"maxTasks":1`, `"maxTasks":2`, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode[ClaimRequest]([]byte(c.body))
			requireCode(t, err, "REQUEST_SCHEMA_INVALID")
		})
	}
	for _, seconds := range []int{20, 30} {
		v := ClaimRequest{testWorker, []Capability{Text}, 1, seconds}
		_, err := Decode[ClaimRequest](encode(t, v))
		requireCode(t, err, "")
	}
	_, err := Decode[*ClaimRequest]([]byte(valid))
	requireCode(t, err, "")
	_, err = Decode[ClaimRequest]([]byte(strings.Replace(valid, `"maxTasks":1`, `"maxTasks":1e999999999`, 1)))
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
}
func TestWireDTOs(t *testing.T) {
	j := leased(t, Text)
	task := ClaimedTask{JobID: j.JobID, LeaseID: testLease, LeaseUntil: j.LeaseUntil, RenewAfter: j.RenewAfter, GraceUntil: j.GraceUntil, Capability: j.Capability, Protocol: j.Protocol, Model: j.Model, Request: j.Request, InputFiles: j.InputFiles, Attempt: j.Attempt}
	values := []Message{
		HeartbeatRequest{testWorker, []Capability{Text}, 32}, HeartbeatResponse{testWorker, testNow, 0, 20}, ClaimRequest{testWorker, []Capability{Text}, 1, 20}, task, ClaimResponse{&task, false, testNow, 0}, LeaseRequest{testWorker, testLease}, LeaseResponse{j.JobID, j.LeaseUntil, j.RenewAfter, j.GraceUntil}, completion(), failure(), TerminalResponse{JobID: j.JobID, Status: Succeeded}, NewError("LEASE_EXPIRED").Response(),
		ResponsesPayload{Model: "gpt-5.5", Input: json.RawMessage(`"fixture"`)}, ChatCompletionsPayload{Model: "gpt-5.5", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"fixture"}`)}}, ImageGenerationPayload{Model: ImageModel, Prompt: "fixture", N: 1}, ImageEditPayload{ImageGenerationPayload: ImageGenerationPayload{Model: ImageModel, Prompt: "fixture", N: 1}},
	}
	for _, v := range values {
		t.Run(v.SchemaName(), func(t *testing.T) {
			requireCode(t, v.Validate(), "")
			requireCode(t, ValidateWire(v.SchemaName(), encode(t, v)), "")
		})
	}
	b := encode(t, task)
	for _, forbidden := range []string{"businessTaskId", "leaseToken", "lease_token", "job_id"} {
		if bytes.Contains(b, []byte(forbidden)) {
			t.Fatal("internal/legacy field leaked")
		}
	}
	b = bytes.Replace(b, []byte(`"jobId":`), []byte(`"businessTaskId":"forbidden","jobId":`), 1)
	_, err := Decode[ClaimedTask](b)
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
	task.RenewAfter = task.GraceUntil
	requireCode(t, task.Validate(), "REQUEST_SCHEMA_INVALID")
	_, err = Decode[ClaimResponse]([]byte(`{"task":null,"empty":false,"serverTime":"2026-07-31T12:00:00Z","waitedMs":0}`))
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
	_, err = Decode[ClaimResponse]([]byte(`{"empty":true,"serverTime":"2026-07-31T12:00:00Z","waitedMs":0}`))
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
}
func TestPayloadWhitelist(t *testing.T) {
	for _, model := range TextModels() {
		requireCode(t, ValidatePayload(Text, Responses, model, encode(t, map[string]any{"model": model, "input": "fixture", "stream": false})), "")
	}
	cases := []struct {
		name              string
		cap               Capability
		protocol          Protocol
		model, body, code string
	}{
		{"model", Text, Responses, "not-a-v1-model", `{}`, "MODEL_NOT_ALLOWED"},
		{"pair", Image, Responses, ImageModel, `{}`, "PROTOCOL_CAPABILITY_MISMATCH"},
		{"unknown", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"fixture","stream":false,"extra":1}`, "PAYLOAD_FIELD_NOT_ALLOWED"},
		{"mismatch", Text, Responses, "gpt-5.5", `{"model":"gpt-5.6-sol","input":"fixture","stream":false}`, "PAYLOAD_MODEL_MISMATCH"},
		{"stream", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"fixture","stream":true}`, "STREAM_NOT_ALLOWED"},
		{"missing-stream", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"fixture"}`, "PAYLOAD_INVALID"},
		{"nested-secret", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"fixture","stream":false,"metadata":{"nested":{"apiKey":"test-only"}}}`, "PAYLOAD_SECRET_NOT_ALLOWED"},
		{"base64", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"data:image/png;base64,AAAA","stream":false}`, "BASE64_REQUEST_NOT_ALLOWED"},
		{"local-url", Text, Responses, "gpt-5.5", `{"model":"gpt-5.5","input":"https://127.0.0.1/private","stream":false}`, "PAYLOAD_SECRET_NOT_ALLOWED"},
		{"empty-chat", Text, ChatCompletions, "gpt-5.5", `{"model":"gpt-5.5","messages":[],"stream":false}`, "PAYLOAD_INVALID"},
		{"image-count", Image, ImageGenerations, ImageModel, `{"model":"gpt-image-2","prompt":"fixture","n":2}`, "PAYLOAD_INVALID"},
		{"image-fraction", Image, ImageGenerations, ImageModel, `{"model":"gpt-image-2","prompt":"fixture","n":1.1}`, "PAYLOAD_INVALID"},
		{"image-empty", Image, ImageGenerations, ImageModel, `{"model":"gpt-image-2","prompt":" ","n":1}`, "PAYLOAD_INVALID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireCode(t, ValidatePayload(c.cap, c.protocol, c.model, []byte(c.body)), c.code)
		})
	}
	for _, size := range []int{32000, 32001} {
		code := ""
		if size > 32000 {
			code = "PAYLOAD_INVALID"
		}
		prompt := strings.Repeat("ab ", size/3) + strings.Repeat("x", size%3)
		requireCode(t, ValidatePayload(Image, ImageGenerations, ImageModel, encode(t, map[string]any{"model": ImageModel, "prompt": prompt, "n": 1})), code)
	}
	large := encode(t, map[string]any{"model": "gpt-5.5", "input": strings.Repeat("x ", MaxRequestBytes), "stream": false})
	requireCode(t, ValidatePayload(Text, Responses, "gpt-5.5", large), "WORKER_REQUEST_TOO_LARGE")
	deep := any(map[string]any{"refresh_token": "test-only"})
	for i := 0; i < 25; i++ {
		deep = map[string]any{"nested": deep}
	}
	requireCode(t, ValidatePayload(Text, Responses, "gpt-5.5", encode(t, map[string]any{"model": "gpt-5.5", "input": "fixture", "stream": false, "metadata": deep})), "PAYLOAD_SECRET_NOT_ALLOWED")
}
func TestResultAndMetadataValidation(t *testing.T) {
	for _, body := range []string{`{"text":"fixture"}`, `{"output_text":"fixture"}`, `{"choices":[{"message":{"content":"fixture"}}]}`, `{"output":[{"content":[{"text":"fixture"}]}]}`} {
		got, err := ExtractText(json.RawMessage(body))
		requireCode(t, err, "")
		if got != "fixture" {
			t.Fatal(got)
		}
	}
	cases := []struct{ field, body, code string }{{"result", `null`, "RESULT_REQUIRED"}, {"metadata", `{"preview":"data:image/png;base64,AAAA"}`, "BASE64_RESULT_NOT_ALLOWED"}, {"result", `{"text":"fixture","api_key":"test-only"}`, "RESULT_SECRET_NOT_ALLOWED"}, {"metadata", `{"local":{"baseUrl":"https://example.com"}}`, "RESULT_SECRET_NOT_ALLOWED"}, {"metadata", `[]`, "REQUEST_SCHEMA_INVALID"}, {"result", `{"text":"a","text":"b"}`, "REQUEST_SCHEMA_INVALID"}}
	for _, c := range cases {
		t.Run(c.field+c.code, func(t *testing.T) {
			v := completion()
			if c.field == "result" {
				v.Result = json.RawMessage(c.body)
			} else {
				v.Metadata = json.RawMessage(c.body)
			}
			requireCode(t, v.Validate(), c.code)
		})
	}
	v := completion()
	v.ArtifactID = "awa_test"
	requireCode(t, v.Validate(), "REQUEST_SCHEMA_INVALID")
	v = completion()
	v.Result = nil
	requireCode(t, v.Validate(), "REQUEST_SCHEMA_INVALID")
	v = completion()
	v.Result = encode(t, map[string]any{"text": strings.Repeat("a ", MaxResultBytes)})
	requireCode(t, v.Validate(), "RESULT_TOO_LARGE")
}
func TestErrorTaxonomyAndSanitization(t *testing.T) {
	for code, spec := range errorSpecs {
		e := NewError(code)
		if e.HTTPStatus != spec.status || e.Detail.Code != code {
			t.Fatal(code)
		}
		requireCode(t, e.Response().Validate(), "")
	}
	for _, code := range []string{"NETWORK_ERROR", "NETWORK_RESET", "UPSTREAM_TIMEOUT", "UPSTREAM_RATE_LIMITED", "UPSTREAM_UNAVAILABLE", "UPSTREAM_502", "WORKER_RUNTIME_ERROR", "OUTPUT_DIMENSION_MISMATCH", "OUTPUT_ASPECT_RATIO_MISMATCH"} {
		if !Retryable(0, code) {
			t.Fatal(code)
		}
	}
	for _, code := range []string{"CONTENT_POLICY_REJECTED", "MODEL_NOT_ALLOWED", "UPSTREAM_AUTH_FAILED", "LEASE_EXPIRED", "PAYLOAD_INVALID"} {
		if Retryable(0, code) {
			t.Fatal(code)
		}
	}
	for _, status := range []int{408, 429, 500, 599} {
		if !Retryable(status, "OTHER") {
			t.Fatal(status)
		}
	}
	for _, code := range []string{"JOB_CANCELLED", "LEASE_EXPIRED", "WORKER_MISMATCH", "LEASE_ID_INVALID", "JOB_TERMINAL_CONFLICT"} {
		if ShouldRetryTransport(ErrorResponse{ErrorDetail{Code: code, Retryable: true}}) {
			t.Fatal(code)
		}
	}
	s := SanitizeErrorMessage(`Bearer ppw_test_fixture_only api_key:"sk-test_fixture_only" refresh_token:test_only client_secret:test_only`)
	if strings.Contains(s, "test_fixture_only") || strings.Contains(s, "test_only") || !strings.Contains(s, "***") {
		t.Fatal("redaction failed")
	}
	if got := NormalizeErrorCode(" upstream timeout "); got != "UPSTREAM_TIMEOUT" {
		t.Fatal(got)
	}
	if len([]rune(SanitizeErrorMessage(strings.Repeat("x ", 1000)))) > 500 {
		t.Fatal("error length")
	}
}

func TestFailureLocationRedactionAndExactReceiptContent(t *testing.T) {
	for _, message := range []string{
		`Cannot open C:\private\fixture.txt`,
		`Cannot open "C:/Users/Test User/private fixture.txt"`,
		`Cannot read \\test-host\private share\fixture.txt`,
		`Cannot open /home/test-user/private fixture.txt`,
		`Cannot open ~/private fixture.txt`,
		`Cannot open ../private/fixture.txt`,
		`Request failed https://example.invalid/private?signature=test-only-label`,
		`Request failed file:///C:/private/fixture.txt`,
	} {
		if got := SanitizeErrorMessage(message); got != "Worker error detail redacted" {
			t.Fatal("location was not fully redacted")
		}
	}
	if got := SanitizeErrorMessage("The upstream request timed out"); got != "The upstream request timed out" {
		t.Fatal("safe diagnostic changed")
	}
	a, b := failure(), failure()
	a.ErrorMessage = `Cannot open C:\private\one.txt`
	b.ErrorMessage = `Cannot open C:\private\two.txt`
	ha, err := FailFingerprint(a)
	requireCode(t, err, "")
	hb, err := FailFingerprint(b)
	requireCode(t, err, "")
	if ha == hb {
		t.Fatal("redacted but different failure content reused a receipt")
	}
	j := leased(t, Text)
	done, _, err := Fail(j, a, testKey, testNow)
	requireCode(t, err, "")
	_, _, err = Fail(done, b, testKey, testNow)
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
}

func TestCanonicalFingerprint(t *testing.T) {
	a, err := CanonicalHash([]byte(`{"z":[1,2],"a":{"x":9007199254740993,"y":"<tag>"}}`))
	requireCode(t, err, "")
	b, err := CanonicalHash([]byte(`{ "a": {"y":"<tag>","x":9007199254740993},"z":[1,2]}`))
	requireCode(t, err, "")
	if a != b {
		t.Fatal("object order/spacing changed hash")
	}
	c, _ := CanonicalHash([]byte(`{"a":{"y":"<tag>","x":9007199254740992},"z":[1,2]}`))
	if a == c {
		t.Fatal("numeric precision lost")
	}
	c, _ = CanonicalHash([]byte(`{"a":{"y":"<tag>","x":9007199254740993},"z":[2,1]}`))
	if a == c {
		t.Fatal("array order lost")
	}
	v := completion()
	v.Metadata = nil
	a, err = CompleteFingerprint(v)
	requireCode(t, err, "")
	v.Metadata = json.RawMessage(`{}`)
	b, _ = CompleteFingerprint(v)
	if a != b {
		t.Fatal("omitted metadata must canonicalize as empty object")
	}
	_, err = CanonicalHash([]byte(`{"x":1,"x":2}`))
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
}

func FuzzStrictDecode(f *testing.F) {
	f.Add([]byte(`{"workerId":"fixture-worker-01","capabilities":["text"],"maxTasks":1,"waitSeconds":25}`))
	f.Add([]byte(`{"task":null}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Decode[ClaimRequest](b)
		if err == nil {
			requireCode(t, v.Validate(), "")
			_, err = Decode[ClaimRequest](encode(t, v))
			requireCode(t, err, "")
		}
		_, _ = Decode[CompleteRequest](b)
		_, _ = CanonicalHash(b)
	})
}
