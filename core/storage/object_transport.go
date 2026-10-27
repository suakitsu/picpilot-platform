package storage

import (
	"io"
	"net/http"
	"strings"
)

// The SDK also disables redirects. This boundary additionally rejects every
// redirect response and restricts signed requests to one bucket/prefix/origin.
type objectTransport struct {
	base                         http.RoundTripper
	scheme, host, bucket, prefix string
	virtual                      bool
}

func (t *objectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != t.scheme || r.URL.Host != t.host || r.URL.User != nil || (r.Host != "" && r.Host != t.host) {
		return nil, ErrObjectRedirect
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if !t.virtual {
		if path == t.bucket {
			path = ""
		} else {
			var ok bool
			path, ok = strings.CutPrefix(path, t.bucket+"/")
			if !ok {
				return nil, ErrObjectUnavailable
			}
		}
	}
	objectKey, hasPrefix := strings.CutPrefix(path, t.prefix)
	isObject := hasPrefix && keyPattern.MatchString(objectKey)
	if isObject {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		default:
			return nil, ErrObjectUnavailable
		}
	} else if path == "" {
		if r.Method != http.MethodHead && !(r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" && r.URL.Query().Get("prefix") == t.prefix) {
			return nil, ErrObjectUnavailable
		}
	} else {
		return nil, ErrObjectUnavailable
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, objectError(err)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, ErrObjectRedirect
	}
	// Error/list XML is bounded; a successful media GET remains streaming.
	if !(isObject && r.Method == http.MethodGet && response.StatusCode >= 200 && response.StatusCode < 300) {
		response.Body = &objectBoundedBody{ReadCloser: response.Body, remaining: 2 << 20}
	}
	return response, nil
}

type objectBoundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *objectBoundedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			return 0, ErrObjectUnavailable
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
