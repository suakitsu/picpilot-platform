package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

// SubmitProjectTaskTx is the transaction-scoped counterpart of SubmitTask for
// CreativeWrite. It deliberately uses the same hash document and ledger types.
// The caller must commit or roll back; this method never starts a separate tx.
func (s *Store) SubmitProjectTaskTx(ctx context.Context, tx pgx.Tx, in SubmitTask) (Task, bool, error) {
	if in.UserID == "" || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 128 || in.CreditsCost < 0 || in.CreditsCost > 1000000000 {
		return Task{}, false, ErrConflict
	}
	if s.Edition == config.Personal && in.CreditsCost != 0 {
		return Task{}, false, ErrConflict
	}
	request, err := CanonicalJSON(in.Request)
	if err != nil {
		return Task{}, false, err
	}
	identity := request
	if len(in.RequestIdentity) > 0 {
		identity, err = CanonicalJSON(in.RequestIdentity)
		if err != nil {
			return Task{}, false, err
		}
	}
	hashData, err := json.Marshal(struct {
		Project, Capability, Kind, Title string
		Cost                             int64
		Request                          json.RawMessage
	}{in.ProjectID, in.Capability, in.Kind, in.Title, in.CreditsCost, identity})
	if err != nil {
		return Task{}, false, err
	}
	sum := sha256.Sum256(hashData)
	hash := hex.EncodeToString(sum[:])
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", in.UserID+"\x1f"+in.IdempotencyKey); err != nil {
		return Task{}, false, err
	}
	existing, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND idempotency_key=$2", in.UserID, in.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != hash {
			return Task{}, false, ErrConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, false, err
	}
	var status string
	if err = tx.QueryRow(ctx, "SELECT status FROM users WHERE id=$1 FOR UPDATE", in.UserID).Scan(&status); err != nil {
		return Task{}, false, normalizeError(err)
	}
	if status != "active" {
		return Task{}, false, ErrDisabled
	}
	if in.ProjectID != "" {
		var owns bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)", in.ProjectID, in.UserID).Scan(&owns); err != nil {
			return Task{}, false, err
		}
		if !owns {
			return Task{}, false, ErrNotFound
		}
	}
	id := security.NewID("task")
	if in.CreditsCost > 0 && !in.SkipCharge {
		if _, err = adjustCredits(ctx, tx, in.UserID, -in.CreditsCost, "generation_charge", id, "Generation task", ""); err != nil {
			return Task{}, false, err
		}
	}
	t, err := scanTask(tx.QueryRow(ctx, `INSERT INTO tasks(id,user_id,project_id,capability,kind,title,request_json,request_hash,idempotency_key,credits_cost) VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10) RETURNING `+taskColumns, id, in.UserID, in.ProjectID, in.Capability, in.Kind, in.Title, request, hash, in.IdempotencyKey, in.CreditsCost))
	return t, err == nil, err
}

func (s *Store) CreativeReplay(ctx context.Context, user, key string, identity json.RawMessage) (WorkflowResult, bool, error) {
	canonical, err := CanonicalJSON(identity)
	if err != nil {
		return WorkflowResult{}, false, err
	}
	sum := sha256.Sum256(canonical)
	var hash string
	var raw json.RawMessage
	err = s.Pool.QueryRow(ctx, "SELECT request_hash,result FROM creative_requests WHERE user_id=$1 AND request_key=$2", user, key).Scan(&hash, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowResult{}, false, nil
	}
	if err != nil {
		return WorkflowResult{}, false, err
	}
	if hash != hex.EncodeToString(sum[:]) {
		return WorkflowResult{}, false, ErrConflict
	}
	var result WorkflowResult
	if json.Unmarshal(raw, &result) != nil {
		return result, false, ErrConflict
	}
	return result, true, nil
}
