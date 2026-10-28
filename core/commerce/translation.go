package commerce

import (
	"fmt"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
)

type Language struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"native_name"`
	PromptName string `json:"prompt_name"`
}

func Languages() []Language {
	return []Language{
		{"ms-MY", "\u9a6c\u6765\u8bed", "Bahasa Melayu", "Malaysian Malay (Bahasa Melayu, not Indonesian)"},
		{"en-US", "\u82f1\u8bed", "English", "English"},
		{"id-ID", "\u5370\u5c3c\u8bed", "Bahasa Indonesia", "Indonesian"},
		{"th-TH", "\u6cf0\u8bed", "\u0e44\u0e17\u0e22", "Thai"},
		{"vi-VN", "\u8d8a\u5357\u8bed", "Ti\u1ebfng Vi\u1ec7t", "Vietnamese"},
		{"fil-PH", "\u83f2\u5f8b\u5bbe\u8bed", "Filipino", "Filipino"},
		{"es-ES", "\u897f\u73ed\u7259\u8bed", "Espa\u00f1ol", "Spanish"},
		{"pt-BR", "\u8461\u8404\u7259\u8bed", "Portugu\u00eas (Brasil)", "Brazilian Portuguese"},
		{"fr-FR", "\u6cd5\u8bed", "Fran\u00e7ais", "French"},
		{"de-DE", "\u5fb7\u8bed", "Deutsch", "German"},
		{"ja-JP", "\u65e5\u8bed", "\u65e5\u672c\u8a9e", "Japanese"},
		{"ko-KR", "\u97e9\u8bed", "\ud55c\uad6d\uc5b4", "Korean"},
		{"ar-SA", "\u963f\u62c9\u4f2f\u8bed", "\u0627\u0644\u0639\u0631\u0628\u064a\u0629", "Arabic"},
		{"zh-TW", "\u7e41\u4f53\u4e2d\u6587", "\u7e41\u9ad4\u4e2d\u6587", "Traditional Chinese"},
	}
}

type TranslationRequest struct {
	TargetLanguage    string   `json:"target_language"`
	Resolution        string   `json:"resolution"`
	Style             string   `json:"style"`
	ReferenceImageIDs []string `json:"reference_image_ids"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
}

func (in *TranslationRequest) Normalize() (Language, error) {
	in.TargetLanguage = strings.TrimSpace(in.TargetLanguage)
	in.Resolution = Resolution(in.Resolution, "1K")
	in.Style = creative.Default(in.Style, "preserve")
	if creative.ValidateFields(in) != nil || len([]rune(in.Style)) > 1000 {
		return Language{}, creative.ErrInvalid
	}
	for _, l := range Languages() {
		if strings.EqualFold(in.TargetLanguage, l.Code) {
			in.TargetLanguage = l.Code
			return l, nil
		}
	}
	return Language{}, creative.ErrInvalid
}

func TranslationImage(in TranslationRequest, language Language, id, filename string, width, height, index int, cost int64) (PlannedImage, error) {
	size, err := AspectSize(width, height, in.Resolution)
	if err != nil {
		return PlannedImage{}, err
	}
	prompt := fmt.Sprintf(`Edit this exact source image by translating its existing readable text to %s (%s). Detect the source language automatically.
Only replace existing text pixels. Preserve all non-text content, product identity, people, layout, framing, crop, colors, lighting and textures. Do not redesign, center-crop, stretch or upscale the source to fill a canvas. Keep its original aspect ratio and composition.
Preserve brands, logos, platform names, proper names, prices, quantities, numbers, units, SKUs and product codes exactly. Do not invent claims, captions, QR codes, watermarks, badges or decorations. Unreadable text must stay unchanged; never guess. If there is no readable text, return a visually identical image.
Fit translated typography inside the original text regions, preserving alignment, weight and visual hierarchy; do not cover nearby objects. Translation style: %s. Style affects text and phrasing only, never non-text pixels.`, language.PromptName, language.Code, in.Style)
	name := fmt.Sprintf("%d - %s", index+1, filename)
	p := Payload{Payload: jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: prompt, Size: size, ReferenceAssetIDs: []string{id}, OutputFormat: "png"}}, Snapshot: map[string]any{"taskKind": "image_translation", "imageName": name, "translationSourceImageId": id, "sourceImageIndex": index, "sourceFilename": filename, "sourceWidth": width, "sourceHeight": height, "sourceLanguage": "auto", "targetLanguageCode": language.Code, "targetLanguageName": language.Name, "style": in.Style, "resolution": in.Resolution, "generationSize": size, "outputSize": size, "intent": "translate_image_text", "text_mode": "replace_existing_text_only", "reference_policy": "preserve_non_text_exactly"}}
	return PlannedImage{fmt.Sprintf("image_translation_%02d", index+1), name, cost, p}, nil
}
