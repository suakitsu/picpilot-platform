package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/security"
	"github.com/suakitsu/picpilot-platform-go/core/storage"
	"github.com/suakitsu/picpilot-platform-go/core/store"
	"github.com/suakitsu/picpilot-platform-go/core/taskrunner"
	"github.com/suakitsu/picpilot-platform-go/core/video"
)

const videoLeaseTTL = 2 * time.Minute

// RunVideoTasks is a service-lifetime hook, never a request handler. The shared
// runner owns queued submissions and crash recovery; separate fenced poll claims
// can only read already-known upstream IDs. Shutdown joins every worker/download.
func (s *Server) RunVideoTasks(ctx context.Context) error {
	client, err := provider.NewHTTPClient(provider.Options{
		AllowLocalDevelopment: s.Config.AllowPrivateProviders,
		Timeout:               5 * time.Minute,
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxRequestBytes:       4 << 20,
		MaxResponseBytes:      storage.MaxMediaBytes,
	})
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	return s.runVideoTasks(ctx, client)
}

func (s *Server) runVideoTasks(parent context.Context, client *http.Client) error {
	if client == nil || client.Transport == nil || s.Store == nil || s.Files == nil {
		return errors.New("video runtime dependencies unavailable")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	runner := taskrunner.Runner{Store: s.Store, Concurrency: 2, Capabilities: []string{"video"}, LeaseTTL: videoLeaseTTL,
		Process: func(ctx context.Context, t store.Task) { s.executeVideoSubmission(ctx, t, client) },
		OnError: func(code string) { s.videoRuntimeError(code) },
	}
	var wg sync.WaitGroup
	owner := security.NewID("video-poller")
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				claim, err := s.Store.ClaimVideoPoll(ctx, owner, videoLeaseTTL)
				if err != nil {
					if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
						s.videoRuntimeError("video_poll_claim_failed")
					}
					if !videoWait(ctx, time.Second) {
						return
					}
					continue
				}
				s.executeVideoPollLease(ctx, claim, client)
			}
		}()
	}
	err := runner.Run(ctx)
	cancel()
	wg.Wait()
	return err
}

func videoWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Server) videoRuntimeError(code string) {
	s.Logger.Error("video runtime operation failed", "code", code)
}

func videoRecordContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 8*time.Second)
}

func (s *Server) videoGateway(p store.Provider, family video.Provider, client *http.Client) (*video.Gateway, error) {
	if p.ID == "" || p.OwnerScope == "" || p.Capability != "video" || p.ExecutionMode != "http" {
		return nil, store.ErrConflict
	}
	secret, err := s.Vault.Decrypt(p.OwnerScope+"/"+p.ID, p.SecretCiphertext)
	if err != nil {
		return nil, errors.New("video credential unavailable")
	}
	conf := video.Config{Provider: family, APIKey: secret, BaseURL: p.BaseURL, Client: client,
		AllowHTTP:      s.Config.AllowPrivateProviders,
		MaxResultBytes: storage.MaxMediaBytes,
	}
	if p.TimeoutSeconds > 0 {
		conf.SubmitTimeout = min(time.Duration(p.TimeoutSeconds)*time.Second, 45*time.Second)
		conf.PollTimeout = min(time.Duration(p.TimeoutSeconds)*time.Second, 30*time.Second)
	}
	if family == video.Agnes {
		conf.AgnesModel = p.Model
	} else if family == video.Gemini || family == video.Seedance {
		// Compiled references are already absolute. A local personal UI origin
		// must not invalidate an otherwise HTTPS provider configuration. Polling
		// works without advertising a callback the upstream cannot reach.
		public, err := url.Parse(s.Config.PublicBaseURL)
		if err == nil && (public.Scheme == "https" || s.Config.AllowPrivateProviders) {
			conf.WebhookURL = s.VideoWebhookURL()
		}
	}
	return video.NewGateway(conf)
}

func (s *Server) executeVideoSubmission(ctx context.Context, task store.Task, client *http.Client) {
	d, err := decodeVideoTask(task)
	if err != nil {
		s.failVideoBeforeSubmission(task, "", "INVALID_VIDEO_PAYLOAD")
		return
	}
	// A zero-cost demo task must never become a paid job after a server restart.
	if s.Config.DemoMode || d.MockMode {
		s.failVideoBeforeSubmission(task, "", "VIDEO_DEMO_EXECUTION_UNAVAILABLE")
		return
	}
	c, err := s.videoConfig(ctx)
	if err != nil {
		s.videoRuntimeError("video_configuration_unavailable")
		return
	}
	if !c.Enabled {
		_ = s.Store.ReleaseUnsubmittedTask(ctx, task, 30*time.Second)
		return
	}
	_, selected, err := s.CompileVideoTask(ctx, task)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.failVideoBeforeSubmission(task, "", "VIDEO_INPUT_OR_ROUTE_UNAVAILABLE")
		} else {
			s.videoRuntimeError("video_compile_unavailable")
		}
		return
	}
	a, err := s.Store.ReserveAttempt(ctx, task, selected.OwnerScope, selected.ID)
	if errors.Is(err, store.ErrProviderBusy) || errors.Is(err, store.ErrCircuitOpen) {
		_ = s.Store.ReleaseUnsubmittedTask(ctx, task, 10*time.Second)
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.failVideoBeforeSubmission(task, "", "VIDEO_PROVIDER_UNAVAILABLE")
		} else if !errors.Is(err, store.ErrLeaseInvalid) && ctx.Err() == nil {
			s.videoRuntimeError("video_reservation_failed")
		}
		return
	}
	input, err := s.CompileVideoAttempt(ctx, task, a)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.failVideoBeforeSubmission(task, a.ID, "VIDEO_INPUT_UNAVAILABLE")
		}
		return
	}
	g, err := s.videoGateway(a.Provider, d.Provider, client)
	if err != nil {
		s.failVideoBeforeSubmission(task, a.ID, "VIDEO_PROVIDER_CONFIG_INVALID")
		return
	}
	if err := s.Store.BeginVideoSubmission(ctx, task, a); err != nil {
		if ctx.Err() == nil && !errors.Is(err, store.ErrLeaseInvalid) {
			s.videoRuntimeError("video_submission_boundary_failed")
		}
		return
	}
	submission, submitErr := g.Submit(ctx, input)
	recordCtx, cancel := videoRecordContext()
	defer cancel()
	if submission.UpstreamTaskID != "" {
		// Contradictory envelopes can include a valid ID with ErrAmbiguous. Keep
		// that ID and reconcile it; neither rejection nor a second POST is safe.
		if _, err := s.Store.RecordVideoSubmission(recordCtx, task, a.ID, submission, time.Duration(c.PollSeconds)*time.Second); err != nil {
			s.videoRuntimeError("video_acknowledgement_persistence_failed")
			_ = s.Store.MarkUncertain(recordCtx, task, a.ID, "VIDEO_ACKNOWLEDGEMENT_UNCONFIRMED")
			return
		}
		_ = s.Store.RecordProviderOutcome(recordCtx, a.Provider, "", false)
		return
	}
	code, authFailure := videoOutcomeCode(submitErr, "VIDEO_SUBMISSION_UNKNOWN")
	if errors.Is(submitErr, video.ErrRejected) || errors.Is(submitErr, video.ErrInvalidRequest) {
		if !errors.Is(submitErr, video.ErrInvalidRequest) {
			_ = s.Store.RecordProviderOutcome(recordCtx, a.Provider, code, authFailure)
		}
		if err := s.Store.RejectAttempt(recordCtx, task, a.ID, code, false); err != nil {
			s.videoRuntimeError("video_rejection_persistence_failed")
			return
		}
		if _, err := s.Store.FailTask(recordCtx, task, code); err != nil {
			s.videoRuntimeError("video_failure_persistence_failed")
		}
		return
	}
	_ = s.Store.RecordProviderOutcome(recordCtx, a.Provider, code, authFailure)
	if err := s.Store.MarkUncertain(recordCtx, task, a.ID, "VIDEO_SUBMISSION_UNKNOWN"); err != nil {
		s.videoRuntimeError("video_uncertain_persistence_failed")
	}
}

func (s *Server) failVideoBeforeSubmission(task store.Task, attemptID, code string) {
	ctx, cancel := videoRecordContext()
	defer cancel()
	if attemptID != "" {
		if err := s.Store.RejectAttempt(ctx, task, attemptID, code, false); err != nil {
			return
		}
	}
	if _, err := s.Store.FailTask(ctx, task, code); err != nil && !errors.Is(err, store.ErrLeaseInvalid) {
		s.videoRuntimeError("video_preflight_settlement_failed")
	}
}

func videoOutcomeCode(err error, fallback string) (string, bool) {
	var e *video.Error
	if errors.As(err, &e) {
		switch e.HTTPStatus {
		case 401, 403:
			return "VIDEO_PROVIDER_AUTHENTICATION", true
		case 429:
			return "VIDEO_PROVIDER_RATE_LIMITED", false
		}
		if errors.Is(err, video.ErrRejected) {
			return "VIDEO_PROVIDER_REJECTED", false
		}
		if errors.Is(err, video.ErrInvalidRequest) {
			return "VIDEO_INVALID_REQUEST", false
		}
	}
	return fallback, false
}

func (s *Server) executeVideoPollLease(parent context.Context, claim store.VideoPollClaim, client *http.Client) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(videoLeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Store.RenewTask(ctx, claim.Task, videoLeaseTTL); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		if recover() != nil {
			s.videoRuntimeError("video_poll_processor_panic")
		}
		cancel()
		<-done
	}()
	s.executeVideoPoll(ctx, claim, client)
}

func (s *Server) executeVideoPoll(ctx context.Context, claim store.VideoPollClaim, client *http.Client) {
	lease := claim.Task
	delay := 5 * time.Second
	if c, err := s.videoConfig(ctx); err == nil {
		delay = time.Duration(max(2, min(c.PollSeconds, 60))) * time.Second
	}
	// Release never requeues submission. Even a download/storage error leaves
	// completed evidence staged, with backoff rather than a tight fetch loop.
	defer func() {
		recordCtx, cancel := videoRecordContext()
		defer cancel()
		if err := s.Store.ReleaseVideoPoll(recordCtx, lease, delay); err != nil && !errors.Is(err, store.ErrLeaseInvalid) {
			s.videoRuntimeError("video_poll_release_failed")
		}
	}()
	if s.Config.DemoMode {
		delay = time.Minute
		return
	}
	if claim.Provider.ID == "" {
		delay = time.Hour
		s.videoRuntimeError("video_poll_snapshot_missing")
		return
	}
	g, err := s.videoGateway(claim.Provider, claim.Submission.Provider, client)
	if err != nil {
		delay = 5 * time.Minute
		s.videoRuntimeError("video_poll_credential_unavailable")
		return
	}
	if claim.Pending == nil {
		result, err := g.Poll(ctx, claim.Submission)
		if err != nil {
			delay = time.Minute
			code, authFailure := videoOutcomeCode(err, "VIDEO_POLL_UNAVAILABLE")
			if authFailure {
				delay = 5 * time.Minute
			}
			var pe *video.Error
			if errors.As(err, &pe) {
				delay = min(time.Hour, max(delay, pe.RetryAfter))
			}
			recordCtx, cancel := videoRecordContext()
			if ctx.Err() == nil {
				_ = s.Store.RecordProviderOutcome(recordCtx, claim.Provider, code, authFailure)
			}
			cancel()
			return
		}
		recordCtx, cancel := videoRecordContext()
		_ = s.Store.RecordProviderOutcome(recordCtx, claim.Provider, "", false)
		_, err = s.Store.ApplyVideoPoll(recordCtx, claim.Task, result, delay)
		cancel()
		if err != nil || result.Status == video.Failed {
			return
		}
	}
	// Re-read after the poll: an authenticated callback can win the terminal race.
	claim, err = s.Store.VideoPollState(ctx, claim.Task)
	if err != nil || claim.Pending == nil || claim.Pending.Status != video.Completed {
		return
	}
	asset, err := s.Store.VideoResultAsset(ctx, claim.Task)
	if err == nil {
		file, openErr := s.Files.Open(asset.StorageKey)
		if openErr == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr != nil || info.Size() != asset.FileSize {
				err = storage.ErrInvalidMedia
			}
		} else {
			err = openErr
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		delay = time.Minute
		return
	}
	if err != nil {
		asset, err = s.downloadVideoAsset(ctx, claim.Task, g, claim.Pending.ResultURL)
		if err != nil {
			delay = time.Minute
			s.videoRuntimeError("video_result_storage_unavailable")
			return
		}
	}
	recordCtx, cancel := videoRecordContext()
	defer cancel()
	if _, err := s.Store.CompleteVideoTask(recordCtx, claim.Task, asset.ID); err != nil {
		delay = time.Minute
		s.videoRuntimeError("video_completion_persistence_failed")
		// Retain the owned asset for the next fenced poll to reuse. Do not fetch
		// again or remove a file another worker may already be completing.
	}
}

func (s *Server) downloadVideoAsset(ctx context.Context, task store.Task, g *video.Gateway, resultURL string) (store.Asset, error) {
	ctx, cancel := context.WithCancel(ctx)
	reader, writer := io.Pipe()
	done := make(chan struct{})
	var fetched video.FetchResult
	var fetchErr error
	go func() {
		defer close(done)
		fetched, fetchErr = g.FetchResult(ctx, resultURL, writer)
		_ = writer.CloseWithError(fetchErr)
	}()
	defer func() {
		_ = reader.Close()
		cancel()
		<-done
	}()
	var head [512]byte
	n, err := io.ReadFull(reader, head[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return store.Asset{}, storage.ErrInvalidMedia
	}
	mime := http.DetectContentType(head[:n])
	ext := "mp4"
	if mime == "video/webm" {
		ext = "webm"
	} else if mime != "video/mp4" {
		return store.Asset{}, storage.ErrInvalidMedia
	}
	asset, err := s.saveMedia(ctx, task.UserID, task.ProjectID, task.ID, "generated-video", task.ID+"."+ext, mime, io.MultiReader(bytes.NewReader(head[:n]), reader))
	if err != nil {
		return store.Asset{}, err
	}
	<-done
	if fetchErr != nil || fetched.Bytes != asset.FileSize {
		return store.Asset{}, storage.ErrInvalidMedia
	}
	return asset, nil
}
