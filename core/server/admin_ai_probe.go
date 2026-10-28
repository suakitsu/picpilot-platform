package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func adminAIProbeEndpoint(p store.Provider) (string, error) {
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/images/generations", "/images/edits", "/models"} {
		path = strings.TrimSuffix(path, suffix)
	}
	if path == "" {
		if p.Protocol == "gemini_generate_content" {
			path = "/v1beta"
		} else {
			path = "/v1"
		}
	}
	u.Path, u.RawPath = path+"/models", ""
	return u.String(), nil
}

// A probe tests metadata connectivity only. It never invokes a model or creates
// a task/attempt/worker job, even when the legacy UI describes generation probes.
func (s *Server) adminAIProbe(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !oneOf(kind, "image", "text") {
		fail(w, r, 400, "INVALID_REQUEST", "Probe kind must be image or text")
		return
	}
	_, ps, valid := s.adminAIRequest(w, r)
	if !valid {
		return
	}
	var selected *store.Provider
	for i := range ps {
		if ps[i].Capability == kind && ps[i].Enabled {
			selected = &ps[i]
			break
		}
	}
	if selected == nil {
		fail(w, r, 400, "AI_CONFIG_INVALID", "Enable a configured provider before probing")
		return
	}
	p := *selected
	result := map[string]any{"ok": false, "kind": kind, "provider": p.ID, "model": p.Model, "endpoint": "", "message": "Metadata connectivity check failed; model execution was not tested", "probe_type": "metadata", "network_checked": false, "model_called": false}
	start := time.Now()
	status := 0
	if p.ExecutionMode == "pull_worker" {
		result["ok"], result["probe_type"], result["message"] = true, "configuration", "Local Worker configuration is valid; no job was dispatched and worker execution was not tested"
	} else {
		endpoint, err := adminAIProbeEndpoint(p)
		if err != nil {
			fail(w, r, 400, "AI_CONFIG_INVALID", "Invalid provider endpoint")
			return
		}
		key, err := s.Vault.Decrypt("platform/"+p.ID, p.SecretCiphertext)
		if err != nil {
			fail(w, r, 409, "CREDENTIAL_UNAVAILABLE", "The stored credential could not be read")
			return
		}
		client, err := provider.NewHTTPClient(provider.Options{AllowLocalDevelopment: s.Config.AllowPrivateProviders, Timeout: 10 * time.Second, MaxResponseBytes: 256 * 1024})
		if err != nil {
			fail(w, r, 500, "INTERNAL_ERROR", "Could not initialize the probe")
			return
		}
		defer client.CloseIdleConnections()
		// Stricter than the shared client's policy: do not follow any redirects.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			fail(w, r, 400, "AI_CONFIG_INVALID", "Invalid provider endpoint")
			return
		}
		req.Header.Set("Accept", "application/json")
		if key != "" {
			if p.Profile == "google_gemini" {
				req.Header.Set("X-Goog-API-Key", key)
			} else {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		result["endpoint"], result["network_checked"] = endpoint, true
		resp, err := client.Do(req)
		if err == nil {
			status = resp.StatusCode
			_, readErr := io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			result["http_status"] = status
			if readErr == nil && status >= 200 && status < 300 {
				result["ok"], result["message"] = true, "Metadata endpoint responded successfully; model execution and model availability were not tested"
			}
		}
	}
	result["latency_ms"] = time.Since(start).Milliseconds()
	if err := s.Store.Audit(r.Context(), currentUser(r).ID, "ai_config_probe", "provider", p.ID, requestID(r), map[string]any{"capability": kind, "http_status": status, "model_called": false}); err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, result)
}
