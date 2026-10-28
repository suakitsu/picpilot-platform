package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/store"
)

type billingHandler struct {
	*Server
	gateway billing.Gateway
}

func (s *Server) registerBillingRoutes() { s.registerBillingRoutesWithGateway(s.BillingGateway) }

func (s *Server) registerBillingRoutesWithGateway(gateway billing.Gateway) {
	if s.Config.Edition != config.Platform || s.Store.Edition != config.Platform {
		return
	}
	_, pattern := s.mux.Handler(&http.Request{Method: "GET", URL: &url.URL{Path: "/api/billing/summary"}})
	if pattern == "GET /api/billing/summary" {
		return
	}
	b := &billingHandler{Server: s, gateway: gateway}
	user := map[string]http.HandlerFunc{
		"GET /api/billing/summary":             b.summary,
		"GET /api/billing/plans":               b.plans,
		"GET /api/billing/channels":            b.channels,
		"GET /api/billing/orders":              b.orders,
		"POST /api/billing/orders":             b.createOrder,
		"POST /api/billing/orders/{id}/sync":   b.syncOrder,
		"POST /api/billing/orders/{id}/cancel": b.cancelOrder,
		"GET /api/billing/ledger":              b.ledger,
		"POST /api/billing/redeem":             b.redeem,
	}
	for path, handler := range user {
		s.mux.HandleFunc(path, s.auth(handler))
	}
	admin := map[string]http.HandlerFunc{
		"GET /api/admin/billing/plans":                 b.adminPlans,
		"POST /api/admin/billing/plans/{id}":           b.savePlan,
		"GET /api/admin/billing/channels":              b.adminChannels,
		"POST /api/admin/billing/channels/{id}":        b.saveChannel,
		"GET /api/admin/billing/orders":                b.adminOrders,
		"PATCH /api/admin/billing/orders/{id}/confirm": b.confirmOrder,
		"PATCH /api/admin/billing/orders/{id}/cancel":  b.adminCancelOrder,
		"GET /api/admin/billing/redemption-codes":      b.codes,
		"POST /api/admin/billing/redemption-codes":     b.createCodes,
		"POST /api/admin/benefits/card-waves":          b.createWave,
		"GET /api/admin/benefits/redemption-codes":     b.codes,
		"GET /api/admin/benefits/redemption-records":   b.records,
		"GET /api/admin/benefits/config":               b.benefitConfig,
		"POST /api/admin/benefits/config":              b.saveBenefitConfig,
		"POST /api/admin/benefits/qr":                  b.uploadBenefitQR,
	}
	for path, handler := range admin {
		s.mux.HandleFunc(path, s.auth(s.admin(handler)))
	}
	s.mux.HandleFunc("GET /api/public/benefit-config", b.benefitConfig)
	s.mux.HandleFunc("GET /api/public/benefits/qr/{id}", b.serveBenefitQR)
	s.mux.HandleFunc("POST /api/billing/afdian/webhook", b.afdianWebhook)
	s.mux.HandleFunc("GET /api/billing/ykmcn/notify", b.ykmcnNotify)
	s.mux.HandleFunc("POST /api/billing/ykmcn/notify", b.ykmcnNotify)
}

func (b *billingHandler) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, billing.ErrInvalid):
		fail(w, r, 400, "INVALID_BILLING_REQUEST", "Invalid billing request or payment evidence")
	case errors.Is(err, billing.ErrSignature):
		fail(w, r, 401, "INVALID_PAYMENT_SIGNATURE", "Payment authentication failed")
	case errors.Is(err, billing.ErrUnconfigured):
		fail(w, r, 503, "PAYMENT_UNCONFIGURED", "Payment channel is not configured")
	case errors.Is(err, billing.ErrUnpaid):
		fail(w, r, 400, "PAYMENT_NOT_FOUND", "No verified payment found; try again after completing payment")
	case errors.Is(err, billing.ErrUpstream):
		fail(w, r, 502, "PAYMENT_UNAVAILABLE", "Payment verification is temporarily unavailable")
	case errors.Is(err, store.ErrDisabled):
		fail(w, r, 403, "ACCOUNT_DISABLED", "Account is disabled")
	default:
		b.databaseError(w, r, err)
	}
}

func billingFilter(w http.ResponseWriter, r *http.Request, admin bool) (billing.Filter, bool) {
	q := r.URL.Query()
	f := billing.Filter{Page: 1, PageSize: 5, Status: strings.TrimSpace(q.Get("status"))}
	if admin {
		f.PageSize = 20
		f.Keyword = strings.TrimSpace(q.Get("keyword"))
		f.Channel = strings.TrimSpace(q.Get("channel"))
	}
	for key, dst := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize} {
		if q.Get(key) != "" {
			n, err := strconv.Atoi(q.Get(key))
			if err != nil || n < 1 {
				fail(w, r, 400, "INVALID_REQUEST", "Invalid pagination")
				return f, false
			}
			*dst = n
		}
	}
	if !admin && f.PageSize > 50 {
		f.PageSize = 50
	}
	if len(f.Keyword) > 200 || len(f.Status) > 40 || len(f.Channel) > 100 {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid filter")
		return f, false
	}
	if admin {
		for key, dst := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
			raw := q.Get(key)
			if raw == "" {
				continue
			}
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				value, err = time.Parse("2006-01-02", raw)
				if err == nil && key == "to" {
					value = value.Add(24*time.Hour - time.Nanosecond)
				}
			}
			if err != nil {
				fail(w, r, 400, "INVALID_REQUEST", "Invalid date filter")
				return f, false
			}
			*dst = &value
		}
		if f.From != nil && f.To != nil && f.From.After(*f.To) {
			fail(w, r, 400, "INVALID_REQUEST", "Invalid date range")
			return f, false
		}
	}
	return f.Normalize(), true
}

func (b *billingHandler) summary(w http.ResponseWriter, r *http.Request) {
	result, err := b.Store.BillingSummary(r.Context(), currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) plans(w http.ResponseWriter, r *http.Request)      { b.listPlans(w, r, false) }
func (b *billingHandler) adminPlans(w http.ResponseWriter, r *http.Request) { b.listPlans(w, r, true) }
func (b *billingHandler) listPlans(w http.ResponseWriter, r *http.Request, admin bool) {
	result, err := b.Store.BillingPlans(r.Context(), admin)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) channels(w http.ResponseWriter, r *http.Request) {
	b.listChannels(w, r, false)
}
func (b *billingHandler) adminChannels(w http.ResponseWriter, r *http.Request) {
	b.listChannels(w, r, true)
}
func (b *billingHandler) listChannels(w http.ResponseWriter, r *http.Request, admin bool) {
	result, err := b.Store.BillingChannels(r.Context(), admin)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if admin {
		for i := range result {
			cfg, err := b.channelConfig(result[i].ID, result[i].Ciphertext)
			if err != nil {
				b.failure(w, r, err)
				return
			}
			result[i].Config = cfg.RedactedJSON()
		}
	}
	ok(w, r, result)
}
func (b *billingHandler) channelConfig(id, ciphertext string) (billing.ChannelConfig, error) {
	raw, err := b.Vault.Decrypt("billing/channel/"+id, ciphertext)
	if err != nil {
		return billing.ChannelConfig{}, billing.ErrUnconfigured
	}
	cfg, err := billing.ParseConfig(raw)
	if err != nil {
		return cfg, billing.ErrUnconfigured
	}
	return cfg, nil
}

func (b *billingHandler) orders(w http.ResponseWriter, r *http.Request) { b.listOrders(w, r, false) }
func (b *billingHandler) adminOrders(w http.ResponseWriter, r *http.Request) {
	b.listOrders(w, r, true)
}
func (b *billingHandler) listOrders(w http.ResponseWriter, r *http.Request, admin bool) {
	f, valid := billingFilter(w, r, admin)
	if !valid {
		return
	}
	if !admin {
		f.UserID = currentUser(r).ID
	}
	result, err := b.Store.BillingOrders(r.Context(), f)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) ledger(w http.ResponseWriter, r *http.Request) {
	f, valid := billingFilter(w, r, false)
	if !valid {
		return
	}
	result, err := b.Store.BillingLedger(r.Context(), currentUser(r).ID, f)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}

func (b *billingHandler) createOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlanID               string `json:"plan_id"`
		ChannelCode          string `json:"channel_code"`
		TurnstileToken       string `json:"turnstile_token"`
		LegacyPlanID         string `json:"planId"`
		LegacyChannelCode    string `json:"channelCode"`
		LegacyTurnstileToken string `json:"turnstileToken"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.PlanID == "" {
		input.PlanID = input.LegacyPlanID
	}
	if input.ChannelCode == "" {
		input.ChannelCode = input.LegacyChannelCode
	}
	u := currentUser(r)
	if !b.limiter.allow("billing-order:"+u.ID, 20, 30*time.Minute) {
		fail(w, r, 429, "RATE_LIMITED", "Too many orders; try again later")
		return
	}
	c, err := b.Store.BillingChannel(r.Context(), input.ChannelCode, true)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	cfg, err := b.channelConfig(c.ID, c.Ciphertext)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	o, err := b.Store.CreateBillingOrder(r.Context(), u.ID, input.PlanID, input.ChannelCode, r.Header.Get("Idempotency-Key"), c.Version, cfg)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	// An idempotent replay must use the original, encrypted channel snapshot.
	cfg, err = b.channelConfig(o.ChannelID, o.Ciphertext)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	target, links, hint, err := cfg.Checkout(o)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	result := map[string]any{"order_id": o.ID, "status": o.Status, "message": o.Remark, "payment_hint": hint}
	if target != "" {
		result["payment_url"] = target
	}
	if len(links) > 0 {
		result["payment_links"] = links
	}
	ok(w, r, result)
}

func (b *billingHandler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	o, err := b.Store.BillingOrder(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if o.Status == "pending" && o.Adapter == "ykmcn" {
		result, err := b.syncPayment(r.Context(), o)
		if err == nil && result.Status == "paid" {
			ok(w, r, result)
			return
		}
		if err != nil && !errors.Is(err, billing.ErrUnpaid) {
			b.failure(w, r, err)
			return
		}
	}
	result, err := b.Store.CancelBillingOrder(r.Context(), r.PathValue("id"), currentUser(r).ID, currentUser(r).ID, false)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) confirmOrder(w http.ResponseWriter, r *http.Request) {
	result, err := b.Store.ConfirmBillingOrder(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) adminCancelOrder(w http.ResponseWriter, r *http.Request) {
	result, err := b.Store.CancelBillingOrder(r.Context(), r.PathValue("id"), "", currentUser(r).ID, true)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}

func (b *billingHandler) savePlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name       *string `json:"name"`
		Credits    *int64  `json:"credits"`
		PriceCents *int64  `json:"price_cents"`
		Currency   *string `json:"currency"`
		Enabled    *bool   `json:"enabled"`
		SortOrder  *int    `json:"sort_order"`
	}
	if !decode(w, r, &input) {
		return
	}
	p, err := b.Store.BillingPlan(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		p = billing.Plan{ID: r.PathValue("id"), Currency: "CNY"}
	} else if err != nil {
		b.failure(w, r, err)
		return
	}
	if input.Name != nil {
		p.Name = strings.TrimSpace(*input.Name)
	}
	if input.Credits != nil {
		p.Credits = *input.Credits
	}
	if input.PriceCents != nil {
		p.PriceCents = *input.PriceCents
	}
	if input.Currency != nil {
		p.Currency = strings.ToUpper(*input.Currency)
	}
	if input.Enabled != nil {
		p.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		p.SortOrder = *input.SortOrder
	}
	p, err = b.Store.SaveBillingPlan(r.Context(), p, currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, p)
}

func (b *billingHandler) saveChannel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      *string `json:"name"`
		Code      *string `json:"code"`
		Visible   *bool   `json:"visible"`
		Enabled   *bool   `json:"enabled"`
		SortOrder *int    `json:"sort_order"`
		Config    *string `json:"config"`
		Version   *int64  `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	c, err := b.Store.BillingChannel(r.Context(), r.PathValue("id"), false)
	if errors.Is(err, store.ErrNotFound) {
		c = billing.Channel{ID: r.PathValue("id"), Code: r.PathValue("id")}
	} else if err != nil {
		b.failure(w, r, err)
		return
	}
	if input.Version != nil && c.Version != *input.Version {
		b.failure(w, r, store.ErrConflict)
		return
	}
	if input.Code != nil {
		if c.Version > 0 && c.Code != *input.Code {
			b.failure(w, r, store.ErrConflict)
			return
		}
		c.Code = *input.Code
	}
	if input.Name != nil {
		c.Name = strings.TrimSpace(*input.Name)
	}
	if input.Visible != nil {
		c.Visible = *input.Visible
	}
	if input.Enabled != nil {
		c.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		c.SortOrder = *input.SortOrder
	}
	cfg, err := b.channelConfig(c.ID, c.Ciphertext)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if input.Config != nil && strings.TrimSpace(*input.Config) != "" {
		next, err := billing.ParseConfig(*input.Config)
		if err != nil {
			b.failure(w, r, err)
			return
		}
		next.PreserveCredentials(cfg)
		cfg = next
	}
	if c.Enabled {
		if err = cfg.Ready(cfg.Adapter(c.Code)); err != nil {
			b.failure(w, r, err)
			return
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		b.failure(w, r, billing.ErrInvalid)
		return
	}
	c.Ciphertext, err = b.Vault.Encrypt("billing/channel/"+c.ID, string(raw))
	if err != nil {
		b.failure(w, r, billing.ErrUnconfigured)
		return
	}
	c, err = b.Store.SaveBillingChannel(r.Context(), c, currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	c.Config = cfg.RedactedJSON()
	ok(w, r, c)
}
