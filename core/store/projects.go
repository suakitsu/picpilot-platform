package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type Project struct {
	ID             string          `json:"id"`
	UserID         string          `json:"-"`
	SourceType     string          `json:"source_type"`
	ProductName    string          `json:"product_name"`
	Brand          string          `json:"brand"`
	Country        string          `json:"country"`
	Language       string          `json:"language"`
	Status         string          `json:"status"`
	OriginalInfo   json.RawMessage `json:"original_info"`
	GeneratedCopy  json.RawMessage `json:"generated_copy"`
	ConfirmedFacts json.RawMessage `json:"confirmed_facts"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

const projectColumns = `id,user_id,source_type,product_name,brand,country,language,status,original_info,generated_copy,confirmed_facts,metadata,created_at,updated_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.UserID, &p.SourceType, &p.ProductName, &p.Brand, &p.Country, &p.Language, &p.Status, &p.OriginalInfo, &p.GeneratedCopy, &p.ConfirmedFacts, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
	return p, normalizeError(err)
}

func (s *Store) Project(ctx context.Context, user, id string) (Project, error) {
	return scanProject(s.Pool.QueryRow(ctx, "SELECT "+projectColumns+" FROM projects WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL", user, id))
}

func (s *Store) ProjectTx(ctx context.Context, tx pgx.Tx, user, id string) (Project, error) {
	return scanProject(tx.QueryRow(ctx, "SELECT "+projectColumns+" FROM projects WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", user, id))
}

func (s *Store) Projects(ctx context.Context, user string) ([]Project, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+projectColumns+" FROM projects WHERE user_id=$1 AND deleted_at IS NULL ORDER BY updated_at DESC,id LIMIT 500", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateProjectTx(ctx context.Context, tx pgx.Tx, p Project) (Project, error) {
	if p.ID == "" {
		p.ID = security.NewID("prod")
	}
	if p.SourceType == "" {
		p.SourceType = "listing"
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	for _, v := range []*json.RawMessage{&p.OriginalInfo, &p.GeneratedCopy, &p.ConfirmedFacts, &p.Metadata} {
		if len(*v) == 0 {
			*v = json.RawMessage(`{}`)
		}
	}
	return scanProject(tx.QueryRow(ctx, `INSERT INTO projects(id,user_id,source_type,product_name,brand,country,language,status,original_info,generated_copy,confirmed_facts,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+projectColumns, p.ID, p.UserID, p.SourceType, p.ProductName, p.Brand, p.Country, p.Language, p.Status, p.OriginalInfo, p.GeneratedCopy, p.ConfirmedFacts, p.Metadata))
}

func (s *Store) UpdateProjectTx(ctx context.Context, tx pgx.Tx, p Project) error {
	tag, err := tx.Exec(ctx, `UPDATE projects SET status=$3,original_info=$4,generated_copy=$5,confirmed_facts=$6,metadata=$7,updated_at=now() WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, p.ID, p.UserID, p.Status, p.OriginalInfo, p.GeneratedCopy, p.ConfirmedFacts, p.Metadata)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

type WorkflowResult struct {
	ProjectID string   `json:"project_id,omitempty"`
	AplusID   string   `json:"aplus_project_id,omitempty"`
	ChatID    string   `json:"chat_id,omitempty"`
	VersionNo int      `json:"version_no,omitempty"`
	TaskIDs   []string `json:"task_ids,omitempty"`
}

// CreativeWrite serializes an owner's multi-task business operation, including
// request idempotency, project/chat writes, task inserts and credit charges.
// Identity is only hashed. Uploaded data and signed URLs are never stored here.
func (s *Store) CreativeWrite(ctx context.Context, user, key string, identity json.RawMessage, fn func(pgx.Tx) (WorkflowResult, error)) (WorkflowResult, bool, error) {
	if user == "" || key == "" || len(key) > 200 {
		return WorkflowResult{}, false, ErrConflict
	}
	canonical, err := CanonicalJSON(identity)
	if err != nil {
		return WorkflowResult{}, false, err
	}
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return WorkflowResult{}, false, err
	}
	defer tx.Rollback(context.Background())
	var status string
	if err = tx.QueryRow(ctx, "SELECT status FROM users WHERE id=$1 FOR UPDATE", user).Scan(&status); err != nil {
		return WorkflowResult{}, false, normalizeError(err)
	}
	if status != "active" {
		return WorkflowResult{}, false, ErrDisabled
	}
	var existingHash string
	var existing json.RawMessage
	err = tx.QueryRow(ctx, "SELECT request_hash,result FROM creative_requests WHERE user_id=$1 AND request_key=$2", user, key).Scan(&existingHash, &existing)
	if err == nil {
		if existingHash != hash {
			return WorkflowResult{}, false, ErrConflict
		}
		var result WorkflowResult
		if json.Unmarshal(existing, &result) != nil {
			return result, false, ErrConflict
		}
		return result, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return WorkflowResult{}, false, err
	}
	result, err := fn(tx)
	if err != nil {
		return WorkflowResult{}, false, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return WorkflowResult{}, false, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO creative_requests(user_id,request_key,request_hash,result) VALUES($1,$2,$3,$4)", user, key, hash, data); err != nil {
		return WorkflowResult{}, false, err
	}
	return result, true, tx.Commit(ctx)
}

func (s *Store) LinkProjectAssetsTx(ctx context.Context, tx pgx.Tx, user, project string, ids []string, kind string) error {
	if len(ids) > 10 {
		return ErrConflict
	}
	if _, err := s.ProjectTx(ctx, tx, user, project); err != nil {
		return err
	}
	for i, id := range ids {
		var owns bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND image_type<>'worker-candidate')", id, user).Scan(&owns); err != nil {
			return err
		}
		if !owns {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `INSERT INTO project_asset_refs(project_id,asset_id,position,image_type) VALUES($1,$2,$3,$4) ON CONFLICT(project_id,asset_id) DO UPDATE SET position=EXCLUDED.position,image_type=EXCLUDED.image_type`, project, id, i, kind); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ProjectAssets(ctx context.Context, user, project string) ([]Asset, error) {
	if _, err := s.Project(ctx, user, project); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.id,a.user_id,COALESCE(a.project_id,''),COALESCE(a.task_id,''),COALESCE(r.image_type,a.image_type),a.storage_key,a.original_filename,a.content_type,a.file_size,a.width,a.height,a.sha256,a.metadata,a.created_at
        FROM assets a LEFT JOIN project_asset_refs r ON r.asset_id=a.id AND r.project_id=$2
        WHERE a.user_id=$1 AND a.deleted_at IS NULL AND a.image_type<>'worker-candidate' AND ((a.project_id=$2 AND a.task_id IS NULL) OR r.asset_id IS NOT NULL)
        ORDER BY COALESCE(r.position,2147483647),a.created_at,a.id`, user, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ProjectTasks(ctx context.Context, user, project string) ([]Task, error) {
	if _, err := s.Project(ctx, user, project); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND project_id=$2 AND deleted_at IS NULL ORDER BY created_at,id", user, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ProjectTasksTx(ctx context.Context, tx pgx.Tx, user, project string) ([]Task, error) {
	rows, err := tx.Query(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND project_id=$2 AND deleted_at IS NULL ORDER BY created_at,id", user, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetProjectFacts(ctx context.Context, user, project string, facts json.RawMessage) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE projects SET confirmed_facts=$3,status=CASE WHEN status IN ('draft','copy_generated','facts_confirmed') THEN 'facts_confirmed' ELSE status END,updated_at=now() WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, user, project, facts)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

// ApplyProjectCopy uses a task pointer compare-and-swap so an older completion
// cannot overwrite a newer request, facts confirmation or the A+ selection.
func (s *Store) ApplyProjectCopy(ctx context.Context, user, project, task string, copy json.RawMessage, valid bool, code string) error {
	status := "copy_failed"
	if valid {
		status = "copy_generated"
	}
	_, err := s.Pool.Exec(ctx, `UPDATE projects SET generated_copy=CASE WHEN $6 THEN $4 ELSE generated_copy END,status=$5,metadata=metadata || jsonb_build_object('copy_applied_task_id',$3::text,'copy_error',CASE WHEN $6 THEN '' ELSE $7::text END),updated_at=now()
		WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL AND metadata->>'copy_task_id'=$3 AND COALESCE(metadata->>'copy_applied_task_id','')<>$3`, user, project, task, copy, status, valid, code)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, user, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var owner string
	if err = tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", user).Scan(&owner); err != nil {
		return normalizeError(err)
	}
	p, err := s.ProjectTx(ctx, tx, user, id)
	if err != nil {
		return err
	}
	ids := []string{id}
	var metadata map[string]any
	_ = json.Unmarshal(p.Metadata, &metadata)
	chat, _ := metadata["source_chat_id"].(string)
	if chat != "" {
		if err = tx.QueryRow(ctx, "SELECT user_id FROM creative_chats WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE", chat, user).Scan(&owner); err != nil {
			return normalizeError(err)
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM creative_turns WHERE chat_id=$1 AND status='pending')", chat).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrConflict
		}
		rows, err := tx.Query(ctx, "SELECT DISTINCT project_id FROM creative_versions WHERE chat_id=$1", chat)
		if err != nil {
			return err
		}
		for rows.Next() {
			var related string
			if err = rows.Scan(&related); err != nil {
				rows.Close()
				return err
			}
			if related != id {
				ids = append(ids, related)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE user_id=$1 AND project_id=ANY($2) AND deleted_at IS NULL AND status NOT IN ('succeeded','failed','cancelled'))`, user, ids).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, "UPDATE tasks SET deleted_at=now() WHERE user_id=$1 AND (project_id=ANY($2) OR id IN (SELECT task_id FROM creative_turns WHERE chat_id=$3))", user, ids, chat); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_messages SET deleted_at=now() WHERE chat_id=$2 OR version_id IN (SELECT id FROM creative_versions WHERE project_id=ANY($1))", ids, chat); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_versions SET deleted_at=now() WHERE project_id=ANY($1)", ids); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE projects SET deleted_at=now(),updated_at=now() WHERE id=ANY($1) AND user_id=$2", ids, user); err != nil {
		return err
	}
	if chat != "" {
		if _, err = tx.Exec(ctx, "UPDATE creative_chats SET deleted_at=now(),updated_at=now() WHERE id=$1 AND user_id=$2", chat, user); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteProjectFailedTask(ctx context.Context, user, project, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = s.ProjectTx(ctx, tx, user, project); err != nil {
		return err
	}
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND project_id=$2 AND id=$3 AND deleted_at IS NULL FOR UPDATE", user, project, id))
	if err != nil {
		return err
	}
	if t.Status != "failed" || t.Capability != "image" {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, "UPDATE tasks SET deleted_at=now() WHERE id=$1", id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_messages SET deleted_at=now() WHERE version_id IN (SELECT id FROM creative_versions WHERE task_id=$1)", id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE creative_versions SET deleted_at=now(),updated_at=now() WHERE task_id=$1", id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE projects SET deleted_at=CASE WHEN source_type='creative' THEN now() ELSE deleted_at END,status=CASE WHEN source_type='listing' THEN 'copy_generated' ELSE status END,updated_at=now()
        WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM tasks WHERE project_id=$1 AND capability='image' AND deleted_at IS NULL)`, project); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) PendingProjectCopies(ctx context.Context) ([]Project, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+projectColumns+` FROM projects WHERE deleted_at IS NULL AND COALESCE(metadata->>'copy_task_id','')<>COALESCE(metadata->>'copy_applied_task_id','') AND metadata->>'copy_task_id' IN (SELECT id FROM tasks WHERE status IN ('succeeded','failed','cancelled') AND deleted_at IS NULL) ORDER BY updated_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
