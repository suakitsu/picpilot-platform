package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/nova"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type NovaSettings struct {
	nova.Settings
	PasswordHash string `json:"-"`
	Revision     int64  `json:"revision"`
}

func (s *Store) validNovaScope(scope string) bool {
	return s.Edition == config.Platform && scope == "platform" || s.Edition == config.Personal && strings.HasPrefix(scope, "u_") && len(scope) <= 100
}

func (s *Store) NovaSettings(ctx context.Context, scope string) (NovaSettings, error) {
	n := NovaSettings{Settings: nova.DefaultSettings()}
	if !s.validNovaScope(scope) {
		return n, ErrNotFound
	}
	var prompts, keywords json.RawMessage
	err := s.Pool.QueryRow(ctx, "SELECT mode,prompts,keywords,password_hash,revision FROM nova_settings WHERE scope=$1", scope).Scan(&n.Mode, &prompts, &keywords, &n.PasswordHash, &n.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	if json.Unmarshal(prompts, &n.Prompts) != nil || json.Unmarshal(keywords, &n.Keywords) != nil || n.Settings.Validate() != nil {
		return n, ErrConflict
	}
	return n, nil
}

// PutNovaSettings is for a trusted platform-admin/personal-owner adapter. A nil
// password preserves it, an empty password removes it. No plaintext is stored.
// Revision 0 creates; later writes must supply the current revision.
func (s *Store) PutNovaSettings(ctx context.Context, scope string, settings nova.Settings, password *string, revision int64) (NovaSettings, error) {
	if !s.validNovaScope(scope) || settings.Validate() != nil || revision < 0 {
		return NovaSettings{}, ErrConflict
	}
	var hash any
	if password != nil {
		if len(*password) > 256 || len(*password) > 0 && len(*password) < 8 {
			return NovaSettings{}, ErrConflict
		}
		value := ""
		if *password != "" {
			var err error
			value, err = security.HashPassword(*password)
			if err != nil {
				return NovaSettings{}, err
			}
		}
		hash = value
	}
	if settings.Prompts == nil {
		settings.Prompts = []nova.Prompt{}
	}
	if settings.Keywords == nil {
		settings.Keywords = []string{}
	}
	prompts, _ := json.Marshal(settings.Prompts)
	keywords, _ := json.Marshal(settings.Keywords)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return NovaSettings{}, err
	}
	defer tx.Rollback(context.Background())
	if s.Edition == config.Personal {
		var active bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')", scope).Scan(&active); err != nil {
			return NovaSettings{}, err
		}
		if !active {
			return NovaSettings{}, ErrNotFound
		}
	}
	var n NovaSettings
	err = tx.QueryRow(ctx, `INSERT INTO nova_settings(scope,mode,prompts,keywords,password_hash)
 SELECT $1,$2,$3,$4,COALESCE($5,'') WHERE $6::bigint=0 OR EXISTS(SELECT 1 FROM nova_settings WHERE scope=$1)
 ON CONFLICT(scope) DO UPDATE SET mode=EXCLUDED.mode,prompts=EXCLUDED.prompts,keywords=EXCLUDED.keywords,
 password_hash=COALESCE($5,nova_settings.password_hash),revision=nova_settings.revision+1,updated_at=now()
 WHERE nova_settings.revision=$6 RETURNING password_hash,revision`, scope, settings.Mode, prompts, keywords, hash, revision).Scan(&n.PasswordHash, &n.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, ErrConflict
	}
	if err != nil {
		return n, err
	}
	n.Settings = settings
	if _, err = tx.Exec(ctx, "DELETE FROM nova_gallery_grants WHERE scope=$1", scope); err != nil {
		return n, err
	}
	return n, tx.Commit(ctx)
}

func (s *Store) GrantNovaGallery(ctx context.Context, user, scope string, revision int64) error {
	if !s.validNovaScope(scope) || s.Edition == config.Personal && user != scope {
		return ErrNotFound
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO nova_gallery_grants(user_id,scope,revision,expires_at)
 SELECT $1,scope,revision,now()+interval '1 hour' FROM nova_settings WHERE scope=$2 AND revision=$3 AND mode<>'3'
 ON CONFLICT(user_id,scope) DO UPDATE SET revision=EXCLUDED.revision,expires_at=EXCLUDED.expires_at`, user, scope, revision)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func (s *Store) NovaGalleryAllowed(ctx context.Context, user, scope string, settings NovaSettings) (bool, error) {
	if !s.validNovaScope(scope) || s.Edition == config.Personal && user != scope {
		return false, ErrNotFound
	}
	if settings.Mode == "3" {
		return false, nil
	}
	if settings.Mode == "1" || settings.PasswordHash == "" {
		return true, nil
	}
	var allowed bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM nova_gallery_grants g JOIN nova_settings n ON n.scope=g.scope
 WHERE g.user_id=$1 AND g.scope=$2 AND g.revision=n.revision AND n.revision=$3 AND g.expires_at>$4)`, user, scope, settings.Revision, time.Now()).Scan(&allowed)
	return allowed, err
}
