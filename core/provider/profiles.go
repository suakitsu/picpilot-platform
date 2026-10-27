package provider

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func imageSettings(p Provider, size string, w, h int) (string, string, int) {
	model, limit := p.Model, 10
	family := strings.EqualFold(model, "gpt-image-2") || strings.EqualFold(model, "gpt-image-2pro")
	if p.ImageProfile != OpenAI && family {
		model = "gpt-image-2"
	}
	switch p.ImageProfile {
	case Hfsy:
		if family && max(w, h) >= 1800 {
			model, limit = "gpt-image-2pro", 4
		}
	case Yunfei:
		model, limit = "gpt-image-2", 1
	case Rua:
		model, limit = "gpt-image-2", 4
		if w == h {
			if w >= 2800 {
				size = "2880x2880"
			} else {
				size = "1600x1600"
			}
		}
	case XinZhiZhu:
		limit = 5
		if w == h {
			switch {
			case w >= 2800:
				size = "2880x2880"
			case w >= 1800:
				size = "2048x2048"
			default:
				size = "1600x1600"
			}
		}
	case GeminiProxy:
		limit = 4
	case Aishuch:
		if mapped, ok := aishuchSizes[size]; ok {
			size = mapped
		} else {
			size = aspectRatio(size)
			if size == "7:3" {
				size = "21:9"
			}
		}
	}
	return model, size, limit
}

func imageFields(p Provider, r ImageRequest, size string, edit bool) map[string]any {
	fields := map[string]any{"model": p.Model, "prompt": r.Prompt, "size": size, "n": 1}
	switch p.ImageProfile {
	case Rua:
		fields["quality"], fields["output_format"], fields["moderation"] = "auto", "png", "low"
	case XinZhiZhu:
		fields["quality"], fields["output_format"] = "auto", "png"
		if edit {
			fields["model"], fields["response_format"] = "gpt-image-2", "b64_json"
		}
	case Yunfei:
		fields["response_format"] = r.ResponseFormat
		if edit {
			fields["response_format"] = "b64_json"
			delete(fields, "n")
		}
	case Aishuch:
		fields["resolution"] = strings.ToLower(resolution(r.Size))
	default:
		fields["response_format"] = r.ResponseFormat
	}
	if p.ImageProfile == OpenAI {
		if nativeGPTImage(p.Model) {
			delete(fields, "response_format")
		}
		for key, value := range map[string]string{"quality": r.Quality, "output_format": r.OutputFormat, "background": r.Background, "moderation": r.Moderation} {
			if value != "" {
				fields[key] = value
			}
		}
	}
	return fields
}

func nativeGPTImage(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "gpt-image-") || strings.EqualFold(model, "chatgpt-image-latest")
}

func imageJSON(p Provider, r ImageRequest, size string, refs []loadedReference) map[string]any {
	if p.ImageProfile == GeminiProxy || p.ImageProfile == GoogleGemini {
		parts := []any{map[string]any{"text": r.Prompt}}
		for _, ref := range refs {
			if p.ImageProfile == GeminiProxy {
				parts = append(parts, map[string]any{"fileData": map[string]any{"fileUri": ref.url}})
			} else {
				parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": ref.image.MIMEType, "data": base64.StdEncoding.EncodeToString(ref.image.Bytes)}})
			}
		}
		imageConfig := map[string]any{"aspectRatio": aspectRatio(size)}
		config := map[string]any{"imageConfig": imageConfig}
		if p.ImageProfile == GeminiProxy {
			imageConfig["resolution"] = resolution(size)
		} else {
			if imageConfig["aspectRatio"] == "7:3" {
				imageConfig["aspectRatio"] = "21:9"
			}
			imageConfig["imageSize"] = resolution(size)
			config["responseModalities"] = []string{"TEXT", "IMAGE"}
		}
		return map[string]any{"contents": []any{map[string]any{"parts": parts}}, "generationConfig": config}
	}
	body := imageFields(p, r, size, false)
	if len(refs) > 0 {
		urls := make([]string, len(refs))
		for i, ref := range refs {
			urls[i] = ref.url
		}
		field := "reference_images"
		if p.ImageProfile == Aishuch {
			field = "image_urls"
		}
		body[field] = urls
	}
	return body
}

func aspectRatio(size string) string {
	w, h, ok := parseSize(size)
	if !ok {
		return "1:1"
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	return fmt.Sprintf("%d:%d", w/a, h/a)
}

func resolution(size string) string {
	w, h, _ := parseSize(size)
	if max(w, h) >= 2800 {
		return "4K"
	}
	if max(w, h) >= 1800 {
		return "2K"
	}
	return "1K"
}

// Exact pixel mappings from AiImageClient, not nearest-ratio/crop heuristics.
var aishuchSizes = map[string]string{
	"1024x1024": "1024x1024", "2048x2048": "2048x2048", "2880x2880": "2880x2880", "4096x4096": "2880x2880",
	"1040x832": "1280x1024", "2080x1664": "2560x2048", "3200x2560": "3216x2576",
	"720x1280": "864x1536", "1152x2048": "1152x2048", "2160x3840": "2160x3840",
	"1280x720": "1536x864", "2048x1152": "2048x1152", "3840x2160": "3840x2160", "4096x2304": "3840x2160",
	"1024x768": "1024x768", "2048x1536": "2048x1536", "3264x2448": "3312x2480", "4096x3072": "3312x2480",
	"1008x672": "1536x1024", "2016x1344": "2048x1360", "3504x2336": "3520x2336",
	"832x1040": "1024x1280", "1664x2080": "2048x2560", "2560x3200": "2576x3216",
	"768x1024": "768x1024", "1536x2048": "1536x2048", "2448x3264": "2480x3312", "3072x4096": "2480x3312",
	"672x1008": "1024x1536", "1344x2016": "1360x2048", "2336x3504": "2336x3520",
	"1344x576": "2016x864", "2016x864": "2688x1152", "3696x1584": "3840x1648",
}
