package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
	wp "github.com/suakitsu/picpilot-platform-go/core/workerprotocol"
)

// Resume persisted local work before looking at today's provider configuration.
// A disabled or reordered provider must not hide a live lease or a ready result.
func (s *Server) resumeWorkerTask(ctx context.Context, t store.Task) (bool, error) {
	w, err := s.Store.LatestWorkerJobForTask(ctx, t.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	wait := 30
	p, err := s.Store.Provider(ctx, w.ProviderScope, w.Job.ProviderID)
	if err == nil {
		wait = p.LocalWaitSeconds
	} else if !errors.Is(err, store.ErrNotFound) {
		return true, err
	}
	w, allowed, err := s.Store.ReconcileWorkerJob(ctx, t.ID, w.Job.JobID, wait)
	if err != nil {
		return true, err
	}
	switch w.Job.Status {
	case wp.Queued, wp.Leased:
		return true, s.Store.WaitForWorker(ctx, t, w.Job.JobID, 3*time.Second)
	case wp.Expired:
		_, err = s.Store.RecordWorkerReceipt(ctx, t, w.Job.JobID)
		return true, err
	case wp.Cancelled:
		if _, err = s.Store.RecordWorkerReceipt(ctx, t, w.Job.JobID); err != nil {
			return true, err
		}
		if !w.Job.LeasedAt.IsZero() {
			return true, nil
		}
		if allowed {
			return false, nil
		}
		_, err = s.Store.CancelUnsubmittedWorkerTask(ctx, t)
		return true, err
	case wp.Failed:
		if _, err = s.Store.RecordWorkerReceipt(ctx, t, w.Job.JobID); err != nil {
			return true, err
		}
		if allowed && wp.Retryable(0, w.Job.ErrorCode) {
			return false, nil
		}
		_, err = s.Store.FailTask(ctx, t, w.Job.ErrorCode)
		return true, err
	case wp.Succeeded:
		result, err := s.workerBusinessResult(ctx, w)
		if err != nil {
			return true, err
		}
		if _, err = s.Store.RecordWorkerReceipt(ctx, t, w.Job.JobID); err != nil {
			return true, err
		}
		if p.ID != "" {
			_ = s.Store.RecordProviderOutcome(ctx, p, "", false)
		}
		_, err = s.Store.CompleteTask(ctx, t, result)
		return true, err
	default:
		return true, store.ErrConflict
	}
}

func (s *Server) workerBusinessResult(ctx context.Context, w store.WorkerJob) (json.RawMessage, error) {
	var result map[string]json.RawMessage
	if json.Unmarshal(w.Job.ResultJSON, &result) != nil {
		return nil, store.ErrConflict
	}
	if w.Job.Capability == wp.Image {
		var id string
		if json.Unmarshal(result["artifact_id"], &id) != nil {
			return nil, store.ErrConflict
		}
		a, err := s.Store.WorkerArtifactAsset(ctx, w.TaskID, id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"artifact_id": a.ID, "width": a.Width, "height": a.Height, "content_type": a.ContentType, "model": w.Job.Model, "worker_job_id": w.Job.JobID})
	}
	text, err := wp.ExtractText(result["result"])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"text": text, "payload": result["result"], "model": w.Job.Model, "worker_job_id": w.Job.JobID})
}

func (s *Server) enqueueLocalTask(ctx context.Context, t store.Task, p store.Provider, payload jobs.Payload) (bool, error) {
	in, err := s.compileWorkerInput(ctx, t, p, payload)
	if err != nil {
		var protocolErr *wp.ProtocolError
		if errors.Is(err, jobs.ErrTextProtocol) || errors.As(err, &protocolErr) {
			return false, nil
		}
		return true, err
	}
	w, _, err := s.Store.EnqueueWorkerJob(ctx, in, s.workerOptions())
	if errors.Is(err, store.ErrDisabled) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.Store.WaitForWorker(ctx, t, w.Job.JobID, 3*time.Second)
}

func (s *Server) compileWorkerInput(ctx context.Context, t store.Task, p store.Provider, payload jobs.Payload) (store.EnqueueWorkerJobInput, error) {
	in := store.EnqueueWorkerJobInput{TaskID: t.ID, ProviderScope: p.OwnerScope, ProviderID: p.ID, Capability: wp.Capability(t.Capability), Protocol: wp.Protocol(p.Protocol), Model: p.Model}
	body := map[string]any{"model": p.Model}
	if payload.Image != nil {
		input := payload.Image
		in.Protocol = wp.ImageGenerations
		if len(input.ReferenceAssetIDs) > 0 {
			in.Protocol = wp.ImageEdits
			in.InputAssetIDs = append([]string(nil), input.ReferenceAssetIDs...)
		}
		body["prompt"], body["size"], body["n"] = input.Prompt, input.Size, 1
		for key, value := range map[string]string{"quality": input.Quality, "output_format": input.OutputFormat, "background": input.Background, "moderation": input.Moderation} {
			if value != "" {
				body[key] = value
			}
		}
	} else if payload.Text != nil {
		input, err := jobs.AdaptTextInput(*payload.Text, provider.Protocol(p.Protocol))
		if err != nil {
			return in, err
		}
		for k, v := range input.Options {
			body[k] = v
		}
		body["model"], body["stream"] = p.Model, false
		if in.Protocol == wp.ChatCompletions {
			body["messages"] = input.Messages
		} else if in.Protocol == wp.Responses {
			body["input"] = input.Input
			if _, ok := body["store"]; !ok {
				body["store"] = false
			}
		} else {
			return in, jobs.ErrTextProtocol
		}
		raw, _ := json.Marshal(body)
		in.TextInputs, err = workerTextReferences(raw)
		if err != nil {
			return in, err
		}
		// The store replaces these typed HTTPS fields with owned placeholders,
		// then creates fresh signed URLs only when a Worker actually claims.
		input, err = s.materializeTextInput(ctx, t.UserID, input, true)
		if err != nil {
			return in, err
		}
		if in.Protocol == wp.ChatCompletions {
			body["messages"] = input.Messages
		} else {
			body["input"] = input.Input
		}
	} else {
		return in, wp.NewError("CAPABILITY_INVALID")
	}
	var err error
	in.Request, err = json.Marshal(body)
	if err != nil {
		return in, err
	}
	return in, wp.ValidatePayload(in.Capability, in.Protocol, in.Model, in.Request)
}

func workerTextReferences(raw json.RawMessage) ([]store.WorkerTextInput, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var root any
	if d.Decode(&root) != nil {
		return nil, store.ErrConflict
	}
	refs := []store.WorkerTextInput{}
	var walk func(any, string) error
	walk = func(value any, pointer string) error {
		switch v := value.(type) {
		case []any:
			for i, item := range v {
				if err := walk(item, pointer+"/"+strconv.Itoa(i)); err != nil {
					return err
				}
			}
		case map[string]any:
			typ, _ := v["type"].(string)
			uri, path := "", ""
			if typ == "input_image" {
				uri, _ = v["image_url"].(string)
				path = pointer + "/image_url"
			}
			if typ == "image_url" {
				image, ok := v["image_url"].(map[string]any)
				if !ok {
					return store.ErrConflict
				}
				uri, _ = image["url"].(string)
				path = pointer + "/image_url/url"
			}
			if path != "" {
				id, ok := strings.CutPrefix(uri, assetURIPrefix)
				if !ok || id == "" {
					return store.ErrConflict
				}
				refs = append(refs, store.WorkerTextInput{AssetID: id, JSONPointer: path})
				return nil
			}
			for _, key := range []string{"messages", "input", "content"} {
				if item, ok := v[key]; ok {
					if err := walk(item, pointer+"/"+key); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return refs, nil
}
