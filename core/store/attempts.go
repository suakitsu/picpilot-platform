package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

var ErrProviderBusy = errors.New("provider is at capacity")

type Attempt struct {
	Provider       Provider `json:"-"`
	ID             string
	TaskID         string
	Number         int
	ProviderScope  string
	ProviderID     string
	Protocol       string
	Model          string
	Status         string
	UpstreamTaskID string
}

func (s *Store) ReserveAttempt(ctx context.Context, lease Task, scope, providerID string) (Attempt, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 FOR UPDATE", lease.ID))
	if err != nil {
		return Attempt{}, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT lease_until>clock_timestamp() FROM tasks WHERE id=$1", t.ID).Scan(&valid); err != nil {
		return Attempt{}, err
	}
	if !valid || t.LeaseOwner != lease.LeaseOwner || !security.ConstantEqual(t.LeaseID, lease.LeaseID) || t.Status != "running" || t.CancelRequestedAt != nil {
		return Attempt{}, ErrLeaseInvalid
	}
	expectedScope := "platform"
	if s.Edition == config.Personal {
		expectedScope = t.UserID
	}
	if scope != expectedScope {
		return Attempt{}, ErrNotFound
	}
	p, err := scanProvider(tx.QueryRow(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 AND id=$2 FOR UPDATE", scope, providerID))
	if err != nil {
		return Attempt{}, err
	}
	if !p.Enabled || p.Capability != t.Capability {
		return Attempt{}, ErrConflict
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM task_attempts WHERE provider_scope=$1 AND provider_id=$2 AND status IN ('reserved','submitting','submitted','uncertain')`, scope, providerID).Scan(&count); err != nil {
		return Attempt{}, err
	}
	if count >= p.MaxConcurrency {
		return Attempt{}, ErrProviderBusy
	}
	if err := providerCircuitReady(ctx, tx, p); err != nil {
		return Attempt{}, err
	}
	a := Attempt{Provider: p, ID: security.NewID("attempt"), TaskID: t.ID, Number: t.Attempt + 1, ProviderScope: scope, ProviderID: providerID, Protocol: p.Protocol, Model: p.Model, Status: "reserved"}
	if _, err = tx.Exec(ctx, `INSERT INTO task_attempts(id,task_id,attempt,provider_scope,provider_id,protocol,model,status,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,'reserved',$8)`, a.ID, t.ID, a.Number, scope, providerID, p.Protocol, p.Model, t.RequestHash); err != nil {
		return Attempt{}, normalizeError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET attempt=$2,provider_scope=$3,provider_id=$4,model=$5,updated_at=now() WHERE id=$1`, t.ID, a.Number, scope, providerID, p.Model); err != nil {
		return Attempt{}, err
	}
	return a, tx.Commit(ctx)
}

// Persist the submission boundary before sending any paid HTTP request. After
// this point, a crash cannot be treated as a safe retry without upstream evidence.
func (s *Store) BeginSubmission(ctx context.Context, lease Task, attemptID string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockRunningTask(ctx, tx, lease, false); err != nil {
		return err
	}
	r, err := tx.Exec(ctx, `UPDATE task_attempts SET status='submitting',submitted_at=clock_timestamp(),updated_at=now() WHERE id=$1 AND task_id=$2 AND status='reserved'`, attemptID, lease.ID)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrLeaseInvalid
	}
	return tx.Commit(ctx)
}

// ResolveAttempt requires a definitive upstream outcome. Ambiguous timeouts use
// MarkUncertain instead and never open a second paid route automatically.
func (s *Store) RejectAttempt(ctx context.Context, lease Task, attemptID, code string, retryable bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockRunningTask(ctx, tx, lease, true); err != nil {
		return err
	}
	r, err := tx.Exec(ctx, `UPDATE task_attempts SET status='failed',error_code=$3,retryable=$4,updated_at=now() WHERE id=$1 AND task_id=$2 AND status IN ('reserved','submitting','submitted')`, attemptID, lease.ID, code, retryable)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrLeaseInvalid
	}
	return tx.Commit(ctx)
}

func lockRunningTask(ctx context.Context, tx pgx.Tx, lease Task, allowCancellation bool) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM tasks WHERE id=$1 AND lease_owner=$2 AND lease_id=$3 AND lease_until>clock_timestamp() AND status='running' AND ($4 OR cancel_requested_at IS NULL) FOR UPDATE`, lease.ID, lease.LeaseOwner, lease.LeaseID, allowCancellation).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseInvalid
	}
	if err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT lease_until>clock_timestamp() FROM tasks WHERE id=$1", lease.ID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrLeaseInvalid
	}
	return nil
}

func (s *Store) MarkUncertain(ctx context.Context, lease Task, attemptID, code string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var id string
	err = tx.QueryRow(ctx, `UPDATE tasks SET status='uncertain',error_code=$4,updated_at=now() WHERE id=$1 AND lease_owner=$2 AND lease_id=$3 AND lease_until>now() AND status='running' RETURNING id`, lease.ID, lease.LeaseOwner, lease.LeaseID, code).Scan(&id)
	if err != nil {
		return ErrLeaseInvalid
	}
	r, err := tx.Exec(ctx, `UPDATE task_attempts SET status='uncertain',error_code=$3,updated_at=now() WHERE id=$1 AND task_id=$2 AND status IN ('submitting','submitted')`, attemptID, lease.ID, code)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

func (s *Store) ReleaseUnsubmittedTask(ctx context.Context, lease Task, delay time.Duration) error {
	if delay < 0 || delay > time.Hour {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockRunningTask(ctx, tx, lease, false); err != nil {
		return err
	}
	r, err := tx.Exec(ctx, `UPDATE tasks SET status='queued',lease_owner=NULL,lease_id=NULL,lease_until=NULL,next_run_at=now()+$4*interval '1 second',updated_at=now()
 WHERE id=$1 AND lease_owner=$2 AND lease_id=$3 AND lease_until>now() AND status='running' AND cancel_requested_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=$1 AND status IN ('reserved','submitting','submitted','uncertain'))`, lease.ID, lease.LeaseOwner, lease.LeaseID, int64(delay/time.Second))
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrLeaseInvalid
	}
	return tx.Commit(ctx)
}

func (s *Store) RecoverExpiredTaskLeases(ctx context.Context) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, "SELECT "+taskColumns+" FROM tasks WHERE status='running' AND lease_until<now() ORDER BY lease_until FOR UPDATE SKIP LOCKED LIMIT 100")
	if err != nil {
		return 0, err
	}
	var expired []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, t := range expired {
		var hasWorker bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM worker_jobs WHERE task_id=$1)`, t.ID).Scan(&hasWorker); err != nil {
			return 0, err
		}
		if hasWorker {
			var httpActive bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=$1 AND status IN ('reserved','submitting','submitted','uncertain') AND NOT(metadata ? 'worker_job_id'))`, t.ID).Scan(&httpActive); err != nil {
				return 0, err
			}
			if !httpActive {
				if _, err = tx.Exec(ctx, `UPDATE tasks SET status='waiting_worker',lease_owner=NULL,lease_id=NULL,lease_until=NULL,next_run_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, t.ID); err != nil {
					return 0, err
				}
				continue
			}
		}
		var unknown bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=$1 AND status IN ('submitting','submitted','uncertain'))`, t.ID).Scan(&unknown); err != nil {
			return 0, err
		}
		if unknown {
			if _, err = tx.Exec(ctx, "UPDATE task_attempts SET status='uncertain',error_code='SUBMISSION_OUTCOME_UNKNOWN',updated_at=now() WHERE task_id=$1 AND status IN ('submitting','submitted','uncertain')", t.ID); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, "UPDATE tasks SET status='uncertain',error_code='SUBMISSION_OUTCOME_UNKNOWN',updated_at=now() WHERE id=$1", t.ID); err != nil {
				return 0, err
			}
			continue
		}
		if _, err = tx.Exec(ctx, "UPDATE task_attempts SET status='failed',error_code='LEASE_EXPIRED_BEFORE_SUBMISSION',retryable=true,updated_at=now() WHERE task_id=$1 AND status='reserved'", t.ID); err != nil {
			return 0, err
		}
		if t.CancelRequestedAt != nil {
			if err = refundTask(ctx, tx, t); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, "UPDATE tasks SET status='cancelled',completed_at=now(),updated_at=now() WHERE id=$1", t.ID); err != nil {
				return 0, err
			}
		} else {
			if _, err = tx.Exec(ctx, "UPDATE tasks SET status='queued',lease_owner=NULL,lease_id=NULL,lease_until=NULL,next_run_at=now(),updated_at=now() WHERE id=$1", t.ID); err != nil {
				return 0, err
			}
		}
	}
	return len(expired), tx.Commit(ctx)
}
