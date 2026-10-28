package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
	wp "github.com/suakitsu/picpilot-platform/core/workerprotocol"
)

func dispatchWorkerFixture(t *testing.T, capability string) (*workerHTTPFixture, store.Provider, *provider.Gateway) {
	t.Helper()
	f := newWorkerHTTPFixture(t, config.Platform)
	protocol, model := string(wp.ImageGenerations), wp.ImageModel
	if capability == "text" {
		protocol, model = string(wp.ChatCompletions), "gpt-5.5"
	}
	p, err := f.s.Store.PutProvider(context.Background(), store.Provider{OwnerScope: f.scope, ID: "local-test-dispatch", Name: "Mock local", Capability: capability, Protocol: protocol, Profile: "openai", Model: model, Enabled: true, Priority: -100, ExecutionMode: "pull_worker", MaxConcurrency: 2, TimeoutSeconds: 90, LocalWaitSeconds: 1, OfflineAction: "fallback", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Store.AdjustCredits(context.Background(), f.user.ID, 100, "fixture", "dispatch-initial", "Mock credits", f.user.ID); err != nil {
		t.Fatal(err)
	}
	g, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.CloseIdleConnections)
	return f, p, g
}
func dispatchTask(t *testing.T, f *workerHTTPFixture, payload jobs.Payload) store.Task {
	t.Helper()
	capability := "image"
	if payload.Text != nil {
		capability = "text"
	}
	data, _ := json.Marshal(payload)
	task, _, err := f.s.Store.SubmitTask(context.Background(), store.SubmitTask{UserID: f.user.ID, Capability: capability, Kind: "dispatch-test", Request: data, IdempotencyKey: "dispatch-test-task", CreditsCost: 6})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func runDispatch(t *testing.T, f *workerHTTPFixture, g *provider.Gateway) store.Task {
	t.Helper()
	_, err := f.s.Store.Pool.Exec(context.Background(), `UPDATE tasks SET next_run_at=clock_timestamp() WHERE status IN ('queued','waiting_worker')`)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := f.s.Store.ClaimTask(context.Background(), "dispatch-test-server", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	f.s.processHTTPModelTask(context.Background(), lease, g)
	current, err := f.s.Store.Task(context.Background(), f.user.ID, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}
func dispatchFallback(t *testing.T, f *workerHTTPFixture) (*atomic.Int32, *httptest.Server) {
	t.Helper()
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); mockImageReply(w) }))
	t.Cleanup(server.Close)
	secret, err := f.s.Vault.Encrypt(f.scope+"/http-test-fallback", "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Store.PutProvider(context.Background(), store.Provider{OwnerScope: f.scope, ID: "http-test-fallback", Name: "Mock fallback", Capability: "image", Protocol: string(wp.ImageGenerations), Profile: "openai", BaseURL: server.URL, Model: wp.ImageModel, Enabled: true, Priority: 100, ExecutionMode: "http", MaxConcurrency: 2, TimeoutSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, &secret)
	if err != nil {
		t.Fatal(err)
	}
	return count, server
}

func TestPostgresWorkerDispatchImageCompleteChargesOnce(t *testing.T) {
	f, p, g := dispatchWorkerFixture(t, "image")
	count, _ := dispatchFallback(t, f)
	task := dispatchTask(t, f, jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "Mock product, no crop", Size: "3x2"}})
	if status := runDispatch(t, f, g).Status; status != "waiting_worker" {
		t.Fatal("worker enqueue did not release runner", status)
	}
	claim := f.claim(t)
	if claim.Protocol != wp.ImageGenerations || claim.Model != wp.ImageModel {
		t.Fatal("worker protocol mapping incorrect")
	}
	// Disabling a provider after claim cannot hide the outstanding lease.
	p.Enabled = false
	if _, err := f.s.Store.PutProvider(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if status := runDispatch(t, f, g).Status; status != "waiting_worker" || count.Load() != 0 {
		t.Fatal("active local lease raced an HTTP fallback")
	}
	source := workerPNG(t)
	form := wp.ArtifactForm{WorkerID: "http-test-worker", LeaseID: claim.LeaseID, IdempotencyKey: "dispatch-artifact-001", SHA256: wp.SHA256(source)}
	body, ct := workerMultipart(t, form, source, false)
	w := workerUpload(f.s, claim.JobID, body, ct)
	artifact, err := wp.Decode[wp.ArtifactResponse](w.Body.Bytes())
	if err != nil || w.Code != 200 {
		t.Fatal("artifact upload failed", w.Code)
	}
	complete, _ := json.Marshal(wp.CompleteRequest{WorkerID: form.WorkerID, LeaseID: claim.LeaseID, IdempotencyKey: "dispatch-complete-001", ArtifactID: artifact.ArtifactID})
	w = workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/complete", httpWorkerTestToken, complete)
	if w.Code != 200 {
		t.Fatal("worker complete failed", w.Code)
	}
	final := runDispatch(t, f, g)
	if final.Status != "succeeded" || count.Load() != 0 {
		t.Fatal("local result not settled", final.Status, final.ErrorCode)
	}
	var result map[string]any
	_ = json.Unmarshal(final.Result, &result)
	if !strings.HasPrefix(result["artifact_id"].(string), "asset_") {
		t.Fatal("worker receipt ID exposed instead of owned asset")
	}
	w = workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/complete", httpWorkerTestToken, complete)
	replay, err := wp.Decode[wp.TerminalResponse](w.Body.Bytes())
	if w.Code != 200 || err != nil || !replay.Idempotent {
		t.Fatal("callback replay after settlement failed")
	}
	u, _ := f.s.Store.UserByID(context.Background(), f.user.ID)
	var attempts, charges int
	if err = f.s.Store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM task_attempts WHERE task_id=$1),(SELECT count(*) FROM credit_ledger WHERE biz_id=$1 AND biz_type='generation_charge')`, task.ID).Scan(&attempts, &charges); err != nil || u.Credits != 94 || attempts != 1 || charges != 1 {
		t.Fatal("local completion settled more than once")
	}
}

func TestPostgresWorkerUnclaimedFallbackDoesNotDuplicate(t *testing.T) {
	f, _, g := dispatchWorkerFixture(t, "image")
	count, _ := dispatchFallback(t, f)
	task := dispatchTask(t, f, jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "Mock only", Size: "32x32"}})
	runDispatch(t, f, g)
	if _, err := f.s.Store.Pool.Exec(context.Background(), `UPDATE worker_jobs SET queued_at=clock_timestamp()-interval '10 seconds' WHERE task_id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	final := runDispatch(t, f, g)
	if final.Status != "succeeded" || count.Load() != 1 {
		t.Fatal("unclaimed offline fallback failed", final.Status, final.ErrorCode)
	}
	job, err := f.s.Store.LatestWorkerJobForTask(context.Background(), task.ID)
	if err != nil || job.Job.Status != wp.Cancelled || job.Job.ErrorCode != "LOCAL_WORKER_OFFLINE" {
		t.Fatal("job remained claimable during fallback")
	}
	u, _ := f.s.Store.UserByID(context.Background(), f.user.ID)
	if u.Credits != 94 {
		t.Fatal("fallback charged or refunded twice")
	}
}

func TestPostgresWorkerExpiredLeaseNeverStartsPaidFallback(t *testing.T) {
	f, _, g := dispatchWorkerFixture(t, "image")
	count, _ := dispatchFallback(t, f)
	dispatchTask(t, f, jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "Mock only", Size: "32x32"}})
	runDispatch(t, f, g)
	claim := f.claim(t)
	if _, err := f.s.Store.Pool.Exec(context.Background(), `UPDATE worker_jobs SET renew_after=clock_timestamp()-interval '30 seconds',lease_until=clock_timestamp()-interval '20 seconds',grace_until=clock_timestamp()-interval '5 seconds' WHERE job_id=$1`, claim.JobID); err != nil {
		t.Fatal(err)
	}
	final := runDispatch(t, f, g)
	u, _ := f.s.Store.UserByID(context.Background(), f.user.ID)
	if final.Status != "uncertain" || count.Load() != 0 || u.Credits != 94 {
		t.Fatal("unknown worker outcome was retried or refunded", final.Status)
	}
}

func TestPostgresWorkerTextVisionAndEditsInputMapping(t *testing.T) {
	f, p, g := dispatchWorkerFixture(t, "text")
	a, err := f.s.saveImage(context.Background(), f.user.ID, "", "", "reference", "fixture.png", bytes.NewReader(workerPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal([]any{map[string]any{"type": "text", "text": "Keep exact text"}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": assetURIPrefix + a.ID}}})
	dispatchTask(t, f, jobs.Payload{Version: 1, Text: &jobs.TextInput{Messages: []provider.Message{{Role: "user", Content: content}}}})
	if status := runDispatch(t, f, g).Status; status != "waiting_worker" {
		t.Fatal("vision did not use local", status)
	}
	claim := f.claim(t)
	if claim.Protocol != wp.ChatCompletions || bytes.Contains(claim.Request, []byte(assetURIPrefix)) || bytes.Contains(claim.Request, []byte("base64")) || !bytes.Contains(claim.Request, []byte("https://worker.example.invalid/api/files/assets/")) {
		t.Fatal("vision URL was not materialized at claim")
	}
	complete, _ := json.Marshal(wp.CompleteRequest{WorkerID: "http-test-worker", LeaseID: claim.LeaseID, IdempotencyKey: "dispatch-text-complete-001", Result: json.RawMessage(`{"text":"Mock vision response"}`)})
	w := workerHTTPRequest(f.s, "POST", wp.BasePath+"/jobs/"+claim.JobID+"/complete", httpWorkerTestToken, complete)
	if w.Code != 200 {
		t.Fatal("text complete failed", w.Code)
	}
	final := runDispatch(t, f, g)
	if final.Status != "succeeded" || !bytes.Contains(final.Result, []byte("Mock vision response")) {
		t.Fatal("text result not normalized")
	}
	p.Capability = "image"
	p.Protocol = string(wp.ImageGenerations)
	p.Model = wp.ImageModel
	in, err := f.s.compileWorkerInput(context.Background(), store.Task{ID: "task_fixture", UserID: f.user.ID, Capability: "image"}, p, jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "Use reference", Size: "3x2", ReferenceAssetIDs: []string{a.ID}}})
	if err != nil || in.Protocol != wp.ImageEdits || len(in.InputAssetIDs) != 1 {
		t.Fatal("reference image not mapped to edits", err)
	}
}
