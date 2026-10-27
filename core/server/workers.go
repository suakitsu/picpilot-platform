package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/storage"
	"github.com/suakitsu/picpilot-platform-go/core/store"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

type workerPrincipalKey struct{}

// The parent calls this from routes(). Repeated registration is harmless for
// integration tests and applications which install feature routes explicitly.
func (s *Server) registerWorkerRoutes() {
	probe := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: wp.BasePath + "/heartbeat"}}
	if _, pattern := s.mux.Handler(probe); pattern == "POST "+wp.BasePath+"/heartbeat" {
		return
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"POST " + wp.BasePath + "/heartbeat", s.workerHeartbeat},
		{"POST " + wp.BasePath + "/claim", s.workerClaim},
		{"POST " + wp.BasePath + "/jobs/{jobId}/renew", s.workerRenew},
		{"POST " + wp.BasePath + "/jobs/{jobId}/artifacts", s.workerArtifact},
		{"POST " + wp.BasePath + "/jobs/{jobId}/complete", s.workerComplete},
		{"POST " + wp.BasePath + "/jobs/{jobId}/fail", s.workerFail},
		{"GET " + wp.BasePath + "/inputs/{jobId}/{index}", s.workerInput},
	} {
		s.mux.HandleFunc(route.path, s.workerAuth(route.handler))
	}
	s.mux.HandleFunc(wp.BasePath+"/", s.workerAuth(func(w http.ResponseWriter, r *http.Request) { s.workerError(w, r, wp.NewError("JOB_NOT_FOUND")) }))
	for _, root := range []string{"/api/admin/ai-config", "/api/me/ai-config"} {
		guard := s.admin
		if strings.HasPrefix(root, "/api/me/") {
			guard = s.workerPersonalOwner
		}
		s.mux.HandleFunc("GET "+root+"/workers", s.auth(guard(s.workerAdminState)))
		s.mux.HandleFunc("POST "+root+"/worker-tokens", s.auth(guard(s.workerTokenCreate)))
		s.mux.HandleFunc("POST "+root+"/worker-tokens/{tokenId}/regenerate", s.auth(guard(s.workerTokenRegenerate)))
		s.mux.HandleFunc("POST "+root+"/worker-tokens/{tokenId}/revoke", s.auth(guard(s.workerTokenRevoke)))
	}
}

func (s *Server) workerPersonalOwner(next http.HandlerFunc) http.HandlerFunc {
	return s.personal(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != "owner" {
			fail(w, r, 403, "FORBIDDEN", "Owner access is required")
			return
		}
		next(w, r)
	})
}
func (s *Server) workerOptions() store.WorkerOptions {
	return store.WorkerOptions{PublicBaseURL: s.Config.PublicBaseURL, SigningKey: s.Config.WorkerSigningKey, GraceSeconds: s.Config.WorkerGraceSeconds, InputTTLSeconds: s.Config.WorkerInputTTLSeconds}
}
func workerPrincipal(r *http.Request) store.WorkerPrincipal {
	p, _ := r.Context().Value(workerPrincipalKey{}).(store.WorkerPrincipal)
	return p
}

// The shared policy accepts TLS or an explicitly trusted immediate proxy peer.
func (s *Server) workerHTTPS(r *http.Request) bool { return s.secureRequest(r) }

func (s *Server) workerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if recover() != nil {
				s.Logger.Error("worker request panic", "request_id", requestID(r))
				s.workerError(w, r, wp.NewError("WORKER_INTERNAL_ERROR"))
			}
		}()
		if !s.workerHTTPS(r) {
			s.workerError(w, r, wp.NewError("HTTPS_REQUIRED"))
			return
		}
		headers := r.Header.Values("Authorization")
		if len(headers) == 0 {
			s.workerError(w, r, wp.NewError("WORKER_TOKEN_MISSING"))
			return
		}
		if len(headers) != 1 {
			s.workerError(w, r, wp.NewError("WORKER_TOKEN_INVALID"))
			return
		}
		scheme, token, ok := strings.Cut(headers[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n,") {
			s.workerError(w, r, wp.NewError("WORKER_TOKEN_INVALID"))
			return
		}
		p, err := s.Store.AuthenticateWorkerToken(r.Context(), token, s.Config.WorkerPepper)
		if err != nil {
			s.workerError(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), workerPrincipalKey{}, p)))
	}
}
func (s *Server) workerError(w http.ResponseWriter, r *http.Request, err error) {
	var protocol *wp.ProtocolError
	if !errors.As(err, &protocol) {
		switch {
		case errors.Is(err, storage.ErrInvalidImage):
			protocol = wp.NewError("ARTIFACT_INVALID")
		case errors.Is(err, storage.ErrImageTooLarge):
			protocol = wp.NewError("ARTIFACT_INVALID")
		default:
			s.Logger.Error("worker operation failed", "request_id", requestID(r), "sqlstate", store.SQLState(err))
			protocol = wp.NewError("WORKER_INTERNAL_ERROR")
		}
	}
	writeJSON(w, protocol.HTTPStatus, protocol.Response())
}
func readWorkerJSON[T wp.Message](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return zero, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	r.Body = http.MaxBytesReader(w, r.Body, wp.MaxRequestBytes+8192)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return zero, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	return wp.Decode[T](raw)
}
func (s *Server) workerHeartbeat(w http.ResponseWriter, r *http.Request) {
	in, err := readWorkerJSON[wp.HeartbeatRequest](w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	out, err := s.Store.WorkerHeartbeat(r.Context(), workerPrincipal(r), in)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) workerClaim(w http.ResponseWriter, r *http.Request) {
	in, err := readWorkerJSON[wp.ClaimRequest](w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	start := time.Now()
	deadline := start.Add(time.Duration(in.WaitSeconds) * time.Second)
	for {
		task, err := s.Store.ClaimWorkerJob(r.Context(), workerPrincipal(r), in, s.workerOptions())
		if err != nil {
			s.workerError(w, r, err)
			return
		}
		now := time.Now()
		if task != nil || !now.Before(deadline) {
			response, err := wp.NewClaimResponse(task, now.UTC(), now.Sub(start).Milliseconds())
			if err != nil {
				s.workerError(w, r, err)
				return
			}
			writeJSON(w, 200, response)
			return
		}
		wait := min(500*time.Millisecond, time.Until(deadline))
		timer := time.NewTimer(wait)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Server) workerRenew(w http.ResponseWriter, r *http.Request) {
	in, err := readWorkerJSON[wp.LeaseRequest](w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	out, err := s.Store.RenewWorkerJob(r.Context(), workerPrincipal(r), r.PathValue("jobId"), in, s.workerOptions())
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) workerComplete(w http.ResponseWriter, r *http.Request) {
	in, err := readWorkerJSON[wp.CompleteRequest](w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	out, err := s.Store.CompleteWorkerJob(r.Context(), workerPrincipal(r), r.PathValue("jobId"), in, s.workerOptions())
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) workerFail(w http.ResponseWriter, r *http.Request) {
	in, err := readWorkerJSON[wp.FailRequest](w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	out, err := s.Store.FailWorkerJob(r.Context(), workerPrincipal(r), r.PathValue("jobId"), in, s.workerOptions())
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func readWorkerArtifact(w http.ResponseWriter, r *http.Request) (wp.ArtifactForm, []byte, string, error) {
	zero := wp.ArtifactForm{}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	r.Body = http.MaxBytesReader(w, r.Body, wp.DefaultMaxArtifactBytes+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	fields := map[string][]string{}
	files := []string{}
	var data []byte
	filename := ""
	contentType := ""
	parts := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return zero, nil, "", wp.NewError("ARTIFACT_READ_FAILED")
		}
		parts++
		if parts > 5 {
			part.Close()
			return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
		}
		name := part.FormName()
		if part.FileName() != "" {
			if name != "file" || len(files) > 0 {
				part.Close()
				return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
			}
			files = append(files, name)
			filename = part.FileName()
			contentType = part.Header.Get("Content-Type")
			data, err = io.ReadAll(io.LimitReader(part, wp.DefaultMaxArtifactBytes+1))
			part.Close()
			if err != nil {
				return zero, nil, "", wp.NewError("ARTIFACT_READ_FAILED")
			}
			if int64(len(data)) > wp.DefaultMaxArtifactBytes {
				return zero, nil, "", wp.NewError("ARTIFACT_INVALID")
			}
		} else {
			switch name {
			case "workerId", "leaseId", "idempotencyKey", "sha256":
			default:
				part.Close()
				return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
			}
			v, err := io.ReadAll(io.LimitReader(part, 513))
			part.Close()
			if err != nil || len(v) > 512 {
				return zero, nil, "", wp.NewError("REQUEST_SCHEMA_INVALID")
			}
			fields[name] = append(fields[name], string(v))
		}
	}
	form, err := wp.ValidateMultipartFields(fields, files)
	if err != nil {
		return zero, nil, "", err
	}
	if err = wp.VerifyArtifactSource(form.SHA256, data, wp.DefaultMaxArtifactBytes); err != nil {
		return zero, nil, "", err
	}
	detected := http.DetectContentType(data)
	if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
		return zero, nil, "", wp.NewError("ARTIFACT_MIME_UNSUPPORTED")
	}
	if contentType != "" {
		media, _, err := mime.ParseMediaType(contentType)
		if err != nil || (media != "application/octet-stream" && media != detected) {
			return zero, nil, "", wp.NewError("ARTIFACT_MIME_UNSUPPORTED")
		}
	}
	return form, data, filename, nil
}
func (s *Server) workerArtifact(w http.ResponseWriter, r *http.Request) {
	form, source, filename, err := readWorkerArtifact(w, r)
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	var created store.Asset
	out, err := s.Store.SaveWorkerArtifact(r.Context(), workerPrincipal(r), r.PathValue("jobId"), form, source, s.workerOptions(), func(job store.WorkerJob) (store.Asset, error) {
		var err error
		created, err = s.saveImage(r.Context(), job.UserID, job.ProjectID, job.TaskID, "worker-candidate", filename, bytes.NewReader(source))
		return created, err
	})
	if created.ID != "" && (err != nil || out.Idempotent) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		removed, cleanupErr := s.Store.DiscardWorkerArtifactAsset(ctx, created.ID)
		cancel()
		if cleanupErr == nil && removed {
			_ = s.Files.Remove(created.StorageKey)
		}
	}
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) workerInput(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 3 || len(q["workerId"]) != 1 || len(q["expires"]) != 1 || len(q["signature"]) != 1 {
		s.workerError(w, r, wp.NewError("INPUT_SIGNATURE_INVALID"))
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.workerError(w, r, wp.NewError("INPUT_NOT_FOUND"))
		return
	}
	expires, err := strconv.ParseInt(q.Get("expires"), 10, 64)
	if err != nil {
		s.workerError(w, r, wp.NewError("INPUT_SIGNATURE_INVALID"))
		return
	}
	a, err := s.Store.WorkerInputAsset(r.Context(), workerPrincipal(r), r.PathValue("jobId"), q.Get("workerId"), index, expires, q.Get("signature"), s.workerOptions())
	if err != nil {
		s.workerError(w, r, err)
		return
	}
	f, err := s.Files.Open(a.StorageKey)
	if err != nil {
		s.workerError(w, r, wp.NewError("INPUT_NOT_FOUND"))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": a.OriginalFilename}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, a.OriginalFilename, a.CreatedAt, f)
}

func (s *Server) workerScope(r *http.Request) string {
	if s.Config.Edition == config.Personal {
		return currentUser(r).ID
	}
	return "platform"
}
func (s *Server) workerAdminError(w http.ResponseWriter, r *http.Request, err error) {
	var p *wp.ProtocolError
	if errors.As(err, &p) {
		fail(w, r, p.HTTPStatus, p.Detail.Code, p.Detail.Message)
		return
	}
	s.databaseError(w, r, err)
}
func (s *Server) workerAdminState(w http.ResponseWriter, r *http.Request) {
	state, err := s.Store.WorkerAdminState(r.Context(), s.workerScope(r))
	if err != nil {
		s.workerAdminError(w, r, err)
		return
	}
	state["token_pepper_configured"] = len(s.Config.WorkerPepper) >= 32
	state["signing_secret_configured"] = len(s.Config.WorkerSigningKey) >= 32
	ok(w, r, state)
}
func (s *Server) workerTokenCreate(w http.ResponseWriter, r *http.Request) {
	if !s.workerHTTPS(r) {
		fail(w, r, 426, "HTTPS_REQUIRED", "Worker token issuance requires HTTPS")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	out, err := s.Store.CreateWorkerToken(r.Context(), s.workerScope(r), currentUser(r).ID, input.Name, s.Config.WorkerPepper)
	if err != nil {
		s.workerAdminError(w, r, err)
		return
	}
	ok(w, r, out)
}
func (s *Server) workerTokenRegenerate(w http.ResponseWriter, r *http.Request) {
	if !s.workerHTTPS(r) {
		fail(w, r, 426, "HTTPS_REQUIRED", "Worker token issuance requires HTTPS")
		return
	}
	out, err := s.Store.RegenerateWorkerToken(r.Context(), s.workerScope(r), r.PathValue("tokenId"), s.Config.WorkerPepper)
	if err != nil {
		s.workerAdminError(w, r, err)
		return
	}
	ok(w, r, out)
}
func (s *Server) workerTokenRevoke(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.RevokeWorkerToken(r.Context(), s.workerScope(r), r.PathValue("tokenId"))
	if err != nil {
		s.workerAdminError(w, r, err)
		return
	}
	ok(w, r, out)
}
