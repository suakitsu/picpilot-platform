package video

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
)

type FetchResult struct {
	Bytes       int64  `json:"bytes"`
	ContentType string `json:"content_type"`
}

// FetchResult streams a result without Authorization, cookies, or redirects.
// The injected transport MUST enforce the parent's URL/DNS/IP egress policy for
// untrusted result hosts and honor context cancellation. The writer must not
// block indefinitely. On any error, discard its partial output; never publish it.
func (g *Gateway) FetchResult(ctx context.Context, rawURL string, dst io.Writer) (FetchResult, error) {
	if err := checkContext(ctx, "fetch"); err != nil {
		return FetchResult{}, err
	}
	if dst == nil {
		return FetchResult{}, failure(InvalidRequest, "fetch", "writer_required")
	}
	u, err := g.resultURL(rawURL)
	if err != nil {
		return FetchResult{}, failure(InvalidRequest, "fetch", "result_url")
	}
	ctx, cancel := context.WithTimeout(ctx, g.config.FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return FetchResult{}, failure(InvalidRequest, "fetch", "result_url")
	}
	response, err := g.client.Do(req)
	if err != nil {
		return FetchResult{}, contextFailure(TransportError, "fetch", err)
	}
	defer response.Body.Close()
	// Partial responses must not be mistaken for a complete saved video.
	if response.StatusCode != http.StatusOK {
		e := failure(ProtocolError, "fetch", "http_status")
		e.HTTPStatus = response.StatusCode
		return FetchResult{}, e
	}
	if response.ContentLength > g.config.MaxResultBytes {
		return FetchResult{}, failure(LimitExceeded, "fetch", "result_limit")
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "video/mp4"
	}
	if !strings.HasPrefix(contentType, "video/") || len(contentType) > 128 {
		return FetchResult{}, failure(ProtocolError, "fetch", "content_type")
	}
	n, err := io.Copy(dst, io.LimitReader(response.Body, g.config.MaxResultBytes))
	if err != nil {
		return FetchResult{}, contextFailure(TransportError, "fetch", err)
	}
	var extra [1]byte
	count, err := io.ReadFull(response.Body, extra[:])
	if count > 0 {
		return FetchResult{}, failure(LimitExceeded, "fetch", "result_limit")
	}
	if err != nil && err != io.EOF {
		return FetchResult{}, contextFailure(TransportError, "fetch", err)
	}
	if n == 0 {
		return FetchResult{}, failure(ProtocolError, "fetch", "empty_result")
	}
	return FetchResult{Bytes: n, ContentType: contentType}, nil
}
