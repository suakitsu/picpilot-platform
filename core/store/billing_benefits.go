package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/billing"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

const billingCodeColumns = `c.id,c.code,c.credits,c.max_uses,c.used_count,c.expires_at,c.enabled,c.benefit_type,COALESCE(c.batch_id,''),COALESCE(b.name,''),COALESCE(b.exclusive,false),c.note,c.created_at`
const billingCodeStatus = `CASE WHEN c.expires_at<=now() THEN 'expired' WHEN NOT c.enabled THEN 'disabled' WHEN c.used_count>=c.max_uses THEN 'used_up' ELSE 'active' END`

func scanBillingCode(row pgx.Row) (billing.RedemptionCode, error) {
	var c billing.RedemptionCode
	err := row.Scan(&c.ID, &c.Code, &c.Credits, &c.MaxUses, &c.UsedCount, &c.ExpiresAt, &c.Enabled, &c.BenefitType, &c.BatchID, &c.BatchName, &c.BatchExclusive, &c.Note, &c.CreatedAt)
	c.RecordCount = c.UsedCount
	c.Status = "active"
	if c.UsedCount >= c.MaxUses {
		c.Status = "used_up"
	}
	if !c.Enabled {
		c.Status = "disabled"
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(time.Now()) {
		c.Status = "expired"
	}
	return c, normalizeError(err)
}

func (s *Store) CreateBillingCodes(ctx context.Context, req billing.CodeRequest, actor string, wave bool) (billing.Wave, error) {
	result := billing.Wave{Codes: []billing.RedemptionCode{}}
	if err := s.billingPlatform(ctx); err != nil {
		return result, err
	}
	tiers := req.Tiers
	limit := 500
	if !wave {
		limit = 200
		if req.Quantity == 0 {
			req.Quantity = 1
		}
		tiers = []billing.CodeTier{{Credits: req.Credits, Quantity: req.Quantity, MaxUses: req.MaxUses}}
	}
	if len(tiers) == 0 || len(tiers) > 500 || len(req.Note) > 2000 || len(req.BenefitType) > 120 || len(req.Name) > 160 || len(req.BatchName) > 160 {
		return result, billing.ErrInvalid
	}
	count := 0
	for i := range tiers {
		if tiers[i].MaxUses == 0 {
			tiers[i].MaxUses = 1
		}
		t := tiers[i]
		if t.Quantity < 1 || t.Quantity > limit || t.Credits <= 0 || t.MaxUses < 1 || t.MaxUses > 10000 {
			return result, billing.ErrInvalid
		}
		count += t.Quantity
		if count > limit {
			return result, billing.ErrInvalid
		}
	}
	prefix := strings.ToUpper(strings.TrimSpace(req.Prefix))
	if prefix == "" {
		prefix = "RC"
		if wave {
			prefix = "FL"
		}
	}
	if len(prefix) > 24 || !billingID.MatchString(prefix) {
		return result, billing.ErrInvalid
	}
	if req.ValidDays != nil {
		if *req.ValidDays < 1 || *req.ValidDays > 36500 {
			return result, billing.ErrInvalid
		}
		if req.ExpiresAt == nil {
			expires := time.Now().UTC().AddDate(0, 0, *req.ValidDays)
			req.ExpiresAt = &expires
		}
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return result, billing.ErrInvalid
	}
	batchID, batchName, exclusive := strings.TrimSpace(req.BatchID), strings.TrimSpace(req.BatchName), req.BatchExclusive
	if wave {
		exclusive = true
		batchName = strings.TrimSpace(req.Name)
		if batchName == "" {
			batchName = "Benefit wave " + time.Now().UTC().Format("2006-01-02")
		}
		if batchID == "" {
			batchID = security.NewID("wave")
		}
	}
	if batchID != "" && !billingID.MatchString(batchID) || exclusive && batchID == "" {
		return result, billing.ErrInvalid
	}
	if req.BenefitType == "" {
		req.BenefitType = "Redemption credits"
		if wave {
			req.BenefitType = "Benefit card"
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if batchID != "" {
		_, err = tx.Exec(ctx, `INSERT INTO benefit_batches(id,name,exclusive,created_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, batchID, batchName, exclusive, actor)
		if err != nil {
			return result, normalizeError(err)
		}
		var name string
		var ex bool
		if err = tx.QueryRow(ctx, `SELECT name,exclusive FROM benefit_batches WHERE id=$1 FOR SHARE`, batchID).Scan(&name, &ex); err != nil {
			return result, err
		}
		if name != batchName || ex != exclusive {
			return result, ErrConflict
		}
	}
	for _, tier := range tiers {
		for i := 0; i < tier.Quantity; i++ {
			c := billing.RedemptionCode{ID: security.NewID("rc"), Code: prefix + "-" + strings.ToUpper(strings.TrimPrefix(security.NewID("code"), "code_")), Credits: tier.Credits, MaxUses: tier.MaxUses, ExpiresAt: req.ExpiresAt, Enabled: true, BenefitType: req.BenefitType, BatchID: batchID, BatchName: batchName, BatchExclusive: exclusive, Status: "active", Note: req.Note}
			err = tx.QueryRow(ctx, `INSERT INTO redemption_codes(id,code,credits,max_uses,expires_at,benefit_type,batch_id,note,created_by) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9) RETURNING created_at`, c.ID, c.Code, c.Credits, c.MaxUses, c.ExpiresAt, c.BenefitType, c.BatchID, c.Note, actor).Scan(&c.CreatedAt)
			if err != nil {
				return result, normalizeError(err)
			}
			result.Codes = append(result.Codes, c)
		}
	}
	result.BatchID = batchID
	result.BatchName = batchName
	result.Quantity = len(result.Codes)
	if err = billingAudit(ctx, tx, actor, "redemption_codes_create", batchID, map[string]any{"quantity": result.Quantity, "exclusive": exclusive}); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) BillingCodes(ctx context.Context, f billing.Filter) (billing.Page[billing.RedemptionCode], error) {
	f = f.Normalize()
	query := `SELECT ` + billingCodeColumns + ` FROM redemption_codes c LEFT JOIN benefit_batches b ON b.id=c.batch_id WHERE ($1='' OR ` + billingCodeStatus + `=$1) AND ($2='' OR strpos(lower(concat_ws(' ',c.id,c.code,c.benefit_type,c.batch_id,b.name,c.note)),lower($2))>0)`
	return billingPage(ctx, s, f, query, ` ORDER BY c.created_at DESC,c.id DESC`, []any{f.Status, f.Keyword}, scanBillingCode)
}

func (s *Store) RedeemBillingCode(ctx context.Context, user, rawCode string) (billing.RedemptionResult, error) {
	var result billing.RedemptionResult
	if err := s.billingPlatform(ctx); err != nil {
		return result, err
	}
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(rawCode), " ", ""))
	if len(code) > 64 || !billingID.MatchString(code) {
		return result, billing.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	c, err := scanBillingCode(tx.QueryRow(ctx, `SELECT `+billingCodeColumns+` FROM redemption_codes c LEFT JOIN benefit_batches b ON b.id=c.batch_id WHERE c.code=$1 FOR UPDATE OF c`, code))
	if err != nil {
		return result, err
	}
	if !c.Enabled || c.UsedCount >= c.MaxUses || (c.ExpiresAt != nil && !c.ExpiresAt.After(time.Now())) {
		return result, ErrConflict
	}
	// The code lock bounds global uses; the owner lock serializes all codes in an
	// exclusive wave for this user. Unique indexes remain the final backstop.
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, user))
	if err != nil {
		return result, err
	}
	if u.Status != "active" {
		return result, ErrDisabled
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM redemption_records WHERE user_id=$1 AND (code_id=$2 OR ($3 AND batch_id=$4)))`, user, c.ID, c.BatchExclusive, c.BatchID).Scan(&exists)
	if err != nil {
		return result, err
	}
	if exists {
		return result, ErrConflict
	}
	recordID := security.NewID("rr")
	_, err = tx.Exec(ctx, `INSERT INTO redemption_records(id,code_id,batch_id,batch_exclusive,user_id,user_email_masked,user_nickname,credits) VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8)`, recordID, c.ID, c.BatchID, c.BatchExclusive, user, billingMask(u.Email), u.Nickname, c.Credits)
	if err != nil {
		return result, normalizeError(err)
	}
	e, err := adjustCredits(ctx, tx, user, c.Credits, "redeem_code", recordID, "Redemption code credit", "")
	if err != nil {
		return result, err
	}
	consumed, err := tx.Exec(ctx, `UPDATE redemption_codes SET used_count=used_count+1 WHERE id=$1 AND enabled AND used_count<max_uses AND (expires_at IS NULL OR expires_at>clock_timestamp())`, c.ID)
	if err != nil {
		return result, err
	}
	if consumed.RowsAffected() != 1 {
		return result, ErrConflict
	}
	result = billing.RedemptionResult{Credits: c.Credits, BalanceAfter: e.Balance, RecordID: recordID}
	return result, tx.Commit(ctx)
}

func (s *Store) BillingRedemptions(ctx context.Context, f billing.Filter) (billing.Page[billing.RedemptionRecord], error) {
	query := `SELECT r.id,c.code,r.code_id,COALESCE(r.batch_id,''),r.user_id,r.user_email_masked,r.user_nickname,r.credits,r.redeemed_at FROM redemption_records r JOIN redemption_codes c ON c.id=r.code_id WHERE ($1='' OR strpos(lower(concat_ws(' ',r.id,c.code,r.code_id,r.batch_id,r.user_id,r.user_email_masked,r.user_nickname)),lower($1))>0)`
	return billingPage(ctx, s, f, query, ` ORDER BY r.redeemed_at DESC,r.id DESC`, []any{f.Keyword}, func(row pgx.Row) (billing.RedemptionRecord, error) {
		var r billing.RedemptionRecord
		err := row.Scan(&r.ID, &r.Code, &r.CodeID, &r.BatchID, &r.UserID, &r.UserEmailMasked, &r.UserNickname, &r.Credits, &r.RedeemedAt)
		return r, err
	})
}

func (s *Store) BillingBenefitConfig(ctx context.Context) (billing.BenefitConfig, error) {
	var c billing.BenefitConfig
	if err := s.billingPlatform(ctx); err != nil {
		return c, err
	}
	raw, err := s.Setting(ctx, "benefit_onboarding")
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, nil
}

func (s *Store) PatchBillingBenefitConfig(ctx context.Context, patch map[string]json.RawMessage, actor string) (billing.BenefitConfig, error) {
	var c billing.BenefitConfig
	if err := s.billingPlatform(ctx); err != nil {
		return c, err
	}
	allowed := map[string]bool{"enabled": true, "title": true, "description": true, "qq_group": true, "qq_link": true, "wechat_group": true, "qq_qr_url": true, "wechat_qr_url": true, "redeem_tutorial": true, "free_highlight_label": true}
	for key, value := range patch {
		if !allowed[key] || len(value) > 5000 || string(value) == "null" {
			return c, billing.ErrInvalid
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(712496152246)`); err != nil {
		return c, err
	}
	existing := map[string]json.RawMessage{}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT value FROM app_settings WHERE key='benefit_onboarding'`).Scan(&raw)
	if err != nil && err != pgx.ErrNoRows {
		return c, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &existing); err != nil {
			return c, err
		}
	}
	for k, v := range patch {
		existing[k] = v
	}
	raw, err = json.Marshal(existing)
	if err != nil {
		return c, err
	}
	if json.Unmarshal(raw, &c) != nil {
		return c, billing.ErrInvalid
	}
	for _, value := range []string{c.QQLink, c.QQQRURL, c.WechatQRURL} {
		localQR := strings.HasPrefix(value, "/api/public/benefits/qr/") && billingID.MatchString(strings.TrimPrefix(value, "/api/public/benefits/qr/"))
		if value != "" && !billing.ValidURL(value) && !localQR {
			return c, billing.ErrInvalid
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES('benefit_onboarding',$1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_by=EXCLUDED.updated_by,version=app_settings.version+1,updated_at=now()`, raw, actor)
	if err != nil {
		return c, err
	}
	if err = billingAudit(ctx, tx, actor, "benefit_config_update", "benefit_onboarding", map[string]any{"enabled": c.Enabled}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
