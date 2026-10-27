package video

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var modelID = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
var taskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// ResolveModel maps public catalog codes or native provider model IDs without
// choosing a provider. An explicit native HFSY model must match its resolution.
func ResolveModel(provider Provider, model, resolution string) (string, error) {
	model = strings.TrimSpace(model)
	resolution = normalizeResolution(model, resolution)
	invalid := func() (string, error) { return "", failure(InvalidRequest, "validate", "model_or_resolution") }
	switch provider {
	case Gemini:
		if (model == "" || model == "gemini-omni" || model == "gemini-omni-flash-preview") && resolution == "720p" {
			return "gemini-omni-flash-preview", nil
		}
	case Agnes:
		if (model == "" || model == "agnes-v2" || model == "agnes-video-v2.0") && resolution == "720p" {
			return "agnes-video-v2.0", nil
		}
	case Seedance:
		switch model {
		case "seedance-mini":
			model = "doubao-seedance-2.0-mini"
		case "", "seedance-fast":
			model = "doubao-seedance-2.0-fast"
		case "seedance-2":
			model = "doubao-seedance-2.0"
		}
		switch model {
		case "doubao-seedance-2.0-mini", "doubao-seedance-2.0-fast":
			if oneOf(resolution, "480p", "720p") {
				return model, nil
			}
		case "doubao-seedance-2.0":
			if oneOf(resolution, "480p", "720p", "1080p", "4K") {
				return model, nil
			}
		}
	case HFSY:
		if !oneOf(resolution, "480p", "720p") {
			return invalid()
		}
		switch model {
		case "seedance-mini":
			model = "sd-2.0-mini"
		case "", "seedance-fast":
			model = "sd-2.0-fast"
			if resolution == "480p" {
				model = "sd-2.0-fast-480"
			}
		case "seedance-2":
			model = "sd-2.0"
			if resolution == "480p" {
				model = "sd-2.0-480"
			}
		}
		if model == "sd-2.0-mini" ||
			(oneOf(model, "sd-2.0-fast-480", "sd-2.0-480") && resolution == "480p") ||
			(oneOf(model, "sd-2.0-fast", "sd-2.0") && resolution == "720p") {
			return model, nil
		}
	}
	return invalid()
}

func normalizeResolution(model, resolution string) string {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	if resolution == "4k" {
		return "4K"
	}
	if resolution == "" {
		if oneOf(model, "sd-2.0-fast-480", "sd-2.0-480") {
			return "480p"
		}
		return "720p"
	}
	return resolution
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func (g *Gateway) requestBody(r Request) (map[string]any, string, error) {
	bad := func(code string) (map[string]any, string, error) {
		return nil, "", failure(InvalidRequest, "submit", code)
	}
	r.Prompt = strings.TrimSpace(r.Prompt)
	if !utf8.ValidString(r.Prompt) || len(r.Prompt) > 16000 {
		return bad("prompt_length")
	}
	if n := len(utf16.Encode([]rune(r.Prompt))); n < 4 || n > 4000 {
		return bad("prompt_length")
	}
	r.Model = strings.TrimSpace(r.Model)
	r.Resolution = normalizeResolution(r.Model, r.Resolution)
	model, err := ResolveModel(g.config.Provider, r.Model, r.Resolution)
	if g.config.Provider == Agnes && g.config.AgnesModel != "" &&
		oneOf(r.Model, "", "agnes-v2", g.config.AgnesModel) && r.Resolution == "720p" {
		model, err = g.config.AgnesModel, nil
	}
	if err != nil {
		return bad("model_or_resolution")
	}
	if r.DurationSeconds == 0 {
		r.DurationSeconds = 10
	}
	if r.AspectRatio == "" {
		r.AspectRatio = "9:16"
	}
	maxReferences := 4
	switch g.config.Provider {
	case Gemini:
		maxReferences = 3
		if !oneOf(r.AspectRatio, "9:16", "16:9") {
			return bad("aspect_ratio")
		}
	case Seedance:
		if !oneOf(r.AspectRatio, "9:16", "16:9", "1:1", "3:4", "4:3", "adaptive") {
			return bad("aspect_ratio")
		}
	case HFSY:
		if !oneOf(r.AspectRatio, "9:16", "16:9", "1:1") {
			return bad("aspect_ratio")
		}
	case Agnes:
		maxReferences = 1
		if !oneOf(r.AspectRatio, "9:16", "16:9", "1:1", "3:4", "4:3") {
			return bad("aspect_ratio")
		}
	}
	durationOK := r.DurationSeconds >= 3 && r.DurationSeconds <= 10
	if g.config.Provider == Seedance {
		durationOK = durationOK || r.DurationSeconds == 15
	} else if g.config.Provider == HFSY {
		durationOK = r.DurationSeconds >= 4 && r.DurationSeconds <= 15
	}
	if !durationOK {
		return bad("duration")
	}
	if len(r.SourceURLs) < 1 || len(r.SourceURLs) > maxReferences {
		return bad("reference_count")
	}
	images := make([]string, 0, len(r.SourceURLs))
	for _, raw := range r.SourceURLs {
		u, err := g.sourceURL(raw)
		if err != nil {
			return bad("reference_url")
		}
		if g.config.Provider == Gemini && !oneOf(imageExtension(u.Path), ".jpg", ".jpeg", ".png") {
			return bad("reference_format")
		}
		images = append(images, u.String())
	}
	if g.config.Provider != Seedance && (r.GenerateAudio != nil || r.Seed != nil) {
		return bad("unsupported_audio_or_seed")
	}
	if r.Seed != nil && (*r.Seed < 0 || *r.Seed > 2147483647) {
		return bad("seed")
	}
	if g.config.Provider != HFSY && r.Watermark != nil {
		return bad("unsupported_watermark")
	}
	body := map[string]any{"model": model}
	switch g.config.Provider {
	case Gemini, Seedance:
		body["prompt"] = compilePrompt(r.Prompt, g.config.Provider, len(images))
		body["image_urls"] = images
		if g.config.Provider == Seedance {
			body["duration"], body["resolution"], body["size"] = r.DurationSeconds, r.Resolution, r.AspectRatio
			body["generate_audio"] = r.GenerateAudio == nil || *r.GenerateAudio
			if r.Seed != nil {
				body["seed"] = *r.Seed
			}
		} else {
			body["aspect_ratio"] = r.AspectRatio
		}
		if g.config.WebhookURL != "" {
			body["webhook"] = g.config.WebhookURL
		}
	case HFSY:
		body["prompt"] = compilePrompt(r.Prompt, HFSY, len(images))
		body["images"], body["ratio"], body["duration"] = images, r.AspectRatio, r.DurationSeconds
		body["watermark"] = r.Watermark != nil && *r.Watermark
		body["size"] = "large"
		if model == "sd-2.0-mini" && r.Resolution == "480p" {
			body["size"] = "small"
		}
	case Agnes:
		dimensions := map[string][2]int{
			"9:16": {720, 1280}, "16:9": {1280, 720}, "1:1": {720, 720},
			"3:4": {720, 960}, "4:3": {960, 720},
		}[r.AspectRatio]
		body["prompt"], body["negative_prompt"] = r.Prompt+agnesContinuity, agnesNegativePrompt
		body["image"] = images[0]
		body["width"], body["height"] = dimensions[0], dimensions[1]
		body["frame_rate"], body["num_frames"] = 24, r.DurationSeconds*24+1
	}
	return body, model, nil
}

func imageExtension(path string) string {
	index := strings.LastIndexByte(path, '.')
	if index == -1 {
		return ""
	}
	return strings.ToLower(path[index:])
}

func (g *Gateway) sourceURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidRequest
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if !u.IsAbs() {
		if g.config.PublicBaseURL == "" || u.Host != "" || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
			return nil, ErrInvalidRequest
		}
		raw = strings.TrimRight(g.config.PublicBaseURL, "/") + "/" + strings.TrimLeft(raw, "/")
	}
	return parseHTTPURL(raw, g.config.AllowHTTP)
}
