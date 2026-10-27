package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
)

func (s *Server) registerAdminCommerceRoutes() {
	if s.Config.Edition != config.Platform {
		return
	}
	if _, pattern := s.mux.Handler(&http.Request{Method: "GET", URL: commerceRouteURL("/api/admin/commerce-suite/catalog")}); pattern == "GET /api/admin/commerce-suite/catalog" {
		return
	}
	s.mux.HandleFunc("GET /api/admin/commerce-suite/catalog", s.auth(s.admin(s.commerceCatalog)))
	s.mux.HandleFunc("POST /api/admin/commerce-suite/catalog", s.auth(s.admin(s.commerceSaveCatalog)))
	s.mux.HandleFunc("GET /api/admin/commerce-suite/platforms", s.auth(s.admin(s.commerceAdminPlatforms)))
	s.mux.HandleFunc("GET /api/admin/commerce-suite/templates", s.auth(s.admin(s.commerceAdminTemplates)))
	s.mux.HandleFunc("PUT /api/admin/commerce-suite/platforms/{code}", s.auth(s.admin(s.commerceSavePlatform)))
	s.mux.HandleFunc("PUT /api/admin/commerce-suite/templates/{id}", s.auth(s.admin(s.commerceSaveTemplate)))
}

func (s *Server) commerceAdminPlatforms(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CommerceCatalog(r.Context())
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, c.Platforms)
}
func (s *Server) commerceAdminTemplates(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.CommerceCatalog(r.Context())
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []commerce.Template{}
	platform := r.URL.Query().Get("platform")
	for _, t := range c.Templates {
		if platform == "" || t.Platform == platform {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	ok(w, r, out)
}
func (s *Server) commerceSaveCatalog(w http.ResponseWriter, r *http.Request) {
	var in commerce.Catalog
	if !commerceDecode(w, r, &in, false) {
		return
	}
	c, err := s.Store.UpdateCommerceCatalog(r.Context(), currentUser(r).ID, requestID(r), func(c *commerce.Catalog) error { *c = in; return nil })
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, c)
}
func (s *Server) commerceSavePlatform(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if !commerceDecode(w, r, &patch, false) {
		return
	}
	code := r.PathValue("code")
	var result commerce.Platform
	_, err := s.Store.UpdateCommerceCatalog(r.Context(), currentUser(r).ID, requestID(r), func(c *commerce.Catalog) error {
		i := -1
		result = commerce.Platform{Code: code, Enabled: true, DefaultResolution: "1K", DefaultSize: "1024x1024", Language: "zh-CN", OverlayLanguage: "zh-CN"}
		for index, p := range c.Platforms {
			if p.Code == code {
				i = index
				result = p
				break
			}
		}
		if err := commerce.Decode(creative.JSON(patch), &result); err != nil {
			return err
		}
		result.Code = code
		if i < 0 {
			c.Platforms = append(c.Platforms, result)
		} else {
			c.Platforms[i] = result
		}
		return nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, result)
}
func (s *Server) commerceSaveTemplate(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if !commerceDecode(w, r, &patch, false) {
		return
	}
	id := r.PathValue("id")
	var result commerce.Template
	_, err := s.Store.UpdateCommerceCatalog(r.Context(), currentUser(r).ID, requestID(r), func(c *commerce.Catalog) error {
		i := -1
		result = commerce.Template{ID: id, Enabled: true, DefaultSize: "1024x1024", ProviderParams: map[string]any{}, OverlayTemplate: map[string]any{}}
		for index, t := range c.Templates {
			if t.ID == id {
				i = index
				result = t
				break
			}
		}
		if err := commerce.Decode(creative.JSON(patch), &result); err != nil {
			return err
		}
		result.ID = id
		if i < 0 {
			c.Templates = append(c.Templates, result)
		} else {
			c.Templates[i] = result
		}
		return nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, result)
}
