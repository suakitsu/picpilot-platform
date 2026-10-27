package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func adminTaskWhere(f AdminFilter) adminWhere {
	q := adminWhere{parts: []string{"t.deleted_at IS NULL"}}
	q.search([]string{"t.id", "t.project_id", "t.kind", "t.provider_id", "t.model"}, f.Keyword)
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	q.exact("t.user_id", f.UserID)
	status := f.Status
	if status == "success" {
		status = "succeeded"
	}
	q.exact("t.status", status)
	q.exact("t.provider_id", f.Provider)
	q.exact("t.model", f.Model)
	q.exact("t.kind", f.ImageType)
	q.exact("t.capability", f.MediaType)
	q.dates("t.created_at", f)
	return q
}

const adminTaskSelection = `jsonb_build_object('task_id',t.id,'id',t.id,'user_id',t.user_id,'user_email',u.email,'user_account',u.email,'user_email_masked',` + adminMaskedEmail + `,
 'user_nickname',u.nickname,'project_id',t.project_id,'image_type',t.kind,'media_type',t.capability,
 'status',CASE WHEN t.status='succeeded' THEN 'success' ELSE t.status END,'internal_status',t.status,
 'credits_cost',t.credits_cost,'actual_cost',(SELECT sum(c.amount_cny) FROM admin_task_costs c WHERE c.task_id=t.id),
 'cost_known',EXISTS(SELECT 1 FROM admin_task_costs c WHERE c.task_id=t.id),'provider',t.provider_id,'model',t.model,
 'safe_error',CASE WHEN t.error_code ~ '^[A-Z0-9_]{0,80}$' THEN t.error_code ELSE 'UPSTREAM_ERROR' END,
 'artifact_id',(SELECT a.id FROM assets a WHERE a.id=t.result_json->>'artifact_id' AND a.user_id=t.user_id AND a.deleted_at IS NULL),
 'created_at',t.created_at,'updated_at',t.updated_at,'completed_at',t.completed_at,
 'refunded_at',(SELECT l.created_at FROM credit_ledger l WHERE l.user_id=t.user_id AND l.biz_type='generation_refund' AND l.biz_id=t.id))`

func (s *Store) AdminTasks(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	return adminPaged(ctx, tx, f, "tasks t JOIN users u ON u.id=t.user_id", adminTaskSelection, "t.created_at DESC,t.id DESC", adminTaskWhere(f))
}
func (s *Store) AdminTaskDetail(ctx context.Context, actor, id, capability string, requestIDs ...string) (map[string]any, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	result, err := adminJSON(tx.QueryRow(ctx, "SELECT "+adminTaskSelection+" FROM tasks t JOIN users u ON u.id=t.user_id WHERE t.id=$1 AND t.deleted_at IS NULL AND ($2='' OR t.capability=$2)", id, capability))
	if err != nil {
		return nil, err
	}
	attempts, err := adminJSONRows(ctx, tx, `SELECT jsonb_build_object('id',a.id,'attempt',a.attempt,'provider',a.provider_id,'model',a.model,'protocol',a.protocol,'status',a.status,'success',a.status='succeeded','errorType',CASE WHEN a.error_code ~ '^[A-Z0-9_]{0,80}$' THEN a.error_code ELSE 'UPSTREAM_ERROR' END,'created_at',a.created_at,'updated_at',a.updated_at) FROM task_attempts a WHERE task_id=$1 ORDER BY attempt,id`, id)
	if err != nil {
		return nil, err
	}
	result["provider_attempts"] = attempts
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT request_json FROM tasks WHERE id=$1", id).Scan(&raw); err != nil {
		return nil, err
	}
	details, referenceIDs := adminTaskInputs(raw, result["media_type"].(string))
	refs, err := adminJSONRows(ctx, tx, `SELECT jsonb_build_object('id',a.id,'image_type',a.image_type,'content_type',a.content_type,'file_size',a.file_size,'width',a.width,'height',a.height,'created_at',a.created_at) FROM assets a JOIN tasks t ON t.user_id=a.user_id WHERE t.id=$1 AND a.deleted_at IS NULL AND a.id=ANY($2::text[]) ORDER BY a.created_at,a.id`, id, referenceIDs)
	if err != nil {
		return nil, err
	}
	ownedRefs := make([]string, 0, len(refs))
	for _, ref := range refs {
		ownedRefs = append(ownedRefs, ref["id"].(string))
	}
	details["reference_asset_ids"] = ownedRefs
	result["prompt_details"] = details
	snapshot, _ := json.Marshal(details)
	result["prompt_snapshot"] = string(snapshot)
	result["reference_images"] = refs
	project, err := adminJSON(tx.QueryRow(ctx, `SELECT jsonb_build_object('id',p.id,'product_name',p.product_name,'brand',p.brand,'country',p.country,'language',p.language) FROM projects p JOIN tasks t ON t.project_id=p.id AND t.user_id=p.user_id WHERE t.id=$1 AND p.deleted_at IS NULL`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	result["project"] = project
	requestID := ""
	if len(requestIDs) > 0 {
		requestID = requestIDs[0]
	}
	if err = adminAudit(ctx, tx, actor, "task_detail_view", "task", id, requestID, map[string]any{"capability": result["media_type"], "submitted_inputs_viewed": true}); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

// Only submitted scalar generation inputs are inspectable. Raw provider bodies,
// options, URLs, tool output, system prompts, headers and arbitrary metadata are not.
func adminTaskInputs(raw []byte, capability string) (map[string]any, []string) {
	out := map[string]any{"capability": capability}
	refs := []string{}
	var root map[string]json.RawMessage
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || json.Unmarshal(root[capability], &input) != nil {
		return out, refs
	}
	fields := []string{}
	switch capability {
	case "image":
		fields = []string{"prompt", "size", "quality", "output_format", "background", "moderation"}
	case "video":
		fields = []string{"prompt", "original_prompt", "model", "model_code", "mode", "aspect_ratio", "resolution"}
	case "text":
		fields = []string{"model", "protocol"}
	}
	for _, field := range fields {
		var value string
		if string(input[field]) != "null" && json.Unmarshal(input[field], &value) == nil && len(value) <= 320000 {
			out[field] = value
		}
	}
	if capability == "image" {
		_ = json.Unmarshal(input["reference_asset_ids"], &refs)
	}
	if capability == "video" {
		var items []struct {
			AssetID string `json:"asset_id"`
		}
		_ = json.Unmarshal(input["references"], &items)
		for _, item := range items {
			if item.AssetID != "" {
				refs = append(refs, item.AssetID)
			}
		}
		for _, field := range []string{"duration_seconds", "seed"} {
			var n int64
			if string(input[field]) != "null" && json.Unmarshal(input[field], &n) == nil {
				out[field] = n
			}
		}
		var audio bool
		if string(input["generate_audio"]) != "null" && json.Unmarshal(input["generate_audio"], &audio) == nil {
			out["generate_audio"] = audio
		}
	}
	if capability == "text" {
		messages := adminSubmittedMessages(input["messages"])
		var text string
		if json.Unmarshal(input["input"], &text) == nil && len(text) <= 320000 {
			messages = append(messages, map[string]string{"role": "user", "content": text})
		} else {
			messages = append(messages, adminSubmittedMessages(input["input"])...)
		}
		out["messages"] = messages
	}
	if len(refs) > 100 {
		refs = refs[:100]
	}
	return out, refs
}

func adminSubmittedMessages(raw json.RawMessage) []map[string]string {
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	out := []map[string]string{}
	if json.Unmarshal(raw, &messages) != nil {
		return out
	}
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(m.Content, &text) == nil {
			if len(text) <= 320000 {
				out = append(out, map[string]string{"role": "user", "content": text})
			}
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(m.Content, &parts) == nil {
			for _, part := range parts {
				if (part.Type == "text" || part.Type == "input_text") && len(part.Text) <= 320000 {
					out = append(out, map[string]string{"role": "user", "content": part.Text})
				}
			}
		}
	}
	return out
}

func adminTaskSafe(ctx context.Context, tx pgx.Tx, id string) error {
	var unsafe bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE task_id=$1 AND status IN ('reserved','submitting','submitted','uncertain')) OR EXISTS(SELECT 1 FROM worker_jobs WHERE task_id=$1 AND status IN ('queued','leased','expired'))`, id).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return ErrUnsafeRetry
	}
	return nil
}

func adminTaskRefunded(ctx context.Context, tx pgx.Tx, t Task) error {
	if t.CreditsCost == 0 {
		return nil
	}
	var settled bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_ledger WHERE user_id=$1 AND biz_type='generation_charge' AND biz_id=$2 AND change_amount=-$3::bigint) AND EXISTS(SELECT 1 FROM credit_ledger WHERE user_id=$1 AND biz_type='generation_refund' AND biz_id=$2 AND change_amount=$3)`, t.UserID, t.ID, t.CreditsCost).Scan(&settled)
	if err != nil {
		return err
	}
	if !settled {
		return ErrUnsafeRetry
	}
	return nil
}

// A retry is a new task with its own charge/refund identity. The failed parent
// keeps its ledger history, and can have exactly one replacement.
func (s *Store) AdminRetryImageTask(ctx context.Context, actor, id, requestID string) (Task, bool, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback(context.Background())
	var retryID string
	err = tx.QueryRow(ctx, `SELECT retry_task_id FROM admin_task_retries WHERE original_task_id=$1`, id).Scan(&retryID)
	if err == nil {
		t, e := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1", retryID))
		return t, true, e
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Task{}, false, err
	}
	old, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND deleted_at IS NULL", id))
	if err != nil {
		return Task{}, false, err
	}
	if old.Capability != "image" || old.Status != "failed" {
		return Task{}, false, ErrUnsafeRetry
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT status='active' FROM users WHERE id=$1 FOR UPDATE", old.UserID).Scan(&active); err != nil {
		return Task{}, false, err
	}
	if !active {
		return Task{}, false, ErrDisabled
	}
	// NOWAIT avoids a lock inversion with a still-finishing task callback.
	old, err = scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE NOWAIT", id))
	if SQLState(err) == "55P03" {
		return Task{}, false, ErrConflict
	}
	if err != nil {
		return Task{}, false, err
	}
	if old.Status != "failed" {
		return Task{}, false, ErrUnsafeRetry
	}
	if err = adminTaskSafe(ctx, tx, id); err != nil {
		return Task{}, false, err
	}
	if err = adminTaskRefunded(ctx, tx, old); err != nil {
		return Task{}, false, err
	}
	t, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: old.UserID, ProjectID: old.ProjectID, Capability: old.Capability, Kind: old.Kind, Title: old.Title, Request: old.Request, CreditsCost: old.CreditsCost, IdempotencyKey: "admin-retry:" + id})
	if err != nil {
		return Task{}, false, err
	}
	if old.ProjectID != "" {
		var payload struct {
			Image struct {
				Prompt            string   `json:"prompt"`
				ReferenceAssetIDs []string `json:"reference_asset_ids"`
			} `json:"image"`
		}
		if json.Unmarshal(old.Request, &payload) != nil {
			return Task{}, false, ErrConflict
		}
		if err = s.ReplaceCreativeTaskTx(ctx, tx, old.UserID, old.ProjectID, id, t.ID, payload.Image.ReferenceAssetIDs, payload.Image.Prompt); err != nil {
			return Task{}, false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE projects SET status='images_queued',updated_at=now() WHERE id=$1 AND user_id=$2`, old.ProjectID, old.UserID); err != nil {
			return Task{}, false, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_task_retries(original_task_id,retry_task_id,actor_id) VALUES($1,$2,$3)`, id, t.ID, actor); err != nil {
		return Task{}, false, err
	}
	if err = adminAudit(ctx, tx, actor, "image_task_retry", "image_task", id, requestID, map[string]any{"retry_task_id": t.ID, "credits_cost": t.CreditsCost}); err != nil {
		return Task{}, false, err
	}
	return t, false, tx.Commit(ctx)
}

func (s *Store) AdminDeleteImageTask(ctx context.Context, actor, id, requestID string) error {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=$1 AND capability='image' AND deleted_at IS NULL FOR UPDATE", id))
	if err != nil {
		return err
	}
	if t.Status != "failed" && t.Status != "succeeded" && t.Status != "cancelled" {
		return ErrUnsafeRetry
	}
	if err = adminTaskSafe(ctx, tx, id); err != nil {
		return err
	}
	if t.Status != "succeeded" {
		if err = adminTaskRefunded(ctx, tx, t); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE tasks SET deleted_at=now(),updated_at=now() WHERE id=$1", id); err != nil {
		return err
	}
	if err = adminAudit(ctx, tx, actor, "image_task_delete", "image_task", id, requestID, map[string]any{"status": t.Status}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
