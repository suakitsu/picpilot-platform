package workerprotocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLeaseDeadlineBoundaries(t *testing.T) {
	j := leased(t, Text)
	identity := LeaseRequest{testWorker, testLease}
	if j.LeaseUntil != testNow.Add(120*time.Second) || j.RenewAfter != testNow.Add(80*time.Second) || j.GraceUntil != testNow.Add(135*time.Second) {
		t.Fatal("wrong deadlines")
	}
	for _, at := range []time.Time{testNow, j.LeaseUntil, j.GraceUntil.Add(-time.Nanosecond)} {
		t.Run(at.Format(time.RFC3339Nano), func(t *testing.T) {
			requireCode(t, VerifyLease(j, identity, testKey, at), "")
			_, _, err := Complete(j, completion(), testKey, at)
			requireCode(t, err, "")
			_, _, err = Fail(j, failure(), testKey, at)
			requireCode(t, err, "")
		})
	}
	for _, at := range []time.Time{j.GraceUntil, j.GraceUntil.Add(time.Nanosecond)} {
		requireCode(t, VerifyLease(j, identity, testKey, at), "LEASE_EXPIRED")
		before := cloneJob(j)
		after, _, err := Complete(j, completion(), testKey, at)
		requireCode(t, err, "LEASE_EXPIRED")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("late result changed state")
		}
		_, _, err = Fail(j, failure(), testKey, at)
		requireCode(t, err, "LEASE_EXPIRED")
		_, _, err = Renew(j, identity, testKey, at, DefaultLeasePolicy())
		requireCode(t, err, "LEASE_EXPIRED")
	}
	_, changed := Expire(j, j.GraceUntil.Add(-time.Nanosecond))
	if changed {
		t.Fatal("expired during grace")
	}
	expired, changed := Expire(j, j.GraceUntil)
	if !changed || expired.Status != Expired {
		t.Fatal("hard deadline must be exclusive")
	}
	legacy := j
	legacy.GraceUntil = time.Time{}
	requireCode(t, VerifyLease(legacy, identity, testKey, j.LeaseUntil), "LEASE_EXPIRED")
}
func TestRenewInGraceAndIdentity(t *testing.T) {
	j := leased(t, Text)
	at := j.LeaseUntil.Add(time.Second)
	next, response, err := Renew(j, LeaseRequest{testWorker, testLease}, testKey, at, DefaultLeasePolicy())
	requireCode(t, err, "")
	if next.LeaseUntil != at.Add(120*time.Second) || next.LeaseHash != j.LeaseHash || next.Attempt != j.Attempt {
		t.Fatal("renew changed identity/attempt")
	}
	requireCode(t, response.Validate(), "")
	if j.LeaseUntil == next.LeaseUntil {
		t.Fatal("input snapshot mutated")
	}
	requireCode(t, VerifyLease(j, LeaseRequest{"other-worker", testLease}, testKey, at), "WORKER_MISMATCH")
	requireCode(t, VerifyLease(j, LeaseRequest{testWorker, "awl_test_wrong_identity_0001"}, testKey, at), "LEASE_ID_INVALID")
	_, _, err = Renew(j, LeaseRequest{testWorker, testLease}, testKey, at, LeasePolicy{29, 15})
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
}
func TestRequeueRejectsOldAttempt(t *testing.T) {
	j := leased(t, Text)
	expired, _ := Expire(j, j.GraceUntil)
	q, changed := Requeue(expired, j.GraceUntil)
	if !changed || q.Status != Queued || q.WorkerID != "" || q.LeaseHash != "" || q.Attempt != 1 || q.QueuedAt != j.QueuedAt {
		t.Fatal("invalid requeue")
	}
	_, _, err := Acquire(q, LeaseRequest{testWorker, testLease}, testKey, j.GraceUntil, DefaultLeasePolicy())
	requireCode(t, err, "LEASE_ID_INVALID")
	next, task, err := Acquire(q, LeaseRequest{testWorker, "awl_test_new_attempt_0002"}, testKey, j.GraceUntil, DefaultLeasePolicy())
	requireCode(t, err, "")
	if next.Attempt != 2 || task.LeaseID == testLease || task.LeaseUntil == j.LeaseUntil {
		t.Fatal("new attempt not isolated")
	}
	_, _, err = Complete(next, completion(), testKey, j.GraceUntil.Add(time.Second))
	requireCode(t, err, "LEASE_ID_INVALID")
	expired.OfflineAction = Fallback
	if _, changed := Requeue(expired, j.GraceUntil); changed {
		t.Fatal("fallback jobs must not requeue")
	}
}
func TestTerminalReplayAndConflict(t *testing.T) {
	j := leased(t, Text)
	v := completion()
	done, response, err := Complete(j, v, testKey, testNow)
	requireCode(t, err, "")
	if response.Idempotent || done.Status != Succeeded || j.Status != Leased {
		t.Fatal("bad initial completion")
	}
	replay, receipt, err := Complete(done, v, testKey, j.GraceUntil.Add(time.Hour))
	requireCode(t, err, "")
	if !receipt.Idempotent || !reflect.DeepEqual(done, replay) {
		t.Fatal("receipt replay must not mutate terminal state")
	}
	for _, mutate := range []func(*CompleteRequest){func(v *CompleteRequest) { v.Result = json.RawMessage(`{"text":"changed"}`) }, func(v *CompleteRequest) { v.Metadata = json.RawMessage(`{"model":"gpt-5.6-sol"}`) }, func(v *CompleteRequest) { v.IdempotencyKey = "test-other-key-0001" }} {
		changed := v
		mutate(&changed)
		_, _, err = Complete(done, changed, testKey, testNow)
		requireCode(t, err, "IDEMPOTENCY_CONFLICT")
	}
	wrong := v
	wrong.LeaseID = "awl_test_wrong_lease_0001"
	_, _, err = Complete(done, wrong, testKey, testNow)
	requireCode(t, err, "LEASE_ID_INVALID")
	_, _, err = Fail(done, failure(), testKey, testNow)
	requireCode(t, err, "JOB_TERMINAL_CONFLICT")
	legacy := done
	legacy.CompletionKey = ""
	legacy.CompletionHash = ""
	_, _, err = Complete(legacy, v, testKey, testNow)
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
	v = fingerprintOrderRequest()
	first, _, err := Complete(j, v, testKey, testNow)
	requireCode(t, err, "")
	v.Result = json.RawMessage(`{"text":"fixture","extra":{"a":1,"b":2}}`)
	_, receipt, err = Complete(first, v, testKey, testNow)
	requireCode(t, err, "")
	if !receipt.Idempotent {
		t.Fatal("canonical replay")
	}
}
func fingerprintOrderRequest() CompleteRequest {
	v := completion()
	v.Result = json.RawMessage(`{"extra":{"b":2,"a":1},"text":"fixture"}`)
	return v
}
func TestFailureReplayAndRedaction(t *testing.T) {
	j := leased(t, Text)
	v := failure()
	v.ErrorMessage = `Bearer ppw_test_fixture_only api_key:"sk-test_fixture_only"`
	done, response, err := Fail(j, v, testKey, testNow)
	requireCode(t, err, "")
	if response.Retryable == nil || !*response.Retryable || strings.Contains(done.ErrorMessage, "test_fixture_only") {
		t.Fatal("retry/redaction")
	}
	_, receipt, err := Fail(done, v, testKey, j.GraceUntil.Add(time.Hour))
	requireCode(t, err, "")
	if !receipt.Idempotent {
		t.Fatal("not replayed")
	}
	v.ErrorMessage = "different safe failure"
	_, _, err = Fail(done, v, testKey, testNow)
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
	_, _, err = Complete(done, completion(), testKey, testNow)
	requireCode(t, err, "JOB_TERMINAL_CONFLICT")
	if !AllowsNextProvider(done) || AllowsNextProvider(j) {
		t.Fatal("fallback from wrong state")
	}
}
func TestCancellationAndOfflinePolicy(t *testing.T) {
	j := leased(t, Image)
	cancelled, changed := ReconcileBusinessTask(j, false, testNow)
	if !changed || cancelled.Status != Cancelled {
		t.Fatal("inactive business not cancelled")
	}
	v := completion()
	v.Result = nil
	v.ArtifactID = "awa_test_unused"
	_, _, err := Complete(cancelled, v, testKey, testNow)
	requireCode(t, err, "JOB_CANCELLED")
	_, _, err = Fail(cancelled, failure(), testKey, testNow)
	requireCode(t, err, "JOB_CANCELLED")
	_, _, err = Renew(cancelled, LeaseRequest{testWorker, testLease}, testKey, testNow, DefaultLeasePolicy())
	requireCode(t, err, "JOB_CANCELLED")
	form, source, artifact := artifactFixture()
	_, _, err = RecordArtifact(cancelled, form, source, artifact, testKey, testNow, 0)
	requireCode(t, err, "JOB_CANCELLED")
	requireCode(t, VerifyInputAccess(cancelled, testWorker, 0, testNow.Unix(), "", "https://assets.example.com/input.png", testKey, testNow), "JOB_CANCELLED")
	for _, status := range []Status{Succeeded, Failed, Cancelled, Expired} {
		copy := j
		copy.Status = status
		if _, changed := Cancel(copy, testNow); changed {
			t.Fatal("cancel overwrote terminal state")
		}
	}
	q := queued(Text)
	q.OfflineAction = Fallback
	if _, ok := OfflineFallback(q, q.QueuedAt.Add(24*time.Second), 25); ok {
		t.Fatal("premature fallback")
	}
	if next, ok := OfflineFallback(q, q.QueuedAt.Add(25*time.Second), 25); !ok || next.Status != Cancelled {
		t.Fatal("missing fallback")
	}
	j.OfflineAction = Fallback
	if _, ok := OfflineFallback(j, j.GraceUntil.Add(time.Hour), 1); ok {
		t.Fatal("claimed work cannot race HTTP fallback")
	}
	deadline, ok := ProcessingDeadline(j, 90)
	if !ok || deadline != j.LeasedAt.Add(90*time.Second) {
		t.Fatal("text processing timer includes queue wait")
	}
}

func artifactFixture() (ArtifactForm, []byte, ArtifactResponse) {
	source := []byte("test-only-uploaded-bytes")
	normalized := []byte("test-only-normalized-bytes")
	form := ArtifactForm{WorkerID: testWorker, LeaseID: testLease, IdempotencyKey: "test-artifact-0001", SHA256: SHA256(source)}
	response := ArtifactResponse{ArtifactID: "awa_test_fixture", ContentType: "image/png", FileSize: int64(len(normalized)), SHA256: SHA256(normalized), SourceSHA256: SHA256(source), Width: 2, Height: 2, Format: "png"}
	return form, source, response
}
func TestArtifactChecksumAndIdempotency(t *testing.T) {
	j := leased(t, Image)
	form, source, candidate := artifactFixture()
	next, response, err := RecordArtifact(j, form, source, candidate, testKey, j.LeaseUntil.Add(time.Second), 0)
	requireCode(t, err, "")
	if response.Idempotent || len(next.Artifacts) != 1 || len(j.Artifacts) != 0 || response.SHA256 == response.SourceSHA256 {
		t.Fatal("normalization/source metadata or input mutation")
	}
	_, receipt, err := RecordArtifact(next, form, source, ArtifactResponse{}, testKey, j.GraceUntil.Add(time.Hour), 0)
	requireCode(t, err, "")
	if !receipt.Idempotent || receipt.ArtifactID != response.ArtifactID {
		t.Fatal("duplicate upload not replayed")
	}
	_, _, err = RecordArtifact(next, form, []byte("changed-test-bytes"), candidate, testKey, testNow, 0)
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
	changed := form
	changed.SHA256 = SHA256([]byte("changed"))
	_, _, err = RecordArtifact(next, changed, []byte("changed"), candidate, testKey, testNow, 0)
	requireCode(t, err, "IDEMPOTENCY_CONFLICT")
	_, _, err = RecordArtifact(j, form, []byte("wrong"), candidate, testKey, testNow, 0)
	requireCode(t, err, "ARTIFACT_SHA256_MISMATCH")
	_, _, err = RecordArtifact(j, form, source, candidate, testKey, j.GraceUntil, 0)
	requireCode(t, err, "LEASE_EXPIRED")
	_, _, err = RecordArtifact(leased(t, Text), form, source, candidate, testKey, testNow, 0)
	requireCode(t, err, "ARTIFACT_NOT_ALLOWED")
	v := completion()
	v.ArtifactID = response.ArtifactID
	v.Result = nil
	done, _, err := Complete(next, v, testKey, testNow)
	requireCode(t, err, "")
	if done.Status != Succeeded {
		t.Fatal("image completion")
	}
	bad := cloneJob(next)
	bad.Artifacts[0].LeaseHash = strings.Repeat("0", 64)
	_, _, err = Complete(bad, v, testKey, testNow)
	requireCode(t, err, "ARTIFACT_REQUIRED")
	bad = cloneJob(next)
	bad.Artifacts[0].WorkerID = "other-worker"
	_, _, err = RecordArtifact(bad, form, source, candidate, testKey, testNow, 0)
	requireCode(t, err, "ARTIFACT_WORKER_MISMATCH")
	_, _, err = Complete(j, v, testKey, testNow)
	requireCode(t, err, "ARTIFACT_REQUIRED")
	q := queued(Image)
	q.Attempt = next.Attempt
	q.PreviousLeaseHash = next.LeaseHash
	q.Artifacts = next.Artifacts
	reclaimed, _, err := Acquire(q, LeaseRequest{testWorker, "awl_test_reclaimed_0002"}, testKey, testNow, DefaultLeasePolicy())
	requireCode(t, err, "")
	form.LeaseID = "awl_test_reclaimed_0002"
	_, _, err = RecordArtifact(reclaimed, form, source, candidate, testKey, testNow, 0)
	requireCode(t, err, "LEASE_ID_INVALID")
}
func TestArtifactValidation(t *testing.T) {
	form, source, response := artifactFixture()
	requireCode(t, response.Validate(), "")
	requireCode(t, VerifyArtifactSource(form.SHA256, source, 2), "ARTIFACT_INVALID")
	requireCode(t, VerifyArtifactSource(form.SHA256, nil, 0), "ARTIFACT_EMPTY")
	requireCode(t, VerifyArtifactSource("not-sha", source, 0), "ARTIFACT_SHA256_INVALID")
	requireCode(t, VerifyArtifactSource(strings.ToUpper(form.SHA256), source, 0), "")
	response.Width = 10000
	response.Height = 5001
	requireCode(t, response.Validate(), "ARTIFACT_INVALID")
	response.Height = 5000
	requireCode(t, response.Validate(), "")
	response.ContentType = "image/jpeg"
	requireCode(t, response.Validate(), "ARTIFACT_MIME_UNSUPPORTED")
	fields := map[string][]string{"workerId": {form.WorkerID}, "leaseId": {form.LeaseID}, "idempotencyKey": {form.IdempotencyKey}, "sha256": {form.SHA256}}
	_, err := ValidateMultipartFields(fields, []string{"file"})
	requireCode(t, err, "")
	fields["extra"] = []string{"test"}
	_, err = ValidateMultipartFields(fields, []string{"file"})
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
	delete(fields, "extra")
	fields["sha256"] = append(fields["sha256"], form.SHA256)
	_, err = ValidateMultipartFields(fields, []string{"file"})
	requireCode(t, err, "REQUEST_SCHEMA_INVALID")
}
func TestWorkerBindingAndCapacity(t *testing.T) {
	req := HeartbeatRequest{testWorker, []Capability{Text, Image}, 2}
	w, response, err := BindWorker(nil, testTokenID, req, testNow, 1, 60)
	requireCode(t, err, "")
	requireCode(t, response.Validate(), "")
	if response.HeartbeatIntervalSeconds != 20 {
		t.Fatal(response)
	}
	req.Capabilities[0] = Image
	if w.Capabilities[0] != Text {
		t.Fatal("binding aliased caller capabilities")
	}
	req.Capabilities = []Capability{Text}
	_, _, err = BindWorker(&w, "awt_test_other_identity", req, testNow, 1, 60)
	requireCode(t, err, "WORKER_ID_IN_USE")
	requireCode(t, VerifyWorkerOwnership(nil, testTokenID, testWorker), "WORKER_HEARTBEAT_REQUIRED")
	requireCode(t, VerifyWorkerOwnership(&w, "awt_test_other_identity", testWorker), "WORKER_TOKEN_MISMATCH")
	requireCode(t, VerifyWorkerOwnership(&w, testTokenID, testWorker), "")
	for _, c := range []Capacity{{0, 0, 1, 1}, {1, 2, 2, 3}} {
		if !c.Available() {
			t.Fatal(c)
		}
	}
	for _, c := range []Capacity{{1, 0, 1, 1}, {0, 1, 1, 1}, {0, 0, 0, 1}, {0, 0, 33, 1}, {-1, 0, 1, 1}} {
		if c.Available() {
			t.Fatal(c)
		}
	}
}
func inputFixture(t *testing.T) (InputFile, string) {
	t.Helper()
	source := "https://assets.example.com/fixture.png"
	expires := testNow.Add(5 * time.Minute).Unix()
	sig, err := InputSignature(testKey, "awj_test_fixture", 0, testWorker, expires, source)
	requireCode(t, err, "")
	u := "https://picpilot.example.com" + BasePath + "/inputs/awj_test_fixture/0?workerId=" + url.QueryEscape(testWorker) + "&expires=" + fmt.Sprint(expires) + "&signature=" + sig
	return InputFile{Index: 0, URL: u, Filename: "fixture.png", ContentType: "image/png", FileSize: 10, URLExpiresAt: time.Unix(expires, 0).UTC(), URLChecksum: sig, URLSignatureAlgorithm: "HMAC-SHA256"}, source
}
func TestSignedInputs(t *testing.T) {
	file, source := inputFixture(t)
	requireCode(t, file.Validate(), "")
	requireCode(t, ValidateInputFiles(ImageEdits, "awj_test_fixture", []InputFile{file}), "")
	j := leased(t, Image)
	j.InputFiles = []InputFile{file}
	j.Protocol = ImageEdits
	requireCode(t, VerifyInputAccess(j, testWorker, 0, file.URLExpiresAt.Unix(), file.URLChecksum, source, testKey, testNow), "")
	requireCode(t, VerifyInputAccess(j, testWorker, 0, file.URLExpiresAt.Unix(), strings.Repeat("0", 64), source, testKey, testNow), "INPUT_SIGNATURE_INVALID")
	requireCode(t, VerifyInputAccess(j, testWorker, 1, file.URLExpiresAt.Unix(), file.URLChecksum, source, testKey, testNow), "INPUT_NOT_FOUND")
	requireCode(t, VerifyInputAccess(j, testWorker, 0, testNow.Unix()-1, file.URLChecksum, source, testKey, testNow), "INPUT_URL_EXPIRED")
	requireCode(t, VerifyInputAccess(j, testWorker, 0, file.URLExpiresAt.Unix(), file.URLChecksum, source, testKey, j.GraceUntil), "LEASE_EXPIRED")
	bad := file
	bad.URLChecksum = strings.Repeat("0", 64)
	requireCode(t, bad.Validate(), "INPUT_SIGNATURE_INVALID")
	bad = file
	bad.URL += "&signature=" + file.URLChecksum
	requireCode(t, bad.Validate(), "INPUT_SIGNATURE_INVALID")
	requireCode(t, ValidateInputFiles(ImageGenerations, j.JobID, []InputFile{file}), "INPUT_FILES_NOT_ALLOWED")
	requireCode(t, ValidateInputFiles(ChatCompletions, j.JobID, []InputFile{file}), "INPUT_FILES_NOT_ALLOWED")
	requireCode(t, ValidateInputFiles(ImageEdits, j.JobID, nil), "INPUT_FILES_REQUIRED")
}
func TestPublicURLValidation(t *testing.T) {
	for _, host := range []string{"localhost", "worker.local", "worker.internal", "10.0.0.1", "127.0.0.1", "100.64.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.2", "198.18.0.1", "224.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "[fd00::1]", "[fe80::1]", "2130706433", "127.1", "0x7f000001"} {
		t.Run(host, func(t *testing.T) {
			requireCode(t, ValidatePublicHTTPSURL("https://"+host+"/fixture"), "INPUT_URL_NOT_PUBLIC")
		})
	}
	requireCode(t, ValidatePublicHTTPSURL("https://assets.example.com/fixture.png"), "")
	requireCode(t, ValidatePublicHTTPSURL("http://assets.example.com/fixture.png"), "INPUT_HTTPS_REQUIRED")
	requireCode(t, ValidatePublicHTTPSURL("https://test-only:test-only@assets.example.com/fixture.png"), "INPUT_HTTPS_REQUIRED")
}
func TestTransitionsAreCopyOnWriteAndDeterministic(t *testing.T) {
	j := queued(Text)
	before := cloneJob(j)
	a, ta, err := Acquire(j, LeaseRequest{testWorker, testLease}, testKey, testNow, DefaultLeasePolicy())
	requireCode(t, err, "")
	b, tb, err := Acquire(j, LeaseRequest{testWorker, testLease}, testKey, testNow, DefaultLeasePolicy())
	requireCode(t, err, "")
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(ta, tb) || !reflect.DeepEqual(j, before) {
		t.Fatal("not deterministic/copy-on-write")
	}
	a.Request[0] = ' '
	if !bytes.Equal(j.Request, before.Request) || ta.Request[0] == ' ' {
		t.Fatal("request backing slice shared")
	}
	for i := 0; i < 12; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			_, _, err := Acquire(j, LeaseRequest{testWorker, testLease}, testKey, testNow, DefaultLeasePolicy())
			requireCode(t, err, "")
		})
	}
}
