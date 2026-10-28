package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/store"
)

var providerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)
var allowedProtocols = map[string][]string{
	"text":  {"openai_chat_completions", "openai_responses"},
	"image": {"openai_images_generations", "openai_images_edits", "gemini_generate_content"},
	"video": {"apimart_video", "hfsy_video", "agnes_video"},
}

func (s *Server) providerScope(r *http.Request) string {
	if s.Config.Edition == config.Platform {
		return "platform"
	}
	return currentUser(r).ID
}
func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.Store.Providers(r.Context(), s.providerScope(r))
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"providers": providers, "allowed_protocols": allowedProtocols})
}

type providerInput struct {
	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name"`
	Capability       string          `json:"capability"`
	Protocol         string          `json:"protocol"`
	Profile          string          `json:"profile,omitempty"`
	BaseURL          string          `json:"base_url"`
	APIKey           *string         `json:"api_key,omitempty"`
	ClearAPIKey      bool            `json:"clear_api_key,omitempty"`
	HasAPIKey        bool            `json:"has_api_key,omitempty"`
	Model            string          `json:"model"`
	Enabled          bool            `json:"enabled"`
	Priority         int             `json:"priority"`
	ExecutionMode    string          `json:"execution_mode"`
	MaxConcurrency   int             `json:"max_concurrency"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	LocalWaitSeconds int             `json:"local_wait_seconds,omitempty"`
	OfflineAction    string          `json:"offline_action,omitempty"`
	LeaseSeconds     int             `json:"lease_seconds,omitempty"`
	Settings         json.RawMessage `json:"settings,omitempty"`
	Version          int64           `json:"version,omitempty"`
}

func (s *Server) validateProvider(p store.Provider) string {
	if !providerIDPattern.MatchString(p.ID) || len(p.Name) == 0 || len(p.Name) > 160 {
		return "Invalid provider ID or name"
	}
	if !oneOf(p.Protocol, allowedProtocols[p.Capability]...) {
		return "Protocol does not match capability"
	}
	if len(p.Model) == 0 || len(p.Model) > 160 || strings.ContainsAny(p.Model, "\r\n\x00") {
		return "A valid model ID is required"
	}
	if !oneOf(p.ExecutionMode, "http", "pull_worker") {
		return "Invalid execution mode"
	}
	if p.MaxConcurrency < 1 || p.MaxConcurrency > 100 || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 3600 || p.LocalWaitSeconds < 0 || p.LocalWaitSeconds > 3600 || p.LeaseSeconds < 30 || p.LeaseSeconds > 1800 || p.Priority < -10000 || p.Priority > 10000 || !oneOf(p.OfflineAction, "queue", "fallback") {
		return "Invalid concurrency or timeout settings"
	}
	if !oneOf(p.Profile, "openai", "legacy_openai", "hfsyapi", "yunfei", "rua", "xin_zhi_zhu", "aishuch", "gemini_proxy", "google_gemini", "apimart", "agnes") {
		return "Unknown adapter profile"
	}
	if p.ExecutionMode == "pull_worker" {
		if p.MaxConcurrency > 32 || p.LocalWaitSeconds < 1 || p.LocalWaitSeconds > 300 || (p.Capability == "text" && p.TimeoutSeconds > 300) {
			return "Local Worker concurrency must be 1-32 and wait/text timeout 1-300 seconds"
		}
		if p.Capability == "video" || p.Protocol == "gemini_generate_content" {
			return "Local Worker v1 supports OpenAI image and text tasks only"
		}
		if p.Capability == "image" && p.Model != "gpt-image-2" {
			return "Local Worker images require gpt-image-2"
		}
		if p.Capability == "text" && !oneOf(p.Model, "gpt-5.6-sol", "gpt-5.5", "gpt-5.6-terra", "gpt-5.6-luna") {
			return "Choose a Local Worker v1 text model"
		}
	} else {
		if oneOf(p.ID, "local-cliproxy-image", "local-cliproxy-text") {
			return "Reserved Local Worker providers require pull_worker execution"
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !oneOf(u.Scheme, "https", "http") {
			return "Base URL must be an HTTP(S) URL without credentials or query parameters"
		}
		if u.Scheme != "https" && !s.Config.AllowPrivateProviders {
			return "HTTP providers require explicit PICPILOT_ALLOW_PRIVATE_PROVIDERS=true"
		}
		if p.Enabled && !p.HasAPIKey && !s.Config.DemoMode {
			return "Configure an API key before enabling this HTTP provider"
		}
	}
	var settings map[string]json.RawMessage
	if len(p.Settings) > 65536 || json.Unmarshal(p.Settings, &settings) != nil || settings == nil {
		return "Settings must be a JSON object"
	}
	for key := range settings {
		if !oneOf(key, "model_mapping", "size_mapping", "headers_profile", "pricing", "circuit_breaker", "image_options", "text_options", "video_options", "worker_ids") {
			return "Unsupported provider setting"
		}
	}
	return ""
}

func (s *Server) putProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if in.ID != "" && in.ID != id {
		fail(w, r, 400, "INVALID_REQUEST", "Provider ID does not match path")
		return
	}
	if in.ExecutionMode == "" {
		in.ExecutionMode = "http"
		if oneOf(id, "local-cliproxy-image", "local-cliproxy-text") {
			in.ExecutionMode = "pull_worker"
		}
	}
	if in.Profile == "" {
		in.Profile = "openai"
	}
	if in.MaxConcurrency == 0 {
		in.MaxConcurrency = 1
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 300
		if in.ExecutionMode == "pull_worker" && in.Capability == "text" {
			in.TimeoutSeconds = 90
		}
	}
	if in.ExecutionMode == "pull_worker" && in.LocalWaitSeconds == 0 {
		in.LocalWaitSeconds = 30
	}
	if in.OfflineAction == "" {
		in.OfflineAction = "fallback"
	}
	if in.LeaseSeconds == 0 {
		in.LeaseSeconds = 120
	}
	if len(in.Settings) == 0 {
		in.Settings = json.RawMessage(`{}`)
	}
	scope := s.providerScope(r)
	existing, _ := s.Store.Provider(r.Context(), scope, id)
	p := store.Provider{ID: id, OwnerScope: scope, Name: strings.TrimSpace(in.Name), Capability: in.Capability, Protocol: in.Protocol, Profile: in.Profile, BaseURL: strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), Model: strings.TrimSpace(in.Model), Enabled: in.Enabled, Priority: in.Priority, ExecutionMode: in.ExecutionMode, MaxConcurrency: in.MaxConcurrency, TimeoutSeconds: in.TimeoutSeconds, LocalWaitSeconds: in.LocalWaitSeconds, OfflineAction: in.OfflineAction, LeaseSeconds: in.LeaseSeconds, Settings: in.Settings, Version: in.Version, HasAPIKey: existing.HasAPIKey}
	var cipher *string
	if in.ClearAPIKey {
		empty := ""
		cipher = &empty
		p.HasAPIKey = false
	} else if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		if len(*in.APIKey) > 8192 || strings.ContainsAny(*in.APIKey, "\r\n\x00") {
			fail(w, r, 400, "INVALID_REQUEST", "Invalid API key")
			return
		}
		value, err := s.Vault.Encrypt(scope+"/"+id, strings.TrimSpace(*in.APIKey))
		if err != nil {
			fail(w, r, 500, "INTERNAL_ERROR", "Could not store credential")
			return
		}
		cipher = &value
		p.HasAPIKey = true
	}
	if p.ExecutionMode == "pull_worker" {
		p.BaseURL = ""
		empty := ""
		cipher = &empty
		p.HasAPIKey = false
	}
	if reason := s.validateProvider(p); reason != "" {
		fail(w, r, 400, "PROVIDER_CONFIG_INVALID", reason)
		return
	}
	p, err := s.Store.PutProvider(r.Context(), p, cipher)
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	_ = s.Store.Audit(r.Context(), currentUser(r).ID, "provider.update", "provider", id, requestID(r), map[string]any{"capability": p.Capability, "execution_mode": p.ExecutionMode, "enabled": p.Enabled})
	ok(w, r, p)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProvider(r.Context(), s.providerScope(r), r.PathValue("id")); err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, map[string]bool{"deleted": true})
}
func (s *Server) checkProvider(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.Provider(r.Context(), s.providerScope(r), r.PathValue("id"))
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	reason := s.validateProvider(p)
	ok(w, r, map[string]any{"valid": reason == "", "message": reason, "network_checked": false, "model_called": false})
}
