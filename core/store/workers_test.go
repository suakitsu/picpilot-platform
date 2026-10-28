package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform/core/security"
	wp "github.com/suakitsu/picpilot-platform/core/workerprotocol"
)

const workerTestToken = "ppw_test_only-worker-label-0001"

var workerTestKey = bytes.Repeat([]byte{0x27}, 32)

type workerFixture struct {
	s         *Store
	user      User
	principal WorkerPrincipal
	options   WorkerOptions
	provider  Provider
}

func newWorkerFixture(t *testing.T, cap wp.Capability, proto wp.Protocol, max int) *workerFixture {
	t.Helper()
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: config.Platform}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Initialize(ctx, "worker-fixture@example.invalid", "test-only-password-hash", "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	model := wp.ImageModel
	if cap == wp.Text {
		model = "gpt-5.5"
	}
	p, err := s.PutProvider(ctx, Provider{ID: "test-worker-provider", OwnerScope: "platform", Name: "Worker fixture", Capability: string(cap), Protocol: string(proto), Profile: "openai", Model: model, Enabled: true, ExecutionMode: "pull_worker", MaxConcurrency: max, TimeoutSeconds: 180, LocalWaitSeconds: 30, OfflineAction: "queue", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := WorkerTokenDigest(workerTestKey, workerTestToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO worker_tokens(id,owner_scope,name,token_hash,token_prefix,created_by) VALUES('test-token','platform','Test label',$1,'ppw_test', $2)`, hash, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := s.AuthenticateWorkerToken(ctx, workerTestToken, workerTestKey)
	if err != nil {
		t.Fatal(err)
	}
	f := &workerFixture{s: s, user: u, principal: principal, provider: p, options: WorkerOptions{PublicBaseURL: "https://worker.example.invalid", SigningKey: workerTestKey}}
	f.heartbeat(t, "test-worker-01", max)
	return f
}
func (f *workerFixture) heartbeat(t *testing.T, id string, max int) {
	t.Helper()
	_, err := f.s.WorkerHeartbeat(context.Background(), f.principal, wp.HeartbeatRequest{WorkerID: id, Capabilities: []wp.Capability{wp.Image, wp.Text}, MaxConcurrency: max})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *workerFixture) input(t *testing.T, label string) EnqueueWorkerJobInput {
	t.Helper()
	task, _, err := f.s.SubmitTask(context.Background(), SubmitTask{UserID: f.user.ID, Capability: f.provider.Capability, Kind: "worker-test", Title: "Worker test", Request: json.RawMessage(`{"version":1,"fixture":true}`), IdempotencyKey: "task-" + label})
	if err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"model":"gpt-image-2","prompt":"test image","n":1}`)
	if f.provider.Capability == "text" {
		request = json.RawMessage(`{"model":"gpt-5.5","input":"test text","stream":false}`)
	}
	return EnqueueWorkerJobInput{TaskID: task.ID, ProviderScope: "platform", ProviderID: f.provider.ID, Capability: wp.Capability(f.provider.Capability), Protocol: wp.Protocol(f.provider.Protocol), Model: f.provider.Model, Request: request}
}
func (f *workerFixture) enqueue(t *testing.T, label string) WorkerJob {
	t.Helper()
	job, created, err := f.s.EnqueueWorkerJob(context.Background(), f.input(t, label), f.options)
	if err != nil || !created {
		t.Fatalf("enqueue failed: %v", err)
	}
	return job
}
func (f *workerFixture) claim(t *testing.T, worker string) *wp.ClaimedTask {
	t.Helper()
	task, err := f.s.ClaimWorkerJob(context.Background(), f.principal, wp.ClaimRequest{WorkerID: worker, Capabilities: []wp.Capability{wp.Image, wp.Text}, MaxTasks: 1, WaitSeconds: 20}, f.options)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func workerCode(t *testing.T, err error, code string) {
	t.Helper()
	var p *wp.ProtocolError
	if !errors.As(err, &p) || p.Detail.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func workerExpired(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.Pool.Exec(context.Background(), `UPDATE worker_jobs SET renew_after=clock_timestamp()-interval '3 seconds',lease_until=clock_timestamp()-interval '2 seconds',grace_until=clock_timestamp()-interval '1 second' WHERE job_id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresWorkerTokenLifecycleAndBinding(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	ctx := context.Background()
	state, err := f.s.WorkerAdminState(ctx, "platform")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(state)
	if bytes.Contains(encoded, []byte(workerTestToken)) || bytes.Contains(encoded, []byte("token_hash")) {
		t.Fatal("raw token or hash exposed")
	}
	issued, err := f.s.CreateWorkerToken(ctx, "platform", f.user.ID, "Second fixture", workerTestKey)
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.s.AuthenticateWorkerToken(ctx, issued.Token, workerTestKey)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.WorkerHeartbeat(ctx, other, wp.HeartbeatRequest{WorkerID: "test-worker-01", Capabilities: []wp.Capability{wp.Text}, MaxConcurrency: 1})
	workerCode(t, err, "WORKER_ID_IN_USE")
	_, err = f.s.ClaimWorkerJob(ctx, other, wp.ClaimRequest{WorkerID: "test-worker-01", Capabilities: []wp.Capability{wp.Text}, MaxTasks: 1, WaitSeconds: 20}, f.options)
	workerCode(t, err, "WORKER_TOKEN_MISMATCH")
	rotated, err := f.s.RegenerateWorkerToken(ctx, "platform", f.principal.TokenID, workerTestKey)
	if err != nil || !rotated.ShownOnce {
		t.Fatal("regeneration failed")
	}
	_, err = f.s.AuthenticateWorkerToken(ctx, workerTestToken, workerTestKey)
	workerCode(t, err, "WORKER_TOKEN_INVALID")
	_, err = f.s.WorkerHeartbeat(ctx, f.principal, wp.HeartbeatRequest{WorkerID: "test-worker-01", Capabilities: []wp.Capability{wp.Text}, MaxConcurrency: 1})
	workerCode(t, err, "WORKER_TOKEN_INVALID")
	fresh, err := f.s.AuthenticateWorkerToken(ctx, rotated.Token, workerTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RevokeWorkerToken(ctx, "platform", fresh.TokenID); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.AuthenticateWorkerToken(ctx, rotated.Token, workerTestKey)
	workerCode(t, err, "WORKER_TOKEN_INVALID")
	var dump string
	if err = f.s.Pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t))::text FROM worker_tokens t`).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, issued.Token) || strings.Contains(dump, rotated.Token) || strings.Contains(dump, workerTestToken) {
		t.Fatal("database contains a complete token")
	}
}

func TestPostgresWorkerEnqueueConcurrentIdempotencyAndValidation(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	ctx := context.Background()
	in := f.input(t, "dedup")
	var wins atomic.Int32
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, created, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
			if err != nil {
				t.Error(err)
				return
			}
			if created {
				wins.Add(1)
			}
			ids <- job.Job.JobID
		}()
	}
	wg.Wait()
	close(ids)
	if wins.Load() != 1 {
		t.Fatalf("created %d jobs", wins.Load())
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate jobs")
		}
	}
	in.Request = json.RawMessage(`{"model":"gpt-5.5","input":"changed normalized request","stream":false}`)
	_, _, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
	if !errors.Is(err, ErrConflict) {
		t.Fatal("second unfinished job accepted for same task")
	}
	in.Request = json.RawMessage(`{"version":1,"text":{"input":"not a worker payload"}}`)
	_, _, err = f.s.EnqueueWorkerJob(ctx, in, f.options)
	workerCode(t, err, "PAYLOAD_FIELD_NOT_ALLOWED")
	in = f.input(t, "disabled")
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET enabled=false WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.s.EnqueueWorkerJob(ctx, in, f.options)
	if !errors.Is(err, ErrDisabled) {
		t.Fatal("disabled provider enqueued")
	}
}

func TestPostgresWorkerAtomicCapacityAndFIFO(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	queued := []WorkerJob{}
	for i := 0; i < 8; i++ {
		queued = append(queued, f.enqueue(t, fmt.Sprint(i)))
	}
	for i := 0; i < 4; i++ {
		f.heartbeat(t, fmt.Sprintf("concurrent-worker-%d", i), 1)
	}
	var wg sync.WaitGroup
	claims := make(chan *wp.ClaimedTask, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); claims <- f.claim(t, fmt.Sprintf("concurrent-worker-%d", i%4)) }(i)
	}
	wg.Wait()
	close(claims)
	seen := map[string]bool{}
	for task := range claims {
		if task != nil {
			if seen[task.JobID] {
				t.Error("job leased twice")
			}
			seen[task.JobID] = true
		}
	}
	if len(seen) != 2 || !seen[queued[0].Job.JobID] || !seen[queued[1].Job.JobID] {
		t.Fatal("provider capacity or FIFO violated")
	}
	var max int
	if err := f.s.Pool.QueryRow(context.Background(), `SELECT max(n) FROM(SELECT count(*) n FROM worker_jobs WHERE status='leased' GROUP BY worker_id) q`).Scan(&max); err != nil || max != 1 {
		t.Fatal("worker capacity violated")
	}
}

func TestPostgresWorkerTerminalReceiptsAndNoBusinessSettlement(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	job := f.enqueue(t, "terminal")
	claim := f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("no claim")
	}
	ctx := context.Background()
	in := wp.CompleteRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "complete-test-0001", Result: json.RawMessage(`{"text":"done"}`)}
	var first atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := f.s.CompleteWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
			if err != nil {
				t.Error(err)
				return
			}
			if !out.Idempotent {
				first.Add(1)
			}
		}()
	}
	wg.Wait()
	if first.Load() != 1 {
		t.Fatal("terminal result committed more than once")
	}
	workerExpired(t, f.s, claim.JobID)
	out, err := f.s.CompleteWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
	if err != nil || !out.Idempotent {
		t.Fatal("terminal replay rejected after deadline")
	}
	in.Result = json.RawMessage(`{"text":"changed"}`)
	_, err = f.s.CompleteWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
	workerCode(t, err, "IDEMPOTENCY_CONFLICT")
	_, err = f.s.FailWorkerJob(ctx, f.principal, claim.JobID, wp.FailRequest{WorkerID: in.WorkerID, LeaseID: in.LeaseID, IdempotencyKey: "failure-test-0001", ErrorCode: "NETWORK_ERROR", ErrorMessage: "network unavailable"}, f.options)
	workerCode(t, err, "JOB_TERMINAL_CONFLICT")
	var lease string
	if err = f.s.Pool.QueryRow(ctx, `SELECT lease_hash FROM worker_jobs WHERE job_id=$1`, claim.JobID).Scan(&lease); err != nil || lease == claim.LeaseID || len(lease) != 64 {
		t.Fatal("raw lease retained")
	}
	business, err := f.s.Task(ctx, f.user.ID, job.TaskID)
	if err != nil || business.Status != "queued" {
		t.Fatal("callback settled business task")
	}
	var credits int
	if err = f.s.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE biz_id=$1`, job.TaskID).Scan(&credits); err != nil || credits != 0 {
		t.Fatal("callback changed billing")
	}
}

func TestPostgresWorkerExpiryRequeueCancellationAndFallback(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	ctx := context.Background()
	job := f.enqueue(t, "expiry")
	old := f.claim(t, "test-worker-01")
	if old == nil {
		t.Fatal("no claim")
	}
	workerExpired(t, f.s, old.JobID)
	_, err := f.s.RenewWorkerJob(ctx, f.principal, old.JobID, wp.LeaseRequest{WorkerID: "test-worker-01", LeaseID: old.LeaseID}, f.options)
	workerCode(t, err, "LEASE_EXPIRED")
	n, err := f.s.RecoverWorkerJobs(ctx, "platform")
	if err != nil || n != 1 {
		t.Fatal("recovery failed")
	}
	fresh := f.claim(t, "test-worker-01")
	if fresh == nil || fresh.JobID != old.JobID || fresh.LeaseID == old.LeaseID || fresh.Attempt != 2 {
		t.Fatal("requeue reused lease or lost job")
	}
	_, err = f.s.CompleteWorkerJob(ctx, f.principal, old.JobID, wp.CompleteRequest{WorkerID: "test-worker-01", LeaseID: old.LeaseID, IdempotencyKey: "old-complete-0001", Result: json.RawMessage(`{"text":"stale"}`)}, f.options)
	workerCode(t, err, "LEASE_ID_INVALID")
	if _, err = f.s.CancelTask(ctx, f.user.ID, job.TaskID); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.RenewWorkerJob(ctx, f.principal, fresh.JobID, wp.LeaseRequest{WorkerID: "test-worker-01", LeaseID: fresh.LeaseID}, f.options)
	workerCode(t, err, "JOB_CANCELLED")
	w, err := f.s.WorkerJobForTask(ctx, job.TaskID, job.Job.JobID)
	if err != nil || w.Job.Status != wp.Cancelled {
		t.Fatal("cancellation not persisted")
	}
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET offline_action='fallback' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	fallback := f.enqueue(t, "fallback")
	if _, err = f.s.Pool.Exec(ctx, `UPDATE worker_jobs SET queued_at=clock_timestamp()-interval '60 seconds' WHERE job_id=$1`, fallback.Job.JobID); err != nil {
		t.Fatal(err)
	}
	allowed, err := f.s.FallbackWorkerJob(ctx, fallback.TaskID, fallback.Job.JobID, 30)
	if err != nil || !allowed {
		t.Fatal("unclaimed fallback not authorized")
	}
	if f.claim(t, "test-worker-01") != nil {
		t.Fatal("cancelled fallback claimed")
	}
	busy := f.enqueue(t, "busy-fallback")
	c := f.claim(t, "test-worker-01")
	if c == nil {
		t.Fatal("no busy claim")
	}
	allowed, err = f.s.FallbackWorkerJob(ctx, busy.TaskID, busy.Job.JobID, 1)
	if err != nil || allowed {
		t.Fatal("fallback overlapped a lease")
	}
}

func (f *workerFixture) asset(t *testing.T, taskID string) Asset {
	t.Helper()
	id := security.NewID("asset")
	a, err := f.s.SaveAsset(context.Background(), Asset{ID: id, UserID: f.user.ID, TaskID: taskID, ImageType: "reference", StorageKey: "fixture/" + id, OriginalFilename: "fixture.png", ContentType: "image/png", FileSize: 12, Width: 1, Height: 1, SHA256: wp.SHA256([]byte("test-source"))})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestPostgresWorkerSignedInputsAndDelayedTextMaterialization(t *testing.T) {
	t.Run("image edits", func(t *testing.T) {
		f := newWorkerFixture(t, wp.Image, wp.ImageEdits, 1)
		ctx := context.Background()
		in := f.input(t, "edit")
		asset := f.asset(t, "")
		in.InputAssetIDs = []string{asset.ID}
		job, _, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.s.Pool.Exec(ctx, `UPDATE worker_jobs SET queued_at=clock_timestamp()-interval '2 hours' WHERE job_id=$1`, job.Job.JobID); err != nil {
			t.Fatal(err)
		}
		claim := f.claim(t, "test-worker-01")
		if claim == nil || len(claim.InputFiles) != 1 || claim.Validate() != nil {
			t.Fatal("invalid edit claim")
		}
		input := claim.InputFiles[0]
		if time.Until(input.URLExpiresAt) < 4*time.Minute {
			t.Fatal("input URL signed before claim")
		}
		u, _ := url.Parse(input.URL)
		q := u.Query()
		expires, _ := strconv.ParseInt(q.Get("expires"), 10, 64)
		a, err := f.s.WorkerInputAsset(ctx, f.principal, claim.JobID, "test-worker-01", 0, expires, q.Get("signature"), f.options)
		if err != nil || a.ID != asset.ID {
			t.Fatal("signed input rejected")
		}
		_, err = f.s.WorkerInputAsset(ctx, f.principal, claim.JobID, "test-worker-01", 0, expires, strings.Repeat("0", 64), f.options)
		workerCode(t, err, "INPUT_SIGNATURE_INVALID")
		if _, err = f.s.CancelTask(ctx, f.user.ID, in.TaskID); err != nil {
			t.Fatal(err)
		}
		_, err = f.s.WorkerInputAsset(ctx, f.principal, claim.JobID, "test-worker-01", 0, expires, q.Get("signature"), f.options)
		workerCode(t, err, "JOB_CANCELLED")
	})
	t.Run("text URLs", func(t *testing.T) {
		f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
		ctx := context.Background()
		in := f.input(t, "text-image")
		asset := f.asset(t, "")
		in.Request = json.RawMessage(`{"model":"gpt-5.5","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://worker.example.invalid/old-link"}]}],"stream":false}`)
		in.TextInputs = []WorkerTextInput{{AssetID: asset.ID, JSONPointer: "/input/0/content/0/image_url"}}
		job, _, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(job.Job.Request), "old-link") || !strings.Contains(string(job.Job.Request), "picpilot-asset:") {
			t.Fatal("durable text request retained expiring URL")
		}
		if _, err = f.s.Pool.Exec(ctx, `UPDATE worker_jobs SET queued_at=clock_timestamp()-interval '2 hours' WHERE job_id=$1`, job.Job.JobID); err != nil {
			t.Fatal(err)
		}
		claim := f.claim(t, "test-worker-01")
		if claim == nil || claim.Validate() != nil || len(claim.InputFiles) != 0 || strings.Contains(string(claim.Request), "picpilot-asset:") || !strings.Contains(string(claim.Request), "signature=") {
			t.Fatal("text image not materialized or changed v1 inputFiles")
		}
		replayed, created, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
		if err != nil || created || replayed.Job.JobID != job.Job.JobID {
			t.Fatal("text input canonical idempotency failed")
		}
	})
}

func TestPostgresWorkerArtifactIdempotencyAndTerminalOwnership(t *testing.T) {
	f := newWorkerFixture(t, wp.Image, wp.ImageGenerations, 2)
	ctx := context.Background()
	job := f.enqueue(t, "artifact")
	claim := f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("no claim")
	}
	source := []byte("fixed synthetic decoded-by-parent test image")
	form := wp.ArtifactForm{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "artifact-test-0001", SHA256: wp.SHA256(source)}
	var calls atomic.Int32
	var created Asset
	create := func(w WorkerJob) (Asset, error) {
		calls.Add(1)
		id := security.NewID("asset")
		var err error
		created, err = f.s.SaveAsset(ctx, Asset{ID: id, UserID: w.UserID, TaskID: w.TaskID, ProjectID: w.ProjectID, ImageType: "worker-output", StorageKey: "fixture/" + id, OriginalFilename: "output.png", ContentType: "image/png", FileSize: 12, Width: 1, Height: 1, SHA256: wp.SHA256([]byte("normalized image"))})
		return created, err
	}
	out, err := f.s.SaveWorkerArtifact(ctx, f.principal, claim.JobID, form, source, f.options, create)
	if err != nil || out.Idempotent {
		t.Fatal("artifact failed", err)
	}
	workerExpired(t, f.s, claim.JobID)
	replay, err := f.s.SaveWorkerArtifact(ctx, f.principal, claim.JobID, form, source, f.options, create)
	if err != nil || !replay.Idempotent || replay.ArtifactID != out.ArtifactID || calls.Load() != 1 {
		t.Fatal("artifact replay created a new asset")
	}
	if removed, err := f.s.DiscardWorkerArtifactAsset(ctx, created.ID); err != nil || removed {
		t.Fatal("cleanup deleted committed artifact")
	}
	changed := []byte("changed")
	form.SHA256 = wp.SHA256(changed)
	_, err = f.s.SaveWorkerArtifact(ctx, f.principal, claim.JobID, form, changed, f.options, create)
	workerCode(t, err, "IDEMPOTENCY_CONFLICT")
	asset, err := f.s.WorkerArtifactAsset(ctx, job.TaskID, out.ArtifactID)
	if err != nil || asset.ID != created.ID {
		t.Fatal("runner artifact lookup failed")
	}
	_, err = f.s.WorkerArtifactAsset(ctx, "another-task", out.ArtifactID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("artifact task ownership missing")
	}
}

func TestWorkerTextPointerSafety(t *testing.T) {
	for _, tc := range []struct {
		raw, pointer string
		valid        bool
	}{
		{`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"picpilot-asset:asset_test","detail":"auto"}}]}]}`, "/messages/0/content/0/image_url/url", true},
		{`{"input":[{"content":[{"type":"input_image","image_url":"picpilot-asset:asset_test"}]}]}`, "/input/0/content/0/image_url", true},
		{`{"prompt":"picpilot-asset:asset_test"}`, "/prompt", false},
		{`{"input":[{"content":[{"type":"input_text","image_url":"not an image"}]}]}`, "/input/0/content/0/image_url", false},
	} {
		out, err := rewriteWorkerText(json.RawMessage(tc.raw), []WorkerInputSource{{AssetID: "asset_test", JSONPointer: tc.pointer}}, func(WorkerInputSource) string { return "https://worker.example.invalid/fresh" })
		if (err == nil) != tc.valid {
			t.Fatal("pointer validation mismatch")
		}
		if tc.valid && !strings.Contains(string(out), "/fresh") {
			t.Fatal("pointer not replaced")
		}
	}
}

func TestPostgresWorkerGraceAndLateFirstWrites(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	ctx := context.Background()
	f.enqueue(t, "grace")
	claim := f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("no claim")
	}
	_, err := f.s.Pool.Exec(ctx, `UPDATE worker_jobs SET renew_after=clock_timestamp()-interval '3 seconds',lease_until=clock_timestamp()-interval '1 second',grace_until=clock_timestamp()+interval '10 seconds' WHERE job_id=$1`, claim.JobID)
	if err != nil {
		t.Fatal(err)
	}
	renew, err := f.s.RenewWorkerJob(ctx, f.principal, claim.JobID, wp.LeaseRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID}, f.options)
	if err != nil || !renew.LeaseUntil.After(time.Now()) {
		t.Fatal("grace-period renewal rejected")
	}
	workerExpired(t, f.s, claim.JobID)
	_, err = f.s.CompleteWorkerJob(ctx, f.principal, claim.JobID, wp.CompleteRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "late-complete-0001", Result: json.RawMessage(`{"text":"too late"}`)}, f.options)
	workerCode(t, err, "LEASE_EXPIRED")
	_, err = f.s.FailWorkerJob(ctx, f.principal, claim.JobID, wp.FailRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "late-failure-0001", ErrorCode: "NETWORK_ERROR", ErrorMessage: "too late"}, f.options)
	workerCode(t, err, "LEASE_EXPIRED")
	w, err := f.s.WorkerJobForTask(ctx, f.input(t, "grace").TaskID, claim.JobID)
	if err != nil || w.Job.Status != wp.Expired {
		t.Fatal("hard deadline not persisted")
	}
}

func TestPostgresWorkerFailureReplayAndSanitizedStorage(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
	ctx := context.Background()
	job := f.enqueue(t, "failure")
	claim := f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("no claim")
	}
	in := wp.FailRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "failure-test-0001", ErrorCode: "NETWORK_ERROR", ErrorMessage: "Authorization: Bearer <fixture-redacted>\n<fixture-path>"}
	out, err := f.s.FailWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
	if err != nil || out.Retryable == nil || !*out.Retryable || out.Idempotent {
		t.Fatal("failure classification incorrect", err)
	}
	workerExpired(t, f.s, claim.JobID)
	out, err = f.s.FailWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
	if err != nil || !out.Idempotent {
		t.Fatal("failure replay rejected")
	}
	state, err := f.s.WorkerJobForTask(ctx, job.TaskID, claim.JobID)
	if err != nil || strings.Contains(state.Job.ErrorMessage, "sensitive-label") || strings.Contains(state.Job.ErrorMessage, "private") {
		t.Fatal("failure text leaked secrets or local paths")
	}
	in.ErrorCode = "BAD_REQUEST"
	_, err = f.s.FailWorkerJob(ctx, f.principal, claim.JobID, in, f.options)
	workerCode(t, err, "IDEMPOTENCY_CONFLICT")
}

func TestPostgresWorkerUploadRechecksRevocationAndDeadlineAfterStorage(t *testing.T) {
	for _, scenario := range []string{"revoked", "expired", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newWorkerFixture(t, wp.Image, wp.ImageGenerations, 1)
			ctx := context.Background()
			job := f.enqueue(t, scenario)
			claim := f.claim(t, "test-worker-01")
			if claim == nil {
				t.Fatal("no claim")
			}
			source := []byte("fixed-test-only upload")
			form := wp.ArtifactForm{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "upload-race-test-0001", SHA256: wp.SHA256(source)}
			var created Asset
			_, err := f.s.SaveWorkerArtifact(ctx, f.principal, claim.JobID, form, source, f.options, func(w WorkerJob) (Asset, error) {
				id := security.NewID("asset")
				var e error
				created, e = f.s.SaveAsset(ctx, Asset{ID: id, UserID: w.UserID, TaskID: w.TaskID, ImageType: "worker-output", StorageKey: "fixture/" + id, OriginalFilename: "race.png", ContentType: "image/png", FileSize: 12, Width: 1, Height: 1, SHA256: wp.SHA256(source)})
				if e != nil {
					return created, e
				}
				switch scenario {
				case "revoked":
					_, e = f.s.RevokeWorkerToken(ctx, "platform", f.principal.TokenID)
				case "expired":
					workerExpired(t, f.s, claim.JobID)
				case "cancelled":
					_, e = f.s.CancelTask(ctx, f.user.ID, job.TaskID)
				}
				return created, e
			})
			code := map[string]string{"revoked": "WORKER_TOKEN_INVALID", "expired": "LEASE_EXPIRED", "cancelled": "JOB_CANCELLED"}[scenario]
			workerCode(t, err, code)
			if removed, e := f.s.DiscardWorkerArtifactAsset(ctx, created.ID); e != nil || !removed {
				t.Fatal("unaccepted upload was not removable")
			}
			var receipts int
			if e := f.s.Pool.QueryRow(ctx, `SELECT count(*) FROM worker_artifacts WHERE job_id=$1`, claim.JobID).Scan(&receipts); e != nil || receipts != 0 {
				t.Fatal("stale upload committed a receipt")
			}
		})
	}
}

func TestPostgresWorkerRunnerReconcileAndRestart(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
	ctx := context.Background()
	job := f.enqueue(t, "runner-resume")
	if _, err := f.s.Pool.Exec(ctx, `UPDATE tasks SET status='waiting_worker' WHERE id=$1`, job.TaskID); err != nil {
		t.Fatal(err)
	}
	resumed, err := f.s.LatestWorkerJobForTask(ctx, job.TaskID)
	if err != nil || resumed.Job.JobID != job.Job.JobID {
		t.Fatal("runner could not find durable worker job")
	}
	state, mayNext, err := f.s.ReconcileWorkerJob(ctx, job.TaskID, job.Job.JobID, 30)
	if err != nil || mayNext || state.Job.Status != wp.Queued {
		t.Fatal("pending worker work authorized HTTP")
	}
	claim := f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("waiting_worker business state was not claimable")
	}
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET enabled=false WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	state, err = f.s.LatestWorkerJobForTask(ctx, job.TaskID)
	if err != nil || state.Job.Status != wp.Leased {
		t.Fatal("disabled provider hid outstanding lease")
	}
	state, mayNext, err = f.s.ReconcileWorkerJob(ctx, job.TaskID, claim.JobID, 1)
	if err != nil || mayNext || state.Job.Status != wp.Leased {
		t.Fatal("reconcile overlapped leased execution")
	}
	if _, err = f.s.CancelTask(ctx, f.user.ID, job.TaskID); err != nil {
		t.Fatal(err)
	}
	state, mayNext, err = f.s.ReconcileWorkerJob(ctx, job.TaskID, claim.JobID, 1)
	if err != nil || mayNext || state.Job.Status != wp.Cancelled {
		t.Fatal("waiting_worker cancellation not reconciled")
	}
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET enabled=true,offline_action='fallback' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	offline := f.enqueue(t, "runner-offline")
	if _, err = f.s.Pool.Exec(ctx, `UPDATE worker_jobs SET queued_at=clock_timestamp()-interval '90 seconds' WHERE job_id=$1`, offline.Job.JobID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		state, mayNext, err = f.s.ReconcileWorkerJob(ctx, offline.TaskID, offline.Job.JobID, 30)
		if err != nil || !mayNext || state.Job.Status != wp.Cancelled || state.Job.ErrorCode != "LOCAL_WORKER_OFFLINE" {
			t.Fatal("committed offline fallback did not survive restart")
		}
	}
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET offline_action='queue' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	queued := f.enqueue(t, "runner-expiry")
	claim = f.claim(t, "test-worker-01")
	if claim == nil {
		t.Fatal("queue claim missing")
	}
	workerExpired(t, f.s, claim.JobID)
	state, mayNext, err = f.s.ReconcileWorkerJob(ctx, queued.TaskID, claim.JobID, 30)
	if err != nil || mayNext || state.Job.Status != wp.Queued {
		t.Fatal("queue expiry did not requeue atomically")
	}
}

func TestPostgresWorkerImageProviderAcceptsGenerationAndOwnedEdits(t *testing.T) {
	f := newWorkerFixture(t, wp.Image, wp.ImageGenerations, 2)
	ctx := context.Background()
	in := f.input(t, "default-image-edit")
	in.Protocol = wp.ImageEdits
	_, _, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
	workerCode(t, err, "INPUT_FILES_REQUIRED")
	asset := f.asset(t, "")
	in.InputAssetIDs = []string{asset.ID}
	edit, created, err := f.s.EnqueueWorkerJob(ctx, in, f.options)
	if err != nil || !created {
		t.Fatal("default generation provider rejected owned edit", err)
	}
	generation := f.enqueue(t, "default-generation")
	first, second := f.claim(t, "test-worker-01"), f.claim(t, "test-worker-01")
	if first == nil || first.JobID != edit.Job.JobID || first.Protocol != wp.ImageEdits || len(first.InputFiles) != 1 || first.Validate() != nil {
		t.Fatal("default provider did not claim signed image edit")
	}
	if second == nil || second.JobID != generation.Job.JobID || second.Protocol != wp.ImageGenerations || len(second.InputFiles) != 0 || second.Validate() != nil {
		t.Fatal("same provider did not claim image generation")
	}
	text := newWorkerFixture(t, wp.Text, wp.Responses, 1)
	textIn := text.input(t, "text-exact-protocol")
	textIn.Protocol = wp.ChatCompletions
	textIn.Request = json.RawMessage(`{"model":"gpt-5.5","messages":[{"role":"user","content":"test"}],"stream":false}`)
	_, _, err = text.s.EnqueueWorkerJob(ctx, textIn, text.options)
	workerCode(t, err, "PROTOCOL_CAPABILITY_MISMATCH")
}

func TestPostgresWorkerProviderAllowlistAndHeartbeatCapabilities(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 2)
	ctx := context.Background()
	if _, err := f.s.Pool.Exec(ctx, `UPDATE providers SET settings='{"worker_ids":["allowed-worker"]}' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	f.enqueue(t, "allowlist")
	if f.claim(t, "test-worker-01") != nil {
		t.Fatal("provider allowlist was ignored")
	}
	_, err := f.s.WorkerHeartbeat(ctx, f.principal, wp.HeartbeatRequest{WorkerID: "allowed-worker", Capabilities: []wp.Capability{wp.Image}, MaxConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if f.claim(t, "allowed-worker") != nil {
		t.Fatal("claim capabilities bypassed heartbeat capabilities")
	}
	f.heartbeat(t, "allowed-worker", 2)
	if f.claim(t, "allowed-worker") == nil {
		t.Fatal("allowlisted capable worker could not claim")
	}
	state, err := f.s.WorkerAdminState(ctx, "platform")
	if err != nil {
		t.Fatal(err)
	}
	providers := state["providers"].(map[string]any)
	view := providers[f.provider.ID].(map[string]any)
	if view["online_workers"] != 1 {
		t.Fatal("status counted disallowed workers")
	}
	f.enqueue(t, "malformed-after-queue")
	if _, err = f.s.Pool.Exec(ctx, `UPDATE providers SET settings='{"worker_ids":"allowed-worker"}' WHERE id=$1`, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	if f.claim(t, "allowed-worker") != nil {
		t.Fatal("malformed allowlist became unrestricted")
	}
	_, _, err = f.s.EnqueueWorkerJob(ctx, f.input(t, "malformed-before-queue"), f.options)
	workerCode(t, err, "REQUEST_SCHEMA_INVALID")
}

func TestWorkerProviderAllowlistValidation(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
		count int
	}{
		{`{}`, true, 0}, {`{"worker_ids":[]}`, true, 0}, {`{"worker_ids":["worker-one","worker-two"]}`, true, 2},
		{`{"worker_ids":null}`, false, 0}, {`{"worker_ids":false}`, false, 0}, {`{"worker_ids":"worker-one"}`, false, 0}, {`{"worker_ids":["x"]}`, false, 0}, {`{"worker_ids":["worker-one","worker-one"]}`, false, 0},
	} {
		ids, err := workerAllowedIDs(json.RawMessage(tc.raw))
		if (err == nil) != tc.valid || tc.valid && len(ids) != tc.count {
			t.Fatal("allowlist validation mismatch")
		}
	}
}

func TestPostgresWorkerProviderChangesFenceOnlyPendingJobs(t *testing.T) {
	for _, change := range []string{"disabled", "model", "protocol", "removed"} {
		t.Run(change, func(t *testing.T) {
			for _, leased := range []bool{false, true} {
				t.Run(fmt.Sprint(leased), func(t *testing.T) {
					f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
					ctx := context.Background()
					if _, err := f.s.Pool.Exec(ctx, `UPDATE providers SET offline_action='fallback' WHERE id=$1`, f.provider.ID); err != nil {
						t.Fatal(err)
					}
					job := f.enqueue(t, "configuration-change")
					var claim *wp.ClaimedTask
					if leased {
						claim = f.claim(t, "test-worker-01")
						if claim == nil {
							t.Fatal("no claim")
						}
					}
					var err error
					switch change {
					case "disabled":
						_, err = f.s.Pool.Exec(ctx, `UPDATE providers SET enabled=false WHERE id=$1`, f.provider.ID)
					case "model":
						_, err = f.s.Pool.Exec(ctx, `UPDATE providers SET model='gpt-5.6-sol' WHERE id=$1`, f.provider.ID)
					case "protocol":
						_, err = f.s.Pool.Exec(ctx, `UPDATE providers SET protocol='openai_chat_completions' WHERE id=$1`, f.provider.ID)
					case "removed":
						err = f.s.DeleteProvider(ctx, "platform", f.provider.ID)
					}
					if err != nil {
						t.Fatal(err)
					}
					state, mayNext, err := f.s.ReconcileWorkerJob(ctx, job.TaskID, job.Job.JobID, 30)
					if err != nil {
						t.Fatal(err)
					}
					if leased {
						if mayNext || state.Job.Status != wp.Leased {
							t.Fatal("provider change interrupted a live lease or started fallback")
						}
						_, err = f.s.RenewWorkerJob(ctx, f.principal, claim.JobID, wp.LeaseRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID}, f.options)
						if err != nil {
							t.Fatal("existing lease could not renew", err)
						}
						_, err = f.s.CompleteWorkerJob(ctx, f.principal, claim.JobID, wp.CompleteRequest{WorkerID: "test-worker-01", LeaseID: claim.LeaseID, IdempotencyKey: "changed-provider-complete-0001", Result: json.RawMessage(`{"text":"finished old configuration"}`)}, f.options)
						if err != nil {
							t.Fatal("existing result was lost", err)
						}
					} else if !mayNext || state.Job.Status != wp.Cancelled || !strings.HasPrefix(state.Job.ErrorCode, "WORKER_PROVIDER_") {
						t.Fatal("unclaimed stale configuration not cancelled explicitly")
					}
				})
			}
		})
	}
	t.Run("queue does not silently become fallback", func(t *testing.T) {
		f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
		ctx := context.Background()
		job := f.enqueue(t, "queue-change")
		if _, err := f.s.Pool.Exec(ctx, `UPDATE providers SET enabled=false WHERE id=$1`, f.provider.ID); err != nil {
			t.Fatal(err)
		}
		state, mayNext, err := f.s.ReconcileWorkerJob(ctx, job.TaskID, job.Job.JobID, 30)
		if err != nil || mayNext || state.Job.Status != wp.Cancelled {
			t.Fatal("queue policy silently authorized HTTP fallback")
		}
	})
}
