package video

import (
	"encoding/json"
	"math"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func at(root any, path string) any {
	current := root
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			current = node[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			current = node[i]
		default:
			return nil
		}
	}
	return current
}

func textAt(root any, path string) string {
	switch value := at(root, path).(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	}
	return ""
}

func firstText(root any, paths ...string) string {
	for _, path := range paths {
		if value := textAt(root, path); value != "" {
			return value
		}
	}
	return ""
}

func extractTaskID(root any, provider Provider) string {
	switch provider {
	case Agnes:
		return firstText(root, "video_id", "data.video_id", "id", "data.id")
	case HFSY:
		return firstText(root, "task_id", "data.task_id", "id", "data.id", "result.task_id", "data.result.task_id")
	default:
		return firstText(root, "data.0.task_id", "data.0.id", "task_id", "data.task_id", "id", "data.id")
	}
}

// Only explicit non-admission is a rejection. A server error, an opaque error
// envelope, or an otherwise successful response without an ID is ambiguous.
func envelopeFailure(root map[string]any) (rejected, uncertain bool) {
	for _, path := range []string{"code", "data.code", "data.0.code"} {
		code := strings.ToLower(textAt(root, path))
		if n, err := strconv.Atoi(code); err == nil {
			if n >= 500 || n == 408 {
				return false, true
			}
			if n >= 400 && n < 500 {
				return true, false
			}
			if n != 0 && (n < 200 || n >= 300) {
				uncertain = true
			}
		} else if code != "" && !oneOf(code, "ok", "success", "succeeded") {
			uncertain = true
		}
	}
	for _, path := range []string{"success", "data.success", "data.0.success"} {
		if value, ok := at(root, path).(bool); ok && !value {
			rejected = true
		}
	}
	for _, path := range []string{"status", "data.status", "data.0.status"} {
		if normalizeStatus(textAt(root, path)) == Failed {
			rejected = true
		}
	}
	for _, path := range []string{"error", "data.error", "data.0.error"} {
		if value := at(root, path); value != nil && value != "" && value != false {
			uncertain = true
		}
	}
	return rejected, uncertain
}

func normalizeStatus(raw string) Status {
	s := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(s, "fail") || strings.Contains(s, "error") || strings.Contains(s, "cancel") || strings.Contains(s, "unsuccess") {
		return Failed
	}
	if strings.Contains(s, "queue") || strings.Contains(s, "pending") || strings.Contains(s, "submit") {
		return Submitted
	}
	if !strings.Contains(s, "incomplete") && !strings.Contains(s, "not_complete") &&
		(strings.Contains(s, "success") || strings.Contains(s, "complete") || strings.Contains(s, "finish") || s == "succeeded" || s == "done") {
		return Completed
	}
	return Generating
}

func (g *Gateway) parsePoll(root map[string]any, model string) (PollResult, error) {
	bad := func(code string) (PollResult, error) { return PollResult{}, failure(ProtocolError, "parse", code) }
	if data, ok := root["data"].([]any); ok {
		if len(data) != 1 {
			return bad("unexpected_task_count")
		}
		if _, ok := data[0].(map[string]any); !ok {
			return bad("invalid_task")
		}
		copyRoot := make(map[string]any, len(root))
		for k, v := range root {
			copyRoot[k] = v
		}
		copyRoot["data"] = data[0]
		root = copyRoot
	}
	statusText := firstText(root, "status", "data.status", "state", "data.state")
	progress := firstInt(root, "progress", "data.progress")
	if g.config.Provider == Gemini || g.config.Provider == Seedance {
		statusText = firstText(root, "data.status", "status", "data.state", "state")
		progress = firstInt(root, "data.progress", "progress")
	}
	status := normalizeStatus(statusText)
	// HTTP success can still contain an API-level error. Never interpret it as
	// completion; only explicit task failure is a terminal task result.
	if rejected, uncertain := envelopeFailure(root); status != Failed && (rejected || uncertain) {
		return bad("upstream_error")
	}
	result := PollResult{Status: status, Progress: 10, Metadata: compactMetadata(root, g.config.Provider, model)}
	if status == Failed {
		result.Progress, result.Error = 100, "upstream video generation failed"
		return result, nil
	}
	if status != Completed {
		if progress != nil {
			result.Progress = max(10, min(95, *progress))
		}
		return result, nil
	}
	var resultURL string
	switch g.config.Provider {
	case Gemini, Seedance:
		resultURL = firstText(root,
			"data.result.videos.0.url.0", "data.result.videos.0.url",
			"result.videos.0.url.0", "result.videos.0.url",
			"data.result.video_url", "data.result.url", "data.video_url", "data.url",
			"result.video_url", "result.url", "video_url", "url")
		result.Seconds = decimalAt(root, "data.seconds", "seconds")
	case HFSY:
		resultURL = firstText(root, "result_url", "data.result_url", "result.url", "data.result.url", "video_url", "data.video_url", "url", "data.url")
		result.Width = positiveInt(root, "width", "data.width", "result.width", "data.result.width")
		result.Height = positiveInt(root, "height", "data.height", "result.height", "data.result.height")
		result.Seconds = decimalAt(root, "seconds", "data.seconds", "duration", "data.duration")
	case Agnes:
		resultURL = firstText(root, "metadata.url", "data.metadata.url", "url", "data.url")
		result.Width = positiveInt(root, "metadata.size_mapping.width", "data.metadata.size_mapping.width", "width", "data.width")
		result.Height = positiveInt(root, "metadata.size_mapping.height", "data.metadata.size_mapping.height", "height", "data.height")
		w, h := parseSize(firstText(root, "size", "data.size"))
		if result.Width == nil && w > 0 {
			result.Width = &w
		}
		if result.Height == nil && h > 0 {
			result.Height = &h
		}
		result.Seconds = decimalAt(root, "seconds", "data.seconds")
	}
	u, err := g.resultURL(resultURL)
	if err != nil {
		return bad("missing_or_invalid_result_url")
	}
	result.Progress, result.ResultURL = 100, u.String()
	return result, nil
}

func firstInt(root any, paths ...string) *int {
	for _, path := range paths {
		if number, ok := at(root, path).(json.Number); ok {
			if f, err := number.Float64(); err == nil && f >= math.MinInt32 && f <= math.MaxInt32 {
				n := int(f)
				return &n
			}
		}
		s := strings.TrimSpace(strings.ReplaceAll(textAt(root, path), "%", ""))
		if value, err := strconv.ParseInt(s, 10, 32); err == nil {
			n := int(value)
			return &n
		}
	}
	return nil
}

func positiveInt(root any, paths ...string) *int {
	value := firstInt(root, paths...)
	if value == nil || *value < 1 || *value > 65536 {
		return nil
	}
	return value
}

var decimalPattern = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,18})?([eE][+-]?[0-9]{1,2})?$`)

func decimalAt(root any, paths ...string) string {
	for _, path := range paths {
		value := textAt(root, path)
		if decimalPattern.MatchString(value) {
			return value
		}
	}
	return ""
}

func parseSize(raw string) (int, int) {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(raw, " ", "")), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, we := strconv.Atoi(parts[0])
	h, he := strconv.Atoi(parts[1])
	if we != nil || he != nil || w < 1 || h < 1 || w > 65536 || h > 65536 {
		return 0, 0
	}
	return w, h
}

func compactMetadata(root any, provider Provider, model string) map[string]string {
	meta := make(map[string]string)
	put := func(key, value string) {
		if value != "" {
			meta[key] = value
		}
	}
	if provider == HFSY {
		rawStatus := firstText(root, "status", "data.status", "state", "data.state")
		if oneOf(strings.ToLower(rawStatus), "success", "succeeded", "completed", "complete", "finished", "done", "failed", "failure", "error", "cancelled", "canceled", "queued", "pending", "submitted", "running", "generating", "processing") {
			meta["upstream_status"] = rawStatus
		} else {
			meta["upstream_status"] = string(normalizeStatus(rawStatus))
		}
		put("upstream_quota", decimalAt(root, "quota", "data.quota"))
		cost := map[string]string{"sd-2.0-mini": "1.200000", "sd-2.0-fast-480": "2.000000", "sd-2.0-fast": "3.000000", "sd-2.0-480": "3.000000", "sd-2.0": "4.000000"}[model]
		if cost != "" {
			meta["actual_cost"], meta["upstream_cost"] = cost, cost
			meta["upstream_cost_unit"], meta["cost_source"] = "CNY_PER_CALL", "hfsy_public_fixed_price"
		}
		return meta
	}
	if w, h := parseSize(firstText(root, "size", "data.size")); w > 0 {
		meta["size"] = strconv.Itoa(w) + "x" + strconv.Itoa(h)
	}
	put("seconds", decimalAt(root, "seconds", "data.seconds"))
	resolution := firstText(root, "metadata.size_mapping.resolution", "data.metadata.size_mapping.resolution")
	if oneOf(resolution, "480p", "720p", "1080p", "4k", "4K") {
		meta["mapped_resolution"] = resolution
	}
	for key, paths := range map[string][]string{
		"upstream_actual_time_seconds": {"actual_time", "data.actual_time"},
		"upstream_completed_epoch":     {"completed", "data.completed"},
		"upstream_result_expires_at":   {"result.videos.0.expires_at", "data.result.videos.0.expires_at"},
	} {
		put(key, decimalAt(root, paths...))
	}
	for key, paths := range map[string][]string{
		"mapped_width":  {"metadata.size_mapping.width", "data.metadata.size_mapping.width"},
		"mapped_height": {"metadata.size_mapping.height", "data.metadata.size_mapping.height"},
	} {
		if n := positiveInt(root, paths...); n != nil {
			meta[key] = strconv.Itoa(*n)
		}
	}
	if provider == Agnes {
		put("actual_cost", decimalAt(root, "metadata.cost", "data.metadata.cost", "cost", "data.cost"))
		return meta
	}
	cost := decimalAt(root, "data.credits_cost", "credits_cost")
	rate, unit, source := "0.679110", "APIMART_CREDIT", "upstream_credits_cost"
	if cost != "" {
		meta["upstream_credits_cost"] = cost
	} else {
		cost = decimalAt(root, "data.cost", "data.metadata.cost", "metadata.cost", "cost")
		rate, unit, source = "6.791100", "USD", "upstream_cost"
	}
	if cost != "" {
		amount, ok := new(big.Rat).SetString(cost)
		conversion, _ := new(big.Rat).SetString(rate)
		if ok {
			meta["actual_cost"] = new(big.Rat).Mul(amount, conversion).FloatString(6)
			meta["upstream_cost"], meta["cost_conversion_rate"] = cost, rate
			meta["upstream_cost_unit"], meta["cost_source"] = unit, source
		}
	}
	return meta
}

func (g *Gateway) resultURL(raw string) (*url.URL, error) {
	u, err := parseHTTPURL(raw, g.config.AllowHTTP)
	if err != nil {
		return nil, ErrProtocol
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil || strings.Contains(raw, g.config.APIKey) || strings.Contains(decoded, g.config.APIKey) {
		return nil, ErrProtocol
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, ErrProtocol
	}
	for key := range query {
		if oneOf(strings.ToLower(key), "api_key", "apikey", "authorization", "prompt", "negative_prompt", "password", "secret", "access_token") {
			return nil, ErrProtocol
		}
	}
	return u, nil
}
