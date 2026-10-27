package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

func TestPostgresTextTelemetryFollowsDurableHTTPAttempt(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "telemetry@example.invalid")
	ctx := context.Background()
	p, err := s.PutProvider(ctx, Provider{ID: "telemetry-text", OwnerScope: "platform", Name: "Telemetry", Capability: "text", Protocol: "openai_chat_completions", Profile: "openai", Model: "gpt-fixture", Enabled: true, ExecutionMode: "http", MaxConcurrency: 2, TimeoutSeconds: 90, LocalWaitSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := s.SubmitTask(ctx, SubmitTask{UserID: u.ID, Capability: "text", Kind: "fixture", IdempotencyKey: "text-telemetry-http", Request: json.RawMessage(`{"version":1,"text":{"messages":[{"role":"user","content":"hello"}]}}`)})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.ClaimTask(ctx, "telemetry-runner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.ReserveAttempt(ctx, lease, "platform", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 0 {
		t.Fatal("unsubmitted reservation logged as call", n, err)
	}
	if err = s.BeginSubmission(ctx, lease, a.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 1 {
		t.Fatal("inflight text attempt missing", n, err)
	}
	if _, err = s.CompleteTask(ctx, lease, json.RawMessage(`{"text":"world","payload":{"choices":[{"message":{"content":"world"}}]}}`)); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 1 {
		t.Fatal("terminal telemetry missing", n, err)
	}
	if n, err := s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 0 {
		t.Fatal("terminal telemetry duplicated", n, err)
	}
	var input, output int64
	var status, user, taskID string
	if err = s.Pool.QueryRow(ctx, `SELECT input_chars,output_chars,status,user_id,task_id FROM admin_text_calls WHERE id=$1`, a.ID).Scan(&input, &output, &status, &user, &taskID); err != nil {
		t.Fatal(err)
	}
	if input != 5 || output != 5 || status != "success" || user != u.ID || taskID != task.ID {
		t.Fatal("text attribution or counts changed")
	}
	var raw string
	if err = s.Pool.QueryRow(ctx, `SELECT row_to_json(c)::text FROM admin_text_calls c WHERE id=$1`, a.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "hello") || strings.Contains(raw, "world") {
		t.Fatal("text content persisted in telemetry")
	}
}

func TestPostgresTextTelemetryFollowsWorkerWithoutDoubleCountingReceipt(t *testing.T) {
	f := newWorkerFixture(t, wp.Text, wp.Responses, 1)
	ctx := context.Background()
	w := f.enqueue(t, "telemetry")
	if n, err := f.s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 1 {
		t.Fatal("worker queue missing", n, err)
	}
	claimed := f.claim(t, "test-worker-01")
	if claimed == nil {
		t.Fatal("worker did not receive text task")
	}
	_, err := f.s.CompleteWorkerJob(ctx, f.principal, w.Job.JobID, wp.CompleteRequest{WorkerID: "test-worker-01", LeaseID: claimed.LeaseID, IdempotencyKey: "telemetry-complete", Result: json.RawMessage(`{"text":"done"}`)}, f.options)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 1 {
		t.Fatal("worker completion missing", n, err)
	}
	if n, err := f.s.SyncTextCallTelemetry(ctx, 100); err != nil || n != 0 {
		t.Fatal("worker completion repeated", n, err)
	}
	var count int
	var chars int64
	if err = f.s.Pool.QueryRow(ctx, `SELECT count(*),max(output_chars) FROM admin_text_calls WHERE task_id=$1`, w.TaskID).Scan(&count, &chars); err != nil || count != 1 || chars != 4 {
		t.Fatal("worker telemetry mismatch", err, count, chars)
	}
}
