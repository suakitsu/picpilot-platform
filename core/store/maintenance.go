package store

import (
	"context"
	"time"
)

func (s *Store) WorkerRecoveryScopes(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT provider_scope FROM worker_jobs WHERE status IN ('queued','leased','expired') ORDER BY provider_scope LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, rows.Err()
}

// Candidates are never exposed as user assets. An accepted receipt promotes
// the row in the same transaction, so this cannot collect accepted output.
func (s *Store) QueueAbandonedWorkerAssets(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `WITH candidates AS (
 SELECT id FROM assets a WHERE image_type='worker-candidate'
 AND created_at<clock_timestamp()-interval '24 hours'
 AND NOT EXISTS(SELECT 1 FROM worker_artifacts w WHERE w.asset_id=a.id)
 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 100
), removed AS (
 DELETE FROM assets a USING candidates c WHERE a.id=c.id RETURNING a.storage_key
) INSERT INTO asset_deletion_queue(storage_key) SELECT storage_key FROM removed ON CONFLICT DO NOTHING`)
	return err
}

func (s *Store) PendingAssetDeletion(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT storage_key FROM asset_deletion_queue ORDER BY created_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (s *Store) AcknowledgeAssetDeletion(ctx context.Context, key string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM asset_deletion_queue WHERE storage_key=$1`, key)
	return err
}

func (s *Store) PurgeExpiredAuthStateBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM auth_challenges WHERE id IN (SELECT id FROM auth_challenges WHERE expires_at<$1 LIMIT 1000)`, cutoff)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM oauth_states WHERE id IN (SELECT id FROM oauth_states WHERE expires_at<$1 LIMIT 1000)`, cutoff)
	return err
}
