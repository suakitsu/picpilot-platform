package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/security"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

func workerArtifacts(ctx context.Context, tx pgx.Tx, id string) ([]wp.ArtifactRecord, error) {
	rows, err := tx.Query(ctx, `SELECT job_id,worker_id,lease_hash,idempotency_key,response_json FROM worker_artifacts WHERE job_id=$1 ORDER BY created_at,artifact_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []wp.ArtifactRecord{}
	for rows.Next() {
		var a wp.ArtifactRecord
		var response []byte
		if err = rows.Scan(&a.JobID, &a.WorkerID, &a.LeaseHash, &a.IdempotencyKey, &response); err != nil {
			return nil, err
		}
		if json.Unmarshal(response, &a.Response) != nil {
			return nil, wp.NewError("WORKER_INTERNAL_ERROR")
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// All authenticated operations recheck the token inside the mutation transaction.
// Job locks serialize callbacks, claims, expiry, cancellation, and receipts. Task
// SHARE locks linearize acceptance against business cancellation/deletion.
func workerAction[T any](ctx context.Context, s *Store, p WorkerPrincipal, jobID, workerID string, fn func(pgx.Tx, WorkerJob, time.Time) (wp.Job, T, error)) (T, error) {
	var zero T
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if err = workerTokenLock(ctx, tx, p); err != nil {
		return zero, err
	}
	if _, err = workerBinding(ctx, tx, p, workerID, false); err != nil {
		return zero, err
	}
	w, err := scanWorkerJob(tx.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.job_id=$1 AND j.provider_scope=$2 FOR UPDATE OF j`, jobID, p.OwnerScope))
	if errors.Is(err, ErrNotFound) {
		return zero, wp.NewError("JOB_NOT_FOUND")
	}
	if err != nil {
		return zero, err
	}
	live, err := workerBusinessActive(ctx, tx, w.TaskID)
	if err != nil {
		return zero, err
	}
	now, err := workerNow(ctx, tx)
	if err != nil {
		return zero, err
	}
	if !live {
		if next, changed := wp.Cancel(w.Job, now); changed {
			if err = saveWorkerJob(ctx, tx, next); err != nil {
				return zero, err
			}
			if err = tx.Commit(ctx); err != nil {
				return zero, err
			}
			return zero, wp.NewError("JOB_CANCELLED")
		}
	}
	w.Job.Artifacts, err = workerArtifacts(ctx, tx, jobID)
	if err != nil {
		return zero, err
	}
	next, result, err := fn(tx, w, now)
	if err != nil {
		var protocol *wp.ProtocolError
		if errors.As(err, &protocol) && protocol.Detail.Code == "LEASE_EXPIRED" {
			if expired, changed := wp.Expire(w.Job, now); changed {
				if e := saveWorkerJob(ctx, tx, expired); e != nil {
					return zero, e
				}
				if e := tx.Commit(ctx); e != nil {
					return zero, e
				}
			}
		}
		return zero, err
	}
	if err = saveWorkerJob(ctx, tx, next); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

func (s *Store) RenewWorkerJob(ctx context.Context, p WorkerPrincipal, jobID string, r wp.LeaseRequest, o WorkerOptions) (wp.LeaseResponse, error) {
	if err := r.Validate(); err != nil {
		return wp.LeaseResponse{}, err
	}
	return workerAction(ctx, s, p, jobID, r.WorkerID, func(tx pgx.Tx, w WorkerJob, now time.Time) (wp.Job, wp.LeaseResponse, error) {
		seconds := 120
		err := tx.QueryRow(ctx, `SELECT lease_seconds FROM providers WHERE owner_scope=$1 AND id=$2`, w.ProviderScope, w.Job.ProviderID).Scan(&seconds)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return w.Job, wp.LeaseResponse{}, err
		}
		return wp.Renew(w.Job, r, o.SigningKey, now, o.policy(seconds))
	})
}
func (s *Store) CompleteWorkerJob(ctx context.Context, p WorkerPrincipal, jobID string, r wp.CompleteRequest, o WorkerOptions) (wp.TerminalResponse, error) {
	if err := r.Validate(); err != nil {
		return wp.TerminalResponse{}, err
	}
	return workerAction(ctx, s, p, jobID, r.WorkerID, func(_ pgx.Tx, w WorkerJob, now time.Time) (wp.Job, wp.TerminalResponse, error) {
		return wp.Complete(w.Job, r, o.SigningKey, now)
	})
}
func (s *Store) FailWorkerJob(ctx context.Context, p WorkerPrincipal, jobID string, r wp.FailRequest, o WorkerOptions) (wp.TerminalResponse, error) {
	if err := r.Validate(); err != nil {
		return wp.TerminalResponse{}, err
	}
	return workerAction(ctx, s, p, jobID, r.WorkerID, func(_ pgx.Tx, w WorkerJob, now time.Time) (wp.Job, wp.TerminalResponse, error) {
		return wp.Fail(w.Job, r, o.SigningKey, now)
	})
}

// SaveWorkerArtifact replays existing receipts without storage writes. Concurrent
// first uploads may normalize independently; only one receipt can commit. The
// caller must discard its created asset on an error OR an idempotent response.
// File normalization is outside the job transaction: saveImage uses its own
// connection, and holding a pooled connection during it can exhaust the pool.
func (s *Store) SaveWorkerArtifact(ctx context.Context, p WorkerPrincipal, jobID string, r wp.ArtifactForm, source []byte, o WorkerOptions, create func(WorkerJob) (Asset, error)) (wp.ArtifactResponse, error) {
	if err := r.Validate(); err != nil {
		return wp.ArtifactResponse{}, err
	}
	if int64(len(source)) > wp.DefaultMaxArtifactBytes {
		return wp.ArtifactResponse{}, wp.NewError("ARTIFACT_INVALID")
	}
	type preflight struct {
		job    WorkerJob
		replay wp.ArtifactResponse
	}
	checked, err := workerAction(ctx, s, p, jobID, r.WorkerID, func(_ pgx.Tx, w WorkerJob, now time.Time) (wp.Job, preflight, error) {
		j := w.Job
		for _, a := range j.Artifacts {
			if a.IdempotencyKey == r.IdempotencyKey {
				next, out, err := wp.RecordArtifact(j, r, source, wp.ArtifactResponse{}, o.SigningKey, now, wp.DefaultMaxArtifactBytes)
				return next, preflight{replay: out}, err
			}
		}
		if err := wp.VerifyLease(j, wp.LeaseRequest{WorkerID: r.WorkerID, LeaseID: r.LeaseID}, o.SigningKey, now); err != nil {
			return j, preflight{}, err
		}
		if j.Capability != wp.Image {
			return j, preflight{}, wp.NewError("ARTIFACT_NOT_ALLOWED")
		}
		if err := wp.VerifyArtifactSource(r.SHA256, source, wp.DefaultMaxArtifactBytes); err != nil {
			return j, preflight{}, err
		}
		return j, preflight{job: w}, nil
	})
	if err != nil {
		return wp.ArtifactResponse{}, err
	}
	if checked.replay.ArtifactID != "" {
		return checked.replay, nil
	}
	if create == nil {
		return wp.ArtifactResponse{}, wp.NewError("WORKER_INTERNAL_ERROR")
	}
	asset, err := create(checked.job)
	if err != nil {
		return wp.ArtifactResponse{}, err
	}
	return workerAction(ctx, s, p, jobID, r.WorkerID, func(tx pgx.Tx, w WorkerJob, now time.Time) (wp.Job, wp.ArtifactResponse, error) {
		j := w.Job
		if asset.UserID != w.UserID || asset.TaskID != w.TaskID || asset.ProjectID != w.ProjectID {
			return j, wp.ArtifactResponse{}, wp.NewError("ARTIFACT_INVALID")
		}
		candidate := wp.ArtifactResponse{ArtifactID: security.NewID("awa"), ContentType: asset.ContentType, FileSize: asset.FileSize, SHA256: asset.SHA256, SourceSHA256: wp.SHA256(source), Width: asset.Width, Height: asset.Height, Format: strings.TrimPrefix(asset.ContentType, "image/")}
		next, response, err := wp.RecordArtifact(j, r, source, candidate, o.SigningKey, now, wp.DefaultMaxArtifactBytes)
		if err != nil {
			return j, wp.ArtifactResponse{}, err
		}
		if response.Idempotent {
			return next, response, nil
		}
		tag, err := tx.Exec(ctx, `UPDATE assets SET image_type='worker-output' WHERE id=$1 AND user_id=$2 AND task_id=$3 AND image_type IN ('worker-candidate','worker-output') AND deleted_at IS NULL`, asset.ID, w.UserID, w.TaskID)
		if err != nil {
			return j, wp.ArtifactResponse{}, err
		}
		if tag.RowsAffected() != 1 {
			return j, wp.ArtifactResponse{}, wp.NewError("ARTIFACT_INVALID")
		}
		data, _ := json.Marshal(response)
		_, err = tx.Exec(ctx, `INSERT INTO worker_artifacts(artifact_id,job_id,worker_id,lease_hash,idempotency_key,asset_id,response_json,source_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, response.ArtifactID, j.JobID, r.WorkerID, j.LeaseHash, r.IdempotencyKey, asset.ID, data, response.SourceSHA256)
		return next, response, err
	})
}

// DiscardWorkerArtifactAsset only removes an unreferenced worker-output row.
// The caller removes its file only when this returns true. Never guess after
// an uncertain database outcome; retain the file for later reconciliation.
func (s *Store) DiscardWorkerArtifactAsset(ctx context.Context, assetID string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM assets a WHERE a.id=$1 AND a.image_type IN ('worker-candidate','worker-output') AND NOT EXISTS(SELECT 1 FROM worker_artifacts w WHERE w.asset_id=a.id)`, assetID)
	return tag.RowsAffected() == 1, err
}

func (s *Store) WorkerArtifactAsset(ctx context.Context, taskID, artifactID string) (Asset, error) {
	var assetID string
	err := s.Pool.QueryRow(ctx, `SELECT a.asset_id FROM worker_artifacts a JOIN worker_jobs j ON j.job_id=a.job_id WHERE j.task_id=$1 AND a.artifact_id=$2`, taskID, artifactID).Scan(&assetID)
	if err != nil {
		return Asset{}, normalizeError(err)
	}
	return s.Asset(ctx, assetID)
}

func (s *Store) WorkerInputAsset(ctx context.Context, p WorkerPrincipal, jobID, workerID string, index int, expires int64, signature string, o WorkerOptions) (Asset, error) {
	return workerAction(ctx, s, p, jobID, workerID, func(tx pgx.Tx, w WorkerJob, now time.Time) (wp.Job, Asset, error) {
		j := w.Job
		if j.Protocol != wp.ImageEdits || index < 0 || index >= len(w.InputSources) {
			return j, Asset{}, wp.NewError("INPUT_NOT_FOUND")
		}
		// Only the count is used by VerifyInputAccess; these never go on the wire.
		j.InputFiles = make([]wp.InputFile, len(w.InputSources))
		ref := w.InputSources[index]
		if err := wp.VerifyInputAccess(j, workerID, index, expires, signature, ref.SourceURL, o.SigningKey, now); err != nil {
			return j, Asset{}, err
		}
		a, err := scanAsset(tx.QueryRow(ctx, "SELECT "+assetColumns+" FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR SHARE", ref.AssetID, w.UserID))
		if errors.Is(err, ErrNotFound) {
			err = wp.NewError("INPUT_NOT_FOUND")
		}
		return j, a, err
	})
}
