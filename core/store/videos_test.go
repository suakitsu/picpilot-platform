package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/video"
)

func videoStoreFixture(t *testing.T, edition config.Edition) (*Store, User, Provider) {
	t.Helper()
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: edition}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Initialize(ctx, "video@example.invalid", "test-only-hash", "Video tester")
	if err != nil {
		t.Fatal(err)
	}
	scope := "platform"
	if edition == config.Platform {
		if _, err := s.AdjustCredits(ctx, u.ID, 100000, "fixture", "funding", "Fixture", u.ID); err != nil {
			t.Fatal(err)
		}
	} else {
		scope = u.ID
	}
	p, err := s.PutProvider(ctx, Provider{ID: "video-fixture", OwnerScope: scope, Name: "Fixture", Capability: "video", Protocol: "apimart_video", Profile: "apimart", BaseURL: "https://example.invalid", Model: "gemini-omni-flash-preview", Enabled: true, MaxConcurrency: 4, TimeoutSeconds: 45, ExecutionMode: "http", OfflineAction: "queue", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, u, p
}

func videoQueuedFixture(t *testing.T, s *Store, u User, key string) Task {
	t.Helper()
	cost := int64(30)
	if s.Edition == config.Personal {
		cost = 0
	}
	task, _, err := s.SubmitTask(context.Background(), SubmitTask{UserID: u.ID, Capability: "video", Kind: "product_video", Title: "Fixture", Request: json.RawMessage(`{"version":1,"video":{"provider":"gemini"}}`), IdempotencyKey: key, CreditsCost: cost})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func videoSubmittingFixture(t *testing.T, s *Store, u User, p Provider, key string) (Task, Attempt) {
	t.Helper()
	ctx := context.Background()
	queued := videoQueuedFixture(t, s, u, key)
	task, err := s.ClaimTaskCapabilities(ctx, "video-submitter", time.Minute, []string{"video"})
	if err != nil || task.ID != queued.ID {
		t.Fatal("video claim", err)
	}
	a, err := s.ReserveAttempt(ctx, task, p.OwnerScope, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Provider.ID != p.ID || a.Provider.Version == 0 {
		t.Fatal("missing reservation provider snapshot")
	}
	if err := s.BeginVideoSubmission(ctx, task, a); err != nil {
		t.Fatal(err)
	}
	return task, a
}

func videoRecordFixture(t *testing.T, s *Store, task Task, a Attempt, id string) Task {
	t.Helper()
	updated, err := s.RecordVideoSubmission(context.Background(), task, a.ID, video.Submission{UpstreamTaskID: id, Provider: videoFamily(a.Protocol, a.Model), Model: a.Model}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func videoLedgerCount(t *testing.T, s *Store, id, kind string) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE biz_id=$1 AND biz_type=$2", id, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPostgresVideoKnownIDLeasesAndFailureSettlement(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	task, a := videoSubmittingFixture(t, s, u, p, "known-id")
	videoRecordFixture(t, s, task, a, "upstream-known")
	videoRecordFixture(t, s, task, a, "upstream-known")
	if _, err := s.ClaimTaskCapabilities(ctx, "another-submitter", time.Minute, []string{"video"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("acknowledged video returned to submission queue", err)
	}
	var wins atomic.Int32
	claims := make(chan VideoPollClaim, 6)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := s.ClaimVideoPoll(ctx, "poller", time.Minute)
			if err == nil {
				claims <- claim
				wins.Add(1)
			} else if !errors.Is(err, ErrNotFound) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(claims)
	if wins.Load() != 1 {
		t.Fatal("poll claim was not exclusive")
	}
	first := <-claims
	if first.Submission.UpstreamTaskID != "upstream-known" {
		t.Fatal("poll claim lost upstream identity")
	}
	if err := s.RenewTask(ctx, first.Task, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyVideoPoll(ctx, first.Task, video.PollResult{Status: video.Generating, Progress: 60}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyVideoPoll(ctx, first.Task, video.PollResult{Status: video.Generating, Progress: 20}, 0); err != nil {
		t.Fatal(err)
	}
	v, err := s.VideoTask(ctx, u.ID, task.ID)
	if err != nil || v.Progress != 60 || v.UpstreamTaskID != "upstream-known" {
		t.Fatal("progress or identity lost", err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE tasks SET lease_until=now()-interval '1 second' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimVideoPoll(ctx, "recovery-poller", time.Minute)
	if err != nil || second.Task.LeaseID == first.Task.LeaseID {
		t.Fatal("expired poll not reclaimed", err)
	}
	if _, err := s.ApplyVideoPoll(ctx, first.Task, video.PollResult{Status: video.Failed}, 0); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatal("stale poll result accepted", err)
	}
	if err := s.ReleaseVideoPoll(ctx, second.Task, 0); err != nil {
		t.Fatal(err)
	}
	third, err := s.ClaimVideoPoll(ctx, "poller", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.ApplyVideoPoll(ctx, third.Task, video.PollResult{Status: video.Failed, Error: "fake credential and prompt must not persist", Metadata: map[string]string{"api_key": "fixture-key", "actual_cost": "1.25"}}, 0)
	if err != nil || failed.Status != "failed" || strings.Contains(string(failed.Result), "fixture-key") || strings.Contains(string(failed.Result), "credential") {
		t.Fatal("definitive failure not normalized", err)
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ReconcileVideoCallback(ctx, video.Callback{UpstreamTaskID: "upstream-known", Result: video.PollResult{Status: video.Failed}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if videoLedgerCount(t, s, task.ID, "generation_charge") != 1 || videoLedgerCount(t, s, task.ID, "generation_refund") != 1 {
		t.Fatal("duplicate settlement")
	}
	if _, err := s.VideoMetrics(ctx); err != nil {
		t.Fatal("metrics query", err)
	}
}

func TestPostgresVideoEarlyCallbackStorageAndTerminalFencing(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	task, a := videoSubmittingFixture(t, s, u, p, "early-callback")
	completed := video.PollResult{Status: video.Completed, ResultURL: "https://cdn.example.invalid/result.mp4", Progress: 100, Metadata: map[string]string{"actual_cost": "1.20", "prompt": "do not store"}}
	if applied, err := s.ReconcileVideoCallback(ctx, video.Callback{UpstreamTaskID: "early-id", Result: completed}); err != nil || applied {
		t.Fatal("early callback must be retained", err)
	}
	videoRecordFixture(t, s, task, a, "early-id")
	claim, err := s.ClaimVideoPoll(ctx, "storage-poller", time.Minute)
	if err != nil || claim.Pending == nil || claim.Pending.Status != video.Completed || claim.Pending.Metadata["prompt"] != "" {
		t.Fatal("early completion not staged safely", err)
	}
	if _, err := s.ApplyVideoPoll(ctx, claim.Task, video.PollResult{Status: video.Failed}, 0); err != nil {
		t.Fatal(err)
	}
	v, err := s.VideoTask(ctx, u.ID, task.ID)
	if err != nil || v.Status != "polling" || len(v.PendingResult) == 0 || strings.Contains(string(v.Result), "cdn.example") {
		t.Fatal("unverified result published or first completion reversed", err)
	}
	if _, err := s.CompleteVideoTask(ctx, claim.Task, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unpersisted asset completed", err)
	}
	asset, err := s.SaveAsset(ctx, Asset{ID: security.NewID("asset"), UserID: u.ID, TaskID: task.ID, ImageType: "video", StorageKey: "video/test.mp4", ContentType: "video/mp4", FileSize: 32, SHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		finished, err := s.CompleteVideoTask(ctx, claim.Task, asset.ID)
		if err != nil || finished.Status != "succeeded" || strings.Contains(string(finished.Result), "result_url") || !strings.Contains(string(finished.Result), asset.ID) {
			t.Fatal("persisted asset completion", err)
		}
	}
	if applied, err := s.ReconcileVideoCallback(ctx, video.Callback{UpstreamTaskID: "early-id", Result: video.PollResult{Status: video.Failed}}); err != nil || applied {
		t.Fatal("terminal callback was not ignored", err)
	}
	if videoLedgerCount(t, s, task.ID, "generation_refund") != 0 {
		t.Fatal("completed video refunded")
	}
}

func TestPostgresVideoUncertainAndCancellationNeverRepost(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	task, a := videoSubmittingFixture(t, s, u, p, "unknown-submission")
	if err := s.MarkUncertain(ctx, task, a.ID, "SUBMISSION_OUTCOME_UNKNOWN"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.CancelVideoTask(ctx, u.ID, task.ID)
	if err != nil || cancelled.Status != "uncertain" || cancelled.CancelRequestedAt == nil {
		t.Fatal("uncertain cancellation changed settlement", err)
	}
	if _, err := s.ClaimTaskCapabilities(ctx, "retry", time.Minute, []string{"video"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("uncertain submission was queued", err)
	}
	if _, err := s.ClaimVideoPoll(ctx, "poll", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown id was polled", err)
	}
	if videoLedgerCount(t, s, task.ID, "generation_refund") != 0 {
		t.Fatal("uncertain cancellation refunded")
	}
	late := videoRecordFixture(t, s, task, a, "reconciled-known-id")
	if late.Status != "polling" || late.CancelRequestedAt == nil {
		t.Fatal("late acknowledgement not reconciled")
	}
	claim, err := s.ClaimVideoPoll(ctx, "poll", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyVideoPoll(ctx, claim.Task, video.PollResult{Status: video.Failed}, 0); err != nil {
		t.Fatal(err)
	}
	queued := videoQueuedFixture(t, s, u, "cancel-queued")
	for range 2 {
		if _, err := s.CancelVideoTask(ctx, u.ID, queued.ID); err != nil {
			t.Fatal(err)
		}
	}
	if videoLedgerCount(t, s, queued.ID, "generation_refund") != 1 || videoLedgerCount(t, s, task.ID, "generation_refund") != 1 {
		t.Fatal("cancellation refund count")
	}
}

func TestPostgresVideoOwnershipHistoryHideAndAudit(t *testing.T) {
	s, u, _ := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	other, err := s.CreateUser(ctx, "other@example.invalid", "fixture", "Other")
	if err != nil {
		t.Fatal(err)
	}
	task := videoQueuedFixture(t, s, u, "hide-idempotency")
	if _, err := s.VideoTask(ctx, other.ID, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user read", err)
	}
	if err := s.DeleteVideoTask(ctx, u.ID, task.ID, false, ""); !errors.Is(err, ErrTerminalConflict) {
		t.Fatal("active task hidden", err)
	}
	if _, err := s.CancelVideoTask(ctx, u.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVideoTask(ctx, u.ID, task.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	page, err := s.VideoTasks(ctx, VideoTaskFilter{UserID: u.ID})
	if err != nil || page.Total != 0 {
		t.Fatal("hidden task in own history", err)
	}
	page, err = s.VideoTasks(ctx, VideoTaskFilter{Admin: true, Status: "cancelled", Keyword: task.ID, Page: 1, PageSize: 1})
	if err != nil || page.Total != 1 || page.TotalPages != 1 || page.Rows[0].HiddenAt == nil {
		t.Fatal("admin hidden history", err)
	}
	if err := s.DeleteVideoTask(ctx, other.ID, task.ID, true, "fixture"); !errors.Is(err, ErrNotFound) {
		t.Fatal("non-admin deletion", err)
	}
	if err := s.DeleteVideoTask(ctx, u.ID, task.ID, true, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VideoTaskByKey(ctx, u.ID, "hide-idempotency"); err != nil {
		t.Fatal("deletion erased idempotency", err)
	}
	var audit int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='video_task_delete' AND target_id=$1", task.ID).Scan(&audit); err != nil || audit != 1 {
		t.Fatal("missing deletion audit", err)
	}
}

func TestPostgresVideoProviderIdentityAndConfigurationCircuit(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	if err := s.RecordProviderOutcome(ctx, p, "VIDEO_AUTH", true); err != nil {
		t.Fatal(err)
	}
	queued := videoQueuedFixture(t, s, u, "circuit-video")
	lease, err := s.ClaimTaskCapabilities(ctx, "worker", time.Minute, []string{"video"})
	if err != nil || lease.ID != queued.ID {
		t.Fatal(err)
	}
	if _, err := s.ReserveAttempt(ctx, lease, p.OwnerScope, p.ID); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("video bypassed shared circuit", err)
	}
	if err := s.SaveVideoConfiguration(ctx, u.ID, json.RawMessage(`{"enabled":true}`), []VideoProviderUpdate{{Provider: p}}); err != nil {
		t.Fatal(err)
	}
	health, err := s.ProviderHealth(ctx, "platform")
	if err != nil || len(health) != 0 {
		t.Fatal("legacy config failed to reset health", err)
	}
	a, err := s.ReserveAttempt(ctx, lease, p.OwnerScope, p.ID)
	if err != nil || a.Provider.Version <= p.Version {
		t.Fatal("reservation did not capture current provider", err)
	}
	if err := s.BeginVideoSubmission(ctx, lease, a); err != nil {
		t.Fatal(err)
	}
	newCipher := "fixture-encrypted-key"
	if err := s.SaveVideoConfiguration(ctx, u.ID, json.RawMessage(`{}`), []VideoProviderUpdate{{Provider: p, Ciphertext: &newCipher}}); !errors.Is(err, ErrConflict) {
		t.Fatal("credential changed under paid attempt", err)
	}
	if _, err := s.RecordVideoSubmission(ctx, lease, a.ID, video.Submission{Provider: video.Agnes, Model: a.Model, UpstreamTaskID: "identity"}, 0); !errors.Is(err, ErrConflict) {
		t.Fatal("incorrect provider acknowledgement accepted", err)
	}
	videoRecordFixture(t, s, lease, a, "identity")
	p2 := p
	p2.ID, p2.Version = "video-second-account", 0
	p2, err = s.PutProvider(ctx, p2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t2, a2 := videoSubmittingFixture(t, s, u, p2, "second-account")
	videoRecordFixture(t, s, t2, a2, "identity")
	if _, err := s.ReconcileVideoCallback(ctx, video.Callback{UpstreamTaskID: "identity", Result: video.PollResult{Status: video.Failed}}); !errors.Is(err, ErrConflict) {
		t.Fatal("ambiguous account callback accepted", err)
	}
}

func TestVideoResultSanitization(t *testing.T) {
	for _, raw := range []string{"file:///tmp/result", "https://user:pass@example.invalid/a.mp4", "https://example.invalid/a.mp4?api_key=fixture", "https://example.invalid/a.mp4?prompt=fixture"} {
		if _, err := safeVideoResult(video.PollResult{Status: video.Completed, ResultURL: raw}); err == nil {
			t.Fatal("unsafe result accepted")
		}
	}
	if _, err := safeVideoResult(video.PollResult{Status: "invented"}); err == nil {
		t.Fatal("unknown state accepted")
	}
	result, err := safeVideoResult(video.PollResult{Status: video.Completed, ResultURL: "https://cdn.example.invalid/a.mp4?signature=download-token", Metadata: map[string]string{"actual_cost": "1.23", "prompt": "fixture", "upstream_cost": "not-a-number"}})
	if err != nil || len(result.Metadata) != 1 {
		t.Fatal("result allowlist", err)
	}
}

func TestPostgresVideoPollUsesReservedCredentialSnapshot(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	oldCipher := "fixture-old-encrypted-credential"
	p, err := s.PutProvider(ctx, p, &oldCipher)
	if err != nil {
		t.Fatal(err)
	}
	task, a := videoSubmittingFixture(t, s, u, p, "snapshot-video")
	newCipher := "fixture-rotated-encrypted-credential"
	rotated, err := s.PutProvider(ctx, p, &newCipher)
	if err != nil {
		t.Fatal(err)
	}
	videoRecordFixture(t, s, task, a, "snapshot-known-id")
	claim, err := s.ClaimVideoPoll(ctx, "poller", time.Minute)
	if err != nil || claim.Provider.SecretCiphertext != oldCipher || claim.Provider.Version != a.Provider.Version || claim.Provider.Version == rotated.Version {
		t.Fatal("poll did not retain reservation snapshot", err)
	}
	if err := s.RecordProviderOutcome(ctx, claim.Provider, "OLD_ACCOUNT_AUTH_FAILURE", true); err != nil {
		t.Fatal(err)
	}
	health, err := s.ProviderHealth(ctx, p.OwnerScope)
	if err != nil || len(health) != 0 {
		t.Fatal("old video snapshot reopened new provider circuit", err)
	}
	encoded, err := json.Marshal(claim)
	if err != nil || strings.Contains(string(encoded), oldCipher) {
		t.Fatal("poll claim JSON exposed credential snapshot", err)
	}
	if err := s.BeginVideoSubmission(ctx, task, a); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatal("submission boundary entered twice", err)
	}
}

func TestPostgresVideoConfigurationCASAndRetiredKeyMetrics(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	if err := s.SaveVideoConfigurationCAS(ctx, u.ID, json.RawMessage(`{}`), json.RawMessage(`{"enabled":true}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVideoConfigurationCAS(ctx, u.ID, json.RawMessage(`{}`), json.RawMessage(`{"enabled":false}`), nil); !errors.Is(err, ErrConflict) {
		t.Fatal("stale config update overwrote newer settings", err)
	}
	p.Protocol, p.Model = "agnes_video", "agnes-video-v2.0"
	p, err := s.PutProvider(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, a := videoSubmittingFixture(t, s, u, p, "key-metric")
	videoRecordFixture(t, s, task, a, "metric-id")
	metrics, err := s.VideoKeyMetrics(ctx, "platform")
	if err != nil || metrics[p.ID].Active != 1 || metrics[p.ID].Today != 1 {
		t.Fatal("Agnes active metrics", err)
	}
	claim, err := s.ClaimVideoPoll(ctx, "poll", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyVideoPoll(ctx, claim.Task, video.PollResult{Status: video.Failed}, 0); err != nil {
		t.Fatal(err)
	}
	metrics, err = s.VideoKeyMetrics(ctx, "platform")
	if err != nil || metrics[p.ID].Active != 0 || metrics[p.ID].Failed != 1 {
		t.Fatal("Agnes terminal metrics", err)
	}
}

func TestPostgresVideoCrashAfterSubmissionBoundary(t *testing.T) {
	s, u, p := videoStoreFixture(t, config.Platform)
	ctx := context.Background()
	task, a := videoSubmittingFixture(t, s, u, p, "crash-boundary")
	if _, err := s.Pool.Exec(ctx, "UPDATE tasks SET lease_until=now()-interval '1 second' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RecoverExpiredTaskLeases(ctx); err != nil || n != 1 {
		t.Fatal("submission crash not recovered", err)
	}
	if _, err := s.ClaimTaskCapabilities(ctx, "resubmit", time.Minute, []string{"video"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("crash reopened paid submission", err)
	}
	if _, err := s.ClaimVideoPoll(ctx, "poll", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing upstream id eligible for poll", err)
	}
	late := videoRecordFixture(t, s, task, a, "late-crash-ack")
	if late.Status != "polling" {
		t.Fatal("late acknowledgement not accepted for same fenced attempt")
	}
	claim, err := s.ClaimVideoPoll(ctx, "poll", time.Minute)
	if err != nil || claim.Provider.ID != p.ID || claim.Submission.UpstreamTaskID != "late-crash-ack" {
		t.Fatal("crash lost durable poll snapshot", err)
	}
	if videoLedgerCount(t, s, task.ID, "generation_refund") != 0 {
		t.Fatal("crash refunded an ambiguous paid request")
	}
}
