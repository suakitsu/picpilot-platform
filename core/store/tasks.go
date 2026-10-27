package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

type Task struct {
	ID                string          `json:"id"`
	UserID            string          `json:"user_id"`
	ProjectID         string          `json:"project_id,omitempty"`
	Capability        string          `json:"capability"`
	Kind              string          `json:"kind"`
	Title             string          `json:"title"`
	Status            string          `json:"status"`
	Request           json.RawMessage `json:"-"`
	RequestHash       string          `json:"-"`
	IdempotencyKey    string          `json:"-"`
	CreditsCost       int64           `json:"credits_cost"`
	Result            json.RawMessage `json:"result"`
	ErrorCode         string          `json:"error_code"`
	ProviderScope     string          `json:"-"`
	ProviderID        string          `json:"provider"`
	Model             string          `json:"model"`
	Attempt           int             `json:"attempt"`
	LeaseOwner        string          `json:"-"`
	LeaseID           string          `json:"-"`
	LeaseUntil        *time.Time      `json:"-"`
	NextRunAt         time.Time       `json:"-"`
	CancelRequestedAt *time.Time      `json:"-"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

const taskColumns = `id,user_id,COALESCE(project_id,''),capability,kind,title,status,request_json,request_hash,idempotency_key,credits_cost,result_json,error_code,provider_scope,provider_id,model,attempt,COALESCE(lease_owner,''),COALESCE(lease_id,''),lease_until,next_run_at,cancel_requested_at,completed_at,created_at,updated_at`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Capability, &t.Kind, &t.Title, &t.Status, &t.Request, &t.RequestHash, &t.IdempotencyKey, &t.CreditsCost, &t.Result, &t.ErrorCode, &t.ProviderScope, &t.ProviderID, &t.Model, &t.Attempt, &t.LeaseOwner, &t.LeaseID, &t.LeaseUntil, &t.NextRunAt, &t.CancelRequestedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, normalizeError(err)
}

type SubmitTask struct {
	UserID     string
	ProjectID  string
	Capability string
	Kind       string
	Title      string
	Request    json.RawMessage
	// RequestIdentity is an optional pre-normalization document used only for
	// hashing retries. It is never persisted (and may contain transient inputs).
	RequestIdentity json.RawMessage
	IdempotencyKey  string
	CreditsCost     int64
	// SkipCharge is restricted to an enclosing atomic business operation that
	// records its own aggregate ledger entry.
	SkipCharge bool
}

func CanonicalJSON(data []byte) ([]byte, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, ErrConflict
	}
	if !json.Valid(data) {
		return nil, ErrConflict
	}
	return json.Marshal(v)
}

func (s *Store) SubmitTask(ctx context.Context, in SubmitTask) (Task, bool, error) {
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
	h := sha256.Sum256(hashData)
	hash := hex.EncodeToString(h[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", in.UserID+"\x1f"+in.IdempotencyKey); err != nil {
		return Task{}, false, err
	}
	existing, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND idempotency_key=$2", in.UserID, in.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != hash {
			return Task{}, false, ErrConflict
		}
		return existing, false, tx.Commit(ctx)
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
	if err != nil {
		return Task{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, false, err
	}
	return t, true, nil
}

func (s *Store) Task(ctx context.Context, userID, id string) (Task, error) {
	return scanTask(s.Pool.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL", id, userID))
}

func (s *Store) ClaimTask(ctx context.Context, owner string, ttl time.Duration) (Task, error) {
	return s.ClaimTaskCapabilities(ctx, owner, ttl, nil)
}

func (s *Store) ClaimTaskCapabilities(ctx context.Context, owner string, ttl time.Duration, capabilities []string) (Task, error) {
	if owner == "" || ttl < 10*time.Second || ttl > 30*time.Minute {
		return Task{}, ErrConflict
	}
	return scanTask(s.Pool.QueryRow(ctx, `UPDATE tasks SET status='running',lease_owner=$1,lease_id=$2,lease_until=now()+$3*interval '1 second',updated_at=now()
 WHERE id=(SELECT id FROM tasks WHERE (status='queued' AND cancel_requested_at IS NULL OR status='waiting_worker') AND next_run_at<=now() AND ($4::text[] IS NULL OR capability=ANY($4::text[])) ORDER BY next_run_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING `+taskColumns, owner, security.NewID("lease"), int64(ttl/time.Second), capabilities))
}

func (s *Store) RenewTask(ctx context.Context, t Task, ttl time.Duration) error {
	if ttl < 10*time.Second || ttl > 30*time.Minute {
		return ErrConflict
	}
	r, err := s.Pool.Exec(ctx, `UPDATE tasks SET lease_until=now()+$4*interval '1 second',updated_at=now() WHERE id=$1 AND lease_owner=$2 AND lease_id=$3 AND lease_until>now() AND status IN ('running','polling','waiting_worker')`, t.ID, t.LeaseOwner, t.LeaseID, int64(ttl/time.Second))
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrLeaseInvalid
	}
	return nil
}

func (s *Store) CompleteTask(ctx context.Context, lease Task, result json.RawMessage) (Task, error) {
	return s.finishTask(ctx, lease, "succeeded", "", result)
}
func (s *Store) FailTask(ctx context.Context, lease Task, code string) (Task, error) {
	return s.finishTask(ctx, lease, "failed", code, json.RawMessage(`{}`))
}
func (s *Store) CancelUnsubmittedWorkerTask(ctx context.Context, lease Task) (Task, error) {
	var safe bool
	if err := s.Pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM worker_jobs WHERE task_id=$1 AND (leased_at IS NOT NULL OR status IN ('queued','leased'))) AND EXISTS(SELECT 1 FROM worker_jobs WHERE task_id=$1 AND status='cancelled')`, lease.ID).Scan(&safe); err != nil {
		return Task{}, err
	}
	if !safe {
		return Task{}, ErrConflict
	}
	return s.finishTask(ctx, lease, "cancelled", "TASK_CANCELLED_BEFORE_SUBMISSION", json.RawMessage(`{}`))
}

func (s *Store) finishTask(ctx context.Context, lease Task, status, code string, result json.RawMessage) (Task, error) {
	canonical, err := CanonicalJSON(result)
	if err != nil {
		return Task{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 FOR UPDATE", lease.ID))
	if err != nil {
		return Task{}, err
	}
	if t.LeaseID == "" || !security.ConstantEqual(t.LeaseID, lease.LeaseID) || t.LeaseOwner != lease.LeaseOwner {
		return Task{}, ErrLeaseInvalid
	}
	if t.Status == "succeeded" || t.Status == "failed" || t.Status == "cancelled" {
		stored, _ := CanonicalJSON(t.Result)
		if t.Status == status && t.ErrorCode == code && bytes.Equal(stored, canonical) {
			return t, tx.Commit(ctx)
		}
		return Task{}, ErrTerminalConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT lease_until>clock_timestamp() FROM tasks WHERE id=$1", t.ID).Scan(&valid); err != nil || !valid {
		return Task{}, ErrLeaseInvalid
	}
	if t.Status == "uncertain" {
		return Task{}, ErrTerminalConflict
	}
	if status == "failed" || status == "cancelled" {
		if err = refundTask(ctx, tx, t); err != nil {
			return Task{}, err
		}
	}
	attemptStatus := status
	if attemptStatus == "cancelled" {
		attemptStatus = "failed"
	}
	if _, err = tx.Exec(ctx, `UPDATE task_attempts SET status=$2,error_code=$3,updated_at=now() WHERE task_id=$1 AND status IN ('reserved','submitting','submitted')`, t.ID, attemptStatus, code); err != nil {
		return Task{}, err
	}
	t, err = scanTask(tx.QueryRow(ctx, `UPDATE tasks SET status=$2,result_json=$3,error_code=$4,completed_at=now(),updated_at=now() WHERE id=$1 RETURNING `+taskColumns, t.ID, status, canonical, code))
	if err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Cancellation is immediate only before submission. In-flight work keeps its
// lease so an actual outcome can be reconciled without a duplicate refund.
func (s *Store) CancelTask(ctx context.Context, userID, id string) (Task, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND user_id=$2 FOR UPDATE", id, userID))
	if err != nil {
		return Task{}, err
	}
	if t.Status == "succeeded" || t.Status == "failed" {
		return Task{}, ErrTerminalConflict
	}
	if t.Status == "cancelled" {
		return t, tx.Commit(ctx)
	}
	if t.Status == "queued" {
		if err = refundTask(ctx, tx, t); err != nil {
			return Task{}, err
		}
		_, err = tx.Exec(ctx, "UPDATE tasks SET status='cancelled',cancel_requested_at=now(),completed_at=now(),updated_at=now() WHERE id=$1", id)
	} else {
		_, err = tx.Exec(ctx, "UPDATE tasks SET cancel_requested_at=COALESCE(cancel_requested_at,now()),updated_at=now() WHERE id=$1", id)
	}
	if err != nil {
		return Task{}, err
	}
	t, err = scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1", id))
	if err != nil {
		return Task{}, err
	}
	return t, tx.Commit(ctx)
}
