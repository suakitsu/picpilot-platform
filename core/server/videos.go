package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
	"github.com/suakitsu/picpilot-platform/core/video"
)

func (s *Server) registerVideoRoutes() {
	probe, _ := http.NewRequest(http.MethodGet, "/api/videos/config", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/videos/config" {
		return
	}
	s.mux.HandleFunc("GET /api/videos/config", s.auth(s.getVideoConfig))
	s.mux.HandleFunc("GET /api/videos/tasks", s.auth(s.listVideos))
	s.mux.HandleFunc("GET /api/videos/tasks/{id}", s.auth(s.getVideo))
	s.mux.HandleFunc("POST /api/videos/tasks", s.auth(s.createVideo))
	s.mux.HandleFunc("DELETE /api/videos/tasks/{id}", s.auth(s.deleteVideo))
	s.mux.HandleFunc("POST /api/videos/tasks/{id}/cancel", s.auth(s.cancelVideo))
	s.mux.HandleFunc("POST /api/videos/script-preview", s.auth(s.previewVideoScript))
	s.mux.HandleFunc("GET /api/admin/video-config", s.auth(s.admin(s.getVideoAdminConfig)))
	s.mux.HandleFunc("PATCH /api/admin/video-config", s.auth(s.admin(s.patchVideoConfig)))
	s.mux.HandleFunc("GET /api/admin/video-tasks", s.auth(s.admin(s.listAdminVideos)))
	s.mux.HandleFunc("DELETE /api/admin/video-tasks/{id}", s.auth(s.admin(s.deleteAdminVideo)))
	s.mux.HandleFunc("POST /api/public/video-webhooks/apimart", s.receiveVideoWebhook)
	s.mux.HandleFunc("POST /api/public/video-webhooks/apimart/{secret}/callback", s.receiveVideoWebhook)
	// The suffix preserves Gemini's JPG/PNG URL contract for signed local assets.
	s.mux.HandleFunc("GET /api/videos/references/{id}/{filename}", s.serveVideoReference)
}

type videoVariant struct {
	Resolution string `json:"resolution"`
	Duration   int    `json:"duration_seconds"`
	Price      int64  `json:"price"`
	Enabled    bool   `json:"enabled"`
}
type videoModel struct {
	Code               string         `json:"code"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Provider           video.Provider `json:"provider"`
	Model              string         `json:"model"`
	Enabled            bool           `json:"enabled"`
	DefaultResolution  string         `json:"default_resolution"`
	DefaultDuration    int            `json:"default_duration_seconds"`
	DurationSelectable bool           `json:"duration_selectable"`
	AudioToggle        bool           `json:"audio_toggle"`
	MaxReferences      int            `json:"max_reference_images"`
	Ratios             []string       `json:"aspect_ratios"`
	Resolutions        []string       `json:"resolutions"`
	Durations          []int          `json:"durations"`
	Variants           []videoVariant `json:"variants"`
}
type videoModelUpdate struct {
	Code     string         `json:"code"`
	Enabled  bool           `json:"enabled"`
	Variants []videoVariant `json:"variants"`
}
type videoKeySetting struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Concurrency int    `json:"concurrency"`
}
type videoRuntimeConfig struct {
	Enabled              bool               `json:"enabled"`
	Paid                 bool               `json:"paid_provider_requested"`
	Gemini               bool               `json:"gemini_enabled"`
	Seedance             bool               `json:"seedance_enabled"`
	HFSY                 bool               `json:"hfsy_enabled"`
	Agnes                bool               `json:"agnes_route_enabled"`
	StandardEnabled      bool               `json:"standard_enabled"`
	StandardDuration     int                `json:"standard_duration_seconds"`
	StandardResolution   string             `json:"standard_resolution"`
	StandardPrice        int64              `json:"standard_price"`
	QualityEnabled       bool               `json:"quality_enabled"`
	QualityDuration      int                `json:"quality_duration_seconds"`
	QualityResolution    string             `json:"quality_resolution"`
	QualityPrice         int64              `json:"quality_price"`
	AgnesPercent         int                `json:"agnes_route_percent"`
	AgnesDailyLimit      int                `json:"agnes_daily_limit"`
	AgnesConcurrency     int                `json:"agnes_concurrency"`
	AgnesCircuitFailures int                `json:"agnes_circuit_failures"`
	PollSeconds          int                `json:"poll_seconds"`
	TimeoutMinutes       int                `json:"timeout_minutes"`
	AgnesModel           string             `json:"agnes_model"`
	AgnesKeys            []videoKeySetting  `json:"agnes_keys"`
	Models               []videoModelUpdate `json:"model_configs"`
}

func defaultVideoConfig() videoRuntimeConfig {
	return videoRuntimeConfig{StandardEnabled: true, StandardDuration: 10, StandardResolution: "720p", StandardPrice: 900, QualityEnabled: true, QualityDuration: 10, QualityResolution: "720p", QualityPrice: 1200, AgnesDailyLimit: 40, AgnesConcurrency: 1, AgnesCircuitFailures: 3, PollSeconds: 5, TimeoutMinutes: 30, AgnesModel: "agnes-video-v2.0", Models: []videoModelUpdate{}}
}
func (s *Server) videoConfig(ctx context.Context) (videoRuntimeConfig, error) {
	c := defaultVideoConfig()
	if s.Config.Edition == config.Personal {
		c.Enabled, c.Paid, c.Gemini, c.Seedance, c.HFSY, c.Agnes = true, true, true, true, true, true
		return c, nil
	}
	data, err := s.Store.Setting(ctx, "video_runtime_config")
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, store.ErrConflict
	}
	return c, nil
}

func videoCatalog(c videoRuntimeConfig) []videoModel {
	ratios := []string{"9:16", "16:9", "1:1", "3:4", "4:3", "adaptive"}
	models := []videoModel{
		{Code: "gemini-omni", Name: "Gemini Omni", Provider: video.Gemini, Model: "gemini-omni-flash-preview", MaxReferences: 3, Ratios: ratios[:2], Resolutions: []string{"720p"}, Durations: []int{10}},
		{Code: "agnes-v2", Name: "Agnes V2.0", Provider: video.Agnes, Model: c.AgnesModel, MaxReferences: 1, Ratios: ratios[:5], Resolutions: []string{"720p"}, Durations: []int{10}},
		{Code: "seedance-mini", Name: "Seedance Mini", Provider: video.Seedance, Model: "doubao-seedance-2.0-mini", MaxReferences: 4, Ratios: ratios, Resolutions: []string{"480p", "720p"}, Durations: []int{5, 8, 10, 15}, AudioToggle: true, DurationSelectable: true},
		{Code: "seedance-fast", Name: "Seedance 2.0 Fast", Provider: video.Seedance, Model: "doubao-seedance-2.0-fast", MaxReferences: 4, Ratios: ratios, Resolutions: []string{"480p", "720p"}, Durations: []int{5, 8, 10, 15}, AudioToggle: true, DurationSelectable: true},
		{Code: "seedance-2", Name: "Seedance 2.0", Provider: video.Seedance, Model: "doubao-seedance-2.0", MaxReferences: 4, Ratios: ratios, Resolutions: []string{"480p", "720p", "1080p", "4k"}, Durations: []int{5, 8, 10, 15}, AudioToggle: true, DurationSelectable: true},
	}
	prices := [][]int64{{950}, {100}, {150, 230, 290, 430, 320, 490, 610, 920}, {290, 460, 570, 850, 610, 980, 1220, 1830}, {360, 570, 710, 1060, 760, 1220, 1520, 2280, 1890, 3030, 3780, 5670, 3850, 6160, 7700, 11550}}
	for i := range models {
		m := &models[i]
		m.Enabled = true
		m.DefaultResolution = "720p"
		m.DefaultDuration = 10
		m.Description = m.Name
		for _, resolution := range m.Resolutions {
			for _, duration := range m.Durations {
				m.Variants = append(m.Variants, videoVariant{resolution, duration, prices[i][len(m.Variants)], true})
			}
		}
		for _, update := range c.Models {
			if update.Code == m.Code {
				m.Enabled = update.Enabled
				for j, v := range m.Variants {
					for _, configured := range update.Variants {
						if v.Resolution == configured.Resolution && v.Duration == configured.Duration {
							m.Variants[j] = configured
						}
					}
				}
			}
		}
	}
	return models
}

type videoCreateInput struct {
	Title          string   `json:"title"`
	Prompt         string   `json:"prompt"`
	ModelCode      string   `json:"model_code"`
	AspectRatio    string   `json:"aspect_ratio"`
	Mode           string   `json:"mode"`
	Duration       int      `json:"duration_seconds"`
	Resolution     string   `json:"resolution"`
	GenerateAudio  *bool    `json:"generate_audio"`
	Seed           *int64   `json:"seed"`
	OriginalPrompt string   `json:"original_prompt"`
	ImageURLs      []string `json:"image_urls"`
	ProjectID      string   `json:"project_id,omitempty"`
}
type videoReference struct {
	URL     string `json:"url,omitempty"`
	AssetID string `json:"asset_id,omitempty"`
}

// This DTO is private durable input, not a public response and not video.Request's
// intentionally redacted JSON representation. No credentials or image bytes occur.
type videoTaskDocument struct {
	Prompt          string           `json:"prompt"`
	OriginalPrompt  string           `json:"original_prompt,omitempty"`
	ModelCode       string           `json:"model_code"`
	Mode            string           `json:"mode"`
	Model           string           `json:"model"`
	Provider        video.Provider   `json:"provider"`
	ProviderID      string           `json:"provider_id"`
	ProviderScope   string           `json:"provider_scope"`
	ProviderVersion int64            `json:"provider_version"`
	AspectRatio     string           `json:"aspect_ratio"`
	Duration        int              `json:"duration_seconds"`
	Resolution      string           `json:"resolution"`
	GenerateAudio   *bool            `json:"generate_audio,omitempty"`
	Seed            *int64           `json:"seed,omitempty"`
	References      []videoReference `json:"references"`
	Fingerprint     string           `json:"fingerprint"`
	ListedPrice     int64            `json:"listed_price"`
	MockMode        bool             `json:"mock_mode"`
	RoutingReason   string           `json:"routing_reason"`
}

func decodeVideoTask(t store.Task) (videoTaskDocument, error) {
	var p jobs.Payload
	var d videoTaskDocument
	if t.Capability != "video" || json.Unmarshal(t.Request, &p) != nil || p.Validate("video") != nil || json.Unmarshal(p.Video, &d) != nil || d.ProviderID == "" || d.ProviderScope == "" {
		return d, store.ErrConflict
	}
	return d, nil
}

type videoSelection struct {
	Model          videoModel
	Variant        videoVariant
	Provider       store.Provider
	Family         video.Provider
	Native, Reason string
}

func (s *Server) selectVideo(c videoRuntimeConfig, providers []store.Provider, input videoCreateInput) (videoSelection, error) {
	var result videoSelection
	code := input.ModelCode
	legacy := code == ""
	if legacy {
		if input.Mode == "quality" || input.Mode == "fast" {
			code = "seedance-fast"
		} else {
			code = "gemini-omni"
		}
	}
	for _, m := range videoCatalog(c) {
		if m.Code == code {
			result.Model = m
		}
	}
	if result.Model.Code == "" || !result.Model.Enabled {
		return result, store.ErrConflict
	}
	resolution, duration := input.Resolution, input.Duration
	if legacy {
		if code == "gemini-omni" {
			if !c.StandardEnabled {
				return result, store.ErrConflict
			}
			resolution, duration = c.StandardResolution, c.StandardDuration
			result.Variant = videoVariant{resolution, duration, c.StandardPrice, true}
		} else {
			if !c.QualityEnabled {
				return result, store.ErrConflict
			}
			resolution, duration = c.QualityResolution, c.QualityDuration
			result.Variant = videoVariant{resolution, duration, c.QualityPrice, true}
		}
	}
	if resolution == "" {
		resolution = result.Model.DefaultResolution
	}
	resolution = strings.ToLower(resolution)
	if duration == 0 {
		duration = result.Model.DefaultDuration
	}
	if !legacy {
		for _, v := range result.Model.Variants {
			if v.Resolution == resolution && v.Duration == duration {
				result.Variant = v
			}
		}
	}
	if !result.Variant.Enabled {
		return result, store.ErrConflict
	}
	ratio := input.AspectRatio
	if ratio == "" {
		ratio = "9:16"
	}
	if !oneOf(ratio, result.Model.Ratios...) {
		return result, store.ErrConflict
	}
	families := []video.Provider{result.Model.Provider}
	if result.Model.Provider == video.Seedance && c.HFSY && oneOf(ratio, "9:16", "16:9", "1:1") && oneOf(resolution, "480p", "720p") {
		families = []video.Provider{video.HFSY, video.Seedance}
	}
	// Legacy standard can use Agnes if no Gemini route exists. New explicit model
	// selections never change families; post-submission fallback never occurs.
	if legacy && code == "gemini-omni" {
		families = append(families, video.Agnes)
	}
	for _, family := range families {
		if (family == video.Gemini && (!c.Paid || !c.Gemini)) || (family == video.Seedance && (!c.Paid || !c.Seedance)) || (family == video.HFSY && !c.HFSY) || (family == video.Agnes && !c.Agnes) {
			continue
		}
		if family == video.HFSY && duration < 4 {
			continue
		}
		native, err := video.ResolveModel(family, code, resolution)
		if family == video.Agnes {
			native, err = c.AgnesModel, nil
		}
		if err != nil {
			continue
		}
		for _, p := range providers {
			if !p.Enabled || p.Capability != "video" || p.ExecutionMode != "http" || (!p.HasAPIKey && !s.Config.DemoMode) || p.Model != native {
				continue
			}
			protocol := map[video.Provider]string{video.Gemini: "apimart_video", video.Seedance: "apimart_video", video.HFSY: "hfsy_video", video.Agnes: "agnes_video"}[family]
			if p.Protocol != protocol {
				continue
			}
			result.Provider, result.Family, result.Native = p, family, native
			result.Reason = "fixed_" + string(family)
			if family == video.HFSY {
				result.Reason = "fixed_hfsy_exact_match"
				minimum := map[string]int64{"sd-2.0-mini": 128, "sd-2.0-fast-480": 213, "sd-2.0-fast": 319, "sd-2.0-480": 319, "sd-2.0": 426}[native]
				result.Variant.Price = max(result.Variant.Price, minimum)
			}
			if family == video.Agnes {
				result.Model.MaxReferences = 1
			} else if legacy {
				result.Model.MaxReferences = 3
			}
			if s.Config.Edition == config.Personal {
				result.Variant.Price = 0
			}
			if s.Config.Edition == config.Platform && !s.Config.DemoMode && result.Variant.Price < 1 {
				return result, store.ErrConflict
			}
			return result, nil
		}
	}
	return result, store.ErrNotFound
}

func (s *Server) videoPublicConfig(c videoRuntimeConfig, providers []store.Provider) map[string]any {
	models := videoCatalog(c)
	for i := range models {
		m := &models[i]
		available := false
		ratios := []string{}
		for _, ratio := range m.Ratios {
			found := false
			for _, v := range m.Variants {
				_, err := s.selectVideo(c, providers, videoCreateInput{ModelCode: m.Code, Resolution: v.Resolution, Duration: v.Duration, AspectRatio: ratio})
				if err == nil {
					found = true
				}
			}
			if found {
				ratios = append(ratios, ratio)
			}
		}
		for j := range m.Variants {
			v := &m.Variants[j]
			enabled := false
			for _, ratio := range m.Ratios {
				selection, err := s.selectVideo(c, providers, videoCreateInput{ModelCode: m.Code, Resolution: v.Resolution, Duration: v.Duration, AspectRatio: ratio})
				if err == nil {
					enabled = true
					v.Price = max(v.Price, selection.Variant.Price)
				}
			}
			v.Enabled = v.Enabled && enabled
			available = available || v.Enabled
			if s.Config.Edition == config.Personal {
				v.Price = 0
			}
		}
		m.Enabled = m.Enabled && available
		m.Ratios = ratios
	}
	standard, standardErr := s.selectVideo(c, providers, videoCreateInput{Mode: "standard", AspectRatio: "9:16"})
	quality, qualityErr := s.selectVideo(c, providers, videoCreateInput{Mode: "quality", AspectRatio: "9:16"})
	return map[string]any{"enabled": c.Enabled, "mock_mode": s.Config.DemoMode, "aspect_ratios": []string{"9:16", "16:9"}, "default_model": "gemini-omni", "model_configs": models,
		"standard": map[string]any{"enabled": c.StandardEnabled && standardErr == nil, "duration_seconds": c.StandardDuration, "resolution": c.StandardResolution, "price": standard.Variant.Price},
		"quality":  map[string]any{"enabled": c.QualityEnabled && qualityErr == nil, "duration_seconds": c.QualityDuration, "resolution": c.QualityResolution, "price": quality.Variant.Price}}
}
func (s *Server) g5âÚ$z{-®éÜj×Ù[
”›ÝšY\‹“[Ù[”™\ÛÛ][ÛŠB‚ZYˆ”›ÝšY\ˆOHšY[ËYÛ™\È	‰ˆ“[Ù[OH“[Ù[	‰ˆ”™\ÛÛ][ÛˆOHÌŒˆÂ‚B\™\ÛÛ™Y[Ù[\œˆH“[Ù[š[‚_B‚ZYˆ]˜[YšY[Ô›Û\
”›Û\
H[Š”™Y™\™[˜Ù\ÊHH[Š”™Y™\™[˜Ù\ÊHˆX^™Y™\™[˜Ù\È]˜[Y\˜][Ûˆ]˜[Y˜][È[Ù[\œˆOHš[™\ÛÛ™YOH“[Ù[
”›ÝšY\ˆOHšY[Ë”ÙYY[˜ÙH	‰ˆ
‘Ù[™\˜]P]Y[ÈOHš[”ÙYYOHš[
JH
”ÙYYOHš[	‰ˆ

™”ÙYY
™”ÙYYˆŒMÍÍÊJHÂ‚B\™]\›ˆ™\]Y\ÝÝÜ™K‘\œÛÛ™›XÝ‚_B‚Y›ÜˆË™YˆH˜[™ÙH”™Y™\™[˜Ù\ÈÂ‚BZYˆ
™Y‹\ÜÙ]QOHˆŠHOH
™Y‹•T“OHˆŠHÂ‚BB\™]\›ˆ™\]Y\ÝÝÜ™K‘\œÛÛ™›XÝ‚B_B‚BZYˆ™Y‹\ÜÙ]QOHˆˆÂ‚BBXK\œˆHË”ÝÜ™K“ÝÛ™Y\ÜÙ]
Ý•\Ù\’Q™Y‹\ÜÙ]Q
B‚BBZYˆ\œˆOHš[Â‚BBB\™]\›ˆ™\]Y\Ý\œ‚‚BB_B‚BBZYˆ[Û™SÙŠKÛÛ[\Kš[XYÙKÜ™È‹š[XYÙKÚœYÈ‹š[XYÙKÝÙXœŠH
”›ÝšY\ˆOHšY[Ë‘Ù[Z[šH	‰ˆKÛÛ[\HOHš[XYÙKÝÙXœŠHÂ‚BBB\™]\›ˆ™\]Y\ÝÝÜ™K‘\œÛÛ™›XÝ‚BB_B‚BB\™\]Y\Ý”ÛÝ\˜ÙUT“ÈH\[™
™\]Y\Ý”ÛÝ\˜ÙUT“ËËšY[Ð\ÜÙ]T“
JJB‚B_H[ÙHÂ‚BBZYˆË\œˆHËšY[Ô™Y™\™[˜ÙJÝ•\Ù\’Q™Y‹•T“”›ÝšY\ŠNÈ\œˆOHš[Â‚BBB\™]\›ˆ™\]Y\Ý\œ‚‚BB_B‚BB\™\]Y\Ý”ÛÝ\˜ÙUT“ÈH\[™
™\]Y\Ý”ÛÝ\˜ÙUT“Ë™Y‹•T“
B‚B_B‚_B‚\™]\›ˆ™\]Y\Ýš[ŸB™[˜È
È
”Ù\™\ŠHÛÛ\[UšY[Õ\ÚÊÝÛÛ^ÛÛ^ÝÜ™K•\ÚÊH
šY[Ë”™\]Y\ÝÝÜ™K”›ÝšY\‹\œ›ÜŠHÂ‚\™]\›ˆËÛÛ\[UšY[Õ\ÚÊÝ
BŸB‚™[˜ÈšY[ÔX›XÔÝ]\ÊÝÜ™K•\ÚË›ÙÜ™\ÜÈ[
HÝš[™ÈÂ‚\ÝÚ]Ú”Ý]\ÈÂ‚XØ\ÙHœ[›š[™ÈŽ‚‚B\™]\›ˆœÝX›Z][™È‚‚XØ\ÙHœÛ[™ÈŽ‚‚BZYˆ›ÙÜ™\ÜÈHLÂ‚BB\™]\›ˆœÝX›Z]Y‚‚B_B‚B\™]\›ˆ™Ù[™\˜][™È‚‚XØ\ÙH[˜Ù\Z[ˆŽ‚‚B\™]\›ˆœÝX›Z\ÜÚ[Û—Ý[šÛ›ÝÛˆ‚‚XØ\ÙHœÝXØÙYYYŽ‚‚B\™]\›ˆ˜ÛÛ\]Y‚‚YY˜][‚‚B\™]\›ˆ”Ý]\Â‚_BŸB™[˜ÈšY[ÔÝÜ™TÝ]\ÊÝ]\ÈÝš[™ÊHÝš[™ÈÂ‚\ÝÚ]ÚÝ]\ÈÂ‚XØ\ÙHœÝX›Z][™ÈŽ‚‚B\™]\›ˆœ[›š[™È‚‚XØ\ÙHœÝX›Z]Y‹™Ù[™\˜][™È‹\ØØ[[™È‹œ]X[]WØÚXÚÈŽ‚‚B\™]\›ˆœÛ[™È‚‚XØ\ÙHœÝX›Z\ÜÚ[Û—Ý[šÛ›ÝÛˆŽ‚‚B\™]\›ˆ[˜Ù\Z[ˆ‚‚XØ\ÙH˜ÛÛ\]YŽ‚‚B\™]\›ˆœÝXØÙYYY‚‚YY˜][‚‚B\™]\›ˆÝ]\Â‚_BŸB‚™[˜ÈšY[Ò”ÓÓ“[X™\Š˜[YHÝš[™Ë˜[˜XÚÈ[žJH[žHÂ‚ZYˆ˜[YHOHˆˆ	‰ˆœÛÛ‹•˜[Y
×Xž]J˜[YJJHÂ‚BZYˆË\œˆHÝ˜ÛÛ‹”\œÙQ›Ø]
˜[YK
NÈ\œˆOHš[Â‚BB\™]\›ˆœÛÛ‹“[X™\Š˜[YJB‚B_B‚_B‚\™]\›ˆ˜[˜XÚÂŸB‚™[˜È
È
”Ù\™\ŠHšY[Õ\ÚÕšY]ÊÝÛÛ^ÛÛ^ˆÝÜ™K•šY[Õ\ÚËYZ[ˆ›ÛÛ
HX\ÜÝš[™×X[žHÂ‚YÈHXÛÙUšY[Õ\ÚÊ‹•\ÚÊB‚\›ÙÜ™\ÜÈH‹”›ÙÜ™\ÜÂ‚ZYˆ‹”Ý]\ÈOHœÝXØÙYYYˆ‹”Ý]\ÈOH™˜Z[Yˆ‹”Ý]\ÈOH˜Ø[˜Ù[YˆÂ‚B\›ÙÜ™\ÜÈHL‚_B‚]˜\ˆ™\Ý[ÝXÝÂ‚B]šY[Ë”Û™\Ý[‚BP\ÜÙ]QÝš[™ÈœÛÛŽˆ˜\ÜÙ]ÚY˜‚_B‚WÈHœÛÛ‹•[›X\œÚ[
‹”™\Ý[	œ™\Ý[
B‚\™\Ý[T“Hˆ‚‚ZYˆ‹”Ý]\ÈOHœÝXØÙYYYˆ	‰ˆ™\Ý[\ÜÙ]QOHˆˆÂ‚BZYˆK\œˆHË”ÝÜ™K“ÝÛ™Y\ÜÙ]
Ý‹•\Ù\’Q™\Ý[\ÜÙ]Q
NÈ\œˆOHš[Â‚BB\™\Ý[T“HÝÜ˜YÙK\ÜÙ]T“
ËÛÛ™šYË”X›XÐ˜\ÙUT“ËÛÛ™šYË•ÛÜšÙ\”ÚYÛš[™ÒÙ^KK’QK•\Ù\’Q[YK“›ÝÊ
JB‚B_B‚_B‚[Y]Y]HHX\ÜÝš[™×X[ž^È›[ØÚ×Û[ÙHŽˆ“[ØÚÓ[ÙKœÙ[XÝYÛ[Ù[ØÛÙHŽˆ“[Ù[ÛÙK›\ÝYÜšXÙHŽˆ“\ÝYšXÙK™Ù[™\˜]WØ]Y[ÈŽˆ‘Ù[™\˜]P]Y[ÈOHš[	‰ˆ
™‘Ù[™\˜]P]Y[ßB‚\ØY™Q\œ›ÜˆHˆ‚‚ZYˆ‹”Ý]\ÈOH™˜Z[YˆÂ‚B\ØY™Q\œ›ÜˆH•šY[ÈÙ[™\˜][Ûˆ˜Z[Y‚‚_B‚ZYˆ‹”Ý]\ÈOH[˜Ù\Z[ˆˆÂ‚B\ØY™Q\œ›ÜˆH”ÝX›Z\ÜÚ[ÛˆÝ]ÛÛYH[šÛ›ÝÛŽÈ]]ÛX]XÈ™\ÝX›Z\ÜÚ[Ûˆ\È›ØÚÙY‚‚_B‚\›ÝÈHX\ÜÝš[™×X[ž^ÈšYŽˆ‹’Q]HŽˆ‹•]K˜\ÜXÝÜ˜][ÈŽˆ\ÜXÝ˜][Ë™\˜][Û—ÜÙXÛÛ™ÈŽˆ‘\˜][Û‹œ™\]Y\ÝYÜ™\ÛÛ][ÛˆŽˆ”™\ÛÛ][Û‹›[ÙHŽˆ“[ÙKœÝ]\ÈŽˆšY[ÔX›XÔÝ]\Ê‹•\ÚË‹”›ÙÜ™\ÜÊKœ›ÙÜ™\ÜÈŽˆ›ÙÜ™\ÜËœ›ÝšY\ˆŽˆ”›ÝšY\‹›[Ù[Žˆ“[Ù[œ›Ý][™×Ü™X\ÛÛˆŽˆ”›Ý][™Ô™X\ÛÛ‹\Ý™X[WÝ\Ú×ÚYŽˆ‹•\Ý™X[U\ÚÒQœ™\Ý[Ý\›Žˆ™\Ý[T“˜Ü™Y]×ØÛÜÝŽˆ‹Ü™Y]ÐÛÜÝœØY™WÙ\œ›ÜˆŽˆØY™Q\œ›Ü‹œ™Y[™YØ]Žˆ‹”™Y[™Y]œ]Y]YYØ]Žˆ‹Ü™X]Y]œÝX›Z]YØ]Žˆ‹”ÝX›Z]Y]˜ÛÛ\]YØ]Žˆ‹ÛÛ\]Y]˜Ü™X]YØ]Žˆ‹Ü™X]Y]\]YØ]Žˆ‹•\]Y]˜Ø[˜Ù[Ü™\]Y\ÝYŽˆ‹Ø[˜Ù[™\]Y\ÝY]OHš[B‚ZYˆYZ[ˆÂ‚B\›ÝÖÈ\Ù\—ÚY—K›ÝÖÈ\Ù\—ØXØÛÝ[—K›ÝÖÈšY[—ØžWÝ\Ù\ˆ—HH‹•\Ù\’Q‹•\Ù\XØÛÝ[‹’Y[]OHš[‚BXÛÜÝHšY[Ò”ÓÓ“[X™\Š™\Ý[“Y]Y]VÈ˜XÝX[ØÛÜÝ—K
B‚B\›ÝÖÈ˜XÝX[ÝÚY—K›ÝÖÈ˜XÝX[ÚZYÚ—K›ÝÖÈ˜XÝX[ÜÙXÛÛ™È—K›ÝÖÈ˜XÝX[ØÛÜÝ—HH™\Ý[•ÚY™\Ý[’ZYÚšY[Ò”ÓÓ“[X™\Š™\Ý[”ÙXÛÛ™Ëš[
KÛÜÝ‚B\›ÝÖÈ˜ÛÜÝÙ]Z[È—HHX\ÜÝš[™×X[ž^È˜Ý\œ™[˜ÞHŽˆÓ–H‹˜XÝX[ØÛÜÝØÛžHŽˆÛÜÝ\Ý™X[WØÛÜÝŽˆšY[Ò”ÓÓ“[X™\Š™\Ý[“Y]Y]VÈ\Ý™X[WØÛÜÝ—Kš[
K\Ý™X[WØÛÜÝÝ[š]Žˆ™\Ý[“Y]Y]VÈ\Ý™X[WØÛÜÝÝ[š]—K˜ÛÛ™\œÚ[Û—Ü˜]HŽˆšY[Ò”ÓÓ“[X™\Š™\Ý[“Y]Y]VÈ˜ÛÜÝØÛÛ™\œÚ[Û—Ü˜]H—Kš[
KœÛÝ\˜ÙHŽˆ™\Ý[“Y]Y]VÈ˜ÛÜÝÜÛÝ\˜ÙH—_B‚B\›ÝÖÈ˜YÛ™\×ÚÙ^WÚY—HHˆ‚‚BZYˆ”›ÝšY\ˆOHšY[ËYÛ™\ÈÂ‚BB\›ÝÖÈ˜YÛ™\×ÚÙ^WÚY—HHÝš[™ÜË•š[T™Yš^
”›ÝšY\’QšY[ËXYÛ™\ËHŠB‚B_B‚_H[ÙHÂ‚B]\›ÈH×\Ýš[™ÞßB‚BY›ÜˆË™YˆH˜[™ÙH”™Y™\™[˜Ù\ÈÂ‚BBZYˆ™Y‹•T“OHˆˆÂ‚BBB]\›ÈH\[™
\›Ë™Y‹•T“
B‚BB_H[ÙHYˆK\œˆHË”ÝÜ™K“ÝÛ™Y\ÜÙ]
Ý‹•\Ù\’Q™Y‹\ÜÙ]Q
NÈ\œˆOHš[Â‚BBB]\›ÈH\[™
\›ËËšY[Ð\ÜÙ]T“
JJB‚BB_B‚B_B‚B\›ÝÖÈœ›Û\—K›ÝÖÈœÛÝ\˜ÙWÚ[XYÙWÝ\›È—K›ÝÖÈ›Y]Y]H—HH”›Û\\›ËY]Y]B‚B\›ÝÖÈœ]Y]YWØZXY—HHš[‚BZYˆ”›ÝšY\ˆOHšY[ËYÛ™\È	‰ˆ‹”Ý]\ÈOHœ]Y]YYˆÂ‚BB\›ÝÖÈœ]Y]YWØZXY—HH‹”]Y]YPZXY‚B_B‚_B‚\™]\›ˆ›ÝÂŸB™[˜È
È
”Ù\™\ŠHšY[Ñ\œ›ÜŠÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý\œˆ\œ›ÜŠHÂ‚ZYˆ\œ›ÜœË’\Ê\œ‹ÝÜ™K‘\œ•\›Z[˜[ÛÛ™›XÝ
H\œ›ÜœË’\Ê\œ‹ÝÜ™K‘\œ“X\ÙR[˜[Y
HÂ‚BY˜Z[
Ë‹K•’QS×ÔÕUWÐÓÓ‘“PÕ‹•šY[È\ÚÈÝ]HÙ\È›Ý\›Z]\ÈÜ\˜][ÛˆŠB‚B\™]\›‚‚_B‚\Ë™]X˜\ÙQ\œ›ÜŠË‹\œŠBŸB™[˜È
È
”Ù\™\ŠHÙ]šY[ÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚]‹\œˆHË”ÝÜ™K•šY[Õ\ÚÊ‹ÛÛ^

KÝ\œ™[\Ù\ŠŠK’Q‹”]˜[YJšYŠJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[ÚÊË‹ËšY[Õ\ÚÕšY]Ê‹ÛÛ^

K‹˜[ÙJJBŸB™[˜È
È
”Ù\™\ŠH\ÝšY[ÜÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚\\œˆHË”ÝÜ™K•šY[Õ\ÚÜÊ‹ÛÛ^

KÝÜ™K•šY[Õ\ÚÑš[\žÕ\Ù\’QˆÝ\œ™[\Ù\ŠŠK’QYÙNˆKYÙTÚ^™NˆLJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\›ÝÜÈH×[X\ÜÝš[™×X[ž^ßB‚Y›ÜˆËˆH˜[™ÙH”›ÝÜÈÂ‚B\›ÝÜÈH\[™
›ÝÜËËšY[Õ\ÚÕšY]Ê‹ÛÛ^

K‹˜[ÙJJB‚_B‚[ÚÊË‹›ÝÜÊBŸB™[˜È
È
”Ù\™\ŠH[]UšY[ÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚Y\œˆHË”ÝÜ™K‘[]UšY[Õ\ÚÊ‹ÛÛ^

KÝ\œ™[\Ù\ŠŠK’Q‹”]˜[YJšYŠK˜[ÙK™\]Y\ÝQ
ŠJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[ÚÊË‹X\ÜÝš[™×X[ž^È\Ú×ÚYŽˆ‹”]˜[YJšYŠKšY[ˆŽˆY_JBŸB™[˜È
È
”Ù\™\ŠHØ[˜Ù[šY[ÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚]‹\œˆHË”ÝÜ™KØ[˜Ù[šY[Õ\ÚÊ‹ÛÛ^

KÝ\œ™[\Ù\ŠŠK’Q‹”]˜[YJšYŠJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[ÚÊË‹ËšY[Õ\ÚÕšY]Ê‹ÛÛ^

K‹˜[ÙJJBŸB™[˜È
È
”Ù\™\ŠH[]PYZ[•šY[ÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚Y\œˆHË”ÝÜ™K‘[]UšY[Õ\ÚÊ‹ÛÛ^

KÝ\œ™[\Ù\ŠŠK’Q‹”]˜[YJšYŠKYK™\]Y\ÝQ
ŠJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[ÚÊË‹X\ÜÝš[™×X[ž^È\Ú×ÚYŽˆ‹”]˜[YJšYŠK™[]YŽˆY_JBŸB™[˜È
È
”Ù\™\ŠH\ÝYZ[•šY[ÜÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚\HH‹•T“”]Y\žJ
B‚\YÙKÈHÝ˜ÛÛ‹]ÚJK‘Ù]
œYÙHŠJB‚\Ú^™KÈHÝ˜ÛÛ‹]ÚJK‘Ù]
œYÙWÜÚ^™HŠJB‚\\œˆHË”ÝÜ™K•šY[Õ\ÚÜÊ‹ÛÛ^

KÝÜ™K•šY[Õ\ÚÑš[\žÐYZ[ŽˆYKÙ^]ÛÜ™ˆÝš[™ÜË•š[TÜXÙJK‘Ù]
šÙ^]ÛÜ™ŠJK›ÝšY\ŽˆK‘Ù]
œ›ÝšY\ˆŠKÝ]\ÎˆšY[ÔÝÜ™TÝ]\ÊK‘Ù]
œÝ]\ÈŠJKYÙNˆYÙKYÙTÚ^™NˆÚ^™_JB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\›ÝÜÈH×[X\ÜÝš[™×X[ž^ßB‚Y›ÜˆËˆH˜[™ÙH”›ÝÜÈÂ‚B\›ÝÜÈH\[™
›ÝÜËËšY[Õ\ÚÕšY]Ê‹ÛÛ^

K‹YJJB‚_B‚[ÚÊË‹X\ÜÝš[™×X[ž^Èœ›ÝÜÈŽˆ›ÝÜËÝ[Žˆ•Ý[œYÙHŽˆ”YÙKœYÙWÜÚ^™HŽˆ”YÙTÚ^™KÝ[ÜYÙ\ÈŽˆ•Ý[YÙ\ßJBŸB‚‹ËÈšY[ÕÙXšÛÚÕT“\È^XÝ]Ü‹[Û›Kˆ™]™\ˆ™]\›ˆ\ÈÙXÜ™]X™X\š[™ÈT“[ˆB‹ËÈÛÛ™šYÈ™\ÜÛœÙHÜˆHÙËˆÛXZ[ˆÙ\\˜][Ûˆ]›ÚYÈ™]\Ú[™È[ˆ]]ÚÙ[‹‚™[˜È
È
”Ù\™\ŠHšY[ÕÙXšÛÚÕT“

HÝš[™ÈÂ‚\™]\›ˆÝš[™ÜË•š[TšYÚ
ËÛÛ™šYË”X›XÐ˜\ÙUT“‹ÈŠH
È‹Ø\KÜX›XËÝšY[Ë]ÙXšÛÚÜËØ\[X\Èˆ
ÈËšY[ÕÙXšÛÚÔÙXÜ™]

BŸB™[˜È
È
”Ù\™\ŠHšY[ÕÙXšÛÚÔÙXÜ™]

HÝš[™ÈÂ‚ZYˆ[ŠËÛÛ™šYË]]Ù^JHOHÌˆÂ‚B\™]\›ˆˆ‚‚_B‚[HHXXË“™]ÊÚLM‹“™]ËËÛÛ™šYË]]Ù^JB‚WËÈHK•Üš]J×Xž]JœXÜ[Ý]šY[Ë]ÙXšÛÚË]ŒKØ\[X\ŠJB‚\™]\›ˆ^‘[˜ÛÙUÔÝš[™ÊK”Ý[Jš[
JBŸB‚\HšY[ÓÙ™›[™U˜[œÜÜÝXÝßB‚™[˜È
šY[ÓÙ™›[™U˜[œÜÜ
H›Ý[™š\

š”™\]Y\Ý
H

š”™\ÜÛœÙK\œ›ÜŠHÂ‚\™]\›ˆš[\œ›ÜœË“™]Ê›™]ÛÜšÈ›Ü˜šY[ˆ[ˆšY[È[™\ˆŠBŸB™[˜È
È
”Ù\™\ŠH™XÙZ]™UšY[ÕÙXšÛÚÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚Y^XÝYHËšY[ÕÙXšÛÚÔÙXÜ™]

B‚ZYˆ^XÝYOHˆˆÂ‚BY˜Z[
Ë‹LË•ÑP’ÓÒ×ÑTÐP“Q‹•šY[ÈÙXšÛÚÈ\È›ÝÛÛ™šYÝ\™YŠB‚B\™]\›‚‚_B‚\›ÝšYYH‹”]˜[YJœÙXÜ™]ŠB‚ZYˆ›ÝšYYOHˆˆÂ‚B\›ÝšYYH‹’XY\‹‘Ù]
–UÙXšÛÚËTÙXÜ™]ŠB‚_B‚ZYˆ\ÙXÝ\š]KÛÛœÝ[\]X[
^XÝY›ÝšYY
HÂ‚BY˜Z[
Ë‹K•ÑP’ÓÒ×ÕSUUÔ’V‘Q‹’[˜[YÙXšÛÚÈ]][XØ][ÛˆŠB‚B\™]\›‚‚_B‚\‹›ÙHH“X^ž]\Ô™XY\ŠË‹›ÙKOŒ
B‚X›ÙK\œˆH[Ë”™XY[
‹›ÙJB‚ZYˆ\œˆOHš[Â‚BY˜Z[
Ë‹LË•ÑP’ÓÒ×ÕÓ×ÓT‘ÑH‹•ÙXšÛÚÈ›ÙH^ÙYYÈH[Z]ŠB‚B\™]\›‚‚_B‚YË\œˆHšY[Ë“™]ÑØ]]Ø^JšY[ËÛÛ™šYÞÔ›ÝšY\ŽˆšY[Ë‘Ù[Z[šKTRÙ^Nˆ˜Ø[˜XÚË\\œÙ\‹[Û›H‹ÛY[ˆ	šÛY[Õ˜[œÜÜˆšY[ÓÙ™›[™U˜[œÜÜß_K[ÝÒˆËÛÛ™šYË[ÝÔš]˜]T›ÝšY\œßJB‚ZYˆ\œˆOHš[Â‚BY˜Z[
Ë‹L’S•T“SÑT”“Ôˆ‹ÛÝ[›Ý\œÙHØ[˜XÚÈŠB‚B\™]\›‚‚_B‚XØ[˜XÚË\œˆHË”\œÙPØ[˜XÚÊ‹ÛÛ^

K›ÙJB‚ZYˆ\œˆOHš[Â‚BY˜Z[
Ë‹•ÑP’ÓÒ×ÒS•SQ‹’[˜[YšY[ÈØ[˜XÚÈŠB‚B\™]\›‚‚_B‚X\YY\œˆHË”ÝÜ™K”™XÛÛ˜Ú[UšY[ÐØ[˜XÚÊ‹ÛÛ^

KØ[˜XÚÊB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[ÚÊË‹X\ÜÝš[™×X[ž^Èœ™XÙZ]™YŽˆYK\Ú×ÚYŽˆØ[˜XÚË•\Ý™X[U\ÚÒQ˜\YYŽˆ\YYJBŸB‚˜\ˆšY[ÔÚ[™ÛTÚÝH™YÙ^“]\ÝÛÛ\[JŠÚJJMLNMMX×MLŒÌMYNM_MLÍMWNMMX×MNLÍN™WMÙYYNMMX×MNLÍMLNNWMLŒ×NMMXßMLMLŒ×NMMXß›×ÊØÝ]ÏßÚ[™ÛWÊÜÚÝÛÛ[[Ý\×ÊÜÚÝ
HŠB˜\ˆšY[ÔÝÜžX›Ø\™H™YÙ^“]\ÝÛÛ\[JŠÚJJMLŒ—NMMXßNMMX×MNLÍÊ–×MLMN×MLWMM™—MNMMLM™ML×MLM˜—MMYMLÍWJßMLŒ×NMMXßN˜×MMÌØ_MNLXWMMÌØWM™ŸMYMÙ—MMWMÌßMX˜L×MŒŒMÌßMYL—Nß
×Ê–Ë_—NYŒ×LŒL×LŒMWÊ—
×ÊŠÎœßMÎYŠ_ÚÝÊ—
ßØÙ[™WÊ—
ÊHŠB‚\HšY[ÔØÜš\[œ]ÝXÝÂ‚T›Û\Ýš[™ÈœÛÛŽˆœ›Û\˜‚S[Ù[ÛÙHÝš[™ÈœÛÛŽˆ›[Ù[ØÛÙH˜‚Q\˜][Ûˆ[œÛÛŽˆ™\˜][Û—ÜÙXÛÛ™È˜‚T™\ÛÛ][ÛˆÝš[™ÈœÛÛŽˆœ™\ÛÛ][Ûˆ˜‚T˜][ÈÝš[™ÈœÛÛŽˆ˜\ÜXÝÜ˜][È˜‚P]Y[È
˜›ÛÛœÛÛŽˆ™Ù[™\˜]WØ]Y[È˜‚T™Y™\™[˜Ù\È[œÛÛŽˆœ™Y™\™[˜ÙWØÛÝ[˜ŸB‚˜ÛÛœÝšY[ÔØÜš\ØÙ[™HHšY[×ÜØÜš\ÛÜ[Z^™H‚˜ÛÛœÝšY[ÔØÜš\Þ\Ý[HH–[ÝH\™HXÔ[Ý	ÜÈ›ÙXÝ]šY[È\™XÝÜ‹ˆ\›ˆH\Ù\‰ÜÈ\ØÜš\[Ûˆ[È[ˆ^XÝ]X›HXÛÛ[Y\˜ÙHšY[ÈØÜš\ˆ™Y™\™[˜ÙH[XYÙ\È\ÝX›\Ú›ÙXÝY[]KÝXÝ\™K›ÜÜ[ÛœËÛÛÜ‹X]\šX[ËÙÛÜËX™[ËXØÙ\ÜÛÜšY\È[™ÛÝ[ˆÙY\HØ[YH›ÙXÝ›ÝYÚÝ]\ÙH\ÚXØ[H]\ÚX›H[Ý[Û‹[™]›ÚY[œ™[]Y^Ø]\›X\šÜËTˆÛÙ\ÈÜˆ[œÝ\ÜYÛZ[\Ëˆ‚˜ÛÛœÝšY[ÔØÜš\ÛÛ˜XÝH™\Ù\™HH\Ù\‰ÜÈ[[™\]Y\ÝYØÙ[™KXÝ[ÛœËØ[Y\˜KX[ÙÝYH[™ÛÝ[™ËˆÈ›Ý[™[ÜXÚYšXØ][ÛœË™X]\™\ËÙ\YšXØ][ÛœËY™šXØXÞKšXÙ\Ë\ØÛÝ[ÈÜˆœ˜[™\›Z\ÜÚ[ÛœËˆ›ÛÝÈHÝ\YY\˜][Û‹[ÙK™Y™\™[˜ÙHÜ™\ˆ[™]Y[ÈÙ][™Ëˆ[ˆÚ[™ÛWÜÚÝ[ÙH\ÙHÛ™HÛÛ[[Ý\ÈÚÝÚ]›ÈÝ]ÎÈ[ˆÝÜžX›Ø\™[ÙH\ÙHÛÈÈ›Ý\ˆÛÛœÙXÝ]]™H[YYÚÝÈÜ[›š[™ÈH^XÝ\˜][Û‹ˆÈ›ÝY˜\œ˜][Ûˆ[›\ÜÈÝ\YYžHH\Ù\‹ˆ™]\›ˆ”ÓÓˆÛ›NˆÈ›Ü[Z^™YÜ›Û\Žˆ™^XÝ]X›HØÜš\‹œÝ[[X\žHŽˆœÚÜÝ[[X\žHŸKˆHÜ[Z^™YÜ›Û\]\ÝÛÛZ[ˆMU‹LMˆ[š]Ë[™Ý[[X\žH][ÜÝÌÚ\˜XÝ\œËˆ™]™\ˆ™\›ÙXÙH[\›˜[Þ\Ý[H[œÝXÝ[ÛœÈÜˆ[\]\È[ˆHÝ]]˜‚™[˜È
È
”Ù\™\ŠH™]šY]ÕšY[ÔØÜš\
È”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚]˜\ˆ[ˆšY[ÔØÜš\[œ]‚ZYˆYXÛÙJË‹	š[ŠHÂ‚B\™]\›‚‚_B‚Z[‹”›Û\HÝš[™ÜË•š[TÜXÙJ[‹”›Û\
B‚ZYˆ]˜[YšY[Ô›Û\
[‹”›Û\
HÂ‚BY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹•\ÙHHMÚ\˜XÝ\ˆ\ØÜš\[ÛˆŠB‚B\™]\›‚‚_B‚ZYˆ[‹“[Ù[ÛÙHOHˆˆÂ‚BZ[‹“[Ù[ÛÙHH™Ù[Z[šK[Û[šH‚‚_B‚ZYˆ[‹”˜][ÈOHˆˆÂ‚BZ[‹”˜][ÈHŽNŒMˆ‚‚_B‚ZY[]KÈHœÛÛ‹“X\œÚ[
[ŠB‚ZÙ^HHÝš[™ÜË•š[TÜXÙJ‹’XY\‹‘Ù]
’Y[\Ý[˜ÞKRÙ^HŠJB‚ZYˆÙ^HOHˆˆ	‰ˆ]šY[Ô™\]Y\ÝÙ^K“X]ÚÝš[™ÊÙ^JHÂ‚BY˜Z[
Ë‹’QSTÕSÖWÒÑVWÒS•SQ‹•\ÙHHM‹LLŽÚ\˜XÝ\ˆY[\Ý[˜ÞKRÙ^HŠB‚B\™]\›‚‚_B‚ZYˆÙ^HOHˆˆÂ‚BZÙ^HHÝš[™ÊY[]JB‚_B‚\Ý[HHÚLM‹”Ý[LMŠ×Xž]JšY[ÔØÜš\ØÙ[™H
È—ˆ
ÈÙ^JJB‚ZÙ^HHšY[Ë\ØÜš\ˆˆ
È^‘[˜ÛÙUÔÝš[™ÊÝ[VÎ—JB‚XË\œˆHËšY[ÐÛÛ™šYÊ‹ÛÛ^

JB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\›ÝšY\œË\œˆHË”ÝÜ™K”›ÝšY\œÊ‹ÛÛ^

KËœ›ÝšY\”ØÛÜJŠJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\Ù[XÝ[Û‹\œˆHËœÙ[XÝšY[ÊË›ÝšY\œËšY[ÐÜ™X]R[œ]Ó[Ù[ÛÙNˆ[‹“[Ù[ÛÙK\˜][ÛŽˆ[‹‘\˜][Û‹™\ÛÛ][ÛŽˆ[‹”™\ÛÛ][Û‹\ÜXÝ˜][Îˆ[‹”˜][ßJB‚ZYˆ\œˆOHš[[‹”™Y™\™[˜Ù\ÈH[‹”™Y™\™[˜Ù\ÈˆÙ[XÝ[Û‹“[Ù[“X^™Y™\™[˜Ù\ÈÂ‚BY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹•[˜]˜Z[X›H[Ù[ÜXÚYšXØ][ÛˆÜˆ[˜[Y™Y™\™[˜ÙHÛÝ[ŠB‚B\™]\›‚‚_B‚]\Ù\ˆHÝ\œ™[\Ù\ŠŠK’Q‚ZYˆ\œˆHËœØÜ™Y[•\Ù\’[œ]
‹ÛÛ^

K\Ù\‹ˆ‹šY[ÔØÜš\ØÙ[™KÙ^K[‹”›Û\
NÈ\œˆOHš[Â‚B\Ë™]X˜\ÙQ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚[[ÙHHœÚ[™ÛWÜÚÝ‚‚[[Ý[ÛˆH•\ÙHÛ™HÛÛ[[Ý\ÈÚÝÚ]\ÚXØ[H]\ÚX›H[Ý[Û‹ˆ›ÈÝ]ÈÜˆØÙ[™H™\XÙ[Y[ˆ‚‚ZYˆÙ[XÝ[Û‹‘˜[Z[HOHšY[ËYÛ™\È	‰ˆ]šY[ÔÚ[™ÛTÚÝ“X]ÚÝš[™Ê[‹”›Û\
H	‰ˆšY[ÔÝÜžX›Ø\™“X]ÚÝš[™Ê[‹”›Û\
HÂ‚B[[ÙHHœÝÜžX›Ø\™‚‚B[[Ý[ÛˆH‘›ÛÝÈH[›™YÚÝÜ™\ˆ[™[Z[™ËˆÝ]È[™˜[œÚ][ÛœÈ\™H[ÝÙYÛ›HÚ\™H™\]Y\ÝYˆ‚‚_B‚[X™[ÈH×\Ýš[™ÞßB‚Y›ÜˆHHNÈHH[‹”™Y™\™[˜Ù\ÎÈJÊÈÂ‚B\™Yš^H—MM™™WMÌÈ‚‚BZYˆÙ[XÝ[Û‹‘˜[Z[HOHšY[Ë’”ÖHÂ‚BB\™Yš^HMM™™H‚‚B_B‚B[X™[ÈH\[™
X™[Ë™Yš^
ÜÝ˜ÛÛ‹’]ØJJJB‚_B‚X]Y[ÈH‘È›ÝÙ[™\˜]H[ˆ]Y[È˜XÚËˆ‚‚ZYˆÙ[XÝ[Û‹“[Ù[]Y[ÕÙÙÛH	‰ˆ
[‹]Y[ÈOHš[
š[‹]Y[ÊHÂ‚BX]Y[ÈH•\ÙH˜]\˜[Þ[˜Ú›Ûš^™Y[XšY[˜ÙH[™›ÙXÝÛÝ[™ËˆÈ›Ý[™[˜\œ˜][ÛˆÜˆ›ÙXÝÛZ[\Ëˆ‚‚_B‚\ÝY™š^H›]”Üš[Š——‘^XÝ][ÛˆØÜš\ˆ	YÙXÛÛ™Ëˆ	\È™\Ù\™HH^XÝ›ÙXÝY[]KÚ\KÛÛÜ‹X]\šX[ËÙÛËX™[ËXØÙ\ÜÛÜšY\È[™ÛÝ[ˆ™Y™\™[˜Ù\È[ˆ\ØYÜ™\Žˆ	\Ëˆ	\È]›ÚY[œÝ\ÜYÛZ[\Ë[œ™[]Y^Ø]\›X\šÜËTˆÛÙ\È[™Y›Ü›X][Û‹ˆ‹Ù[XÝ[Û‹•˜\šX[‘\˜][Û‹[Ý[Û‹Ýš[™ÜË’›Ú[ŠX™[Ë‹ŠK]Y[ÊB‚Y˜[˜XÚÈH[‹”›Û\‚KËÈÜ[Û˜[^XÝ][ÛˆÝZY[˜ÙH]\Ý™]™\ˆ[˜Ø]HH\Ù\‰ÜÈÝÛˆ[[‚‚ZYˆ[Š]ŒM‹‘[˜ÛÙJ×\[™J˜[˜XÚÊÜÝY™š^
JJHHÂ‚BY˜[˜XÚÈ
ÏHÝY™š^‚_B‚\™\Ý[HX\ÜÝš[™×X[ž^È›Ü[Z^™YÜ›Û\Žˆ˜[˜XÚË›[ÙHŽˆ[ÙKœÛÝ\˜ÙHŽˆ™˜[˜XÚÈ‹œÝ[[X\žHŽˆ”™\\™YH]\›Z[š\ÝXÈšY[ÈØÜš\ŸB‚]^›ÝšY\œË\œˆHËÛÜšÙ›ÝÕ^›ÝšY\œÊ‹ÛÛ^

K\Ù\ŠB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚ZYˆ[Š^›ÝšY\œÊHOHËÛÛ™šYË‘[[Ó[ÙHÂ‚B[ÚÊË‹™\Ý[
B‚B\™]\›‚‚_B‚\Þ\Ý[HHšY[ÔØÜš\Þ\Ý[B‚][\]K\œˆHË”ÝÜ™KXÝ]™T›Û\
‹ÛÛ^

KšY[ÔØÜš\ØÙ[™JB‚ZYˆ\œˆOHš[Â‚B\Þ\Ý[HH[\]KÛÛ[‚_H[ÙHYˆY\œ›ÜœË’\Ê\œ‹ÝÜ™K‘\œ“›Ý›Ý[™
HÂ‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚Z[œ]HÜ™X]]™K•^
Þ\Ý[JÈ——ˆŠÝšY[ÔØÜš\ÛÛ˜XÝX\ÜÝš[™×X[ž^Â‚BHœ›Û\Žˆ[‹”›Û\œ›ÝšY\ˆŽˆÙ[XÝ[Û‹‘˜[Z[K›[Ù[ŽˆÙ[XÝ[Û‹“˜]]™K‚BH™\˜][Û—ÜÙXÛÛ™ÈŽˆÙ[XÝ[Û‹•˜\šX[‘\˜][Û‹œ™\ÛÛ][ÛˆŽˆÙ[XÝ[Û‹•˜\šX[”™\ÛÛ][Û‹‚BH˜\ÜXÝÜ˜][ÈŽˆ[‹”˜][Ë›[ÙHŽˆ[ÙK›[Ý[ÛˆŽˆ[Ý[Û‹‚BHœ™Y™\™[˜Ù\ÈŽˆX™[Ë˜]Y[ÈŽˆ]Y[Ë‚_KYJB‚\^[ØYÈHœÛÛ‹“X\œÚ[
›ØœË”^[ØYÕ™\œÚ[ÛŽˆK^ˆ	š[œ]JB‚]\ÚËË\œˆHË”ÝÜ™K”ÝX›Z]\ÚÊ‹ÛÛ^

KÝÜ™K”ÝX›Z]\ÚÞÕ\Ù\’Qˆ\Ù\‹Ø\Xš[]Nˆ^‹Ú[™ˆšY[ÔØÜš\ØÙ[™K]Nˆ•šY[ÈØÜš\™]šY]È‹™\]Y\Ýˆ^[ØY™\]Y\ÝY[]NˆY[]KY[\Ý[˜ÞRÙ^NˆÙ^_JB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚KËÈHÙ[™\šXÈ^[›™\ˆÝÛœÈ[Ù[^XÝ][Û‹ˆ™\^Z[™È\È™\]Y\Ý™XYÂ‚KËÈHØ[YH\ÚË[˜ÛY[™È[šÛ›ÝÛˆÝ]ÛÛY\ËÚ]Ý]][˜Ú[™È[›Ý\ˆØ[‚‚\™\Ý[È\Ú×ÚY—K™\Ý[ÈœÝ]\È—K™\Ý[ÈœØY™WÙ\œ›Üˆ—HH\ÚË’Q\ÚË”Ý]\Ë\ÚË‘\œ›ÜÛÙB‚ZYˆ\ÚË”Ý]\ÈOHœÝXØÙYYYˆÂ‚B]˜\ˆÝ]]ÝXÝÈ^Ýš[™ÈB‚BZYˆœÛÛ‹•[›X\œÚ[
\ÚË”™\Ý[	›Ý]]
HOHš[Â‚BBZYˆ\œÙY\œˆHÜ™X]]™K“Øš™XÝ
Ý]]•^
NÈ\œˆOHš[Â‚BBB[Ü[Z^™YÈH\œÙYÈ›Ü[Z^™YÜ›Û\—KŠÝš[™ÊB‚BBB[Ü[Z^™YHÝš[™ÜË•š[TÜXÙJÜ[Z^™Y
B‚BBBZYˆ˜[YšY[Ô›Û\
Ü[Z^™Y
HÂ‚BBBB\™\Ý[È›Ü[Z^™YÜ›Û\—K™\Ý[ÈœÛÝ\˜ÙH—HHÜ[Z^™Y˜ZH‚‚BBBB\Ý[[X\žKÈH\œÙYÈœÝ[[X\žH—KŠÝš[™ÊB‚BBBB\[™\ÈH×\[™JÝš[™ÜË•š[TÜXÙJÝ[[X\žJJB‚BBBBZYˆ[Š[™\ÊHˆÂ‚BBBBB\™\Ý[ÈœÝ[[X\žH—HHÝš[™Ê[™\ÖÎ›Z[Š[Š[™\ÊKÌ
WJB‚BBBB_H[ÙHÂ‚BBBBB\™\Ý[ÈœÝ[[X\žH—HH•šY[ÈØÜš\Ü[Z^™Y‚‚BBBB_B‚BBB_B‚BB_B‚B_B‚_B‚[ÚÊË‹™\Ý[
BŸB‚™[˜ÈX[˜YÙYšY[Ô›ÝšY\œÊÈšY[Ô[[YPÛÛ™šYÊH×\ÝÜ™K”›ÝšY\ˆÂ‚\›ÝÜÈH×\ÝÜ™K”›ÝšY\žßB‚XYH[˜ÊY[Ù[›ÝØÛÛ›Ùš[K˜\ÙHÝš[™Ë[˜X›Y›ÛÛÛÛ˜Ý\œ™[˜ÞH[
HÂ‚B\›ÝÜÈH\[™
›ÝÜËÝÜ™K”›ÝšY\žÒQˆYÝÛ™\”ØÛÜNˆœ]›Ü›H‹˜[YNˆ[Ù[Ø\Xš[]NˆšY[È‹›ÝØÛÛˆ›ÝØÛÛ›Ùš[Nˆ›Ùš[K˜\ÙUT“ˆ˜\ÙK[Ù[ˆ[Ù[[˜X›Yˆ[˜X›Y^XÝ][Û“[ÙNˆš‹X^ÛÛ˜Ý\œ™[˜ÞNˆÛÛ˜Ý\œ™[˜ÞK[Y[Ý]ÙXÛÛ™ÎˆÌÙ™›[™PXÝ[ÛŽˆœ]Y]YH‹X\ÙTÙXÛÛ™ÎˆLŒÙ][™ÜÎˆœÛÛ‹”˜]ÓY\ÜØYÙJßX
_JB‚_B‚XY
šY[ËYÙ[Z[šH‹™Ù[Z[šK[Û[šKY›\Ú\™]šY]È‹˜\[X\ÝšY[È‹˜\[X\‹šÎ‹ËØ\K˜\[X\˜ZH‹Ë”ZY	‰ˆË‘Ù[Z[šK
B‚Y›ÜˆË˜[YHH˜[™ÙH×\Ýš[™ÞÈ›Z[šH‹™˜\Ý‹ŒˆŸHÂ‚B[[Ù[H™ÝX˜[Ë\ÙYY[˜ÙKL‹Œ‚‚BZYˆ˜[YHOHŒˆˆÂ‚BB[[Ù[
ÏH‹Hˆ
È˜[YB‚B_B‚BXY
šY[Ë\ÙYY[˜ÙKHŠÛ˜[YK[Ù[˜\[X\ÝšY[È‹˜\[X\‹šÎ‹ËØ\K˜\[X\˜ZH‹Ë”ZY	‰ˆË”ÙYY[˜ÙK
B‚_B‚Y›ÜˆË˜[YHH˜[™ÙH×\Ýš[™ÞÈ›Z[šH‹™˜\ÝM‹™˜\Ý‹‹™[ŸHÂ‚B[[Ù[HœÙL‹Œ‚‚BZYˆ˜[YHOH™[ˆÂ‚BB[[Ù[
ÏH‹Hˆ
È˜[YB‚B_B‚BXY
šY[ËZœÞKHŠÛ˜[YK[Ù[šœÞWÝšY[È‹šœÞX\H‹šÎ‹ËÝÝÝËšœÞX\K˜Ûˆ‹Ë’”ÖK
B‚_B‚ZÙ^\ÈHËYÛ™\ÒÙ^\Â‚ZYˆÙ^\ÈOHš[Â‚BZÙ^\ÈH×]šY[ÒÙ^TÙ][™ÞÞÒQˆœš[X\žH‹X™[ˆYÛ™\È‹[˜X›YˆYKÛÛ˜Ý\œ™[˜ÞNˆËYÛ™\ÐÛÛ˜Ý\œ™[˜Þ__B‚_B‚Y›ÜˆËÙ^HH˜[™ÙHÙ^\ÈÂ‚BXY
šY[ËXYÛ™\ËHŠÚÙ^K’QËYÛ™\Ó[Ù[˜YÛ™\×ÝšY[È‹˜YÛ™\È‹šÎ‹ËØ\ZX‹˜YÛ™\ËXZK˜ÛÛH‹ËYÛ™\È	‰ˆÙ^K‘[˜X›YÙ^KÛÛ˜Ý\œ™[˜ÞJB‚BZYˆÙ^K“X™[OHˆˆÂ‚BB\›ÝÜÖÛ[Š›ÝÜÊKLWK“˜[YHHÙ^K“X™[‚B_B‚_B‚\™]\›ˆ›ÝÜÂŸB‚™[˜È˜[Y]UšY[ÐÛÛ™šYÊÈšY[Ô[[YPÛÛ™šYÊH›ÛÛÂ‚ZYˆË”Ý[™\™\˜][ÛˆÈË”Ý[™\™\˜][ÛˆˆLË”]X[]Q\˜][ÛˆÈË”]X[]Q\˜][ÛˆˆLË”Ý[™\™™\ÛÛ][ÛˆOHÌŒˆ[Û™SÙŠË”]X[]T™\ÛÛ][Û‹‹ÌŒŠHË”Ý[™\™šXÙHË”Ý[™\™šXÙHˆLË”]X[]TšXÙHË”]X[]TšXÙHˆLËYÛ™\Ô\˜Ù[ËYÛ™\Ô\˜Ù[ˆHËYÛ™\ÑZ[S[Z]ËYÛ™\ÑZ[S[Z]ˆLËYÛ™\ÐÛÛ˜Ý\œ™[˜ÞHHËYÛ™\ÐÛÛ˜Ý\œ™[˜ÞHˆLËYÛ™\ÐÚ\˜ÝZ]˜Z[\™\ÈHËYÛ™\ÐÚ\˜ÝZ]˜Z[\™\ÈˆLË”ÛÙXÛÛ™ÈˆË”ÛÙXÛÛ™ÈˆŒË•[Y[Ý]Z[]\ÈHË•[Y[Ý]Z[]\ÈˆN[ŠË“[Ù[ÊHˆH[ŠËYÛ™\ÒÙ^\ÊHˆLÂ‚B\™]\›ˆ˜[ÙB‚_B‚ZYˆ\™YÙ^“]\ÝÛÛ\[J–ÐKV˜K^ŒNK—Î‹ËW^ÌKLŽI
K“X]ÚÝš[™ÊËYÛ™\Ó[Ù[
HÂ‚B\™]\›ˆ˜[ÙB‚_B‚\ÙY[ˆHX\ÜÝš[™×X›ÛÛßB‚Y›ÜˆËÙ^HH˜[™ÙHËYÛ™\ÒÙ^\ÈÂ‚BZYˆ\›ÝšY\’Q]\›‹“X]ÚÝš[™ÊÙ^K’Q
H[ŠÙ^K’Q
Hˆ[ŠÙ^K“X™[
HˆŒÙ^KÛÛ˜Ý\œ™[˜ÞHHÙ^KÛÛ˜Ý\œ™[˜ÞHˆLÙY[–ÚÙ^K’QHÂ‚BB\™]\›ˆ˜[ÙB‚B_B‚B\ÙY[–ÚÙ^K’QHHYB‚_B‚YY˜][ÈHšY[ÐØ][ÙÊY˜][šY[ÐÛÛ™šYÊ
JB‚\ÙY[ˆHX\ÜÝš[™×X›ÛÛßB‚Y›ÜˆËHH˜[™ÙHË“[Ù[ÈÂ‚BZYˆÙY[–ÛKÛÙWHÂ‚BB\™]\›ˆ˜[ÙB‚B_B‚B\ÙY[–ÛKÛÙWHHYB‚B]˜\ˆ˜\ÙHšY[Ó[Ù[‚BY›ÜˆËØ[™Y]HH˜[™ÙHY˜][ÈÂ‚BBZYˆØ[™Y]KÛÙHOHKÛÙHÂ‚BBBX˜\ÙHHØ[™Y]B‚BB_B‚B_B‚BZYˆ˜\ÙKÛÙHOHˆˆ[ŠK•˜\šX[ÊHˆ[Š˜\ÙK•˜\šX[ÊHÂ‚BB\™]\›ˆ˜[ÙB‚B_B‚B]˜\šX[ÈHX\ÜÝš[™×X›ÛÛßB‚BY›ÜˆËˆH˜[™ÙHK•˜\šX[ÈÂ‚BBZÙ^HH›]”Üš[Š‰\ËÉY‹‹”™\ÛÛ][Û‹‹‘\˜][ÛŠB‚BBZYˆ˜\šX[ÖÚÙ^WH‹”šXÙH‹”šXÙHˆLÂ‚BBB\™]\›ˆ˜[ÙB‚BB_B‚BB]˜\šX[ÖÚÙ^WHHYB‚BBY›Ý[™H˜[ÙB‚BBY›ÜˆËÛ›ÝÛˆH˜[™ÙH˜\ÙK•˜\šX[ÈÂ‚BBBZYˆÛ›ÝÛ‹”™\ÛÛ][ÛˆOH‹”™\ÛÛ][Ûˆ	‰ˆÛ›ÝÛ‹‘\˜][ÛˆOH‹‘\˜][ÛˆÂ‚BBBBY›Ý[™HYB‚BBB_B‚BB_B‚BBZYˆY›Ý[™Â‚BBB\™]\›ˆ˜[ÙB‚BB_B‚B_B‚_B‚\™]\›ˆYBŸB™[˜È
È
”Ù\™\ŠH]ÚšY[ÐÛÛ™šYÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚]˜\ˆ[œ]X\ÜÝš[™×ZœÛÛ‹”˜]ÓY\ÜØYÙB‚ZYˆYXÛÙJË‹	š[œ]
HÂ‚B\™]\›‚‚_B‚ZYˆ[œ]OHš[Â‚BY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹‘^XÝYHšY[ÈÛÛ™šYÝ\˜][ÛˆØš™XÝŠB‚B\™]\›‚‚_B‚Y^XÝY\œˆHË”ÝÜ™K”Ù][™Ê‹ÛÛ^

KšY[×Ü[[YWØÛÛ™šYÈŠB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚XÈHY˜][šY[ÐÛÛ™šYÊ
B‚ZYˆ\œˆHœÛÛ‹•[›X\œÚ[
^XÝY	˜ÊNÈ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹ÝÜ™K‘\œÛÛ™›XÝ
B‚B\™]\›‚‚_B‚[Û”ÓÓ‹ÈHœÛÛ‹“X\œÚ[
ÊB‚]˜\ˆ˜[Y\ÈX\ÜÝš[™×ZœÛÛ‹”˜]ÓY\ÜØYÙB‚WÈHœÛÛ‹•[›X\œÚ[
Û”ÓÓ‹	˜[Y\ÊB‚\ÙXÜ™]šY[ÈH×\Ýš[™ÞÈ˜\[X\Ø\WÚÙ^H‹˜YÛ™\×Ø\WÚÙ^H‹šœÞWØ\WÚÙ^H‹˜ÛX\—Ø\[X\Ø\WÚÙ^H‹˜ÛX\—ØYÛ™\×Ø\WÚÙ^H‹˜ÛX\—ÚœÞWØ\WÚÙ^HŸB‚Y›ÜˆÙ^K˜[YHH˜[™ÙH[œ]Â‚BZYˆÛ™SÙŠÙ^KÙXÜ™]šY[Ë‹‹ŠHÂ‚BBXÛÛ[YB‚B_B‚BZYˆËÛ›ÝÛˆH˜[Y\ÖÚÙ^WNÈZÛ›ÝÛˆÂ‚BBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹•[šÛ›ÝÛˆšY[ÈÛÛ™šYÝ\˜][ÛˆšY[ŠB‚BB\™]\›‚‚B_B‚BZYˆÝš[™Ê˜[YJHOH›[ˆÂ‚BBXÛÛ[YB‚B_B‚B]˜[Y\ÖÚÙ^WHH˜[YB‚_B‚]\HÙ^U\]HÝXÝÂ‚B]šY[ÒÙ^TÙ][™Â‚BPTRÙ^H
œÝš[™ÈœÛÛŽˆ˜\WÚÙ^H˜‚BPÛX\ˆ›ÛÛœÛÛŽˆ˜ÛX\—Ø\WÚÙ^H˜‚_B‚ZÙ^U\]\ÈH×ZÙ^U\]^ßB‚ZYˆ˜]Ë™\Ù[H[œ]È˜YÛ™\×ÚÙ^\È—NÈ™\Ù[	‰ˆÝš[™Ê˜]ÊHOH›[ˆÂ‚BYHœÛÛ‹“™]ÑXÛÙ\Šž]\Ë“™]Ô™XY\Š˜]ÊJB‚BY‘\Ø[ÝÕ[šÛ›ÝÛ‘šY[Ê
B‚BZYˆ‘XÛÙJ	šÙ^U\]\ÊHOHš[Â‚BBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹’[˜[YYÛ™\ÈÙ^HÛÛ™šYÝ\˜][ÛˆŠB‚BB\™]\›‚‚B_B‚BXËYÛ™\ÒÙ^\ÈHXZÙJ×]šY[ÒÙ^TÙ][™Ë[ŠÙ^U\]\ÊJB‚BY›ÜˆHH˜[™ÙHÙ^U\]\ÈÂ‚BBZYˆÙ^U\]\ÖÚWK’QOHˆˆÂ‚BBBZÙ^U\]\ÖÚWK’QHÙXÝ\š]K“™]ÒQ
šÙ^HŠB‚BB_B‚BBXËYÛ™\ÒÙ^\ÈH\[™
ËYÛ™\ÒÙ^\ËÙ^U\]\ÖÚWKšY[ÒÙ^TÙ][™ÊB‚B_B‚B]˜[Y\ÖÈ˜YÛ™\×ÚÙ^\È—KÈHœÛÛ‹“X\œÚ[
ËYÛ™\ÒÙ^\ÊB‚_B‚[Y\™ÙYÈHœÛÛ‹“X\œÚ[
˜[Y\ÊB‚YXÛÙ\ˆHœÛÛ‹“™]ÑXÛÙ\Šž]\Ë“™]Ô™XY\ŠY\™ÙY
JB‚YXÛÙ\‹‘\Ø[ÝÕ[šÛ›ÝÛ‘šY[Ê
B‚ZYˆXÛÙ\‹‘XÛÙJ	˜ÊHOHš[]˜[Y]UšY[ÐÛÛ™šYÊÊHÂ‚BY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹’[˜[YšY[È[Ù[šXÙK\˜][ÛˆÜˆ]Y]YHÛÛ™šYÝ\˜][ÛˆŠB‚B\™]\›‚‚_B‚ZYˆ\ËÛÛ™šYË‘[[Ó[ÙH	‰ˆË‘[˜X›YÂ‚BZYˆ
Ë”Ý[™\™[˜X›Y	‰ˆË”Ý[™\™šXÙHJH
Ë”]X[]Q[˜X›Y	‰ˆË”]X[]TšXÙHJHÂ‚BBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹‘[˜X›Y]›Ü›HšY[ÈY\œÈ™\]Z\™HHÜÚ]]™HšXÙHŠB‚BB\™]\›‚‚B_B‚BY›ÜˆËHH˜[™ÙHšY[ÐØ][ÙÊÊHÂ‚BBY›ÜˆËˆH˜[™ÙHK•˜\šX[ÈÂ‚BBBZYˆK‘[˜X›Y	‰ˆ‹‘[˜X›Y	‰ˆ‹”šXÙHHÂ‚BBBBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹‘[˜X›YšY[È˜\šX[È™\]Z\™HHÜÚ]]™HšXÙHŠB‚BBBB\™]\›‚‚BBB_B‚BB_B‚B_B‚_B‚\›ÝšY\œË\œˆHË”ÝÜ™K”›ÝšY\œÊ‹ÛÛ^

Kœ]›Ü›HŠB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚Y^\Ý[™ÈHX\ÜÝš[™×\ÝÜ™K”›ÝšY\žßB‚Y›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BY^\Ý[™ÖÜ’QHH‚_B‚]\]\ÈH×\ÝÜ™K•šY[Ô›ÝšY\•\]^ßB‚[X[˜YÙYHX[˜YÙYšY[Ô›ÝšY\œÊÊB‚ZÙ\HX\ÜÝš[™×X›ÛÛßB‚Y›ÜˆËH˜[™ÙHX[˜YÙYÂ‚BZÙ\Ü’QHHYB‚B]˜\ˆ˜]È
œÝš[™Â‚BXÛX\ˆH˜[ÙB‚BZÙ^S˜[YHH˜\[X\‚‚BZYˆ”›ÝØÛÛOHšœÞWÝšY[ÈˆÂ‚BBZÙ^S˜[YHHšœÞH‚‚B_B‚BZYˆ”›ÝØÛÛOH˜YÛ™\×ÝšY[ÈˆÂ‚BBZÙ^S˜[YHH˜YÛ™\È‚‚B_B‚BZYˆ˜[YK™\Ù[H[œ]ÚÙ^S˜[YJÈ—Ø\WÚÙ^H—NÈ™\Ù[	‰ˆÝš[™Ê˜[YJHOH›[ˆÂ‚BBZYˆœÛÛ‹•[›X\œÚ[
˜[YK	œ˜]ÊHOHš[Â‚BBBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹’[˜[YTHÙ^HŠB‚BBB\™]\›‚‚BB_B‚B_B‚BZYˆ˜[YK™\Ù[H[œ]È˜ÛX\—ÈŠÚÙ^S˜[YJÈ—Ø\WÚÙ^H—NÈ™\Ù[	‰ˆÝš[™Ê˜[YJHOH›[ˆÂ‚BBZYˆœÛÛ‹•[›X\œÚ[
˜[YK	˜ÛX\ŠHOHš[Â‚BBBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹’[˜[YÙ^HÛX\š[™È›YÈŠB‚BBB\™]\›‚‚BB_B‚B_B‚BZYˆÙ^S˜[YHOH˜YÛ™\ÈˆÂ‚BBZYˆËYÛ™\ÒÙ^\ÈOHš[Â‚BBB\˜]ËÛX\ˆHš[˜[ÙB‚BB_B‚BBY›ÜˆË\]HH˜[™ÙHÙ^U\]\ÈÂ‚BBBZYˆ’QOHšY[ËXYÛ™\ËHŠÝ\]K’QÂ‚BBBB\˜]ËÛX\ˆH\]KTRÙ^K\]KÛX\‚‚BBB_B‚BB_B‚B_B‚B]˜\ˆÚ\\ˆ
œÝš[™Â‚BZYˆÛX\ˆÂ‚BBY[\HHˆ‚‚BBXÚ\\ˆH	™[\B‚B_H[ÙHYˆ˜]ÈOHš[	‰ˆÝš[™ÜË•š[TÜXÙJ
œ˜]ÊHOHˆˆÂ‚BBZÙ^HHÝš[™ÜË•š[TÜXÙJ
œ˜]ÊB‚BBZYˆ[ŠÙ^JHˆNLˆÝš[™ÜËÛÛZ[œÐ[žJÙ^K———ŠHÂ‚BBBY˜Z[
Ë‹’S•SQÔ‘TUQTÕ‹’[˜[YTHÙ^HŠB‚BBB\™]\›‚‚BB_B‚BBY[˜Üž\YHHË•˜][‘[˜Üž\
œ]›Ü›KÈŠÜ’QÙ^JB‚BBZYˆHOHš[Â‚BBB\ËšY[Ñ\œ›ÜŠË‹JB‚BBB\™]\›‚‚BB_B‚BBXÚ\\ˆH	™[˜Üž\Y‚B_B‚B\š[ÜˆH^\Ý[™ÖÜ’QB‚BZYˆš[Ü‹˜\ÙUT“OHˆˆÂ‚BB\˜\ÙUT“Hš[Ü‹˜\ÙUT“‚B_B‚B]\]\ÈH\[™
\]\ËÝÜ™K•šY[Ô›ÝšY\•\]^Ô›ÝšY\ŽˆÚ\\^ˆÚ\\ŸJB‚_B‚Y›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BZYˆÝš[™ÜË’\Ô™Yš^
’QšY[ËXYÛ™\ËHŠH	‰ˆZÙ\Ü’QHÂ‚BB\‘[˜X›YH˜[ÙB‚BB]\]\ÈH\[™
\]\ËÝÜ™K•šY[Ô›ÝšY\•\]^Ô›ÝšY\ŽˆJB‚B_B‚_B‚Y]KÈHœÛÛ‹“X\œÚ[
ÊB‚ZYˆ\œˆHË”ÝÜ™K”Ø]™UšY[ÐÛÛ™šYÝ\˜][ÛÐTÊ‹ÛÛ^

KÝ\œ™[\Ù\ŠŠK’Q^XÝY]K\]\ÊNÈ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\Ë™Ù]šY[ÐYZ[ÛÛ™šYÊËŠBŸB‚™[˜È
È
”Ù\™\ŠHÙ]šY[ÐYZ[ÛÛ™šYÊÈ”™\ÜÛœÙUÜš]\‹ˆ
š”™\]Y\Ý
HÂ‚XË\œˆHËšY[ÐÛÛ™šYÊ‹ÛÛ^

JB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\›ÝšY\œË\œˆHË”ÝÜ™K”›ÝšY\œÊ‹ÛÛ^

Kœ]›Ü›HŠB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚XÝ\œ™[Ù^\ÈHX\ÜÝš[™×X›ÛÛßB‚Y›ÜˆËH˜[™ÙHX[˜YÙYšY[Ô›ÝšY\œÊÊHÂ‚BXÝ\œ™[Ù^\ÖÜ’QHHYB‚_B‚]š\ÚX›HH›ÝšY\œÖÎŒB‚Y›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BZYˆÝš[™ÜË’\Ô™Yš^
’QšY[ËXYÛ™\ËHŠH	‰ˆXÝ\œ™[Ù^\ÖÜ’QHÂ‚BBXÛÛ[YB‚B_B‚B]š\ÚX›HH\[™
š\ÚX›K
B‚_B‚\›ÝšY\œÈHš\ÚX›B‚]\HÙ^TÝ]HÝXÝÂ‚BTÝÜ™Y\ØX›K\œ›Üˆ›ÛÛ‚BQš[™Ù\œš[Ýš[™Â‚_B‚\Ý]\ÈHX\ÜÝš[™×ZÙ^TÝ]^ßB‚Y›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚B\Ý]HHÙ^TÝ]^ÔÝÜ™Yˆ’\ÐTRÙ^_B‚BZYˆ’\ÐTRÙ^HÂ‚BB\ÙXÜ™]\œˆHË•˜][‘XÜž\
“ÝÛ™\”ØÛÜJÈ‹ÈŠÜ’Q”ÙXÜ™]Ú\\^
B‚BB\Ý]K‘\œ›ÜˆH\œˆOHš[‚BB\Ý]K•\ØX›HH\œˆOHš[	‰ˆÝš[™ÜË•š[TÜXÙJÙXÜ™]
HOHˆ‚‚BBZYˆÝ]K•\ØX›HÂ‚BBBYYÙ\ÝHÚLM‹”Ý[LMŠ×Xž]JÙXÜ™]
JB‚BBB\Ý]K‘š[™Ù\œš[H^‘[˜ÛÙUÔÝš[™ÊYÙ\ÝÎJB‚BB_B‚B_B‚B\Ý]\ÖÜ’QHHÝ]B‚_B‚Y]KÈHœÛÛ‹“X\œÚ[
ÊB‚]šY]ÈHX\ÜÝš[™×X[ž^ßB‚WÈHœÛÛ‹•[›X\œÚ[
]K	šY]ÊB‚]šY]ÖÈ›[Ù[ØÛÛ™šYÜÈ—HHšY[ÐØ][ÙÊÊB‚]šY]ÖÈ›[ØÚ×Û[ÙH—KšY]ÖÈ›[ØÚ×Ù[—Ù[˜X›Y—HHËÛÛ™šYË‘[[Ó[ÙKËÛÛ™šYË‘[[Ó[ÙB‚]šY]ÖÈœZYÜ›ÝšY\—Ù[—Ù[˜X›Y—KšY]ÖÈ˜YÛ™\×Ù[—Ù[˜X›Y—HHYKYB‚Y›ÜˆË˜[YHH˜[™ÙH×\Ýš[™ÞÈ˜\[X\‹˜YÛ™\È‹šœÞHŸHÂ‚B\Ù]ÝÜ™YÙ^Q\œ›Ü‹Y™™XÝ]™HH˜[ÙK˜[ÙK˜[ÙK˜[ÙB‚BY›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BB[X]ÚH
˜[YHOH˜\[X\ˆ	‰ˆ”›ÝØÛÛOH˜\[X\ÝšY[ÈŠH
˜[YHOH˜YÛ™\Èˆ	‰ˆ”›ÝØÛÛOH˜YÛ™\×ÝšY[ÈŠH
˜[YHOHšœÞHˆ	‰ˆ”›ÝØÛÛOHšœÞWÝšY[ÈŠB‚BBZYˆX]ÚÂ‚BBB\Ý]HHÝ]\ÖÜ’QB‚BBB\Ù]ÝÜ™YÙ^Q\œ›ÜˆHÙ]Ý]K•\ØX›KÝÜ™YÝ]K”ÝÜ™YÙ^Q\œ›ÜˆÝ]K‘\œ›Ü‚‚BBBYY™™XÝ]™HHY™™XÝ]™H
‘[˜X›Y	‰ˆÝ]K•\ØX›JB‚BB_B‚B_B‚B\ÛÝ\˜ÙHH››Û™H‚‚BZYˆÝÜ™YÂ‚BB\ÛÝ\˜ÙHH˜YZ[ˆ‚‚B_B‚BZYˆÙ^Q\œ›Üˆ	‰ˆ\Ù]Â‚BB\ÛÝ\˜ÙHHš[˜[Y‚‚B_B‚B]šY]ÖÛ˜[YJÈ—ÚÙ^WÜÙ]—KšY]ÖÛ˜[YJÈ—ÜÝÜ™YÚÙ^WÜÙ]—KšY]ÖÛ˜[YJÈ—ÚÙ^WÜÛÝ\˜ÙH—KšY]ÖÛ˜[YJÈ—ÚÙ^WÙ\œ›Üˆ—HHÙ]ÝÜ™YÛÝ\˜ÙKÙ^Q\œ›Ü‚‚B]šY]ÖÛ˜[YJÈ—ÙY™™XÝ]™H—HHY™™XÝ]™B‚_B‚]šY]ÖÈœZYÜ›ÝšY\—ÙY™™XÝ]™H—HHË”ZY	‰ˆšY]ÖÈ˜\[X\ÙY™™XÝ]™H—HOHYB‚Y›ÜˆË˜[Z[HH˜[™ÙH×]šY[Ë”›ÝšY\žÝšY[Ë‘Ù[Z[šKšY[Ë”ÙYY[˜Ù_HÂ‚BX]˜Z[X›HH˜[ÙB‚BY›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BBZYˆ”›ÝØÛÛOH˜\[X\ÝšY[Èˆ	‰ˆ‘[˜X›Y	‰ˆÝ]\ÖÜ’QK•\ØX›H	‰ˆ

˜[Z[HOHšY[Ë‘Ù[Z[šH	‰ˆÝš[™ÜË’\Ô™Yš^
“[Ù[™Ù[Z[šKHŠJH
˜[Z[HOHšY[Ë”ÙYY[˜ÙH	‰ˆÝš[™ÜË’\Ô™Yš^
“[Ù[™ÝX˜[Ë\ÙYY[˜ÙKHŠJJHÂ‚BBBX]˜Z[X›HHYB‚BB_B‚B_B‚B]šY]ÖÜÝš[™Ê˜[Z[JJÈ—ÙY™™XÝ]™H—HH]˜Z[X›B‚_B‚ZÙ^\ÈH×[X\ÜÝš[™×X[ž^ßB‚ZÙ^SY]šXÜË\œˆHË”ÝÜ™K•šY[ÒÙ^SY]šXÜÊ‹ÛÛ^

Kœ]›Ü›HŠB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚Y›ÜˆËH˜[™ÙH›ÝšY\œÈÂ‚BZYˆ”›ÝØÛÛOH˜YÛ™\×ÝšY[ÈˆÂ‚BB\Ý]KY]šXÈHÝ]\ÖÜ’QKÙ^SY]šXÜÖÜ’QB‚BBZÙ^\ÈH\[™
Ù^\ËX\ÜÝš[™×X[ž^ÈšYŽˆÝš[™ÜË•š[T™Yš^
’QšY[ËXYÛ™\ËHŠK›X™[Žˆ“˜[YK™[˜X›YŽˆ‘[˜X›Y˜ÛÛ˜Ý\œ™[˜ÞHŽˆ“X^ÛÛ˜Ý\œ™[˜ÞK˜ÛÛ™šYÝ\™YŽˆÝ]K•\ØX›K™š[™Ù\œš[ŽˆÝ]K‘š[™Ù\œš[œÛÝ\˜ÙHŽˆ˜YZ[ˆ‹™\œ›ÜˆŽˆÝ]K‘\œ›Ü‹˜XÝ]™WØÛÝ[ŽˆY]šXËXÝ]™KÙ^WØÛÝ[ŽˆY]šXË•Ù^KœÝXØÙ\Ü×ØÛÝ[ŽˆY]šXË”ÝXØÙYYY™˜Z[\™WØÛÝ[ŽˆY]šXË‘˜Z[YJB‚B_B‚_B‚]šY]ÖÈ˜YÛ™\×ÚÙ^\È—HHÙ^\Â‚[X\[™ÜÈH×[X\ÜÝš[™×X[ž^ßB‚Y›ÜˆËÛÙHH˜[™ÙH×\Ýš[™ÞÈœÙYY[˜ÙK[Z[šH‹œÙYY[˜ÙKY˜\Ý‹œÙYY[˜ÙKLˆŸHÂ‚BY›ÜˆË™\ÈH˜[™ÙH×\Ýš[™ÞÈ‹ÌŒŸHÂ‚BB[˜]]™KÈHšY[Ë”™\ÛÛ™S[Ù[
šY[Ë’”ÖKÛÙK™\ÊB‚BBXÛÜÝHX\ÜÝš[™×Y›Ø]ÈœÙL‹Œ[Z[šHŽˆKŒ‹œÙL‹ŒY˜\ÝMŽˆ‹œÙL‹ŒY˜\ÝŽˆËœÙL‹ŒMŽˆËœÙL‹ŒŽˆVÛ˜]]™WB‚BB[Z[š[][HHX\ÜÝš[™×Z[ÈœÙL‹Œ[Z[šHŽˆLŽœÙL‹ŒY˜\ÝMŽˆŒLËœÙL‹ŒY˜\ÝŽˆÌNKœÙL‹ŒMŽˆÌNKœÙL‹ŒŽˆŸVÛ˜]]™WB‚BB[X\[™ÜÈH\[™
X\[™ÜËX\ÜÝš[™×X[ž^È›[Ù[ØÛÙHŽˆÛÙK›[Ù[Û˜[YHŽˆÛÙKœ™\ÛÛ][ÛˆŽˆ™\ËšœÞWÛ[Ù[Žˆ˜]]™K˜ÛÜÝØÛžHŽˆÛÜÝ›Z[š[][WØÜ™Y]ÈŽˆZ[š[][K˜\ÜXÝÜ˜][ÜÈŽˆ×\Ýš[™ÞÈŽNŒMˆ‹ŒMŽŽH‹ŒNŒHŸ_JB‚B_B‚_B‚]šY]ÖÈšœÞWÛX\[™ÜÈ—HHX\[™ÜÂ‚]šY]ÖÈ›[Ù[È—HHX\ÜÝš[™×\Ýš[™ÞÈœÝ[™\™Žˆ™Ù[Z[šK[Û[šKY›\Ú\™]šY]È‹œÝ[™\™Ü›Ý]HŽˆËYÛ™\Ó[Ù[œ]X[]HŽˆ™ÝX˜[Ë\ÙYY[˜ÙKL‹ŒY˜\ÝŸB‚\YÙK\œˆHË”ÝÜ™K•šY[Õ\ÚÜÊ‹ÛÛ^

KÝÜ™K•šY[Õ\ÚÑš[\žÐYZ[ŽˆYKYÙTÚ^™NˆLŸJB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚\™XÙ[H×[X\ÜÝš[™×X[ž^ßB‚Y›ÜˆËˆH˜[™ÙHYÙK”›ÝÜÈÂ‚B\™XÙ[H\[™
™XÙ[ËšY[Õ\ÚÕšY]Ê‹ÛÛ^

K‹YJJB‚_B‚]šY]ÖÈœ™XÙ[Ý\ÚÜÈ—HH™XÙ[‚[Y]šXÜË\œˆHË”ÝÜ™K•šY[ÓY]šXÜÊ‹ÛÛ^

JB‚ZYˆ\œˆOHš[Â‚B\ËšY[Ñ\œ›ÜŠË‹\œŠB‚B\™]\›‚‚_B‚]šY]ÖÈ›Y]šXÜÈ—HHY]šXÜÂ‚[ÚÊË‹šY]ÊBŸB