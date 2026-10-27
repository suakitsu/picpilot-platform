// Package billing contains payment protocol adapters and the legacy billing wire types.
package billing

import (
	"errors"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid billing request")
	ErrUnconfigured = errors.New("payment channel is not configured")
	ErrSignature    = errors.New("invalid payment signature")
	ErrUnpaid       = errors.New("no verified payment found")
	ErrUpstream     = errors.New("payment verification unavailable")
)

type Plan struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Credits    int64  `json:"credits"`
	PriceCents int64  `json:"price_cents"`
	Currency   string `json:"currency"`
	Enabled    bool   `json:"enabled"`
	SortOrder  int    `json:"sort_order"`
}

type Channel struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Visible    bool   `json:"visible"`
	Enabled    bool   `json:"enabled"`
	SortOrder  int    `json:"sort_order"`
	Config     string `json:"config"`
	Version    int64  `json:"version"`
	Ciphertext string `json:"-"`
}

type Order struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	UserEmailMasked string     `json:"user_email_masked,omitempty"`
	UserNickname    string     `json:"user_nickname,omitempty"`
	PlanID          string     `json:"plan_id"`
	ChannelID       string     `json:"-"`
	ChannelCode     string     `json:"channel_code"`
	ChannelVersion  int64      `json:"-"`
	Ciphertext      string     `json:"-"`
	Adapter         string     `json:"-"`
	AccountHash     string     `json:"-"`
	AmountCents     int64      `json:"amount_cents"`
	Credits         int64      `json:"credits"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	Remark          string     `json:"remark"`
	ExternalTradeNo string     `json:"external_trade_no,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type PaymentResult struct {
	OrderID      string `json:"order_id"`
	Status       string `json:"status"`
	BalanceAfter int64  `json:"balance_after"`
	Credits      int64  `json:"credits"`
	LedgerID     string `json:"ledger_id,omitempty"`
	Idempotent   bool   `json:"idempotent,omitempty"`
	Message      string `json:"message,omitempty"`
}

// Payment is accepted only after authenticating the provider and verifying its
// order identifier, amount and currency. Store settlement rechecks the snapshot.
type Payment struct {
	OrderID     string
	Adapter     string
	AccountHash string
	TradeNo     string
	AmountCents int64
	Currency    string
}

type Page[T any] struct {
	Rows       []T   `json:"rows"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int64 `json:"total_pages"`
}

type Filter struct {
	UserID   string
	Keyword  string
	Status   string
	Channel  string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

func (f Filter) Normalize() Filter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Page > 1000000 {
		f.Page = 1000000
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	if f.Status == "all" {
		f.Status = ""
	}
	if f.Channel == "all" {
		f.Channel = ""
	}
	return f
}

func NewPage[T any](rows []T, total int64, f Filter) Page[T] {
	if rows == nil {
		rows = []T{}
	}
	f = f.Normalize()
	return Page[T]{Rows: rows, Total: total, Page: f.Page, PageSize: f.PageSize, TotalPages: (total + int64(f.PageSize) - 1) / int64(f.PageSize)}
}

type RedemptionCode struct {
	ID             string     `json:"id"`
	Code           string     `json:"code"`
	Credits        int64      `json:"credits"`
	MaxUses        int        `json:"max_uses"`
	UsedCount      int        `json:"used_count"`
	RecordCount    int        `json:"record_count"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Enabled        bool       `json:"enabled"`
	BenefitType    string     `json:"benefit_type"`
	BatchID        string     `json:"batch_id"`
	BatchName      string     `json:"batch_name"`
	BatchExclusive bool       `json:"batch_exclusive"`
	Status         string     `json:"status"`
	Note           string     `json:"note"`
	CreatedAt      time.Time  `json:"created_at"`
}

type RedemptionRecord struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	CodeID          string    `json:"code_id"`
	BatchID         string    `json:"batch_id"`
	UserID          string    `json:"user_id"`
	UserEmailMasked string    `json:"user_email_masked"`
	UserNickname    string    `json:"user_nickname"`
	Credits         int64     `json:"credits"`
	RedeemedAt      time.Time `json:"redeemed_at"`
}

type RedemptionResult struct {
	Credits      int64  `json:"credits"`
	BalanceAfter int64  `json:"balance_after"`
	RecordID     string `json:"record_id"`
}

type CodeTier struct {
	Credits  int64 `json:"credits"`
	Quantity int   `json:"quantity"`
	MaxUses  int   `json:"max_uses"`
}

type CodeRequest struct {
	Credits        int64      `json:"credits"`
	Quantity       int        `json:"quantity"`
	MaxUses        int        `json:"max_uses"`
	ExpiresAt      *time.Time `json:"expires_at"`
	ValidDays      *int       `json:"valid_days"`
	BenefitType    string     `json:"benefit_type"`
	Prefix         string     `json:"prefix"`
	Note           string     `json:"note"`
	BatchID        string     `json:"batch_id"`
	BatchName      string     `json:"batch_name"`
	BatchExclusive bool       `json:"batch_exclusive"`
	Name           string     `json:"name"`
	Tiers          []CodeTier `json:"tiers"`
}

type Wave struct {
	BatchID   string           `json:"batch_id"`
	BatchName string           `json:"batch_name"`
	Quantity  int              `json:"quantity"`
	Codes     []RedemptionCode `json:"codes"`
}

type BenefitConfig struct {
	Enabled            bool   `json:"enabled"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	QQGroup            string `json:"qq_group"`
	QQLink             string `json:"qq_link"`
	WechatGroup        string `json:"wechat_group"`
	QQQRURL            string `json:"qq_qr_url"`
	WechatQRURL        string `json:"wechat_qr_url"`
	RedeemTutorial     string `json:"redeem_tutorial"`
	FreeHighlightLabel string `json:"free_highlight_label"`
}
