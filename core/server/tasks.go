package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

var imagePriceTiers = map[string]string{
	"1024x1024": "1K", "1040x832": "1K", "720x1280": "1K", "1280x720": "1K", "1024x768": "1K", "1008x672": "1K", "832x1040": "1K", "768x1024": "1K", "672x1008": "1K", "1344x576": "1K",
	"2048x2048": "2K", "2080x1664": "2K", "1152x2048": "2K", "2048x1152": "2K", "2048x1536": "2K", "2016x1344": "2K", "1664x2080": "2K", "1536x2048": "2K", "1344x2016": "2K", "2016x864": "2K",
	"2880x2880": "4K", "3200x2560": "4K", "2160x3840": "4K", "3840x2160": "4K", "3264x2448": "4K", "3504x2336": "4K", "2560x3200": "4K", "2448x3264": "4K", "2336x3504": "4K", "3696x1584": "4K", "4096x4096": "4K",
}

func (s *Server) registerTaskRoutes() {
	s.mux.HandleFunc("POST /api/tasks", s.auth(s.submitGenericTask))
	s.mux.HandleFunc("GET /api/tasks", s.auth(s.listGenericTasks))
	s.mux.HandleFunc("GET /api/tasks/{id}", s.auth(s.getGenericTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/cancel", s.auth(s.cancelGenericTask))
}

func (s *Server) imageCost(ctx context.Context, size string) (int64, error) {
	tier, exists := imagePriceTiers[size]
	if !exists {
		return 0, store.ErrConflict
	}
	if s.Config.Edition == config.Personal || s.Config.DemoMode {
		return 0, nil
	}
	prices := map[string]int64{"1K": 6, "2K": 12, "4K": 30}
	raw, err := s.Store.Setting(ctx, "image_pricing")
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(raw, &prices); err != nil {
		return 0, store.ErrConflict
	}
	cost := prices[tier]
	if cost < 0 || cost > 1000000 {
		return 0, store.ErrConflict
	}
	return cost, nil
}

func (s *Server) submitGenericTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Capability string       `json:"capability"`
		ProjectID  string       `json:"project_id,omitempty"`
		Title      string       `json:"title,omitempty"`
		Payload    jobs.Payload `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !oneOf(in.Capability, "image", "text") || len(in.Title) > 200 || in.Payload.Validate(in.Capability) != nil {
		fail(w, r, 400, "INVALID_TASK", "Invalid task input")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		fail(w, r, 400, "IDEMPOTENCY_KEY_REQUIRED", "Provide an Idempotency-Key of 8 to 128 characters")
		return
	}
	u := currentUser(r)
	if err := s.screenUserInput(r.Context(), u.ID, in.ProjectID, "direct_"+in.Capability, key, directUserText(in.Payload)); err != nil {
		s.databaseError(w, r, err)
		return
	}
	identity, _ := json.Marshal(in.Payload)
	var cost int64
	if in.Payload.Image != nil {
		var err error
		cost, err = s.imageCost(r.Context(), in.Payload.Image.Size)
		if err != nil {
			fail(w, r, 400, "INVALID_IMAGE_SIZE", "Unsupported image size")
			return
		}
		for _, id := range in.Payload.Image.ReferenceAssetIDs {
			if _, err := s.Store.OwnedAsset(r.Context(), u.ID, id); err != nil {
				fail(w, r, 404, "REFERENCE_NOT_FOUND", "Reference image not found")
				return
			}
		}
	}
	if in.Payload.Text != nil {
		prepared, err := s.prepareTextInput(r.Context(), u.ID, *in.Payload.Text)
		if err != nil {
			fail(w, r, 400, "TEXT_REFERENCE_INVALID", "Text references must be valid images that you can access")
			return
		}
		in.Payload.Text = &prepared
	}
	raw, _ := json.Marshal(in.Payload)
	t, created, err := s.Store.SubmitTask(r.Context(), store.SubmitTask{UserID: u.ID, ProjectID: in.ProjectID, Capability: in.Capability, Kind: "direct", Title: in.Title, Request: raw, RequestIdentity: identity, IdempotencyKey: key, CreditsCost: cost})
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"task": s.taskView(r.Context(), t), "created": created})
}

func (s *Server) taskView(ctx context.Context, t store.Task) map[string]any {
	raw, _ := json.Marshal(t)
	var view map[string]any
	_ = json.Unmarshal(raw, &view)
	var result struct {
		ArtifactID string `json:"artifact_id"`
	}
	_ = json.Unmarshal(t.Result, &result)
	if result.ArtifactID != "" {
		if a, err := s.Store.OwnedAsset(ctx, t.UserID, result.ArtifactID); err == nil {
			view["result_url"] = s.assetView(a)["url"]
			view["artifact"] = s.assetView(a)
		}
	}
	return view
}

func (s *Server) getGenericTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Task(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, s.taskView(r.Context(), t))
}
func (s *Server) cancelGenericTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.CancelTask(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, s.taskView(r.Context(), t))
}
func (s *Server) listGenericTasks(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		fail(w, r, 400, "INVALID_PAGE", "Invalid page number")
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	tasks, total, err := s.Store.ListTasks(r.Context(), currentUser(r).ID, size, (page-1)*size)
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	views := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		views = append(views, s.taskView(r.Context(), t))
	}
	ok(w, r, map[string]any{"rows": views, "total": total, "page": page, "page_size": size, "total_pages": (total + size - 1) / size})
}
