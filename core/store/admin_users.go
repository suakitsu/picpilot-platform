package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const adminUserSelection = `jsonb_build_object('id',u.id,'email_display',u.email,'email_masked',` + adminMaskedEmail + `,
 'nickname',u.nickname,'role',u.role,'status',u.status,'credits',u.credits,'created_at',u.created_at,'updated_at',u.updated_at,
 'last_request_at',(SELECT last_request_at FROM admin_user_activity WHERE user_id=u.id),
 'project_count',(SELECT count(*) FROM projects p WHERE p.user_id=u.id AND p.deleted_at IS NULL),
 'recharge_count',(SELECT count(*) FROM recharge_orders o WHERE o.user_id=u.id AND o.status='paid'),
 'risk_count',(SELECT count(*) FROM admin_risk_events e WHERE e.user_id=u.id AND e.cleared_at IS NULL))`

func (s *Store) AdminUsers(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.Keyword)
	q.exact("u.id", f.UserID)
	q.exact("u.status", f.Status)
	q.exact("u.role", f.Role)
	q.dates("u.created_at", f)
	if f.MinCredits != nil {
		q.add("u.credits>=?", *f.MinCredits)
	}
	if f.MaxCredits != nil {
		q.add("u.credits<=?", *f.MaxCredits)
	}
	order := map[string]string{"": "u.created_at DESC,u.id DESC", "created_desc": "u.created_at DESC,u.id DESC", "created_asc": "u.created_at,u.id", "credits_desc": "u.credits DESC,u.created_at DESC,u.id DESC", "credits_asc": "u.credits,u.created_at DESC,u.id DESC", "last_active_desc": "(SELECT last_request_at FROM admin_user_activity WHERE user_id=u.id) DESC NULLS LAST,u.created_at DESC,u.id DESC"}[f.Sort]
	if order == "" {
		return AdminPage{}, ErrConflict
	}
	return adminPaged(ctx, tx, f, "users u", adminUserSelection, order, q)
}

func (s *Store) AdminUser(ctx context.Context, actor, id string) (map[string]any, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	return adminJSON(tx.QueryRow(ctx, "SELECT "+adminUserSelection+" FROM users u WHERE id=$1", id))
}

func adminTarget(ctx context.Context, tx pgx.Tx, actor, target string) (User, error) {
	var role string
	if err := tx.QueryRow(ctx, "SELECT role FROM users WHERE id=$1", actor).Scan(&role); err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id=$1 FOR UPDATE", target))
	if err != nil {
		return u, err
	}
	if u.Role == "owner" || (role != "super_admin" && u.Role != "user") {
		return User{}, ErrAdminForbidden
	}
	return u, nil
}

func (s *Store) AdminAdjustCredits(ctx context.Context, actor, user string, amount int64, key, remark, requestID string) (LedgerEntry, bool, error) {
	key = strings.TrimSpace(key)
	remark = strings.TrimSpace(remark)
	if len(key) < 8 || len(key) > 128 || amount == 0 || len(remark) > 1000 || strings.ContainsRune(key+remark, 0) {
		return LedgerEntry{}, false, ErrConflict
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return LedgerEntry{}, false, err
	}
	defer tx.Rollback(context.Background())
	// Credit adjustments do not change roles; ordinary admins may adjust any account.
	var before int64
	if err = tx.QueryRow(ctx, "SELECT credits FROM users WHERE id=$1 FOR UPDATE", user).Scan(&before); err != nil {
		return LedgerEntry{}, false, normalizeError(err)
	}
	var existingAmount int64
	var existingRemark, existingActor string
	err = tx.QueryRow(ctx, `SELECT change_amount,remark,COALESCE(actor_id,'') FROM credit_ledger WHERE user_id=$1 AND biz_type='manual_adjust' AND biz_id=$2`, user, key).Scan(&existingAmount, &existingRemark, &existingActor)
	replay := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LedgerEntry{}, false, err
	}
	if replay && (existingAmount != amount || existingRemark != remark || existingActor != actor) {
		return LedgerEntry{}, false, ErrConflict
	}
	entry, err := adjustCredits(ctx, tx, user, amount, "manual_adjust", key, remark, actor)
	if err != nil {
		return LedgerEntry{}, false, err
	}
	if !replay {
		err = adminAudit(ctx, tx, actor, "manual_credit_adjust", "user", user, requestID, map[string]any{"change_amount": amount, "balance_before": before, "balance_after": entry.Balance, "ledger_id": entry.ID})
	}
	if err != nil {
		return LedgerEntry{}, false, err
	}
	return entry, replay, tx.Commit(ctx)
}

func (s *Store) AdminSetUserStatus(ctx context.Context, actor, id, status, requestID string) error {
	if status != "active" && status != "disabled" {
		return ErrConflict
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	u, err := adminTarget(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if status == "disabled" && u.Role == "super_admin" && u.Status == "active" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='super_admin' AND status='active'`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET status=$2,auth_version=auth_version+1,metadata=metadata-'risk_restricted',updated_at=now() WHERE id=$1", id, status); err != nil {
		return err
	}
	if err = adminAudit(ctx, tx, actor, "user_status_update", "user", id, requestID, map[string]any{"before_status": u.Status, "status": status}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AdminResetPassword(ctx context.Context, actor, id, hash, requestID string) error {
	if _, err := bcrypt.Cost([]byte(hash)); err != nil {
		return ErrConflict
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = adminTarget(ctx, tx, actor, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET password_hash=$2,auth_version=auth_version+1,updated_at=now() WHERE id=$1", id, hash); err != nil {
		return err
	}
	if err = adminAudit(ctx, tx, actor, "user_password_reset", "user", id, requestID, map[string]any{"password_reset": true}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AdminLedger(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"l.id", "l.biz_type", "l.biz_id", "l.remark"}, f.Keyword)
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	q.exact("l.user_id", f.UserID)
	q.exact("l.biz_type", f.BizType)
	q.dates("l.created_at", f)
	return adminPaged(ctx, tx, f, "credit_ledger l JOIN users u ON u.id=l.user_id", `jsonb_build_object('id',l.id,'user_id',l.user_id,'user_email',u.email,'user_nickname',u.nickname,'change_amount',l.change_amount,'balance_after',l.balance_after,'biz_type',l.biz_type,'biz_id',l.biz_id,'remark',l.remark,'actor_id',l.actor_id,'created_at',l.created_at)`, "l.created_at DESC,l.id DESC", q)
}

func (s *Store) AdminAuditLogs(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"a.id", "a.actor_id", "a.action", "a.target_type", "a.target_id"}, f.Keyword)
	q.exact("a.action", f.Action)
	q.exact("a.target_type", f.TargetType)
	q.exact("a.actor_id", f.UserID)
	q.dates("a.created_at", f)
	// Historical metadata is untrusted and may have been written by other modules.
	return adminPaged(ctx, tx, f, "audit_events a LEFT JOIN users u ON u.id=a.actor_id", `jsonb_build_object('id',a.id,'admin_id',a.actor_id,'admin_email_masked',`+adminMaskedEmail+`,'action',a.action,'target_type',a.target_type,'target_id',a.target_id,'summary',a.action,'request_id',a.request_id,'before_json',NULL,'after_json',NULL,'metadata_redacted',true,'created_at',a.created_at)`, "a.created_at DESC,a.id DESC", q)
}
