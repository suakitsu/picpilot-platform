package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func (s *Store) WaitForWorker(ctx context.Context, lease Task, jobID string, delay time.Duration) error {
	if delay < time.Second || delay > time.Minute {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockRunningTask(ctx, tx, lease, true); err != nil {
		return err
	}
	var scope, provider, model string
	err = tx.QueryRow(ctx, `SELECT provider_scope,provider_id,model FROM worker_jobs WHERE task_id=$1 AND job_id=$2`, lease.ID, jobID).Scan(&scope, &provider, &model)
	if err != nil {
		return normalizeError(err)
	}
	r, err := tx.Exec(ctx, `UPDATE tasks SET status='waiting_worker',provider_scope=$2,provider_id=$3,model=$4,lease_owner=NULL,lease_id=NULL,lease_until=NULL,next_run_at=clock_timestamp()+$5*interval '1 second',updated_at=clock_timestamp() WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=$1 AND status IN ('reserved','submitting','submitted','uncertain'))`, lease.ID, scope, provider, model, int64(delay/time.Second))
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

// RecordWorkerReceipt joins a terminal protocol job to the ordinary attempt
// history exactly once. It never charges/refunds; finishTask owns settlement.
func (s *Store) RecordWorkerReceipt(ctx context.Context, lease Task, jobID string) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var scope, provider, protocol, model, status, code string
	var leased bool
	err = tx.QueryRow(ctx, `SELECT provider_scope,provider_id,protocol,model,status,error_code,leased_at IS NOT NULL FROM worker_jobs WHERE task_id=$1 AND job_id=$2 FOR SHARE`, lease.ID, jobID).Scan(&scope, &provider, &protocol, &model, &status, &code, &leased)
	if err != nil {
		return "", normalizeError(err)
	}
	if err = lockRunningTask(ctx, tx, lease, true); err != nil {
		return "", err
	}
	result := status
	if status == "cancelled" && code == "LOCAL_WORKER_OFFLINE" {
		result = "failed"
	}
	if status == "expired" || status == "cancelled" && leased {
		result = "uncertain"
	}
	if status == "cancelled" && !leased {
		result = "failed"
	}
	if result != "failed" && result != "succeeded" && result != "uncertain" {
		return "", ErrConflict
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM task_attempts WHERE task_id=$1 AND metadata->>'worker_job_id'=$2`, lease.ID, jobID).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var number int
	if err = tx.QueryRow(ctx, `UPDATE tasks SET attempt=attempt+1,provider_scope=$2,provider_id=$3,model=$4,updated_at=clock_timestamp() WHERE id=$1 RETURNING attempt`, lease.ID, scope, provider, model).Scan(&number); err != nil {
		return "", err
	}
	id := security.NewID("attempt")
	meta, _ := json.Marshal(map[string]string{"worker_job_id": jobID})
	_, err = tx.Exec(ctx, `INSERT INTO task_attempts(id,task_id,attempt,provider_scope,provider_id,protocol,model,status,upstream_task_id,request_hash,error_code,retryable,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, lease.ID, number, scope, provider, protocol, model, result, jobID, lease.RequestHash, code, result == "failed", meta)
	if err != nil {
		return "", normalizeError(err)
	}
	if result == "uncertain" {
		if _, err = tx.Exec(ctx, `UPDATE tasks SET status='uncertain',error_code='WORKER_OUTCOME_UNKNOWN',updated_at=clock_timestamp() WHERE id=$1`, lease.ID); err != nil {
			return "", err
		}
	}
	return id, tx.Commit(ctx)
}
