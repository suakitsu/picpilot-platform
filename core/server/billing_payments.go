package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/billing"
	"github.com/suakitsu/picpilot-platform-go/core/config"
)

func (b *billingHandler) syncPayment(ctx context.Context, o billing.Order) (billing.PaymentResult, error) {
	if o.Status == "paid" || o.Status == "cancelled" || o.Status == "channel_disabled" {
		return b.Store.BillingOrderResult(ctx, o.ID, o.UserID)
	}
	cfg, err := b.channelConfig(o.ChannelID, o.Ciphertext)
	if err != nil {
		return billing.PaymentResult{}, err
	}
	p, err := b.gateway.Query(ctx, cfg, o, "")
	if err == nil {
		return b.Store.SettleBillingPayment(ctx, p)
	}
	if errors.Is(err, billing.ErrUnpaid) && !o.ExpiresAt.After(time.Now()) {
		return b.Store.ExpireBillingOrder(ctx, o.ID)
	}
	return billing.PaymentResult{}, err
}

func (b *billingHandler) syncOrder(w http.ResponseWriter, r *http.Request) {
	if !b.limiter.allow("billing-sync:"+currentUser(r).ID, 30, time.Minute) {
		fail(w, r, 429, "RATE_LIMITED", "Too many payment queries")
		return
	}
	o, err := b.Store.BillingOrder(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	result, err := b.syncPayment(r.Context(), o)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}

func (b *billingHandler) ykmcnNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	reject := func() { w.WriteHeader(http.StatusBadRequest); _, _ = io.WriteString(w, "fail") }
	if !b.limiter.allow("billing-notify:"+b.clientAddress(r), 240, time.Minute) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, "fail")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if r.ParseForm() != nil || len(r.Form) > 40 {
		reject()
		return
	}
	for _, values := range r.Form {
		if len(values) != 1 {
			reject()
			return
		}
	}
	o, err := b.Store.BillingOrder(r.Context(), r.Form.Get("out_trade_no"), "")
	if err != nil {
		reject()
		return
	}
	cfg, err := b.channelConfig(o.ChannelID, o.Ciphertext)
	if err != nil {
		reject()
		return
	}
	p, err := billing.VerifyYkmcn(r.Form, cfg, o)
	if err != nil {
		reject()
		return
	}
	if _, err = b.Store.SettleBillingPayment(r.Context(), p); err != nil {
		reject()
		return
	}
	_, _ = io.WriteString(w, "success")
}

func (b *billingHandler) afdianWebhook(w http.ResponseWriter, r *http.Request) {
	if !b.limiter.allow("billing-notify:"+b.clientAddress(r), 120, time.Minute) {
		fail(w, r, 429, "RATE_LIMITED", "Too many payment notifications")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		b.failure(w, r, billing.ErrInvalid)
		return
	}
	c, err := b.Store.BillingChannel(r.Context(), "afdian", true)
	if err != nil {
		b.failure(w, r, billing.ErrUnconfigured)
		return
	}
	cfg, err := b.channelConfig(c.ID, c.Ciphertext)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if err = billing.VerifyAfdian(body, r.Header, r.URL.Query().Get("secret"), cfg, time.Now()); err != nil {
		b.failure(w, r, err)
		return
	}
	trade, err := billing.AfdianTrade(body)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	// Neither amount nor local order ID from the callback is trusted. The
	// authenticated merchant query is the authoritative payment evidence.
	p, err := b.gateway.Query(r.Context(), cfg, billing.Order{Adapter: "afdian"}, trade)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if _, err = b.Store.SettleBillingPayment(r.Context(), p); err != nil {
		b.failure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ec": 200, "em": ""})
}

// ReconcileBilling is a bounded sweep for the platform lifecycle scheduler.
// Call periodically with a cancellable context; no goroutine is started here.
func (s *Server) ReconcileBilling(ctx context.Context, limit int) error {
	if s.Config.Edition != config.Platform || s.Store.Edition != config.Platform {
		return nil
	}
	if limit < 1 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	b := &billingHandler{Server: s, gateway: s.BillingGateway}
	orders, err := s.Store.BillingReconciliationOrders(ctx, limit)
	if err != nil {
		return err
	}
	var first error
	for _, o := range orders {
		if err = ctx.Err(); err != nil {
			return err
		}
		if o.Adapter == "manual" || o.Adapter == "mock" {
			continue
		}
		_, err = b.syncPayment(ctx, o)
		if err != nil && !errors.Is(err, billing.ErrUnpaid) && first == nil {
			first = err
		}
	}
	return first
}
