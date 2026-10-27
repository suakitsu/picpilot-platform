package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
)

func TestPostgresGitHubOAuthIsBrowserBoundAndSingleUse(t *testing.T) {
	s := testServer(t, config.Platform)
	initialize(t, s)
	s.Config.GitHubClientID = "fixture-client"
	s.Config.GitHubClientSecret = "fixture-client-secret"
	s.Config.PublicBaseURL = "https://signin.example.invalid"
	s.Config.AllowedOrigins = append(s.Config.AllowedOrigins, s.Config.PublicBaseURL)
	s.Config.RegistrationEnabled = true
	start := httptest.NewRecorder()
	s.Handler().ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/github?next=%2Fcommerce%3Ftab%3Dsuite", nil))
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil || start.Code != 302 || location.Host != "github.com" {
		t.Fatal("OAuth authorization redirect missing")
	}
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("OAuth cookie security")
	}
	digest := sha256.Sum256([]byte(cookies[0].Value))
	if location.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE binding missing")
	}
	var calls int
	s.IdentityHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("code") != "fixture-code" || r.Form.Get("code_verifier") != cookies[0].Value {
				t.Fatal("OAuth exchange binding")
			}
			body = `{"access_token":"fixture-oauth-access","token_type":"bearer"}`
		case "api.github.com/user":
			body = `{"id":90101,"login":"fixture","name":"Fixture"}`
		case "api.github.com/user/emails":
			body = `[{"email":"github-member@example.invalid","verified":true,"primary":true}]`
		default:
			t.Fatal("unexpected OAuth endpoint")
		}
		if r.URL.Host == "api.github.com" && r.Header.Get("Authorization") != "Bearer fixture-oauth-access" {
			t.Fatal("identity access token missing")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	callback := "/api/auth/github/callback?state=" + url.QueryEscape(location.Query().Get("state")) + "&code=fixture-code"
	bad := httptest.NewRecorder()
	s.Handler().ServeHTTP(bad, httptest.NewRequest("GET", callback, nil))
	if bad.Code != 303 || calls != 0 {
		t.Fatal("unbound browser exchanged a token")
	}
	r := httptest.NewRequest("GET", callback, nil)
	r.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 303 || calls != 3 || !strings.Contains(w.Header().Get("Location"), "/auth/github/callback?next=") {
		t.Fatal("OAuth callback failed")
	}
	if strings.Contains(w.Header().Get("Location"), "token=") || strings.Contains(w.Header().Get("Location"), "profile=") {
		t.Fatal("credentials entered redirect URL")
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "picpilot_session" {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure {
		t.Fatal("session cookie missing")
	}
	me := httptest.NewRequest("GET", "/api/me", nil)
	me.AddCookie(session)
	meResult := httptest.NewRecorder()
	s.Handler().ServeHTTP(meResult, me)
	if meResult.Code != 200 {
		t.Fatal("cookie sign-in failed")
	}
	u, err := s.Store.UserByEmail(context.Background(), "github-member@example.invalid")
	if err != nil || u.Role != "user" {
		t.Fatal("OAuth privilege mismatch")
	}
	replay := httptest.NewRecorder()
	r = httptest.NewRequest("GET", callback, nil)
	r.AddCookie(cookies[0])
	s.Handler().ServeHTTP(replay, r)
	if replay.Code != 303 || !strings.Contains(replay.Header().Get("Location"), "GITHUB_SIGN_IN_FAILED") || calls != 3 {
		t.Fatal("OAuth state replay accepted")
	}
}

func TestPostgresGitHubCannotAutoLinkEmailOrUseUnverifiedEmail(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		verified    bool
	}{{"existing", "owner@example.invalid", true}, {"unverified", "new@example.invalid", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t, config.Platform)
			initialize(t, s)
			s.Config.GitHubClientID = "fixture-client"
			s.Config.GitHubClientSecret = "fixture-secret"
			s.Config.RegistrationEnabled = true
			start := httptest.NewRecorder()
			s.Handler().ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/github", nil))
			u, _ := url.Parse(start.Header().Get("Location"))
			s.IdentityHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"fixture-oauth-access","token_type":"bearer"}`
				if r.URL.Path == "/user" {
					body = `{"id":90909,"login":"fixture"}`
				}
				if r.URL.Path == "/user/emails" {
					body = fmt.Sprintf(`[{"email":%q,"verified":%t,"primary":true}]`, tc.email, tc.verified)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			r := httptest.NewRequest("GET", "/api/auth/github/callback?state="+u.Query().Get("state")+"&code=fixture", nil)
			r.AddCookie(start.Result().Cookies()[0])
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if !strings.Contains(w.Header().Get("Location"), "GITHUB_SIGN_IN_FAILED") {
				t.Fatal("unsafe OAuth link accepted")
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "picpilot_session" {
					t.Fatal("unsafe OAuth session issued")
				}
			}
			var count int
			if err := s.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM oauth_identities`).Scan(&count); err != nil || count != 0 {
				t.Fatal("unsafe OAuth identity persisted")
			}
		})
	}
}
