package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// GenerateImage automatically uses edits for reference-bearing OpenAI, Yunfei,
// Rua and XinZhiZhu requests. Other profiles retain their Java JSON contracts.
func (g *Gateway) GenerateImage(ctx context.Context, p Provider, r ImageRequest) (ImageResponse, error) {
	return g.image(ctx, p, r, ImageGenerations)
}

func (g *Gateway) EditImage(ctx context.Context, p Provider, r ImageRequest) (ImageResponse, error) {
	return g.image(ctx, p, r, ImageEdits)
}

type loadedReference struct {
	image Image
	url   string
}

func (g *Gateway) image(ctx context.Context, p Provider, r ImageRequest, protocol Protocol) (ImageResponse, error) {
	ctx, cancel := g.callContext(ctx, p)
	defer cancel()
	if ctx.Err() != nil {
		return ImageResponse{}, contextual(ctx.Err(), NotSubmitted)
	}
	if p.ImageProfile == "" {
		p.ImageProfile = OpenAI
	}
	if r.Size == "" {
		r.Size = "1024x1024"
	}
	w, h, ok := parseSize(r.Size)
	if !ok || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 320_000 || len(r.References) > 10 {
		return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
	}
	if protocol == ImageEdits && len(r.References) == 0 {
		return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
	}
	if r.ResponseFormat == "" {
		r.ResponseFormat = "b64_json"
	}
	if r.ResponseFormat != "b64_json" && r.ResponseFormat != "url" {
		return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
	}
	if r.OutputFormat != "" && r.OutputFormat != "png" && r.OutputFormat != "jpeg" && r.OutputFormat != "webp" {
		return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
	}
	for _, field := range []string{r.Quality, r.Background, r.Moderation} {
		if len(field) > 64 {
			return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
		}
	}
	profile := p.ImageProfile
	switch profile {
	case OpenAI, LegacyOpenAI, Hfsy, Yunfei, Rua, XinZhiZhu, Aishuch, GeminiProxy, GoogleGemini:
	default:
		return ImageResponse{}, fail(Unsupported, NotSubmitted)
	}
	gemini := profile == GeminiProxy || profile == GoogleGemini
	if protocol == GeminiGenerateContent && !gemini {
		return ImageResponse{}, fail(InvalidRequest, NotSubmitted)
	}
	if protocol == ImageEdits && (profile == LegacyOpenAI || profile == Hfsy || profile == Aishuch) {
		return ImageResponse{}, fail(Unsupported, NotSubmitted)
	}
	model, size, refLimit := imageSettings(p, r.Size, w, h)
	p.Model = model
	if profile == OpenAI && nativeGPTImage(model) && r.ResponseFormat == "url" {
		return ImageResponse{}, fail(Unsupported, NotSubmitted)
	}
	path := "/v1/images/generations"
	edit := len(r.References) > 0 && (profile == OpenAI || profile == Yunfei || profile == Rua || profile == XinZhiZhu)
	if edit {
		path = "/v1/images/edits"
	}
	if gemini {
		path = "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	}
	endpoint, err := g.endpoint(p, path)
	if err != nil {
		return ImageResponse{}, err
	}
	count := min(len(r.References), refLimit)
	refs, err := g.loadReferences(ctx, r.References[:count], p, profile == LegacyOpenAI || profile == Hfsy || profile == Aishuch || profile == GeminiProxy)
	if err != nil {
		return ImageResponse{}, err
	}
	var root map[string]json.RawMessage
	if edit {
		body, contentType, err := g.multipartImage(p, r, size, refs)
		if err != nil {
			return ImageResponse{}, err
		}
		b, err := g.exchange(ctx, http.MethodPost, endpoint, body, contentType, &p, g.opts.MaxResponseBytes, NotSubmitted)
		if err != nil {
			return ImageResponse{}, err
		}
		root, err = decodeObject(b)
		if err != nil {
			return ImageResponse{}, fail(InvalidResponse, Uncertain)
		}
	} else {
		body := imageJSON(p, r, size, refs)
		root, err = g.jsonCall(ctx, http.MethodPost, endpoint, body, p, NotSubmitted)
		if err != nil {
			return ImageResponse{}, err
		}
	}
	if profile == Aishuch {
		root, err = g.pollImage(ctx, p, root)
		if err != nil {
			return ImageResponse{}, err
		}
	}
	if responseFailure(root) {
		return ImageResponse{}, fail(UpstreamFailure, Uncertain)
	}
	if pendingStatus(imageStatus(root)) {
		return ImageResponse{}, fail(Unsupported, Uncertain)
	}
	payload, b64, err := imagePayload(root, gemini)
	if err != nil {
		return ImageResponse{}, err
	}
	var data []byte
	if b64 {
		data, err = g.decodeBase64(payload, Uncertain)
	} else {
		if containsSecret(payload, p.APIKey) {
			return ImageResponse{}, fail(InvalidResponse, Uncertain)
		}
		data, err = g.exchange(ctx, http.MethodGet, payload, nil, "", nil, g.opts.MaxImageBytes, Uncertain)
	}
	if err != nil {
		return ImageResponse{}, err
	}
	if p.APIKey != "" && bytes.Contains(data, []byte(p.APIKey)) {
		return ImageResponse{}, fail(InvalidResponse, Uncertain)
	}
	img, err := g.inspectImage(data, Uncertain)
	if err != nil {
		return ImageResponse{}, err
	}
	if ctx.Err() != nil {
		return ImageResponse{}, contextual(ctx.Err(), Uncertain)
	}
	return ImageResponse{
		Image: img, Model: redact(model, p.APIKey), RequestedSize: r.Size, SubmittedSize: size,
		ReferencesUsed: count, ReferencesTruncated: count < len(r.References),
	}, nil
}

func (g *Gateway) loadReferences(ctx context.Context, refs []Reference, p Provider, urlsOnly bool) ([]loadedReference, error) {
	loaded := make([]loadedReference, 0, len(refs))
	var total int64
	for _, ref := range refs {
		if ctx.Err() != nil {
			return nil, contextual(ctx.Err(), NotSubmitted)
		}
		if (len(ref.Data) == 0) == (ref.URL == "") || (urlsOnly && ref.URL == "") {
			return nil, fail(InvalidRequest, NotSubmitted)
		}
		data := ref.Data
		if ref.URL != "" {
			if containsSecret(ref.URL, p.APIKey) {
				return nil, fail(InvalidRequest, NotSubmitted)
			}
			var err error
			data, err = g.exchange(ctx, http.MethodGet, ref.URL, nil, "", nil, g.opts.MaxImageBytes, NotSubmitted)
			if err != nil {
				return nil, err
			}
		}
		total += int64(len(data))
		if total > g.opts.MaxRequestBytes || (p.ImageProfile == Hfsy && len(data) > 5<<20) {
			return nil, fail(LimitExceeded, NotSubmitted)
		}
		img, err := g.inspectImage(data, NotSubmitted)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, loadedReference{image: img, url: ref.URL})
	}
	return loaded, nil
}

func (g *Gateway) inspectImage(data []byte, state Submission) (Image, error) {
	if int64(len(data)) > g.opts.MaxImageBytes {
		return Image{}, fail(LimitExceeded, state)
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return Image{}, fail(InvalidResponse, state)
	}
	if c.Width <= 0 || c.Height <= 0 || int64(c.Width) > g.opts.MaxPixels/int64(c.Height) {
		return Image{}, fail(LimitExceeded, state)
	}
	// Decode fully after the allocation guard; a plausible header is not an image.
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != c.Width || decoded.Bounds().Dy() != c.Height {
		return Image{}, fail(InvalidResponse, state)
	}
	return Image{Bytes: data, MIMEType: "image/" + format, Width: c.Width, Height: c.Height}, nil
}

func (g *Gateway) decodeBase64(value string, state Submission) ([]byte, error) {
	if strings.HasPrefix(value, "data:") {
		header, payload, ok := strings.Cut(value, ",")
		if !ok || (header != "data:image/png;base64" && header != "data:image/jpeg;base64" && header != "data:image/webp;base64") {
			return nil, fail(InvalidResponse, state)
		}
		value = payload
	}
	if int64(len(value)) > (g.opts.MaxImageBytes+2)/3*4+4 {
		return nil, fail(LimitExceeded, state)
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding.Strict(), base64.URLEncoding.Strict(), base64.RawStdEncoding.Strict(), base64.RawURLEncoding.Strict()} {
		b, err := encoding.DecodeString(value)
		if err == nil {
			if int64(len(b)) > g.opts.MaxImageBytes {
				return nil, fail(LimitExceeded, state)
			}
			return b, nil
		}
	}
	return nil, fail(InvalidResponse, state)
}

func (g *Gateway) multipartImage(p Provider, r ImageRequest, size string, refs []loadedReference) ([]byte, string, error) {
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	fields := imageFields(p, r, size, true)
	// Fixed field and file order makes the multipart wire format predictable.
	for _, name := range []string{"model", "prompt", "size", "quality", "output_format", "response_format", "background", "moderation", "n"} {
		if value, ok := fields[name]; ok {
			if err := m.WriteField(name, fmt.Sprint(value)); err != nil {
				return nil, "", fail(InvalidRequest, NotSubmitted)
			}
		}
	}
	for i, ref := range refs {
		field := "image"
		if p.ImageProfile == OpenAI && len(refs) > 1 {
			field = "image[]"
		}
		ext := strings.TrimPrefix(ref.image.MIMEType, "image/")
		if ext == "jpeg" {
			ext = "jpg"
		}
		filename := fmt.Sprintf("reference-%d.%s", i+1, ext)
		if p.ImageProfile == Yunfei {
			filename = "reference." + ext
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
		h.Set("Content-Type", ref.image.MIMEType)
		part, err := m.CreatePart(h)
		if err != nil {
			return nil, "", fail(InvalidRequest, NotSubmitted)
		}
		if _, err := part.Write(ref.image.Bytes); err != nil {
			return nil, "", fail(InvalidRequest, NotSubmitted)
		}
		if int64(b.Len()) > g.opts.MaxRequestBytes {
			return nil, "", fail(LimitExceeded, NotSubmitted)
		}
	}
	if err := m.Close(); err != nil {
		return nil, "", fail(InvalidRequest, NotSubmitted)
	}
	return b.Bytes(), m.FormDataContentType(), nil
}

func (g *Gateway) pollImage(ctx context.Context, p Provider, root map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if responseFailure(root) {
		return nil, fail(UpstreamFailure, Uncertain)
	}
	if _, _, err := imagePayload(root, false); err == nil {
		return root, nil
	}
	id := imageTaskID(root)
	if id == "" {
		return root, nil
	}
	if len(id) > 256 || containsSecret(id, p.APIKey) {
		return nil, fail(InvalidResponse, Uncertain)
	}
	endpoint, err := g.endpoint(p, "/v1/tasks/"+url.PathEscape(id))
	if err != nil {
		return nil, contextual(err, Uncertain)
	}
	for {
		// Polling observes the same job. It never repeats the generation POST.
		root, err = g.jsonCall(ctx, http.MethodGet, endpoint, nil, p, Uncertain)
		if err != nil {
			return nil, err
		}
		if responseFailure(root) {
			return nil, fail(UpstreamFailure, Uncertain)
		}
		if _, _, err := imagePayload(root, false); err == nil {
			return root, nil
		}
		status := imageStatus(root)
		if status != "" && !pendingStatus(status) && status != "completed" && status != "success" {
			return nil, fail(InvalidResponse, Uncertain)
		}
		t := time.NewTimer(g.opts.PollInterval)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, contextual(ctx.Err(), Uncertain)
		case <-t.C:
		}
	}
}

func parseSize(s string) (int, int, bool) {
	a, b, ok := strings.Cut(s, "x")
	if !ok || len(a) > 5 || len(b) > 5 {
		return 0, 0, false
	}
	w, e1 := strconv.Atoi(a)
	h, e2 := strconv.Atoi(b)
	return w, h, e1 == nil && e2 == nil && w > 0 && h > 0 && w <= 16384 && h <= 16384 && fmt.Sprintf("%dx%d", w, h) == s
}
