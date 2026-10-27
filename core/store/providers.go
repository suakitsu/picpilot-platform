package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Provider struct {
	ID               string          `json:"id"`
	OwnerScope       string          `json:"-"`
	Name             string          `json:"name"`
	Capability       string          `json:"capability"`
	Protocol         string          `json:"protocol"`
	Profile          string          `json:"profile"`
	BaseURL          string          `json:"base_url"`
	SecretCiphertext string          `json:"-"`
	HasAPIKey        bool            `json:"has_api_key"`
	Model            string          `json:"model"`
	Enabled          bool            `json:"enabled"`
	Priority         int             `json:"priority"`
	ExecutionMode    string          `json:"execution_mode"`
	MaxConcurrency   int             `json:"max_concurrency"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	LocalWaitSeconds int             `json:"local_wait_seconds"`
	OfflineAction    string          `json:"offline_action"`
	LeaseSeconds     int             `json:"lease_seconds"`
	Settings         json.RawMessage `json:"settings"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

const providerColumns = `id,owner_scope,name,capability,protocol,profile,base_url,secret_ciphertext,model,enabled,priority,execution_mode,max_concurrency,timeout_seconds,local_wait_seconds,offline_action,lease_seconds,settings,version,created_at,updated_at`

func scanProvider(row pgx.Row) (Provider, error) {
	var p Provider
	err := row.Scan(&p.ID, &p.OwnerScope, &p.Name, &p.Capability, &p.Protocol, &p.Profile, &p.BaseURL, &p.SecretCiphertext, &p.Model, &p.Enabled, &p.Priority, &p.ExecutionMode, &p.MaxConcurrency, &p.TimeoutSeconds, &p.LocalWaitSeconds, &p.OfflineAction, &p.LeaseSeconds, &p.Settings, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	p.HasAPIKey = p.SecretCiphertext != ""
	return p, normalizeError(err)
}

func (s *Store) Providers(ctx context.Context, scope string) ([]Provider, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 ORDER BY capability,priority,id", scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Provider, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) Provider(ctx context.Context, scope, id string) (Provider, error) {
	return scanProvider(s.Pool.QueryRow(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 AND id=$2", scope, id))
}

// A nil ciphertext keeps an existing key; a pointer to an empty string clears it.
func (s *Store) PutProvider(ctx context.Context, p Provider, ciphertext *string) (Provider, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Provider{}, err
	}
	defer tx.Rollback(context.Background())
	if len(p.Settings) == 0 {
		p.Settings = json.RawMessage(`{}`)
	}
	var secret any
	if ciphertext != nil {
		secret = *ciphertext
	}
	updated, err := scanProvider(tx.QueryRow(ctx, `INSERT INTO providers(owner_scope,id,name,capability,protocol,profile,base_url,secret_ciphertext,model,enabled,priority,execution_mode,max_concurrency,timeout_seconds,local_wait_seconds,offline_action,lease_seconds,settings)
 VALUES($1,$2,$3,$4,$5,$6,$7,COALESCE($8,''),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
 ON CONFLICT(owner_scope,id) DO UPDATE SET name=EXCLUDED.name,capability=EXCLUDED.capability,protocol=EXCLUDED.protocol,profile=EXCLUDED.profile,base_url=EXCLUDED.base_url,
 secret_ciphertext=COALESCE($8,providers.secret_ciphertext),model=EXCLUDED.model,enabled=EXCLUDED.enabled,priority=EXCLUDED.priority,execution_mode=EXCLUDED.execution_mode,max_concurrency=EXCLUDED.max_concurrency,
 timeout_seconds=EXCLUDED.timeout_seconds,local_wait_seconds=EXCLUDED.local_wait_seconds,offline_action=EXCLUDED.offline_action,lease_seconds=EXCLUDED.lease_seconds,settings=EXCLUDED.settings,version=providers.version+1,updated_at=now()
 WHERE $19::bigint=0 OR providers.version=$19 RETURNING `+providerColumns, p.OwnerScope, p.ID, p.Name, p.Capability, p.Protocol, p.Profile, p.BaseURL, secret, p.Model, p.Enabled, p.Priority, p.ExecutionMode, p.MaxConcurrency, p.TimeoutSeconds, p.LocalWaitSeconds, p.OfflineAction, p.LeaseSeconds, []byte(p.Settings), p.Version))
	if errors.Is(err, ErrNotFound) {
		err = ErrConflict
	}
	if err != nil {
		return Provider{}, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM provider_health WHERE owner_scope=$1 AND provider_id=$2", p.OwnerScope, p.ID); err != nil {
		return Provider{}, err
	}
	return updated, tx.Commit(ctx)
}

func (s *Store) DeleteProvider(ctx context.Context, scope, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var exists string
	if err = tx.QueryRow(ctx, "SELECT id FROM providers WHERE owner_scope=$1 AND id=$2 FOR UPDATE", scope, id).Scan(&exists); err != nil {
		return normalizeError(err)
	}
	var busy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE provider_scope=$1 AND provider_id=$2 AND status IN ('reserved','submitting','submitted','uncertain'))`, scope, id).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, "DELETE FROM providers WHERE owner_scope=$1 AND id=$2", scope, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
