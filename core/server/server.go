package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/suakitsu/picpilot-platform/core/billing"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/mailer"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/risk"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
	wp "github.com/suakitsu/picpilot-platform/core/workerprotocol"
)

type Server struct {
	Config            config.Config
	Store             *store.Store
	Vault             *security.Vault
	Sessions          *security.Sessions
	Logger            *slog.Logger
	Files             storage.Files
	NormalizeImage    func(context.Context, io.Reader) (storage.Image, error)
	Mailer            mailer.Sender
	IdentityHTTP      *http.Client
	BillingGateway    billing.Gateway
	mux               *http.ServeMux
	limiter           *limiter
	dummyPasswordHash string
	cleanupCursor     string
}

type contextKey string

const userKey contextKey = "user"
const requestKey contextKey = "request-id"

func New(c config.Config, db *store.Store, logger *slog.Logger) (*Server, error) {
	if db.Edition != c.Edition {
		return nil, store.ErrEditionMismatch
	}
	vault, err := security.NewVault(c.EncryptionKey)
	if err != nil {
		return nil, err
	}
	sessions, err := security.NewSessions(c.AuthKey, "picpilot-"+string(c.Edition))
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	dummy, err := security.HashPassword("only-used-for-timing-of-unknown-accounts")
	if err != nil {
		return nil, err
	}
	if c.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	var files storage.Files
	var local *storage.Local
	if c.AssetStorageBackend == "object" {
		object, objectErr := storage.NewObject(storage.ObjectOptions{
			Endpoint: c.ObjectStorageEndpoint, Bucket: c.ObjectStorageBucket, Region: c.ObjectStorageRegion,
			Prefix: c.ObjectStoragePrefix, AccessKey: c.ObjectStorageAccess, SecretKey: c.ObjectStorageSecret,
			SessionToken: c.ObjectStorageSession, StagingDir: filepath.Join(c.DataDir, "object-staging"),
			AllowInsecureHTTP: c.ObjectStorageInsecure, VirtualHostStyle: c.ObjectStorageVirtual,
			OperationTimeout:     time.Duration(c.ObjectStorageOpTimeoutSeconds) * time.Second,
			UploadTimeout:        time.Duration(c.ObjectStorageUploadTimeoutSeconds) * time.Second,
			ReadTimeout:          time.Duration(c.ObjectStorageReadTimeoutSeconds) * time.Second,
			MaxConcurrentUploads: c.ObjectStorageMaxUploads,
		})
		if objectErr != nil {
			return nil, objectErr
		}
		files = object
	} else {
		local, err = storage.NewLocal(filepath.Join(c.DataDir, "assets"))
		if err != nil {
			return nil, err
		}
		files = storage.WrapLocal(local)
	}
	normalizeImage := func(_ context.Context, reader io.Reader) (storage.Image, error) {
		return storage.NormalizeImage(reader)
	}
	if c.HEIFEnabled {
		converter, converterErr := storage.NewHEIFConverter(storage.HEIFOptions{
			VIPSPath: c.HEIFVIPSPath, VIPSHeaderPath: c.HEIFVIPSHeaderPath, TempDir: c.HEIFTempDir,
			Timeout: time.Duration(c.HEIFTimeoutSeconds) * time.Second, MaxConcurrent: c.HEIFMaxConcurrent,
		})
		if converterErr != nil {
			_ = files.Close()
			return nil, converterErr
		}
		normalizeImage = converter.Normalize
	}
	s := &Server{Config: c, Store: db, Vault: vault, Sessions: sessions, Logger: logger, Files: files, NormalizeImage: normalizeImage, mux: http.NewServeMux(), limiter: &limiter{entries: make(map[string]rateWindow)}, dummyPasswordHash: dummy}
	smtpSender, err := mailer.New(c.SMTP)
	if err != nil {
		files.Close()
		return nil, err
	}
	if smtpSender != nil {
		s.Mailer = smtpSender
	}
	s.IdentityHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	paymentHTTP, err := provider.NewHTTPClient(provider.Options{AllowLocalDevelopment: c.AllowPrivateProviders, Timeout: 60 * time.Second, MaxResponseBytes: 1 << 20})
	if err != nil {
		files.Close()
		return nil, err
	}
	s.BillingGateway = &billing.Client{HTTP: paymentHTTP}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	s.registerTaskRoutes()
	s.registerEmailAuthRoutes()
	s.registerIdentityRoutes()
	s.registerWorkerRoutes()
	s.registerProjectRoutes()
	s.registerAssistantRoutes()
	s.registerVideoRoutes()
	s.registerBillingRoutes()
	s.registerAdminRoutes()
	s.registerMessageRoutes()
	s.registerAdminAIRoutes()
	s.registerCommerceRoutes()
	s.registerTranslationRoutes()
	s.registerNovaRoutes()
	s.mux.HandleFunc("GET /api/ai/capabilities", s.auth(s.aiCapabilities))
	s.mux.HandleFunc("GET /api/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/health/ready", s.ready)
	s.mux.HandleFunc("GET /api/public/ping", func(w http.ResponseWriter, r *http.Request) { ok(w, r, map[string]any{"status": "ok"}) })
	s.mux.HandleFunc("GET /api/public/config", s.publicConfig)
	s.mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	s.mux.HandleFunc("POST /api/setup", s.setup)
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("POST /api/auth/admin/login", s.login)
	s.mux.HandleFunc("POST /api/auth/logout", s.auth(s.logout))
	s.mux.HandleFunc("GET /api/auth/me", s.auth(s.me))
	s.mux.HandleFunc("GET /api/me", s.auth(s.me))
	s.mux.HandleFunc("PATCH /api/auth/me", s.auth(s.updateProfile))
	s.mux.HandleFunc("PATCH /api/me", s.auth(s.updateProfile))
	s.mux.HandleFunc("POST /api/auth/change-password", s.auth(s.changePassword))
	s.mux.HandleFunc("GET /api/me/providers", s.auth(s.personal(s.listProviders)))
	s.mux.HandleFunc("PUT /api/me/providers/{id}", s.auth(s.personal(s.putProvider)))
	s.mux.HandleFunc("DELETE /api/me/providers/{id}", s.auth(s.personal(s.deleteProvider)))
	s.mux.HandleFunc("POST /api/me/providers/{id}/check", s.auth(s.personal(s.checkProvider)))
	s.mux.HandleFunc("GET /api/admin/providers", s.auth(s.admin(s.listProviders)))
	s.mux.HandleFunc("PUT /api/admin/providers/{id}", s.auth(s.admin(s.putProvider)))
	s.mux.HandleFunc("DELETE /api/admin/providers/{id}", s.auth(s.admin(s.deleteProvider)))
	s.mux.HandleFunc("POST /api/admin/providers/{id}/check", s.auth(s.admin(s.checkProvider)))
	s.mux.HandleFunc("POST /api/assets", s.auth(s.uploadAsset))
	s.mux.HandleFunc("POST /api/me/avatar", s.auth(s.uploadAvatar))
	s.mux.HandleFunc("POST /api/projects/{id}/product-images", s.auth(s.uploadProjectImage))
	s.mux.HandleFunc("GET /api/files/assets/{id}", s.serveAsset)
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, http.StatusNotFound, "NOT_FOUND", "Endpoint not found")
	})
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := security.NewID("req")
		r = r.WithContext(context.WithValue(r.Context(), requestKey, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if recover() != nil {
				s.Logger.Error("request panic", "request_id", id, "method", r.Method)
				if strings.HasPrefix(r.URL.Path, wp.BasePath+"/") {
					s.workerError(w, r, wp.NewError("WORKER_INTERNAL_ERROR"))
				} else {
					fail(w, r, 500, "INTERNAL_ERROR", "The request could not be completed")
				}
			}
		}()
		// Machine-to-machine Worker requests keep their own raw error envelope,
		// including unknown methods and paths. Browser CORS never changes v1.
		if strings.HasPrefix(r.URL.Path, wp.BasePath+"/") {
			if r.Header.Get("Origin") != "" || r.Method == http.MethodOptions {
				s.workerError(w, r, wp.NewError("REQUEST_SCHEMA_INVALID"))
				return
			}
			s.mux.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.originAllowed(origin) {
				fail(w, r, 403, "ORIGIN_NOT_ALLOWED", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.Config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if header := r.Header.Get("Authorization"); header != "" {
			scheme, value, found := strings.Cut(header, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") {
				fail(w, r, 401, "UNAUTHORIZED", "Please sign in")
				return
			}
			token = value
		} else if cookie, err := r.Cookie("picpilot_session"); err == nil {
			if r.Method != "GET" && r.Method != "HEAD" && !s.originAllowed(r.Header.Get("Origin")) {
				fail(w, r, 403, "CSRF_REJECTED", "A same-origin request is required")
				return
			}
			token = cookie.Value
		}
		if len(token) > 8192 || token == "" {
			fail(w, r, 401, "UNAUTHORIZED", "Please sign in")
			return
		}
		claims, err := s.Sessions.Parse(token, time.Now())
		if err != nil {
			fail(w, r, 401, "UNAUTHORIZED", "Please sign in again")
			return
		}
		u, err := s.Store.UserByID(r.Context(), claims.Subject)
		if err != nil || u.Status != "active" || u.AuthVersion != claims.Version {
			fail(w, r, 401, "UNAUTHORIZED", "Please sign in again")
			return
		}
		if err := s.Store.RecordUserActivity(r.Context(), u.ID); err != nil && r.Context().Err() == nil {
			s.Logger.Error("user activity persistence failed", "sqlstate", store.SQLState(err))
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}
func requestID(r *http.Request) string { id, _ := r.Context().Value(requestKey).(string); return id }

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if s.Config.Edition != config.Platform || (u.Role != "admin" && u.Role != "super_admin") {
			fail(w, r, 403, "FORBIDDEN", "Administrator access is required")
			return
		}
		next(w, r)
	}
}
func (s *Server) personal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Config.Edition != config.Personal {
			fail(w, r, 404, "NOT_FOUND", "Endpoint not found")
			return
		}
		next(w, r)
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		fail(w, r, 415, "CONTENT_TYPE_INVALID", "Expected application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid or oversized JSON request")
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, r, 400, "INVALID_REQUEST", "Expected one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func ok(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, 200, map[string]any{"success": true, "code": "OK", "message": "", "data": data, "request_id": requestID(r)})
}
func fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"success": false, "code": code, "message": message, "data": nil, "request_id": requestID(r)})
}

func (s *Server) databaseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, risk.ErrBlocked):
		fail(w, r, 422, "CONTENT_BLOCKED", "This input was blocked before generation; no credits were charged")
	case errors.Is(err, risk.ErrAccountRestricted), errors.Is(err, store.ErrDisabled):
		fail(w, r, 403, "ACCOUNT_RESTRICTED", "This account cannot generate content")
	case errors.Is(err, risk.ErrInvalidRequest):
		fail(w, r, 400, "INVALID_GENERATION_INPUT", "Invalid generation input")
	case errors.Is(err, store.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "Record not found")
	case errors.Is(err, store.ErrConflict):
		fail(w, r, 409, "CONFLICT", "The record changed or this request conflicts with an earlier request")
	case errors.Is(err, store.ErrAlreadySetup):
		fail(w, r, 409, "ALREADY_INITIALIZED", "This instance has already been initialized")
	case errors.Is(err, store.ErrInsufficientCredits):
		fail(w, r, 402, "INSUFFICIENT_CREDITS", "Insufficient credits")
	default:
		s.Logger.Error("database operation failed", "request_id", requestID(r), "sqlstate", store.SQLState(err))
		fail(w, r, 500, "INTERNAL_ERROR", "The request could not be completed")
	}
}

type rateWindow struct {
	Count int
	Until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]rateWindow
}

func (l *limiter) allow(key string, max int, period time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) > 10000 {
		for k, v := range l.entries {
			if !v.Until.After(now) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 10000 {
			return false
		}
	}
	w := l.entries[key]
	if !w.Until.After(now) {
		w = rateWindow{Until: now.Add(period)}
	}
	w.Count++
	l.entries[key] = w
	return w.Count <= max
}
func (s *Server) authRate(w http.ResponseWriter, r *http.Request) bool {
	host := s.clientAddress(r)
	if !s.limiter.allow("auth:"+host, 30, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		fail(w, r, 429, "RATE_LIMITED", "Too many attempts; try again later")
		return false
	}
	return true
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.Store.Pool.Ping(ctx) != nil {
		writeJSON(w, 503, map[string]any{"status": "unavailable"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}
func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	needs, err := s.Store.NeedsSetup(r.Context())
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"edition": s.Config.Edition, "setup_required": needs, "billing_enabled": s.Config.Edition == config.Platform, "registration_enabled": s.Config.RegistrationEnabled, "demo_mode": s.Config.DemoMode, "email_login_enabled": s.Mailer != nil, "turnstile_site_key": s.Config.TurnstileSiteKey})
}
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := s.Store.NeedsSetup(r.Context())
	if err != nil {
		s.databaseError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"needs_setup": needs})
}
