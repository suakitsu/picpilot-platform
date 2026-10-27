package video

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

const fixtureKey = "mock-video-credential"
const fixtureImage = "https://images.example.test/product.png"
const fixtureVideo = "https://cdn.example.test/result.mp4"

func gatewayFor(t *testing.T, provider Provider, handler http.HandlerFunc, configure ...func(*Config)) (*Gateway, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	c := Config{Provider: provider, APIKey: fixtureKey, BaseURL: server.URL, Client: server.Client()}
	for _, change := range configure {
		change(&c)
	}
	gateway, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	return gateway, server
}

func validRequest() Request {
	return Request{Prompt: "Show the product rotating slowly", SourceURLs: []string{fixtureImage}}
}

func boolPtr(v bool) *bool    { return &v }
func int64Ptr(v int64) *int64 { return &v }

func createResponse(w http.ResponseWriter, provider Provider) {
	w.Header().Set("Content-Type", "application/json")
	if provider == Agnes {
		_, _ = w.Write([]byte(`{"video_id":"task_1"}`))
	} else {
		_, _ = w.Write([]byte(`{"task_id":"task_1"}`))
	}
}

func TestCreatePayloads(t *testing.T) {
	tests := []struct {
		provider   Provider
		model      string
		resolution string
		upstream   string
		size       string
	}{
		{Gemini, "gemini-omni", "720p", "gemini-omni-flash-preview", ""},
		{Seedance, "seedance-mini", "480p", "doubao-seedance-2.0-mini", ""},
		{Seedance, "seedance-mini", "720p", "doubao-seedance-2.0-mini", ""},
		{Seedance, "seedance-fast", "480p", "doubao-seedance-2.0-fast", ""},
		{Seedance, "seedance-fast", "720p", "doubao-seedance-2.0-fast", ""},
		{Seedance, "seedance-2", "480p", "doubao-seedance-2.0", ""},
		{Seedance, "seedance-2", "720p", "doubao-seedance-2.0", ""},
		{Seedance, "seedance-2", "1080p", "doubao-seedance-2.0", ""},
		{Seedance, "seedance-2", "4k", "doubao-seedance-2.0", ""},
		{HFSY, "seedance-mini", "480p", "sd-2.0-mini", "small"},
		{HFSY, "seedance-mini", "720p", "sd-2.0-mini", "large"},
		{HFSY, "seedance-fast", "480p", "sd-2.0-fast-480", "large"},
		{HFSY, "seedance-fast", "720p", "sd-2.0-fast", "large"},
		{HFSY, "seedance-2", "480p", "sd-2.0-480", "large"},
		{HFSY, "seedance-2", "720p", "sd-2.0", "large"},
		{Agnes, "agnes-v2", "720p", "agnes-video-v2.0", ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider)+"/"+tt.model+"/"+tt.resolution, func(t *testing.T) {
			var received map[string]any
			g, _ := gatewayFor(t, tt.provider, func(w http.ResponseWriter, r *http.Request) {
				path := "/v1/videos/generations"
				if tt.provider == HFSY {
					path = "/v1/video/create"
				} else if tt.provider == Agnes {
					path = "/v1/videos"
				}
				if r.Method != "POST" || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer "+fixtureKey || r.Header.Get("Content-Type") != "application/json" {
					t.Error("incorrect create request")
				}
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				createResponse(w, tt.provider)
			})
			r := validRequest()
			r.Model, r.Resolution = tt.model, tt.resolution
			s, err := g.Submit(context.Background(), r)
			if err != nil || s.Model != tt.upstream || s.Provider != tt.provider || s.UpstreamTaskID != "task_1" {
				t.Fatalf("submission=%+v err=%v", s, err)
			}
			expected := map[string]any{"model": tt.upstream, "prompt": compilePrompt(r.Prompt, tt.provider, 1)}
			switch tt.provider {
			case Gemini:
				expected["aspect_ratio"], expected["image_urls"] = "9:16", []any{fixtureImage}
			case Seedance:
				expected["image_urls"] = []any{fixtureImage}
				expected["duration"], expected["size"] = float64(10), "9:16"
				expected["resolution"], expected["generate_audio"] = normalizeResolution(tt.model, tt.resolution), true
			case HFSY:
				expected["images"], expected["ratio"] = []any{fixtureImage}, "9:16"
				expected["duration"], expected["size"], expected["watermark"] = float64(10), tt.size, false
			case Agnes:
				expected["prompt"], expected["negative_prompt"] = r.Prompt+agnesContinuity, agnesNegativePrompt
				expected["image"], expected["width"], expected["height"] = fixtureImage, float64(720), float64(1280)
				expected["frame_rate"], expected["num_frames"] = float64(24), float64(241)
			}
			if !reflect.DeepEqual(expected, received) {
				t.Errorf("payload did not match the full allowed field set for %s", tt.provider)
			}
		})
	}
}

func TestAudioSeedWatermarkAndWebhook(t *testing.T) {
	for _, audio := range []bool{false, true} {
		for _, seed := range []int64{0, 2147483647} {
			g, _ := gatewayFor(t, Seedance, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["generate_audio"] != audio || body["seed"] != float64(seed) || body["webhook"] != "https://callback.example.test/apimart/token" {
					t.Error("audio, seed, or webhook not forwarded correctly")
				}
				createResponse(w, Seedance)
			}, func(c *Config) { c.WebhookURL = "https://callback.example.test/apimart/token" })
			r := validRequest()
			r.GenerateAudio, r.Seed = &audio, &seed
			if _, err := g.Submit(context.Background(), r); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, watermark := range []bool{false, true} {
		g, _ := gatewayFor(t, HFSY, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["watermark"] != watermark {
				t.Error("watermark not forwarded")
			}
			createResponse(w, HFSY)
		})
		r := validRequest()
		r.Watermark = &watermark
		if _, err := g.Submit(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgnesAllDimensionsAndCustomModel(t *testing.T) {
	for ratio, dimensions := range map[string][2]int{"9:16": {720, 1280}, "16:9": {1280, 720}, "1:1": {720, 720}, "3:4": {720, 960}, "4:3": {960, 720}} {
		g, _ := gatewayFor(t, Agnes, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["width"] != float64(dimensions[0]) || body["height"] != float64(dimensions[1]) || body["num_frames"] != float64(73) || body["model"] != "agnes-video-v2.1-custom" {
				t.Errorf("incorrect Agnes parameters for %s", ratio)
			}
			createResponse(w, Agnes)
		}, func(c *Config) { c.AgnesModel = "agnes-video-v2.1-custom" })
		r := validRequest()
		r.AspectRatio, r.DurationSeconds = ratio, 3
		if _, err := g.Submit(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidationDoesNotSend(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		t.Run(string(provider), func(t *testing.T) {
			var calls atomic.Int32
			g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); createResponse(w, provider) })
			cases := []func(*Request){
				func(r *Request) { r.Model = "not-a-model" },
				func(r *Request) { r.Resolution = "8k" },
				func(r *Request) { r.DurationSeconds = -1 },
				func(r *Request) { r.DurationSeconds = 16 },
				func(r *Request) { r.AspectRatio = "2:1" },
				func(r *Request) { r.Prompt = "abc" },
				func(r *Request) { r.Prompt = strings.Repeat("a", 4001) },
				func(r *Request) { r.SourceURLs = nil },
				func(r *Request) {
					r.SourceURLs = []string{fixtureImage, fixtureImage, fixtureImage, fixtureImage, fixtureImage}
				},
				func(r *Request) { r.SourceURLs = []string{""} },
				func(r *Request) { r.SourceURLs = []string{"file:///secret.png"} },
				func(r *Request) { r.SourceURLs = []string{"https://user:pass@images.example.test/file.png"} },
				func(r *Request) { r.SourceURLs = []string{"//images.example.test/file.png"} },
				func(r *Request) { r.SourceURLs = []string{"/image.png"} },
			}
			if provider != Seedance {
				cases = append(cases, func(r *Request) { r.GenerateAudio = boolPtr(false) }, func(r *Request) { r.Seed = int64Ptr(1) })
			} else {
				cases = append(cases, func(r *Request) { r.Seed = int64Ptr(-1) }, func(r *Request) { r.Seed = int64Ptr(2147483648) }, func(r *Request) { r.Model, r.Resolution = "seedance-mini", "1080p" })
			}
			if provider != HFSY {
				cases = append(cases, func(r *Request) { r.Watermark = boolPtr(false) })
			} else {
				cases = append(cases, func(r *Request) { r.Model, r.Resolution = "sd-2.0-fast-480", "720p" })
			}
			if provider == Gemini {
				cases = append(cases, func(r *Request) { r.SourceURLs = []string{"https://images.example.test/file.webp"} })
			}
			for i, change := range cases {
				r := validRequest()
				change(&r)
				if _, err := g.Submit(context.Background(), r); !errors.Is(err, ErrInvalidRequest) {
					t.Errorf("case %d: expected invalid request, got %v", i, err)
				}
			}
			if calls.Load() != 0 {
				t.Fatal("invalid request reached mock upstream")
			}
		})
	}
}

func TestNativeModelsDefaultsAndRelativeReferences(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			encoded, _ := json.Marshal(body)
			if !strings.Contains(string(encoded), "https://images.example.test/static/product.png") {
				t.Error("relative source was not resolved")
			}
			createResponse(w, provider)
		}, func(c *Config) { c.PublicBaseURL = "https://images.example.test/static/" })
		r := validRequest()
		r.SourceURLs = []string{"/product.png"}
		s, err := g.Submit(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		r.Model = s.Model
		if _, err := g.Submit(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	model, err := ResolveModel(HFSY, "sd-2.0-fast-480", "")
	if err != nil || model != "sd-2.0-fast-480" {
		t.Fatalf("native resolution default: %s %v", model, err)
	}
}

func TestPromptPolicyParity(t *testing.T) {
	for _, prompt := range []string{"shot 1 product; scene 2 table", "\u955c\u59341\u5c55\u793a\u5546\u54c1\uff1b\u955c\u59342\u8fdb\u5165\u573a\u666f", "0-5s product; 5-10s scene"} {
		compiled := compilePrompt(prompt, HFSY, 2)
		if !strings.Contains(compiled, "Cuts and transitions are allowed") || !strings.Contains(compiled, "@\u56fe1") || !strings.Contains(compiled, "@\u56fe2") {
			t.Error("storyboard/reference policy mismatch")
		}
	}
	for _, prompt := range []string{"single shot; scene 1", "\u4e00\u955c\u5230\u5e95\u5c55\u793a\u5546\u54c1", "rotate the product"} {
		if !strings.Contains(compilePrompt(prompt, Seedance, 1), "No cuts, no scene replacement") {
			t.Error("single-shot policy mismatch")
		}
	}
	for _, provider := range []Provider{Gemini, Seedance, HFSY} {
		compiled := compilePrompt(strings.Repeat("\U0001f600", 2000), provider, 4)
		limit := 4000
		if provider == HFSY {
			limit = 5000
		}
		if !utf8.ValidString(compiled) || len(utf16.Encode([]rune(compiled))) > limit {
			t.Error("prompt truncation split Unicode or exceeded provider budget")
		}
	}
}
