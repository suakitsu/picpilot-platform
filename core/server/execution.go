package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
	"github.com/suakitsu/picpilot-platform/core/taskrunner"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func (s *Server) processHTTPModelTask(ctx context.Context, t store.Task, gateway *provider.Gateway) {
	var payload jobs.Payload
	if json.Unmarshal(t.Request, &payload) != nil || payload.Validate(t.Capability) != nil {
		_, _ = s.Store.FailTask(ctx, t, "INVALID_TASK_PAYLOAD")
		return
	}
	if handled, err := s.resumeWorkerTask(ctx, t); handled || err != nil {
		if err != nil && ctx.Err() == nil {
			s.Logger.Error("worker business reconciliation failed", "task_id", t.ID, "sqlstate", store.SQLState(err))
		}
		return
	}
	if s.Config.DemoMode {
		s.processMockTask(ctx, t, payload)
		return
	}
	scope := "platform"
	if s.Config.Edition == config.Personal {
		scope = t.UserID
	}
	providers, err := s.Store.Providers(ctx, scope)
	if err != nil {
		return
	}
	if payload.Image != nil {
		providers = orderedImageProviders(providers, *payload.Image)
	}
	failed, err := s.Store.FailedProviders(ctx, t.ID)
	if err != nil {
		return
	}
	busy := false
	cooling := false
	attempted := false
	for _, p := range providers {
		if !p.Enabled || p.Capability != t.Capability || failed[p.ID] {
			continue
		}
		if payload.Text != nil && (payload.Text.Model != "" && payload.Text.Model != p.Model || payload.Text.Protocol != "" && string(payload.Text.Protocol) != p.Protocol) {
			continue
		}
		if p.ExecutionMode == "pull_worker" {
			handled, err := s.enqueueLocalTask(ctx, t, p, payload)
			if err != nil {
				if ctx.Err() == nil {
					s.Logger.Error("worker dispatch failed", "task_id", t.ID, "sqlstate", store.SQLState(err))
				}
				return
			}
			if handled {
				return
			}
			continue
		}
		a, err := s.Store.ReserveAttempt(ctx, t, scope, p.ID)
		if errors.Is(err, store.ErrCircuitOpen) {
			cooling = true
			continue
		}
		if errors.Is(err, store.ErrProviderBusy) {
			busy = true
			continue
		}
		if err != nil {
			return
		}
		attempted = true
		p = a.Provider
		if payload.Image != nil && !imageProviderMatches(p, *payload.Image) {
			_ = s.Store.RejectAttempt(ctx, t, a.ID, "IMAGE_ROUTING_CHANGED", true)
			continue
		}
		if payload.Text != nil && (payload.Text.Model != "" && payload.Text.Model != p.Model || payload.Text.Protocol != "" && string(payload.Text.Protocol) != p.Protocol) {
			_ = s.Store.RejectAttempt(ctx, t, a.ID, "TEXT_ROUTING_CHANGED", true)
			continue
		}
		secret, err := s.Vault.Decrypt(scope+"/"+p.ID, p.SecretCiphertext)
		if err != nil {
			_ = s.Store.RejectAttempt(ctx, t, a.ID, "PROVIDER_CREDENTIAL_UNAVAILABLE", false)
			continue
		}
		pr := provider.Provider{BaseURL: p.BaseURL, APIKey: provider.Secret(secret), Model: p.Model, ImageProfile: provider.ImageProfile(p.Profile), TextProtocol: provider.Protocol(p.Protocol), Timeout: time.Duration(p.TimeoutSeconds) * time.Second}
		var refs []provider.Reference
		var textInput jobs.TextInput
		if payload.Image != nil {
			refs, err = s.loadTaskReferences(ctx, t, payload.Image.ReferenceAssetIDs)
			if err != nil {
				_ = s.Store.RejectAttempt(ctx, t, a.ID, "REFERENCE_UNAVAILABLE", false)
				_, _ = s.Store.FailTask(ctx, t, "REFERENCE_UNAVAILABLE")
				return
			}
		}
		if payload.Text != nil {
			textInput, err = jobs.AdaptTextInput(*payload.Text, provider.Protocol(p.Protocol))
			if err != nil {
				_ = s.Store.RejectAttempt(ctx, t, a.ID, "TEXT_PROTOCOL_UNSUPPORTED", true)
				continue
			}
			textInput, err = s.materializeTextInput(ctx, t.UserID, textInput, false)
			if err != nil {
				_ = s.Store.RejectAttempt(ctx, t, a.ID, "REFERENCE_UNAVAILABLE", false)
				_, _ = s.Store.FailTask(ctx, t, "REFERENCE_UNAVAILABLE")
				return
			}
		}
		if err = s.Store.BeginSubmission(ctx, t, a.ID); err != nil {
			return
		}
		var result json.RawMessage
		if payload.Image != nil {
			input := payload.Image
			var output provider.ImageResponse
			output, err = gateway.GenerateImage(ctx, pr, provider.ImageRequest{Prompt: input.Prompt, Size: input.Size, Quality: input.Quality, OutputFormat: input.OutputFormat, Background: input.Background, Moderation: input.Moderation, References: refs})
			if err == nil {
				var asset store.Asset
				asset, err = s.saveImage(ctx, t.UserID, t.ProjectID, t.ID, "generated", t.ID+".png", bytes.NewReader(output.Image.Bytes))
				if err == nil {
					result, _ = json.Marshal(map[string]any{"artifact_id": asset.ID, "width": asset.Width, "height": asset.Height, "content_type": asset.ContentType, "model": output.Model, "references_used": output.ReferencesUsed, "references_truncated": output.ReferencesTruncated})
				}
			}
		} else if payload.Text != nil {
			var output provider.TextResponse
			output, err = gateway.GenerateText(ctx, pr, provider.TextRequest{Messages: textInput.Messages, Input: textInput.Input, Options: textInput.Options})
			if err == nil {
				result, _ = json.Marshal(map[string]any{"text": output.Text, "payload": output.Payload, "model": p.Model})
				if len(result) > 256<<10 {
					err = errors.New("text result limit exceeded")
				}
			}
		} else {
			err = &provider.Error{Kind: provider.Unsupported, Submission: provider.NotSubmitted}
		}
		if err == nil {
			_ = s.Store.RecordProviderOutcome(ctx, p, "", false)
			if _, err = s.Store.CompleteTask(ctx, t, result); err != nil {
				s.Logger.Error("task result persistence failed", "task_id", t.ID, "sqlstate", store.SQLState(err))
			}
			return
		}
		var pe *provider.Error
		if errors.As(err, &pe) && pe.Definitive() {
			code := "PROVIDER_" + strings.ToUpper(string(pe.Kind))
			if pe.Kind != provider.InvalidRequest && pe.Kind != provider.Unsupported {
				_ = s.Store.RecordProviderOutcome(ctx, p, code, pe.Kind == provider.Authentication)
			}
			if err = s.Store.RejectAttempt(ctx, t, a.ID, code, true); err != nil {
				return
			}
			if pe.Kind == provider.InvalidRequest {
				_, _ = s.Store.FailTask(ctx, t, code)
				return
			}
			continue
		}
		// A model may have completed even when downloading or persisting its
		// output failed. Preserve the uncertain state instead of submitting again.
		recordCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = s.Store.RecordProviderOutcome(recordCtx, p, "SUBMISSION_OUTCOME_UNKNOWN", false)
		_ = s.Store.MarkUncertain(recordCtx, t, a.ID, "SUBMISSION_OUTCOME_UNKNOWN")
		cancel()
		return
	}
	if busy {
		_ = s.Store.ReleaseUnsubmittedTask(ctx, t, 3*time.Second)
		return
	}
	code := "NO_PROVIDER_CONFIGURED"
	if cooling {
		code = "PROVIDERS_COOLING_DOWN"
	}
	if attempted || len(failed) > 0 {
		code = "ALL_PROVIDERS_REJECTED"
	}
	_, _ = s.Store.FailTask(ctx, t, code)
}

func (s *Server) RunTasks(ctx context.Context) error {
	gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: s.Config.AllowPrivateProviders, MaxImageBytes: storage.MaxImageBytes, MaxPixels: storage.MaxImagePixels})
	if err != nil {
		return err
	}
	defer gateway.CloseIdleConnections()
	runner := taskrunner.Runner{Store: s.Store, Concurrency: 6, Capabilities: []string{"image", "text"}, Process: func(ctx context.Context, t store.Task) { s.processHTTPModelTask(ctx, t, gateway) }, OnError: func(code string) { s.Logger.Error("background task error", "code", code) }}
	return runner.Run(ctx)
}

func (s *Server) loadTaskReferences(ctx context.Context, t store.Task, ids []string) ([]provider.Reference, error) {
	if len(ids) > 10 {
		return nil, store.ErrConflict
	}
	refs := make([]provider.Reference, 0, len(ids))
	var total int64
	for _, id := range ids {
		a, err := s.Store.OwnedAsset(ctx, t.UserID, id)
		if err != nil {
			return nil, err
		}
		total += a.FileSize
		if total > 28<<20 {
			return nil, storage.ErrImageTooLarge
		}
		data, err := s.Files.Read(a.StorageKey, storage.MaxImageBytes)
		if err != nil {
			return nil, err
		}
		refs = append(refs, provider.Reference{Data: data})
	}
	return refs, nil
}

func (s *Server) processMockTask(ctx context.Context, t store.Task, p jobs.Payload) {
	var result json.RawMessage
	if p.Image != nil {
		parts := strings.Split(p.Image.Size, "x")
		if len(parts) != 2 {
			_, _ = s.Store.FailTask(ctx, t, "INVALID_IMAGE_SIZE")
			return
		}
		w, _ := strconv.Atoi(parts[0])
		h, _ := strconv.Atoi(parts[1])
		if w < 1 || h < 1 || w > 4096 || h > 4096 {
			_, _ = s.Store.FailTask(ctx, t, "INVALID_IMAGE_SIZE")
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 235, G: 240, B: 243, A: 255}), image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(0, 0, w, min(h, 40)), image.NewUniform(color.RGBA{R: 18, G: 127, B: 115, A: 255}), image.Point{}, draw.Src)
		d := font.Drawer{Dst: img, Src: image.White, Face: basicfont.Face7x13, Dot: fixed.P(12, min(h-1, 25))}
		d.DrawString("PICPILOT MOCK - NO MODEL WAS CALLED")
		var data bytes.Buffer
		if png.Encode(&data, img) != nil {
			return
		}
		a, err := s.saveImage(ctx, t.UserID, t.ProjectID, t.ID, "generated", t.ID+".png", &data)
		if err != nil {
			return
		}
		result, _ = json.Marshal(map[string]any{"artifact_id": a.ID, "width": a.Width, "height": a.Height, "mock": true})
	} else if p.Text != nil {
		result = json.RawMessage(`{"text":"[MOCK] PicPilot task completed without calling a model.","mock":true}`)
	} else {
		_, _ = s.Store.FailTask(ctx, t, "UNSUPPORTED_MOCK_CAPABILITY")
		return
	}
	_, _ = s.Store.CompleteTask(ctx, t, result)
}
