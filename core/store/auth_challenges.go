package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

var ErrChallengeInvalid = errors.New("verification code invalid or expired")
var ErrChallengeCooldown = errors.New("verification code cooldown active")

// Challenge issuance and consumption share a per-address lock across instances.
func lockChallenge(ctx context.Context, tx pgx.Tx, email string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "auth-challenge:"+strings.ToLower(email))
	return err
}

func (s *Store) IssueAuthChallenge(ctx context.Context, email, purpose, tokenHash string) (string, error) {
	if !validChallengePurpose(purpose) || len(tokenHash) != 64 {
		return "", ErrChallengeInvalid
	}
	if purpose == "register" && s.Edition != config.Platform {
		return "", ErrConflict
	}
	email = strings.ToLower(email)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	if err = lockChallenge(ctx, tx, email); err != nil {
		return "", err
	}
	var limited bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_challenges WHERE email=$1 AND created_at>clock_timestamp()-interval '60 seconds') OR (SELECT count(*) FROM auth_challenges WHERE email=$1 AND created_at>clock_timestamp()-interval '1 hour')>=10`, email).Scan(&limited); err != nil {
		return "", err
	}
	if limited {
		return "", ErrChallengeCooldown
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_challenges SET consumed_at=clock_timestamp() WHERE email=$1 AND purpose=$2 AND consumed_at IS NULL`, email, purpose); err != nil {
		return "", err
	}
	id := security.NewID("code")
	if _, err = tx.Exec(ctx, `INSERT INTO auth_challenges(id,email,purpose,token_hash,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '10 minutes')`, id, email, purpose, tokenHash); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (s *Store) InvalidateAuthChallenge(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE auth_challenges SET consumed_at=clock_timestamp() WHERE id=$1 AND consumed_at IS NULL`, id)
	return err
}

func validChallengePurpose(purpose string) bool {
	return purpose == "register" || purpose == "login" || purpose == "reset_password"
}

type ChallengeAction struct {
	Email, Purpose, TokenHash string
	PasswordHash, Nickname    string
}

// The challenge and account mutation commit together, so a code cannot be reused
// by a concurrent login/reset or be lost after a transaction rollback.
func (s *Store) RedeemAuthChallenge(ctx context.Context, in ChallengeAction) (User, error) {
	if !validChallengePurpose(in.Purpose) || len(in.TokenHash) != 64 || (in.Purpose != "login" && in.PasswordHash == "") {
		return User{}, ErrChallengeInvalid
	}
	if in.Purpose == "register" && s.Edition != config.Platform {
		return User{}, ErrConflict
	}
	email := strings.ToLower(in.Email)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	if err = lockChallenge(ctx, tx, email); err != nil {
		return User{}, err
	}
	var id, tokenHash string
	var attempts int
	var usable bool
	err = tx.QueryRow(ctx, `SELECT id,token_hash,attempts,consumed_at IS NULL AND expires_at>clock_timestamp() FROM auth_challenges WHERE email=$1 AND purpose=$2 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, email, in.Purpose).Scan(&id, &tokenHash, &attempts, &usable)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrChallengeInvalid
	}
	if err != nil {
		return User{}, err
	}
	if !usable || attempts >= 5 {
		return User{}, ErrChallengeInvalid
	}
	if !security.ConstantEqual(tokenHash, in.TokenHash) {
		if _, err = tx.Exec(ctx, `UPDATE auth_challenges SET attempts=attempts+1 WHERE id=$1`, id); err != nil {
			return User{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return User{}, err
		}
		return User{}, ErrChallengeInvalid
	}
	var u User
	if in.Purpose == "register" {
		u, err = scanUser(tx.QueryRow(ctx, `INSERT INTO users(id,email,password_hash,nickname,role) SELECT $1,$2,$3,$4,'user' FROM instance_state WHERE setup_completed_at IS NOT NULL RETURNING `+userColumns, security.NewID("u"), email, in.PasswordHash, in.Nickname))
	} else {
		u, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email)=$1 FOR UPDATE`, email))
		if err == nil && u.Status != "active" {
			return User{}, ErrChallengeInvalid
		}
		if err == nil && in.Purpose == "reset_password" {
			u, err = scanUser(tx.QueryRow(ctx, `UPDATE users SET password_hash=$2,auth_version=auth_version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+userColumns, u.ID, in.PasswordHash))
		}
	}
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrChallengeInvalid
	}
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_challenges SET consumed_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) CreateOAuthState(ctx context.Context, stateHash, browserHash, next string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO oauth_states(id,token_hash,browser_hash,next_path,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '10 minutes')`, security.NewID("oauth"), stateHash, browserHash, next)
	return err
}

func (s *Store) ConsumeOAuthState(ctx context.Context, stateHash, browserHash string) (string, error) {
	var next string
	err := s.Pool.QueryRow(ctx, `UPDATE oauth_states SET consumed_at=clock_timestamp() WHERE token_hash=$1 AND browser_hash=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp() RETURNING next_path`, stateHash, browserHash).Scan(&next)
	return next, normalizeError(err)
}

// External identities never implicitly attach to an existing email account.
// Linking must be performed from an authenticated account in a separate flow.
func (s *Store) GitHubLogin(ctx context.Context, subject, email, nickname, passwordHash string, allowRegistration bool) (User, error) {
	if s.Edition != config.Platform {
		return User{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "oauth:github:"+subject); err != nil {
		return User{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT user_id FROM oauth_identities WHERE provider='github' AND subject=$1`, subject).Scan(&id)
	var u User
	if err == nil {
		u, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, id))
	} else if errors.Is(err, pgx.ErrNoRows) && allowRegistration {
		u, err = scanUser(tx.QueryRow(ctx, `INSERT INTO users(id,email,password_hash,nickname,role) SELECT $1,$2,$3,$4,'user' FROM instance_state WHERE setup_completed_at IS NOT NULL RETURNING `+userColumns, security.NewID("u"), strings.ToLower(email), passwordHash, nickname))
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO oauth_identities(provider,subject,user_id) VALUES('github',$1,$2)`, subject, u.ID)
		}
	}
	if err != nil {
		return User{}, normalizeError(err)
	}
	if u.Status != "active" {
		return User{}, ErrConflict
	}
	return u, tx.Commit(ctx)
}

func (s *Store) PurgeExpiredAuthState(ctx context.Context) error {
	// Retain issuance metadata for one day for database-backed rate limiting.
	return s.PurgeExpiredAuthStateBefore(ctx, time.Now().Add(-24*time.Hour))
}
