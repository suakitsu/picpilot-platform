package commerce

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/creative"
)

// Java accepted both snake_case and camelCase DTO fields, including nested
// product/catalog DTOs. Free-form template dictionaries keep their own keys.
func Decode(data []byte, dst any) error {
	var input any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !json.Valid(data) || decoder.Decode(&input) != nil {
		return creative.ErrInvalid
	}
	if _, ok := input.(map[string]any); !ok {
		return creative.ErrInvalid
	}
	var validStrings func(any) bool
	validStrings = func(value any) bool {
		switch v := value.(type) {
		case string:
			return !strings.ContainsRune(v, 0)
		case []any:
			for _, x := range v {
				if !validStrings(x) {
					return false
				}
			}
		case map[string]any:
			for k, x := range v {
				if strings.ContainsRune(k, 0) || !validStrings(x) {
					return false
				}
			}
		}
		return true
	}
	if !validStrings(input) {
		return creative.ErrInvalid
	}
	aliases := map[string]string{
		"productInfo": "product_info", "productName": "product_name", "sellingPoints": "selling_points", "priceText": "price_text", "promoText": "promo_text",
		"selectedSlots": "selected_slots", "referenceImageIds": "reference_image_ids", "referenceImageIDs": "reference_image_ids", "referenceAssetIds": "reference_asset_ids", "referenceAssetIDs": "reference_asset_ids", "referenceUrl": "reference_url", "overlayOptions": "overlay_options", "targetLanguage": "target_language", "referenceText": "reference_text",
		"defaultResolution": "default_resolution", "defaultSize": "default_size", "defaultSlots": "default_slots", "stylePrompt": "style_prompt", "overlayLanguage": "overlay_language", "sortOrder": "sort_order",
		"promptTemplate": "prompt_template", "negativePrompt": "negative_prompt", "overlayTemplate": "overlay_template", "providerParams": "provider_params", "costRule": "cost_rule", "updatedAt": "updated_at",
	}
	var normalize func(any) (any, error)
	normalize = func(value any) (any, error) {
		switch v := value.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, val := range v {
				if alias, ok := aliases[k]; ok {
					k = alias
				}
				if _, exists := out[k]; exists {
					return nil, creative.ErrInvalid
				}
				if k != "overlay_template" && k != "overlay_options" && k != "provider_params" && k != "cost_rule" {
					var err error
					val, err = normalize(val)
					if err != nil {
						return nil, err
					}
				}
				out[k] = val
			}
			return out, nil
		case []any:
			out := []any{}
			for _, val := range v {
				x, err := normalize(val)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		default:
			return value, nil
		}
	}
	normalized, err := normalize(input)
	if err != nil {
		return err
	}
	decoder = json.NewDecoder(bytes.NewReader(creative.JSON(normalized)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || creative.ValidateFields(dst) != nil {
		return creative.ErrInvalid
	}
	return nil
}
