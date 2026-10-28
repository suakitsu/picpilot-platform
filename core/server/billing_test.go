package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func billingHTTPServer(t *testing.T, edition config.Edition, gateway billing.Gateway) (*Server, store.User, string) {
	t.Helper()
	s := testServer(t, edition)
	// Install the injected gateway before ordinary route registration; the latter
	// is idempotent so parent wiring does not change these end-to-end tests.
	s.mux = http.NewServeMux()
	s.registerBillingRoutesWithGateway(gateway)
	s.routes()
	u, err := s.Store.Initialize(context.Background(), "billing-owner@example.invalid", "fixture-hash", "Billing owner")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, u, token
}

func billingHTTPData(t *testing.T, s *Server, method, path, token string, body any) map[string]any {
	t.Helper()
	status, result, _ := request(t, s, method, path, token, body)
	if status != 200 {
		t.Fatalf("%s %s returned %d (%v)", method, path, status, result["code"])
	}
	return result["data"].(map[string]any)
}
func billingHTTPCatalog(t *testing.T, s *Server, token, code string, cfg billing.ChannelConfig) {
	t.Helper()
	billingHTTPData(t, s, "POST", "/api/admin/billing/plans/test-plan", token, map[string]any{"name": "Fixture credits", "credits": 40, "price_cents": 1250, "currency": "CNY", "enabled": true, "sort_order": 1})
	raw, _ := json.Marshal(cfg)
	billingHTTPData(t, s, "POST", "/api/admin/billing/channels/"+code, token, map[string]any{"name": "Fixture channel", "code": code, "enabled": true, "visible": true, "sort_order": 1, "config": string(raw)})
}
func billingHTTPOrder(t *testing.T, s *Server, token, code string) string {
	t.Helper()
	data := billingHTTPData(t, s, "POST", "/api/billing/orders", token, map[string]any{"plan_id": "test-plan", "channel_code": code, "turnstile_token": ""})
	return data["order_id"].(string)
}
func billingUserToken(t *testing.T, s *Server, email string) (store.User, string) {
	t.Helper()
	u, err := s.Store.CreateUser(context.Background(), email, "fixture-hash", "Member")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Sessions.Issue(u.ID, u.AuthVersion, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestPostgresBillingHTTPManualMockOwnershipAndShapes(t *testing.T) {
	mock := &billing.Mock{}
	s, owner, admin := billingHTTPServer(t, config.Platform, mock)
	billingHTTPCatalog(t, s, admin, "mock", billing.ChannelConfig{})
	member, token := billingUserToken(t, s, "member@example.invalid")
	_, otherToken := billingUserToken(t, s, "other@example.invalid")
	if status, _, _ := request(t, s, "GET", "/api/billing/summary", "", nil); status != 401 {
		t.Fatal("anonymous billing session accepted")
	}
	if status, _, _ := request(t, s, "GET", "/api/admin/billing/orders", token, nil); status != 403 {
		t.Fatal("member granted admin access")
	}
	orderID := billingHTTPOrder(t, s, token, "mock")
	for _, action := range []string{"sync", "cancel"} {
		if status, _, _ := request(t, s, "POST", "/api/billing/orders/"+orderID+"/"+action, otherToken, nil); status != 404 {
			t.Fatal("cross-user order mutation")
		}
	}
	if status, _, _ := request(t, s, "POST", "/api/billing/orders/"+orderID+"/sync", token, nil); status != 400 {
		t.Fatal("mock fabricated payment")
	}
	o, err := s.Store.BillingOrder(context.Background(), orderID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	mock.Set(billing.Payment{OrderID: o.ID, Adapter: o.Adapter, AccountHash: o.AccountHash, TradeNo: "explicit-mock-payment", AmountCents: o.AmountCents, Currency: o.Currency})
	result := billingHTTPData(t, s, "POST", "/api/billing/orders/"+orderID+"/sync", token, nil)
	if result["status"] != "paid" || result["balance_after"] != float64(40) {
		t.Fatal("mock payment not credited")
	}
	result = billingHTTPData(t, s, "PATCH", "/api/admin/billing/orders/"+orderID+"/confirm", admin, nil)
	if result["idempotent"] != true {
		t.Fatal("manual retry not idempotent")
	}
	second := billingHTTPOrder(t, s, token, "mock")
	billingHTTPData(t, s, "PATCH", "/api/admin/billing/orders/"+second+"/confirm", admin, nil)
	third := billingHTTPOrder(t, s, token, "mock")
	if result = billingHTTPData(t, s, "POST", "/api/billing/orders/"+third+"/cancel", token, nil); result["status"] != "failed" {
		t.Fatal("user cancellation shape")
	}
	fourth := billingHTTPOrder(t, s, token, "mock")
	if result = billingHTTPData(t, s, "PATCH", "/api/admin/billing/orders/"+fourth+"/cancel", admin, nil); result["status"] != "cancelled" {
		t.Fatal("admin cancellation shape")
	}
	summary := billingHTTPData(t, s, "GET", "/api/billing/summary", token, nil)
	if summary["credits"] != float64(80) || summary["pending_orders"] != float64(0) {
		t.Fatal("summary shape")
	}
	page := billingHTTPData(t, s, "GET", "/api/billing/ledger?page=2&page_size=1", token, nil)
	if page["total"] != float64(2) || page["total_pages"] != float64(2) || len(page["rows"].([]any)) != 1 {
		t.Fatal("ledger page shape")
	}
	page = billingHTTPData(t, s, "GET", "/api/admin/billing/orders?keyword=member%40example.invalid&status=paid&page_size=1", admin, nil)
	if page["total"] != float64(2) || len(page["rows"].([]any)) != 1 {
		t.Fatal("admin filter shape")
	}
	row := page["rows"].([]any)[0].(map[string]any)
	if row["user_email_masked"] == "member@example.invalid" || row["user_nickname"] != "Member" {
		t.Fatal("admin identity shape")
	}
	untouched, _ := s.Store.UserByID(context.Background(), owner.ID)
	if untouched.Credits != 0 {
		t.Fatal("credits applied to admin")
	}
	if status, _, _ := request(t, s, "GET", "/api/admin/billing/orders?from=invalid", admin, nil); status != 400 {
		t.Fatal("invalid date accepted")
	}
}

func TestPostgresBillingHTTPEncryptedRedactedChannelsAndSnapshots(t *testing.T) {
	s, _, admin := billingHTTPServer(t, config.Platform, &billing.Mock{})
	cfg := billing.ChannelConfig{BaseURL: "https://payment.example.invalid", PID: "fixture-merchant", Key: "fixture-secret-only", Type: "alipay", NotifyURL: "https://app.example.invalid/api/billing/ykmcn/notify", ReturnURLTemplate: "https://app.example.invalid/credits?order_id={order_id}"}
	billingHTTPCatalog(t, s, admin, "ykmcn", cfg)
	status, result, body := request(t, s, "GET", "/api/admin/billing/channels", admin, nil)
	if status != 200 || strings.Contains(body, cfg.Key) || strings.Contains(body, cfg.PID) || strings.Contains(body, "ciphertext") {
		t.Fatal("channel credentials leaked")
	}
	c, err := s.Store.BillingChannel(context.Background(), "ykmcn", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ciphertext == "" || strings.Contains(c.Ciphertext, cfg.Key) {
		t.Fatal("plaintext stored")
	}
	raw, err := s.Vault.Decrypt("billing/channel/ykmcn", c.Ciphertext)
	if err != nil || !strings.Contains(raw, cfg.Key) {
		t.Fatal("encrypted configuration missing")
	}
	if _, err = s.Vault.Decrypt("billing/channel/other", c.Ciphertext); err == nil {
		t.Fatal("ciphertext not channel-bound")
	}
	redacted := result["data"].([]any)[0].(map[string]any)["config"].(string)
	billingHTTPData(t, s, "POST", "/api/admin/billing/channels/ykmcn", admin, map[string]any{"config": redacted, "name": "Updated"})
	after, _ := s.Store.BillingChannel(context.Background(), "ykmcn", false)
	raw, _ = s.Vault.Decrypt("billing/channel/ykmcn", after.Ciphertext)
	if !strings.Contains(raw, cfg.Key) {
		t.Fatal("redacted save destroyed key")
	}
	_, _, body = request(t, s, "GET", "/api/billing/channels", admin, nil)
	if strings.Contains(body, "base_url") || strings.Contains(body, "pid") {
		t.Fatal("public channel configuration exposed")
	}
	orderID := billingHTTPOrder(t, s, admin, "ykmcn")
	cfg.Key = "rotated-fixture-key"
	updated, _ := json.Marshal(cfg)
	billingHTTPData(t, s, "POST", "/api/admin/billing/channels/ykmcn", admin, map[string]any{"config": string(updated)})
	o, _ := s.Store.BillingOrder(context.Background(), orderID, "")
	raw, _ = s.Vault.Decrypt("billing/channel/ykmcn", o.Ciphertext)
	if !strings.Contains(raw, "fixture-secret-only") || strings.Contains(raw, "rotated-fixture-key") {
		t.Fatal("order credential snapshot changed")
	}
	if status, _, _ := request(t, s, "POST", "/api/admin/billing/channels/ykmcn", admin, map[string]any{"version": 1, "name": "stale"}); status != 409 {
		t.Fatal("stale config update accepted")
	}
	billingHTTPData(t, s, "POST", "/api/admin/billing/channels/ykmcn", admin, map[string]any{"enabled": false})
	result = billingHTTPData(t, s, "POST", "/api/billing/orders", admin, map[string]any{"plan_id": "test-plan", "channel_code": "ykmcn"})
	if result["status"] != "channel_disabled" || result["payment_url"] != nil {
		t.Fatal("disabled channel payment link")
	}
}

func TestPostgresBillingHTTPYkmcnWebhookExactlyOnce(t *testing.T) {
	s, u, token := billingHTTPServer(t, config.Platform, &billing.Mock{})
	cfg := billing.ChannelConfig{BaseURL: "https://payments.example.invalid", PID: "fixture-merchant", Key: "fixture-payment-key", Type: "alipay", NotifyURL: "https://app.example.invalid/notify", ReturnURLTemplate: "https://app.example.invalid/credits"}
	billingHTTPCatalog(t, s, token, "ykmcn", cfg)
	id := billingHTTPOrder(t, s, token, "ykmcn")
	values := url.Values{"out_trade_no": {id}, "pid": {cfg.PID}, "trade_no": {"one-http-trade"}, "money": {"12.50"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}, "sign_type": {"MD5"}}
	send := func(params url.Values, post bool) (int, string) {
		path := "/api/billing/ykmcn/notify"
		method := "GET"
		var body io.Reader
		if post {
			method = "POST"
			body = strings.NewReader(params.Encode())
		} else {
			path += "?" + params.Encode()
		}
		r := httptest.NewRequest(method, path, body)
		if post {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	for _, mutate := range []func(url.Values){func(v url.Values) { v.Set("money", "12.49") }, func(v url.Values) { v.Set("pid", "wrong") }, func(v url.Values) { v.Set("trade_no", "") }, func(v url.Values) { v.Set("trade_status", "pending") }, func(v url.Values) { v.Add("money", "12.50") }} {
		v, _ := url.ParseQuery(values.Encode())
		mutate(v)
		v.Set("sign", billing.YkmcnSign(v, cfg.Key))
		if status, body := send(v, true); status != 400 || body != "fail" {
			t.Fatal("invalid webhook accepted")
		}
	}
	// A channel-key rotation cannot invalidate a previously issued order.
	next := cfg
	next.Key = "rotated-fixture"
	raw, _ := json.Marshal(next)
	billingHTTPData(t, s, "POST", "/api/admin/billing/channels/ykmcn", token, map[string]any{"config": string(raw)})
	values.Set("sign", billing.YkmcnSign(values, cfg.Key))
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if status, body := send(values, i%2 == 0); status != 200 || body != "success" {
				t.Errorf("valid webhook rejected: %d", status)
			}
		}()
	}
	wg.Wait()
	actual, _ := s.Store.UserByID(context.Background(), u.ID)
	if actual.Credits != 40 {
		t.Fatal("duplicate webhook credit")
	}
	var count int
	s.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM credit_ledger WHERE biz_type='recharge'").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate webhook ledger")
	}
	other := billingHTTPOrder(t, s, token, "ykmcn")
	values.Set("out_trade_no", other)
	values.Set("sign", billing.YkmcnSign(values, next.Key))
	if status, _ := send(values, true); status != 400 {
		t.Fatal("external trade reused for another order")
	}
}

func TestPostgresBillingHTTPAfdianAuthenticatesAndRequeries(t *testing.T) {
	var queryCount atomic.Int32
	var amount atomic.Int64
	amount.Store(1249)
	var providerOrderID atomic.Value
	providerOrderID.Store("")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"ec": 200, "data": map[string]any{"total_page": 1, "list": []any{map[string]any{"out_trade_no": "afdian-fixture-trade", "custom_order_id": providerOrderID.Load(), "total_amount": billing.Money(amount.Load()), "status": 2}}}})
	}))
	defer provider.Close()
	s, u, token := billingHTTPServer(t, config.Platform, billing.NewClient())
	cfg := billing.ChannelConfig{BaseURL: provider.URL, UserID: "fixture-afdian-merchant", APIToken: "fixture-api-token", WebhookSecret: "fixture-hook", PaymentURL: provider.URL}
	billingHTTPCatalog(t, s, token, "afdian", cfg)
	providerOrderID.Store(billingHTTPOrder(t, s, token, "afdian"))
	body := []byte(`{"data":{"order":{"out_trade_no":"afdian-fixture-trade","custom_order_id":"attacker-order","total_amount":"9999999"}}}`)
	send := func(secret string, timestamp int64) (int, string) {
		r := httptest.NewRequest("POST", "/api/billing/afdian/webhook", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		ts := strconv.FormatInt(timestamp, 10)
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(ts + "."))
		m.Write(body)
		r.Header.Set("X-Openimage-Timestamp", ts)
		r.Header.Set("X-Openimage-Signature", hex.EncodeToString(m.Sum(nil)))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if status, _ := send("wrong", time.Now().Unix()); status != 401 || queryCount.Load() != 0 {
		t.Fatal("unauthenticated provider query")
	}
	if status, _ := send(cfg.WebhookSecret, time.Now().Add(-time.Hour).Unix()); status != 401 || queryCount.Load() != 0 {
		t.Fatal("expired callback accepted")
	}
	if status, _ := send(cfg.WebhookSecret, time.Now().Unix()); status != 400 || queryCount.Load() != 1 {
		t.Fatal("trusted callback amount instead of provider")
	}
	actual, _ := s.Store.UserByID(context.Background(), u.ID)
	if actual.Credits != 0 {
		t.Fatal("underpayment credited")
	}
	amount.Store(1250)
	for range 2 {
		status, reply := send(cfg.WebhookSecret, time.Now().Unix())
		if status != 200 || !strings.Contains(reply, `"ec":200`) {
			t.Fatal("verified Afdian payment failed")
		}
	}
	actual, _ = s.Store.UserByID(context.Background(), u.ID)
	if actual.Credits != 40 {
		t.Fatal("Afdian replay credited twice")
	}
}

func TestPostgresBillingHTTPBenefitsAndQR(t *testing.T) {
	s, _, admin := billingHTTPServer(t, config.Platform, &billing.Mock{})
	_, member := billingUserToken(t, s, "redeemer@example.invalid")
	cfg := billingHTTPData(t, s, "GET", "/api/public/benefit-config", "", nil)
	if cfg["enabled"] != false || cfg["qq_group"] != "" {
		t.Fatal("real benefit account/default enabled seeded")
	}
	billingHTTPData(t, s, "POST", "/api/admin/benefits/config", admin, map[string]any{"enabled": true, "title": "Fixture benefit", "qq_link": "https://community.example.invalid"})
	if status, _, _ := request(t, s, "POST", "/api/admin/benefits/config", admin, map[string]any{"qq_link": "javascript:alert(1)"}); status != 400 {
		t.Fatal("unsafe benefit link")
	}
	wave := billingHTTPData(t, s, "POST", "/api/admin/benefits/card-waves", admin, map[string]any{"name": "Fixture wave", "prefix": "TEST", "valid_days": 2, "benefit_type": "Fixture", "note": "Isolated test", "tiers": []any{map[string]any{"credits": 11, "quantity": 2, "max_uses": 1}}})
	codes := wave["codes"].([]any)
	code := codes[0].(map[string]any)["code"].(string)
	redemption := billingHTTPData(t, s, "POST", "/api/billing/redeem", member, map[string]any{"code": strings.ToLower(code)})
	if redemption["credits"] != float64(11) || redemption["record_id"] == nil {
		t.Fatal("redemption wire shape")
	}
	otherCode := codes[1].(map[string]any)["code"].(string)
	if status, _, _ := request(t, s, "POST", "/api/billing/redeem", member, map[string]any{"code": otherCode}); status != 409 {
		t.Fatal("exclusive wave bypass")
	}
	page := billingHTTPData(t, s, "GET", "/api/admin/benefits/redemption-records", admin, nil)
	if page["total"] != float64(1) {
		t.Fatal("redemption records endpoint")
	}
	for _, path := range []string{"/api/admin/billing/redemption-codes", "/api/admin/benefits/redemption-codes"} {
		page = billingHTTPData(t, s, "GET", path, admin, nil)
		if page["total"] != float64(2) {
			t.Fatal("code listing alias")
		}
	}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	writer.WriteField("kind", "wechat")
	part, _ := writer.CreateFormFile("file", "qr.png")
	part.Write(img.Bytes())
	writer.Close()
	r := httptest.NewRequest("POST", "/api/admin/benefits/qr", &form)
	r.Header.Set("Authorization", "Bearer "+admin)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("QR upload failed", w.Code)
	}
	var reply struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.URL == "" {
		t.Fatal("QR result shape")
	}
	get := httptest.NewRequest("GET", reply.Data.URL, nil)
	imageResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(imageResponse, get)
	if imageResponse.Code != 200 || !strings.HasPrefix(imageResponse.Header().Get("Content-Type"), "image/") {
		t.Fatal("public QR cannot load")
	}
	billingHTTPData(t, s, "POST", "/api/admin/benefits/config", admin, map[string]any{"wechat_qr_url": ""})
	imageResponse = httptest.NewRecorder()
	s.Handler().ServeHTTP(imageResponse, get)
	if imageResponse.Code != 404 {
		t.Fatal("unreferenced asset remains publicly accessible")
	}
}

func TestPostgresBillingHTTPPersonalEveryRouteIs404(t *testing.T) {
	s, _, token := billingHTTPServer(t, config.Personal, &billing.Mock{})
	endpoints := []struct{ method, path string }{
		{"GET", "/api/billing/summary"}, {"GET", "/api/billing/plans"}, {"GET", "/api/billing/channels"}, {"GET", "/api/billing/orders"}, {"POST", "/api/billing/orders"}, {"POST", "/api/billing/orders/example/sync"}, {"POST", "/api/billing/orders/example/cancel"}, {"GET", "/api/billing/ledger"}, {"POST", "/api/billing/redeem"},
		{"GET", "/api/admin/billing/plans"}, {"POST", "/api/admin/billing/plans/example"}, {"GET", "/api/admin/billing/channels"}, {"POST", "/api/admin/billing/channels/example"}, {"GET", "/api/admin/billing/orders"}, {"PATCH", "/api/admin/billing/orders/example/confirm"}, {"PATCH", "/api/admin/billing/orders/example/cancel"}, {"GET", "/api/admin/billing/redemption-codes"}, {"POST", "/api/admin/billing/redemption-codes"},
		{"POST", "/api/admin/benefits/card-waves"}, {"GET", "/api/admin/benefits/redemption-codes"}, {"GET", "/api/admin/benefits/redemption-records"}, {"GET", "/api/admin/benefits/config"}, {"POST", "/api/admin/benefits/config"}, {"POST", "/api/admin/benefits/qr"}, {"GET", "/api/public/benefit-config"}, {"GET", "/api/public/benefits/qr/example"}, {"POST", "/api/billing/afdian/webhook"}, {"GET", "/api/billing/ykmcn/notify"}, {"POST", "/api/billing/ykmcn/notify"},
	}
	for _, e := range endpoints {
		for _, auth := range []string{"", token} {
			status, _, _ := request(t, s, e.method, e.path, auth, map[string]any{})
			if status != 404 {
				t.Errorf("%s %s returned %d", e.method, e.path, status)
			}
		}
	}
}

func TestPostgresBillingHTTPOrderIdempotencyAndCSRF(t *testing.T) {
	s, _, token := billingHTTPServer(t, config.Platform, &billing.Mock{})
	billingHTTPCatalog(t, s, token, "manual", billing.ChannelConfig{})
	// Login CAPTCHA does not add a billing CAPTCHA absent from the legacy UI.
	s.Config.TurnstileSecret = "fixture-auth-captcha-only"
	const body = `{"plan_id":"test-plan","channel_code":"manual"}`
	var id string
	for range 2 {
		r := httptest.NewRequest("POST", "/api/billing/orders", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "fixture-create-request")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("idempotent create", w.Code)
		}
		var reply struct {
			Data struct {
				ID string `json:"order_id"`
			} `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &reply)
		if id == "" {
			id = reply.Data.ID
		}
		if id != reply.Data.ID {
			t.Fatal("HTTP duplicate order")
		}
	}
	r := httptest.NewRequest("POST", "/api/billing/orders", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "picpilot_session", Value: token})
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("billing cookie CSRF")
	}
	if status, _, _ := request(t, s, "POST", "/api/billing/orders", token, map[string]any{"plan_id": "test-plan", "channel_code": "manual", "credits": 99999}); status != 400 {
		t.Fatal("client supplied credits accepted")
	}
	before := billingHTTPData(t, s, "GET", "/api/billing/summary", token, nil)
	if fmt.Sprint(before["pending_orders"]) != "1" {
		t.Fatal("idempotency created extra order")
	}
}
