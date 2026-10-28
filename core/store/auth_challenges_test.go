package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestPostgresVerificationCodeRateLimitAndSingleUse(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "owner@example.invalid")
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	var issued atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.IssueAuthChallenge(ctx, u.Email, "reset_password", hash)
			if err == nil {
				issued.Add(1)
			} else if !errors.Is(err, ErrChallengeCooldown) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if issued.Load() != 1 {
		t.Fatal("multiple codes issued inside the same cooldown")
	}
	var redeemed atomic.Int32
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RedeemAuthChallenge(ctx, ChallengeAction{Email: u.Email, Purpose: "reset_password", TokenHash: hash, PasswordHash: "new-test-hash"})
			if err == nil {
				redeemed.Add(1)
			} else if !errors.Is(err, ErrChallengeInvalid) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if redeemed.Load() != 1 {
		t.Fatal("verification code did not execute exactly once")
	}
	after, err := s.UserByID(ctx, u.ID)
	if err != nil || after.AuthVersion != u.AuthVersion+1 || after.PasswordHash != "new-test-hash" {
		t.Fatal("password reset was not atomic")
	}
}

func TestPostgresVerificationCodePurposeExpiryAndBruteForce(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "owner@example.invalid")
	ctx := context.Background()
	correct, wrong := strings.Repeat("a", 64), strings.Repeat("b", 64)
	id, err := s.IssueAuthChallenge(ctx, u.Email, "login", correct)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemAuthChallenge(ctx, ChallengeAction{Email: u.Email, Purpose: "reset_password", TokenHash: correct, PasswordHash: "new-test-hash"}); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatal("code accepted for a different purpose")
	}
	for range 5 {
		if _, err = s.RedeemAuthChallenge(ctx, ChallengeAction{Email: u.Email, Purpose: "login", TokenHash: wrong}); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatal("wrong code accepted")
		}
	}
	if _, err = s.RedeemAuthChallenge(ctx, ChallengeAction{Email: u.Email, Purpose: "login", TokenHash: correct}); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatal("attempt limit ignored")
	}
	_, err = s.Pool.Exec(ctx, `UPDATE auth_challenges SET attempts=0,expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemAuthChallenge(ctx, ChallengeAction{Email: u.Email, Purpose: "login", TokenHash: correct}); !errors.Is(err, ErrChallengeInvalid) {
		t.Fatal("expired code accepted")
	}
}

func TestPostgresOAuthStateAndIdentityIsolation(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "owner@example.invalid")
	ctx := context.Background()
	state, browser := strings.Repeat("c", 64), strings.Repeat("d", 64)
	if err := s.CreateOAuthState(ctx, state, browser, "/workspace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeOAuthState(ctx, state, "different-browser"); !errors.Is(err, ErrNotFound) {
		t.Fatal("OAuth state not bound to browser")
	}
	next, err := s.ConsumeOAuthState(ctx, state, browser)
	if err != nil || next != "/workspace" {
		t.Fatal("OAuth state consumption failed")
	}
	if _, err = s.ConsumeOAuthState(ctx, state, browser); !errors.Is(err, ErrNotFound) {
		t.Fatal("OAuth state reused")
	}
	if _, err = s.GitHubLogin(ctx, "1234", u.Email, "External", "test-hash", true); !errors.Is(err, ErrConflict) {
		t.Fatal("OAuth silently linked existing email")
	}
	if _, err = s.GitHubLogin(ctx, "1234", "github@example.invalid", "External", "test-hash", false); !errors.Is(err, ErrNotFound) {
		t.Fatal("closed registration bypassed")
	}
	created, err := s.GitHubLogin(ctx, "1234", "github@example.invalid", "External", "test-hash", true)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := s.GitHubLogin(ctx, "1234", "changed@example.invalid", "Changed", "another-hash", false)
	if err != nil || created.ID != repeat.ID || repeat.Email != "github@example.invalid" {
		t.Fatal("external identity was not stable")
	}
}
