package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func testServer(t *testing.T, edition config.Edition) *Server {
	t.Helper()
	db := &store.Store{Pool: testpg.Open(t), Edition: edition}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := config.Config{Edition: edition, PublicBaseURL: "http://localhost:8080", AllowedOrigins: []string{"http://localhost:8080"}, AuthKey: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), WorkerPepper: bytes.Repeat([]byte{3}, 32), WorkerSigningKey: bytes.Repeat([]byte{4}, 32), SetupToken: "automated-test-only-setup-token-123456", SessionTTL: time.Hour}
	c.DataDir = t.TempDir()
	s, err := New(c, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Files.Close() })
	return s
}

func request(t *testing.T, s *Server, method, path, token string, body any) (int, map[string]any, string) {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal("invalid JSON response")
	}
	return w.Code, result, w.Body.String()
}

func initialize(t *testing.T, s *Server) string {
	t.Helper()
	status, result, _ := request(t, s, "POST", "/api/setup", "", map[string]any{"email": "owner@example.invalid", "password": "fixture-only-long-password", "confirm_password": "fixture-only-long-password", "setup_token": s.Config.SetupToken})
	if status != 200 {
		t.Fatal("setup failed", result["code"])
	}
	return result["data"].(map[string]any)["access_token"].(string)
}

func TestPostgresHTTPSetupAndAuthIsolation(t *testing.T) {
	s := testServer(t, config.Personal)
	status, result, _ := request(t, s, "GET", "/api/public/config", "", nil)
	if status != 200 || result["data"].(map[string]any)["billing_enabled"] != false {
		t.Fatal("personal config incorrect")
	}
	status, _, _ = request(t, s, "POST", "/api/setup", "", map[string]any{"email": "owner@example.invalid", "password": "fixture-only-long-password", "confirm_password": "fixture-only-long-password", "setup_token": "wrong"})
	if status != 403 {
		t.Fatal("setup token not checked")
	}
	token := initialize(t, s)
	status, _, _ = request(t, s, "GET", "/api/me", token, nil)
	if status != 200 {
		t.Fatal("owner session rejected")
	}
	status, _, _ = request(t, s, "GET", "/api/admin/providers", token, nil)
	if status != 403 {
		t.Fatal("personal access to platform admin")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/logout", token, nil)
	if status != 200 {
		t.Fatal("logout failed")
	}
	status, _, _ = request(t, s, "GET", "/api/me", token, nil)
	if status != 401 {
		t.Fatal("logout did not revoke token")
	}
}

func TestPostgresHTTPPersonalProviderSecretAndOwnership(t *testing.T) {
	s := testServer(t, config.Personal)
	token := initialize(t, s)
	input := map[string]any{"name": "Personal mock", "capability": "image", "protocol": "openai_images_generations", "base_url": "https://example.invalid/v1", "api_key": "fixture-key-never-a-real-secret", "model": "gpt-image-2", "enabled": true, "priority": 0, "max_concurrency": 2, "timeout_seconds": 300}
	status, _, body := request(t, s, "PUT", "/api/me/providers/mine", token, input)
	if status != 200 {
		t.Fatal("provider configuration failed", body)
	}
	if strings.Contains(body, "fixture-key") || strings.Contains(body, "secret_ciphertext") {
		t.Fatal("credential leaked in response")
	}
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Store.Provider(context.Background(), u.ID, "mine")
	if err != nil || p.SecretCiphertext == "" || p.SecretCiphertext == input["api_key"] {
		t.Fatal("credential not encrypted")
	}
	if _, err := s.Vault.Decrypt("another-user/mine", p.SecretCiphertext); err == nil {
		t.Fatal("credential accepted under a different owner")
	}
	input["api_key"] = ""
	status, _, _ = request(t, s, "PUT", "/api/me/providers/mine", token, input)
	if status != 200 {
		t.Fatal("empty key update rejected")
	}
	after, _ := s.Store.Provider(context.Background(), u.ID, "mine")
	if after.SecretCiphertext != p.SecretCiphertext {
		t.Fatal("blank key erased saved key")
	}
	status, reply, _ := request(t, s, "POST", "/api/me/providers/mine/check", token, map[string]any{})
	if status != 200 || reply["data"].(map[string]any)["model_called"] != false {
		t.Fatal("configuration check made model call")
	}
}

func TestPostgresHTTPRejectsCSRFCrossOriginAndUnknownFields(t *testing.T) {
	s := testServer(t, config.Personal)
	token := initialize(t, s)
	r := httptest.NewRequest("PATCH", "/api/me", strings.NewReader(`{"nickname":"attack"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "picpilot_session", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cookie mutation without Origin accepted")
	}
	r = httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Origin", "https://attacker.invalid")
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
	status, _, _ := request(t, s, "PATCH", "/api/me", token, map[string]any{"nickname": "Owner", "role": "super_admin", "credits": 10000})
	if status != 400 {
		t.Fatal("unknown privileged fields accepted")
	}
}

func TestPostgresHTTPPasswordChangeRequiresNewLogin(t *testing.T) {
	s := testServer(t, config.Platform)
	token := initialize(t, s)
	status, reply, _ := request(t, s, "POST", "/api/auth/change-password", token, map[string]any{"current_password": "fixture-only-long-password", "new_password": "fixture-only-new-password", "confirm_password": "fixture-only-new-password"})
	if status != 200 || reply["data"].(map[string]any)["requires_login"] != true {
		t.Fatal("password change failed")
	}
	status, _, _ = request(t, s, "GET", "/api/me", token, nil)
	if status != 401 {
		t.Fatal("old password session survived")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/login", "", map[string]any{"email": "owner@example.invalid", "password": "fixture-only-new-password"})
	if status != 200 {
		t.Fatal("new password cannot log in")
	}
}
