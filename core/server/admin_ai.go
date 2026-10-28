package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/store"
)

// Worker/token routes continue to be owned by registerWorkerRoutes.
func (s *Server) registerAdminAIRoutes() {
	if s.Config.Edition != config.Platform {
		return
	}
	probe, _ := http.NewRequest(http.MethodGet, "/api/admin/ai-config", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/admin/ai-config" {
		return
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/admin/ai-config":               s.adminAIGet,
		"PATCH /api/admin/ai-config":             s.adminAISave,
		"POST /api/admin/ai-config/validate":     s.adminAIValidate,
		"POST /api/admin/ai-config/rollback":     s.adminAIRollback,
		"GET /api/admin/ai-config/versions":      s.adminAIVersions,
		"GET /api/admin/ai-config/catalog":       s.adminAICatalog,
		"GET /api/admin/ai-config/runtime":       s.adminAIGet,
		"POST /api/admin/ai-config/probe/{kind}": s.adminAIProbe,
	} {
		s.mux.HandleFunc(pattern, s.auth(s.admin(handler)))
	}
}

type adminAIProviderInput struct {
	ID               string   `json:"id"`
	Name             *string  `json:"name"`
	Enabled          *bool    `json:"enabled"`
	BaseURL          *string  `json:"base_url"`
	Model            *string  `json:"model"`
	APIKey           *string  `json:"api_key"`
	ClearAPIKey      bool     `json:"clear_api_key"`
	APIKeySet        bool     `json:"api_key_set"`
	APIKeyMasked     string   `json:"api_key_masked"`
	Provider         *string  `json:"provider"`
	Profile          *string  `json:"profile"`
	Protocol         *string  `json:"protocol"`
	EnvKey           string   `json:"env_key"`
	Priority         *int     `json:"priority"`
	TimeoutSeconds   *int     `json:"timeout_seconds"`
	ExecutionMode    *string  `json:"execution_mode"`
	LocalWaitSeconds *int     `json:"local_wait_seconds"`
	OfflineAction    *string  `json:"offline_action"`
	LeaseSeconds     *int     `json:"lease_seconds"`
	MaxConcurrency   *int     `json:"max_concurrency"`
	ActualCost       *float64 `json:"actual_cost"`
	SupportedSizes   *string  `json:"supported_sizes"`
	Notes            *string  `json:"notes"`
	Version          *int64   `json:"version"`
}

type adminAIInput struct {
	Image          *adminAIProviderInput   `json:"image"`
	Text           *adminAIProviderInput   `json:"text"`
	ImageProviders *[]adminAIProviderInput `json:"image_providers"`
	TextProviders  *[]adminAIProviderInput `json:"text_providers"`
	ETag           string                  `json:"etag"`
	CanRollback    bool                    `json:"can_rollback"`
	UpdatedAt      json.RawMessage         `json:"updated_at"`
	Workers        json.RawMessage         `json:"workers"`
}

type adminAIExtras struct {
	Provider       string  `json:"provider"`
	ActualCost     float64 `json:"actual_cost"`
	SupportedSizes string  `json:"supported_sizes"`
	Notes          string  `json:"notes"`
}

func adminAIExtra(p store.Provider) adminAIExtras {
	var root map[string]json.RawMessage
	var options map[string]json.RawMessage
	var extra adminAIExtras
	_ = json.Unmarshal(p.Settings, &root)
	_ = json.Unmarshal(root[p.Capability+"_options"], &options)
	_ = json.Unmarshal(options["admin_compat"], &extra)
	if extra.Provider == "" {
		extra.Provider = p.Profile
	}
	return extra
}

func adminAIProviderView(p store.Provider) map[string]any {
	e := adminAIExtra(p)
	mask := ""
	if p.HasAPIKey {
		mask = "********"
	}
	return map[string]any{"id": p.ID, "name": p.Name, "enabled": p.Enabled, "base_url": p.BaseURL,
		"model": p.Model, "api_key": "", "api_key_set": p.HasAPIKey, "api_key_masked": mask,
		"provider": e.Provider, "profile": p.Profile, "protocol": p.Protocol, "env_key": "",
		"priority": p.Priority, "timeout_seconds": p.TimeoutSeconds, "execution_mode": p.ExecutionMode,
		"local_wait_seconds": p.LocalWaitSeconds, "offline_action": p.OfflineAction,
		"lease_seconds": p.LeaseSeconds, "max_concurrency": p.MaxConcurrency, "actual_cost": e.ActualCost,
		"supported_sizes": e.SupportedSizes, "notes": e.Notes, "version": p.Version}
}

func adminAIView(state store.AdminAIState) map[string]any {
	groups := map[string][]map[string]any{"image": {}, "text": {}}
	effective := map[string]map[string]any{}
	for _, p := range state.Providers {
		view := adminAIProviderView(p)
		groups[p.Capability] = append(groups[p.Capability], view)
		if prior := effective[p.Capability]; prior == nil || (!prior["enabled"].(bool) && p.Enabled) {
			effective[p.Capability] = view
		}
	}
	for _, kind := range []string{"image", "text"} {
		if effective[kind] == nil {
			effective[kind] = adminAIProviderView(adminAINewProvider(kind, ""))
		}
	}
	return map[string]any{"image": effective["image"], "text": effective["text"],
		"image_providers": groups["image"], "text_providers": groups["text"],
		"etag": state.ETag, "can_rollback": state.CanRollback, "updated_at": state.UpdatedAt}
}

func adminAINewProvider(kind, id string) store.Provider {
	protocol := "openai_chat_completions"
	if kind == "image" {
		protocol = "openai_images_generations"
	}
	return store.Provider{ID: id, OwnerScope: "platform", Name: kind, Capability: kind, Profile: "openai", Protocol: protocol, ExecutionMode: "http", MaxConcurrency: 1, TimeoutSeconds: 90, LocalWaitSeconds: 30, OfflineAction: "fallback", LeaseSeconds: 120, Settings: json.RawMessage(`{}`)}
}

func adminAIProfile(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "hfsyapi":
		return "hfsyapi"
	case "yunfei", "yunfei-1k", "yunfei-hires":
		return "yunfei"
	case "rua", "rua.chat":
		return "rua"
	case "xin-zhi-zhu", "xin_zhi_zhu":
		return "xin_zhi_zhu"
	case "aishuch":
		return "aishuch"
	case "gemini_proxy", "google_gemini", "legacy_openai":
		return strings.ToLower(strings.TrimSpace(label))
	default:
		return "openai"
	}
}

func (s *Server) adminAIMerge(in adminAIInput, state store.AdminAIState) ([]store.Provider, error) {
	if in.ETag != "" && in.ETag != state.ETag {
		return nil, store.ErrConflict
	}
	existing := map[string]store.Provider{}
	for _, p := range state.Providers {
		existing[p.ID] = p
	}
	desired := make([]store.Provider, 0)
	seen := map[string]bool{}
	for _, kind := range []string{"image", "text"} {
		list, singleton := in.ImageProviders, in.Image
		if kind == "text" {
			list, singleton = in.TextProviders, in.Text
		}
		if list == nil && singleton == nil {
			for _, p := range state.Providers {
				if p.Capability == kind {
					desired = append(desired, p)
					seen[p.ID] = true
				}
			}
			continue
		}
		items := []adminAIProviderInput{}
		if list != nil {
			items = *list
		} else {
			item := *singleton
			if item.ID == "" {
				for _, p := range state.Providers {
					if p.Capability == kind {
						item.ID = p.ID
						if p.Enabled {
							break
						}
					}
				}
			}
			items = append(items, item)
			for _, p := range state.Providers {
				if p.Capability == kind && p.ID != item.ID {
					desired = append(desired, p)
					seen[p.ID] = true
				}
			}
		}
		if len(items) > 100 {
			return nil, errors.New("At most 100 providers per capability are supported")
		}
		for _, item := range items {
			if item.ID == "" {
				item.ID = security.NewID("provider")
			}
			if seen[item.ID] {
				return nil, errors.New("Provider IDs must be unique")
			}
			seen[item.ID] = true
			p, exists := existing[item.ID]
			if !exists {
				p = adminAINewProvider(kind, item.ID)
			}
			if p.Capability != kind {
				return nil, errors.New("A provider cannot change capability")
			}
			if item.Version != nil && *item.Version != 0 && (!exists || *item.Version != p.Version) {
				return nil, store.ErrConflict
			}
			if item.EnvKey != "" {
				return nil, errors.New("Environment key references are not supported; store the API key in the encrypted provider credential")
			}
			for _, field := range []struct{ source, target *string }{{item.Name, &p.Name}, {item.BaseURL, &p.BaseURL}, {item.Model, &p.Model}, {item.Protocol, &p.Protocol}, {item.ExecutionMode, &p.ExecutionMode}, {item.OfflineAction, &p.OfflineAction}} {
				if field.source != nil {
					*field.target = strings.TrimSpace(*field.source)
				}
			}
			p.BaseURL = strings.TrimRight(p.BaseURL, "/")
			for _, field := range []struct{ source, target *int }{{item.Priority, &p.Priority}, {item.TimeoutSeconds, &p.TimeoutSeconds}, {item.LocalWaitSeconds, &p.LocalWaitSeconds}, {item.LeaseSeconds, &p.LeaseSeconds}, {item.MaxConcurrency, &p.MaxConcurrency}} {
				if field.source != nil {
					*field.target = *field.source
				}
			}
			if item.Enabled != nil {
				p.Enabled = *item.Enabled
			}
			e := adminAIExtra(p)
			priorExtras := e
			prior := existing[item.ID]
			if item.Provider != nil {
				e.Provider = strings.TrimSpace(*item.Provider)
				if !exists || e.Provider != priorExtras.Provider {
					p.Profile = adminAIProfile(e.Provider)
				}
			}
			labelChanged := item.Provider != nil && e.Provider != priorExtras.Provider
			explicitProfile := item.Profile != nil && (!exists || *item.Profile != prior.Profile)
			explicitProtocol := item.Protocol != nil && (!exists || *item.Protocol != prior.Protocol)
			if item.Profile != nil && (!labelChanged || explicitProfile) {
				p.Profile = strings.TrimSpace(*item.Profile)
			}
			// Older clients have no protocol selector. Preserve explicit choices,
			// but do not treat unchanged echoed fields as a new routing decision.
			if kind == "image" && p.ExecutionMode == "http" && (!exists || labelChanged || p.Model != prior.Model) {
				gemini := strings.Contains(strings.ToLower(e.Provider+" "+p.Model), "gemini") || (explicitProfile && oneOf(p.Profile, "gemini_proxy", "google_gemini"))
				if !explicitProfile {
					if gemini {
						p.Profile = "gemini_proxy"
						if endpoint, err := url.Parse(p.BaseURL); err == nil && strings.EqualFold(endpoint.Hostname(), "generativelanguage.googleapis.com") {
							p.Profile = "google_gemini"
						}
					} else if oneOf(p.Profile, "gemini_proxy", "google_gemini") {
						p.Profile = adminAIProfile(e.Provider)
					}
				}
				if !explicitProtocol {
					if gemini {
						p.Protocol = "gemini_generate_content"
					} else if p.Protocol == "gemini_generate_content" {
						p.Protocol = "openai_images_generations"
					}
				}
			}
			if kind == "image" && ((p.Protocol == "gemini_generate_content") != oneOf(p.Profile, "gemini_proxy", "google_gemini")) {
				return nil, errors.New("Gemini image protocol and adapter profile must match")
			}
			if item.ActualCost != nil {
				e.ActualCost = *item.ActualCost
			}
			if item.SupportedSizes != nil {
				e.SupportedSizes = strings.TrimSpace(*item.SupportedSizes)
			}
			if item.Notes != nil {
				e.Notes = *item.Notes
			}
			if e.ActualCost < 0 || e.ActualCost > 1000000 || len(e.Provider) > 160 || len(e.Notes) > 4000 || len(e.SupportedSizes) > 1000 || strings.ContainsAny(e.Provider+e.Notes+e.SupportedSizes, "\x00\r") {
				return nil, errors.New("Invalid configured cost, provider label, notes, or supported sizes")
			}
			var root map[string]json.RawMessage
			if json.Unmarshal(p.Settings, &root) != nil || root == nil {
				return nil, store.ErrConflict
			}
			options := map[string]json.RawMessage{}
			if b := root[kind+"_options"]; len(b) != 0 && (json.Unmarshal(b, &options) != nil || options == nil) {
				return nil, store.ErrConflict
			}
			if !exists || e != priorExtras {
				options["admin_compat"], _ = json.Marshal(e)
				root[kind+"_options"], _ = json.Marshal(options)
				p.Settings, _ = json.Marshal(root)
			}
			if item.ClearAPIKey {
				p.SecretCiphertext = ""
			}
			if item.APIKey != nil && strings.TrimSpace(*item.APIKey) != "" {
				if item.ClearAPIKey || len(*item.APIKey) > 8192 || strings.ContainsAny(*item.APIKey, "\r\n\x00") {
					return nil, errors.New("Invalid API key")
				}
				var err error
				p.SecretCiphertext, err = s.Vault.Encrypt("platform/"+p.ID, strings.TrimSpace(*item.APIKey))
				if err != nil {
					return nil, errors.New("Could not encrypt credential")
				}
			}
			if p.ExecutionMode == "pull_worker" {
				p.BaseURL, p.SecretCiphertext = "", ""
			}
			if prior := existing[p.ID]; exists && prior.SecretCiphertext != "" && p.SecretCiphertext == prior.SecretCiphertext {
				oldURL, oldErr := url.Parse(prior.BaseURL)
				newURL, newErr := url.Parse(p.BaseURL)
				if oldErr != nil || newErr != nil || !strings.EqualFold(oldURL.Scheme, newURL.Scheme) || !strings.EqualFold(oldURL.Host, newURL.Host) {
					return nil, errors.New("Re-enter or clear the API key when changing the provider origin")
				}
			}
			p.HasAPIKey = p.SecretCiphertext != ""
			if reason := s.validateProvider(p); reason != "" {
				return nil, errors.New(reason)
			}
			desired = append(desired, p)
		}
		// Keep the protocol's reserved local providers available for workers/tokens.
		for _, p := range state.Providers {
			if p.Capability == kind && p.ID == "local-cliproxy-"+kind && !seen[p.ID] {
				p.Enabled = false
				desired = append(desired, p)
				seen[p.ID] = true
			}
		}
	}
	sort.Slice(desired, func(i, j int) bool {
		a, b := desired[i], desired[j]
		if a.Capability != b.Capability {
			return a.Capability < b.Capability
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return a.ID < b.ID
	})
	return desired, nil
}

func (s *Server) adminAIRequest(w http.ResponseWriter, r *http.Request) (store.AdminAIState, []store.Provider, bool) {
	var in adminAIInput
	if !decode(w, r, &in) {
		return store.AdminAIState{}, nil, false
	}
	state, err := s.Store.AdminAIConfig(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return state, nil, false
	}
	ps, err := s.adminAIMerge(in, state)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			s.adminError(w, r, err)
		} else {
			fail(w, r, 400, "AI_CONFIG_INVALID", err.Error())
		}
		return state, nil, false
	}
	return state, ps, true
}

func (s *Server) adminAIGet(w http.ResponseWriter, r *http.Request) {
	state, err := s.Store.AdminAIConfig(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	view := adminAIView(state)
	workers, err := s.Store.WorkerAdminState(r.Context(), "platform")
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	view["workers"] = workers
	ok(w, r, view)
}
func (s *Server) adminAIValidate(w http.ResponseWriter, r *http.Request) {
	_, _, valid := s.adminAIRequest(w, r)
	if valid {
		ok(w, r, map[string]any{"ok": true, "valid": true, "message": "Configuration is valid; no network request was made", "network_checked": false, "model_called": false})
	}
}
func (s *Server) adminAISave(w http.ResponseWriter, r *http.Request) {
	state, ps, valid := s.adminAIRequest(w, r)
	if !valid {
		return
	}
	if err := s.Store.AdminSaveAIConfig(r.Context(), currentUser(r).ID, requestID(r), state.ETag, "save", ps, s.Vault); err != nil {
		s.adminError(w, r, err)
		return
	}
	s.adminAIGet(w, r)
}
func (s *Server) adminAIRollback(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64  `json:"version"`
		ETag    string `json:"etag"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if in.Version < 0 {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid version")
		return
	}
	state, err := s.Store.AdminAIConfig(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	if in.ETag != "" && in.ETag != state.ETag {
		s.adminError(w, r, store.ErrConflict)
		return
	}
	ps, err := s.Store.AdminAIRollbackSnapshot(r.Context(), currentUser(r).ID, in.Version, s.Vault)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	for _, p := range ps {
		if reason := s.validateProvider(p); reason != "" {
			fail(w, r, 409, "ROLLBACK_INVALID", "The historical configuration does not satisfy current provider validation")
			return
		}
	}
	if err = s.Store.AdminSaveAIConfig(r.Context(), currentUser(r).ID, requestID(r), state.ETag, "rollback", ps, s.Vault); err != nil {
		s.adminError(w, r, err)
		return
	}
	s.adminAIGet(w, r)
}
func (s *Server) adminAIVersions(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	p, err := s.Store.AdminAIVersions(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, p)
}
func (s *Server) adminAICatalog(w http.ResponseWriter, r *http.Request) {
	state, err := s.Store.AdminAIConfig(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	c, err := s.videoConfig(r.Context())
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	models := map[string][]string{"image": {}, "text": {}}
	for _, p := range state.Providers {
		if !oneOf(p.Model, models[p.Capability]...) {
			models[p.Capability] = append(models[p.Capability], p.Model)
		}
	}
	ok(w, r, map[string]any{"protocols": allowedProtocols, "configured_models": models, "video_models": videoCatalog(c), "profiles": []string{"openai", "legacy_openai", "hfsyapi", "yunfei", "rua", "xin_zhi_zhu", "aishuch", "gemini_proxy", "google_gemini"}, "worker_models": map[string]any{"image": []string{"gpt-image-2"}, "text": []string{"gpt-5.6-sol", "gpt-5.5", "gpt-5.6-terra", "gpt-5.6-luna"}}, "probe_type": "metadata", "model_called": false})
}
