package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/nova"
	"github.com/suakitsu/picpilot-platform-go/core/security"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

// registerNovaRoutes is mounted by the parent, independently of static assets.
func (s *Server) registerNovaRoutes() {
	probe, _ := http.NewRequest(http.MethodGet, "/api/nova/config", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/nova/config" {
		return
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/nova/config":                 s.novaConfig,
		"GET /api/nova/prompts":                s.novaPrompts,
		"GET /api/nova/blacklist":              s.novaBlacklist,
		"POST /api/nova/prompt-gallery/verify": s.novaVerifyGallery,
		"PUT /api/nova/settings":               s.novaPutSettings,
		"GET /api/nova/proxy/models":           s.novaModels,
		"GET /api/nova/queue-status":           s.novaQueue,
		"POST /api/nova/tasks":                 s.novaSubmitImages,
		"GET /api/nova/tasks":                  s.novaListTasks,
		"GET /api/nova/tasks/{id}":             s.novaGetTask,
		"POST /api/nova/tasks/{id}/ack":        s.novaAck,
		"POST /api/nova/tasks/{id}/cancel":     s.novaCancel,
		"GET /api/nova/images/{id}/{index}":    s.novaImage,
		"POST /api/nova/proxy/text":            s.novaText,
		"GET /api/nova/tasks/{id}/response":    s.novaTextResponse,
	} {
		s.mux.HandleFunc(pattern, s.novaAuth(handler))
	}
}

// Use the application's auth/CSRF policy, adapting only its error envelope.
func (s *Server) novaAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Header.Get("X-API-Key") != "" {
			novaFail(w, 400, nova.ErrCredentials.Code, nova.ErrCredentials.Message)
			return
		}
		capture := &novaCapture{header: make(http.Header)}
		entered := false
		s.auth(func(_ http.ResponseWriter, authenticated *http.Request) { entered = true; next(w, authenticated) })(capture, r)
		if !entered {
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(capture.body.Bytes(), &e) != nil || e.Code == "" {
				novaFail(w, 401, "UNAUTHORIZED", "Please sign in")
				return
			}
			novaFail(w, capture.status, e.Code, e.Message)
		}
	}
}

type novaCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *novaCapture) Header() http.Header { return w.header }
func (w *novaCapture) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *novaCapture) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}

func novaFail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": message, "code": code, "message": message})
}
func (s *Server) novaError(w http.ResponseWriter, err error) {
	var n *nova.Error
	var workflow *workflowError
	switch {
	case errors.As(err, &n):
		novaFail(w, 400, n.Code, n.Message)
	case errors.As(err, &workflow):
		novaFail(w, workflow.status, workflow.code, workflow.message)
	case errors.Is(err, store.ErrNotFound):
		novaFail(w, 404, "NOT_FOUND", "Task or asset not found")
	case errors.Is(err, store.ErrConflict):
		novaFail(w, 409, "IDEMPOTENCY_CONFLICT", "The key or revision conflicts with an earlier request")
	case errors.Is(err, store.ErrNovaQueueFull):
		w.Header().Set("Retry-After", "3")
		novaFail(w, 429, "QUEUE_FULL", "Too many pending Nova outputs")
	case errors.Is(err, store.ErrInsufficientCredits):
		novaFail(w, 402, "INSUFFICIENT_CREDITS", "Insufficient credits")
	case errors.Is(err, store.ErrDisabled):
		novaFail(w, 403, "DISABLED", "Account is disabled")
	default:
		novaFail(w, 500, "INTERNAL_ERROR", "The request could not be completed")
	}
}

func novaDecode(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		novaFail(w, 415, "JSON_REQUIRED", "Expected application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		novaFail(w, 413, "REQUEST_TOO_LARGE", "Request is too large")
		return false
	}
	if nova.Decode(data, dst) != nil {
		novaFail(w, 400, nova.ErrInvalid.Code, nova.ErrInvalid.Message)
		return false
	}
	return true
}

func novaKey(r *http.Request) (string, error) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", nova.ErrIdempotency
	}
	return values[0], nova.ValidateKey(values[0])
}

func (s *Server) novaScope(r *http.Request) string {
	if s.Config.Edition == config.Personal {
		return currentUser(r).ID
	}
	return "platform"
}

func (s *Server) novaConfig(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.NovaSettings(r.Context(), s.novaScope(r))
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, nova.Config{PromptGalleryMode: n.Mode, PromptGalleryPasswordEnabled: n.Mode == "2" && n.PasswordHash != ""})
}
func (s *Server) novaPrompts(w http.ResponseWriter, r *http.Request) {
	scope := s.novaScope(r)
	n, err := s.Store.NovaSettings(r.Context(), scope)
	if err != nil {
		s.novaError(w, err)
		return
	}
	allowed, err := s.Store.NovaGalleryAllowed(r.Context(), currentUser(r).ID, scope, n)
	if err != nil {
		s.novaError(w, err)
		return
	}
	if !allowed {
		novaFail(w, 403, "GALLERY_LOCKED", "Prompt gallery is locked or hidden")
		return
	}
	writeJSON(w, 200, n.Prompts)
}
func (s *Server) novaBlacklist(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.NovaSettings(r.Context(), s.novaScope(r))
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keywords": n.Keywords})
}
func (s *Server) novaVerifyGallery(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow("nova-gallery:"+currentUser(r).ID, 5, time.Minute) {
		w.Header().Set("Retry-After", "60")
		novaFail(w, 429, "RATE_LIMITED", "Try again later")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !novaDecode(w, r, 1024, &in) {
		return
	}
	if len(in.Password) > 256 {
		s.novaError(w, nova.ErrInvalid)
		return
	}
	scope := s.novaScope(r)
	n, err := s.Store.NovaSettings(r.Context(), scope)
	if err != nil {
		s.novaError(w, err)
		return
	}
	valid := n.Mode != "3" && (n.Mode == "1" || n.PasswordHash == "" || security.VerifyPassword(n.PasswordHash, in.Password))
	if valid && n.Mode == "2" && n.PasswordHash != "" {
		if err = s.Store.GrantNovaGallery(r.Context(), currentUser(r).ID, scope, n.Revision); err != nil {
			s.novaError(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": valid})
}

func (s *Server) novaPutSettings(w http.ResponseWriter, r *http.Request) {
	if s.Config.Edition == config.Platform {
		role := currentUser(r).Role
		if role != "admin" && role != "super_admin" {
			novaFail(w, 403, "FORBIDDEN", "Administrator access is required")
			return
		}
	} else if currentUser(r).Role != "owner" {
		novaFail(w, 403, "FORBIDDEN", "Personal owner access is required")
		return
	}
	var in struct {
		nova.Settings
		Password *string `json:"password,omitempty"`
		Revision int64   `json:"revision"`
	}
	if !novaDecode(w, r, 2<<20, &in) {
		return
	}
	n, err := s.Store.PutNovaSettings(r.Context(), s.novaScope(r), in.Settings, in.Password, in.Revision)
	if err != nil {
		s.novaError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"revision": n.Revision, "config": nova.Config{PromptGalleryMode: n.Mode, PromptGalleryPasswordEnabled: n.Mode == "2" && n.PasswordHash != ""}})
}

func (s *Server) novaModels(w http.ResponseWriter, r *http.Request) {
	q, err := parseNovaModelQuery(r)
	if err != nil {
		s.novaError(w, err)
		return
	}
	ps, err := s.Store.Providers(r.Context(), s.novaScope(r))
	if err != nil {
		s.novaError(w, err)
		return
	}
	models := []map[string]string{}
	seen := map[string]bool{}
	for _, p := range ps {
		if !p.Enabled || p.Capability == "video" || q.capability != "" && p.Capability != q.capability {
			continue
		}
		protocol := p.Protocol
		if p.Capability == "image" {
			protocol = "openai"
		} else {
			if protocol == "" {
				protocol = "openai_chat_completions"
			}
			protocol = strings.ReplaceAll(protocol, "_", "-")
		}
		if q.protocol != "" && q.protocol != protocol || p.Model == "" {
			continue
		}
		key := p.Capability + ":" + protocol + ":" + p.Model
		if seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, map[string]string{"id": p.Model, "capability": p.Capability, "protocol": protocol})
	}
	writeJSON(w, 200, map[string]any{"data": models})
}

type novaModelQuery struct{ capability, protocol string }

func parseNovaModelQuery(r *http.Request) (novaModelQuery, error) {
	q := novaModelQuery{}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, nova.ErrInvalid
	}
	for k, v := range values {
		if len(v) != 1 {
			return q, nova.ErrInvalid
		}
		switch k {
		case "apiKey", "baseUrl":
			if v[0] != "" {
				return q, nova.ErrCredentials
			}
		case "protocol":
			q.protocol = v[0]
		case "capability":
			q.capability = v[0]
		default:
			return q, nova.ErrInvalid
		}
	}
	if q.capability != "" && q.capability != "image" && q.capability != "text" {
		return q, nova.ErrInvalid
	}
	if q.protocol != "" && q.protocol != "openai" && q.protocol != "openai-chat-completions" && q.protocol != "openai-responses" {
		return q, nova.ErrProtocol
	}
	return q, nil
}

func (s *Server) novaQueue(w http.ResponseWriter, r *http.Request) {
	queued, processing, err := s.Store.NovaQueueCounts(r.Context(), currentUser(r).ID)
	if err != nil {
		s.novaError(w, err)
		return
	}
	ps, err := s.Store.Providers(r.Context(), s.novaScope(r))
	if err != nil {
		s.novaError(w, err)
		return
	}
	capacity := 0
	for _, p := range ps {
		if p.Enabled && (p.Capability == "image" || p.Capability == "text") {
			capacity += p.MaxConcurrency
		}
	}
	if s.Config.DemoMode && capacity == 0 {
		capacity = 1
	}
	q := nova.QueueStatus{ConcurrencyLimit: capacity, ConfiguredConcurrency: capacity, ProcessingCount: processing, QueuedCount: queued, PendingCount: queued + processing, MaxQueueSize: nova.MaxPending, RemainingQueueSlots: max(0, nova.MaxPending-queued-processing), DisplayConcurrency: processing, DisplayQueued: queued, AcceptingNewTasks: capacity > 0 && queued+processing < nova.MaxPending}
	if !q.AcceptingNewTasks {
		q.RetryAfterSeconds = 3
		q.ServerMessage = "No configured capacity or pending output limit reached"
	}
	writeJSON(w, 200, q)
}

// RunNovaMaintenance resumes only cancellation intents. Business jobs and
// settlement remain owned by the parent's runner. Safe on multiple instances.
func (s *Server) RunNovaMaintenance(ctx context.Context) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Store.ReconcileNovaCancellations(ctx, 50); err != nil && ctx.Err() == nil {
			s.Logger.Warn("nova cancellation reconciliation failed", "sqlstate", store.SQLState(err))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func novaWait(r *http.Request) (time.Duration, error) {
	if len(r.Header.Values("Prefer")) > 1 {
		return 0, nova.ErrInvalid
	}
	value := r.Header.Get("Prefer")
	if value == "" {
		return 20 * time.Second, nil
	}
	if value == "respond-async" {
		return 0, nil
	}
	if !strings.HasPrefix(value, "wait=") {
		return 0, nova.ErrInvalid
	}
	n, err := strconv.Atoi(strings.TrimPrefix(value, "wait="))
	if err != nil || n < 0 || n > 25 {
		return 0, nova.ErrInvalid
	}
	return time.Duration(n) * time.Second, nil
}
