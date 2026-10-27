package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func (s *Server) registerIdentityRoutes() {
	s.mux.HandleFunc("GET /api/auth/github/status", func(w http.ResponseWriter, r *http.Request) { ok(w, r, map[string]bool{"enabled": s.githubEnabled()}) })
	s.mux.HandleFunc("GET /api/auth/github", s.githubStart)
	s.mux.HandleFunc("GET /api/auth/github/callback", s.githubCallback)
	s.mux.HandleFunc("POST /api/auth/session", s.auth(func(w http.ResponseWriter, r *http.Request) { s.sessionResponse(w, r, currentUser(r)) }))
}

func (s *Server) verifyCaptcha(w http.ResponseWriter, r *http.Request, token string) bool {
	if s.Config.TurnstileSecret == "" {
		return true
	}
	if len(token) == 0 || len(token) > 2048 {
		fail(w, r, 400, "CAPTCHA_REQUIRED", "Complete the verification challenge")
		return false
	}
	form := url.Values{"secret": {s.Config.TurnstileSecret}, "response": {token}, "remoteip": {s.clientAddress(r)}}
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
	}
	if err = s.identityJSON(req, &result); err != nil {
		fail(w, r, 503, "CAPTCHA_UNAVAILABLE", "Verification service is temporarily unavailable")
		return false
	}
	if !result.Success || !s.captchaHostnameAllowed(result.Hostname) {
		fail(w, r, 400, "CAPTCHA_INVALID", "Verification failed; try again")
		return false
	}
	return true
}

func (s *Server) captchaHostnameAllowed(hostname string) bool {
	if hostname == "" {
		return false
	}
	origins := append([]string{s.Config.PublicBaseURL}, s.Config.AllowedOrigins...)
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(hostname, parsed.Hostname()) {
			return true
		}
	}
	return false
}

func (s *Server) githubEnabled() bool {
	return s.Config.Edition == config.Platform && s.Config.GitHubClientID != "" && s.Config.GitHubClientSecret != ""
}

func safeNextPath(raw string) string {
	if len(raw) > 2048 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n\t") {
		return "/workspace"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n") {
		return "/workspace"
	}
	return raw
}

func (s *Server) githubStart(w http.ResponseWriter, r *http.Request) {
	if !s.githubEnabled() {
		fail(w, r, 404, "OAUTH_DISABLED", "GitHub sign-in is not configured")
		return
	}
	if !s.authRate(w, r) {
		return
	}
	state, err := security.RandomToken("", 32)
	if err != nil {
		fail(w, r, 500, "INTERNAL_ERROR", "Could not initialize sign-in")
		return
	}
	verifier, err := security.RandomToken("", 32)
	if err != nil {
		fail(w, r, 500, "INTERNAL_ERROR", "Could not initialize sign-in")
		return
	}
	err = s.Store.CreateOAuthState(r.Context(), security.TokenHash(s.Config.AuthKey, "oauth-state:"+state), security.TokenHash(s.Config.AuthKey, "oauth-browser:"+verifier), safeNextPath(r.URL.Query().Get("next")))
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_oauth", Value: verifier, Path: "/api/auth/github/callback", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {s.Config.GitHubClientID}, "redirect_uri": {s.Config.PublicBaseURL + "/api/auth/github/callback"}, "scope": {"read:user user:email"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	failure := func() {
		http.Redirect(w, r, s.Config.PublicBaseURL+"/login?error=GITHUB_SIGN_IN_FAILED", http.StatusSeeOther)
	}
	if !s.githubEnabled() || !s.authRate(w, r) {
		if !s.githubEnabled() {
			failure()
		}
		return
	}
	cookie, err := r.Cookie("picpilot_oauth")
	http.SetCookie(w, &http.Cookie{Name: "picpilot_oauth", Value: "", Path: "/api/auth/github/callback", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	if err != nil || len(cookie.Value) != 43 || len(state) != 43 || code == "" || len(code) > 1024 || r.URL.Query().Get("error") != "" {
		failure()
		return
	}
	next, err := s.Store.ConsumeOAuthState(r.Context(), security.TokenHash(s.Config.AuthKey, "oauth-state:"+state), security.TokenHash(s.Config.AuthKey, "oauth-browser:"+cookie.Value))
	if err != nil {
		failure()
		return
	}
	form := url.Values{"client_id": {s.Config.GitHubClientID}, "client_secret": {s.Config.GitHubClientSecret}, "code": {code}, "redirect_uri": {s.Config.PublicBaseURL + "/api/auth/github/callback"}, "code_verifier": {cookie.Value}}
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		failure()
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if s.identityJSON(req, &token) != nil || token.Error != "" || token.AccessToken == "" || len(token.AccessToken) > 4096 || !strings.EqualFold(token.TokenType, "bearer") {
		failure()
		return
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if s.githubGet(r, "/user", token.AccessToken, &user) != nil || user.ID <= 0 {
		failure()
		return
	}
	var emails []struct {
		Email    string `json:"email"`
		Verified bool   `json:"verified"`
		Primary  bool   `json:"primary"`
	}
	if s.githubGet(r, "/user/emails?per_page=100", token.AccessToken, &emails) != nil {
		failure()
		return
	}
	email := ""
	for _, e := range emails {
		if e.Primary && e.Verified && validEmail(e.Email) {
			email = strings.ToLower(e.Email)
			break
		}
	}
	if email == "" {
		failure()
		return
	}
	randomPassword, err := security.RandomToken("", 32)
	if err != nil {
		failure()
		return
	}
	hash, err := security.HashPassword(randomPassword)
	if err != nil {
		failure()
		return
	}
	nickname := user.Name
	if nickname == "" {
		nickname = user.Login
	}
	if len(nickname) > 160 {
		nickname = "GitHub user"
	}
	u, err := s.Store.GitHubLogin(r.Context(), strconv.FormatInt(user.ID, 10), email, nickname, hash, s.Config.RegistrationEnabled)
	if err != nil {
		failure()
		return
	}
	session, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), s.Config.SessionTTL)
	if err != nil {
		failure()
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_session", Value: session, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: int(s.Config.SessionTTL / time.Second)})
	// Session tokens and profiles must not enter URLs, browser history or referrers.
	http.Redirect(w, r, s.Config.PublicBaseURL+"/auth/github/callback?next="+url.QueryEscape(safeNextPath(next)), http.StatusSeeOther)
}

func (s *Server) githubGet(r *http.Request, path, token string, result any) error {
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "PicPilot")
	return s.identityJSON(req, result)
}

func (s *Server) identityJSON(req *http.Request, dst any) error {
	response, err := s.IdentityHTTP.Do(req)
	if err != nil {
		return errors.New("identity service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("identity service rejected request")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 || json.Unmarshal(data, dst) != nil {
		return errors.New("identity response invalid")
	}
	return nil
}
