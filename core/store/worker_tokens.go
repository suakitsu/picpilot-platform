package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

type WorkerTokenView struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Enabled     bool       `json:"enabled"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
}
type IssuedWorkerToken struct {
	Token       string          `json:"token"`
	WorkerToken WorkerTokenView `json:"worker_token"`
	ShownOnce   bool            `json:"shown_once"`
}

// WorkerPrincipal carries a hash of the authenticated secret, allowing each
// transaction to recheck regeneration/revocation without retaining the raw key.
type WorkerPrincipal struct {
	TokenID    string `json:"-"`
	OwnerScope string `json:"-"`
	TokenHash  string `json:"-"`
}

const workerTokenColumns = `id,name,token_prefix,(revoked_at IS NULL),last_used_at,revoked_at,created_at`

func scanWorkerToken(row pgx.Row) (WorkerTokenView, error) {
	var v WorkerTokenView
	err := row.Scan(&v.ID, &v.Name, &v.TokenPrefix, &v.Enabled, &v.LastUsedAt, &v.RevokedAt, &v.CreatedAt)
	return v, normalizeError(err)
}
func WorkerTokenDigest(pepper []byte, raw string) (string, error) {
	if len(pepper) < 32 {
		return "", wp.NewError("WORKER_INTERNAL_ERROR")
	}
	if len(raw) < 16 || len(raw) > 512 || !strings.HasPrefix(raw, "ppw_") {
		return "", wp.NewError("WORKER_TOKEN_INVALID")
	}
	h := sha256.New()
	h.Write(pepper)
	h.Write([]byte{0})
	h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Store) validWorkerScope(scope string) bool {
	return s.Edition == config.Platform && scope == "platform" || s.Edition == config.Personal && scope != "" && scope != "platform"
}
func (s *Store) CreateWorkerToken(ctx context.Context, scope, actor, name string, pepper []byte) (IssuedWorkerToken, error) {
	name = strings.TrimSpace(name)
	if !s.validWorkerScope(scope) || name == "" || utf8.RuneCountInString(name) > 120 {
		return IssuedWorkerToken{}, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	raw, err := security.RandomToken("ppw_", 32)
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	hash, err := WorkerTokenDigest(pepper, raw)
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	v, err := scanWorkerToken(s.Pool.QueryRow(ctx, `INSERT INTO worker_tokens(id,owner_scope,name,token_hash,token_prefix,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+workerTokenColumns, security.NewID("awt"), scope, name, hash, raw[:12], actor))
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	return IssuedWorkerToken{Token: raw, WorkerToken: v, ShownOnce: true}, nil
}
func (s *Store) RegenerateWorkerToken(ctx context.Context, scope, id string, pepper []byte) (IssuedWorkerToken, error) {
	if !s.validWorkerScope(scope) {
		return IssuedWorkerToken{}, ErrNotFound
	}
	raw, err := security.RandomToken("ppw_", 32)
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	hash, err := WorkerTokenDigest(pepper, raw)
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	v, err := scanWorkerToken(s.Pool.QueryRow(ctx, `UPDATE worker_tokens SET token_hash=$3,token_prefix=$4,revoked_at=NULL,last_used_at=NULL WHERE owner_scope=$1 AND id=$2 RETURNING `+workerTokenColumns, scope, id, hash, raw[:12]))
	if err != nil {
		return IssuedWorkerToken{}, err
	}
	return IssuedWorkerToken{Token: raw, WorkerToken: v, ShownOnce: true}, nil
}
func (s *Store) RevokeWorkerToken(ctx context.Context, scope, id string) (WorkerTokenView, error) {
	if !s.validWorkerScope(scope) {
		return WorkerTokenView{}, ErrNotFound
	}
	return scanWorkerToken(s.Pool.QueryRow(ctx, `UPDATE worker_tokens SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE owner_scope=$1 AND id=$2 RETURNING `+workerTokenColumns, scope, id))
}
func (s *Store) AuthenticateWorkerToken(ctx context.Context, raw string, pepper []byte) (WorkerPrincipal, error) {
	hash, err := WorkerTokenDigest(pepper, raw)
	if err != nil {
		return WorkerPrincipal{}, err
	}
	var p WorkerPrincipal
	p.TokenHash = hash
	err = s.Pool.QueryRow(ctx, `UPDATE worker_tokens SET last_used_at=clock_timestamp() WHERE token_hash=$1 AND revoked_at IS NULL RETURNING id,owner_scope`, hash).Scan(&p.TokenID, &p.OwnerScope)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkerPrincipal{}, wp.NewError("WORKER_TOKEN_INVALID")
	}
	if err != nil {
		return WorkerPrincipal{}, err
	}
	if !s.validWorkerScope(p.OwnerScope) {
		return WorkerPrincipal{}, wp.NewError("WORKER_TOKEN_INVALID")
	}
	return p, nil
}
func workerTokenLock(ctx context.Context, tx pgx.Tx, p WorkerPrincipal) error {
	var scope string
	err := tx.QueryRow(ctx, `SELECT owner_scope FROM worker_tokens WHERE id=$1 AND token_hash=$2 AND revoked_at IS NULL FOR SHARE`, p.TokenID, p.TokenHash).Scan(&scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return wp.NewError("WORKER_TOKEN_INVALID")
	}
	if err != nil {
		return err
	}
	if scope != p.OwnerScope {
		return wp.NewError("WORKER_TOKEN_INVALID")
	}
	return nil
}
func workerBinding(ctx context.Context, tx pgx.Tx, p WorkerPrincipal, id string, exclusive bool) (wp.Worker, error) {
	if err := wp.ValidateWorkerID(id); err != nil {
		return wp.Worker{}, err
	}
	query := `SELECT worker_id,token_id,capabilities,max_concurrency,last_heartbeat_at FROM worker_instances WHERE worker_id=$1 FOR SHARE`
	if exclusive {
		query = strings.Replace(query, "FOR SHARE", "FOR UPDATE", 1)
	}
	var w wp.Worker
	var caps []byte
	err := tx.QueryRow(ctx, query, id).Scan(&w.WorkerID, &w.TokenID, &caps, &w.MaxConcurrency, &w.LastHeartbeat)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, wp.NewError("WORKER_HEARTBEAT_REQUIRED")
	}
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal(caps, &w.Capabilities); err != nil {
		return w, wp.NewError("WORKER_INTERNAL_ERROR")
	}
	return w, wp.VerifyWorkerOwnership(&w, p.TokenID, id)
}
func workerNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
	return now, err
}
func (s *Store) WorkerHeartbeat(ctx context.Context, p WorkerPrincipal, request wp.HeartbeatRequest) (wp.HeartbeatResponse, error) {
	if err := request.Validate(); err != nil {
		return wp.HeartbeatResponse{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return wp.HeartbeatResponse{}, err
	}
	defer tx.Rollback(context.Background())
	if err = workerTokenLock(ctx, tx, p); err != nil {
		return wp.HeartbeatResponse{}, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "worker-id:"+request.WorkerID); err != nil {
		return wp.HeartbeatResponse{}, err
	}
	var tokenID string
	err = tx.QueryRow(ctx, "SELECT token_id FROM worker_instances WHERE worker_id=$1 FOR UPDATE", request.WorkerID).Scan(&tokenID)
	if err == nil && tokenID != p.TokenID {
		return wp.HeartbeatResponse{}, wp.NewError("WORKER_ID_IN_USE")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return wp.HeartbeatResponse{}, err
	}
	now, err := workerNow(ctx, tx)
	if err != nil {
		return wp.HeartbeatResponse{}, err
	}
	caps, _ := json.Marshal(request.Capabilities)
	_, err = tx.Exec(ctx, `INSERT INTO worker_instances(worker_id,token_id,capabilities,max_concurrency,last_heartbeat_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(worker_id) DO UPDATE SET capabilities=EXCLUDED.capabilities,max_concurrency=EXCLUDED.max_concurrency,last_heartbeat_at=EXCLUDED.last_heartbeat_at`, request.WorkerID, p.TokenID, caps, request.MaxConcurrency, now)
	if err != nil {
		return wp.HeartbeatResponse{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE worker_id=$1 AND status='leased' AND grace_until>$2`, request.WorkerID, now).Scan(&count); err != nil {
		return wp.HeartbeatResponse{}, err
	}
	response := wp.HeartbeatResponse{WorkerID: request.WorkerID, ServerTime: now, ActiveTasks: count, HeartbeatIntervalSeconds: 20}
	return response, tx.Commit(ctx)
}
func (s *Store) WorkerAdminState(ctx context.Context, scope string) (map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+workerTokenColumns+` FROM worker_tokens WHERE owner_scope=$1 ORDER BY created_at`, scope)
	if err != nil {
		return nil, err
	}
	tokens := []WorkerTokenView{}
	for rows.Next() {
		v, e := scanWorkerToken(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		tokens = append(tokens, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT w.worker_id,w.capabilities,w.last_heartbeat_at,w.max_concurrency,t.revoked_at IS NULL AND w.last_heartbeat_at>clock_timestamp()-interval '60 seconds',ARRAY(SELECT j.job_id FROM worker_jobs j WHERE j.worker_id=w.worker_id AND j.status='leased' AND j.grace_until>clock_timestamp() ORDER BY j.queued_at,j.job_id) FROM worker_instances w JOIN worker_tokens t ON t.id=w.token_id WHERE t.owner_scope=$1 ORDER BY w.last_heartbeat_at DESC`, scope)
	if err != nil {
		return nil, err
	}
	instances := []map[string]any{}
	for rows.Next() {
		var id string
		var caps json.RawMessage
		var last time.Time
		var max int
		var online bool
		var jobs []string
		if err = rows.Scan(&id, &caps, &last, &max, &online, &jobs); err != nil {
			rows.Close()
			return nil, err
		}
		instances = append(instances, map[string]any{"worker_id": id, "capabilities": caps, "online": online, "last_heartbeat_at": last, "max_concurrency": max, "active_tasks": len(jobs), "current_jobs": jobs})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	providers := map[string]any{}
	ps, err := s.Providers(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.ExecutionMode != "pull_worker" {
			continue
		}
		var count, online int
		allowed, allowErr := workerAllowedIDs(p.Settings)
		if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE provider_scope=$1 AND provider_id=$2 AND status='leased' AND grace_until>clock_timestamp()`, scope, p.ID).Scan(&count); err != nil {
			return nil, err
		}
		for _, w := range instances {
			if w["online"] == true && allowErr == nil && (len(allowed) == 0 || slices.Contains(allowed, w["worker_id"].(string))) {
				var caps []string
				json.Unmarshal(w["capabilities"].(json.RawMessage), &caps)
				for _, cap := range caps {
					if cap == p.Capability {
						online++
					}
				}
			}
		}
		providers[p.ID] = map[string]any{"online": online > 0, "online_workers": online, "active_tasks": count, "max_concurrency": p.MaxConcurrency}
	}
	return map[string]any{"tokens": tokens, "instances": instances, "providers": providers, "offline_after_seconds": 60}, nil
}
