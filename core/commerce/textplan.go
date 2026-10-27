package commerce

import "strings"

type slotTextRule struct{ layout, position, density, strategy, composition string }

var slotTextRules = map[string]slotTextRule{
	"main_image":       {"hero_product_minimal", "corner_or_under_product", "medium_when_confirmed", "title + one or two benefits + optional confirmed price", "Large product hero with safe framing; compact confirmed copy, never a tall side spec card."},
	"white_background": {"clean_no_text", "none", "none", "no visible text, no price", "No visible text. Pure product studio image."},
	"selling_point":    {"distributed_benefit_callouts", "around_product", "low", "benefit callouts only, no price", "Small callouts around real features; no generic heading."},
	"scenario":         {"lifestyle_photo_caption", "natural_caption_area", "very_low", "scene caption only, no price", "Lifestyle photo first; at most one short scene caption."},
	"crowd":            {"lifestyle_photo_caption", "natural_caption_area", "very_low", "audience caption only, no price", "Show the confirmed audience; at most one short caption."},
	"detail":           {"macro_detail_labels", "near_relevant_parts", "very_low", "material/detail labels only, no price", "Macro details and tiny labels beside the relevant parts."},
	"model_image":      {"model_or_hand_use_photo", "natural_scene_no_panel", "very_low", "model/use photo with no price and no panel", "Show scale and real use in a natural photo, no text panel."},
	"detail_closeup":   {"macro_closeup_labels", "near_macro_detail", "very_low", "macro detail labels only, no price", "One physical detail, with at most two tiny labels and no headline."},
	"detail_function":  {"single_function_demo", "near_function_area", "very_low", "one function label only, no price", "One confirmed use action or function; short label only when supplied."},
	"sku_display":      {"static_variant_gallery", "small_labels_beside_variants", "very_low", "static SKU/variant labels only, no price", "Real variants or angles only; no marketing panel."},
	"specification":    {"spec_grid_or_measure_lines", "bottom_grid_or_measure_lines", "low", "confirmed specs only, no price", "Compact factual grid or measurement arrows; no invented measurements."},
	"comparison":       {"split_comparison", "inside_each_split_area", "low", "comparison points only, no price", "Labels inside split areas; compare supplied facts only."},
	"packaging":        {"flatlay_item_tags", "beside_each_component", "very_low", "package/component labels only, no price", "Flatlay or unboxing with small tags beside real components."},
	"trust":            {"premium_scene_small_brand_cue", "small_corner_or_near_product", "very_low", "brand/trust cue only, no price", "Product/factory details with restrained branding; no fake seals or badges."},
	"social_cover":     {"mobile_hook_overlay", "top_or_bottom_hook", "very_low", "short mobile hook only, no price", "Mobile content cover with one short confirmed hook."},
	"promo":            {"campaign_visual", "campaign_area", "medium_when_confirmed", "campaign text + optional confirmed price", "Campaign visual; price/promotion only when supplied by the user."},
}

func firstCopy(values ...string) string {
	for _, v := range values {
		if v = VisibleCopy(v); v != "" {
			return v
		}
	}
	return ""
}
func point(p Product, i int) string {
	if i < len(p.SellingPoints) {
		return p.SellingPoints[i]
	}
	return ""
}
func uniqueCopy(limit int, exclude []string, values ...string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range exclude {
		seen[strings.ToLower(VisibleCopy(v))] = true
	}
	for _, v := range values {
		v = VisibleCopy(v)
		key := strings.ToLower(v)
		if v != "" && !seen[key] {
			out = append(out, v)
			seen[key] = true
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func slotCopy(slot string, p Product) (string, string, []string) {
	a, b, c := point(p, 0), point(p, 1), point(p, 2)
	title, subtitle := firstCopy(a, p.ProductName), b
	values := []string{a, b, c}
	switch slot {
	case "main_image":
		title, subtitle = p.ProductName, firstCopy(a, p.Material, p.Specs)
		values = []string{a, b}
	case "promo":
		title, subtitle = firstCopy(p.PromoText, p.ProductName), firstCopy(a, p.Scenario)
		values = []string{a, b}
	case "selling_point":
		subtitle = firstCopy(b, p.Material, p.Specs)
	case "detail", "detail_closeup":
		title, subtitle = firstCopy(p.Material, p.Specs), p.Specs
		values = []string{p.Material, p.Specs}
	case "model_image":
		title, subtitle = firstCopy(p.Audience, p.Scenario), p.Scenario
		values = []string{p.Audience, p.Scenario}
	case "detail_function":
		title, subtitle = firstCopy(a, p.Specs, p.Material), firstCopy(b, p.Scenario)
		values = []string{a, p.Specs}
	case "sku_display":
		title, subtitle = firstCopy(p.Specs, p.Material, p.ProductName), ""
		values = []string{p.Specs, p.Material}
	case "specification":
		title, subtitle = firstCopy(p.Specs, p.Material), ""
		values = []string{p.Specs, p.Material}
	case "packaging":
		title, subtitle = firstCopy(p.Specs, p.Material), p.Material
		values = []string{p.Specs, p.Material}
	case "scenario":
		title, subtitle = firstCopy(p.Scenario, p.Audience), p.Audience
		values = []string{p.Scenario, p.Audience}
	case "crowd":
		title, subtitle = firstCopy(p.Audience, p.Scenario), p.Scenario
		values = []string{p.Audience, p.Scenario}
	case "comparison":
		title, subtitle = firstCopy(a, b), firstCopy(b, c)
		values = []string{a, b}
	case "trust":
		title, subtitle = firstCopy(p.Material, a, p.ProductName), firstCopy(p.Specs, b)
		values = []string{p.Material, p.Specs}
	case "social_cover":
		title, subtitle = firstCopy(a, p.Scenario, p.ProductName), firstCopy(p.Scenario, p.Audience)
		values = []string{a, b}
	case "white_background":
		return "", "", []string{}
	}
	if strings.EqualFold(title, subtitle) {
		subtitle = ""
	}
	return title, subtitle, uniqueCopy(3, []string{title, subtitle}, values...)
}

func visiblePlan(template Template, platform Platform, slot Slot, p Product, vars map[string]string, options map[string]any) map[string]any {
	plan := renderObject(CopyObject(template.OverlayTemplate), vars).(map[string]any)
	layer, _ := plan["text_layer"].(map[string]any)
	if layer == nil {
		layer = map[string]any{}
	}
	if len(options) > 0 {
		plan["user_options"] = CopyObject(options)
		for key, value := range options {
			if value != nil {
				if text, ok := value.(string); !ok || strings.TrimSpace(text) != "" {
					layer[key] = value
				}
			}
		}
	}
	title, subtitle, points := slotCopy(slot.Code, p)
	layer["title"], layer["subtitle"], layer["selling_points"] = title, subtitle, points
	layer["price_text"], layer["badge_text"], layer["footer_text"] = "", "", ""
	if slot.Code == "main_image" || slot.Code == "promo" {
		layer["price_text"] = p.PriceText
	}
	if slot.Code == "main_image" || slot.Code == "promo" || slot.Code == "social_cover" {
		layer["badge_text"] = p.PromoText
	}
	if slot.Code == "main_image" || slot.Code == "trust" {
		layer["footer_text"] = p.Brand
	}
	rule, ok := slotTextRules[slot.Code]
	if !ok {
		rule = slotTextRule{"slot_specific_composition", "composition_dependent", "low", "concise confirmed copy, no price", "Use a distinct composition, avoid repeated side panels."}
	}
	layer["layout"], layer["position"], layer["copy_density"], layer["text_strategy"], layer["composition_instruction"] = rule.layout, rule.position, rule.density, rule.strategy, rule.composition
	background := "transparent_or_subtle_labels"
	switch platform.Code {
	case "pinduoduo":
		background = "small_high_contrast_labels_only"
	case "xiaohongshu":
		background = "light_note_caption_not_card_stack"
	case "douyin", "tiktok_shop":
		background = "minimal_mobile_caption_overlay"
	case "alibaba_1688":
		background = "small_factory_tags_not_full_panel"
	}
	layer["background_style"] = background
	layer["safe_margin_percent"], layer["subject_safe_margin_percent"], layer["max_title_lines"], layer["max_label_lines"] = 8, 6, 2, 2
	layer["edge_policy"] = "Keep every glyph, label, shadow, badge and graphic fully inside the safe area. Auto-shrink or wrap instead of clipping."
	layer["canvas_framing_policy"], layer["human_framing_policy"] = imageSafety, imageSafety
	layer["list_marker_policy"] = "Do not add ordered-list numbers or bullet prefixes to visible copy. Preserve confirmed numeric quantities."
	layer["forbidden_visible_text"] = []string{slot.Code, slot.Name, template.Name, "product main image", "core selling point image", "detail image", "comparison image"}
	if platform.Code == "tiktok_shop" {
		layer["title"], layer["subtitle"], layer["price_text"], layer["badge_text"], layer["footer_text"] = "", "", "", "", ""
		layer["selling_points"], layer["copy_density"] = []string{}, "none"
		layer["text_strategy"] = "TikTok Shop gallery asset, no visible text, no price"
		if slot.Code == "detail" || slot.Code == "detail_closeup" || slot.Code == "detail_function" {
			layer["selling_points"] = uniqueCopy(2, nil, p.Material, p.Specs, point(p, 0))
			layer["copy_density"] = "tiny_labels_only"
			layer["text_strategy"] = "Prefer no added text; at most two tiny confirmed physical-detail labels, no headline, no price."
		}
	}
	plan["platform"], plan["platform_code"], plan["slot"], plan["usage"] = platform.Name, platform.Code, slot.Code, slot.Name
	plan["target_market"], plan["visible_text_language"], plan["enabled"], plan["rendered_by"] = p.Country, p.Language, slot.Code != "white_background", "ai_image_model"
	plan["text_layer"] = layer
	return plan
}
