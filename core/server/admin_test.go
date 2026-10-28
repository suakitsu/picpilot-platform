package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func adminHTTPFixture(t *testing.T, edition config.Edition) (*Server, string) {
	t.Helper()
	s := testServer(t, edition)
	// These tests work both before and after the parent wires the registrations.
	_, pattern := s.mux.Handler(httptest.NewRequest("GET", "/api/admin/users", nil))
	if pattern != "GET /api/admin/users" {
		s.registerAdminRoutes()
	}
	_, pattern = s.mux.Handler(httptest.NewRequest("GET", "/api/messages", nil))
	if pattern != "GET /api/messages" {
		s.registerMessageRoutes()
	}
	return s, initialize(t, s)
}
func adminHTTPUser(t *testing.T, s *Server, email string) (store.User, string) {
	t.Helper()
	u, err := s.Store.CreateUser(context.Background(), email, "PRIVATE_PASSWORD_HASH", "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestAdminHTTPRoleGuardsAndQueryValidation(t *testing.T) {
	s, admin := adminHTTPFixture(t, config.Platform)
	_, user := adminHTTPUser(t, s, "user@example.invalid")
	for _, path := range []string{"/api/admin/dashboard", "/api/admin/dashboard/trends", "/api/admin/users", "/api/admin/ledger", "/api/admin/risk-events", "/api/admin/risk-keywords", "/api/admin/image-tasks", "/api/admin/tasks", "/api/admin/audit-logs", "/api/admin/costs", "/api/admin/text-calls", "/api/admin/messages", "/api/admin/prompts", "/api/admin/image-configs"} {
		status, _, _ := request(t, s, "GET", path, "", nil)
		if status != 401 {
			t.Fatalf("anonymous %s: %d", path, status)
		}
		status, _, _ = request(t, s, "GET", path, user, nil)
		if status != 403 {
			t.Fatalf("user %s: %d", path, status)
		}
		status, _, _ = request(t, s, "GET", path, admin, nil)
		if status != 200 {
			t.Fatalf("admin %s: %d", path, status)
		}
	}
	for _, query := range []string{"page=0", "page=-1", "page=1000001", "page=not-a-number", "page_size=101", "page_size=0", "page=1&page=2", "page_size=20&pageSize=10", "from=invalid", "from=2026-10-26&to=2026-10-25"} {
		status, _, _ := request(t, s, "GET", "/api/admin/users?"+query, admin, nil)
		if status != 400 {
			t.Fatalf("invalid filter %s accepted: %d", query, status)
		}
	}
	status, result, body := request(t, s, "GET", "/api/admin/users?keyword=user%40example.invalid&page_size=1", admin, nil)
	if status != 200 || result["data"].(map[string]any)["total"] != float64(1) || strings.Contains(body, "PRIVATE_PASSWORD_HASH") || strings.Contains(body, "password_hash") {
		t.Fatal("user projection or filtering failed")
	}
}

func TestAdminHTTPCreditsStatusPasswordsAndPrivacy(t *testing.T) {
	s, admin := adminHTTPFixture(t, config.Platform)
	u, userToken := adminHTTPUser(t, s, "user@example.invalid")
	ctx := context.Background()
	path := "/api/admin/users/" + u.ID + "/credits"
	body := map[string]any{"change_amount": 20, "request_token": "http-credit-key", "remark": "support"}
	if status, _, _ := request(t, s, "POST", path, userToken, body); status != 403 {
		t.Fatal("user credit mutation allowed")
	}
	for i := 0; i < 2; i++ {
		if status, _, _ := request(t, s, "POST", path, admin, body); status != 200 {
			t.Fatal("credit mutation failed")
		}
	}
	after, _ := s.Store.UserByID(ctx, u.ID)
	if after.Credits != 20 {
		t.Fatal("duplicate HTTP credit charge")
	}
	body["change_amount"] = 21
	if status, _, _ := request(t, s, "POST", path, admin, body); status != 409 {
		t.Fatal("HTTP idempotency conflict missed")
	}
	a, _ := s.Store.UserByEmail(ctx, "owner@example.invalid")
	if status, _, _ := request(t, s, "PATCH", "/api/admin/users/"+a.ID+"/status", admin, map[string]any{"status": "disabled"}); status != 409 {
		t.Fatal("last admin protection bypassed")
	}
	if status, _, _ := request(t, s, "POST", "/api/admin/users/"+u.ID+"/password", admin, map[string]any{"password": "short"}); status != 400 {
		t.Fatal("short password accepted")
	}
	status, _, reply := request(t, s, "POST", "/api/admin/users/"+u.ID+"/password", admin, map[string]any{"password": "PRIVATE_RESET_PASSWORD"})
	if status != 200 || strings.Contains(reply, "PRIVATE_RESET") {
		t.Fatal("reset response unsafe")
	}
	if status, _, _ := request(t, s, "GET", "/api/messages", userToken, nil); status != 401 {
		t.Fatal("reset did not revoke token")
	}
	if err := s.Store.Audit(ctx, a.ID, "fixture", "user", u.ID, "req", map[string]any{"prompt": "PRIVATE_PROMPT", "authorization": "PRIVATE_AUTH", "api_key": "PRIVATE_KEY"}); err != nil {
		t.Fatal(err)
	}
	_, _, reply = request(t, s, "GET", "/api/admin/audit-logs", admin, nil)
	if strings.Contains(reply, "PRIVATE_") {
		t.Fatal("audit response leaked secrets")
	}
}

func TestAdminHTTPMessagesOwnershipAndLibrary(t *testing.T) {
	s, admin := adminHTTPFixture(t, config.Platform)
	alice, aliceToken := adminHTTPUser(t, s, "alice@example.invalid")
	_, bobToken := adminHTTPUser(t, s, "bob@example.invalid")
	status, result, _ := request(t, s, "POST", "/api/admin/messages", admin, map[string]any{"title": "For Alice", "content": "Message body", "audience": "targeted", "recipient_id": alice.ID})
	if status != 200 {
		t.Fatal("send failed")
	}
	id := result["data"].(map[string]any)["id"].(string)
	status, result, _ = request(t, s, "GET", "/api/messages", bobToken, nil)
	if status != 200 || len(result["data"].([]any)) != 0 {
		t.Fatal("message leaked to Bob")
	}
	status, _, _ = request(t, s, "PATCH", "/api/messages/"+id+"/read", bobToken, nil)
	if status != 404 {
		t.Fatal("Bob can mark Alice's message")
	}
	status, _, _ = request(t, s, "PATCH", "/api/messages/"+id+"/read", aliceToken, nil)
	if status != 200 {
		t.Fatal("mark read failed")
	}
	status, result, _ = request(t, s, "GET", "/api/messages/unread-count", aliceToken, nil)
	if status != 200 || result["data"].(map[string]any)["unread_count"] != float64(0) {
		t.Fatal("unread count failed")
	}
	if status, _, _ := request(t, s, "PATCH", "/api/messages/"+id+"/unread", aliceToken, nil); status != 200 {
		t.Fatal("mark unread failed")
	}
	status, result, _ = request(t, s, "GET", "/api/messages?page_size=1&status=unread", aliceToken, nil)
	if status != 200 || result["data"].(map[string]any)["total"] != float64(1) {
		t.Fatal("paged message history failed")
	}
	status, result, _ = request(t, s, "POST", "/api/admin/prompts", admin, map[string]any{"name": "Fixture", "scene": "listing", "content": "PRIVATE_TEMPLATE"})
	if status != 200 {
		t.Fatal("template create failed")
	}
	id = result["data"].(map[string]any)["id"].(string)
	status, result, _ = request(t, s, "POST", "/api/admin/prompts/"+id, admin, map[string]any{"content": "Revised", "version": 1})
	if status != 200 || result["data"].(map[string]any)["version"] != float64(2) {
		t.Fatal("template update failed")
	}
	if status, _, _ := request(t, s, "POST", "/api/admin/image-configs/ic_main", admin, map[string]any{"default_size": "invalid"}); status != 409 {
		t.Fatal("invalid image config accepted")
	}
	if status, _, _ := request(t, s, "POST", "/api/admin/image-configs/ic_main", admin, map[string]any{"credits_cost": 12, "required_fields": "[\"product_name\"]"}); status != 200 {
		t.Fatal("image config update failed")
	}
}

func TestAdminHTTPPersonalNoAdminOrBillingDependencies(t *testing.T) {
	s, token := adminHTTPFixture(t, config.Personal)
	for _, path := range []string{"/api/admin/users", "/api/admin/costs", "/api/admin/prompts", "/api/admin/risk-keywords"} {
		if status, _, _ := request(t, s, "GET", path, token, nil); status != 404 {
			t.Fatal("personal admin route is present", path)
		}
	}
	// Optional personal message history must not query platform tables.
	if _, err := s.Store.Pool.Exec(context.Background(), "DROP TABLE admin_messages CASCADE"); err != nil {
		t.Fatal(err)
	}
	status, result, _ := request(t, s, "GET", "/api/messages", token, nil)
	if status != 200 {
		t.Fatal("personal messages require admin tables")
	}
	raw, _ := json.Marshal(result["data"])
	if string(raw) != "[]" {
		t.Fatal("personal history not empty")
	}
	if status, _, _ := request(t, s, "PATCH", "/api/messages/missing/read", token, nil); status != 404 {
		t.Fatal("personal missing message falsely marked read")
	}
}
