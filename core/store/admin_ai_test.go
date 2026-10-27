package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func adminAIVault(t *testing.T) *security.Vault {
	t.Helper()
	v, err := security.NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAdminAIPostgresAtomicVersionsAndScope(t *testing.T) {
	s, a, u, _ := adminFixture(t)
	ctx := context.Background()
	v := adminAIVault(t)
	state, err := s.AdminAIConfig(ctx, a.ID)
	if err != nil || len(state.Providers) < 2 {
		t.Fatal("missing canonical worker providers", err)
	}
	if _, err = s.AdminAIConfig(ctx, u.ID); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("ordinary user read AI configuration")
	}
	video := state.Providers[0]
	video.ID, video.Capability, video.Protocol, video.ExecutionMode = "ai-video", "video", "apimart_video", "http"
	video.BaseURL = "https://example.invalid"
	if _, err = s.PutProvider(ctx, video, nil); err != nil {
		t.Fatal(err)
	}
	private := state.Providers[0]
	private.OwnerScope, private.ID = u.ID, "private-provider"
	if _, err = s.PutProvider(ctx, private, nil); err != nil {
		t.Fatal(err)
	}
	ps := append([]Provider{}, state.Providers...)
	ps[0].Name = "Edited worker"
	key, err := v.Encrypt("platform/mock-image", "MOCK_API_KEY_SENTINEL")
	if err != nil {
		t.Fatal(err)
	}
	mock := Provider{ID: "mock-image", OwnerScope: "platform", Name: "Mock", Capability: "image", Protocol: "openai_images_generations", Profile: "openai", BaseURL: "https://example.invalid/v1", SecretCiphertext: key, HasAPIKey: true, Model: "fixture-image", Enabled: true, ExecutionMode: "http", MaxConcurrency: 1, TimeoutSeconds: 90, OfflineAction: "fallback", LeaseSeconds: 120, Settings: json.RawMessage(`{}`)}
	ps = append(ps, mock)
	if err = s.AdminSaveAIConfig(ctx, u.ID, "request", state.ETag, "save", ps, v); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("ordinary user saved AI configuration")
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "request", state.ETag, "save", ps, v); err != nil {
		t.Fatal(err)
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "request", state.ETag, "save", ps, v); !errors.Is(err, ErrConflict) {
		t.Fatal("stale replacement accepted")
	}
	after, err := s.AdminAIConfig(ctx, a.ID)
	if err != nil || !after.CanRollback || after.ETag == state.ETag {
		t.Fatal("missing revision", err)
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "noop", after.ETag, "save", after.Providers, v); err != nil {
		t.Fatal(err)
	}
	versions, err := s.AdminAIVersions(ctx, a.ID, AdminFilter{Page: 1, PageSize: 1})
	if err != nil || versions.Total != 1 {
		t.Fatal("noop created revision", err)
	}
	for _, scopeID := range [][2]string{{"platform", video.ID}, {u.ID, private.ID}} {
		if _, err = s.Provider(ctx, scopeID[0], scopeID[1]); err != nil {
			t.Fatal("save modified an unrelated provider", err)
		}
	}
	// Save a second state so its history actually contains an encrypted key.
	next := append([]Provider{}, after.Providers...)
	for i := range next {
		if next[i].ID == mock.ID {
			next[i].Name = "Revised mock"
		}
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "second", after.ETag, "save", next, v); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err = s.Pool.QueryRow(ctx, "SELECT snapshot_ciphertext FROM admin_ai_config_versions ORDER BY version DESC LIMIT 1").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot, "MOCK_API_KEY") || strings.Contains(snapshot, key) || strings.Contains(snapshot, "fixture-image") {
		t.Fatal("history is not encrypted")
	}
	versions, err = s.AdminAIVersions(ctx, a.ID, AdminFilter{Page: 2, PageSize: 1})
	if err != nil || versions.Total != 2 || len(versions.Rows) != 1 {
		t.Fatal("history pagination", err)
	}
	raw, _ := json.Marshal(versions)
	if strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "MOCK_API_KEY") {
		t.Fatal("history list leaked secrets")
	}
	prior, err := s.AdminAIRollbackSnapshot(ctx, a.ID, 0, v)
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := s.AdminAIConfig(ctx, a.ID)
	if err = s.AdminSaveAIConfig(ctx, a.ID, "rollback", latest.ETag, "rollback", prior, v); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Provider(ctx, "platform", mock.ID)
	if err != nil || restored.Name != mock.Name {
		t.Fatal("rollback did not restore canonical row", err)
	}
	plain, err := v.Decrypt("platform/"+mock.ID, restored.SecretCiphertext)
	if err != nil || plain != "MOCK_API_KEY_SENTINEL" {
		t.Fatal("rollback credential scope broken")
	}
	var audits int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action IN ('ai_config_save','ai_config_rollback')").Scan(&audits); err != nil || audits != 3 {
		t.Fatal("revision/audit atomicity", err)
	}
}

func TestAdminAIPostgresConcurrentSavesBusyAndModelReferences(t *testing.T) {
	s, a, u, _ := adminFixture(t)
	ctx := context.Background()
	v := adminAIVault(t)
	state, err := s.AdminAIConfig(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			ps := append([]Provider{}, state.Providers...)
			ps[0].Name = name
			errs <- s.AdminSaveAIConfig(ctx, a.ID, "concurrent", state.ETag, "save", ps, v)
		}(name)
	}
	wg.Wait()
	close(errs)
	okCount, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			okCount++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if okCount != 1 || conflicts != 1 {
		t.Fatal("concurrent overwrite was not rejected")
	}
	state, err = s.AdminAIConfig(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	task := adminTaskFixture(t, s, u, "busy-ai-task", 0)
	p := state.Providers[0]
	if _, err = s.Pool.Exec(ctx, `INSERT INTO task_attempts(id,task_id,attempt,provider_scope,provider_id,protocol,model,status,request_hash) VALUES('busy-ai-attempt',$1,1,'platform',$2,$3,$4,'uncertain','safe-hash')`, task.ID, p.ID, p.Protocol, p.Model); err != nil {
		t.Fatal(err)
	}
	ps := append([]Provider{}, state.Providers...)
	ps[0].Name = "unsafe-change"
	if err = s.AdminSaveAIConfig(ctx, a.ID, "busy", state.ETag, "save", ps, v); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain provider changed", err)
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "busy-noop", state.ETag, "save", state.Providers, v); err != nil {
		t.Fatal("unchanged busy provider rejected", err)
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "busy-delete", state.ETag, "save", state.Providers[1:], v); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain provider deleted", err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE task_attempts SET status='failed' WHERE id='busy-ai-attempt'"); err != nil {
		t.Fatal(err)
	}
	model := "gpt-image-2"
	if _, err = s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "config", AdminImageConfigInput{DefaultModel: &model}); err != nil {
		t.Fatal(err)
	}
	for i := range ps {
		if ps[i].Capability == "image" {
			ps[i].Model = "replacement-image"
		}
	}
	if err = s.AdminSaveAIConfig(ctx, a.ID, "model", state.ETag, "save", ps, v); !errors.Is(err, ErrConflict) {
		t.Fatal("image-config model reference stranded")
	}
}

func TestAdminAIPostgresAuditFailureRollsBackRevisionAndProviders(t *testing.T) {
	s, a, _, _ := adminFixture(t)
	ctx := context.Background()
	v := adminAIVault(t)
	state, err := s.AdminAIConfig(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `CREATE FUNCTION reject_ai_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='ai_config_save' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `CREATE TRIGGER reject_ai_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_ai_audit()`); err != nil {
		t.Fatal(err)
	}
	ps := append([]Provider{}, state.Providers...)
	ps[0].Name = "must roll back"
	if err = s.AdminSaveAIConfig(ctx, a.ID, "atomic", state.ETag, "save", ps, v); err == nil {
		t.Fatal("audit failure ignored")
	}
	after, err := s.AdminAIConfig(ctx, a.ID)
	if err != nil || after.ETag != state.ETag || after.CanRollback {
		t.Fatal("audit failure left partial provider/revision write", err)
	}
}

func TestAdminReportingPostgresBillingSettlementsAndTimezones(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO recharge_plans(id,name,credits,price_cents,currency) VALUES('report-plan','Reporting',10,100,'CNY')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO payment_channels(id,code,name) VALUES('report-channel','report','Reporting')`); err != nil {
		t.Fatal(err)
	}
	// Shanghai midnight is 16:00 UTC. Creation and settlement deliberately differ.
	for _, o := range []struct {
		id, user, status, currency, paid string
		cents                            int64
	}{
		{"paid-before", u.ID, "paid", "CNY", "2026-10-25T15:59:59Z", 1234},
		{"paid-after", u.ID, "paid", "CNY", "2026-10-25T16:00:00Z", 2501},
		{"paid-other", b.ID, "paid", "CNY", "2026-10-25T16:00:01Z", 9999},
		{"paid-usd", u.ID, "paid", "USD", "2026-10-25T16:00:02Z", 777},
		{"pending", u.ID, "pending", "CNY", "2026-10-25T16:00:03Z", 500},
		{"failed", u.ID, "failed", "CNY", "2026-10-25T16:00:04Z", 600},
	} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO recharge_orders(id,user_id,plan_id,channel_id,channel_code,channel_version,adapter,amount_cents,credits,currency,status,expires_at,paid_at,created_at,updated_at,config_ciphertext) VALUES($1,$2,'report-plan','report-channel','report',1,'mock',$3,10,$4,$5,now(),CASE WHEN $5='paid' THEN $6::timestamptz END,'2026-10-25T16:00:00Z','2026-10-27T00:00:00Z','PAYMENT_SECRET_SENTINEL')`, o.id, o.user, o.cents, o.currency, o.status, o.paid); err != nil {
			t.Fatal(err)
		}
	}
	zone := time.FixedZone("Shanghai", 8*3600)
	from := time.Date(2026, 10, 25, 0, 0, 0, 0, zone)
	until := from.AddDate(0, 0, 2)
	f := adminFilterFixture()
	f.From = &from
	f.Until = &until
	f.UserKeyword = "ALICE"
	dash, err := s.AdminDashboard(ctx, a.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if dash["billing_metrics_available"] != true || adminTestNumber(dash["pending_orders"]) != 1 || adminTestNumber(dash["paid_orders"]) != 3 || adminTestNumber(dash["revenue"]) != 37.35 || adminTestNumber(dash["other_currency_paid_orders"]) != 1 || adminTestNumber(dash["recharge_users"]) != 1 {
		t.Fatal("billing summary is not settled and filtered", dash)
	}
	trends := dash["trends"].([]map[string]any)
	if len(trends) != 2 || adminTestNumber(trends[0]["revenue"]) != 12.34 || adminTestNumber(trends[1]["revenue"]) != 25.01 || adminTestNumber(trends[1]["paid_orders"]) != 2 {
		t.Fatal("paid-at Shanghai date bucketing failed", trends)
	}
	user, err := s.AdminUser(ctx, a.ID, u.ID)
	if err != nil || adminTestNumber(user["recharge_count"]) != 3 {
		t.Fatal("user paid order count", err)
	}
	rows, err := s.AdminUsers(ctx, a.ID, AdminFilter{Page: 1, PageSize: 1, UserID: b.ID})
	if err != nil || rows.Total != 1 || adminTestNumber(rows.Rows[0]["recharge_count"]) != 1 {
		t.Fatal("paged user recharge count", err)
	}
	if _, err = s.AdminDashboard(ctx, u.ID, f); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("ordinary user received billing report")
	}
	raw, _ := json.Marshal(dash)
	if strings.Contains(string(raw), "PAYMENT_SECRET_SENTINEL") {
		t.Fatal("payment configuration leaked")
	}
}

func TestAdminTaskPostgresWhitelistedInputsOwnershipAndAudit(t *testing.T) {
	s, a, u, b := adminFixture(t)
	ctx := context.Background()
	for _, asset := range []struct{ id, user string }{{"own-ref", u.ID}, {"other-ref", b.ID}} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO assets(id,user_id,storage_key,content_type,file_size,sha256,metadata) VALUES($1,$2,$1,'image/png',1,$3,'{"secret":"ASSET_SECRET"}')`, asset.id, asset.user, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	task, _, err := s.SubmitTask(ctx, SubmitTask{UserID: u.ID, Capability: "image", Kind: "main", IdempotencyKey: "whitelist-task", Request: json.RawMessage(`{"version":1,"authorization":"AUTH_SECRET","image":{"prompt":"Visible submitted prompt","size":"1024x1024","reference_asset_ids":["own-ref","other-ref"],"api_key":"KEY_SECRET","options":{"secret":"OPTION_SECRET"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE tasks SET result_json='{"raw":"UPSTREAM_SECRET"}' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []User{u, b} {
		if _, err = s.AdminTaskDetail(ctx, actor.ID, task.ID, "image"); !errors.Is(err, ErrAdminForbidden) {
			t.Fatal("non-admin inspected inputs")
		}
	}
	detail, err := s.AdminTaskDetail(ctx, a.ID, task.ID, "image", "detail-request")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(detail)
	if !strings.Contains(string(raw), "Visible submitted prompt") || !strings.Contains(string(raw), "own-ref") || strings.Contains(string(raw), "other-ref") || strings.Contains(string(raw), "SECRET") {
		t.Fatal("task input projection invalid")
	}
	var count int
	var metadata string
	if err = s.Pool.QueryRow(ctx, `SELECT count(*),max(metadata::text) FROM audit_events WHERE action='task_detail_view' AND target_id=$1 AND actor_id=$2 AND request_id='detail-request'`, task.ID, a.ID).Scan(&count, &metadata); err != nil || count != 1 || strings.Contains(metadata, "Visible submitted") {
		t.Fatal("view audit missing or contains prompt", err)
	}
	if _, err = s.AdminTaskDetail(ctx, a.ID, task.ID, "video"); !errors.Is(err, ErrNotFound) {
		t.Fatal("capability filter ignored")
	}
}

func TestAdminImageConfigPostgresModelAndPriceValidation(t *testing.T) {
	s, a, u, _ := adminFixture(t)
	ctx := context.Background()
	for _, model := range []string{"not-configured", "local-cliproxy-image", "gpt-5.5"} {
		if _, err := s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "validation", AdminImageConfigInput{DefaultModel: &model}); !errors.Is(err, ErrConflict) {
			t.Fatal("non-image model accepted", model)
		}
	}
	model := "gpt-image-2"
	zero := int64(0)
	c, err := s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "valid", AdminImageConfigInput{DefaultModel: &model, FallbackModel: &model, CreditsCost: &zero})
	if err != nil || c.CreditsCost != 0 || c.DefaultModel != model {
		t.Fatal("configured model or explicit free quote rejected", err)
	}
	for _, price := range []int64{-1, 1000001} {
		if _, err = s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "price", AdminImageConfigInput{CreditsCost: &price}); !errors.Is(err, ErrConflict) {
			t.Fatal("invalid quote accepted")
		}
	}
	empty := ""
	if _, err = s.AdminSaveImageConfig(ctx, a.ID, "ic_main", "inherit", AdminImageConfigInput{DefaultModel: &empty, FallbackModel: &empty}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AdminSaveImageConfig(ctx, u.ID, "ic_main", "forbidden", AdminImageConfigInput{CreditsCost: &zero}); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("user edited image configuration")
	}
}

func TestAdminTaskSubmittedTextWhitelist(t *testing.T) {
	detail, _ := adminTaskInputs([]byte(`{"text":{"model":"fixture","messages":[{"role":"system","content":"SYSTEM_SECRET"},{"role":"user","content":[{"type":"input_text","text":"User words"},{"type":"input_image","image_url":"SIGNED_SECRET"}]},{"role":"tool","content":"TOOL_SECRET"}],"options":{"api_key":"OPTION_SECRET"}}}`), "text")
	raw, _ := json.Marshal(detail)
	if !strings.Contains(string(raw), "User words") || strings.Contains(string(raw), "SECRET") {
		t.Fatal("text input whitelist invalid")
	}
}
