package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

func billingFixture(t *testing.T) (*Store, User, billing.Plan, billing.Channel, billing.ChannelConfig) {
	t.Helper()
	s := testStore(t, config.Platform)
	u := testUser(t, s, "billing-owner@example.invalid")
	p, err := s.SaveBillingPlan(context.Background(), billing.Plan{ID: "plan", Name: "Test credits", Credits: 50, PriceCents: 1250, Currency: "CNY", Enabled: true}, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := billing.ChannelConfig{Mode: "ykmcn_legacy", BaseURL: "https://gateway.example.invalid", PID: "fixture-merchant", Key: "fixture-key-only", NotifyURL: "https://app.example.invalid/notify", ReturnURLTemplate: "https://app.example.invalid/credits"}
	c := billingTestChannel(t, s, u.ID, "test-channel", "ykmcn", cfg)
	return s, u, p, c, cfg
}

func billingTestChannel(t *testing.T, s *Store, actor, id, code string, cfg billing.ChannelConfig) billing.Channel {
	t.Helper()
	vault, err := security.NewVault([]byte(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg)
	cipher, err := vault.Encrypt("billing/channel/"+id, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveBillingChannel(context.Background(), billing.Channel{ID: id, Code: code, Name: "Test channel", Visible: true, Enabled: true, Ciphertext: cipher}, actor)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func billingTestOrder(t *testing.T, s *Store, u User, p billing.Plan, c billing.Channel, cfg billing.ChannelConfig) billing.Order {
	t.Helper()
	o, err := s.CreateBillingOrder(context.Background(), u.ID, p.ID, c.Code, "", c.Version, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func billingPayment(o billing.Order, trade string) billing.Payment {
	return billing.Payment{OrderID: o.ID, Adapter: o.Adapter, AccountHash: o.AccountHash, TradeNo: trade, AmountCents: o.AmountCents, Currency: o.Currency}
}

func billingConcurrent(t *testing.T, n int, fn func(int) error) []error {
	t.Helper()
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs[i] = fn(i) }()
	}
	close(start)
	wg.Wait()
	return errs
}

func TestPostgresBillingSettlementExactlyOnce(t *testing.T) {
	s, u, p, c, cfg := billingFixture(t)
	ctx := context.Background()
	o := billingTestOrder(t, s, u, p, c, cfg)
	var ledger string
	var mu sync.Mutex
	errs := billingConcurrent(t, 32, func(i int) error {
		var r billing.PaymentResult
		var err error
		if i%3 == 0 {
			r, err = s.ConfirmBillingOrder(ctx, o.ID, u.ID)
		} else {
			r, err = s.SettleBillingPayment(ctx, billingPayment(o, "external-once"))
		}
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if ledger == "" {
			ledger = r.LedgerID
		}
		if ledger != r.LedgerID || r.BalanceAfter != p.Credits {
			return fmt.Errorf("settlement result mismatch")
		}
		return nil
	})
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	user, _ := s.UserByID(ctx, u.ID)
	if user.Credits != p.Credits {
		t.Fatal("duplicate balance change")
	}
	var count int
	for _, table := range []string{"credit_ledger", "billing_receipts"} {
		if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s must contain exactly one entry", table)
		}
	}
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='recharge_order_confirm'").Scan(&count); err != nil || count != 1 {
		t.Fatal("confirmation audit duplicated")
	}
	wrong := billingPayment(o, "external-once")
	wrong.AmountCents++
	if _, err := s.SettleBillingPayment(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("mutated retry accepted")
	}
	wrong = billingPayment(o, "different-trade")
	if _, err := s.SettleBillingPayment(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("second trade rebound to paid order")
	}
}

func TestPostgresBillingProviderTradeCannotFundTwoChannels(t *testing.T) {
	s, u, p, c, cfg := billingFixture(t)
	ctx := context.Background()
	other := billingTestChannel(t, s, u.ID, "alias", "ykmcn_wxpay", cfg)
	first := billingTestOrder(t, s, u, p, c, cfg)
	second := billingTestOrder(t, s, u, p, other, cfg)
	var success atomic.Int32
	orders := []billing.Order{first, second}
	errs := billingConcurrent(t, 2, func(i int) error {
		_, err := s.SettleBillingPayment(ctx, billingPayment(orders[i], "one-provider-trade"))
		if err == nil {
			success.Add(1)
		}
		return err
	})
	if success.Load() != 1 {
		t.Fatal("external trade funded multiple orders")
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	actual, _ := s.UserByID(ctx, u.ID)
	if actual.Credits != p.Credits {
		t.Fatal("credit receipt uniqueness failed")
	}
}

func TestPostgresBillingRejectsWrongEvidenceAndRollsBackOverflow(t *testing.T) {
	s, u, p, c, cfg := billingFixture(t)
	ctx := context.Background()
	o := billingTestOrder(t, s, u, p, c, cfg)
	for _, mutate := range []func(*billing.Payment){func(p *billing.Payment) { p.AmountCents-- }, func(p *billing.Payment) { p.AmountCents = -1 }, func(p *billing.Payment) { p.Currency = "USD" }, func(p *billing.Payment) { p.AccountHash = "wrong" }, func(p *billing.Payment) { p.Adapter = "afdian" }, func(p *billing.Payment) { p.TradeNo = "" }} {
		event := billingPayment(o, "fixture")
		mutate(&event)
		if _, err := s.SettleBillingPayment(ctx, event); !errors.Is(err, billing.ErrInvalid) {
			t.Fatal("bad evidence accepted", err)
		}
	}
	if _, err := s.AdjustCredits(ctx, u.ID, math.MaxInt64-10, "test", "opening", "", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleBillingPayment(ctx, billingPayment(o, "overflow")); !errors.Is(err, ErrConflict) {
		t.Fatal("overflow not rejected", err)
	}
	after, _ := s.BillingOrder(ctx, o.ID, u.ID)
	if after.Status != "pending" {
		t.Fatal("failed settlement mutated order")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM billing_receipts").Scan(&count)
	if count != 0 {
		t.Fatal("failed transaction retained receipt")
	}
}

func TestPostgresBillingCancellationAndLatePayment(t *testing.T) {
	s, u, p, c, cfg := billingFixture(t)
	ctx := context.Background()
	userCancelled := billingTestOrder(t, s, u, p, c, cfg)
	r, err := s.CancelBillingOrder(ctx, userCancelled.ID, u.ID, u.ID, false)
	if err != nil || r.Status != "failed" {
		t.Fatal("user cancel", err)
	}
	if r, err = s.SettleBillingPayment(ctx, billingPayment(userCancelled, "late")); err != nil || r.Status != "paid" {
		t.Fatal("verified late payment not credited", err)
	}
	if r, err = s.CancelBillingOrder(ctx, userCancelled.ID, u.ID, u.ID, false); err != nil || r.Status != "paid" {
		t.Fatal("user cancellation changed a paid order")
	}
	if _, err = s.CancelBillingOrder(ctx, userCancelled.ID, "", u.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatal("admin cancelled paid order")
	}
	for i := range 8 {
		o := billingTestOrder(t, s, u, p, c, cfg)
		errs := billingConcurrent(t, 2, func(n int) error {
			if n == 0 {
				_, err := s.CancelBillingOrder(ctx, o.ID, "", u.ID, true)
				return err
			}
			_, err := s.SettleBillingPayment(ctx, billingPayment(o, fmt.Sprintf("race-%d", i)))
			return err
		})
		for _, err := range errs {
			if err != nil && !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
		}
		after, _ := s.BillingOrder(ctx, o.ID, u.ID)
		var count int
		s.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE biz_type='recharge' AND biz_id=$1", o.ID).Scan(&count)
		if after.Status == "paid" && count != 1 || after.Status == "cancelled" && count != 0 || (after.Status != "paid" && after.Status != "cancelled") {
			t.Fatal("cancel/settle race violated terminal invariants")
		}
	}
	expired := billingTestOrder(t, s, u, p, c, cfg)
	s.Pool.Exec(ctx, "UPDATE recharge_orders SET expires_at=now()-interval '1 minute' WHERE id=$1", expired.ID)
	if r, err = s.ExpireBillingOrder(ctx, expired.ID); err != nil || r.Status != "failed" {
		t.Fatal("expiration failed")
	}
	if _, err = s.SettleBillingPayment(ctx, billingPayment(expired, "after-expiration")); err != nil {
		t.Fatal("late expired payment lost", err)
	}
}

func TestPostgresBillingSnapshotIdempotencyVisibilityAndPagination(t *testing.T) {
	s, u, p, c, cfg := billingFixture(t)
	ctx := context.Background()
	var id string
	var mu sync.Mutex
	errs := billingConcurrent(t, 16, func(int) error {
		o, err := s.CreateBillingOrder(ctx, u.ID, p.ID, c.Code, "create-once", c.Version, cfg)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if id == "" {
			id = o.ID
		}
		if id != o.ID {
			return errors.New("create duplicate")
		}
		return nil
	})
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	p.Credits = 999
	p.PriceCents = 5000
	if _, err := s.SaveBillingPlan(ctx, p, u.ID); err != nil {
		t.Fatal(err)
	}
	o, _ := s.BillingOrder(ctx, id, u.ID)
	if o.Credits != 50 || o.AmountCents != 1250 {
		t.Fatal("order snapshot changed")
	}
	if r, err := s.ConfirmBillingOrder(ctx, id, u.ID); err != nil || r.Credits != 50 {
		t.Fatal("paid current plan rather than snapshot")
	}
	for range 5 {
		billingTestOrder(t, s, u, p, c, cfg)
	}
	f := billing.Filter{UserID: u.ID, Page: 2, PageSize: 2}
	page, err := s.BillingOrders(ctx, f)
	if err != nil || page.Total != 6 || page.TotalPages != 3 || len(page.Rows) != 2 {
		t.Fatal("order pagination", err)
	}
	f.Page = 999
	page, err = s.BillingOrders(ctx, f)
	if err != nil || len(page.Rows) != 0 || page.Total != 6 {
		t.Fatal("empty page total")
	}
	other, err := s.CreateUser(ctx, "other@example.invalid", "fixture", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BillingOrder(ctx, id, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("order ownership bypass")
	}
	ledger, err := s.BillingLedger(ctx, other.ID, billing.Filter{})
	if err != nil || ledger.Total != 0 {
		t.Fatal("ledger ownership bypass")
	}
	ledger, err = s.BillingLedger(ctx, u.ID, billing.Filter{PageSize: 1})
	if err != nil || ledger.Total != 1 || len(ledger.Rows) != 1 {
		t.Fatal("ledger pagination")
	}
	c.Enabled = false
	c, err = s.SaveBillingChannel(ctx, c, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := billingTestOrder(t, s, u, p, c, cfg)
	if disabled.Status != "channel_disabled" {
		t.Fatal("disabled channel status")
	}
	if _, err = s.ConfirmBillingOrder(ctx, disabled.ID, u.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("disabled channel settled")
	}
	allowed := []string{"other-plan"}
	cfg.AllowedPlanIDs = &allowed
	if _, err = s.CreateBillingOrder(ctx, u.ID, p.ID, c.Code, "", c.Version, cfg); !errors.Is(err, billing.ErrInvalid) {
		t.Fatal("allowlist bypass")
	}
	c.Visible = false
	c, err = s.SaveBillingChannel(ctx, c, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AllowedPlanIDs = nil
	if _, err = s.CreateBillingOrder(ctx, u.ID, p.ID, c.Code, "", c.Version, cfg); !errors.Is(err, ErrNotFound) {
		t.Fatal("hidden channel created order")
	}
}

func TestPostgresBillingRedemptionExhaustionAndExclusiveWave(t *testing.T) {
	s, u, _, _, _ := billingFixture(t)
	ctx := context.Background()
	wave, err := s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 7, Quantity: 1, MaxUses: 3}, u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	users := []User{}
	for i := range 16 {
		v, err := s.CreateUser(ctx, fmt.Sprintf("redeem%d@example.invalid", i), "fixture", "Test")
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, v)
	}
	var success atomic.Int32
	errs := billingConcurrent(t, len(users), func(i int) error {
		_, err := s.RedeemBillingCode(ctx, users[i].ID, strings.ToLower(wave.Codes[0].Code))
		if err == nil {
			success.Add(1)
		}
		return err
	})
	if success.Load() != 3 {
		t.Fatal("redemption capacity not enforced")
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	page, err := s.BillingCodes(ctx, billing.Filter{Status: "used_up"})
	if err != nil || page.Total != 1 || page.Rows[0].UsedCount != 3 || page.Rows[0].RecordCount != 3 {
		t.Fatal("used-up counts", err)
	}
	wave, err = s.CreateBillingCodes(ctx, billing.CodeRequest{Name: "Exclusive", Tiers: []billing.CodeTier{{Credits: 11, Quantity: 12, MaxUses: 2}, {Credits: 17, Quantity: 1}}}, u.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	success.Store(0)
	errs = billingConcurrent(t, len(wave.Codes), func(i int) error {
		_, err := s.RedeemBillingCode(ctx, u.ID, wave.Codes[i].Code)
		if err == nil {
			success.Add(1)
		}
		return err
	})
	if success.Load() != 1 {
		t.Fatal("exclusive wave allowed multiple claims")
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	actual, _ := s.UserByID(ctx, u.ID)
	if actual.Credits != 11 && actual.Credits != 17 {
		t.Fatal("exclusive wave balance")
	}
	records, err := s.BillingRedemptions(ctx, billing.Filter{Keyword: wave.BatchID})
	if err != nil || records.Total != 1 {
		t.Fatal("record pagination/filter")
	}
	if strings.Contains(records.Rows[0].UserEmailMasked, "billing-owner@") {
		t.Fatal("redemption record email unmasked")
	}
}

func TestPostgresBillingRedemptionDuplicateExpiryValidationAndRollback(t *testing.T) {
	s, u, _, _, _ := billingFixture(t)
	ctx := context.Background()
	created, err := s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 9, MaxUses: 20}, u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	errs := billingConcurrent(t, 16, func(int) error {
		_, err := s.RedeemBillingCode(ctx, u.ID, created.Codes[0].Code)
		if err == nil {
			success.Add(1)
		}
		return err
	})
	if success.Load() != 1 {
		t.Fatal("same user redeemed code more than once")
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if _, err = s.CreateBillingCodes(ctx, billing.CodeRequest{Tiers: []billing.CodeTier{{Credits: 10, Quantity: 2}, {Credits: -1, Quantity: 2}}}, u.ID, true); !errors.Is(err, billing.ErrInvalid) {
		t.Fatal("invalid wave partially created")
	}
	codes, err := s.BillingCodes(ctx, billing.Filter{})
	if err != nil || codes.Total != 1 {
		t.Fatal("invalid wave changed inventory")
	}
	expires := time.Now().Add(-time.Hour)
	if _, err = s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 1, ExpiresAt: &expires}, u.ID, false); !errors.Is(err, billing.ErrInvalid) {
		t.Fatal("past expiry accepted")
	}
	expired, err := s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 1}, u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Pool.Exec(ctx, "UPDATE redemption_codes SET expires_at=now()-interval '1 second' WHERE id=$1", expired.Codes[0].ID)
	if _, err = s.RedeemBillingCode(ctx, u.ID, expired.Codes[0].Code); !errors.Is(err, ErrConflict) {
		t.Fatal("expired code accepted")
	}
	overflow, err := s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: math.MaxInt64}, u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemBillingCode(ctx, u.ID, overflow.Codes[0].Code); !errors.Is(err, ErrConflict) {
		t.Fatal("redemption overflow accepted")
	}
	codes, err = s.BillingCodes(ctx, billing.Filter{Keyword: overflow.Codes[0].ID})
	if err != nil || codes.Rows[0].UsedCount != 0 {
		t.Fatal("failed redemption consumed code")
	}
	records, err := s.BillingRedemptions(ctx, billing.Filter{})
	if err != nil || records.Total != 1 {
		t.Fatal("failed redemption retained record")
	}
}

func TestPostgresBillingPersonalDefenseAndReconciliationReservations(t *testing.T) {
	personal := testStore(t, config.Personal)
	u := testUser(t, personal, "personal@example.invalid")
	ctx := context.Background()
	if _, err := personal.BillingPlans(ctx, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("personal billing read")
	}
	if _, err := personal.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 1}, u.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatal("personal billing write")
	}
	personal.Edition = config.Platform
	if _, err := personal.BillingChannels(ctx, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("mutable store edition bypassed immutable database")
	}
	if _, err := personal.Pool.Exec(ctx, `INSERT INTO recharge_plans(id,name,credits,price_cents,currency) VALUES('x','x',1,1,'CNY')`); err == nil {
		t.Fatal("database accepted Personal business")
	}
	s, owner, p, c, cfg := billingFixture(t)
	for range 8 {
		billingTestOrder(t, s, owner, p, c, cfg)
	}
	var count atomic.Int32
	seen := map[string]bool{}
	var mu sync.Mutex
	errs := billingConcurrent(t, 4, func(int) error {
		rows, err := s.BillingReconciliationOrders(ctx, 3)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		for _, o := range rows {
			if seen[o.ID] {
				return errors.New("duplicate sweep reservation")
			}
			seen[o.ID] = true
			count.Add(1)
		}
		return nil
	})
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 8 {
		t.Fatal("sweep did not cover pending orders")
	}
	if rows, err := s.BillingReconciliationOrders(ctx, 100); err != nil || len(rows) != 0 {
		t.Fatal("reserved sweep repeated immediately")
	}
}

func TestPostgresBillingRedemptionExpiryWhileWaitingForBalanceLock(t *testing.T) {
	s, u, _, _, _ := billingFixture(t)
	ctx := context.Background()
	expires := time.Now().Add(200 * time.Millisecond)
	codes, err := s.CreateBillingCodes(ctx, billing.CodeRequest{Credits: 5, ExpiresAt: &expires}, u.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", u.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.RedeemBillingCode(ctx, u.ID, codes.Codes[0].Code); done <- err }()
	time.Sleep(time.Until(expires) + 100*time.Millisecond)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrConflict) {
		t.Fatal("code expired while blocked but was still redeemed", err)
	}
	user, _ := s.UserByID(ctx, u.ID)
	if user.Credits != 0 {
		t.Fatal("expired redemption balance did not roll back")
	}
	page, err := s.BillingRedemptions(ctx, billing.Filter{})
	if err != nil || page.Total != 0 {
		t.Fatal("expired redemption retained record")
	}
}
