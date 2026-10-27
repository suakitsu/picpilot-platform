package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" // Required by the legacy payment-provider wire protocols.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var moneyPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)
var orderPattern = regexp.MustCompile(`ro_[0-9a-fA-F]{32}`)

// ParseMoney rejects rounding, exponent notation, signs and overflowing amounts.
func ParseMoney(value string) (int64, error) {
	if len(value) > 24 || !moneyPattern.MatchString(value) {
		return 0, ErrInvalid
	}
	whole, fraction, _ := strings.Cut(value, ".")
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || units > math.MaxInt64/100 {
		return 0, ErrInvalid
	}
	if len(fraction) == 1 {
		fraction += "0"
	}
	if fraction == "" {
		fraction = "0"
	}
	cents, _ := strconv.ParseInt(fraction, 10, 64)
	if units*100 > math.MaxInt64-cents {
		return 0, ErrInvalid
	}
	return units*100 + cents, nil
}

func Money(cents int64) string {
	return strconv.FormatInt(cents/100, 10) + "." + strconv.FormatInt(100+cents%100, 10)[1:]
}

func YkmcnSign(params url.Values, key string) string {
	keys := make([]string, 0, len(params))
	for k, values := range params {
		if k != "sign" && k != "sign_type" && len(values) == 1 && strings.TrimSpace(values[0]) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params.Get(k))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(sum[:])
}

func VerifyYkmcn(params url.Values, c ChannelConfig, o Order) (Payment, error) {
	if c.Ready("ykmcn") != nil || o.Adapter != "ykmcn" || o.Currency != "CNY" {
		return Payment{}, ErrUnconfigured
	}
	for k, values := range params {
		if len(values) != 1 || len(k) > 128 || len(values[0]) > 8192 {
			return Payment{}, ErrSignature
		}
	}
	actual, err := hex.DecodeString(params.Get("sign"))
	expected, _ := hex.DecodeString(YkmcnSign(params, c.Key))
	if err != nil || !hmac.Equal(expected, actual) || (params.Get("sign_type") != "" && !strings.EqualFold(params.Get("sign_type"), "MD5")) {
		return Payment{}, ErrSignature
	}
	if params.Get("pid") != c.PID || params.Get("out_trade_no") != o.ID || params.Get("trade_status") != "TRADE_SUCCESS" {
		return Payment{}, ErrInvalid
	}
	if currency := params.Get("currency"); currency != "" && currency != o.Currency {
		return Payment{}, ErrInvalid
	}
	if params.Get("type") != c.PaymentType(o.ChannelCode) {
		return Payment{}, ErrInvalid
	}
	amount, err := ParseMoney(params.Get("money"))
	if err != nil {
		return Payment{}, err
	}
	p := Payment{OrderID: o.ID, Adapter: "ykmcn", AccountHash: c.AccountHash("ykmcn"), TradeNo: params.Get("trade_no"), AmountCents: amount, Currency: "CNY"}
	return p, ValidatePayment(o, p)
}

func VerifyAfdian(body []byte, headers http.Header, secretParam string, c ChannelConfig, now time.Time) error {
	if c.WebhookSecret == "" {
		return ErrUnconfigured
	}
	token := headers.Get("X-Openimage-Webhook-Secret")
	if token == "" {
		token = secretParam
	}
	if token != "" && hmac.Equal([]byte(token), []byte(c.WebhookSecret)) {
		return nil
	}
	timestamp := headers.Get("X-Openimage-Timestamp")
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	skew := c.WebhookMaxSkewSeconds
	if skew == 0 {
		skew = 300
	}
	if err != nil || ts < now.Unix()-int64(skew) || ts > now.Unix()+int64(skew) {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	actual, err := hex.DecodeString(strings.TrimPrefix(headers.Get("X-Openimage-Signature"), "sha256="))
	if err != nil || !hmac.Equal(mac.Sum(nil), actual) {
		return ErrSignature
	}
	return nil
}

func AfdianTrade(body []byte) (string, error) {
	var payload struct {
		Data struct {
			Order struct {
				Trade string `json:"out_trade_no"`
			} `json:"order"`
		} `json:"data"`
		Order struct {
			Trade string `json:"out_trade_no"`
		} `json:"order"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", ErrInvalid
	}
	trade := payload.Data.Order.Trade
	if trade == "" {
		trade = payload.Order.Trade
	}
	if trade == "" || len(trade) > 200 {
		return "", ErrInvalid
	}
	return trade, nil
}

func ValidatePayment(o Order, p Payment) error {
	if p.TradeNo == "" || len(p.TradeNo) > 200 || strings.TrimSpace(p.TradeNo) != p.TradeNo || p.OrderID != o.ID || p.Adapter != o.Adapter || p.AccountHash != o.AccountHash || p.Currency != o.Currency || p.AmountCents < o.AmountCents || p.AmountCents < 0 {
		return ErrInvalid
	}
	return nil
}

type Gateway interface {
	Query(context.Context, ChannelConfig, Order, string) (Payment, error)
}

type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) request(ctx context.Context, cfg ChannelConfig, method, rawURL string, body []byte, dst any) error {
	if !ValidURL(rawURL) {
		return ErrUnconfigured
	}
	timeout := cfg.TimeoutSeconds
	if timeout == 0 {
		timeout = 15
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return ErrUpstream
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = NewClient().HTTP
	}
	// Also disable redirects on injected clients, so credentials never follow one.
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := safe.Do(req)
	if err != nil {
		return ErrUpstream
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUpstream
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, dst) != nil {
		return ErrUpstream
	}
	return nil
}

type scalar string

func (s *scalar) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = scalar(v)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = scalar(n.String())
	return nil
}

func (c *Client) Query(ctx context.Context, cfg ChannelConfig, o Order, trade string) (Payment, error) {
	if err := cfg.Ready(o.Adapter); err != nil {
		return Payment{}, err
	}
	switch o.Adapter {
	case "ykmcn":
		return c.queryYkmcn(ctx, cfg, o)
	case "afdian":
		return c.queryAfdian(ctx, cfg, o, trade)
	default:
		return Payment{}, ErrUnpaid
	}
}

func (c *Client) queryYkmcn(ctx context.Context, cfg ChannelConfig, o Order) (Payment, error) {
	u, _ := url.Parse(strings.TrimRight(cfg.BaseURL, "/") + "/api.php")
	q := url.Values{"act": {"order"}, "pid": {cfg.PID}, "key": {cfg.Key}, "out_trade_no": {o.ID}}
	u.RawQuery = q.Encode()
	var response struct {
		Code     scalar `json:"code"`
		Status   scalar `json:"status"`
		Trade    string `json:"trade_no"`
		OrderID  string `json:"out_trade_no"`
		Money    scalar `json:"money"`
		PID      scalar `json:"pid"`
		Currency string `json:"currency"`
	}
	if err := c.request(ctx, cfg, "GET", u.String(), nil, &response); err != nil {
		return Payment{}, err
	}
	if response.Code != "1" || response.Status != "1" {
		return Payment{}, ErrUnpaid
	}
	if response.PID != "" && string(response.PID) != cfg.PID {
		return Payment{}, ErrInvalid
	}
	amount, err := ParseMoney(string(response.Money))
	if err != nil {
		return Payment{}, err
	}
	currency := response.Currency
	if currency == "" {
		currency = "CNY"
	}
	p := Payment{OrderID: response.OrderID, Adapter: "ykmcn", AccountHash: cfg.AccountHash("ykmcn"), TradeNo: response.Trade, AmountCents: amount, Currency: currency}
	return p, ValidatePayment(o, p)
}

func (c *Client) queryAfdian(ctx context.Context, cfg ChannelConfig, o Order, trade string) (Payment, error) {
	for page := 1; page <= 5; page++ {
		params := map[string]any{"page": page}
		if trade != "" {
			params = map[string]any{"out_trade_no": trade}
		}
		raw, _ := json.Marshal(params)
		ts := time.Now().Unix()
		hash := md5.Sum([]byte(cfg.APIToken + "params" + string(raw) + "ts" + strconv.FormatInt(ts, 10) + "user_id" + cfg.UserID))
		body, _ := json.Marshal(map[string]any{"user_id": cfg.UserID, "params": string(raw), "ts": ts, "sign": hex.EncodeToString(hash[:])})
		var response struct {
			EC   scalar `json:"ec"`
			Data struct {
				TotalPage scalar `json:"total_page"`
				List      []struct {
					Trade    string `json:"out_trade_no"`
					Custom   string `json:"custom_order_id"`
					Remark   string `json:"remark"`
					Amount   scalar `json:"total_amount"`
					Status   scalar `json:"status"`
					Currency string `json:"currency"`
				} `json:"list"`
			} `json:"data"`
		}
		if err := c.request(ctx, cfg, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/api/open/query-order", body, &response); err != nil {
			return Payment{}, err
		}
		if response.EC != "200" {
			return Payment{}, ErrUpstream
		}
		for _, row := range response.Data.List {
			if row.Status != "2" || (trade != "" && row.Trade != trade) {
				continue
			}
			id := orderPattern.FindString(row.Custom)
			if id == "" {
				id = orderPattern.FindString(row.Remark)
			}
			if id == "" || (o.ID != "" && id != o.ID) {
				continue
			}
			amount, err := ParseMoney(string(row.Amount))
			if err != nil {
				return Payment{}, err
			}
			currency := row.Currency
			if currency == "" {
				currency = "CNY"
			}
			p := Payment{OrderID: id, Adapter: "afdian", AccountHash: cfg.AccountHash("afdian"), TradeNo: row.Trade, AmountCents: amount, Currency: currency}
			// Webhook lookup has only the provider trade number until this response.
			if o.ID == "" {
				if p.TradeNo == "" {
					return Payment{}, ErrInvalid
				}
				return p, nil
			}
			return p, ValidatePayment(o, p)
		}
		total, _ := strconv.Atoi(string(response.Data.TotalPage))
		if trade != "" || page >= total {
			break
		}
	}
	return Payment{}, ErrUnpaid
}

// Mock never performs network calls or invents a successful payment.
type Mock struct {
	mu       sync.RWMutex
	payments map[string]Payment
}

func (m *Mock) Set(p Payment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.payments == nil {
		m.payments = map[string]Payment{}
	}
	m.payments[p.OrderID] = p
}
func (m *Mock) Query(ctx context.Context, _ ChannelConfig, o Order, trade string) (Payment, error) {
	if err := ctx.Err(); err != nil {
		return Payment{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if o.ID == "" {
		for _, p := range m.payments {
			if p.TradeNo == trade {
				return p, nil
			}
		}
		return Payment{}, ErrUnpaid
	}
	p, ok := m.payments[o.ID]
	if !ok || (trade != "" && p.TradeNo != trade) {
		return Payment{}, ErrUnpaid
	}
	return p, ValidatePayment(o, p)
}
