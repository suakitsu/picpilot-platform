package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

var billingID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var billingCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Store) billingPlatform(ctx context.Context) error {
	if s.Edition != config.Platform {
		return ErrNotFound
	}
	var edition string
	if err := s.Pool.QueryRow(ctx, "SELECT edition FROM instance_state WHERE singleton").Scan(&edition); err != nil {
		return normalizeError(err)
	}
	if edition != string(config.Platform) {
		return ErrNotFound
	}
	return nil
}

func billingAudit(ctx context.Context, tx pgx.Tx, actor, action, id string, metadata map[string]any) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,metadata) VALUES($1,NULLIF($2,''),$3,'billing',$4,$5)`, security.NewID("aud"), actor, action, id, data)
	return err
}

func scanBillingPlan(row pgx.Row) (billing.Plan, error) {
	var p billing.Plan
	err := row.Scan(&p.ID, &p.Name, &p.Credits, &p.PriceCents, &p.Currency, &p.Enabled, &p.SortOrder)
	return p, normalizeError(err)
}

const billingPlanColumns = `id,name,credits,price_cents,currency,enabled,sort_order`

func (s *Store) BillingPlans(ctx context.Context, admin bool) ([]billing.Plan, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+billingPlanColumns+` FROM recharge_plans WHERE ($1 OR enabled) ORDER BY sort_order,id`, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []billing.Plan{}
	for rows.Next() {
		p, err := scanBillingPlan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) BillingPlan(ctx context.Context, id string) (billing.Plan, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return billing.Plan{}, err
	}
	return scanBillingPlan(s.Pool.QueryRow(ctx, `SELECT `+billingPlanColumns+` FROM recharge_plans WHERE id=$1`, id))
}

func (s *Store) SaveBillingPlan(ctx context.Context, p billing.Plan, actor string) (billing.Plan, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return p, err
	}
	if !billingID.MatchString(p.ID) || len(strings.TrimSpace(p.Name)) == 0 || len(p.Name) > 120 || p.Credits <= 0 || p.PriceCents < 0 || !billingCurrency.MatchString(p.Currency) || p.SortOrder < -1000000 || p.SortOrder > 1000000 {
		return p, billing.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(context.Background())
	p, err = scanBillingPlan(tx.QueryRow(ctx, `INSERT INTO recharge_plans(id,name,credits,price_cents,currency,enabled,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,credits=EXCLUDED.credits,price_cents=EXCLUDED.price_cents,currency=EXCLUDED.currency,enabled=EXCLUDED.enabled,sort_order=EXCLUDED.sort_order,updated_at=now() RETURNING `+billingPlanColumns, p.ID, p.Name, p.Credits, p.PriceCents, p.Currency, p.Enabled, p.SortOrder))
	if err != nil {
		return p, err
	}
	if err = billingAudit(ctx, tx, actor, "recharge_plan_update", p.ID, map[string]any{"credits": p.Credits, "price_cents": p.PriceCents, "enabled": p.Enabled}); err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

const billingChannelColumns = `id,code,name,visible,enabled,sort_order,config_ciphertext,version`

func scanBillingChannel(row pgx.Row) (billing.Channel, error) {
	var c billing.Channel
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Visible, &c.Enabled, &c.SortOrder, &c.Ciphertext, &c.Version)
	c.Config = "{}"
	return c, normalizeError(err)
}
func (s *Store) BillingChannels(ctx context.Context, admin bool) ([]billing.Channel, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+billingChannelColumns+` FROM payment_channels WHERE ($1 OR visible) ORDER BY sort_order,id`, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []billing.Channel{}
	for rows.Next() {
		c, err := scanBillingChannel(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (s *Store) BillingChannel(ctx context.Context, id string, byCode bool) (billing.Channel, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return billing.Channel{}, err
	}
	column := "id"
	if byCode {
		column = "code"
	}
	return scanBillingChannel(s.Pool.QueryRow(ctx, `SELECT `+billingChannelColumns+` FROM payment_channels WHERE `+column+`=$1`, id))
}
func (s *Store) SaveBillingChannel(ctx context.Context, c billing.Channel, actor string) (billing.Channel, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return c, err
	}
	if !billingID.MatchString(c.ID) || !billingID.MatchString(c.Code) || strings.TrimSpace(c.Name) == "" || len(c.Name) > 120 || len(c.Ciphertext) > 65536 || (c.Ciphertext != "" && !strings.HasPrefix(c.Ciphertext, "v1:")) || c.SortOrder < -1000000 || c.SortOrder > 1000000 {
		return c, billing.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(context.Background())
	c, err = scanBillingChannel(tx.QueryRow(ctx, `INSERT INTO payment_channels(id,code,name,visible,enabled,sort_order,config_ciphertext) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,visible=EXCLUDED.visible,enabled=EXCLUDED.enabled,sort_order=EXCLUDED.sort_order,config_ciphertext=EXCLUDED.config_ciphertext,version=payment_channels.version+1,updated_at=now() WHERE payment_channels.version=$8 AND payment_channels.code=EXCLUDED.code RETURNING `+billingChannelColumns, c.ID, c.Code, c.Name, c.Visible, c.Enabled, c.SortOrder, c.Ciphertext, c.Version))
	if errors.Is(err, ErrNotFound) {
		err = ErrConflict
	}
	if err != nil {
		return c, err
	}
	if err = billingAudit(ctx, tx, actor, "payment_channel_update", c.ID, map[string]any{"version": c.Version, "visible": c.Visible, "enabled": c.Enabled}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

// Repeatable-read keeps total and rows on the same pagination snapshot.
func billingPage[T any](ctx context.Context, s *Store, f billing.Filter, query, order string, args []any, scan func(pgx.Row) (T, error)) (billing.Page[T], error) {
	result := billing.NewPage[T](nil, 0, f)
	if err := s.billingPlatform(ctx); err != nil {
		return result, err
	}
	f = f.Normalize()
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	var total int64
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM (`+query+`) billing_rows`, args...).Scan(&total); err != nil {
		return result, err
	}
	// LIMIT/OFFSET are normalized bounded integers, never request text.
	rows, err := tx.Query(ctx, query+order+` LIMIT `+billingInt(f.PageSize)+` OFFSET `+billingInt((f.Page-1)*f.PageSize), args...)
	if err != nil {
		return result, err
	}
	items := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return billing.NewPage(items, total, f), nil
}

func (s *Store) BillingLedger(ctx context.Context, user string, f billing.Filter) (billing.Page[LedgerEntry], error) {
	query := `SELECT id,user_id,change_amount,balance_after,biz_type,biz_id,remark,created_at FROM credit_ledger WHERE user_id=$1`
	return billingPage(ctx, s, f, query, ` ORDER BY created_at DESC,id DESC`, []any{user}, func(row pgx.Row) (LedgerEntry, error) {
		var e LedgerEntry
		err := row.Scan(&e.ID, &e.UserID, &e.Change, &e.Balance, &e.Kind, &e.BusinessID, &e.Remark, &e.CreatedAt)
		return e, err
	})
}
