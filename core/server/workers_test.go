package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/netip"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/store"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

const httpWorkerTestToken = "ppw_test_only-http-worker-label-0001"

type workerHTTPFixture struct {
	s     *Server
	user  store.User
	jwt   string
	scope string
}

func newWorkerHTTPFixture(t *testing.T, edition config.Edition) *workerHTTPFixture {
	t.Helper()
	s := testServer(t, edition)
	s.registerWorkerRoutes()
	s.Config.PublicBaseURL = "https://worker.example.invalid"
	jwt := initialize(t, s)
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	scope := "platform"
	if edition == config.Personal {
		scope = u.ID
	}
	hash, err := store.WorkerTokenDigest(s.Config.WorkerPepper, httpWorkerTestToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.Pool.Exec(context.Background(), `INSERT INTO worker_tokens(id,owner_scope,name,token_hash,token_prefix,created_by) VALUES('http-test-token',$1,'HTTP test',$2,'ppw_test',$3)`, scope, hash, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	f := &workerHTTPFixture{s: s, user: u, jwt: jwt, scope: scope}
	w := workerHTTPRequest(s, "POST", wp.BasePath+"/heartbeat", httpWorkerTestToken, []byte(`{"workerId":"http-test-worker","capabilities":["image","text"],"maxConcurrency":4}`))
	if w.Code != 200 {
		t.Fatal("fixture heartbeat failed", w.Code)
	}
	return f
}
func workerHTTPRequest(s *Server, method, path, token string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://worker.example.invalid"+path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func workerHTTPJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if json.Unmarshal(w.Body.Bytes(), &data) != nil {
		t.Fatal("response is not JSON", w.Code)
	}
	return data
}
func workerHTTPError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected HTTP %d, got %d", status, w.Code)
	}
	out, err := wp.Decode[wp.ErrorResponse](w.Body.Bytes())
	if err != nil || out.Error.Code != code {
		t.Fatalf("expected worker error %s", code)
	}
}
func (f *workerHTTPFixture) enqueue(t *testing.T, cap wp.Capability, proto wp.Protocol, inputs []string) store.WorkerJob {
	t.Helper()
	model := wp.ImageModel
	request := json.RawMessage(`{"model":"gpt-image-2","prompt":"fixture image","n":1}`)
	if cap == wp.Text {
		model = "gpt-5.5"
		request = json.RawMessage(`{"model":"gpt-5.5","input":"fixture text","stream":false}`)
	}
	id := "http-test-" + string(proto)
	p, err := f.s.Store.PutProvider(context.Background(), store.Provider{ID: id, OwnerScope: f.scope, Name: "HTTP fixture", Capability: string(cap), Protocol: string(proto), Profile: "openai", Model: model, Enabled: true, ExecutionMode: "pull_worker", MaxConcurrency: 4, TimeoutSeconds: 180, LocalWaitSeconds: 30, OfflineAction: "queue", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := f.s.Store.SubmitTask(context.Background(), store.SubmitTask{UserID: f.user.ID, Capability: string(cap), Kind: "worker-test", Title: "HTTP worker fixture", Request: json.RawMessage(`{"version":1,"test":true}`), IdempotencyKey: fmt.Sprintf("http-worker-%s-%d", proto, time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	job, created, err := f.s.Store.EnqueueWorkerJob(context.Background(), store.EnqueueWorkerJobInput{TaskID: task.ID, ProviderScope: f.scope, ProviderID: p.ID, Capability: cap, Protocol: proto, Model: model, Request: request, InputAssetIDs: inputs}, f.s.workerOptions())
	if err != nil || !created {
		t.Fatal("HTTP fixture enqueue failed", err)
	}
	return job
}
func (f *workerHTTPFixture) claim(t *testing.T) *wp.ClaimedTask {
	t.Helper()
	w := workerHTTPRequest(f.s, "POST", wp.BasePath+"/claim", httpWorkerTestToken, []byte(`{"workerId":"http-test-worker","capabilities":["image","text"],"maxTasks":1,"waitSeconds":20}`))
	if w.Code != 200 {
		t.Fatal("claim failed", w.Code)
	}
	out, err := wp.Decode[wp.ClaimResponse](w.Body.Bytes())
	if err != nil || out.Task == nil {
		t.Fatal("invalid claim envelope")
	}
	data := workerHTTPJSON(t, w)
	if len(data) != 4 || bytes.Contains(w.Body.Bytes(), []byte("businessTaskId")) || bytes.Contains(w.Body.Bytes(), []byte("token_hash")) {
		t.Fatal("claim leaked internal fields or used app envelope")
	}
	return out.Task
}

func TestPostgresWorkerHTTPTransportAuthStrictJSONAndRenew(t *testing.T) {
	f := newWorkerHTTPFixture(t, config.Platform)
	body := []byte(`{"workerId":"http-test-worker","capabilities":["text"],"maxConcurrency":1}`)
	r := httptest.NewRequest("POST", "http://worker.example.invalid"+wp.BasePath+"/heartbeat", bytes.NewReader(body))
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Authorization", "Bearer "+httpWorkerTestToken)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "198.51.100.5:1234"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	workerHTTPError(t, w, 426, "HTTPS_REQUIRED")
	f.s.Config.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	r = r.Clone(context.Background())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("explicit trusted HTTPS proxy rejected")
	}
	r = r.Clone(context.Background())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.Header.Add("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	workerHTTPError(t, w, 426, "HTTPS_REQUIRED")
	workerHTTPError(t, workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", "", body), 401, "WORKER_TOKEN_MISSING")
	workerHTTPError(t, workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", f.jwt, body), 401, "WORKER_TOKEN_INVALID")
	for _, raw := range []string{
		`{"WorkerId":"http-test-worker","capabilities":["text"],"maxConcurrency":1}`,
		`{"workerId":"http-test-worker","capabilities":["text"],"maxConcurrency":1,"unexpected":true}`,
		`{"workerId":"http-test-worker","workerId":"other-worker","capabilities":["text"],"maxConcurrency":1}`,
		`{"workerId":"http-test-worker","capabilities":["text"],"maxConcurrency":"1"}`,
	} {
		workerHTTPError(t, workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", httpWorkerTestToken, []byte(raw)), 400, "REQUEST_SCHEMA_INVALID")
	}
	f.enqueue(t, wp.Text, wp.Responses, nil)
	claim := f.claim(t)
	payload, _ := json.Marshal(wp.LeaseRequest{WorkerID: "http-test-worker", LeaseID: claim.LeaseID})
	w = workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/renew", httpWorkerTestToken, payload)
	if w.Code != 200 {
		t.Fatal("renew failed", w.Code)
	}
	renew, err := wp.Decode[wp.LeaseResponse](w.Body.Bytes())
	if err != nil || renew.JobID != claim.JobID || !renew.GraceUntil.After(renew.LeaseUntil) {
		t.Fatal("invalid renewal deadlines")
	}
}

func TestPostgresWorkerHTTPAdminTokenOnceAndPersonalIsolation(t *testing.T) {
	for _, edition := range []config.Edition{config.Platform, config.Personal} {
		t.Run(string(edition), func(t *testing.T) {
			f := newWorkerHTTPFixture(t, edition)
			root := "/api/admin/ai-config"
			if edition == config.Personal {
				root = "/api/me/ai-config"
			}
			w := workerHTTPRequest(f.s, "POST", root+"/worker-tokens", f.jwt, []byte(`{"name":"Test workstation"}`))
			if w.Code != 200 {
				t.Fatal("token create failed", w.Code)
			}
			envelope := workerHTTPJSON(t, w)
			data := envelope["data"].(map[string]any)
			token := data["token"].(string)
			id := data["worker_token"].(map[string]any)["id"].(string)
			if data["shown_once"] != true || !strings.HasPrefix(token, "ppw_") {
				t.Fatal("one-time issue missing")
			}
			w = workerHTTPRequest(f.s, "GET", root+"/workers", f.jwt, nil)
			if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(token)) || bytes.Contains(w.Body.Bytes(), []byte("token_hash")) {
				t.Fatal("status exposed credentials")
			}
			w = workerHTTPRequest(f.s, "POST", root+"/worker-tokens/"+id+"/regenerate", f.jwt, nil)
			if w.Code != 200 {
				t.Fatal("regenerate failed")
			}
			fresh := workerHTTPJSON(t, w)["data"].(map[string]any)["token"].(string)
			body := []byte(`{"workerId":"new-test-worker","capabilities":["text"],"maxConcurrency":1}`)
			workerHTTPError(t, workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", token, body), 401, "WORKER_TOKEN_INVALID")
			if workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", fresh, body).Code != 200 {
				t.Fatal("fresh token rejected")
			}
			w = workerHTTPRequest(f.s, "POST", root+"/worker-tokens/"+id+"/revoke", f.jwt, nil)
			if w.Code != 200 {
				t.Fatal("revoke failed")
			}
			workerHTTPError(t, workerHTTPRequest(f.s, "POST", wp.BasePath+"/heartbeat", fresh, body), 401, "WORKER_TOKEN_INVALID")
			if edition == config.Personal {
				if workerHTTPRequest(f.s, "GET", "/api/admin/ai-config/workers", f.jwt, nil).Code != 403 {
					t.Fatal("personal owner reached platform administration")
				}
			}
		})
	}
}

func workerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.SetNRGBA(1, 1, color.NRGBA{R: 25, G: 160, B: 210, A: 255})
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		t.Fatal("fixture PNG encoding failed")
	}
	return b.Bytes()
}
func workerMultipart(t *testing.T, form wp.ArtifactForm, data []byte, extra bool) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	for _, v := range []struct{ k, v string }{{"workerId", form.WorkerID}, {"leaseId", form.LeaseID}, {"idempotencyKey", form.IdempotencyKey}, {"sha256", form.SHA256}} {
		if m.WriteField(v.k, v.v) != nil {
			t.Fatal("multipart write failed")
		}
	}
	header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="fixture.png"`}, "Content-Type": {"image/png"}}
	p, err := m.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Write(data); err != nil {
		t.Fatal(err)
	}
	if extra {
		m.WriteField("leaseId", form.LeaseID)
	}
	m.Close()
	return b.Bytes(), m.FormDataContentType()
}
func workerUpload(s *Server, id string, body []byte, ct string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://worker.example.invalid"+wp.BasePath+"/jobs/"+id+"/artifacts", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+httpWorkerTestToken)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestPostgresWorkerHTTPArtifactsConcurrentReceiptsAndCompletion(t *testing.T) {
	f := newWorkerHTTPFixture(t, config.Platform)
	job := f.enqueue(t, wp.Image, wp.ImageGenerations, nil)
	claim := f.claim(t)
	source := workerPNG(t)
	form := wp.ArtifactForm{WorkerID: "http-test-worker", LeaseID: claim.LeaseID, IdempotencyKey: "artifact-http-test-0001", SHA256: wp.SHA256(source)}
	body, ct := workerMultipart(t, form, source, false)
	var wg sync.WaitGroup
	results := make(chan wp.ArtifactResponse, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := workerUpload(f.s, claim.JobID, body, ct)
			if w.Code != 200 {
				t.Error("concurrent upload failed", w.Code)
				return
			}
			out, err := wp.Decode[wp.ArtifactResponse](w.Body.Bytes())
			if err != nil {
				t.Error(err)
				return
			}
			results <- out
		}()
	}
	wg.Wait()
	close(results)
	first := ""
	firstCount := 0
	count := 0
	for out := range results {
		count++
		if first == "" {
			first = out.ArtifactID
		}
		if first != out.ArtifactID {
			t.Error("duplicate receipt IDs")
		}
		if !out.Idempotent {
			firstCount++
		}
		if out.SourceSHA256 != wp.SHA256(source) || out.Width != 3 || out.Height != 2 || out.ContentType != "image/png" {
			t.Error("artifact metadata mismatch")
		}
	}
	if count != 12 || firstCount != 1 {
		t.Fatal("artifact idempotency did not serialize")
	}
	var assets, receipts int
	ctx := context.Background()
	if err := f.s.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM assets WHERE task_id=$1),(SELECT count(*) FROM worker_artifacts WHERE job_id=$2)`, job.TaskID, claim.JobID).Scan(&assets, &receipts); err != nil || assets != 1 || receipts != 1 {
		t.Fatal("duplicate artifact assets were not cleaned up", assets, receipts)
	}
	a, err := f.s.Store.WorkerArtifactAsset(ctx, job.TaskID, first)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.s.Files.Read(a.StorageKey, wp.DefaultMaxArtifactBytes)
	if err != nil || wp.SHA256(stored) != a.SHA256 {
		t.Fatal("stored checksum mismatch")
	}
	badBody, badCT := workerMultipart(t, form, source, true)
	workerHTTPError(t, workerUpload(f.s, claim.JobID, badBody, badCT), 400, "REQUEST_SCHEMA_INVALID")
	workerHTTPError(t, workerUpload(f.s, claim.JobID, body, strings.Replace(ct, "multipart/form-data", "multipart/mixed", 1)), 400, "REQUEST_SCHEMA_INVALID")
	wrong := form
	wrong.SHA256 = strings.Repeat("0", 64)
	badBody, badCT = workerMultipart(t, wrong, source, false)
	workerHTTPError(t, workerUpload(f.s, claim.JobID, badBody, badCT), 400, "ARTIFACT_SHA256_MISMATCH")
	complete := wp.CompleteRequest{WorkerID: form.WorkerID, LeaseID: claim.LeaseID, IdempotencyKey: "complete-http-test-0001", ArtifactID: first}
	payload, _ := json.Marshal(complete)
	w := workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/complete", httpWorkerTestToken, payload)
	if w.Code != 200 {
		t.Fatal("image complete failed", w.Code)
	}
	w = workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/complete", httpWorkerTestToken, payload)
	out, err := wp.Decode[wp.TerminalResponse](w.Body.Bytes())
	if err != nil || !out.Idempotent {
		t.Fatal("complete retry not idempotent")
	}
	status, err := f.s.Store.Task(ctx, f.user.ID, job.TaskID)
	if err != nil || status.Status != "queued" {
		t.Fatal("worker completed business settlement")
	}
}

func TestPostgresWorkerHTTPSignedInputAndCancellation(t *testing.T) {
	f := newWorkerHTTPFixture(t, config.Platform)
	source := workerPNG(t)
	asset, err := f.s.saveImage(context.Background(), f.user.ID, "", "", "reference", "reference.png", bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	job := f.enqueue(t, wp.Image, wp.ImageEdits, []string{asset.ID})
	claim := f.claim(t)
	if len(claim.InputFiles) != 1 {
		t.Fatal("input missing")
	}
	u, _ := url.Parse(claim.InputFiles[0].URL)
	path := u.RequestURI()
	w := workerHTTPRequest(f.s, "GET", path, httpWorkerTestToken, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Location") != "" || wp.SHA256(w.Body.Bytes()) != asset.SHA256 {
		t.Fatal("input did not stream owned bytes")
	}
	workerHTTPError(t, workerHTTPRequest(f.s, "GET", path, "", nil), 401, "WORKER_TOKEN_MISSING")
	q := u.Query()
	q.Set("signature", strings.Repeat("0", 64))
	u.RawQuery = q.Encode()
	workerHTTPError(t, workerHTTPRequest(f.s, "GET", u.RequestURI(), httpWorkerTestToken, nil), 403, "INPUT_SIGNATURE_INVALID")
	if _, err = f.s.Store.CancelTask(context.Background(), f.user.ID, job.TaskID); err != nil {
		t.Fatal(err)
	}
	workerHTTPError(t, workerHTTPRequest(f.s, "GET", path, httpWorkerTestToken, nil), 410, "JOB_CANCELLED")
}

func TestPostgresWorkerHTTPFailureReceiptAndStrictRetryPolicy(t *testing.T) {
	f := newWorkerHTTPFixture(t, config.Platform)
	f.enqueue(t, wp.Text, wp.Responses, nil)
	claim := f.claim(t)
	in := wp.FailRequest{WorkerID: "http-test-worker", LeaseID: claim.LeaseID, IdempotencyKey: "failure-http-test-0001", ErrorCode: "NETWORK_ERROR", ErrorMessage: "Test network unavailable"}
	payload, _ := json.Marshal(in)
	path := wp.BasePath + "/jobs/" + claim.JobID + "/fail"
	for i := 0; i < 2; i++ {
		w := workerHTTPRequest(f.s, "POST", path, httpWorkerTestToken, payload)
		if w.Code != 200 {
			t.Fatal("failure callback failed", w.Code)
		}
		out, err := wp.Decode[wp.TerminalResponse](w.Body.Bytes())
		if err != nil || out.Status != wp.Failed || out.Retryable == nil || !*out.Retryable || out.Idempotent != (i > 0) {
			t.Fatal("failure response/replay classification invalid")
		}
	}
	var extra map[string]any
	json.Unmarshal(payload, &extra)
	extra["retryable"] = false
	bad, _ := json.Marshal(extra)
	workerHTTPError(t, workerHTTPRequest(f.s, "POST", path, httpWorkerTestToken, bad), 400, "REQUEST_SCHEMA_INVALID")
	in.ErrorCode = "CONTENT_POLICY_REJECTED"
	changed, _ := json.Marshal(in)
	workerHTTPError(t, workerHTTPRequest(f.s, "POST", path, httpWorkerTestToken, changed), 409, "IDEMPOTENCY_CONFLICT")
}

func TestPostgresWorkerHTTPEmptyClaimAndWaitCancellation(t *testing.T) {
	f := newWorkerHTTPFixture(t, config.Platform)
	payload := []byte(`{"workerId":"http-test-worker","capabilities":["text"],"maxTasks":1,"waitSeconds":20}`)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("POST", "https://worker.example.invalid"+wp.BasePath+"/claim", bytes.NewReader(payload)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+httpWorkerTestToken)
	r.Header.Set("Content-Type", "application/json")
	start := time.Now()
	f.s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	if time.Since(start) > time.Second {
		t.Fatal("cancelled long poll did not stop")
	}
	w := workerHTTPRequest(f.s, "POST", wp.BasePath+"/claim", httpWorkerTestToken, payload)
	if w.Code != 200 {
		t.Fatal("empty claim is not HTTP 200")
	}
	out, err := wp.Decode[wp.ClaimResponse](w.Body.Bytes())
	if err != nil || !out.Empty || out.Task != nil || out.WaitedMS < 19000 {
		t.Fatal("empty claim envelope or wait duration invalid")
	}
	if len(workerHTTPJSON(t, w)) != 4 {
		t.Fatal("empty claim changed envelope")
	}
}
