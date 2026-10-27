package video

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// Submit sends at most one POST. ErrAmbiguous means a paid task may exist even
// without an ID: quarantine it for reconciliation; do not retry or change route.
// When an ID accompanies contradictory response fields, it is returned with the
// error so the parent can reconcile that exact task.
func (g *Gateway) Submit(ctx context.Context, request Request) (Submission, error) {
	if err := checkContext(ctx, "submit"); err != nil {
		return Submission{}, err
	}
	body, model, err := g.requestBody(request)
	if err != nil {
		return Submission{}, err
	}
	path := "/v1/videos/generations"
	switch g.config.Provider {
	case HFSY:
		path = "/v1/video/create"
	case Agnes:
		path = "/v1/videos"
	}
	root, err := g.doJSON(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return Submission{}, err
	}
	id := extractTaskID(root, g.config.Provider)
	submission := Submission{}
	if g.validTaskID(id) {
		submission = Submission{UpstreamTaskID: id, Provider: g.config.Provider, Model: model}
	}
	if data, ok := root["data"].([]any); ok {
		if len(data) != 1 {
			return submission, failure(Ambiguous, "submit", "unexpected_task_count")
		}
		if _, ok := data[0].(map[string]any); !ok {
			return submission, failure(Ambiguous, "submit", "invalid_task")
		}
	}
	if rejected, uncertain := envelopeFailure(root); rejected || uncertain {
		kind := Ambiguous
		if rejected && submission.UpstreamTaskID == "" {
			kind = Rejected
		}
		return submission, failure(kind, "submit", "upstream_error")
	}
	if submission.UpstreamTaskID == "" {
		return submission, failure(Ambiguous, "submit", "missing_or_invalid_task_id")
	}
	return submission, nil
}

// Poll queries only the supplied persisted task. It does not submit work or
// follow a provider/model to another route. Running progress defaults to 10.
func (g *Gateway) Poll(ctx context.Context, task Submission) (PollResult, error) {
	if err := checkContext(ctx, "poll"); err != nil {
		return PollResult{}, err
	}
	if task.Provider != g.config.Provider || !g.validTaskID(task.UpstreamTaskID) || !g.validPollModel(task.Model) {
		return PollResult{}, failure(InvalidRequest, "poll", "task_identity")
	}
	path, query := "/v1/tasks/"+task.UpstreamTaskID, url.Values{}
	switch g.config.Provider {
	case HFSY:
		path, query = "/v1/video/query", url.Values{"id": {task.UpstreamTaskID}}
	case Agnes:
		path, query = "/agnesapi", url.Values{"video_id": {task.UpstreamTaskID}}
	}
	root, err := g.doJSON(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return PollResult{}, err
	}
	if id := extractTaskID(root, g.config.Provider); id != "" && id != task.UpstreamTaskID {
		return PollResult{}, failure(ProtocolError, "poll", "task_id_mismatch")
	}
	return g.parsePoll(root, task.Model)
}

// ParseCallback normalizes APIMart root, data-object, and single-item data-array
// payloads. Authenticate the header/path secret and bound the HTTP request body
// before calling it. Neither callback authentication nor DB state changes occur.
func (g *Gateway) ParseCallback(ctx context.Context, body []byte) (Callback, error) {
	if err := checkContext(ctx, "callback"); err != nil {
		return Callback{}, err
	}
	if g.config.Provider != Gemini && g.config.Provider != Seedance {
		return Callback{}, failure(InvalidRequest, "callback", "unsupported_provider")
	}
	if int64(len(body)) > g.config.MaxResponseBytes {
		return Callback{}, failure(LimitExceeded, "callback", "response_limit")
	}
	root, err := decodeObject(body)
	if err != nil {
		return Callback{}, failure(ProtocolError, "callback", "invalid_json")
	}
	id := firstText(root, "task_id", "data.task_id", "data.0.task_id", "id", "data.id", "data.0.id")
	if !g.validTaskID(id) {
		return Callback{}, failure(ProtocolError, "callback", "missing_or_invalid_task_id")
	}
	result, err := g.parsePoll(root, "")
	if err != nil {
		return Callback{}, err
	}
	return Callback{UpstreamTaskID: id, Result: result}, nil
}

func (g *Gateway) validTaskID(id string) bool {
	return taskID.MatchString(id) && !strings.Contains(id, g.config.APIKey)
}

func (g *Gateway) validPollModel(model string) bool {
	if g.config.Provider == Agnes && model == g.config.AgnesModel && model != "" {
		return true
	}
	resolved, err := ResolveModel(g.config.Provider, model, "")
	return model != "" && err == nil && resolved == model
}
