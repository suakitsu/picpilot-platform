package billing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const Redacted = "[REDACTED]"

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// The complete configuration, including merchant identity, is encrypted at rest.
type ChannelConfig struct {
	Mode                  string    `json:"mode,omitempty"`
	BaseURL               string    `json:"base_url,omitempty"`
	PID                   string    `json:"pid,omitempty"`
	Key                   string    `json:"key,omitempty"`
	Type                  string    `json:"type,omitempty"`
	UserID                string    `json:"user_id,omitempty"`
	APIToken              string    `json:"api_token,omitempty"`
	WebhookSecret         string    `json:"webhook_secret,omitempty"`
	WebhookMaxSkewSeconds int       `json:"webhook_max_skew_seconds,omitempty"`
	NotifyURL             string    `json:"notify_url,omitempty"`
	ReturnURLTemplate     string    `json:"return_url_template,omitempty"`
	NameTemplate          string    `json:"name_template,omitempty"`
	ParamTemplate         string    `json:"param_template,omitempty"`
	Label                 string    `json:"label,omitempty"`
	Instructions          string    `json:"instructions,omitempty"`
	TimeoutSeconds        int       `json:"timeout_seconds,omitempty"`
	PaymentWindowMinutes  int       `json:"payment_window_minutes,omitempty"`
	CheckoutURL           string    `json:"checkout_url,omitempty"`
	PaymentURL            string    `json:"payment_url,omitempty"`
	PlanID                string    `json:"plan_id,omitempty"`
	ProductType           string    `json:"product_type,omitempty"`
	RemarkTemplate        string    `json:"remark_template,omitempty"`
	CustomOrderIDTemplate string    `json:"custom_order_id_template,omitempty"`
	SKU                   string    `json:"sku,omitempty"`
	ViokrzEx              string    `json:"viokrz_ex,omitempty"`
	Links                 []Link    `json:"links,omitempty"`
	AllowedPlanIDs        *[]string `json:"allowed_plan_ids,omitempty"`
}

func ParseConfig(value string) (ChannelConfig, error) {
	var c ChannelConfig
	if strings.TrimSpace(value) == "" {
		return c, nil
	}
	if len(value) > 32768 || !strings.HasPrefix(strings.TrimSpace(value), "{") {
		return c, ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, ErrInvalid
	}
	return c, c.Validate()
}

func (c ChannelConfig) Validate() error {
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.RawQuery != "" || u.Fragment != "" {
			return ErrInvalid
		}
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 60 || c.PaymentWindowMinutes < 0 || c.PaymentWindowMinutes > 1440 || c.WebhookMaxSkewSeconds < 0 || c.WebhookMaxSkewSeconds > 900 {
		return ErrInvalid
	}
	if len(c.Links) > 20 || len(c.Instructions) > 2000 {
		return ErrInvalid
	}
	for _, raw := range []string{c.BaseURL, c.NotifyURL, c.ReturnURLTemplate, c.CheckoutURL, c.PaymentURL} {
		if raw != "" && !ValidURL(strings.ReplaceAll(raw, "{order_id}", "order")) {
			return ErrInvalid
		}
	}
	for _, link := range c.Links {
		if !ValidURL(link.URL) || len(link.Label) > 120 {
			return ErrInvalid
		}
	}
	for _, secret := range []string{c.PID, c.Key, c.UserID, c.APIToken, c.WebhookSecret} {
		if len(secret) > 4096 {
			return ErrInvalid
		}
	}
	return nil
}

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}

func (c ChannelConfig) Adapter(code string) string {
	if c.Mode == "ykmcn_legacy" || code == "ykmcn" || code == "ykmcn_alipay" || code == "ykmcn_wxpay" {
		return "ykmcn"
	}
	if code == "afdian" {
		return "afdian"
	}
	if code == "mock" {
		return "mock"
	}
	return "manual"
}

func (c ChannelConfig) AccountHash(adapter string) string {
	identity := c.PID
	if adapter == "afdian" {
		identity = c.UserID
	}
	u, _ := url.Parse(c.BaseURL)
	// Scheme/path/channel aliases do not permit reusing one merchant transaction.
	host := ""
	if u != nil {
		host = strings.ToLower(u.Host)
	}
	hash := sha256.Sum256([]byte(adapter + "\x00" + host + "\x00" + identity))
	return hex.EncodeToString(hash[:])
}

func (c ChannelConfig) Ready(adapter string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	switch adapter {
	case "ykmcn":
		if c.BaseURL == "" || c.PID == "" || c.Key == "" || c.NotifyURL == "" || c.ReturnURLTemplate == "" {
			return ErrUnconfigured
		}
	case "afdian":
		if c.BaseURL == "" || c.UserID == "" || c.APIToken == "" || c.WebhookSecret == "" || (c.CheckoutURL == "" && c.PaymentURL == "" && len(c.Links) == 0) {
			return ErrUnconfigured
		}
	}
	return nil
}

func (c ChannelConfig) Window(adapter string) int {
	if c.PaymentWindowMinutes > 0 {
		return c.PaymentWindowMinutes
	}
	if adapter == "afdian" {
		return 10
	}
	return 30
}

func (c ChannelConfig) PaymentType(channelCode string) string {
	if c.Type != "" {
		return c.Type
	}
	if channelCode == "ykmcn_wxpay" {
		return "wxpay"
	}
	return "alipay"
}

func (c ChannelConfig) Allows(planID string) bool {
	if c.AllowedPlanIDs == nil {
		return true
	}
	for _, id := range *c.AllowedPlanIDs {
		if id == planID {
			return true
		}
	}
	return false
}

func (c ChannelConfig) RedactedJSON() string {
	for _, p := range []*string{&c.PID, &c.Key, &c.UserID, &c.APIToken, &c.WebhookSecret} {
		if *p != "" {
			*p = Redacted
		}
	}
	data, _ := json.Marshal(c)
	return string(data)
}

func (c *ChannelConfig) PreserveCredentials(old ChannelConfig) {
	pairs := [][2]*string{{&c.PID, &old.PID}, {&c.Key, &old.Key}, {&c.UserID, &old.UserID}, {&c.APIToken, &old.APIToken}, {&c.WebhookSecret, &old.WebhookSecret}}
	for _, pair := range pairs {
		if *pair[0] == "" || *pair[0] == Redacted {
			*pair[0] = *pair[1]
		}
	}
}

func (c ChannelConfig) Checkout(o Order) (string, []Link, string, error) {
	hint := strings.ReplaceAll(c.Instructions, "{order_id}", o.ID)
	if hint == "" {
		hint = "Awaiting payment or administrator confirmation"
	}
	if o.Status == "channel_disabled" {
		return "", []Link{}, "Payment channel is disabled", nil
	}
	if o.Status == "paid" || o.Status == "failed" || o.Status == "cancelled" {
		return "", []Link{}, "Order is " + o.Status, nil
	}
	replace := func(s string) string {
		return strings.NewReplacer("{order_id}", o.ID, "{credits}", strconv.FormatInt(o.Credits, 10)).Replace(s)
	}
	if o.Adapter == "ykmcn" {
		if err := c.Ready(o.Adapter); err != nil {
			return "", nil, "", err
		}
		paymentType := c.PaymentType(o.ChannelCode)
		name := c.NameTemplate
		if name == "" {
			name = "PicPilot {credits} credits"
		}
		param := c.ParamTemplate
		if param == "" {
			param = "{order_id}"
		}
		values := url.Values{"pid": {c.PID}, "type": {paymentType}, "out_trade_no": {o.ID}, "notify_url": {c.NotifyURL}, "return_url": {replace(c.ReturnURLTemplate)}, "name": {replace(name)}, "money": {Money(o.AmountCents)}, "param": {replace(param)}}
		values.Set("sign", YkmcnSign(values, c.Key))
		values.Set("sign_type", "MD5")
		u, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/submit.php")
		if err != nil {
			return "", nil, "", ErrInvalid
		}
		u.RawQuery = values.Encode()
		label := c.Label
		if label == "" {
			label = "Pay"
		}
		return u.String(), []Link{{Label: label, URL: u.String()}}, hint, nil
	}
	target := c.PaymentURL
	if c.CheckoutURL != "" && c.PlanID != "" {
		u, _ := url.Parse(c.CheckoutURL)
		q := u.Query()
		product := c.ProductType
		if product == "" {
			product = "0"
		}
		remark := c.RemarkTemplate
		if remark == "" {
			remark = "{order_id}"
		}
		custom := c.CustomOrderIDTemplate
		if custom == "" {
			custom = "{order_id}"
		}
		q.Set("product_type", product)
		q.Set("plan_id", c.PlanID)
		q.Set("remark", replace(remark))
		q.Set("custom_order_id", replace(custom))
		if c.ReturnURLTemplate != "" {
			q.Set("return_url", replace(c.ReturnURLTemplate))
		}
		if c.SKU != "" {
			q.Set("sku", c.SKU)
		}
		if c.ViokrzEx != "" {
			q.Set("viokrz_ex", c.ViokrzEx)
		}
		u.RawQuery = q.Encode()
		target = u.String()
	}
	links := append([]Link{}, c.Links...)
	if target != "" {
		label := c.Label
		if label == "" {
			label = "Pay"
		}
		links = []Link{{Label: label, URL: target}}
	} else if len(links) > 0 {
		target = links[0].URL
	}
	return target, links, hint, nil
}
