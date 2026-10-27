package commerce

import (
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
)

const imageSafety = `Treat reference images as the source of truth for product structure, color, material, silhouette and printed marks. Preserve the same single front/back artwork count and placement; do not duplicate motifs, relocate logos or invent details.
Keep the complete product, people, heads, hands and visible words inside the safe area. Do not cut heads or faces at the image edges; for close-ups include the whole head or exclude it completely. Do not force a center crop or upscale the supplied reference.
Only render the confirmed visible words in the requested language. Empty fields require no reserved text space. Never display internal slot names, marketplace UI, QR codes, watermarks, fake badges, invented certifications, prices, measurements or claims.
Vary the natural layout for each image; do not repeat a right-side white information card. Typography must fit the existing composition without obscuring the product.`

const defaultPrompt = `Create an original ecommerce suite image for {{platform_name}}.
Product: {{product_name}}. Category: {{category}}. Brand: {{brand}}.
Target market: {{country}}. Visible text language: {{language}}.
Material/specs: {{material}} {{specs}}. Audience: {{audience}}. Scenario: {{scenario}}.
Real selling points: {{selling_points}}.
Platform style: {{platform_style}}
Slot goal: {{slot_name}} - {{slot_description}}.
{{slot_strategy}}`

func DefaultCatalog() Catalog {
	c := Catalog{Platforms: []Platform{}, Slots: []Slot{}, Templates: []Template{}, CostRule: map[string]int64{"1K": 6, "2K": 24, "4K": 30}, UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	rows := [][4]string{
		{"main_image", "Product main image", "Clear product-first shelf image", "1024x1024"},
		{"white_background", "White background", "Accurate silhouette and structure on pure white", "1024x1024"},
		{"selling_point", "Core selling points", "Two or three confirmed benefits", "1024x1024"},
		{"scenario", "Usage scenario", "A credible real-life use setting", "832x1040"},
		{"detail", "Product details", "Actual material, construction and workmanship", "1024x1024"},
		{"model_image", "Model image", "Natural holding, wearing or use to show scale", "1024x1024"},
		{"detail_closeup", "Detail close-up", "Real textures, connectors and workmanship", "1024x1024"},
		{"detail_function", "Function details", "One verified function or use action", "1024x1024"},
		{"sku_display", "SKU display", "Actual variants or multiple angles when variants are absent", "1024x1024"},
		{"specification", "Specifications", "Only supplied measurements and parameters", "1024x1024"},
		{"comparison", "Comparison", "Only confirmed distinctions, never invented competitors", "1024x1024"},
		{"packaging", "Package contents", "Only supplied packaging, accessories and quantities", "1024x1024"},
		{"crowd", "Target audience", "Credible users, styling and intended setting", "832x1040"},
		{"trust", "Brand trust", "Restrained quality and service, no fake certifications", "1024x1024"},
		{"social_cover", "Social cover", "A natural mobile-first content photograph", "720x1280"},
		{"promo", "Promotion", "Only user-confirmed prices and offers", "1024x1024"},
	}
	for i, r := range rows {
		c.Slots = append(c.Slots, Slot{Code: r[0], Name: r[1], Description: r[2], Category: "commerce", DefaultSize: r[3], SortOrder: i + 1})
	}
	platforms := []struct {
		code, name, size, language, slots string
		beta                              bool
	}{
		{"taobao_tmall", "\u6dd8\u5b9d/\u5929\u732b", "1024x1024", "zh-CN", "main_image selling_point scenario detail specification comparison packaging trust", false},
		{"pinduoduo", "\u62fc\u591a\u591a", "1024x1024", "zh-CN", "main_image selling_point promo comparison detail specification packaging crowd", false},
		{"douyin", "\u6296\u97f3\u7535\u5546", "720x1280", "zh-CN", "social_cover main_image scenario selling_point crowd detail", false},
		{"xiaohongshu", "\u5c0f\u7ea2\u4e66", "768x1024", "zh-CN", "social_cover scenario detail crowd selling_point", false},
		{"tiktok_shop", "TikTok Shop", "1024x1024", "en-US", "main_image model_image scenario detail detail_closeup detail_function sku_display", false},
		{"jd", "\u4eac\u4e1c", "1024x1024", "zh-CN", "main_image white_background selling_point detail specification trust", true},
		{"alibaba_1688", "1688", "1024x1024", "zh-CN", "main_image specification detail packaging comparison trust", true},
		{"vipshop", "\u552f\u54c1\u4f1a", "832x1040", "zh-CN", "main_image scenario detail selling_point crowd promo", true},
	}
	for _, r := range platforms {
		p := Platform{Code: r.code, Name: r.name, Enabled: true, Beta: r.beta, DefaultResolution: "1K", DefaultSize: r.size, DefaultSlots: strings.Fields(r.slots), StylePrompt: platformDirection(r.code), Language: r.language, OverlayLanguage: r.language}
		p.Description = p.StylePrompt
		c.Platforms = append(c.Platforms, p)
		for i, code := range p.DefaultSlots {
			for _, s := range c.Slots {
				if s.Code == code {
					c.Templates = append(c.Templates, Template{ID: p.Code + "-" + code, Platform: p.Code, Slot: code, Name: p.Name + " / " + s.Name, Description: s.Description, PromptTemplate: defaultPrompt, NegativePrompt: "cropped head, clipped text, altered product marks, invented claims, watermark, marketplace UI", OverlayTemplate: map[string]any{"enabled": true, "text_layer": map[string]any{"title": "{{product_name}}", "subtitle": "", "selling_points": []string{"{{selling_point_1}}", "{{selling_point_2}}", "{{selling_point_3}}"}}, "platform_code": p.Code, "slot": code}, DefaultSize: s.DefaultSize, Enabled: true, SortOrder: i + 1, ProviderParams: map[string]any{"quality": "auto", "output_format": "png", "n": 1}})
				}
			}
		}
	}
	return c
}

func platformDirection(code string) string {
	switch code {
	case "taobao_tmall":
		return "Taobao/Tmall mobile PDP set: clear product hero, distributed factual callouts, believable details and scenes; restrained Chinese copy."
	case "pinduoduo":
		return "Pinduoduo shelf set: immediate thumbnail recognition and confirmed benefits, direct colors, only confirmed price offers."
	case "douyin":
		return "Douyin ecommerce set: natural people, handheld use and product-card assets, like short-video stills. Not every image is a live cover or promotion board."
	case "xiaohongshu":
		return "Xiaohongshu seeding set: authentic lifestyle, unboxing and use, light note-like copy without hard-sell poster layouts."
	case "tiktok_shop":
		return "TikTok Shop PDP gallery set: realistic shop product photography, pure white/light background for the main image, no added text or badges on main/model/scenario/SKU images. No live cover, creator UI, ad poster or oversized text panels."
	case "jd":
		return "JD quality retail gallery: clean backgrounds, trustworthy specifications, detailed materials and restrained service claims."
	case "alibaba_1688":
		return "1688 supplier set: materials, construction, confirmed supply, specifications, packaging and purchasing details, not consumer promotional posters."
	case "vipshop":
		return "Vipshop branded retail gallery: fashion, material quality and lifestyle, restrained confirmed promotion details."
	}
	return "Consistent product-first retail gallery."
}

func render(text string, vars map[string]string) string {
	// Replace placeholders once so values containing template syntax remain data.
	var b strings.Builder
	for {
		i := strings.Index(text, "{{")
		if i < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:i])
		text = text[i+2:]
		j := strings.Index(text, "}}")
		if j < 0 {
			b.WriteString("{{" + text)
			break
		}
		b.WriteString(vars[strings.TrimSpace(text[:j])])
		text = text[j+2:]
	}
	return b.String()
}

func renderObject(v any, vars map[string]string) any {
	switch x := v.(type) {
	case string:
		return render(x, vars)
	case []any:
		out := []any{}
		for _, s := range x {
			out = append(out, renderObject(s, vars))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, s := range x {
			out[k] = renderObject(s, vars)
		}
		return out
	default:
		return x
	}
}

func (c Catalog) Build(p Platform, templates []Template, estimate Estimate, in GenerateRequest, refs []string) ([]PlannedImage, error) {
	info := in.Product
	if err := info.Normalize(); err != nil {
		return nil, err
	}
	if info.Language == "" {
		info.Language = p.OverlayLanguage
	}
	if info.Country == "" {
		if info.Language == "zh-CN" {
			info.Country = "CN"
		} else {
			info.Country = "US"
		}
	}
	vars := map[string]string{"platform_name": p.Name, "platform_style": p.StylePrompt, "platform_suite_direction": platformDirection(p.Code), "product_name": info.ProductName, "category": info.Category, "brand": info.Brand, "material": info.Material, "specs": info.Specs, "audience": info.Audience, "scenario": info.Scenario, "price_text": info.PriceText, "promo_text": info.PromoText, "country": info.Country, "language": info.Language, "selling_points": strings.Join(info.SellingPoints, "; ")}
	for i, k := range []string{"selling_point_1", "selling_point_2", "selling_point_3"} {
		if i < len(info.SellingPoints) {
			vars[k] = info.SellingPoints[i]
		}
	}
	images := []PlannedImage{}
	for i, t := range templates {
		var slot Slot
		for _, v := range c.Slots {
			if v.Code == t.Slot {
				slot = v
				break
			}
		}
		vars["slot_name"], vars["slot_description"], vars["slot_strategy"] = slot.Name, slot.Description, slot.Description
		vars["slot_quality_direction"] = imageSafety
		vars["platform_text_policy"] = platformDirection(p.Code)
		plan := visiblePlan(t, p, slot, info, vars, in.OverlayOptions)
		prompt := render(t.PromptTemplate, vars) + "\nRuntime platform suite quality guidance:\n" + platformDirection(p.Code) + "\n" + imageSafety + "\nConfirmed visible text plan:\n" + string(creative.JSON(plan))
		if t.NegativePrompt != "" {
			prompt += "\nAvoid: " + t.NegativePrompt
		}
		payload := Payload{Payload: jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: prompt, Size: estimate.SlotCosts[i].Size, ReferenceAssetIDs: refs}}, Snapshot: map[string]any{"taskKind": "commerce_suite", "productName": info.ProductName, "imageType": t.Slot, "imageName": t.Name, "platform": p.Code, "platformName": p.Name, "slot": t.Slot, "slotName": slot.Name, "generationSize": estimate.SlotCosts[i].Size, "outputSize": estimate.SlotCosts[i].Size, "resolution": estimate.Resolution, "templateId": t.ID, "productInfo": info, "visibleTextPlan": plan, "reference_policy": "preserve", "reference_count": len(refs), "text_mode": "ai_short_text", "intent": "poster_layout", "postProcess": map[string]any{"type": "ai_visible_text", "overlay_saved": false, "background_image": "ai_generated", "final_image": "ai_generated"}}}
		payload.Image.Quality, _ = t.ProviderParams["quality"].(string)
		payload.Image.OutputFormat, _ = t.ProviderParams["output_format"].(string)
		payload.Image.Background, _ = t.ProviderParams["background"].(string)
		payload.Image.Moderation, _ = t.ProviderParams["moderation"].(string)
		if payload.Validate("image") != nil || creative.ValidateFields(payload) != nil {
			return nil, creative.ErrInvalid
		}
		images = append(images, PlannedImage{t.Slot, t.Name, estimate.SlotCosts[i].CreditsCost, payload})
	}
	return images, nil
}
