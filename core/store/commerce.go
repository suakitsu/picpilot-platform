package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
)

type CommerceBatch struct {
	Project    Project
	References []string
	Images     []commerce.PlannedImage
	Key        string
	Identity   json.RawMessage
}

func (s *Store) SubmitCommerceBatch(ctx context.Context, in CommerceBatch) (WorkflowResult, bool, error) {
	if (in.Project.SourceType != "commerce_suite" && in.Project.SourceType != "image_translation") || len(in.Images) == 0 || len(in.Images) > 64 || len(in.References) > 10 {
		return WorkflowResult{}, false, ErrConflict
	}
	var total int64
	for _, im := range in.Images {
		if im.CreditsCost < 0 || im.CreditsCost > 1000000 || im.Payload.Validate("image") != nil || creative.ValidateFields(im.Payload) != nil || im.Payload.Snapshot == nil || im.Payload.Snapshot["taskKind"] != in.Project.SourceType {
			return WorkflowResult{}, false, ErrConflict
		}
		total += im.CreditsCost
	}
	if s.Edition == config.Personal && total != 0 {
		return WorkflowResult{}, false, ErrConflict
	}
	return s.CreativeWrite(ctx, in.Project.UserID, in.Key, in.Identity, func(tx pgx.Tx) (WorkflowResult, error) {
		p, err := s.CreateProjectTx(ctx, tx, in.Project)
		if err != nil {
			return WorkflowResult{}, err
		}
		out := WorkflowResult{ProjectID: p.ID, TaskIDs: []string{}}
		if err = s.LinkProjectAssetsTx(ctx, tx, p.UserID, p.ID, in.References, "reference"); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO commerce_batches(project_id,user_id,kind,credits_cost) VALUES($1,$2,$3,$4)`, p.ID, p.UserID, p.SourceType, total); err != nil {
			return out, err
		}
		if total > 0 {
			if _, err = adjustCredits(ctx, tx, p.UserID, -total, p.SourceType+"_generate", p.ID, "Image batch generation", ""); err != nil {
				return out, err
			}
		}
		allowed := map[string]bool{}
		for _, id := range in.References {
			allowed[id] = true
		}
		for i, im := range in.Images {
			im.Payload.Snapshot = commerce.CopyObject(im.Payload.Snapshot)
			for _, id := range im.Payload.Image.ReferenceAssetIDs {
				if !allowed[id] {
					return out, ErrNotFound
				}
			}
			if p.SourceType == "image_translation" && len(im.Payload.Image.ReferenceAssetIDs) != 1 {
				return out, ErrConflict
			}
			im.Payload.Snapshot["projectId"] = p.ID
			// The batch owns one aggregate debit. Children retain their allocated
			// cost but must not create a second generation_charge receipt.
			t, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: p.UserID, ProjectID: p.ID, Capability: "image", Kind: im.Slot, Title: im.Name, Request: creative.JSON(im.Payload), IdempotencyKey: fmt.Sprintf("%s:%d", in.Key, i), CreditsCost: im.CreditsCost, SkipCharge: true})
			if err != nil {
				return out, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO commerce_batch_items(task_id,project_id,user_id,position,credits_cost,aggregate_charge) VALUES($1,$2,$3,$4,$5,true)`, t.ID, p.ID, p.UserID, i, im.CreditsCost); err != nil {
				return out, err
			}
			out.TaskIDs = append(out.TaskIDs, t.ID)
		}
		return out, nil
	})
}

// commerceTaskRefunded is the admin retry proof for a legacy aggregate-charged member.
// It is read-only: checking retry safety must never create a refund.
func commerceTaskRefunded(ctx context.Context, tx pgx.Tx, t Task) (bool, error) {
	var member, settled bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM commerce_batch_items WHERE task_id=$1 AND aggregate_charge),
 EXISTS(SELECT 1 FROM commerce_batch_items i JOIN commerce_batches b ON b.project_id=i.project_id
 JOIN credit_ledger c ON c.user_id=b.user_id AND c.biz_type=b.kind||'_generate' AND c.biz_id=b.project_id AND c.change_amount=-b.credits_cost
 JOIN credit_ledger r ON r.user_id=i.user_id AND r.biz_type='generation_refund' AND r.biz_id=i.task_id AND r.change_amount=i.credits_cost
 WHERE i.task_id=$1 AND i.user_id=$2 AND i.credits_cost=$3 AND i.aggregate_charge)`, t.ID, t.UserID, t.CreditsCost).Scan(&member, &settled)
	if err != nil {
		return true, err
	}
	if !member {
		return false, nil
	}
	if t.CreditsCost == 0 || settled {
		return true, nil
	}
	return true, ErrUnsafeRetry
}

// linkCommerceRetryTx is called by ReplaceCreativeTaskTx so generic/admin retry
// paths retain business membership without inflating linked output counts.
func linkCommerceRetryTx(ctx context.Context, tx pgx.Tx, user, oldID, newID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO commerce_batch_items(task_id,project_id,user_id,position,credits_cost,aggregate_charge,replaces_task_id)
 SELECT t.id,i.project_id,i.user_id,i.position,t.credits_cost,false,i.task_id FROM commerce_batch_items i JOIN tasks t ON t.id=$3 AND t.user_id=i.user_id AND t.project_id=i.project_id
 WHERE i.task_id=$2 AND i.user_id=$1 ON CONFLICT(task_id) DO NOTHING`, user, oldID, newID)
	return normalizeError(err)
}

// refundCommerceTask is an optional shared refundTask hook for legacy batches.
// New batches and retries use standard per-task charges and fall through.
// Legacy receipts verify the original debit before using the task refund key.
func refundCommerceTask(ctx context.Context, tx pgx.Tx, t Task) (bool, error) {
	var cost, total int64
	var kind, project string
	err := tx.QueryRow(ctx, `SELECT i.credits_cost,b.credits_cost,b.kind,b.project_id FROM commerce_batch_items i JOIN commerce_batches b ON b.project_id=i.project_id WHERE i.task_id=$1 AND i.user_id=$2 AND i.project_id=$3 AND i.aggregate_charge`, t.ID, t.UserID, t.ProjectID).Scan(&cost, &total, &kind, &project)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if cost != t.CreditsCost || cost > total {
		return true, ErrConflict
	}
	if cost == 0 {
		return true, nil
	}
	var charged, allocated int64
	if err = tx.QueryRow(ctx, "SELECT change_amount FROM credit_ledger WHERE user_id=$1 AND biz_type=$2 AND biz_id=$3", t.UserID, kind+"_generate", project).Scan(&charged); err != nil {
		return true, err
	}
	if err = tx.QueryRow(ctx, "SELECT COALESCE(sum(credits_cost),0) FROM commerce_batch_items WHERE project_id=$1 AND aggregate_charge", project).Scan(&allocated); err != nil {
		return true, err
	}
	if charged != -total || allocated != total {
		return true, ErrConflict
	}
	_, err = adjustCredits(ctx, tx, t.UserID, cost, "generation_refund", t.ID, "Image batch member did not complete", "")
	return true, err
}

func (s *Store) CommerceBatchCost(ctx context.Context, user, project string) (int64, error) {
	var cost int64
	err := s.Pool.QueryRow(ctx, "SELECT credits_cost FROM commerce_batches WHERE project_id=$1 AND user_id=$2", project, user).Scan(&cost)
	return cost, normalizeError(err)
}

func (s *Store) RetryCommerceTask(ctx context.Context, user, id, key string, identity json.RawMessage, payload commerce.Payload, cost int64) (WorkflowResult, bool, error) {
	return s.CreativeWrite(ctx, user, key, identity, func(tx pgx.Tx) (WorkflowResult, error) {
		old, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE NOWAIT", id, user))
		if SQLState(err) == "55P03" {
			return WorkflowResult{}, ErrConflict
		}
		if err != nil {
			return WorkflowResult{}, err
		}
		if old.Status != "failed" && old.Status != "cancelled" && old.Status != "succeeded" {
			return WorkflowResult{}, ErrConflict
		}
		p, err := s.ProjectTx(ctx, tx, user, old.ProjectID)
		if err != nil {
			return WorkflowResult{}, err
		}
		var position int
		if err = tx.QueryRow(ctx, "SELECT position FROM commerce_batch_items WHERE task_id=$1 AND user_id=$2", id, user).Scan(&position); err != nil {
			return WorkflowResult{}, normalizeError(err)
		}
		if payload.Validate("image") != nil {
			return WorkflowResult{}, ErrConflict
		}
		if err = s.LinkProjectAssetsTx(ctx, tx, user, p.ID, payload.Image.ReferenceAssetIDs, "reference"); err != nil {
			return WorkflowResult{}, err
		}
		t, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: user, ProjectID: p.ID, Capability: "image", Kind: old.Kind, Title: old.Title, Request: creative.JSON(payload), RequestIdentity: identity, IdempotencyKey: key, CreditsCost: cost})
		if err != nil {
			return WorkflowResult{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO commerce_batch_items(task_id,project_id,user_id,position,credits_cost,aggregate_charge,replaces_task_id) VALUES($1,$2,$3,$4,$5,false,$6)`, t.ID, p.ID, user, position, cost, id); err != nil {
			return WorkflowResult{}, normalizeError(err)
		}
		if err = s.ReplaceCreativeTaskTx(ctx, tx, user, p.ID, id, t.ID, payload.Image.ReferenceAssetIDs, payload.Image.Prompt); err != nil {
			return WorkflowResult{}, err
		}
		p.Status = "images_queued"
		if err = s.UpdateProjectTx(ctx, tx, p); err != nil {
			return WorkflowResult{}, err
		}
		return WorkflowResult{ProjectID: p.ID, TaskIDs: []string{t.ID}}, nil
	})
}

// Only inputs newly created by this request qualify; linked or shared inputs
// are never removed. Physical removal uses the existing durable cleanup queue.
func (s *Store) DiscardCommerceUploads(ctx context.Context, user string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `WITH removed AS (
 DELETE FROM assets a WHERE a.user_id=$1 AND a.id=ANY($2) AND a.image_type='commerce-upload' AND a.project_id IS NULL AND a.task_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM project_asset_refs r WHERE r.asset_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM tasks t WHERE t.request_json->'image'->'reference_asset_ids' ? a.id)
 RETURNING storage_key
) INSERT INTO asset_deletion_queue(storage_key) SELECT storage_key FROM removed ON CONFLICT DO NOTHING`, user, ids)
	return err
}
