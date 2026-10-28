package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"-"`
	PasswordHash  string    `json:"-"`
	Nickname      string    `json:"nickname"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	Credits       int64     `json:"credits"`
	AuthVersion   int64     `json:"-"`
	UILanguage    string    `json:"ui_language"`
	Theme         string    `json:"theme"`
	AvatarAssetID string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const userColumns = `id,email,password_hash,nickname,role,status,credits,auth_version,ui_language,theme,COALESCE(avatar_asset_id,''),created_at,updated_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Nickname, &u.Role, &u.Status, &u.Credits, &u.AuthVersion, &u.UILanguage, &u.Theme, &u.AvatarAssetID, &u.CreatedAt, &u.UpdatedAt)
	return u, normalizeError(err)
}

func (s *Store) NeedsSetup(ctx context.Context) (bool, error) {
	var ready bool
	err := s.Pool.QueryRow(ctx, "SELECT setup_completed_at IS NOT NULL FROM instance_state WHERE singleton=true").Scan(&ready)
	return !ready, err
}

func (s *Store) Initialize(ctx context.Context, email, passwordHash, nickname string) (User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT setup_completed_at IS NOT NULL FROM instance_state WHERE singleton=true FOR UPDATE").Scan(&ready); err != nil {
		return User{}, err
	}
	if ready {
		return User{}, ErrAlreadySetup
	}
	role := "super_admin"
	if s.Edition == config.Personal {
		role = "owner"
	}
	u, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users(id,email,password_hash,nickname,role) VALUES($1,$2,$3,$4,$5) RETURNING `+userColumns, security.NewID("u"), strings.ToLower(email), passwordHash, nickname, role))
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE instance_state SET setup_completed_at=now() WHERE singleton=true"); err != nil {
		return User{}, err
	}
	if s.Edition == config.Platform {
		if err = seedDisabledLocalProviders(ctx, tx); err != nil {
			return User{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.Pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id=$1", id))
}
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.Pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE lower(email)=lower($1)", email))
}

func (s *Store) CreateUser(ctx context.Context, email, hash, nickname string) (User, error) {
	if s.Edition != config.Platform {
		return User{}, ErrConflict
	}
	return scanUser(s.Pool.QueryRow(ctx, `INSERT INTO users(id,email,password_hash,nickname,role) SELECT $1,$2,$3,$4,'user' FROM instance_state WHERE setup_completed_at IS NOT NULL RETURNING `+userColumns, security.NewID("u"), strings.ToLower(email), hash, nickname))
}

func (s *Store) UpdateProfile(ctx context.Context, id, nickname, language, theme string) (User, error) {
	return scanUser(s.Pool.QueryRow(ctx, `UPDATE users SET nickname=$2,ui_language=$3,theme=$4,updated_at=now() WHERE id=$1 RETURNING `+userColumns, id, nickname, language, theme))
}

func (s *Store) ChangePassword(ctx context.Context, id string, expectedVersion int64, hash string) error {
	r, err := s.Pool.Exec(ctx, `UPDATE users SET password_hash=$3,auth_version=auth_version+1,updated_at=now() WHERE id=$1 AND auth_version=$2`, id, expectedVersion, hash)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) RevokeSessions(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE users SET auth_version=auth_version+1,updated_at=now() WHERE id=$1", id)
	return err
}

func (s *Store) Audit(ctx context.Context, actor, action, targetType, targetID, requestID string, metadata map[string]any) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,request_id,metadata) VALUES($1,NULLIF($2,''),$3,$4,$5,$6,$7)`, security.NewID("aud"), actor, action, targetType, targetID, requestID, data)
	return err
}

func (s *Store) Setting(ctx context.Context, key string) (json.RawMessage, error) {
	var data []byte
	err := s.Pool.QueryRow(ctx, "SELECT value FROM app_settings WHERE key=$1", key).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return data, err
}

func (s *Store) PutSetting(ctx context.Context, key string, value json.RawMessage, actor string) error {
	if !json.Valid(value) {
		return ErrConflict
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES($1,$2,NULLIF($3,'')) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,version=app_settings.version+1,updated_by=EXCLUDED.updated_by,updated_at=now()`, key, []byte(value), actor)
	return err
}
