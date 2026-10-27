package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NewGateway validates configuration without making any network requests.
func NewGateway(c Config) (*Gateway, error) {
	bad := func(code string) (*Gateway, error) { return nil, failure(InvalidRequest, "configure", code) }
	if c.Client == nil || c.Client.Transport == nil {
		return bad("secure_transport_required")
	}
	if !oneOf(string(c.Provider), string(Gemini), string(Seedance), string(HFSY), string(Agnes)) {
		return bad("provider")
	}
	if len(c.APIKey) == 0 || len(c.APIKey) > 8192 {
		return bad("credential")
	}
	for _, ch := range c.APIKey {
		if ch < 33 || ch > 126 {
			return bad("credential")
		}
	}
	if c.BaseURL == "" {
		switch c.Provider {
		case Gemini, Seedance:
			c.BaseURL = "https://api.apimart.ai"
		case HFSY:
			c.BaseURL = "https://www.hfsyapi.cn"
		case Agnes:
			c.BaseURL = "https://apihub.agnes-ai.com"
		}
	}
	base, err := parseHTTPURL(c.BaseURL, c.AllowHTTP)
	if err != nil || base.RawQuery != "" || base.ForceQuery {
		return bad("base_url")
	}
	if c.PublicBaseURL != "" {
		u, err := parseHTTPURL(c.PublicBaseURL, c.AllowHTTP)
		if err != nil || u.RawQuery != "" || u.ForceQuery {
			return bad("public_base_url")
		}
	}
	if c.WebhookURL != "" {
		if c.Provider != Gemini && c.Provider != Seedance {
			return bad("unsupported_webhook")
		}
		if _, err := parseHTTPURL(c.WebhookURL, c.AllowHTTP); err != nil {
			return bad("webhook_url")
		}
	}
	if c.AgnesModel != "" && (c.Provider != Agnes || !modelID.MatchString(c.AgnesModel)) {
		return bad("agnes_model")
	}
	for _, bound := range []struct {
		value *time.Duration
		max   time.Duration
	}{
		{&c.SubmitTimeout, 45 * time.Second}, {&c.PollTimeout, 30 * time.Second}, {&c.FetchTimeout, 5 * time.Minute},
	} {
		if *bound.value == 0 {
			*bound.value = bound.max
		}
		if *bound.value < 0 || *bound.value > bound.max {
			return bad("timeout")
		}
	}
	if c.MaxResponseBytes == 0 {
		c.MaxResponseBytes = 1 << 20
	}
	if c.MaxResponseBytes < 1 || c.MaxResponseBytes > 4<<20 {
		return bad("response_limit")
	}
	if c.MaxResultBytes == 0 {
		c.MaxResultBytes = 500 << 20
	}
	if c.MaxResultBytes < 1 || c.MaxResultBytes > 500<<20 {
		return bad("result_limit")
	}
	client := *c.Client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Client = nil
	return &Gateway{config: c, client: client, base: base}, nil
}

func parseHTTPURL(raw string, allowHTTP bool) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\t\\") {
		return nil, ErrInvalidRequest
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, ErrInvalidRequest
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return nil, ErrInvalidRequest
	}
	return u, nil
}

func (g *Gateway) endpoint(path string, query url.Values) string {
	u := *g.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return u.String()
}

func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return failure(InvalidRequest, op, "context_required")
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(InvalidRequest, op, err)
	}
	return nil
}

func (g *Gateway) doJSON(ctx context.Context, method, path string, query url.Values, body map[string]any) (map[string]any, error) {
	op, timeout := "poll", g.config.PollTimeout
	kind := TransportError
	if method == http.MethodPost {
		op, timeout, kind = "submit", g.config.SubmitTimeout, Ambiguous
	}
	if err := checkContext(ctx, op); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil || int64(len(payload)) > g.config.MaxResponseBytes {
			return nil, failure(InvalidRequest, op, "request_limit")
		}
		// Hide bytes.Reader from NewRequest: GetBody must stay nil so Transport
		// cannot replay a paid POST on a stale connection or a 307/308 redirect.
		reader = io.NopCloser(bytes.NewReader(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, g.endpoint(path, query), reader)
	if err != nil {
		return nil, failure(InvalidRequest, op, "request")
	}
	req.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(payload))
	}
	response, err := g.client.Do(req)
	if err != nil {
		return nil, contextFailure(kind, op, err)
	}
	defer response.Body.Close()
	data, readErr := readBounded(response.Body, g.config.MaxResponseBytes)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		e := failure(kind, op, "http_status")
		e.HTTPStatus, e.RetryAfter = response.StatusCode, retryAfter(response.Header.Get("Retry-After"))
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != 408 {
			e.Kind = Rejected
		}
		// The Java worker recognizes this exact explicit non-admission signal.
		// Generic Agnes 503/queue text remains ambiguous; nothing retries here.
		if op == "submit" && g.config.Provider == Agnes && readErr == nil && response.StatusCode == 503 {
			if root, err := decodeObject(data); err == nil && textAt(root, "code") == "video_queue_full" && extractTaskID(root, Agnes) == "" {
				e.Kind, e.Code = Rejected, "video_queue_full"
			}
		}
		return nil, e
	}
	if readErr != nil {
		if op != "submit" && errors.Is(readErr, ErrLimit) {
			kind = LimitExceeded
		}
		return nil, contextFailure(kind, op, readErr)
	}
	root, err := decodeObject(data)
	if err != nil {
		if op != "submit" {
			kind = ProtocolError
		}
		return nil, failure(kind, op, "invalid_json")
	}
	return root, nil
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(data)) > max {
		return nil, ErrLimit
	}
	return data, err
}

func decodeObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, ErrProtocol
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, ErrProtocol
	}
	return value, nil
}

func retryAfter(raw string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		if date, err := http.ParseTime(raw); err == nil {
			seconds = int(time.Until(date).Seconds())
		} else {
			return 0
		}
	}
	return time.Duration(max(0, min(300, seconds))) * time.Second
}
