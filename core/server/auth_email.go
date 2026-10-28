package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) registerEmailAuthRoutes() {
	s.mux.HandleFunc("POST /api/auth/email-code", s.sendEmailCode)
	s.mux.HandleFunc("POST /api/auth/forgot-password", s.forgotPassword)
	s.mux.HandleFunc("POST /api/auth/password-reset", s.passwordReset)
	s.mux.HandleFunc("POST /api/auth/register", s.register)
	s.mux.HandleFunc("POST /api/auth/login/email-code", s.loginEmailCode)
	// Administrators are provisioned by the owner, never through public signup.
	s.mux.HandleFunc("POST /api/auth/admin/register", func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, 403, "REGISTRATION_DISABLED", "Administrator registration is disabled")
	})
}

func (s *Server) codeHash(email, purpose, code string) string {
	return security.TokenHash(s.Config.AuthKey, "email-code\x00"+strings.ToLower(email)+"\x00"+purpose+"\x00"+code)
}

func (s *Server) issueEmailCode(w http.ResponseWriter, r *http.Request, email, purpose, turnstile string) bool {
	if !validEmail(email) || !oneOf(purpose, "login", "register", "reset_password") {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid email or verification purpose")
		return false
	}
	if purpose == "register" && (s.Config.Edition != config.Platform || !s.Config.RegistrationEnabled) {
		fail(w, r, 403, "REGISTRATION_DISABLED", "Registration is disabled")
		return false
	}
	if !s.verifyCaptcha(w, r, turnstile) {
		return false
	}
	if s.Mailer == nil {
		fail(w, r, 503, "EMAIL_NOT_CONFIGURED", "Email delivery is not configured; contact the instance owner")
		return false
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		fail(w, r, 500, "INTERNAL_ERROR", "Could not create verification code")
		return false
	}
	code := fmt.Sprintf("%06d", n.Int64())
	id, err := s.Store.IssueAuthChallenge(r.Context(), email, purpose, s.codeHash(email, purpose, code))
	if errors.Is(err, store.ErrChallengeCooldown) {
		w.Header().Set("Retry-After", "60")
		fail(w, r, 429, "EMAIL_CODE_COOLDOWN", "Wait before requesting another code")
		return false
	}
	if err != nil {
		s.databaseError(w, r, err)
		return false
	}
	if err = s.Mailer.SendCode(r.Context(), email, purpose, code); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.Store.InvalidateAuthChallenge(ctx, id)
		cancel()
		fail(w, r, 503, "EMAIL_DELIVERY_UNAVAILABLE", "Email delivery is temporarily unavailable")
		return false
	}
	return true
}

func (s *Server) sendEmailCode(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email          string `json:"email"`
		Purpose        string `json:"purpose"`
		TurnstileToken string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !s.issueEmailCode(w, r, in.Email, in.Purpose, in.TurnstileToken) {
		return
	}
	ok(w, r, map[string]any{"sent": true, "email_masked": profile(store.User{Email: in.Email})["email_masked"], "purpose": in.Purpose, "expires_in_seconds": 600, "cooldown_seconds": 60})
}

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email          string `json:"email"`
		Note           string `json:"note,omitempty"`
		TurnstileToken string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if len(in.Note) > 2000 {
		fail(w, r, 400, "INVALID_REQUEST", "Note is too long")
		return
	}
	if !s.issueEmailCode(w, r, in.Email, "reset_password", in.TurnstileToken) {
		return
	}
	ok(w, r, map[string]any{"accepted": true, "message": "Check your email for the verification code"})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	if s.Config.Edition != config.Platform || !s.Config.RegistrationEnabled {
		fail(w, r, 403, "REGISTRATION_DISABLED", "Registration is disabled")
		return
	}
	var in struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
		Code            string `json:"verification_code"`
		TurnstileToken  string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) || in.Password != in.ConfirmPassword || !validEmailCode(in.Code) {
		fail(w, r, 400, "INVALID_REQUEST", "Check your email, code and password confirmation")
		return
	}
	if !s.verifyCaptcha(w, r, in.TurnstileToken) {
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		fail(w, r, 400, "PASSWORD_INVALID", "Password must contain 12 to 72 UTF-8 bytes")
		return
	}
	u, err := s.Store.RedeemAuthChallenge(r.Context(), store.ChallengeAction{Email: in.Email, Purpose: "register", TokenHash: s.codeHash(in.Email, "register", in.Code), PasswordHash: hash, Nickname: "New user"})
	if err != nil {
		s.challengeError(w, r, err)
		return
	}
	s.sessionResponse(w, r, u)
}

func (s *Server) loginEmailCode(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email          string `json:"email"`
		Code           string `json:"verification_code"`
		RememberMe     bool   `json:"remember_me,omitempty"`
		TurnstileToken string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) || !validEmailCode(in.Code) {
		fail(w, r, 400, "VERIFICATION_INVALID", "Verification code is invalid or expired")
		return
	}
	if !s.verifyCaptcha(w, r, in.TurnstileToken) {
		return
	}
	u, err := s.Store.RedeemAuthChallenge(r.Context(), store.ChallengeAction{Email: in.Email, Purpose: "login", TokenHash: s.codeHash(in.Email, "login", in.Code)})
	if err != nil {
		s.challengeError(w, r, err)
		return
	}
	ttl := s.Config.SessionTTL
	if in.RememberMe {
		ttl = 14 * 24 * time.Hour
	}
	s.sessionResponseTTL(w, r, u, ttl)
}

func (s *Server) passwordReset(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
		Code            string `json:"verification_code"`
		TurnstileToken  string `json:"turnstile_token,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !validEmail(in.Email) || in.Password != in.ConfirmPassword || !validEmailCode(in.Code) {
		fail(w, r, 400, "INVALID_REQUEST", "Check your email, code and password confirmation")
		return
	}
	if !s.verifyCaptcha(w, r, in.TurnstileToken) {
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		fail(w, r, 400, "PASSWORD_INVALID", "Password must contain 12 to 72 UTF-8 bytes")
		return
	}
	u, err := s.Store.RedeemAuthChallenge(r.Context(), store.ChallengeAction{Email: in.Email, Purpose: "reset_password", TokenHash: s.codeHash(in.Email, "reset_password", in.Code), PasswordHash: hash})
	if err != nil {
		s.challengeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "picpilot_session", Value: "", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	ok(w, r, map[string]any{"updated": true, "requires_login": true, "redirect_to": "/login", "email_masked": profile(u)["email_masked"]})
}

func validEmailCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, v := range code {
		if v < '0' || v > '9' {
			return false
		}
	}
	return true
}

func (s *Server) challengeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrChallengeInvalid) || errors.Is(err, store.ErrConflict) {
		fail(w, r, 400, "VERIFICATION_INVALID", "Verification code or account is invalid or expired")
		return
	}
	s.databaseError(w, r, err)
}
