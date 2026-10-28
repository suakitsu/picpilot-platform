package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/security"
)

func billingInt(v int) string { return strconv.Itoa(v) }

// A short SKIP LOCKED reservation lets multiple schedulers sweep fairly without
// holding database locks during network queries. Settlement remains idempotent.
func (s *Store) BillingReconciliationOrders(ctx context.Context, limit int) ([]billing.Order, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.Pool.Query(ctx, `WITH candidates AS (SELECT id FROM recharge_orders WHERE status='pending' AND adapter IN ('afdian','ykmcn') AND next_check_at<=now() ORDER BY next_check_at,id LIMIT $1 FOR UPDATE SKIP LOCKED) UPDATE recharge_orders o SET next_check_at=now()+interval '1 minute' FROM candidates c WHERE o.id=c.id RETURNING `+billingOrderColumns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []billing.Order{}
	for rows.Next() {
		o, err := scanBillingOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

const billingOrderColumns = `o.id,o.user_id,o.plan_id,o.channel_id,o.channel_code,o.channel_version,o.config_ciphertext,o.adapter,o.account_hash,o.amount_cents,o.credits,o.currency,o.status,o.remark,o.external_trade_no,o.expires_at,o.paid_at,o.created_at,o.updated_at`

func scanBillingOrder(row pgx.Row) (billing.Order, error) {
	var o billing.Order
	err := row.Scan(&o.ID, &o.UserID, &o.PlanID, &o.ChannelID, &o.ChannelCode, &o.ChannelVersion, &o.Ciphertext, &o.Adapter, &o.AccountHash, &o.AmountCents, &o.Credits, &o.Currency, &o.Status, &o.Remark, &o.ExternalTradeNo, &o.ExpiresAt, &o.PaidAt, &o.CreatedAt, &o.UpdatedAt)
	return o, normalizeError(err)
}

func (s *Store) BillingOrder(ctx context.Context, id, user string) (billing.Order, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return billing.Order{}, err
	}
	return scanBillingOrder(s.Pool.QueryRow(ctx, `SELECT `+billingOrderColumns+` FROM recharge_orders o WHERE o.id=$1 AND ($2='' OR o.user_id=$2)`, id, user))
}

func (s *Store) CreateBillingOrder(ctx context.Context, user, planID, channelCode, requestKey string, version int64, cfg billing.ChannelConfig) (billing.Order, error) {
	var o billing.Order
	if err := s.billingPlatform(ctx); err != nil {
		return o, err
	}
	if len(requestKey) > 200 || !billingID.MatchString(planID) || !billingID.MatchString(channelCode) {
		return o, billing.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return o, err
	}
	defer tx.Rollback(context.Background())
	// Lock the owner first to serialize creation retries and active-account checks.
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR NO KEY UPDATE`, user).Scan(&status); err != nil {
		return o, normalizeError(err)
	}
	if status != "active" {
		return o, ErrDisabled
	}
	if requestKey != "" {
		o, err = scanBillingOrder(tx.QueryRow(ctx, `SELECT `+billingOrderColumns+` FROM recharge_orders o WHERE user_id=$1 AND request_key=$2`, user, requestKey))
		if err == nil {
			if o.PlanID != planID || o.ChannelCode != channelCode {
				return o, ErrConflict
			}
			return o, tx.Commit(ctx)
		}
		if !errors.Is(err, ErrNotFound) {
			return o, err
		}
	}
	p, err := scanBillingPlan(tx.QueryRow(ctx, `SELECT `+billingPlanColumns+` FROM recharge_plans WHERE id=$1 AND enabled FOR SHARE`, planID))
	if err != nil {
		return o, err
	}
	c, err := scanBillingChannel(tx.QueryRow(ctx, `SELECT `+billingChannelColumns+` FROM payment_channels WHERE code=$1 AND visible FOR SHARE`, channelCode))
	if err != nil {
		return o, err
	}
	if c.Version != version {
		return o, ErrConflict
	}
	if !cfg.Allows(planID) {
		return o, billing.ErrInvalid
	}
	adapter := cfg.Adapter(c.Code)
	if c.Enabled {
		if err = cfg.Ready(adapter); err != nil {
			return o, err
		}
	}
	if (adapter == "ykmcn" || adapter == "afdian") && p.Currency != "CNY" {
		return o, billing.ErrInvalid
	}
	status = "pending"
	remark := "Awaiting payment or administrator confirmation"
	if !c.Enabled {
		status = "channel_disabled"
		remark = "Payment channel is disabled"
	}
	o, err = scanBillingOrder(tx.QueryRow(ctx, `INSERT INTO recharge_orders AS o(id,user_id,plan_id,channel_id,channel_code,channel_version,config_ciphertext,adapter,account_hash,amount_cents,credits,currency,status,remark,request_key,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),now()+$16*interval '1 minute') RETURNING `+billingOrderColumns, security.NewID("ro"), user, p.ID, c.ID, c.Code, c.Version, c.Ciphertext, adapter, cfg.AccountHash(adapter), p.PriceCents, p.Credits, p.Currency, status, remark, requestKey, cfg.Window(adapter)))
	if err != nil {
		return o, normalizeError(err)
	}
	return o, tx.Commit(ctx)
}

func (s *Store) BillingOrders(ctx context.Context, f billing.Filter) (billing.Page[billing.Order], error) {
	f = f.Normalize()
	query := `SELECT ` + billingOrderColumns + ` FROM recharge_orders o JOIN users u ON u.id=o.user_id WHERE ($1='' OR o.user_id=$1) AND ($2='' OR o.status=$2) AND ($3='' OR o.channel_code=$3) AND ($4='' OR strpos(lower(concat_ws(' ',o.id,o.user_id,o.channel_code,o.external_trade_no,o.remark,u.email,u.nickname)),lower($4))>0) AND ($5::timestamptz IS NULL OR o.created_at >= $5) AND ($6::timestamptz IS NULL OR o.created_at <= $6)`
	page, err := billingPage(ctx, s, f, query, ` ORDER BY o.created_at DESC,o.id DESC`, []any{f.UserID, f.Status, f.Channel, f.Keyword, f.From, f.To}, scanBillingOrder)
	if err != nil {
		return page, err
	}
	if f.UserID == "" {
		for i := range page.Rows {
			u, err := s.UserByID(ctx, page.Rows[i].UserID)
			if err != nil {
				return page, err
			}
			page.Rows[i].UserEmailMasked = billingMask(u.Email)
			page.Rows[i].UserNickname = u.Nickname
		}
	}
	return page, nil
}

func (s *Store) BillingSummary(ctx context.Context, user string) (map[string]int64, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return nil, err
	}
	var credits, pending int64
	err := s.Pool.QueryRow(ctx, `SELECT credits,(SELECT count(*) FROM recharge_orders WHERE user_id=$1 AND status='pending') FROM users WHERE id=$1`, user).Scan(&credits, &pending)
	return map[string]int64{"credits": credits, "pending_orders": pending}, normalizeError(err)
}

func (s *Store) ConfirmBillingOrder(ctx context.Context, id, actor string) (billing.PaymentResult, error) {
	return s.settleBillingOrder(ctx, id, actor, nil)
}
func (s *Store) SettleBillingPayment(ctx context.Context, p billing.Payment) (billing.PaymentResult, error) {
	return s.settleBillingOrder(ctx, p.OrderID, "", &p)
}

func (s *Store) settleBillingOrder(ctx context.Context, id, actor string, p *billing.Payment) (billing.PaymentResult, error) {
	var result billing.PaymentResult
	if err := s.billingPlatform(ctx); err != nil {
		return result, err
	}
	if p == nil && actor == "" {
		return result, billing.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	o, err := scanBillingOrder(tx.QueryRow(ctx, `SELECT `+billingOrderColumns+` FROM recharge_orders o WHERE o.id=$1 FOR UPDATE`, id))
	if err != nil {
		return result, err
	}
	if o.Status != "pending" && o.Status != "failed" && o.Status != "paid" {
		return result, ErrConflict
	}
	if p != nil {
		if err = billing.ValidatePayment(o, *p); err != nil {
			return result, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO billing_receipts(adapter,account_hash,trade_no,order_id,amount_cents,currency) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, p.Adapter, p.AccountHash, p.TradeNo, o.ID, p.AmountCents, p.Currency)
		if err != nil {
			return result, err
		}
		var orderID string
		var amount int64
		var currency string
		err = tx.QueryRow(ctx, `SELECT order_id,amount_cents,currency FROM billing_receipts WHERE adapter=$1 AND account_hash=$2 AND trade_no=$3`, p.Adapter, p.AccountHash, p.TradeNo).Scan(&orderID, &amount, &currency)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrConflict
		}
		if err != nil {
			return result, err
		}
		if orderID != o.ID || amount != p.AmountCents || currency != p.Currency {
			return result, ErrConflict
		}
	}
	wasPaid := o.Status == "paid"
	e, err := adjustCredits(ctx, tx, o.UserID, o.Credits, "recharge", o.ID, "Recharge order confirmed", actor)
	if err != nil {
		return result, err
	}
	trade := o.ExternalTradeNo
	if p != nil {
		trade = p.TradeNo
	}
	remark := "Administrator confirmed payment"
	if p != nil {
		remark = "Verified provider payment"
	}
	_, err = tx.Exec(ctx, `UPDATE recharge_orders SET status='paid',external_trade_no=$2,paid_at=COALESCE(paid_at,now()),remark=CASE WHEN status='paid' THEN remark ELSE $3 END,updated_at=now() WHERE id=$1`, id, trade, remark)
	if err != nil {
		return result, normalizeError(err)
	}
	if !wasPaid {
		if err = billingAudit(ctx, tx, actor, "recharge_order_confirm", id, map[string]any{"ledger_id": e.ID, "credits": e.Change, "verified_provider": p != nil}); err != nil {
			return result, err
		}
	}
	result = billing.PaymentResult{OrderID: id, Status: "paid", BalanceAfter: e.Balance, Credits: o.Credits, LedgerID: e.ID, Idempotent: wasPaid}
	return result, tx.Commit(ctx)
}

// User cancellation keeps the legacy 'failed' status: verified late payments
// may still settle. Administrator cancellation is terminal and never settles.
func (s *Store) CancelBillingOrder(ctx context.Context, id, user, actor string, admin bool) (billing.PaymentResult, error) {
	return s.closeBillingOrder(ctx, id, user, actor, admin, false)
}
func (s *Store) ExpireBillingOrder(ctx context.Context, id string) (billing.PaymentResult, error) {
	return s.closeBillingOrder(ctx, id, "", "", false, true)
}
func (s *Store) closeBillingOrder(ctx context.Context, id, user, actor string, admin, onlyExpired bool) (billing.PaymentResult, error) {
	var result billing.PaymentResult
	if err := s.billingPlatform(ctx); err != nil {
		return result, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	o, err := scanBillingOrder(tx.QueryRow(ctx, `SELECT `+billingOrderColumns+` FROM recharge_orders o WHERE o.id=$1 AND ($2='' OR user_id=$2) FOR UPDATE`, id, user))
	if err != nil {
		return result, err
	}
	if admin && o.Status == "paid" {
		return result, ErrConflict
	}
	result, err = billingResult(ctx, tx, o)
	if err != nil {
		return result, err
	}
	if o.Status == "paid" || o.Status == "cancelled" || (!admin && o.Status != "pending") || (onlyExpired && o.ExpiresAt.After(time.Now())) {
		result.Idempotent = true
		return result, tx.Commit(ctx)
	}
	status, remark := "failed", "Payment cancelled by user"
	if onlyExpired {
		remark = "Payment window expired"
	}
	if admin {
		status = "cancelled"
		remark = "Administrator cancelled order"
	}
	_, err = tx.Exec(ctx, `UPDATE recharge_orders SET status=$2,remark=$3,updated_at=now() WHERE id=$1`, id, status, remark)
	if err != nil {
		return result, err
	}
	if err = billingAudit(ctx, tx, actor, "recharge_order_cancel", id, map[string]any{"status": status}); err != nil {
		return result, err
	}
	result.Status = status
	result.Message = remark
	return result, tx.Commit(ctx)
}

func billingResult(ctx context.Context, tx pgx.Tx, o billing.Order) (billing.PaymentResult, error) {
	r := billing.PaymentResult{OrderID: o.ID, Status: o.Status, Credits: o.Credits}
	var err error
	if o.Status == "paid" {
		err = tx.QueryRow(ctx, `SELECT id,balance_after FROM credit_ledger WHERE user_id=$1 AND biz_type='recharge' AND biz_id=$2`, o.UserID, o.ID).Scan(&r.LedgerID, &r.BalanceAfter)
	} else {
		err = tx.QueryRow(ctx, `SELECT credits FROM users WHERE id=$1`, o.UserID).Scan(&r.BalanceAfter)
	}
	return r, normalizeError(err)
}

func (s *Store) BillingOrderResult(ctx context.Context, id, user string) (billing.PaymentResult, error) {
	if err := s.billingPlatform(ctx); err != nil {
		return billing.PaymentResult{}, err
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return billing.PaymentResult{}, err
	}
	defer tx.Rollback(context.Background())
	o, err := scanBillingOrder(tx.QueryRow(ctx, `SELECT `+billingOrderColumns+` FROM recharge_orders o WHERE id=$1 AND ($2='' OR user_id=$2)`, id, user))
	if err != nil {
		return billing.PaymentResult{}, err
	}
	r, err := billingResult(ctx, tx, o)
	if err != nil {
		return r, err
	}
	r.Idempotent = true
	return r, tx.Commit(ctx)
}

func billingMask(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return "***"
	}
	runes := []rune(local)
	if len(runes) > 2 {
		return string(runes[:2]) + "***@" + domain
	}
	return "***@" + domain
}
