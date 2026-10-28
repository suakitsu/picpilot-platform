package server

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func validEmail(value string) bool {
	a, err := mail.ParseAddress(value)
	return err == nil && a.Address == value && len(value) <= 254 && strings.Contains(value, "@") && !strings.ContainsAny(value, "\r\n")
}

func profile(u store.User) map[string]any {
	name, domain, _ := strings.Cut(u.Email, "@")
	prefix := "*"
	if len(name) > 0 {
		r, _ := utf8.DecodeRuneInString(name)
		prefix = string(r)
	}
	return map[string]any{"id": u.ID, "email_masked": prefix + "***@" + domain, "email_display": u.Email, "nickname": u.Nickname, "role": u.Role, "credits": u.Credits, "ui_language": u.UILanguage, "theme": u.Theme, "avatar_url": ""}
}

func (s *Server) userProfile(ctx context.Context, u store.User) map[string]any {
	result := profile(u)
	if u.AvatarAssetID != "" {
		if a, err := s.Store.OwnedAsset(ctx, u.ID, u.AvatarAssetID); err == nil {
			result["avatar_url"] = s.assetView(a)["url"]
		}
	}
	return result
}

func (s *Server) sessionResponse(w http.ResponseWriter, r *http.Request, u store.User) {
	s.sessionResponseTTL(w, r, u, s.Config.SessionTTL)
}

func (s *Server) sessionResponseTTL(w http.ResponseWriter, r *http.Request, u store.User, ttl time.Duration) {
	token, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), ttl)
	if err != nil {
		fail(w, r, 500, "INTERNAL_ERROR", "Could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl / time.Second)})
	ok(w, r, map[string]any{"user": s.userProfile(r.Context(), u), "access_token": token})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
		SetupToken      string `json:"setup_token"`
		Nickname        string `json:"nickname,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !security.ConstantEqual(in.SetupToken, s.Config.SetupToken) {
		fail(w, r, 403, "SETUP_TOKEN_INVALID", "Invalid setup token")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) || in.Password != in.ConfirmPassword || len(in.Nickname) > 160 {
		fail(w, r, 400, "INVALID_REQUEST", "Check your email and password confirmation")
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		fail(w, r, 400, "PASSWORD_INVALID", "Password must contain 12 to 72 UTF-8 bytes")
		return
	}
	nickname := strings.TrimSpace(in.Nickname)
	if nickname == "" {
		nickname = "Owner"
	}
	u, err := s.Store.Initialize(r.Context(), in.Email, hash, nickname)
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	s.sessionResponse(w, r, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		RememberMe     bool   `json:"remember_me,omitempty"`
		TurnstileToken string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) || len(in.Password) > 72 {
		fail(w, r, 401, "INVALID_CREDENTIALS", "Email or password is incorrect")
		return
	}
	if !s.verifyCaptcha(w, r, in.TurnstileToken) {
		return
	}
	u, err := s.Store.UserByEmail(r.Context(), in.Email)
	hash := s.dummyPasswordHash
	if err == nil {
		hash = u.PasswordHash
	}
	valid := security.VerifyPassword(hash, in.Password)
	if err != nil || !valid || u.Status != "active" {
		fail(w, r, 401, "INVALID_CREDENTIALS", "Email or password is incorrect")
		return
	}
	if r.URL.Path == "/api/auth/admin/login" && (s.Config.Edition != "platform" || !oneOf(u.Role, "admin", "super_admin")) {
		fail(w, r, 403, "FORBIDDEN", "Administrator access is required")
		return
	}
	ttl := s.Config.SessionTTL
	if in.RememberMe {
		ttl = 14 * 24 * time.Hour
	}
	s.sessionResponseTTL(w, r, u, ttl)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeSessions(r.Context(), currentUser(r).ID); err != nil {
		s.databaseError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_session", Value: "", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	ok(w, r, map[string]bool{"logged_out": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	ok(w, r, s.userProfile(r.Context(), currentUser(r)))
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Nickname   *string `json:"nickname"`
		UILanguage *string `json:"ui_language"`
		Theme      *string `json:"theme"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Nickname != nil {
		u.Nickname = strings.TrimSpace(*in.Nickname)
	}
	if in.UILanguage != nil {
		u.UILanguage = *in.UILanguage
	}
	if in.Theme != nil {
		u.Theme = *in.Theme
	}
	if len(u.Nickname) > 160 || !oneOf(u.UILanguage, "zh-CN", "en-US", "ja-JP", "en", "ja") || !oneOf(u.Theme, "system", "light", "dark") {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid profile preferences")
		return
	}
	u, err := s.Store.UpdateProfile(r.Context(), u.ID, u.Nickname, u.UILanguage, u.Theme)
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, s.userProfile(r.Context(), u))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := currentUser(r)
	if !security.VerifyPassword(u.PasswordHash, in.CurrentPassword) {
		fail(w, r, 400, "PASSWORD_INVALID", "Current password is incorrect")
		return
	}
	if in.NewPassword != in.ConfirmPassword {
		fail(w, r, 400, "PASSWORD_INVALID", "Passwords do not match")
		return
	}
	hash, err := security.HashPassword(in.NewPassword)
	if err != nil {
		fail(w, r, 400, "PASSWORD_INVALID", "Password must contain 12 to 72 UTF-8 bytes")
		return
	}
	if err = s.Store.ChangePassword(r.Context(), u.ID, u.AuthVersion, hash); err != nil {
		s.databaseError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_session", Value: "", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	ok(w, r, map[string]any{"reset": true, "requires_login": true})
}

func oneOf(v string, values ...string) bool {
	for _, candidate := range values {
		if v == candidate {
			return true
		}
	}
	return false
}
