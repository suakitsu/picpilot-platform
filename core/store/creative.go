package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

type CreativeChat struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Title     string    `json:"title"`
	Style     string    `json:"style"`
	Size      string    `json:"size"`
	Language  string    `json:"language"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const chatColumns = `id,user_id,title,style,size,language,created_at,updated_at`

func scanChat(row pgx.Row) (CreativeChat, error) {
	var c CreativeChat
	err := row.Scan(&c.ID, &c.UserID, &c.Title, &c.Style, &c.Size, &c.Language, &c.CreatedAt, &c.UpdatedAt)
	return c, normalizeError(err)
}
func (s *Store) CreativeChat(ctx context.Context, user, id string) (CreativeChat, error) {
	return scanChat(s.Pool.QueryRow(ctx, "SELECT "+chatColumns+" FROM creative_chats WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL", id, user))
}
func (s *Store) CreativeChats(ctx context.Context, user string) ([]CreativeChat, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+chatColumns+" FROM creative_chats WHERE user_id=$1 AND deleted_at IS NULL ORDER BY updated_at DESC,id LIMIT 500", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeChat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) SaveCreativeChatTx(ctx context.Context, tx pgx.Tx, c CreativeChat) (CreativeChat, error) {
	if c.ID == "" {
		c.ID = security.NewID("cchat")
		return scanChat(tx.QueryRow(ctx, "INSERT INTO creative_chats(id,user_id,title,style,size,language) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+chatColumns, c.ID, c.UserID, c.Title, c.Style, c.Size, c.Language))
	}
	return scanChat(tx.QueryRow(ctx, "UPDATE creative_chats SET style=$3,size=$4,language=$5,updated_at=now() WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL RETURNING "+chatColumns, c.ID, c.UserID, c.Style, c.Size, c.Language))
}

type CreativeVersion struct {
	ID                string    `json:"id"`
	ChatID            string    `json:"chat_id"`
	VersionNo         int       `json:"version_no"`
	ProjectID         string    `json:"project_id"`
	TaskID            string    `json:"task_id"`
	UserRequest       string    `json:"user_request"`
	FinalPrompt       string    `json:"final_prompt"`
	Style             string    `json:"style"`
	Size              string    `json:"size"`
	ReferenceAssetIDs []string  `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type CreativeMessage struct {
	ID        string    `json:"id"`
	ChatID    string    `json:"chat_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	VersionID string    `json:"-"`
	TaskID    string    `json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreativeVersions(ctx context.Context, user, chat string) ([]CreativeVersion, error) {
	if _, err := s.CreativeChat(ctx, user, chat); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT id,chat_id,version_no,project_id,task_id,user_request,final_prompt,style,size,reference_asset_ids,created_at,updated_at FROM creative_versions WHERE chat_id=$1 AND deleted_at IS NULL ORDER BY version_no", chat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeVersion{}
	for rows.Next() {
		var v CreativeVersion
		var refs json.RawMessage
		if err = rows.Scan(&v.ID, &v.ChatID, &v.VersionNo, &v.ProjectID, &v.TaskID, &v.UserRequest, &v.FinalPrompt, &v.Style, &v.Size, &refs, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(refs, &v.ReferenceAssetIDs); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) CreativeMessages(ctx context.Context, user, chat string) ([]CreativeMessage, error) {
	if _, err := s.CreativeChat(ctx, user, chat); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT id,chat_id,role,content,COALESCE(version_id,''),COALESCE(task_id,''),created_at FROM creative_messages WHERE chat_id=$1 AND deleted_at IS NULL ORDER BY created_at,id", chat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeMessage{}
	for rows.Next() {
		var m CreativeMessage
		if err = rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.VersionID, &m.TaskID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) AddCreativeVersionTx(ctx context.Context, tx pgx.Tx, user string, v CreativeVersion) (CreativeVersion, error) {
	var owner string
	if err := tx.QueryRow(ctx, "SELECT user_id FROM creative_chats WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", v.ChatID).Scan(&owner); err != nil {
		return v, normalizeError(err)
	}
	if owner != user {
		return v, ErrNotFound
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM creative_versions WHERE chat_id=$1 AND deleted_at IS NULL", v.ChatID).Scan(&count); err != nil {
		return v, err
	}
	if count >= 10 {
		return v, ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT COALESCE(max(version_no),0)+1 FROM creative_versions WHERE chat_id=$1", v.ChatID).Scan(&v.VersionNo); err != nil {
		return v, err
	}
	v.ID = security.NewID("cver")
	if v.ReferenceAssetIDs == nil {
		v.ReferenceAssetIDs = []string{}
	}
	refs, _ := json.Marshal(v.ReferenceAssetIDs)
	if _, err := tx.Exec(ctx, `INSERT INTO creative_versions(id,chat_id,version_no,project_id,task_id,user_request,final_prompt,style,size,reference_asset_ids) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.ID, v.ChatID, v.VersionNo, v.ProjectID, v.TaskID, v.UserRequest, v.FinalPrompt, v.Style, v.Size, refs); err != nil {
		return v, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO creative_messages(id,chat_id,role,content,version_id,task_id,created_at) VALUES($1,$2,'user',$3,$4,$5,now()),($6,$2,'assistant',$7,$4,$5,now()+interval '1 microsecond')`, security.NewID("cmsg"), v.ChatID, v.UserRequest, v.ID, v.TaskID, security.NewID("cmsg"), v.FinalPrompt)
	return v, err
}

func (s *Store) IsCreativeChatAsset(ctx context.Context, user, chat, asset string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM creative_chats c JOIN creative_versions v ON v.chat_id=c.id JOIN assets a ON a.user_id=c.user_id
        WHERE c.id=$1 AND c.user_id=$2 AND c.deleted_at IS NULL AND v.deleted_at IS NULL AND a.id=$3 AND a.deleted_at IS NULL AND a.image_type<>'worker-candidate'
        AND (a.project_id=v.project_id OR EXISTS(SELECT 1 FROM project_asset_refs r WHERE r.project_id=v.project_id AND r.asset_id=a.id)))`, chat, user, asset).Scan(&ok)
	return ok, err
}

func (s *Store) ReplaceCreativeTaskTx(ctx context.Context, tx pgx.Tx, user, project, oldTask, newTask string, refs []string, prompt string) error {
	if _, err := s.ProjectTx(ctx, tx, user, project); err != nil {
		return err
	}
	if err := linkCommerceRetryTx(ctx, tx, user, oldTask, newTask); err != nil {
		return err
	}
	if refs == nil {
		refs = []string{}
	}
	raw, _ := json.Marshal(refs)
	if _, err := tx.Exec(ctx, "UPDATE creative_versions SET task_id=$2,reference_asset_ids=$4,final_prompt=$5,updated_at=now() WHERE task_id=$1 AND project_id=$3 AND deleted_at IS NULL", oldTask, newTask, project, raw, prompt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE creative_messages SET task_id=$2 WHERE task_id=$1 AND deleted_at IS NULL", oldTask, newTask); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE tasks SET deleted_at=now() WHERE id=$1 AND user_id=$2 AND project_id=$3", oldTask, user, project)
	return err
}

type CreativeTurn struct {
	ID, ChatID, TaskID, Status, ErrorCode string
	Request                               json.RawMessage
	ReferenceAssetIDs                     []string
	Result                                WorkflowResult
}

func (s *Store) AddCreativeTurnTx(ctx context.Context, tx pgx.Tx, chat, task string, request json.RawMessage, refs []string, message string) error {
	var pending bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM creative_turns WHERE chat_id=$1 AND status='pending')", chat).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrConflict
	}
	if refs == nil {
		refs = []string{}
	}
	raw, _ := json.Marshal(refs)
	if _, err := tx.Exec(ctx, "INSERT INTO creative_turns(id,chat_id,task_id,request,reference_asset_ids) VALUES($1,$2,$3,$4,$5)", security.NewID("cturn"), chat, task, request, raw); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO creative_messages(id,chat_id,role,content,task_id,created_at) VALUES($1,$2,'user',$3,$4,now()),($5,$2,'assistant','',$4,now()+interval '1 microsecond')`, security.NewID("cmsg"), chat, message, task, security.NewID("cmsg"))
	return err
}
func (s *Store) CreativeTurns(ctx context.Context, user, chat string) ([]CreativeTurn, error) {
	if _, err := s.CreativeChat(ctx, user, chat); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT id,chat_id,task_id,request,reference_asset_ids,status,error_code,result FROM creative_turns WHERE chat_id=$1 ORDER BY created_at,id", chat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeTurn{}
	for rows.Next() {
		var v CreativeTurn
		var refs, result json.RawMessage
		if err = rows.Scan(&v.ID, &v.ChatID, &v.TaskID, &v.Request, &refs, &v.Status, &v.ErrorCode, &result); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(refs, &v.ReferenceAssetIDs); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(result, &v.Result); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) FinishCreativeTurnTx(ctx context.Context, tx pgx.Tx, turn CreativeTurn, result WorkflowResult, reply, code string) error {
	status := "applied"
	if code != "" {
		status = "failed"
	}
	raw, _ := json.Marshal(result)
	if _, err := tx.Exec(ctx, "UPDATE creative_turns SET status=$2,result=$3,error_code=$4 WHERE id=$1 AND status='pending'", turn.ID, status, raw, code); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE creative_chats SET updated_at=now() WHERE id=$1", turn.ChatID); err != nil {
		return err
	}
	if result.ProjectID != "" {
		_, err := tx.Exec(ctx, "UPDATE creative_messages SET deleted_at=now() WHERE task_id=$1 AND version_id IS NULL", turn.TaskID)
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE creative_messages SET content=$2 WHERE task_id=$1 AND role='assistant' AND version_id IS NULL", turn.TaskID, reply)
	return err
}

// PendingCreativeChats is used by the optional workflow reconciler. Reads also
// reconcile their own chat, so polling remains sufficient without a second loop.
func (s *Store) PendingCreativeChats(ctx context.Context) ([]CreativeChat, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+chatColumns+" FROM creative_chats WHERE deleted_at IS NULL AND id IN (SELECT chat_id FROM creative_turns WHERE status='pending') ORDER BY updated_at LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeChat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
