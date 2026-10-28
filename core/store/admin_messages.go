package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type AdminMessageInput struct {
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Audience    string     `json:"audience"`
	RecipientID string     `json:"recipient_id"`
	Enabled     *bool      `json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

func (s *Store) AdminPublishMessage(ctx context.Context, actor, requestID string, in AdminMessageInput) (map[string]any, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if in.Audience == "" {
		in.Audience = "all"
	}
	if in.Title == "" || len([]rune(in.Title)) > 200 || in.Content == "" || len([]rune(in.Content)) > 20000 || strings.ContainsRune(in.Title+in.Content, 0) {
		return nil, ErrConflict
	}
	if in.Audience != "all" && in.Audience != "user" && in.Audience != "admin" && in.Audience != "super_admin" && in.Audience != "targeted" {
		return nil, ErrConflict
	}
	if (in.Audience == "targeted") != (in.RecipientID != "") {
		return nil, ErrConflict
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
		return nil, ErrConflict
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if in.RecipientID != "" {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)", in.RecipientID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	id := security.NewID("msg")
	if _, err = tx.Exec(ctx, `INSERT INTO admin_messages(id,title,content,audience,recipient_id,enabled,published_by,expires_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)`, id, in.Title, in.Content, in.Audience, in.RecipientID, enabled, actor, in.ExpiresAt); err != nil {
		return nil, err
	}
	if err = adminAudit(ctx, tx, actor, "station_message_publish", "station_message", id, requestID, map[string]any{"audience": in.Audience, "enabled": enabled}); err != nil {
		return nil, err
	}
	m, err := adminJSON(tx.QueryRow(ctx, `SELECT to_jsonb(m) FROM admin_messages m WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	return m, tx.Commit(ctx)
}

func (s *Store) AdminMessages(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"m.id", "m.title", "m.content", "m.audience"}, f.Keyword)
	q.dates("m.published_at", f)
	if f.Status != "" {
		switch f.Status {
		case "enabled":
			q.parts = append(q.parts, "m.enabled AND (m.expires_at IS NULL OR m.expires_at>now())")
		case "disabled":
			q.parts = append(q.parts, "NOT m.enabled")
		case "expired":
			q.parts = append(q.parts, "m.expires_at<=now()")
		default:
			return AdminPage{}, ErrConflict
		}
	}
	return adminPaged(ctx, tx, f, "admin_messages m", `to_jsonb(m)||jsonb_build_object('read_count',(SELECT count(*) FROM admin_message_reads r WHERE r.message_id=m.id))`, "m.published_at DESC,m.id DESC", q)
}

const visibleMessage = `m.enabled AND m.published_at<=now() AND (m.expires_at IS NULL OR m.expires_at>now()) AND (m.audience='all' OR m.audience=u.role OR (m.audience='admin' AND u.role='super_admin') OR (m.audience='targeted' AND m.recipient_id=u.id))`

func (s *Store) ListMessages(ctx context.Context, user string, f AdminFilter) (AdminPage, error) {
	if err := f.Validate(); err != nil {
		return AdminPage{}, err
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')", user).Scan(&active); err != nil {
		return AdminPage{}, err
	}
	if !active {
		return AdminPage{}, ErrDisabled
	}
	if s.Edition == config.Personal {
		return AdminPage{Rows: []map[string]any{}, Page: f.Page, PageSize: f.PageSize}, nil
	}
	q := adminWhere{parts: []string{visibleMessage}}
	q.exact("u.id", user)
	q.search([]string{"m.title", "m.content"}, f.Keyword)
	if f.Status == "read" {
		q.parts = append(q.parts, "r.message_id IS NOT NULL")
	} else if f.Status == "unread" {
		q.parts = append(q.parts, "r.message_id IS NULL")
	} else if f.Status != "" {
		return AdminPage{}, ErrConflict
	}
	return adminPaged(ctx, tx, f, "admin_messages m CROSS JOIN users u LEFT JOIN admin_message_reads r ON r.user_id=u.id AND r.message_id=m.id", `jsonb_build_object('id',m.id,'title',m.title,'content',m.content,'published_at',m.published_at,'expires_at',m.expires_at,'read',r.message_id IS NOT NULL)`, "m.published_at DESC,m.id DESC", q)
}

func (s *Store) UnreadMessageCount(ctx context.Context, user string) (int64, error) {
	if s.Edition == config.Personal {
		return 0, s.RiskUserAllowed(ctx, user)
	}
	var count int64
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_messages m CROSS JOIN users u WHERE u.id=$1 AND u.status='active' AND `+visibleMessage+` AND NOT EXISTS(SELECT 1 FROM admin_message_reads r WHERE r.user_id=u.id AND r.message_id=m.id)`, user).Scan(&count)
	return count, err
}

func (s *Store) SetMessageRead(ctx context.Context, user, id string, read bool) error {
	if s.Edition == config.Personal {
		return ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var visible bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_messages m CROSS JOIN users u WHERE m.id=$1 AND u.id=$2 AND u.status='active' AND `+visibleMessage+`)`, id, user).Scan(&visible); err != nil {
		return err
	}
	if !visible {
		return ErrNotFound
	}
	if read {
		_, err = tx.Exec(ctx, `INSERT INTO admin_message_reads(message_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, user)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM admin_message_reads WHERE message_id=$1 AND user_id=$2`, id, user)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
