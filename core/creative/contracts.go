// Package creative contains provider-independent business contracts and prompts.
package creative

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
)

const MaxField = 32000
const MaxReferences = 10
const MaxVersions = 10

var ErrInvalid = errors.New("invalid creative request or result")

type CreateProject struct {
	ProductName          string   `json:"product_name"`
	Brand                string   `json:"brand"`
	Country              string   `json:"country"`
	Language             string   `json:"language"`
	Description          string   `json:"description"`
	Category             string   `json:"category"`
	Material             string   `json:"material"`
	Dimensions           string   `json:"dimensions"`
	VisualPrompt         string   `json:"visual_prompt"`
	Keywords             []string `json:"keywords"`
	TitleKeywords        []string `json:"title_keywords"`
	SellingPointKeywords []string `json:"selling_point_keywords"`
	DescriptionKeywords  []string `json:"description_keywords"`
	IncludeAplus         *bool    `json:"include_aplus"`
	AplusModule          string   `json:"aplus_module"`
	AplusSize            string   `json:"aplus_size"`
	AplusPrompt          string   `json:"aplus_prompt"`
}

type ImageRequest struct {
	Model             string   `json:"model,omitempty"`
	FallbackModel     string   `json:"fallback_model,omitempty"`
	ProductName       string   `json:"product_name"`
	Brand             string   `json:"brand"`
	Prompt            string   `json:"prompt"`
	Size              string   `json:"size"`
	Language          string   `json:"language"`
	Module            string   `json:"module"`
	ReferenceURL      string   `json:"reference_url"`
	ReferenceURLs     []string `json:"reference_urls"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
	Intent            string   `json:"intent"`
	TextMode          string   `json:"text_mode"`
	ReferencePolicy   string   `json:"reference_policy"`
	TemplateID        string   `json:"template_id"`
	ChatID            string   `json:"chat_id"`
	Message           string   `json:"message"`
	Style             string   `json:"style"`
}

type PackageRequest struct {
	IncludeAplus *bool  `json:"include_aplus"`
	AplusModule  string `json:"aplus_module"`
	AplusSize    string `json:"aplus_size"`
	AplusPrompt  string `json:"aplus_prompt"`
	Language     string `json:"language"`
}

type Slot struct{ Type, Name, Direction string }

var ListingSlots = []Slot{
	{"main", "1 Main image", "Pure white background. Only the actual product and included accessories; no text, badges, props or invented packaging."},
	{"scenario", "2 Lifestyle", "Show a credible use scene and target audience while preserving the product's shape and color."},
	{"features", "3 Features", "Show verified benefits and close-up details with concise readable captions in the requested language."},
	{"dimensions", "4 Dimensions", "Use only confirmed dimensions and units. If missing, show scale context without invented measurements."},
	{"steps", "5 Instructions", "Show only known use or assembly steps; do not invent mechanisms or included parts."},
	{"comparison", "6 Comparison", "Explain verified distinctions; do not fabricate competitors, claims or performance statistics."},
	{"package", "7 Package and variants", "Show only confirmed package contents and variants; no invented quantities, colors or extras."},
}

func Default(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

// ValidateFields bounds every string, including nested model-generated JSON.
func ValidateFields(value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 1<<20 {
		return ErrInvalid
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return ErrInvalid
	}
	var walk func(any, int) bool
	walk = func(v any, depth int) bool {
		if depth > 32 {
			return false
		}
		switch x := v.(type) {
		case string:
			return utf8.RuneCountInString(x) <= MaxField
		case []any:
			if len(x) > 512 {
				return false
			}
			for _, y := range x {
				if !walk(y, depth+1) {
					return false
				}
			}
		case map[string]any:
			if len(x) > 512 {
				return false
			}
			for k, y := range x {
				if len(k) > 256 || !walk(y, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !walk(v, 0) {
		return ErrInvalid
	}
	return nil
}

func Object(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = strings.TrimSuffix(strings.TrimSpace(text[i+1:]), "```")
		}
	}
	var out map[string]any
	if len(text) > 256<<10 || json.Unmarshal([]byte(text), &out) != nil || out == nil || ValidateFields(out) != nil {
		return nil, ErrInvalid
	}
	return out, nil
}

func Text(system string, value any, jsonMode bool) jobs.TextInput {
	b, _ := json.Marshal(value)
	content, _ := json.Marshal(system)
	user, _ := json.Marshal(string(b))
	input := jobs.TextInput{Messages: []provider.Message{{Role: "system", Content: content}, {Role: "user", Content: user}}}
	if jsonMode {
		input.Options = map[string]json.RawMessage{"response_format": json.RawMessage(`{"type":"json_object"}`)}
	}
	return input
}

func AddVision(input *jobs.TextInput, ids []string) {
	if len(ids) == 0 {
		return
	}
	var text string
	_ = json.Unmarshal(input.Messages[len(input.Messages)-1].Content, &text)
	parts := []any{map[string]any{"type": "text", "text": text}}
	for _, id := range ids[:min(4, len(ids))] {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "picpilot-asset:" + id}})
	}
	input.Messages[len(input.Messages)-1].Content, _ = json.Marshal(parts)
}

func ImagePrompt(kind string, request ImageRequest, facts, copy map[string]any, context []string) (string, error) {
	request.Prompt = CleanPrompt(request.Prompt)
	direction := "Create the requested image."
	for _, slot := range ListingSlots {
		if slot.Type == kind {
			direction = slot.Direction
		}
	}
	if kind == "aplus" {
		direction = "Create an Amazon A+ module with clear visual hierarchy and only verified product claims. Preserve the exact requested canvas; no cropping."
	}
	value := map[string]any{"request": request.Prompt, "style": request.Style, "module": request.Module, "language": request.Language, "size": request.Size, "facts": facts, "copy": copy, "recent_context": context, "intent": request.Intent, "text_mode": request.TextMode, "reference_policy": request.ReferencePolicy}
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	prompt := direction + "\n" + ImagePolicy(request) + "\nPreserve reference order. Background colors are not product colors. Do not invent measurements, certifications, claims, accessories or compatibility. Render only explicitly requested/confirmed text; no watermark, UI, QR code or gibberish. Keep the full composition, no center cropping.\n" + string(b)
	if utf8.RuneCountInString(prompt) > MaxField {
		return "", ErrInvalid
	}
	return prompt, nil
}

const ListingPrompt = `You are a localized Amazon listing writer and compliance reviewer. Use only provided product facts and images. Never infer product color from backgrounds, keywords or scenery; use "As shown" if unconfirmed. Do not invent measurements, materials, certifications, capacity, package contents, compatibility, efficacy or superiority claims. Use the requested language (including Chinese when requested), title/selling-point/description keywords naturally and third-party brand names only as "for" or "compatible with". Produce a title (aim 180-200 characters), exactly five bullets (each <=500 characters), and a description (aim 1000-2500 characters). Review and remove unsupported claims before returning JSON only: {"title":"...","selling_point":["...","...","...","...","..."],"product_description":{"Feature":[],"Specification":"","Material":"","Capacity":"","Color":"","Package Included":""}}. Unknown facts remain empty. Do not claim a compliance certification.`

func ListingCopy(text string, facts map[string]any) (map[string]any, error) {
	obj, err := Object(text)
	if err != nil {
		return nil, err
	}
	title, ok := obj["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return nil, ErrInvalid
	}
	bullets, ok := obj["selling_point"].([]any)
	if !ok {
		bullets, ok = obj["bullets"].([]any)
	}
	if !ok || len(bullets) != 5 {
		return nil, ErrInvalid
	}
	for _, b := range bullets {
		s, ok := b.(string)
		if !ok || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 500 {
			return nil, ErrInvalid
		}
	}
	description := obj["product_description"]
	if description == nil {
		description = obj["description"]
	}
	switch d := description.(type) {
	case string:
		if strings.TrimSpace(d) == "" {
			return nil, ErrInvalid
		}
	case map[string]any:
		if len(d) == 0 {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	briefs := make([]any, 0, len(ListingSlots))
	for _, s := range ListingSlots {
		briefs = append(briefs, map[string]any{"type": s.Type, "name": s.Name, "brief": s.Direction, "generation_size": "1024x1024", "output_size": "1024x1024"})
	}
	return map[string]any{"title": title, "bullets": bullets, "description": description, "confirmed_facts_candidate": facts, "compliance": map[string]any{"passed": false, "warnings": []string{"Human review of product facts and marketplace compliance is required."}, "blocked_claims": []string{"unverified certifications", "invented measurements", "absolute superiority"}}, "image_briefs": briefs, "ai_mode": "queued_listing_copy"}, nil
}

func Facts(product, brand, country, language string, original, confirmed map[string]any, hasMain bool) map[string]any {
	out := map[string]any{"product_name": product, "brand": brand, "target_country": country, "language": language}
	for _, key := range []string{"description", "category", "material", "dimensions", "keywords", "title_keywords", "selling_point_keywords", "description_keywords"} {
		if v, ok := original[key]; ok {
			out[key] = v
		}
	}
	for k, v := range confirmed {
		out[k] = v
	}
	out["main_image_uploaded"] = hasMain
	if hasMain {
		out["color_hints"] = []string{}
		out["color_policy"] = "Match the product reference, not the background or keyword colors."
	}
	return out
}

// DecodeAliases accepts the legacy camelCase spellings without silently accepting typos.
func DecodeAliases(data []byte, dst any) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return ErrInvalid
	}
	aliases := map[string]string{"fallbackModel": "fallback_model", "productName": "product_name", "visualPrompt": "visual_prompt", "titleKeywords": "title_keywords", "sellingPointKeywords": "selling_point_keywords", "descriptionKeywords": "description_keywords", "includeAplus": "include_aplus", "aplusModule": "aplus_module", "aplusSize": "aplus_size", "aplusPrompt": "aplus_prompt", "referenceUrl": "reference_url", "referenceUrls": "reference_urls", "referenceAssetIDs": "reference_asset_ids", "textMode": "text_mode", "referencePolicy": "reference_policy", "templateId": "template_id", "chatId": "chat_id", "confirmedFacts": "confirmed_facts", "requestBody": "request_body", "pageContext": "page_context", "imageType": "image_type", "referenceText": "reference_text"}
	for k, v := range fields {
		if target, ok := aliases[k]; ok {
			if _, exists := fields[target]; exists {
				return ErrInvalid
			}
			delete(fields, k)
			fields[target] = v
		}
	}
	b, _ := json.Marshal(fields)
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		return ErrInvalid
	}
	return nil
}

func String(v any) string        { s, _ := v.(string); return s }
func JSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
