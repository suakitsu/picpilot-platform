package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

// AdminAIState is a trusted internal view. HTTP callers must project Provider fields.
type AdminAIState struct {
	Providers   []Provider `json:"-"`
	ETag        string     `json:"etag"`
	CanRollback bool       `json:"can_rollback"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type adminAISnapshotProvider struct {
	Provider
	Ciphertext string `json:"credential_ciphertext"`
}

func adminAISnapshot(ps []Provider, comparison bool) []byte {
	items := make([]adminAISnapshotProvider, 0, len(ps))
	for _, p := range ps {
		if comparison {
			p.Version, p.CreatedAt, p.UpdatedAt = 0, time.Time{}, time.Time{}
		}
		items = append(items, adminAISnapshotProvider{Provider: p, Ciphertext: p.SecretCiphertext})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	b, _ := json.Marshal(items)
	return b
}

func adminAIETag(ps []Provider) string {
	sum := sha256.Sum256(adminAISnapshot(ps, false))
	return hex.EncodeToString(sum[:])
}

func adminAIProviders(ctx context.Context, tx pgx.Tx, lock bool) ([]Provider, error) {
	query := "SELECT " + providerColumns + " FROM providers WHERE owner_scope='platform' AND capability IN ('image','text') ORDER BY capability,priority,id"
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps := make([]Provider, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

func (s *Store) AdminAIConfig(ctx context.Context, actor string) (AdminAIState, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminAIState{}, err
	}
	defer tx.Rollback(context.Background())
	ps, err := adminAIProviders(ctx, tx, false)
	if err != nil {
		return AdminAIState{}, err
	}
	state := AdminAIState{Providers: ps, ETag: adminAIETag(ps)}
	for _, p := range ps {
		if p.UpdatedAt.After(state.UpdatedAt) {
			state.UpdatedAt = p.UpdatedAt
		}
	}
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM admin_ai_config_versions)").Scan(&state.CanRollback)
	return state, err
}

func (s *Store) AdminAIVersions(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	return adminPaged(ctx, tx, f, "admin_ai_config_versions v", `jsonb_build_object('version',v.version,'action',v.action,'actor_id',v.actor_id,'created_at',v.created_at)`, "v.version DESC", adminWhere{})
}

// A version restores the state immediately before that save, matching legacy rollback.
func (s *Store) AdminAIRollbackSnapshot(ctx context.Context, actor string, version int64, vault *security.Vault) ([]Provider, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	var ciphertext string
	err = tx.QueryRow(ctx, `SELECT snapshot_ciphertext FROM admin_ai_config_versions WHERE $1::bigint=0 OR version=$1 ORDER BY version DESC LIMIT 1`, version).Scan(&ciphertext)
	if err != nil {
		return nil, normalizeError(err)
	}
	plain, err := vault.Decrypt("admin-ai-history/platform", ciphertext)
	if err != nil {
		return nil, err
	}
	var snapshot []adminAISnapshotProvider
	if err = json.Unmarshal([]byte(plain), &snapshot); err != nil {
		return nil, err
	}
	ps := make([]Provider, 0, len(snapshot))
	for _, p := range snapshot {
		p.OwnerScope = "platform"
		p.SecretCiphertext = p.Ciphertext
		p.HasAPIKey = p.Ciphertext != ""
		ps = append(ps, p.Provider)
	}
	return ps, nil
}

// AdminSaveAIConfig atomically replaces only platform image/text providers. The
// table and row locks also serialize callers of the existing provider CRUD API.
func (s *Store) AdminSaveAIConfig(ctx context.Context, actor, requestID, expected, action string, desired []Provider, vault *security.Vault) error {
	if expected == "" || len(desired) > 200 || (action != "save" && action != "rollback") {
		return ErrConflict
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "LOCK TABLE providers IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	current, err := adminAIProviders(ctx, tx, true)
	if err != nil {
		return err
	}
	if adminAIETag(current) != expected {
		return ErrConflict
	}
	old := make(map[string]Provider, len(current))
	for _, p := range current {
		old[p.ID] = p
	}
	next := make(map[string]Provider, len(desired))
	for _, p := range desired {
		if p.OwnerScope != "platform" || p.ID == "" || (p.Capability != "image" && p.Capability != "text") || !json.Valid(p.Settings) {
			return ErrConflict
		}
		if _, exists := next[p.ID]; exists {
			return ErrConflict
		}
		if prior, exists := old[p.ID]; exists && prior.Capability != p.Capability {
			return ErrConflict
		}
		// An ID owned by a video provider may not be repurposed by this API.
		var wrongCapability bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM providers WHERE owner_scope='platform' AND id=$1 AND capability NOT IN ('image','text'))", p.ID).Scan(&wrongCapability); err != nil {
			return err
		}
		if wrongCapability {
			return ErrConflict
		}
		next[p.ID] = p
	}
	for id, p := range old {
		if replacement, exists := next[id]; exists && string(adminAISnapshot([]Provider{p}, true)) == string(adminAISnapshot([]Provider{replacement}, true)) {
			continue
		}
		var busy bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE provider_scope='platform' AND provider_id=$1 AND status IN ('reserved','submitting','submitted','uncertain')) OR EXISTS(SELECT 1 FROM worker_jobs WHERE provider_scope='platform' AND provider_id=$1 AND status IN ('queued','leased','expired'))`, id).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrConflict
		}
	}
	imageModels := []string{}
	for _, p := range desired {
		if p.Capability == "image" {
			imageModels = append(imageModels, p.Model)
		}
	}
	var missingImageModel bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_image_configs WHERE (default_model<>'' AND NOT(default_model=ANY($1::text[]))) OR (fallback_model<>'' AND NOT(fallback_model=ANY($1::text[]))))`, imageModels).Scan(&missingImageModel); err != nil {
		return err
	}
	if missingImageModel {
		return ErrConflict
	}
	if string(adminAISnapshot(current, true)) == string(adminAISnapshot(desired, true)) {
		return tx.Commit(ctx)
	}
	ciphertext, err := vault.Encrypt("admin-ai-history/platform", string(adminAISnapshot(current, false)))
	if err != nil {
		return err
	}
	var version int64
	if err = tx.QueryRow(ctx, `INSERT INTO admin_ai_config_versions(snapshot_ciphertext,actor_id,action) VALUES($1,$2,$3) RETURNING version`, ciphertext, actor, action).Scan(&version); err != nil {
		return err
	}
	for _, p := range desired {
		if prior, exists := old[p.ID]; exists && string(adminAISnapshot([]Provider{prior}, true)) == string(adminAISnapshot([]Provider{p}, true)) {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO providers(owner_scope,id,name,capability,protocol,profile,base_url,secret_ciphertext,model,enabled,priority,execution_mode,max_concurrency,timeout_seconds,local_wait_seconds,offline_action,lease_seconds,settings)
 VALUES('platform',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
 ON CONFLICT(owner_scope,id) DO UPDATE SET name=EXCLUDED.name,protocol=EXCLUDED.protocol,profile=EXCLUDED.profile,base_url=EXCLUDED.base_url,secret_ciphertext=EXCLUDED.secret_ciphertext,model=EXCLUDED.model,enabled=EXCLUDED.enabled,priority=EXCLUDED.priority,execution_mode=EXCLUDED.execution_mode,max_concurrency=EXCLUDED.max_concurrency,timeout_seconds=EXCLUDED.timeout_seconds,local_wait_seconds=EXCLUDED.local_wait_seconds,offline_action=EXCLUDED.offline_action,lease_seconds=EXCLUDED.lease_seconds,settings=EXCLUDED.settings,version=providers.version+1,updated_at=now()`, p.ID, p.Name, p.Capability, p.Protocol, p.Profile, p.BaseURL, p.SecretCiphertext, p.Model, p.Enabled, p.Priority, p.ExecutionMode, p.MaxConcurrency, p.TimeoutSeconds, p.LocalWaitSeconds, p.OfflineAction, p.LeaseSeconds, []byte(p.Settings))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM provider_health WHERE owner_scope='platform' AND provider_id=$1", p.ID); err != nil {
			return err
		}
	}
	for id := range old {
		if _, exists := next[id]; !exists {
			if _, err = tx.Exec(ctx, "DELETE FROM providers WHERE owner_scope='platform' AND id=$1", id); err != nil {
				return err
			}
		}
	}
	if err = adminAudit(ctx, tx, actor, "ai_config_"+action, "ai_config", "platform", requestID, map[string]any{"version": version, "provider_count": len(desired)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
