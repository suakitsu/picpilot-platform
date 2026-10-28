package store

import (
	"context"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

// RecoverOwnerPassword is only exposed by the local operator CLI. The caller
// must already have the instance database credentials and filesystem access.
func (s *Store) RecoverOwnerPassword(ctx context.Context, email, hash string) error {
	role := "super_admin"
	if s.Edition == config.Personal {
		role = "owner"
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email)=lower($1) AND role=$2 AND status='active' FOR UPDATE`, strings.TrimSpace(email), role))
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,auth_version=auth_version+1,updated_at=now() WHERE id=$1`, u.ID, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,metadata) VALUES($1,$2,'owner.password.recovery','user',$2,'{"source":"local-cli"}')`, security.NewID("aud"), u.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
