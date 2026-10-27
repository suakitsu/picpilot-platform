package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform-go/core/risk"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func adminFixture(t *testing.T) (*Store, User, User, User) {
	t.Helper()
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: config.Platform}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Initialize(ctx, "admin@example.invalid", "PRIVATE_PASSWORD_HASH", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice@example.invalid", "PRIVATE_PASSWORD_HASH", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "bob@example.invalid", "PRIVATE_PASSWORD_HASH", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, user, other
}
func adminFilterFixture() AdminFilter { return AdminFilter{Page: 1, PageSize: 20} }
func adminTestNumber(v any) float64   { n, _ := v.(json.Number); value, _ := n.Float64(); return value }
func adminTaskFixture(t *testing.T, s *Store, u User, key string, cost int64) Task {
	t.Helper()
	task, _, err := s.SubmitTask(context.Background(), SubmitTask{UserID: u.ID, Capability: "image", Kind: "main", Request: json.RawMessage(`{"version":1,"image":{"prompt":"PRIVATE_PROMPT_SENTINEL","size":"1024x1024"}}`), IdempotencyKey: key, CreditsCost: cost})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestAdminPostgresCreditsIdempotencyAndAtomicAudit(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.AdminAdjustCredits(ctx, a.ID, u.ID, 50, "shared-credit-key", "support adjustment", "req-test")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.UserByID(ctx, u.ID)
	if err != nil || after.Credits != 50 {
		t.Fatal("credit mutation was not idempotent")
	}
	var ledger, audits int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE user_id=$1", u.ID).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE target_id=$1 AND action='manual_credit_adjust'", u.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 || audits != 1 {
		t.Fatalf("ledger=%d audits=%d", ledger, audits)
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, u.ID, 51, "shared-credit-key", "support adjustment", "req-test"); !errors.Is(err, ErrConflict) {
		t.Fatal("changed amount accepted")
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, u.ID, 50, "shared-credit-key", "different", "req-test"); !errors.Is(err, ErrConflict) {
		t.Fatal("changed remark accepted")
	}
	if _, _, err = s.AdminAdjustCredits(ctx, u.ID, b.ID, 1, "forbidden-credit-key", "", "req-test"); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("user can adjust credit")
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, u.ID, -51, "overdraft-credit-key", "", "req-test"); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatal("overdraft accepted")
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, b.ID, math.MaxInt64, "maximum-credit-key", "", "req"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, b.ID, 1, "overflow-credit-key", "", "req"); !errors.Is(err, ErrConflict) {
		t.Fatal("credit overflow accepted")
	}
	balanceView, err := s.AdminUser(ctx, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := balanceView["credits"].(json.Number); !ok || value.String() != "9223372036854775807" {
		t.Fatal("admin credit projection lost integer precision")
	}
	// Make the audit insertion fail to prove the balance and ledger roll back.
	if _, err = s.Pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT reject_test_audit CHECK(action<>'manual_credit_adjust') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AdminAdjustCredits(ctx, a.ID, u.ID, 7, "atomic-credit-key", "", "req-test"); err == nil {
		t.Fatal("audit failure not propagated")
	}
	after, _ = s.UserByID(ctx, u.ID)
	if after.Credits != 50 {
		t.Fatal("credit committed without audit")
	}
}

func TestAdminPostgresRolesLastAdminAndPassword(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	if err := s.AdminSetUserStatus(ctx, a.ID, a.ID, "disabled", "req"); !errors.Is(err, ErrLastAdmin) {
		t.Fatal("last admin disabled", err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE users SET role='admin' WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	hash, err := security.HashPassword("temporary-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AdminResetPassword(ctx, u.ID, a.ID, hash, "req"); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("admin can reset super admin")
	}
	if err = s.AdminSetUserStatus(ctx, u.ID, a.ID, "disabled", "req"); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("admin can disable super admin")
	}
	if err = s.AdminResetPassword(ctx, u.ID, b.ID, hash, "req"); err != nil {
		t.Fatal(err)
	}
	changed, _ := s.UserByID(ctx, b.ID)
	if changed.AuthVersion != 1 || !security.VerifyPassword(changed.PasswordHash, "temporary-fixture-password") {
		t.Fatal("password reset did not revoke sessions")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE users SET role='super_admin' WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 2)
	go func() { ch <- s.AdminSetUserStatus(ctx, a.ID, a.ID, "disabled", "req-a") }()
	go func() { ch <- s.AdminSetUserStatus(ctx, u.ID, u.ID, "disabled", "req-b") }()
	success, protected := 0, 0
	for i := 0; i < 2; i++ {
		err := <-ch
		if err == nil {
			success++
		} else if errors.Is(err, ErrLastAdmin) {
			protected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || protected != 1 {
		t.Fatal("concurrent last admin protection failed")
	}
	var active int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE role='super_admin' AND status='active'").Scan(&active); err != nil || active != 1 {
		t.Fatal("no active super admin remains")
	}
}

func TestAdminPostgresPaginationFiltersAndPrivacy(t *testing.T) {
	s, a, u, _ := adminFixture(t)
	ctx := context.Background()
	f := adminFilterFixture()
	f.PageSize = 1
	first, err := s.AdminUsers(ctx, a.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	f.Page = 2
	second, err := s.AdminUsers(ctx, a.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || first.TotalPages != 3 || len(first.Rows) != 1 || first.Rows[0]["id"] == second.Rows[0]["id"] {
		t.Fatal("pagination is not stable")
	}
	f.Page = 4
	empty, err := s.AdminUsers(ctx, a.ID, f)
	if err != nil || empty.Total != 3 || len(empty.Rows) != 0 {
		t.Fatal("empty page lost total")
	}
	f.Page = 1
	f.Keyword = "ALICE"
	filtered, err := s.AdminUsers(ctx, a.ID, f)
	if err != nil || filtered.Total != 1 || filtered.Rows[0]["id"] != u.ID {
		t.Fatal("user search failed")
	}
	raw, _ := json.Marshal(filtered)
	if strings.Contains(string(raw), "PRIVATE_PASSWORD") || strings.Contains(string(raw), "password_hash") || !strings.Contains(string(raw), "alice@example.invalid") {
		t.Fatal("privileged user projection incorrect")
	}
	f.Keyword = "%"
	literal, err := s.AdminUsers(ctx, a.ID, f)
	if err != nil || literal.Total != 0 {
		t.Fatal("search wildcard not literal")
	}
	f.PageSize = 101
	if _, err = s.AdminUsers(ctx, a.ID, f); !errors.Is(err, ErrConflict) {
		t.Fatal("oversized page accepted")
	}
	if _, err = s.AdminUsers(ctx, u.ID, adminFilterFixture()); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("user list not guarded")
	}
	if err = s.RecordUserActivity(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	activityFilter := adminFilterFixture()
	activityFilter.Sort = "last_active_desc"
	activity, err := s.AdminUsers(ctx, a.ID, activityFilter)
	if err != nil || activity.Rows[0]["id"] != u.ID || activity.Rows[0]["last_request_at"] == nil {
		t.Fatal("activity sort is not using real observations", err)
	}
	if err = s.Audit(ctx, a.ID, "fixture", "task", "test", "req", map[string]any{"prompt": "PRIVATE_PROMPT_SENTINEL", "authorization": "PRIVATE_AUTH_SENTINEL", "nested": map[string]string{"key": "PRIVATE_KEY_SENTINEL"}}); err != nil {
		t.Fatal(err)
	}
	logs, err := s.AdminAuditLogs(ctx, a.ID, adminFilterFixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(logs)
	if strings.Contains(string(raw), "PRIVATE_") {
		t.Fatal("audit leaked untrusted metadata")
	}
}

func TestAdminPostgresTaskRetryDeleteSafety(t *testing.T) {
	s, a, u, _ := adminFixture(t)
	ctx := context.Background()
	if _, _, err := s.AdminAdjustCredits(ctx, a.ID, u.ID, 100, "fund-task-fixture", "", "req"); err != nil {
		t.Fatal(err)
	}
	original := adminTaskFixture(t, s, u, "original-task-key", 10)
	if err := s.AdminDeleteImageTask(ctx, a.ID, original.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("queued task deleted")
	}
	lease, err := s.ClaimTask(ctx, "fixture", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FailTask(ctx, lease, "FIXTURE_FAILURE"); err != nil {
		t.Fatal(err)
	}
	child, replay, err := s.AdminRetryImageTask(ctx, a.ID, original.ID, "req")
	if err != nil || replay || child.ID == original.ID {
		t.Fatal("failed task retry failed", err)
	}
	again, replay, err := s.AdminRetryImageTask(ctx, a.ID, original.ID, "req")
	if err != nil || !replay || again.ID != child.ID {
		t.Fatal("retry is not idempotent", err)
	}
	after, _ := s.UserByID(ctx, u.ID)
	if after.Credits != 90 {
		t.Fatal("duplicate retry charge")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE tasks SET status='uncertain' WHERE id=$1", child.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AdminRetryImageTask(ctx, a.ID, child.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("uncertain task retried")
	}
	if err = s.AdminDeleteImageTask(ctx, a.ID, child.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("uncertain task deleted")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE tasks SET status='failed' WHERE id=$1", child.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AdminRetryImageTask(ctx, a.ID, child.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("unrefunded failure recharged")
	}
	if err = s.AdminDeleteImageTask(ctx, a.ID, child.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("unsettled failed task hidden")
	}
	if _, err = s.Pool.Exec(ctx, `INSERT INTO task_attempts(id,task_id,attempt,provider_scope,provider_id,protocol,model,status,request_hash,error_code,metadata) VALUES('fixture-attempt',$1,1,'platform','mock','openai_images_generations','fixture','uncertain',$2,'PRIVATE_AUTH value','{"api_key":"PRIVATE_KEY"}')`, child.ID, child.RequestHash); err != nil {
		t.Fatal(err)
	}
	if err = s.AdminDeleteImageTask(ctx, a.ID, child.ID, "req"); !errors.Is(err, ErrUnsafeRetry) {
		t.Fatal("failed task with uncertain upstream deleted")
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE tasks SET request_json=request_json||'{"headers":{"authorization":"PRIVATE_REQUEST_AUTH"},"api_key":"PRIVATE_REQUEST_KEY"}'::jsonb,result_json='{"upstream_response":"PRIVATE_UPSTREAM_RESPONSE","authorization":"PRIVATE_RESULT_AUTH"}' WHERE id=$1`, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AdminTaskDetail(ctx, u.ID, child.ID, "image"); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("ordinary task owner accessed admin detail")
	}
	detail, err := s.AdminTaskDetail(ctx, a.ID, child.ID, "image", "detail-regression")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(detail)
	inputs := detail["prompt_details"].(map[string]any)
	if inputs["prompt"] != "PRIVATE_PROMPT_SENTINEL" || inputs["size"] != "1024x1024" || !strings.Contains(detail["prompt_snapshot"].(string), "PRIVATE_PROMPT_SENTINEL") {
		t.Fatal("authorized admin cannot inspect submitted prompt and size")
	}
	for _, forbidden := range []string{"PRIVATE_AUTH", "PRIVATE_KEY", "PRIVATE_REQUEST_AUTH", "PRIVATE_REQUEST_KEY", "PRIVATE_UPSTREAM_RESPONSE", "PRIVATE_RESULT_AUTH", "request_hash", "request_json", "result_json"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("task detail leaked non-whitelisted input or upstream data", forbidden)
		}
	}
	filter := adminFilterFixture()
	filter.Action = "task_detail_view"
	logs, err := s.AdminAuditLogs(ctx, a.ID, filter)
	if err != nil || logs.Total != 1 || logs.Rows[0]["target_id"] != child.ID || logs.Rows[0]["request_id"] != "detail-regression" {
		t.Fatal("authorized task-detail view is not visible in audit history", err)
	}
	var auditMetadata string
	if err = s.Pool.QueryRow(ctx, `SELECT metadata::text FROM audit_events WHERE action='task_detail_view' AND target_id=$1`, child.ID).Scan(&auditMetadata); err != nil || strings.Contains(auditMetadata, "PRIVATE_") {
		t.Fatal("task-detail audit retained prompt or credentials", err)
	}
	if _, err = s.AdminAuditLogs(ctx, u.ID, filter); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("ordinary user accessed task-detail audit")
	}
	if err = s.AdminDeleteImageTask(ctx, a.ID, original.ID, "req"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Task(ctx, u.ID, original.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted task remains visible")
	}
	var charges int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE user_id=$1 AND biz_type='generation_charge'", u.ID).Scan(&charges); err != nil || charges != 2 {
		t.Fatal("ledger history changed")
	}
}

func TestAdminPostgresMessageOwnershipVisibilityAndUnread(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	f := adminFilterFixture()
	targeted, err := s.AdminPublishMessage(ctx, a.ID, "req", AdminMessageInput{Title: "Private message", Content: "For Alice", Audience: "targeted", RecipientID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.AdminPublishMessage(ctx, a.ID, "req", AdminMessageInput{Title: "Notice", Content: "For everyone"})
	if err != nil {
		t.Fatal(err)
	}
	staff, err := s.AdminPublishMessage(ctx, a.ID, "req", AdminMessageInput{Title: "Staff", Content: "For staff", Audience: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err = s.AdminPublishMessage(ctx, a.ID, "req", AdminMessageInput{Title: "Disabled", Content: "Hidden", Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	expired, err := s.AdminPublishMessage(ctx, a.ID, "req", AdminMessageInput{Title: "Expired", Content: "Hidden"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE admin_messages SET published_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1", expired["id"]); err != nil {
		t.Fatal(err)
	}
	alice, err := s.ListMessages(ctx, u.ID, f)
	if err != nil || alice.Total != 2 {
		t.Fatal("Alice visibility", err)
	}
	bob, err := s.ListMessages(ctx, b.ID, f)
	if err != nil || bob.Total != 1 {
		t.Fatal("Bob visibility", err)
	}
	for _, m := range []map[string]any{targeted, staff, expired} {
		if err = s.SetMessageRead(ctx, b.ID, m["id"].(string), true); !errors.Is(err, ErrNotFound) {
			t.Fatal("out of scope message read accepted")
		}
	}
	id := all["id"].(string)
	for i := 0; i < 2; i++ {
		if err = s.SetMessageRead(ctx, u.ID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.UnreadMessageCount(ctx, u.ID)
	if err != nil || count != 1 {
		t.Fatal("wrong unread count")
	}
	count, err = s.UnreadMessageCount(ctx, b.ID)
	if err != nil || count != 1 {
		t.Fatal("read status crossed users")
	}
	if err = s.SetMessageRead(ctx, u.ID, id, false); err != nil {
		t.Fatal(err)
	}
	count, _ = s.UnreadMessageCount(ctx, u.ID)
	if count != 2 {
		t.Fatal("unread mutation failed")
	}
	if _, err = s.AdminPublishMessage(ctx, u.ID, "req", AdminMessageInput{Title: "Attack", Content: "Unauthorized"}); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("user can publish")
	}
}

func TestAdminPostgresRiskIdempotencyOwnershipRestrictions(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	svc := risk.Service{Backend: s}
	threshold := 2
	if _, err := s.AdminUpdateRiskConfig(ctx, a.ID, "req", AdminRiskPatch{BanThreshold: &threshold}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminRiskKeyword(ctx, a.ID, "req", "high", "test-block", false); err != nil {
		t.Fatal(err)
	}
	req := risk.Request{UserID: u.ID, Feature: "image", Source: "user_input", RequestKey: "risk-request-one", Text: "PRIVATE_PROMPT_SENTINEL test-block"}
	d, err := svc.Check(ctx, req)
	if !errors.Is(err, risk.ErrBlocked) || d.Count != 1 {
		t.Fatal("risk did not block", err)
	}
	d, err = svc.Check(ctx, req)
	if !errors.Is(err, risk.ErrBlocked) || !d.Duplicate || d.Count != 1 {
		t.Fatal("risk replay counted twice")
	}
	req.RequestKey = "risk-request-two"
	d, err = svc.Check(ctx, req)
	if !errors.Is(err, risk.ErrAccountRestricted) || !d.Restricted {
		t.Fatal("threshold did not restrict")
	}
	banned, _ := s.UserByID(ctx, u.ID)
	if banned.Status != "disabled" || banned.AuthVersion != 1 || banned.Credits != 0 {
		t.Fatal("risk restriction incorrect")
	}
	rows, err := s.AdminRiskEvents(ctx, a.ID, adminFilterFixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "PRIVATE_PROMPT_SENTINEL") {
		t.Fatal("risk log leaked input")
	}
	count, status, err := s.AdminClearRiskEvents(ctx, a.ID, u.ID, "req")
	if err != nil || count != 2 || status != "active" {
		t.Fatal("risk clear failed", err)
	}
	if err = s.AdminSetUserStatus(ctx, a.ID, u.ID, "disabled", "req"); err != nil {
		t.Fatal(err)
	}
	_, status, err = s.AdminClearRiskEvents(ctx, a.ID, u.ID, "req")
	if err != nil || status != "disabled" {
		t.Fatal("risk clear bypassed manual disable")
	}
	req.UserID = a.ID
	req.RequestKey = "admin-risk-one"
	_, _ = svc.Check(ctx, req)
	req.RequestKey = "admin-risk-two"
	d, err = svc.Check(ctx, req)
	if !errors.Is(err, risk.ErrBlocked) || d.Restricted {
		t.Fatal("risk disabled an administrator")
	}
	otherTask := adminTaskFixture(t, s, b, "other-risk-task", 0)
	req.UserID = b.ID
	req.TaskID = otherTask.ID
	req.RequestKey = "own-risk-task"
	req.Source = "system_prompt"
	if _, err = svc.Check(ctx, req); err != nil {
		t.Fatal("system prompt screened")
	}
	req.UserID = a.ID
	req.Source = "user_input"
	req.RequestKey = "foreign-risk-task"
	if _, err = svc.Check(ctx, req); !errors.Is(err, ErrNotFound) {
		t.Fatal("risk event accepts foreign task")
	}
}

func TestAdminPostgresLibraryVersionsAndReporting(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	name, scene, content := "Listing", "listing", "PRIVATE_TEMPLATE_CONTENT"
	p, err := s.AdminSavePrompt(ctx, a.ID, "", "req", AdminPromptInput{Name: &name, Scene: &scene, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	changed := "Revised template"
	v := p.Version
	p, err = s.AdminSavePrompt(ctx, a.ID, p.ID, "req", AdminPromptInput{Content: &changed, Version: &v})
	if err != nil || p.Version != 2 {
		t.Fatal("prompt version failed", err)
	}
	if _, err = s.AdminSavePrompt(ctx, a.ID, p.ID, "req", AdminPromptInput{Content: &content, Version: &v}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale prompt overwrite allowed")
	}
	active, err := s.ActivePrompt(ctx, scene)
	if err != nil || active.Content != changed {
		t.Fatal("runtime prompt not readable")
	}
	c, err := s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "req", AdminImageConfigInput{RequiredFields: json.RawMessage(`"Product dimensions / main image"`)})
	if err != nil || c.RequiredFields != "Product dimensions / main image" {
		t.Fatal("legacy image fields failed", err)
	}
	task := adminTaskFixture(t, s, u, "cost-task-one", 0)
	_ = adminTaskFixture(t, s, u, "cost-task-two", 0)
	if err = s.RecordTaskCost(ctx, "cost-observation-one", task.ID, "1.234567"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordTaskCost(ctx, "cost-observation-one", task.ID, "1.234567"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordTaskCost(ctx, "cost-observation-one", task.ID, "2"); !errors.Is(err, ErrConflict) {
		t.Fatal("changed cost accepted")
	}
	f := adminFilterFixture()
	f.MediaType = "image"
	cost, err := s.AdminCosts(ctx, a.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if adminTestNumber(cost["total_cost"]) != 1.234567 || adminTestNumber(cost["unknown_cost_tasks"]) != 1 || adminTestNumber(cost["task_count"]) != 2 {
		t.Fatal("cost summary inaccurate", cost)
	}
	dash, err := s.AdminDashboard(ctx, a.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if adminTestNumber(dash["total_tasks"]) != 2 {
		t.Fatal("dashboard count")
	}
	if len(dash["trends"].([]map[string]any)) != 7 {
		t.Fatal("trend missing empty days")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE tasks SET status='failed' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.AdminDeleteImageTask(ctx, a.ID, task.ID, "req"); err != nil {
		t.Fatal(err)
	}
	cost, err = s.AdminCosts(ctx, a.ID, f)
	if err != nil || adminTestNumber(cost["task_count"]) != 2 || adminTestNumber(cost["total_cost"]) != 1.234567 {
		t.Fatal("deletion erased cost accounting", err)
	}
	metric := TextCallMetric{ID: "text-call-one", UserID: u.ID, Feature: "listing-copy", Provider: "mock", Model: "fixture", Status: "queued"}
	foreign, _, err := s.SubmitTask(ctx, SubmitTask{UserID: b.ID, Capability: "text", Kind: "direct", Request: json.RawMessage(`{"version":1,"text":{"prompt":"fixture"}}`), IdempotencyKey: "foreign-text-task"})
	if err != nil {
		t.Fatal(err)
	}
	foreignMetric := metric
	foreignMetric.TaskID = foreign.ID
	if err = s.RecordTextCall(ctx, foreignMetric); !errors.Is(err, ErrNotFound) {
		t.Fatal("text metric accepted another user's task")
	}
	if err = s.RecordTextCall(ctx, metric); err != nil {
		t.Fatal(err)
	}
	metric.Status = "success"
	metric.DurationMS = 123
	metric.InputChars = 10
	metric.OutputChars = 20
	if err = s.RecordTextCall(ctx, metric); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordTextCall(ctx, metric); err != nil {
		t.Fatal(err)
	}
	f = adminFilterFixture()
	f.UserKeyword = "alice"
	f.Status = "success"
	calls, err := s.AdminTextCalls(ctx, a.ID, f)
	if err != nil || calls.Total != 1 || adminTestNumber(calls.Summary["success_rate"]) != 100 {
		t.Fatal("text call reporting", err)
	}
	metric.Status = "failed"
	if err = s.RecordTextCall(ctx, metric); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal text metric overwritten")
	}
}

func TestAdminPostgresPersonalEditionIsolation(t *testing.T) {
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: config.Personal}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := s.Initialize(ctx, "owner@example.invalid", "fixture", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AdminUsers(ctx, owner.ID, adminFilterFixture()); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("personal admin available")
	}
	if _, _, err = s.AdminAdjustCredits(ctx, owner.ID, owner.ID, 1, "personal-credit-key", "", "req"); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("personal billing dependency")
	}
	msgs, err := s.ListMessages(ctx, owner.ID, adminFilterFixture())
	if err != nil || msgs.Total != 0 {
		t.Fatal("personal message history not empty", err)
	}
	c, err := s.RiskConfig(ctx)
	if err != nil || c.Enabled {
		t.Fatal("personal risk enabled")
	}
	svc := risk.Service{Backend: s}
	if _, err = svc.Check(ctx, risk.Request{UserID: owner.ID, Source: "user_input", RequestKey: "personal-risk-key", Text: "deepfake"}); err != nil {
		t.Fatal("personal request depends on platform risk", err)
	}
}
