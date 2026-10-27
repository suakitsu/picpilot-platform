package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/nova"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func novaBusinessKey(kind, key string, index int) string {
	sum := sha256.Sum256([]byte(key))
	return "nova:" + kind + ":" + hex.EncodeToString(sum[:]) + ":" + strconv.Itoa(index)
}

func (s *Server) novaSubmitImages(w http.ResponseWriter, r *http.Request) {
	key, err := novaKey(r)
	if err != nil {
		s.novaError(w, err)
		return
	}
	var in nova.ImageRequest
	if !novaDecode(w, r, nova.MaxImageBody, &in) {
		return
	}
	if err = in.Validate(); err != nil {
		s.novaError(w, err)
		return
	}
	ctx, user := r.Context(), currentUser(r).ID
	identity := creative.JSON(in)
	if existing, found, err := s.Store.NovaReplay(ctx, user, "image", key, identity); err != nil {
		s.novaError(w, err)
		return
	} else if found {
		s.novaSubmitted(w, existing)
		return
	}
	if in.Style != "" {
		novaFail(w, 400, "UNSUPPORTED_IMAGE_OPTION", "The configured image runner does not support gptImageStyle")
		return
	}
	if err = s.requireWorkflowProvider(ctx, user, "image"); err != nil {
		s.novaError(w, err)
		return
	}
	if err = s.workflowImageSelection(ctx, user, jobs.ImageInput{Model: in.Model}); err != nil {
		s.novaError(w, err)
		return
	}
	size, _ := in.Size()
	cost, err := s.workflowImageCost(ctx, size, "creative")
	if err != nil {
		s.novaError(w, err)
		return
	}
	if err = s.screenUserInput(ctx, user, "", "nova_image", novaBusinessKey("image", key, 0), in.Prompt); err != nil {
		s.novaBusinessError(w, r, err)
		return
	}
	imageRequest := creative.ImageRequest{ProductName: "Nova image", Prompt: in.Prompt, Size: size, Intent: "canvas_generation", ReferenceAssetIDs: in.ReferenceAssetIDs}
	uploads := make([]creativeUpload, 0, len(in.Images))
	for i, ref := range in.Images {
		data, err := ref.Bytes()
		if err != nil || http.DetectContentType(data) != ref.MIMEType {
			s.novaError(w, nova.ErrInvalid)
			return
		}
		uploads = append(uploads, creativeUpload{data: data, name: fmt.Sprintf("nova-reference-%d", i+1)})
	}
	refs, err := s.resolveCreativeInputs(ctx, user, imageRequest, uploads)
	if err != nil {
		s.novaError(w, err)
		return
	}
	imageRequest.ReferenceAssetIDs = refs
	prompt, err := creative.ImagePrompt("creative", imageRequest, map[string]any{}, nil, nil)
	if err != nil {
		s.novaError(w, nova.ErrInvalid)
		return
	}
	input := jobs.ImageInput{Model: in.Model, Prompt: prompt, Size: size, ReferenceAssetIDs: refs, Quality: in.Quality, Background: in.Background}
	run, _, err := s.Store.CreateNovaRun(ctx, user, "image", in.Mode, key, identity, in.ParallelCount, func(tx pgx.Tx, id string) ([]store.Task, error) {
		tasks := make([]store.Task, 0, in.ParallelCount)
		for i := 0; i < in.ParallelCount; i++ {
			p, err := s.Store.CreateProjectTx(ctx, tx, store.Project{UserID: user, SourceType: "creative", ProductName: "Nova image", Language: "zh-CN", Status: "images_queued", OriginalInfo: creative.JSON(imageRequest), Metadata: creative.JSON(map[string]any{"nova_task_id": id, "nova_index": i})})
			if err != nil {
				return nil, err
			}
			if err = s.Store.LinkProjectAssetsTx(ctx, tx, user, p.ID, refs, "reference"); err != nil {
				return nil, err
			}
			t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, ProjectID: p.ID, Capability: "image", Kind: "creative", Title: "Nova image", Request: creative.JSON(jobs.Payload{Version: 1, Image: &input}), IdempotencyKey: novaBusinessKey("image", key, i), CreditsCost: cost})
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, t)
		}
		return tasks, nil
	})
	if err != nil {
		s.novaError(w, err)
		return
	}
	w.Header().Set("X-Nova-Resolved-Size", size)
	s.novaSubmitted(w, run)
}

func (s *Server) novaSubmitted(w http.ResponseWriter, run store.NovaRun) {
	w.Header().Set("Location", "/api/nova/tasks/"+run.ID)
	w.Header().Set("X-Nova-Task-Id", run.ID)
	writeJSON(w, 202, map[string]string{"taskId": run.ID})
}

func (s *Server) novaRunView(ctx context.Context, run store.NovaRun) (nova.TaskView, []store.Asset, error) {
	view := nova.TaskView{ID: run.ID, Kind: run.Kind, Mode: run.Mode, CreatedAt: run.CreatedAt, AcknowledgedAt: run.AcknowledgedAt, CancellationRequested: run.CancelRequestedAt != nil}
	ids, err := s.Store.NovaRunTaskIDs(ctx, run.UserID, run.ID)
	if err != nil {
		return view, nil, err
	}
	states := make([]string, 0, len(ids))
	assets := []store.Asset{}
	images := []string{}
	var payload json.RawMessage
	var completed time.Time
	for _, id := range ids {
		t, err := s.Store.Task(ctx, run.UserID, id)
		if errors.Is(err, store.ErrNotFound) {
			states = append(states, "deleted")
			continue
		}
		if err != nil {
			return view, nil, err
		}
		state := t.Status
		if t.CancelRequestedAt != nil {
			view.CancellationRequested = true
		}
		if t.CompletedAt != nil && t.CompletedAt.After(completed) {
			completed = *t.CompletedAt
		}
		if state == "succeeded" {
			var result struct {
				ArtifactID string          `json:"artifact_id"`
				Payload    json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(t.Result, &result) != nil {
				state = "failed"
			} else if run.Kind == "image" {
				a, err := s.Store.OwnedAsset(ctx, run.UserID, result.ArtifactID)
				if errors.Is(err, store.ErrNotFound) {
					state = "failed"
				} else if err != nil {
					return view, nil, err
				} else if a.TaskID != t.ID {
					state = "failed"
				} else {
					assets = append(assets, a)
					images = append(images, "URL:"+s.assetView(a)["url"].(string))
				}
			} else {
				var object map[string]json.RawMessage
				if json.Unmarshal(result.Payload, &object) != nil || len(object) == 0 || creative.ValidateFields(object) != nil {
					state = "failed"
				} else {
					payload = result.Payload
				}
			}
		}
		states = append(states, state)
	}
	view.Status, view.Warning = nova.Aggregate(states)
	if len(images) > 0 || len(payload) > 0 {
		view.Result = &nova.TaskResult{Images: images, Payload: payload}
	}
	if view.Status == "completed" || view.Status == "failed" || view.Status == "cancelled" {
		if !completed.IsZero() {
			view.CompletedAt = &completed
		}
		if view.Status == "failed" {
			view.Error = "The business task failed or its result is no longer available"
		}
	}
	return view, assets, nil
}

func (s *Server) novaGetTask(w http.ResponseWriter, r *http.Request) {
	run, err := s.Store.NovaRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.novaError(w, err)
		return
	}
	if run.CancelRequestedAt != nil && run.CancelDispatchedAt == nil {
		run, err = s.Store.CancelNovaRun(r.Context(), run.UserID, run.ID)
		if err != nil {
			s.novaError(w, err)
			return
		}
	}
	view, _, err := s.novaRunView(r.Context(), run)
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) novaListTasks(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			s.novaError(w, nova.ErrInvalid)
			return
		}
		limit = n
	}
	before := r.URL.Query().Get("before")
	if len(before) > 100 {
		s.novaError(w, nova.ErrInvalid)
		return
	}
	runs, err := s.Store.NovaRuns(r.Context(), currentUser(r).ID, before, limit)
	if err != nil {
		s.novaError(w, err)
		return
	}
	views := []nova.TaskView{}
	for _, run := range runs {
		view, _, err := s.novaRunView(r.Context(), run)
		if err != nil {
			s.novaError(w, err)
			return
		}
		views = append(views, view)
	}
	cursor := ""
	if len(runs) == limit {
		cursor = runs[len(runs)-1].ID
	}
	writeJSON(w, 200, map[string]any{"tasks": views, "nextCursor": cursor})
}

func (s *Server) novaAck(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.AcknowledgeNovaRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "acknowledged": true, "acknowledgedAt": n.AcknowledgedAt})
}

func (s *Server) novaCancel(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CancelNovaRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.novaError(w, err)
		return
	}
	view, _, err := s.novaRunView(r.Context(), n)
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) novaImage(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 || index >= nova.MaxParallel {
		s.novaError(w, store.ErrNotFound)
		return
	}
	run, err := s.Store.NovaRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil || run.Kind != "image" {
		s.novaError(w, store.ErrNotFound)
		return
	}
	_, assets, err := s.novaRunView(r.Context(), run)
	if err != nil {
		s.novaError(w, err)
		return
	}
	if index >= len(assets) {
		s.novaError(w, store.ErrNotFound)
		return
	}
	a := assets[index]
	if !strings.HasPrefix(a.ContentType, "image/") {
		s.novaError(w, store.ErrNotFound)
		return
	}
	f, err := s.Files.Open(a.StorageKey)
	if err != nil {
		s.novaError(w, store.ErrNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "nova-image", a.CreatedAt, f)
}

func (s *Server) novaBusinessError(w http.ResponseWriter, r *http.Request, err error) {
	capture := &novaCapture{header: make(http.Header)}
	s.databaseError(capture, r, err)
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(capture.body.Bytes(), &e) != nil || e.Code == "" {
		s.novaError(w, err)
		return
	}
	novaFail(w, capture.status, e.Code, e.Message)
}
