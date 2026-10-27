package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
)

type recordedMail struct{ Email, Purpose, Code string }
type mockMailer struct {
	mu       sync.Mutex
	messages []recordedMail
	fail     bool
}

func (m *mockMailer) SendCode(_ context.Context, email, purpose, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("mock delivery unavailable")
	}
	m.messages = append(m.messages, recordedMail{email, purpose, code})
	return nil
}
func (m *mockMailer) last() recordedMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[len(m.messages)-1]
}

func TestPostgresHTTPEmailResetRevokesSessionAndRedirects(t *testing.T) {
	s := testServer(t, config.Personal)
	token := initialize(t, s)
	m := &mockMailer{}
	s.Mailer = m
	status, reply, body := request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "owner@example.invalid", "purpose": "reset_password"})
	if status != 200 || reply["data"].(map[string]any)["sent"] != true {
		t.Fatal("email request failed", reply["code"])
	}
	message := m.last()
	if strings.Contains(body, message.Code) {
		t.Fatal("verification code leaked to browser")
	}
	var saved string
	if err := s.Store.Pool.QueryRow(context.Background(), `SELECT token_hash FROM auth_challenges ORDER BY created_at DESC LIMIT 1`).Scan(&saved); err != nil || saved == message.Code || len(saved) != 64 {
		t.Fatal("verification code not hashed")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "owner@example.invalid", "purpose": "login"})
	if status != 429 {
		t.Fatal("email cooldown is not shared across purposes")
	}
	input := map[string]any{"email": "owner@example.invalid", "verification_code": message.Code, "password": "new-fixture-password-123", "confirm_password": "new-fixture-password-123"}
	status, reply, _ = request(t, s, "POST", "/api/auth/password-reset", "", input)
	if status != 200 || reply["data"].(map[string]any)["redirect_to"] != "/login" || reply["data"].(map[string]any)["requires_login"] != true {
		t.Fatal("reset did not require login")
	}
	status, _, _ = request(t, s, "GET", "/api/me", token, nil)
	if status != 401 {
		t.Fatal("old session survived reset")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/password-reset", "", input)
	if status != 400 {
		t.Fatal("reset code accepted twice")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/login", "", map[string]any{"email": "owner@example.invalid", "password": "new-fixture-password-123"})
	if status != 200 {
		t.Fatal("new password login failed")
	}
}

func TestPostgresHTTPRegistrationAndEmailLogin(t *testing.T) {
	s := testServer(t, config.Platform)
	initialize(t, s)
	m := &mockMailer{}
	s.Mailer = m
	status, _, _ := request(t, s, "POST", "/api/auth/register", "", map[string]any{})
	if status != 403 {
		t.Fatal("registration enabled implicitly")
	}
	s.Config.RegistrationEnabled = true
	status, _, _ = request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "member@example.invalid", "purpose": "register"})
	if status != 200 {
		t.Fatal("registration email failed")
	}
	status, reply, _ := request(t, s, "POST", "/api/auth/register", "", map[string]any{"email": "member@example.invalid", "password": "fixture-user-password", "confirm_password": "fixture-user-password", "verification_code": m.last().Code})
	if status != 200 {
		t.Fatal("verified registration failed", reply["code"])
	}
	token := reply["data"].(map[string]any)["access_token"].(string)
	status, _, _ = request(t, s, "GET", "/api/admin/providers", token, nil)
	if status != 403 {
		t.Fatal("registered user got admin access")
	}
	if _, err := s.Store.Pool.Exec(context.Background(), `UPDATE auth_challenges SET created_at=clock_timestamp()-interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	status, _, _ = request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "member@example.invalid", "purpose": "login"})
	if status != 200 {
		t.Fatal("login email failed")
	}
	status, _, _ = request(t, s, "POST", "/api/auth/login/email-code", "", map[string]any{"email": "member@example.invalid", "verification_code": m.last().Code})
	if status != 200 {
		t.Fatal("verified email login failed")
	}
}

func TestPostgresHTTPEmailUnavailableAndPersonalSignup(t *testing.T) {
	s := testServer(t, config.Personal)
	initialize(t, s)
	status, reply, _ := request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "owner@example.invalid", "purpose": "reset_password"})
	if status != 503 || reply["code"] != "EMAIL_NOT_CONFIGURED" {
		t.Fatal("unconfigured mail pretended to succeed")
	}
	s.Mailer = &mockMailer{fail: true}
	status, reply, _ = request(t, s, "POST", "/api/auth/email-code", "", map[string]any{"email": "owner@example.invalid", "purpose": "reset_password"})
	if status != 503 || reply["code"] != "EMAIL_DELIVERY_UNAVAILABLE" {
		t.Fatal("mail failure pretended to succeed")
	}
	var active int
	if err := s.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auth_challenges WHERE consumed_at IS NULL`).Scan(&active); err != nil || active != 0 {
		t.Fatal("undelivered code left active")
	}
	s.Config.RegistrationEnabled = true
	status, _, _ = request(t, s, "POST", "/api/auth/register", "", map[string]any{})
	if status != 403 {
		t.Fatal("personal edition signup enabled")
	}
}

func TestSafeNextPath(t *testing.T) {
	for _, input := range []string{"https://evil.invalid/", "//evil.invalid/", "/\\evil.invalid/", "/%2f%2fevil.invalid/", "/%5cevil.invalid/", "/\r\nLocation: evil"} {
		if safeNextPath(input) != "/workspace" {
			t.Errorf("unsafe redirect accepted: %q", input)
		}
	}
	if safeNextPath("/commerce?tab=suite") != "/commerce?tab=suite" {
		t.Fatal("normal next path rejected")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPostgresHTTPCaptchaFailsClosed(t *testing.T) {
	s := testServer(t, config.Personal)
	initialize(t, s)
	s.Config.TurnstileSecret = "mock-only-secret"
	s.IdentityHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "challenges.cloudflare.com" {
			t.Fatal("unexpected network endpoint")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})}
	status, reply, _ := request(t, s, "POST", "/api/auth/login", "", map[string]any{"email": "owner@example.invalid", "password": "fixture-only-long-password"})
	if status != 400 || reply["code"] != "CAPTCHA_REQUIRED" {
		t.Fatal("captcha bypassed")
	}
	status, reply, _ = request(t, s, "POST", "/api/auth/login", "", map[string]any{"email": "owner@example.invalid", "password": "fixture-only-long-password", "turnstile_token": "mock-challenge"})
	if status != 503 || reply["code"] != "CAPTCHA_UNAVAILABLE" {
		t.Fatal("malformed captcha response accepted")
	}
	r := httptest.NewRequest("GET", "/api/auth/github/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("personal GitHub enabled")
	}
}
