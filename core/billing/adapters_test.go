package billing

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMoneyExact(t *testing.T) {
	for text, want := range map[string]int64{"0": 0, "0.01": 1, "1.5": 150, "001.20": 120, "92233720368547758.07": math.MaxInt64} {
		got, err := ParseMoney(text)
		if err != nil || got != want {
			t.Fatalf("money %q: %d %v", text, got, err)
		}
	}
	for _, text := range []string{"", "NaN", "1e2", "-1", "+1", ".01", "1.", "1.001", " 1.00", "92233720368547758.08", "9223372036854775807", "1,000"} {
		if _, err := ParseMoney(text); err == nil {
			t.Fatalf("accepted invalid money %q", text)
		}
	}
	for _, v := range []int64{0, 1, 100, 115, math.MaxInt64} {
		parsed, err := ParseMoney(Money(v))
		if err != nil || parsed != v {
			t.Fatal("money round trip")
		}
	}
}

func ykmcnFixture() (ChannelConfig, Order, url.Values) {
	c := ChannelConfig{BaseURL: "https://payments.example.invalid", PID: "fixture-merchant", Key: "fixture-key-only", NotifyURL: "https://app.example.invalid/api/billing/ykmcn/notify", ReturnURLTemplate: "https://app.example.invalid/credits", Type: "alipay"}
	o := Order{ID: "ro_0123456789abcdef0123456789abcdef", Adapter: "ykmcn", AccountHash: c.AccountHash("ykmcn"), AmountCents: 1250, Currency: "CNY"}
	p := url.Values{"out_trade_no": {o.ID}, "trade_no": {"trade-fixture"}, "pid": {c.PID}, "money": {"12.50"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}, "sign_type": {"MD5"}}
	p.Set("sign", YkmcnSign(p, c.Key))
	return c, o, p
}

func TestYkmcnSignatureAmountIdentityAndDuplicateFields(t *testing.T) {
	c, o, p := ykmcnFixture()
	if _, err := VerifyYkmcn(p, c, o); err != nil {
		t.Fatal(err)
	}
	sample := url.Values{"b": {"two"}, "a": {"one"}, "empty": {""}, "sign": {"ignored"}, "sign_type": {"MD5"}}
	sum := md5.Sum([]byte("a=one&b=twofixture"))
	if YkmcnSign(sample, "fixture") != hex.EncodeToString(sum[:]) {
		t.Fatal("signature canonicalization")
	}
	for _, change := range []func(url.Values){
		func(v url.Values) { v.Set("money", "12.49") }, func(v url.Values) { v.Set("money", "12.499") }, func(v url.Values) { v.Set("out_trade_no", "another-order") }, func(v url.Values) { v.Set("pid", "another-merchant") }, func(v url.Values) { v.Set("trade_status", "WAIT_BUYER_PAY") }, func(v url.Values) { v.Set("trade_no", "") }, func(v url.Values) { v.Set("currency", "USD") }, func(v url.Values) { v.Set("type", "wxpay") }, func(v url.Values) { v.Add("money", "12.50") },
	} {
		v, _ := url.ParseQuery(p.Encode())
		change(v)
		v.Set("sign", YkmcnSign(v, c.Key))
		if _, err := VerifyYkmcn(v, c, o); err == nil {
			t.Fatal("accepted invalid signed notification")
		}
	}
	p.Set("money", "100.00")
	if _, err := VerifyYkmcn(p, c, o); !errors.Is(err, ErrSignature) {
		t.Fatal("tampered signature accepted")
	}
}

func TestAfdianAuthentication(t *testing.T) {
	c := ChannelConfig{WebhookSecret: "fixture-hook-secret"}
	body := []byte(`{"data":{"order":{"out_trade_no":"trade"}}}`)
	now := time.Unix(2000000000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	sign := func(ts string) string {
		m := hmac.New(sha256.New, []byte(c.WebhookSecret))
		m.Write([]byte(ts + "."))
		m.Write(body)
		return "sha256=" + hex.EncodeToString(m.Sum(nil))
	}
	h := http.Header{"X-Openimage-Timestamp": {ts}, "X-Openimage-Signature": {sign(ts)}}
	if err := VerifyAfdian(body, h, "", c, now); err != nil {
		t.Fatal(err)
	}
	if VerifyAfdian(append(body, ' '), h, "", c, now) == nil {
		t.Fatal("body mutation accepted")
	}
	for _, timestamp := range []string{"invalid", "-9223372036854775808", "9223372036854775807", strconv.FormatInt(now.Unix()-301, 10)} {
		h.Set("X-Openimage-Timestamp", timestamp)
		h.Set("X-Openimage-Signature", sign(timestamp))
		if VerifyAfdian(body, h, "", c, now) == nil {
			t.Fatal("invalid timestamp accepted")
		}
	}
	if VerifyAfdian(body, http.Header{}, c.WebhookSecret, c, now) != nil {
		t.Fatal("legacy shared token rejected")
	}
	if VerifyAfdian(body, http.Header{}, "", ChannelConfig{}, now) == nil {
		t.Fatal("unconfigured accepted")
	}
	if got, err := AfdianTrade(body); err != nil || got != "trade" {
		t.Fatal("trade parser")
	}
}

func TestClientQueriesOnlyLocalMockProviders(t *testing.T) {
	const id = "ro_0123456789abcdef0123456789abcdef"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api.php":
			if r.URL.Query().Get("key") != "fixture-key-only" || r.URL.Query().Get("out_trade_no") != id {
				t.Error("query credentials/order absent")
			}
			io.WriteString(w, `{"code":1,"status":"1","trade_no":"trade-y","out_trade_no":"`+id+`","money":"12.50","pid":"fixture-merchant"}`)
		case "/api/open/query-order":
			var req struct {
				UserID string `json:"user_id"`
				Params string `json:"params"`
				TS     int64  `json:"ts"`
				Sign   string `json:"sign"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				t.Error("invalid query body")
			}
			sum := md5.Sum([]byte("fixture-api-token" + "params" + req.Params + "ts" + strconv.FormatInt(req.TS, 10) + "user_idfixture-user"))
			if req.Sign != hex.EncodeToString(sum[:]) || req.UserID != "fixture-user" {
				t.Error("Afdian request signing")
			}
			io.WriteString(w, `{"ec":200,"data":{"total_page":1,"list":[{"out_trade_no":"trade-a","custom_order_id":"`+id+`","total_amount":"12.50","status":2}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, o, _ := ykmcnFixture()
	c.BaseURL = server.URL
	o.AccountHash = c.AccountHash("ykmcn")
	client := NewClient()
	if p, err := client.Query(context.Background(), c, o, ""); err != nil || p.TradeNo != "trade-y" {
		t.Fatalf("Ykmcn query: %v", err)
	}
	c.UserID = "fixture-user"
	c.APIToken = "fixture-api-token"
	c.WebhookSecret = "fixture-webhook"
	c.PaymentURL = server.URL
	o.Adapter = "afdian"
	o.AccountHash = c.AccountHash("afdian")
	if p, err := client.Query(context.Background(), c, o, "trade-a"); err != nil || p.TradeNo != "trade-a" {
		t.Fatalf("Afdian query: %v", err)
	}
	if _, err := client.Query(context.Background(), c, o, "wrong-trade"); !errors.Is(err, ErrUnpaid) {
		t.Fatal("provider returned other trade accepted")
	}
	if p, err := client.Query(context.Background(), c, Order{Adapter: "afdian"}, "trade-a"); err != nil || p.OrderID != id {
		t.Fatal("webhook authoritative lookup failed")
	}
	if requests.Load() != 4 {
		t.Fatal("unexpected mock query count")
	}
}

func TestClientRejectsRedirectOversizeAndMalformedResponses(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	for _, mode := range []string{"redirect", "large", "invalid", "failed"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, 302)
				case "large":
					io.WriteString(w, strings.Repeat(" ", 1<<20)+"{}")
				case "invalid":
					io.WriteString(w, "not json")
				case "failed":
					w.WriteHeader(500)
				}
			}))
			defer srv.Close()
			c, o, _ := ykmcnFixture()
			c.BaseURL = srv.URL
			o.AccountHash = c.AccountHash("ykmcn")
			if _, err := NewClient().Query(context.Background(), c, o, ""); !errors.Is(err, ErrUpstream) {
				t.Fatal("bad response accepted")
			}
		})
	}
	if reached.Load() != 0 {
		t.Fatal("credential redirect followed")
	}
}

func TestChannelConfigRedactionCheckoutAndMock(t *testing.T) {
	c, o, _ := ykmcnFixture()
	c.UserID = "fixture-user"
	c.APIToken = "fixture-token"
	c.WebhookSecret = "fixture-hook"
	raw := c.RedactedJSON()
	for _, secret := range []string{c.Key, c.PID, c.UserID, c.APIToken, c.WebhookSecret} {
		if strings.Contains(raw, secret) {
			t.Fatal("credential output leak")
		}
	}
	next, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	next.PreserveCredentials(c)
	if next.Key != c.Key || next.PID != c.PID {
		t.Fatal("redaction round trip erased credentials")
	}
	target, _, _, err := c.Checkout(o)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("key") != "" || q.Get("sign") != YkmcnSign(q, c.Key) || q.Get("money") != "12.50" {
		t.Fatal("invalid signed checkout")
	}
	disabled := o
	disabled.Status = "channel_disabled"
	if target, links, _, _ := c.Checkout(disabled); target != "" || len(links) != 0 {
		t.Fatal("disabled checkout offered")
	}
	for _, status := range []string{"paid", "failed", "cancelled"} {
		closed := o
		closed.Status = status
		if target, links, _, _ := c.Checkout(closed); target != "" || len(links) != 0 {
			t.Fatal("terminal checkout offered")
		}
	}
	if _, err := ParseConfig(`{"unknown_secret":"value"}`); err == nil {
		t.Fatal("unknown field accepted")
	}
	for _, url := range []string{"javascript:alert(1)", "https://user:pass@example.invalid", "http://example.invalid", "//example.invalid"} {
		if ValidURL(url) {
			t.Fatal("unsafe URL accepted")
		}
	}
	mock := &Mock{}
	if _, err := mock.Query(context.Background(), c, o, ""); !errors.Is(err, ErrUnpaid) {
		t.Fatal("mock invented payment")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mock.Set(Payment{OrderID: o.ID, Adapter: o.Adapter, AccountHash: o.AccountHash, TradeNo: "fixture", AmountCents: o.AmountCents, Currency: o.Currency})
			if _, err := mock.Query(context.Background(), c, o, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
