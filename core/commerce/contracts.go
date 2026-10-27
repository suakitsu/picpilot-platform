// Package commerce builds durable image plans; execution belongs to jobs.
package commerce

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
)

type Platform struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Enabled           bool     `json:"enabled"`
	Beta              bool     `json:"beta"`
	DefaultResolution string   `json:"default_resolution"`
	DefaultSize       string   `json:"default_size"`
	DefaultSlots      []string `json:"default_slots"`
	StylePrompt       string   `json:"style_prompt"`
	Language          string   `json:"language"`
	OverlayLanguage   string   `json:"overlay_language"`
}

type Slot struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	DefaultSize string `json:"default_size"`
	SortOrder   int    `json:"sort_order"`
}

type Template struct {
	ID              string         `json:"id"`
	Platform        string         `json:"platform"`
	Slot            string         `json:"slot"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	PromptTemplate  string         `json:"prompt_template"`
	NegativePrompt  string         `json:"negative_prompt"`
	OverlayTemplate map[string]any `json:"overlay_template"`
	DefaultSize     string         `json:"default_size"`
	Enabled         bool           `json:"enabled"`
	SortOrder       int            `json:"sort_order"`
	ProviderParams  map[string]any `json:"provider_params"`
}

type Catalog struct {
	Platforms []Platform       `json:"platforms"`
	Slots     []Slot           `json:"slots"`
	Templates []Template       `json:"templates"`
	CostRule  map[string]int64 `json:"cost_rule"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type Product struct {
	ProductName   string   `json:"product_name"`
	Category      string   `json:"category"`
	Brand         string   `json:"brand"`
	SellingPoints []string `json:"selling_points"`
	Material      string   `json:"material"`
	Specs         string   `json:"specs"`
	Audience      string   `json:"audience"`
	Scenario      string   `json:"scenario"`
	PriceText     string   `json:"price_text"`
	PromoText     string   `json:"promo_text"`
	Country       string   `json:"country"`
	Language      string   `json:"language"`
}

type EstimateRequest struct {
	Platform      string   `json:"platform"`
	SelectedSlots []string `json:"selected_slots"`
	Resolution    string   `json:"resolution"`
}

type GenerateRequest struct {
	Platform          string         `json:"platform"`
	Product           Product        `json:"product_info"`
	SelectedSlots     []string       `json:"selected_slots"`
	Resolution        string         `json:"resolution"`
	ReferenceImageIDs []string       `json:"reference_image_ids"`
	ReferenceAssetIDs []string       `json:"reference_asset_ids"`
	ReferenceURL      string         `json:"reference_url"`
	OverlayOptions    map[string]any `json:"overlay_options"`
}

type SlotCost struct {
	Slot        string `json:"slot"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	CreditsCost int64  `json:"credits_cost"`
}

type Estimate struct {
	Platform    string     `json:"platform"`
	Resolution  string     `json:"resolution"`
	ImageCount  int        `json:"image_count"`
	CreditsCost int64      `json:"credits_cost"`
	SlotCosts   []SlotCost `json:"slot_costs"`
}

// Payload extends the common jobs document only with presentation metadata and
// optional routing constraints. No credentials, URLs or image bytes are stored.
type Payload struct {
	jobs.Payload
	Snapshot      map[string]any `json:"commerce_snapshot"`
	ModelPriority []string       `json:"commerce_model_priority,omitempty"`
}

type PlannedImage struct {
	Slot        string  `json:"slot"`
	Name        string  `json:"name"`
	CreditsCost int64   `json:"credits_cost"`
	Payload     Payload `json:"payload"`
}

var codePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)
var ordinal = regexp.MustCompile(`^\s*(?:[0-9]{1,2}\s*[.\x{3001})\x{ff09}]|[\x{ff08}(][0-9]{1,2}[\x{ff09})]|[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}]+\s*[.\x{3001})\x{ff09}]|[\x{2022}\x{25cf}\x{25aa}\x{25e6}\x{00b7}]|[-*]\s+)\s*`)

func VisibleCopy(s string) string {
	s = strings.TrimSpace(s)
	loc := ordinal.FindStringIndex(s)
	// A decimal quantity such as 1.5L is not an ordinal list marker.
	if loc != nil && !(loc[1] < len(s) && s[loc[1]-1] == '.' && s[loc[1]] >= '0' && s[loc[1]] <= '9') {
		s = s[loc[1]:]
	}
	return strings.Join(strings.Fields(s), " ")
}

func (p *Product) Normalize() error {
	for _, v := range []*string{&p.ProductName, &p.Category, &p.Brand, &p.Material, &p.Specs, &p.Audience, &p.Scenario, &p.PriceText, &p.PromoText, &p.Country, &p.Language} {
		*v = VisibleCopy(*v)
	}
	points := []string{}
	seen := map[string]bool{}
	for _, v := range p.SellingPoints {
		v = VisibleCopy(v)
		if v != "" && !seen[v] && len(points) < 8 {
			points = append(points, v)
			seen[v] = true
		}
	}
	p.SellingPoints = points
	if p.ProductName == "" || creative.ValidateFields(p) != nil {
		return creative.ErrInvalid
	}
	return nil
}

var sizes = [][3]string{
	{"1024x1024", "2048x2048", "2880x2880"}, {"1040x832", "2080x1664", "3200x2560"},
	{"720x1280", "1152x2048", "2160x3840"}, {"1280x720", "2048x1152", "3840x2160"},
	{"1024x768", "2048x1536", "3264x2448"}, {"1008x672", "2016x1344", "3504x2336"},
	{"832x1040", "1664x2080", "2560x3200"}, {"768x1024", "1536x2048", "2448x3264"},
	{"672x1008", "1344x2016", "2336x3504"}, {"1344x576", "2016x864", "3696x1584"},
}

func Resolution(s, fallback string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		s = strings.ToUpper(fallback)
	}
	if s != "1K" && s != "2K" && s != "4K" {
		return "1K"
	}
	return s
}

func Size(base, resolution string) (string, error) {
	i := map[string]int{"1K": 0, "2K": 1, "4K": 2}[Resolution(resolution, "1K")]
	for _, row := range sizes {
		for _, s := range row {
			if s == base {
				return row[i], nil
			}
		}
	}
	return "", creative.ErrInvalid
}

func AspectSize(width, height int, resolution string) (string, error) {
	if width <= 0 || height <= 0 {
		return "", creative.ErrInvalid
	}
	best, delta := "1024x1024", math.MaxFloat64
	for _, row := range sizes {
		var w, h int
		_, _ = fmt.Sscanf(row[0], "%dx%d", &w, &h)
		d := math.Abs(math.Log((float64(width) / float64(height)) / (float64(w) / float64(h))))
		if d < delta {
			delta = d
			best = row[0]
		}
	}
	return Size(best, resolution)
}

func (c Catalog) Validate() error {
	if creative.ValidateFields(c) != nil || len(c.Platforms) == 0 || len(c.Platforms) > 32 || len(c.Slots) == 0 || len(c.Slots) > 64 || len(c.Templates) > 512 {
		return creative.ErrInvalid
	}
	platforms, slots, ids, enabled := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range c.Slots {
		if !codePattern.MatchString(s.Code) || slots[s.Code] || strings.TrimSpace(s.Name) == "" {
			return creative.ErrInvalid
		}
		if _, err := Size(s.DefaultSize, "1K"); err != nil {
			return err
		}
		slots[s.Code] = true
	}
	for _, p := range c.Platforms {
		if !codePattern.MatchString(p.Code) || platforms[p.Code] || p.Name == "" || len(p.DefaultSlots) == 0 || len(p.DefaultSlots) > 64 {
			return creative.ErrInvalid
		}
		if p.DefaultResolution != "1K" && p.DefaultResolution != "2K" && p.DefaultResolution != "4K" {
			return creative.ErrInvalid
		}
		if _, err := Size(p.DefaultSize, "1K"); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, slot := range p.DefaultSlots {
			if !slots[slot] || seen[slot] {
				return creative.ErrInvalid
			}
			seen[slot] = true
		}
		platforms[p.Code] = true
	}
	for _, t := range c.Templates {
		if !codePattern.MatchString(t.ID) || ids[t.ID] || !platforms[t.Platform] || !slots[t.Slot] || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.PromptTemplate) == "" {
			return creative.ErrInvalid
		}
		ids[t.ID] = true
		if t.Enabled {
			key := t.Platform + "/" + t.Slot
			if enabled[key] {
				return creative.ErrInvalid
			}
			enabled[key] = true
		}
		if _, err := Size(t.DefaultSize, "1K"); err != nil {
			return err
		}
		if err := ValidateProviderParams(t.ProviderParams); err != nil {
			return err
		}
	}
	if len(c.CostRule) != 3 {
		return creative.ErrInvalid
	}
	for _, r := range []string{"1K", "2K", "4K"} {
		v, ok := c.CostRule[r]
		if !ok || v < 0 || v > 1000000 {
			return creative.ErrInvalid
		}
	}
	return nil
}

func ValidateProviderParams(params map[string]any) error {
	allowed := map[string][]string{"quality": {"auto", "low", "medium", "high", "standard", "hd"}, "output_format": {"png", "jpeg", "webp"}, "background": {"auto", "transparent", "opaque"}, "moderation": {"auto", "low"}}
	for k, v := range params {
		if k == "n" {
			if string(creative.JSON(v)) != "1" {
				return creative.ErrInvalid
			}
			continue
		}
		values, ok := allowed[k]
		if !ok {
			return creative.ErrInvalid
		}
		s, ok := v.(string)
		if !ok {
			return creative.ErrInvalid
		}
		found := false
		for _, a := range values {
			found = found || s == a
		}
		if !found {
			return creative.ErrInvalid
		}
	}
	return nil
}

func (c Catalog) Select(in EstimateRequest, personal bool) (Platform, []Template, Estimate, error) {
	var p Platform
	for _, v := range c.Platforms {
		if v.Code == in.Platform && v.Enabled {
			p = v
			break
		}
	}
	if p.Code == "" {
		return p, nil, Estimate{}, creative.ErrInvalid
	}
	requested := in.SelectedSlots
	if len(requested) == 0 {
		requested = p.DefaultSlots
	}
	if len(requested) > 64 {
		return p, nil, Estimate{}, creative.ErrInvalid
	}
	res := Estimate{Platform: p.Code, Resolution: Resolution(in.Resolution, p.DefaultResolution), SlotCosts: []SlotCost{}}
	selected := []Template{}
	seen := map[string]bool{}
	for _, code := range requested {
		if seen[code] {
			continue
		}
		seen[code] = true
		for _, t := range c.Templates {
			if t.Platform != p.Code || t.Slot != code || !t.Enabled {
				continue
			}
			size, err := Size(t.DefaultSize, res.Resolution)
			if err != nil {
				return p, nil, res, err
			}
			cost := c.CostRule[res.Resolution]
			if personal {
				cost = 0
			}
			selected = append(selected, t)
			res.SlotCosts = append(res.SlotCosts, SlotCost{code, t.Name, size, cost})
			res.CreditsCost += cost
			break
		}
	}
	res.ImageCount = len(selected)
	if res.ImageCount == 0 {
		return p, nil, res, creative.ErrInvalid
	}
	return p, selected, res, nil
}

func CopyObject(value map[string]any) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(creative.JSON(value), &out)
	return out
}
