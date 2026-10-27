package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/nova"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

// This is only a transport adapter. Provider selection, vision normalization,
// task admission, execution and settlement reuse the creative/business helpers.
func novaTextInput(in nova.TextRequest) (jobs.TextInput, error) {
	canvas, err := in.CanvasBody()
	if err != nil {
		return jobs.TextInput{}, err
	}
	out := jobs.TextInput{Model: in.Model, Protocol: provider.Protocol(canvas["protocol"].(string)), Options: map[string]json.RawMessage{}}
	allowed := " temperature top_p max_tokens max_completion_tokens frequency_penalty presence_penalty response_format stop seed reasoning_effort thinking tools tool_choice parallel_tool_calls "
	if out.Protocol == provider.Responses {
		allowed = " instructions temperature top_p max_output_tokens reasoning text tools tool_choice metadata store previous_response_id parallel_tool_calls truncation include "
	}
	for k, v := range canvas["request_body"].(map[string]json.RawMessage) {
		switch k {
		case "messages":
			decoder := json.NewDecoder(bytes.NewReader(v))
			decoder.DisallowUnknownFields()
			if out.Protocol != provider.ChatCompletions || decoder.Decode(&out.Messages) != nil {
				return out, nova.ErrInvalid
			}
		case "input":
			if out.Protocol != provider.Responses {
				return out, nova.ErrInvalid
			}
			out.Input = v
		case "model":
			var model string
			if json.Unmarshal(v, &model) != nil || in.Model != "" && model != in.Model {
				return out, nova.ErrInvalid
			}
			out.Model = model
		default:
			if !strings.Contains(allowed, " "+k+" ") || len(v) > 256<<10 || k == "tools" && len(v) > 64000 {
				return out, nova.ErrInvalid
			}
			out.Options[k] = v
		}
	}
	if len(out.Messages) > 20 || (len(out.Messages) == 0) == (len(out.Input) == 0) || (jobs.Payload{Version: 1, Text: &out}).Validate("text") != nil {
		return out, nova.ErrInvalid
	}
	for _, m := range out.Messages {
		if !oneOf(m.Role, "system", "developer", "user", "assistant", "tool") || m.Role == "tool" && m.ToolCallID == "" {
			return out, nova.ErrInvalid
		}
	}
	return out, nil
}

func (s *Server) novaText(w http.ResponseWriter, r *http.Request) {
	key, err := novaKey(r)
	if err != nil {
		s.novaError(w, err)
		return
	}
	wait, err := novaWait(r)
	if err != nil {
		s.novaError(w, err)
		return
	}
	var in nova.TextRequest
	if !novaDecode(w, r, nova.MaxTextBody, &in) {
		return
	}
	canvas, err := in.CanvasBody()
	if err != nil {
		s.novaError(w, err)
		return
	}
	identity := creative.JSON(canvas)
	ctx, user := r.Context(), currentUser(r).ID
	run, found, err := s.Store.NovaReplay(ctx, user, "text", key, identity)
	if err != nil {
		s.novaError(w, err)
		return
	}
	if !found {
		input, err := novaTextInput(in)
		if err != nil {
			s.novaError(w, err)
			return
		}
		if err = s.requireWorkflowProvider(ctx, user, "text"); err != nil {
			s.novaError(w, err)
			return
		}
		if err = s.workflowTextSelection(ctx, user, input.Model, string(input.Protocol)); err != nil {
			s.novaError(w, err)
			return
		}
		if err = s.screenUserInput(ctx, user, "", "nova_text", novaBusinessKey("text", key, 0), directUserText(jobs.Payload{Version: 1, Text: &input})); err != nil {
			s.novaBusinessError(w, r, err)
			return
		}
		input, err = s.prepareTextInput(ctx, user, input)
		if err != nil {
			novaFail(w, 400, "INVALID_TEXT_REFERENCE", "Invalid, unsafe or oversized text image reference")
			return
		}
		if creative.ValidateFields(input) != nil {
			s.novaError(w, nova.ErrInvalid)
			return
		}
		run, _, err = s.Store.CreateNovaRun(ctx, user, "text", string(input.Protocol), key, identity, 1, func(tx pgx.Tx, _ string) ([]store.Task, error) {
			t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, Capability: "text", Kind: "canvas_text", Title: "Nova text", Request: creative.JSON(jobs.Payload{Version: 1, Text: &input}), RequestIdentity: identity, IdempotencyKey: novaBusinessKey("text", key, 0)})
			return []store.Task{t}, err
		})
		if err != nil {
			s.novaError(w, err)
			return
		}
	}
	s.novaDeliverText(w, r, run, in.Stream, wait)
}

func (s *Server) novaTextResponse(w http.ResponseWriter, r *http.Request) {
	run, err := s.Store.NovaRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.novaError(w, err)
		return
	}
	if run.Kind != "text" {
		s.novaError(w, store.ErrNotFound)
		return
	}
	stream := r.URL.Query().Get("stream")
	if !oneOf(stream, "", "false", "true") {
		s.novaError(w, nova.ErrInvalid)
		return
	}
	s.novaDeliverText(w, r, run, stream == "true", 0)
}

func (s *Server) novaDeliverText(w http.ResponseWriter, r *http.Request, run store.NovaRun, stream bool, wait time.Duration) {
	w.Header().Set("Location", "/api/nova/tasks/"+run.ID)
	w.Header().Set("X-Nova-Task-Id", run.ID)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		view, _, err := s.novaRunView(r.Context(), run)
		if err != nil {
			s.novaError(w, err)
			return
		}
		switch view.Status {
		case "completed":
			if view.Result == nil || len(view.Result.Payload) == 0 {
				novaFail(w, 502, "INVALID_TEXT_RESULT", "The business task has no structured text result")
				return
			}
			if !stream {
				writeJSON(w, 200, view.Result.Payload)
				return
			}
			events, err := nova.Events(run.Mode, view.Result.Payload)
			if err != nil {
				novaFail(w, 502, "INVALID_TEXT_RESULT", "The settled text payload is not stream-compatible")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(200)
			_, _ = w.Write(events)
			if flush, ok := w.(http.Flusher); ok {
				flush.Flush()
			}
			return
		case "failed":
			novaFail(w, 502, "TEXT_TASK_FAILED", "The text task failed; retrying the same key will not create another task")
			return
		case "cancelled":
			novaFail(w, 409, "TASK_CANCELLED", "The text task was cancelled")
			return
		}
		if wait == 0 {
			w.Header().Set("Retry-After", "3")
			writeJSON(w, 202, map[string]any{"taskId": run.ID, "status": view.Status, "pollUrl": "/api/nova/tasks/" + run.ID, "responseUrl": "/api/nova/tasks/" + run.ID + "/response"})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			wait = 0
		case <-ticker.C:
		}
	}
}
