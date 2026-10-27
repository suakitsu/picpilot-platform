package server

import (
	"net/http"

	"github.com/suakitsu/picpilot-platform-go/core/config"
)

// Users see model capabilities, not operator URLs, keys, health or worker IDs.
func (s *Server) aiCapabilities(w http.ResponseWriter, r *http.Request) {
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = currentUser(r).ID
	}
	providers, err := s.Store.Providers(r.Context(), scope)
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	type model struct {
		ID       string `json:"id"`
		Protocol string `json:"protocol"`
	}
	type capability struct {
		Enabled      bool    `json:"enabled"`
		DefaultModel string  `json:"default_model"`
		Models       []model `json:"models"`
	}
	result := map[string]*capability{"image": {Models: []model{}}, "text": {Models: []model{}}, "video": {Models: []model{}}}
	seen := map[string]bool{}
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		key := p.Capability + ":" + p.Protocol + ":" + p.Model
		if seen[key] {
			continue
		}
		seen[key] = true
		cap, ok := result[p.Capability]
		if !ok {
			continue
		}
		cap.Enabled = true
		if cap.DefaultModel == "" {
			cap.DefaultModel = p.Model
		}
		cap.Models = append(cap.Models, model{ID: p.Model, Protocol: p.Protocol})
	}
	ok(w, r, result)
}
