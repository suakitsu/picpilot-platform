package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "mock-only-provider-key-123"

// Generated lossless 1x1 RGB(20,40,60) fixture; no user image data.
const onePixelWebP = "UklGRh4AAABXRUJQVlA4TBEAAAAvAAAAAAdQlFKUp/+BiOh/AAA="
const headerOnlyJavaWebP = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AA/v89WAAAAA=="

func testGateway(t *testing.T, change func(*Options)) *Gateway {
	t.Helper()
	o := Options{AllowLocalDevelopment: true, Timeout: 2 * time.Second, PollInterval: time.Millisecond}
	if change != nil {
		change(&o)
	}
	g, err := NewGateway(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.CloseIdleConnections)
	return g
}

func testProvider(s *httptest.Server) Provider {
	return Provider{BaseURL: s.URL, APIKey: Secret(testKey), Model: "gpt-image-2"}
}

func fixture(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	if format == "webp" {
		b, err := base64.StdEncoding.DecodeString(onePixelWebP)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			im.Set(x, y, color.NRGBA{R: uint8(x * 13), G: uint8(y * 17), B: 61, A: 255})
		}
	}
	var b bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&b, im, nil)
	} else {
		err = png.Encode(&b, im)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func imageJSONResponse(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
}

func assertError(t *testing.T, err error, kind ErrorKind, state Submission) *Error {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("wanted typed %s error, got %v", kind, err)
	}
	if pe.Kind != kind || pe.Submission != state {
		t.Fatalf("got %v, want %s/%s", pe, kind, state)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", err, err), testKey) {
		t.Fatal("credential disclosed by error")
	}
	return pe
}

func TestImageGenerationDecodedMetadata(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "webp"} {
		t.Run(format, func(t *testing.T) {
			w, h := 19, 7
			if format == "webp" {
				w, h = 1, 1
			}
			data := fixture(t, format, w, h)
			s := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/images/generations" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+testKey {
					t.Error("wrong endpoint/method/auth")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("bad JSON")
				}
				if body["prompt"] != "mock scene" || body["size"] != "1024x1024" || body["n"] != float64(1) || body["response_format"] != nil {
					t.Errorf("bad body: %v", body)
				}
				imageJSONResponse(rw, data)
			}))
			defer s.Close()
			p := testProvider(s)
			p.BaseURL += "/v1/"
			result, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock scene"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Image.Width != w || result.Image.Height != h || result.Image.MIMEType != "image/"+format || !bytes.Equal(data, result.Image.Bytes) {
				t.Fatal("metadata is not decoded or original image was changed")
			}
		})
	}
}

func TestEditsTenOrderedMixedReferences(t *testing.T) {
	formats := []string{"png", "jpeg", "webp", "png", "jpeg", "png", "png", "png", "png", "png"}
	refs := make([]Reference, len(formats))
	for i, format := range formats {
		refs[i] = Reference{Data: fixture(t, format, i+1, 2)}
	}
	output := fixture(t, "png", 13, 9)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Error("not edits")
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		var received [][]byte
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			b, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
				return
			}
			if part.FileName() == "" {
				continue
			}
			i := len(received)
			if part.FormName() != "image[]" || i >= len(formats) || part.Header.Get("Content-Type") != "image/"+formats[i] {
				t.Error("incorrect file field/content type")
			}
			received = append(received, b)
		}
		if len(received) != len(refs) {
			t.Errorf("got %d refs", len(received))
		}
		for i := range received {
			if !bytes.Equal(received[i], refs[i].Data) {
				t.Error("reference order/bytes changed")
			}
		}
		imageJSONResponse(w, output)
	}))
	defer s.Close()
	g := testGateway(t, nil)
	resp, err := g.EditImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock edit", References: refs})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ReferencesUsed != 10 || resp.ReferencesTruncated {
		t.Fatal("wrong reference metadata")
	}
	_, err = g.GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock", References: append(refs, refs[0])})
	assertError(t, err, InvalidRequest, NotSubmitted)
}

func TestLegacyMultipartProfiles(t *testing.T) {
	for _, tc := range []struct {
		profile        ImageProfile
		count          int
		size, expected string
	}{
		{Yunfei, 1, "2048x2048", "2048x2048"}, {Rua, 4, "2048x2048", "1600x1600"}, {XinZhiZhu, 5, "1024x1024", "1600x1600"},
	} {
		t.Run(string(tc.profile), func(t *testing.T) {
			data := fixture(t, "png", 3, 2)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				if len(r.MultipartForm.File["image"]) != tc.count || r.FormValue("model") != "gpt-image-2" || r.FormValue("size") != tc.expected {
					t.Error("Java multipart contract mismatch")
				}
				if tc.profile == Rua && (r.FormValue("moderation") != "low" || r.FormValue("response_format") != "") {
					t.Error("Rua fields changed")
				}
				if tc.profile == XinZhiZhu && (r.FormValue("response_format") != "b64_json" || r.FormValue("quality") != "auto") {
					t.Error("XinZhiZhu fields changed")
				}
				imageJSONResponse(w, data)
			}))
			defer s.Close()
			p := testProvider(s)
			p.ImageProfile = tc.profile
			p.Model = "gpt-image-2pro"
			refs := make([]Reference, 6)
			for i := range refs {
				refs[i].Data = data
			}
			resp, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock", Size: tc.size, References: refs})
			if err != nil {
				t.Fatal(err)
			}
			if resp.ReferencesUsed != tc.count || !resp.ReferencesTruncated || resp.SubmittedSize != tc.expected {
				t.Fatal("truncation not reported")
			}
		})
	}
}

func TestLegacyJSONProfiles(t *testing.T) {
	for _, tc := range []struct {
		profile                                  ImageProfile
		model, size, expectedModel, expectedSize string
		count                                    int
		refField                                 string
	}{
		{LegacyOpenAI, "gpt-image-2pro", "2048x2048", "gpt-image-2", "2048x2048", 6, "reference_images"},
		{Hfsy, "gpt-image-2", "2048x2048", "gpt-image-2pro", "2048x2048", 4, "reference_images"},
		{Aishuch, "gpt-image-2pro", "3696x1584", "gpt-image-2", "3840x1648", 6, "image_urls"},
	} {
		t.Run(string(tc.profile), func(t *testing.T) {
			data := fixture(t, "png", 4, 3)
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/ref/") {
					if r.Header.Get("Authorization") != "" {
						t.Error("reference auth leaked")
					}
					_, _ = w.Write(data)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["model"] != tc.expectedModel || body["size"] != tc.expectedSize {
					t.Errorf("model/size mismatch: %v", body)
				}
				if tc.profile == Aishuch && (body["resolution"] != "4k" || body["response_format"] != nil) {
					t.Error("Aishuch contract mismatch")
				}
				refs, ok := body[tc.refField].([]any)
				if !ok || len(refs) != tc.count {
					t.Error("missing refs")
					return
				}
				for i, ref := range refs {
					if ref != fmt.Sprintf("%s/ref/%d", base, i) {
						t.Error("URL order changed")
					}
				}
				imageJSONResponse(w, data)
			}))
			defer s.Close()
			base = s.URL
			refs := make([]Reference, 6)
			for i := range refs {
				refs[i].URL = fmt.Sprintf("%s/ref/%d", base, i)
			}
			p := testProvider(s)
			p.ImageProfile, p.Model = tc.profile, tc.model
			_, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock", Size: tc.size, References: refs})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeminiProfiles(t *testing.T) {
	for _, profile := range []ImageProfile{GeminiProxy, GoogleGemini} {
		t.Run(string(profile), func(t *testing.T) {
			data := fixture(t, "png", 5, 3)
			var refURLs []string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/ref/") {
					_, _ = w.Write(data)
					return
				}
				if r.URL.Path != "/v1beta/models/gemini-test:generateContent" {
					t.Errorf("bad Gemini endpoint: %s", r.URL.Path)
				}
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				config := rawObject(body["generationConfig"])
				im := rawObject(config["imageConfig"])
				if rawString(im["aspectRatio"]) != "16:9" {
					t.Error("wrong ratio")
				}
				contents := rawArray(body["contents"])
				parts := rawArray(rawObject(contents[0])["parts"])
				if rawString(rawObject(parts[0])["text"]) != "mock Gemini" {
					t.Error("missing first text part")
				}
				if profile == GeminiProxy {
					if len(parts) != 5 || r.Header.Get("Authorization") != "Bearer "+testKey || r.Header.Get("X-Goog-Api-Key") != "" || rawString(im["resolution"]) != "2K" || nonNull(im["imageSize"]) {
						t.Error("proxy contract mismatch")
					}
					for i, part := range parts[1:] {
						if rawString(rawObject(rawObject(part)["fileData"])["fileUri"]) != refURLs[i] {
							t.Error("proxy reference order")
						}
					}
				} else {
					if len(parts) != 7 || r.Header.Get("Authorization") != "" || r.Header.Get("X-Goog-Api-Key") != testKey || rawString(im["imageSize"]) != "2K" || nonNull(im["resolution"]) {
						t.Error("Google contract mismatch")
					}
					if !reflect.DeepEqual(rawArray(config["responseModalities"]), []json.RawMessage{json.RawMessage(`"TEXT"`), json.RawMessage(`"IMAGE"`)}) {
						t.Error("missing modalities")
					}
					for _, part := range parts[1:] {
						inline := rawObject(rawObject(part)["inlineData"])
						if rawString(inline["data"]) != base64.StdEncoding.EncodeToString(data) || rawString(inline["mimeType"]) != "image/png" {
							t.Error("Google inline image mismatch")
						}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": "done"}, map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(data)}}}}}}})
			}))
			defer s.Close()
			refs := make([]Reference, 6)
			for i := range refs {
				refs[i].URL = fmt.Sprintf("%s/ref/%d", s.URL, i)
				refURLs = append(refURLs, refs[i].URL)
			}
			p := testProvider(s)
			p.ImageProfile, p.Model, p.BaseURL = profile, "gemini-test", s.URL+"/v1beta"
			resp, err := testGateway(t, nil).Do(context.Background(), Request{Provider: p, Protocol: GeminiGenerateContent, Image: &ImageRequest{Prompt: "mock Gemini", Size: "2048x1152", References: refs}})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Image.Image.Width != 5 || resp.Image.Image.Height != 3 {
				t.Fatal("bad image metadata")
			}
		})
	}
}

func TestAishuchPollDoesNotResubmit(t *testing.T) {
	data := fixture(t, "png", 7, 2)
	var posts, polls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("missing API auth")
		}
		if r.Method == "POST" {
			posts.Add(1)
			_, _ = io.WriteString(w, `{"code":200,"data":[{"status":"submitted","task_id":"job/1"}]}`)
			return
		}
		if r.URL.EscapedPath() != "/v1/tasks/job%2F1" {
			t.Errorf("task ID not escaped: %s", r.URL.EscapedPath())
		}
		if polls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"data":{"status":"running"}}`)
			return
		}
		imageJSONResponse(w, data)
	}))
	defer s.Close()
	p := testProvider(s)
	p.ImageProfile = Aishuch
	_, err := testGateway(t, nil).GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 || polls.Load() != 2 {
		t.Fatal("wrong submission/poll count")
	}
}

func TestInvalidImagesAndEnvelopes(t *testing.T) {
	valid := fixture(t, "png", 5, 3)
	for _, body := range []string{
		`{}`, `{"data":[]}`, `null`, `{"data":[{"b64_json":"not-an-image"}]}`, `{"error":{"message":"` + testKey + ` prompt"}}`,
		`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(valid[:len(valid)/2]) + `"}]}`,
		`{"prompt_echo":"http://127.0.0.1/should-not-fetch"}`, `{"data":[]} garbage`,
		`{"data":[{"b64_json":"` + headerOnlyJavaWebP + `"}]}`,
	} {
		t.Run(fmt.Sprintf("case-%d", len(body)), func(t *testing.T) {
			var posts atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); _, _ = io.WriteString(w, body) }))
			defer s.Close()
			_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "private mock prompt"})
			var pe *Error
			if !errors.As(err, &pe) || pe.Submission != Uncertain || strings.Contains(err.Error(), testKey) || posts.Load() != 1 {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}

func TestImagePreflightRejectsBeforeSubmission(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { count.Add(1) }))
	defer s.Close()
	g := testGateway(t, func(o *Options) { o.MaxPixels = 10 })
	p := testProvider(s)
	_, err := g.EditImage(context.Background(), p, ImageRequest{Prompt: "mock", References: []Reference{{Data: fixture(t, "png", 5, 3)}}})
	assertError(t, err, LimitExceeded, NotSubmitted)
	_, err = g.EditImage(context.Background(), p, ImageRequest{Prompt: "mock", References: []Reference{{Data: []byte("bogus")}}})
	assertError(t, err, InvalidResponse, NotSubmitted)
	_, err = g.EditImage(context.Background(), p, ImageRequest{Prompt: "mock"})
	assertError(t, err, InvalidRequest, NotSubmitted)
	_, err = g.GenerateImage(context.Background(), p, ImageRequest{Prompt: "mock", Size: "1x-1"})
	assertError(t, err, InvalidRequest, NotSubmitted)
	if count.Load() != 0 {
		t.Fatal("submitted invalid input")
	}
}

func TestImageBase64Variants(t *testing.T) {
	data := fixture(t, "png", 7, 4)
	for _, encoded := range []string{base64.StdEncoding.EncodeToString(data), base64.RawStdEncoding.EncodeToString(data), base64.URLEncoding.EncodeToString(data), "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": encoded}}})
		}))
		_, err := testGateway(t, nil).GenerateImage(context.Background(), testProvider(s), ImageRequest{Prompt: "mock"})
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
