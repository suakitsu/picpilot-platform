package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/billing"
)

func (b *billingHandler) redeem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !b.limiter.allow("billing-redeem:"+currentUser(r).ID, 20, 5*time.Minute) {
		fail(w, r, 429, "RATE_LIMITED", "Too many redemption attempts")
		return
	}
	result, err := b.Store.RedeemBillingCode(r.Context(), currentUser(r).ID, input.Code)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) codes(w http.ResponseWriter, r *http.Request) {
	f, valid := billingFilter(w, r, true)
	if !valid {
		return
	}
	result, err := b.Store.BillingCodes(r.Context(), f)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) createCodes(w http.ResponseWriter, r *http.Request) {
	b.generateCodes(w, r, false)
}
func (b *billingHandler) createWave(w http.ResponseWriter, r *http.Request) {
	b.generateCodes(w, r, true)
}
func (b *billingHandler) generateCodes(w http.ResponseWriter, r *http.Request, wave bool) {
	var input billing.CodeRequest
	if !decode(w, r, &input) {
		return
	}
	result, err := b.Store.CreateBillingCodes(r.Context(), input, currentUser(r).ID, wave)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	if wave {
		ok(w, r, result)
	} else {
		ok(w, r, result.Codes)
	}
}
func (b *billingHandler) records(w http.ResponseWriter, r *http.Request) {
	f, valid := billingFilter(w, r, true)
	if !valid {
		return
	}
	result, err := b.Store.BillingRedemptions(r.Context(), f)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) benefitConfig(w http.ResponseWriter, r *http.Request) {
	result, err := b.Store.BillingBenefitConfig(r.Context())
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}
func (b *billingHandler) saveBenefitConfig(w http.ResponseWriter, r *http.Request) {
	var input map[string]json.RawMessage
	if !decode(w, r, &input) {
		return
	}
	result, err := b.Store.PatchBillingBenefitConfig(r.Context(), input, currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, result)
}

func (b *billingHandler) uploadBenefitQR(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, (3<<20)+(64<<10))
	if err := r.ParseMultipartForm(3 << 20); err != nil {
		fail(w, r, 400, "INVALID_MULTIPART", "Expected one QR image no larger than 3 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	kind := r.FormValue("kind")
	if kind == "" {
		kind = "qq"
	}
	kind = strings.ToLower(kind)
	if kind != "qq" && kind != "wechat" {
		b.failure(w, r, billing.ErrInvalid)
		return
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		b.failure(w, r, billing.ErrInvalid)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		b.failure(w, r, billing.ErrInvalid)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (3<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 3<<20 {
		fail(w, r, 413, "IMAGE_TOO_LARGE", "QR image must be no larger than 3 MiB")
		return
	}
	a, err := b.saveImage(r.Context(), currentUser(r).ID, "", "", "benefit_qr", header.Filename, bytes.NewReader(data))
	if err != nil {
		fail(w, r, 400, "INVALID_QR_IMAGE", "Use a valid PNG, JPEG or WebP image")
		return
	}
	target := "/api/public/benefits/qr/" + a.ID
	raw, _ := json.Marshal(target)
	result, err := b.Store.PatchBillingBenefitConfig(r.Context(), map[string]json.RawMessage{kind + "_qr_url": raw}, currentUser(r).ID)
	if err != nil {
		b.failure(w, r, err)
		return
	}
	ok(w, r, map[string]any{"kind": kind, "url": target, "config": result})
}

func (b *billingHandler) serveBenefitQR(w http.ResponseWriter, r *http.Request) {
	cfg, err := b.Store.BillingBenefitConfig(r.Context())
	if err != nil {
		b.failure(w, r, err)
		return
	}
	target := "/api/public/benefits/qr/" + r.PathValue("id")
	if target != cfg.QQQRURL && target != cfg.WechatQRURL {
		fail(w, r, 404, "NOT_FOUND", "QR image not found")
		return
	}
	a, err := b.Store.Asset(r.Context(), r.PathValue("id"))
	if err != nil || a.ImageType != "benefit_qr" {
		fail(w, r, 404, "NOT_FOUND", "QR image not found")
		return
	}
	f, err := b.Files.Open(a.StorageKey)
	if err != nil {
		fail(w, r, 404, "NOT_FOUND", "QR image not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": a.OriginalFilename}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, a.OriginalFilename, a.CreatedAt, f)
}
