package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

var (
	ErrAdminForbidden = errors.New("administrator access required")
	ErrLastAdmin      = errors.New("the last active super administrator cannot be disabled")
	ErrUnsafeRetry    = errors.New("task cannot safely be retried or deleted")
)

type AdminFilter struct {
	Page, PageSize                                   int
	Keyword, UserKeyword, UserID, Status, Role, Sort string
	Provider, Model, Feature, MediaType, ImageType   string
	RiskLevel, Source, Action, TargetType, BizType   string
	MinCredits, MaxCredits                           *int64
	From, Until                                      *time.Time // Until is exclusive; business dates use Asia/Shanghai.
	Cleared                                          *bool
}

func (f AdminFilter) Validate() error {
	if f.Page < 1 || f.Page > 1000000 || f.PageSize < 1 || f.PageSize > 100 {
		return ErrConflict
	}
	for _, v := range []string{f.Keyword, f.UserKeyword, f.UserID, f.Status, f.Role, f.Sort, f.Provider, f.Model, f.Feature, f.MediaType, f.ImageType, f.RiskLevel, f.Source, f.Action, f.TargetType, f.BizType} {
		if len(v) > 200 || strings.ContainsRune(v, 0) {
			return ErrConflict
		}
	}
	if f.From != nil && f.Until != nil && !f.From.Before(*f.Until) {
		return ErrConflict
	}
	if f.MinCredits != nil && f.MaxCredits != nil && *f.MinCredits > *f.MaxCredits {
		return ErrConflict
	}
	return nil
}

type AdminPage struct {
	Rows       []map[string]any `json:"rows"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int64            `json:"total_pages"`
	Summary    map[string]any   `json:"summary,omitempty"`
}

// All privileged writes share this lock, including the last-super-admin check.
// Never lock the actor row before a task: task completion locks task then user.
func (s *Store) adminBegin(ctx context.Context, actor string) (pgx.Tx, error) {
	if s.Edition != config.Platform {
		return nil, ErrAdminForbidden
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(712496152247)"); err == nil {
		err = adminAuthorize(ctx, tx, actor)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, err
	}
	return tx, nil
}

func adminAuthorize(ctx context.Context, tx pgx.Tx, actor string) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active' AND role IN ('admin','super_admin'))`, actor).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAdminForbidden
	}
	return nil
}

func (s *Store) adminRead(ctx context.Context, actor string) (pgx.Tx, error) {
	if s.Edition != config.Platform {
		return nil, ErrAdminForbidden
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	if err = adminAuthorize(ctx, tx, actor); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, err
	}
	return tx, nil
}

func adminAudit(ctx context.Context, tx pgx.Tx, actor, action, targetType, targetID, requestID string, metadata map[string]any) error {
	// Callers pass only explicit operational metadata, never request bodies.
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,request_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7)`, security.NewID("aud"), actor, action, targetType, targetID, requestID, raw)
	return err
}

type adminWhere struct {
	parts []string
	args  []any
}

func (q *adminWhere) add(expression string, value any) {
	q.args = append(q.args, value)
	q.parts = append(q.parts, strings.ReplaceAll(expression, "?", fmt.Sprintf("$%d", len(q.args))))
}
func (q *adminWhere) exact(column, value string) {
	if value != "" {
		q.add(column+" = ?", value)
	}
}
func (q *adminWhere) search(columns []string, value string) {
	if value == "" {
		return
	}
	parts := make([]string, 0, len(columns))
	for _, c := range columns {
		parts = append(parts, "strpos(lower(COALESCE("+c+",'')),lower(?))>0")
	}
	q.add("("+strings.Join(parts, " OR ")+")", value)
}
func (q *adminWhere) dates(column string, f AdminFilter) {
	if f.From != nil {
		q.add(column+">=?", *f.From)
	}
	if f.Until != nil {
		q.add(column+"<?", *f.Until)
	}
}
func (q adminWhere) sql() string {
	if len(q.parts) == 0 {
		return "TRUE"
	}
	return strings.Join(q.parts, " AND ")
}

func adminJSON(row pgx.Row) (map[string]any, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, normalizeError(err)
	}
	var m map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&m)
	return m, err
}

func adminJSONRows(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		m, err := adminJSON(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func adminPaged(ctx context.Context, tx pgx.Tx, f AdminFilter, from, selection, order string, q adminWhere) (AdminPage, error) {
	p := AdminPage{Rows: []map[string]any{}, Page: f.Page, PageSize: f.PageSize}
	if err := f.Validate(); err != nil {
		return p, err
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+from+" WHERE "+q.sql(), q.args...).Scan(&p.Total); err != nil {
		return p, err
	}
	p.TotalPages = (p.Total + int64(f.PageSize) - 1) / int64(f.PageSize)
	args := append(append([]any{}, q.args...), f.PageSize, (f.Page-1)*f.PageSize)
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d", selection, from, q.sql(), order, len(args)-1, len(args))
	var err error
	p.Rows, err = adminJSONRows(ctx, tx, query, args...)
	return p, err
}

const adminMaskedEmail = `left(u.email,1)||'***@'||split_part(u.email,'@',2)`
