package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
)

type creativeUpload struct {
	data []byte
	name string
}

func readCreativeInput(w http.ResponseWriter, r *http.Request, retry bool) (creative.ImageRequest, []creativeUpload, json.RawMessage, error) {
	var in creative.ImageRequest
	uploads := []creativeUpload{}
	var raw []byte
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
		mr, err := r.MultipartReader()
		if err != nil {
			return in, nil, nil, invalidWorkflow()
		}
		fields := map[string]any{}
		var total int
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return in, nil, nil, invalidWorkflow()
			}
			field := part.FormName()
			if part.FileName() != "" {
				if field != "references" && field != "reference" {
					part.Close()
					return in, nil, nil, invalidWorkflow()
				}
				data, err := io.ReadAll(io.LimitReader(part, storage.MaxImageBytes+1))
				part.Close()
				total += len(data)
				if err != nil || int64(len(data)) > storage.MaxImageBytes || total > 28<<20 || len(uploads) >= 10 {
					return in, nil, nil, invalidWorkflow()
				}
				uploads = append(uploads, creativeUpload{data, part.FileName()})
			} else {
				data, err := io.ReadAll(io.LimitReader(part, creative.MaxField*4+1))
				part.Close()
				if err != nil || len(data) > creative.MaxField*4 {
					return in, nil, nil, invalidWorkflow()
				}
				if field == "referenceUrls" || field == "reference_urls" {
					old, _ := fields[field].([]string)
					fields[field] = append(old, string(data))
				} else {
					if _, exists := fields[field]; exists {
						return in, nil, nil, invalidWorkflow()
					}
					fields[field] = string(data)
				}
			}
		}
		raw = creative.JSON(fields)
	} else {
		if r.Body == nil || r.ContentLength == 0 {
			raw = []byte(`{}`)
		} else {
			if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				return in, nil, nil, invalidWorkflow()
			}
			var err error
			raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err != nil {
				return in, nil, nil, invalidWorkflow()
			}
		}
	}
	if retry {
		var value struct {
			ReferenceURL  string `json:"reference_url"`
			ReferenceText string `json:"reference_text"`
		}
		if creative.DecodeAliases(raw, &value) != nil {
			return in, nil, nil, invalidWorkflow()
		}
		in.ReferenceURL = value.ReferenceURL
		in.Prompt = value.ReferenceText
	} else if creative.DecodeAliases(raw, &in) != nil {
		return in, nil, nil, invalidWorkflow()
	}
	if creative.ValidateFields(in) != nil {
		return in, nil, nil, invalidWorkflow()
	}
	if err := validateWorkflowImageModels(in.Model, in.FallbackModel); err != nil {
		return in, nil, nil, err
	}
	signatures := []string{}
	for _, upload := range uploads {
		sum := sha256.Sum256(upload.data)
		signatures = append(signatures, hex.EncodeToString(sum[:]))
	}
	return in, uploads, creative.JSON(map[string]any{"input": in, "uploads": signatures}), nil
}

func (s *Server) resolveCreativeReference(ctx context.Context, user, uri string) (store.Asset, error) {
	id := ""
	if strings.HasPrefix(uri, "asset_") {
		id = uri
	} else if v, ok := strings.CutPrefix(uri, "picpilot-asset:"); ok {
		id = v
	} else if strings.HasPrefix(uri, "/api/files/assets/") {
		u, err := url.Parse(uri)
		if err != nil {
			return store.Asset{}, invalidWorkflow()
		}
		id = strings.TrimPrefix(u.Path, "/api/files/assets/")
	} else {
		id = s.ownedAssetURLID(uri)
	}
	if id != "" {
		if strings.ContainsAny(id, "/\\?#") {
			return store.Asset{}, invalidWorkflow()
		}
		return s.Store.OwnedAsset(ctx, user, id)
	}
	if strings.TrimSpace(uri) == "" {
		return store.Asset{}, invalidWorkflow()
	}
	input := jobs.TextInput{Messages: []provider.Message{{Role: "user", Content: creative.JSON([]any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": uri}}})}}}
	prepared, err := s.prepareTextInput(ctx, user, input)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Asset{}, err
		}
		return store.Asset{}, &workflowError{400, "REFERENCE_UNAVAILABLE", "Reference is unavailable, unsafe or exceeds image limits"}
	}
	var parts []struct {
		Image struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(prepared.Messages[0].Content, &parts) != nil || len(parts) != 1 {
		return store.Asset{}, invalidWorkflow()
	}
	return s.Store.OwnedAsset(ctx, user, strings.TrimPrefix(parts[0].Image.URL, "picpilot-asset:"))
}

func (s *Server) resolveCreativeInputs(ctx context.Context, user string, in creative.ImageRequest, uploads []creativeUpload) ([]string, error) {
	if creative.IgnoreReferences(in) {
		return []string{}, nil
	}
	if len(in.ReferenceAssetIDs)+len(in.ReferenceURLs)+len(uploads) > 10 {
		return nil, invalidWorkflow()
	}
	ids := []string{}
	seen := map[string]bool{}
	var total int64
	add := func(a store.Asset) error {
		if seen[a.ID] {
			return nil
		}
		total += a.FileSize
		if total > 28<<20 || len(ids) >= 10 {
			return invalidWorkflow()
		}
		seen[a.ID] = true
		ids = append(ids, a.ID)
		return nil
	}
	// Explicit history selection is confined to this chat and keeps composer order.
	for _, uri := range in.ReferenceURLs {
		if in.ChatID == "" {
			return nil, store.ErrNotFound
		}
		id := strings.TrimPrefix(uri, "picpilot-asset:")
		if !strings.HasPrefix(id, "asset_") {
			id = s.ownedAssetURLID(uri)
			if strings.HasPrefix(uri, "/api/files/assets/") {
				u, err := url.Parse(uri)
				if err != nil {
					return nil, invalidWorkflow()
				}
				id = strings.TrimPrefix(u.Path, "/api/files/assets/")
			}
		}
		allowed, err := s.Store.IsCreativeChatAsset(ctx, user, in.ChatID, id)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, store.ErrNotFound
		}
		a, err := s.Store.OwnedAsset(ctx, user, id)
		if err != nil {
			return nil, err
		}
		if err = add(a); err != nil {
			return nil, err
		}
	}
	if in.ReferenceURL != "" {
		a, err := s.resolveCreativeReference(ctx, user, in.ReferenceURL)
		if err != nil {
			return nil, err
		}
		if err = add(a); err != nil {
			return nil, err
		}
	}
	for _, id := range in.ReferenceAssetIDs {
		a, err := s.Store.OwnedAsset(ctx, user, id)
		if err != nil {
			return nil, err
		}
		if err = add(a); err != nil {
			return nil, err
		}
	}
	for _, upload := range uploads {
		a, err := s.saveImage(ctx, user, "", "", "reference", upload.name, bytes.NewReader(upload.data))
		if err != nil {
			return nil, &workflowError{400, "INVALID_REFERENCE", "Use a valid JPEG, PNG or WebP within the image limits"}
		}
		if err = add(a); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (s *Server) creativeSingle(w http.ResponseWriter, r *http.Request) {
	in, uploads, identity, err := readCreativeInput(w, r, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	kind := "creative"
	fallback := "1024x1024"
	if strings.HasSuffix(r.URL.Path, "/aplus-images") {
		kind = "aplus"
		fallback = "970x600"
	} else {
		in.Size = requestedCreativeSize(in.Size, in.Prompt)
	}
	ctx, user := r.Context(), currentUser(r).ID
	key, err := workflowKey(r, kind, identity, false)
	if err == nil {
		err = s.screenUserInput(ctx, user, "", kind+"_image", key, creativeUserText(in))
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	policy, err := s.workflowImagePolicy(ctx, kind, in, fallback, creative.Default(in.ProductName, kind))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	in.Size = policy.Size
	if err = s.requireWorkflowImageProvider(ctx, user, policy); err != nil {
		s.workflowError(w, r, err)
		return
	}
	refs, err := s.resolveCreativeInputs(ctx, user, in, uploads)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	result, _, err := s.Store.CreativeWrite(ctx, user, key, identity, func(tx pgx.Tx) (store.WorkflowResult, error) {
		p, t, err := s.createImageProjectTx(ctx, tx, user, key, in, policy, refs, map[string]any{}, nil)
		return store.WorkflowResult{ProjectID: p.ID, TaskIDs: []string{t.ID}}, err
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	view, err := s.projectDetail(ctx, user, result.ProjectID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, view)
}

func (s *Server) creativeEstimate(w http.ResponseWriter, r *http.Request) {
	var in creative.ImageRequest
	if !workflowDecode(w, r, &in, false) {
		return
	}
	size := requestedCreativeSize(in.Size, in.Message)
	if in.ChatID != "" {
		chat, err := s.Store.CreativeChat(r.Context(), currentUser(r).ID, in.ChatID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		size = creative.Default(size, chat.Size)
	}
	in.Size = size
	policy, err := s.workflowImagePolicy(r.Context(), "creative", in, "1024x1024", "creative")
	if err == nil {
		err = s.workflowImageSelection(r.Context(), currentUser(r).ID, jobs.ImageInput{Model: policy.Model, FallbackModel: policy.FallbackModel})
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	tier := imagePriceTiers[policy.Size]
	ok(w, r, map[string]any{"size": policy.Size, "quality_label": tier, "credits_cost": policy.Cost, "model": policy.Model, "fallback_model": policy.FallbackModel})
}

const creativePlanPrompt = `You are PicPilot's image intent planner. Use the latest request and recent conversation. Return JSON only: {"intent":"chat|clarify|generate_image|edit_image","reply":"","final_prompt":"","confidence":0.0}. Greetings, questions about capabilities/prices, and cancellation are chat; unclear subjects require clarification. Do not generate or charge for mere conversation, prompt-writing requests, or requests not to generate. A concrete image request or a continuation answering a previous clarification may generate; modifications to existing images may edit. For image intents, write an executable final_prompt preserving the subject, exact requested visible text, style, reference identity, composition, lighting and factual constraints. Never invent product specifications, certifications or claims. Do not claim a task or payment has already happened. For chat/clarify give a useful reply; leave final_prompt empty. If confidence is below 0.7, clarify. Each field is at most 32000 characters.`

func (s *Server) creativeGenerate(w http.ResponseWriter, r *http.Request) {
	in, uploads, identity, err := readCreativeInput(w, r, false)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if strings.TrimSpace(in.Message) == "" || len([]rune(in.Message)) > 3000 {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	ctx, user := r.Context(), currentUser(r).ID
	key, err := workflowKey(r, "creative-chat/"+in.ChatID, identity, false)
	if err == nil {
		err = s.screenUserInput(ctx, user, "", "creative_chat", key, creativeUserText(in))
	}
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	in.Size = requestedCreativeSize(in.Size, in.Message)
	var chat store.CreativeChat
	history := []string{}
	if in.ChatID != "" {
		chat, err = s.Store.CreativeChat(ctx, user, in.ChatID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		if err = s.reconcileCreativeChat(ctx, chat); err != nil {
			s.workflowError(w, r, err)
			return
		}
		messages, err := s.Store.CreativeMessages(ctx, user, in.ChatID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		for _, m := range messages[max(0, len(messages)-8):] {
			content := []rune(m.Content)
			if len(content) > 3000 {
				content = content[:3000]
			}
			history = append(history, m.Role+": "+string(content))
		}
		in.Style = creative.Default(in.Style, chat.Style)
		in.Size = creative.Default(in.Size, chat.Size)
		in.Language = creative.Default(in.Language, chat.Language)
	}
	in.Size, err = s.workflowDefaultImageSize(ctx, "creative", in.Size, "1024x1024")
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	in.Language = creative.Default(in.Language, "zh-CN")
	if _, err = s.workflowImageCost(ctx, in.Size, "creative"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	if err = s.requireWorkflowProvider(ctx, user, "text"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	refs, err := s.resolveCreativeInputs(ctx, user, in, uploads)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	if len(refs) == 0 && chat.ID != "" && !creative.IgnoreReferences(in) {
		versions, err := s.Store.CreativeVersions(ctx, user, chat.ID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		if len(versions) > 0 {
			last := versions[len(versions)-1]
			refs = append(refs, last.ReferenceAssetIDs...)
			task, err := s.Store.Task(ctx, user, last.TaskID)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			if task.Status == "succeeded" {
				id := creative.String(workflowMap(task.Result)["artifact_id"])
				if id != "" {
					refs = append([]string{id}, refs...)
					refs = refs[:min(10, len(refs))]
				}
			}
		}
	}
	input := creative.Text(creativePlanPrompt, map[string]any{"message": in.Message, "style": in.Style, "size": in.Size, "language": in.Language, "recent_context": history, "has_image_context": chat.ID != "", "has_reference": len(refs) > 0}, true)
	if err = s.applyWorkflowSystemPrompt(ctx, &input, "prompt/creative-chat", "prompt/optimize"); err != nil {
		s.workflowError(w, r, err)
		return
	}
	creative.AddVision(&input, refs)
	if creative.ValidateFields(input) != nil {
		s.workflowError(w, r, invalidWorkflow())
		return
	}
	result, _, err := s.Store.CreativeWrite(ctx, user, key, identity, func(tx pgx.Tx) (store.WorkflowResult, error) {
		title := []rune(in.Message)
		if len(title) > 60 {
			title = title[:60]
		}
		c, err := s.Store.SaveCreativeChatTx(ctx, tx, store.CreativeChat{ID: chat.ID, UserID: user, Title: string(title), Style: in.Style, Size: in.Size, Language: in.Language})
		if err != nil {
			return store.WorkflowResult{}, err
		}
		t, _, err := s.Store.SubmitProjectTaskTx(ctx, tx, store.SubmitTask{UserID: user, Capability: "text", Kind: "creative_plan", Title: "Creative intent and prompt", Request: creative.JSON(jobs.Payload{Version: 1, Text: &input}), IdempotencyKey: key})
		if err != nil {
			return store.WorkflowResult{}, err
		}
		stored := in
		stored.ReferenceURL = ""
		stored.ReferenceURLs = nil
		stored.ReferenceAssetIDs = refs
		stored.ChatID = c.ID
		if err = s.Store.AddCreativeTurnTx(ctx, tx, c.ID, t.ID, creative.JSON(stored), refs, in.Message); err != nil {
			return store.WorkflowResult{}, err
		}
		return store.WorkflowResult{ChatID: c.ID, TaskIDs: []string{t.ID}}, nil
	})
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	view, err := s.creativeChatView(ctx, user, result.ChatID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	response := map[string]any{"chat": view, "project": nil, "final_prompt": "", "version_no": 0, "task_id": result.TaskIDs[0], "planning_task_id": result.TaskIDs[0], "status": "queued"}
	turns, err := s.Store.CreativeTurns(ctx, user, result.ChatID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	for _, turn := range turns {
		if turn.TaskID != result.TaskIDs[0] {
			continue
		}
		response["status"] = "succeeded"
		if turn.Status == "pending" {
			task, err := s.Store.Task(ctx, user, turn.TaskID)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			response["status"] = task.Status
			response["safe_error"] = task.ErrorCode
		}
		if turn.Status == "failed" {
			response["status"] = "failed"
		}
		if turn.Status != "pending" {
			response["safe_error"] = turn.ErrorCode
		}
		if turn.Result.ProjectID != "" {
			detail, err := s.projectDetail(ctx, user, turn.Result.ProjectID)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			response["project"] = detail
			response["version_no"] = turn.Result.VersionNo
			if len(turn.Result.TaskIDs) > 0 {
				task, err := s.Store.Task(ctx, user, turn.Result.TaskIDs[0])
				if err != nil {
					s.workflowError(w, r, err)
					return
				}
				response["task_id"] = task.ID
				response["status"] = task.Status
				response["safe_error"] = task.ErrorCode
			}
			versions, err := s.Store.CreativeVersions(ctx, user, result.ChatID)
			if err != nil {
				s.workflowError(w, r, err)
				return
			}
			for _, v := range versions {
				if v.ProjectID == turn.Result.ProjectID {
					response["final_prompt"] = v.FinalPrompt
				}
			}
		}
	}
	ok(w, r, response)
}

func (s *Server) creativeChats(w http.ResponseWriter, r *http.Request) {
	chats, err := s.Store.CreativeChats(r.Context(), currentUser(r).ID)
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	out := []any{}
	for _, c := range chats {
		v, err := s.creativeChatView(r.Context(), c.UserID, c.ID)
		if err != nil {
			s.workflowError(w, r, err)
			return
		}
		out = append(out, v["chat"])
	}
	ok(w, r, out)
}
func (s *Server) creativeChat(w http.ResponseWriter, r *http.Request) {
	v, err := s.creativeChatView(r.Context(), currentUser(r).ID, r.PathValue("chatId"))
	if err != nil {
		s.workflowError(w, r, err)
		return
	}
	ok(w, r, v)
}

func (s *Server) creativeChatView(ctx context.Context, user, id string) (map[string]any, error) {
	chat, err := s.Store.CreativeChat(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if err = s.reconcileCreativeChat(ctx, chat); err != nil {
		return nil, err
	}
	chat, err = s.Store.CreativeChat(ctx, user, id)
	if err != nil {
		return nil, err
	}
	versions, err := s.Store.CreativeVersions(ctx, user, id)
	if err != nil {
		return nil, err
	}
	messages, err := s.Store.CreativeMessages(ctx, user, id)
	if err != nil {
		return nil, err
	}
	turns, err := s.Store.CreativeTurns(ctx, user, id)
	if err != nil {
		return nil, err
	}
	summary := workflowMap(creative.JSON(chat))
	summary["version_count"] = len(versions)
	summary["latest_status"] = "draft"
	summary["latest_result_url"] = ""
	summary["reference_url"] = ""
	views := []any{}
	byVersion := map[string]map[string]any{}
	for _, v := range versions {
		t, err := s.Store.Task(ctx, user, v.TaskID)
		if err != nil {
			return nil, err
		}
		task := s.projectTaskView(ctx, t)
		view := workflowMap(creative.JSON(v))
		urls := []string{}
		for _, id := range v.ReferenceAssetIDs {
			a, err := s.Store.OwnedAsset(ctx, user, id)
			if err != nil {
				return nil, err
			}
			urls = append(urls, creative.String(s.assetView(a)["url"]))
		}
		view["reference_urls"] = urls
		view["reference_url"] = ""
		if len(urls) > 0 {
			view["reference_url"] = urls[0]
		}
		view["status"] = t.Status
		view["result_url"] = task["result_url"]
		view["safe_error"] = t.ErrorCode
		view["updated_at"] = t.UpdatedAt
		for _, m := range messages {
			if m.VersionID == v.ID {
				view[m.Role+"_message_id"] = m.ID
			}
		}
		views = append(views, view)
		byVersion[v.ID] = view
		summary["latest_status"] = t.Status
		summary["latest_result_url"] = task["result_url"]
		summary["reference_url"] = view["reference_url"]
	}
	messageViews := []any{}
	for _, m := range messages {
		view := workflowMap(creative.JSON(m))
		view["project_id"] = ""
		view["result_url"] = ""
		view["status"] = "submitted"
		view["version_no"] = 0
		if v := byVersion[m.VersionID]; v != nil {
			view["project_id"] = v["project_id"]
			view["result_url"] = v["result_url"]
			view["status"] = v["status"]
			view["version_no"] = v["version_no"]
		} else if m.TaskID != "" {
			t, err := s.Store.Task(ctx, user, m.TaskID)
			if err != nil {
				return nil, err
			}
			view["status"] = t.Status
			view["safe_error"] = t.ErrorCode
			for _, turn := range turns {
				if turn.TaskID == m.TaskID && turn.Status == "failed" {
					view["status"] = "failed"
					view["safe_error"] = turn.ErrorCode
				}
			}
		}
		if m.Role == "assistant" {
			summary["latest_status"] = view["status"]
		}
		messageViews = append(messageViews, view)
	}
	pending := []any{}
	for _, turn := range turns {
		if turn.Status == "pending" {
			t, err := s.Store.Task(ctx, user, turn.TaskID)
			if err != nil {
				return nil, err
			}
			pending = append(pending, map[string]any{"task_id": t.ID, "status": t.Status})
			summary["latest_status"] = t.Status
		}
	}
	return map[string]any{"chat": summary, "messages": messageViews, "versions": views, "pending_tasks": pending}, nil
}

func (s *Server) reconcileCreativeChat(ctx context.Context, chat store.CreativeChat) error {
	turns, err := s.Store.CreativeTurns(ctx, chat.UserID, chat.ID)
	if err != nil {
		return err
	}
	for _, turn := range turns {
		if turn.Status != "pending" {
			continue
		}
		task, err := s.Store.Task(ctx, chat.UserID, turn.TaskID)
		if err != nil {
			return err
		}
		if task.Status != "succeeded" && task.Status != "failed" && task.Status != "cancelled" {
			continue
		}
		in := creative.ImageRequest{}
		if json.Unmarshal(turn.Request, &in) != nil {
			return creative.ErrInvalid
		}
		plan, parseErr := creative.Object(creative.String(workflowMap(task.Result)["text"]))
		code := ""
		intent, reply, prompt := "", "", ""
		if task.Status != "succeeded" {
			code = creative.Default(task.ErrorCode, "TEXT_TASK_FAILED")
		} else if parseErr != nil {
			code = "INVALID_PLAN_RESULT"
		} else {
			intent = creative.String(plan["intent"])
			reply = creative.String(plan["reply"])
			prompt = creative.String(plan["final_prompt"])
			confidence, _ := plan["confidence"].(float64)
			if confidence < 0.7 && intent != "chat" && intent != "clarify" {
				code = "INVALID_PLAN_RESULT"
			}
			switch intent {
			case "chat", "clarify":
				if strings.TrimSpace(reply) == "" {
					code = "INVALID_PLAN_RESULT"
				}
			case "generate_image", "edit_image":
				if strings.TrimSpace(prompt) == "" {
					code = "INVALID_PLAN_RESULT"
				}
			default:
				code = "INVALID_PLAN_RESULT"
			}
		}
		var policy workflowImagePolicy
		if code == "" && (intent == "generate_image" || intent == "edit_image") {
			policy, err = s.workflowImagePolicy(ctx, "creative", in, "1024x1024", creative.Default(in.ProductName, "creative"))
			if err == nil {
				err = s.requireWorkflowImageProvider(ctx, chat.UserID, policy)
			}
			if err != nil {
				var e *workflowError
				if errors.As(err, &e) {
					code = e.code
				} else {
					return err
				}
			}
		}
		_, _, err = s.Store.CreativeWrite(ctx, chat.UserID, "creative-apply:"+turn.ID, creative.JSON(map[string]string{"turn_id": turn.ID}), func(tx pgx.Tx) (store.WorkflowResult, error) {
			result := store.WorkflowResult{ChatID: chat.ID}
			if code == "" && (intent == "generate_image" || intent == "edit_image") {
				// A savepoint keeps rejected image planning from leaving a project,
				// partial charge or version behind while recording the real failure.
				nested, err := tx.Begin(ctx)
				if err != nil {
					return result, err
				}
				in.Prompt = prompt
				p, t, err := s.createImageProjectTx(ctx, nested, chat.UserID, "creative-image:"+turn.ID, in, policy, turn.ReferenceAssetIDs, map[string]any{}, nil)
				var version store.CreativeVersion
				if err == nil {
					version, err = s.Store.AddCreativeVersionTx(ctx, nested, chat.UserID, store.CreativeVersion{ChatID: chat.ID, ProjectID: p.ID, TaskID: t.ID, UserRequest: in.Message, FinalPrompt: prompt, Style: in.Style, Size: in.Size, ReferenceAssetIDs: turn.ReferenceAssetIDs})
				}
				if err == nil {
					meta := workflowMap(p.Metadata)
					meta["source_chat_id"] = chat.ID
					meta["version_count"] = version.VersionNo
					p.Metadata = creative.JSON(meta)
					err = s.Store.UpdateProjectTx(ctx, nested, p)
				}
				if err != nil {
					_ = nested.Rollback(ctx)
					var configError *workflowError
					switch {
					case errors.As(err, &configError):
						code = configError.code
					case errors.Is(err, store.ErrInsufficientCredits):
						code = "INSUFFICIENT_CREDITS"
					case errors.Is(err, store.ErrConflict):
						code = "VERSION_LIMIT_OR_CONFLICT"
					case errors.Is(err, creative.ErrInvalid):
						code = "INVALID_PLAN_RESULT"
					case errors.Is(err, store.ErrNotFound):
						code = "REFERENCE_UNAVAILABLE"
					default:
						return result, err
					}
				} else {
					if err = nested.Commit(ctx); err != nil {
						return result, err
					}
					result.ProjectID = p.ID
					result.TaskIDs = []string{t.ID}
					result.VersionNo = version.VersionNo
				}
			}
			if err := s.Store.FinishCreativeTurnTx(ctx, tx, turn, result, reply, code); err != nil {
				return result, err
			}
			return result, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RunCreativeWorkflows advances completed creative planning tasks without client
// polling. Run it beside RunTasks with the same shutdown context.
func (s *Server) RunCreativeWorkflows(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			projects, err := s.Store.PendingProjectCopies(ctx)
			if err != nil {
				s.Logger.Error("project copy scan failed", "sqlstate", store.SQLState(err))
			} else {
				for _, p := range projects {
					if _, err = s.reconcileProjectCopy(ctx, p); err != nil {
						s.Logger.Error("project copy reconciliation failed", "sqlstate", store.SQLState(err))
					}
				}
			}
			chats, err := s.Store.PendingCreativeChats(ctx)
			if err != nil {
				s.Logger.Error("creative workflow scan failed", "sqlstate", store.SQLState(err))
				continue
			}
			for _, chat := range chats {
				if err = s.reconcileCreativeChat(ctx, chat); err != nil {
					s.Logger.Error("creative workflow reconciliation failed", "sqlstate", store.SQLState(err))
				}
			}
		}
	}
}
