package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/store"
)

// Called by the owning server bootstrap. No billing/provider/worker routes here.
func (s *Server) registerAdminRoutes() {
	if s.Config.Edition != config.Platform {
		return
	}
	probe, _ := http.NewRequest(http.MethodGet, "/api/admin/users", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/admin/users" {
		return
	}
	routes := map[string]http.HandlerFunc{
		"GET /api/admin/dashboard": s.adminDashboard, "GET /api/admin/dashboard/trends": s.adminTrends,
		"GET /api/admin/users": s.adminPage("users"), "GET /api/admin/users/{id}": s.adminUser,
		"POST /api/admin/users/{id}/credits": s.adminAdjustCredits, "PATCH /api/admin/users/{id}/status": s.adminStatus,
		"POST /api/admin/users/{id}/password": s.adminPassword, "POST /api/admin/users/{id}/risk-events/clear": s.adminClearRisk,
		"GET /api/admin/ledger": s.adminPage("ledger"), "GET /api/admin/credit-ledger": s.adminPage("ledger"), "GET /api/admin/users/{id}/ledger": s.adminPage("ledger"),
		"GET /api/admin/risk-events": s.adminPage("risk"), "GET /api/admin/risk-keywords": s.adminRiskConfig, "GET /api/admin/risk-config": s.adminRiskConfig,
		"PATCH /api/admin/risk-config": s.adminUpdateRisk, "POST /api/admin/risk-keywords": s.adminRiskKeyword, "POST /api/admin/risk-keywords/delete": s.adminRiskKeyword,
		"GET /api/admin/tasks": s.adminPage("tasks"), "GET /api/admin/tasks/{id}": s.adminTaskDetail,
		"GET /api/admin/image-tasks": s.adminPage("image_tasks"), "GET /api/admin/image-tasks/{id}": s.adminTaskDetail,
		"PATCH /api/admin/image-tasks/{id}/retry": s.adminRetryTask, "PATCH /api/admin/image-tasks/{id}/delete": s.adminDeleteTask,
		"GET /api/admin/audit-logs": s.adminPage("audit"), "GET /api/admin/costs": s.adminCosts, "GET /api/admin/text-calls": s.adminPage("text_calls"),
		"GET /api/admin/messages": s.adminPage("messages"), "POST /api/admin/messages": s.adminSendMessage,
		"GET /api/admin/prompts": s.adminPromptList, "POST /api/admin/prompts": s.adminSavePrompt, "POST /api/admin/prompts/{id}": s.adminSavePrompt,
		"GET /api/admin/image-configs": s.adminImageConfigs, "POST /api/admin/image-configs/{id}": s.adminSaveImageConfig,
	}
	for pattern, handler := range routes {
		s.mux.HandleFunc(pattern, s.auth(s.admin(handler)))
	}
}

func (s *Server) adminError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrAdminForbidden):
		fail(w, r, 403, "FORBIDDEN", "Administrator access is required")
	case errors.Is(err, store.ErrLastAdmin):
		fail(w, r, 409, "LAST_SUPER_ADMIN", "At least one active super administrator must remain")
	case errors.Is(err, store.ErrUnsafeRetry):
		fail(w, r, 409, "TASK_NOT_SAFE", "Resolve the existing task and its accounting before retrying or deleting it")
	case errors.Is(err, store.ErrDisabled):
		fail(w, r, 409, "ACCOUNT_DISABLED", "The account is disabled")
	default:
		s.databaseError(w, r, err)
	}
}

func adminFilter(w http.ResponseWriter, r *http.Request) (store.AdminFilter, bool) {
	q := r.URL.Query()
	f := store.AdminFilter{Page: 1, PageSize: 20}
	invalid := func() (store.AdminFilter, bool) {
		fail(w, r, 400, "INVALID_FILTER", "Invalid pagination, filter, or date range")
		return f, false
	}
	for _, values := range q {
		if len(values) > 1 {
			return invalid()
		}
	}
	if raw := q.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return invalid()
		}
		f.Page = n
	}
	size := q.Get("page_size")
	if other := q.Get("pageSize"); other != "" {
		if size != "" && size != other {
			return invalid()
		}
		size = other
	}
	if size != "" {
		n, err := strconv.Atoi(size)
		if err != nil {
			return invalid()
		}
		f.PageSize = n
	}
	f.Keyword = strings.TrimSpace(q.Get("keyword"))
	f.UserKeyword = strings.TrimSpace(q.Get("user_keyword"))
	f.UserID = q.Get("user_id")
	f.Status = q.Get("status")
	f.Role = q.Get("role")
	f.Sort = q.Get("sort")
	f.Provider = q.Get("provider")
	f.Model = q.Get("model")
	f.Feature = q.Get("feature")
	f.MediaType = q.Get("media_type")
	f.ImageType = q.Get("image_type")
	f.RiskLevel = q.Get("risk_level")
	f.Source = q.Get("source")
	f.Action = q.Get("action")
	f.TargetType = q.Get("target_type")
	f.BizType = q.Get("biz_type")
	for key, target := range map[string]**int64{"min_credits": &f.MinCredits, "max_credits": &f.MaxCredits} {
		if raw := q.Get(key); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				return invalid()
			}
			*target = &n
		}
	}
	if raw := q.Get("cleared"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return invalid()
		}
		f.Cleared = &b
	}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for key, target := range map[string]**time.Time{"from": &f.From, "to": &f.Until} {
		if raw := q.Get(key); raw != "" {
			v, err := time.ParseInLocation("2006-01-02", raw, zone)
			if err != nil {
				v, err = time.Parse(time.RFC3339, raw)
			} else if key == "to" {
				v = v.AddDate(0, 0, 1)
			}
			if err != nil || v.Year() < 1970 || v.Year() > 2200 {
				return invalid()
			}
			*target = &v
		}
	}
	if f.Validate() != nil {
		return invalid()
	}
	return f, true
}

func (s *Server) adminPage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, valid := adminFilter(w, r)
		if !valid {
			return
		}
		actor := currentUser(r).ID
		ctx := r.Context()
		var p store.AdminPage
		var err error
		switch kind {
		case "users":
			p, err = s.Store.AdminUsers(ctx, actor, f)
		case "ledger":
			if id := r.PathValue("id"); id != "" {
				f.UserID = id
			}
			p, err = s.Store.AdminLedger(ctx, actor, f)
		case "risk":
			p, err = s.Store.AdminRiskEvents(ctx, actor, f)
		case "audit":
			p, err = s.Store.AdminAuditLogs(ctx, actor, f)
		case "messages":
			p, err = s.Store.AdminMessages(ctx, actor, f)
		case "text_calls":
			p, err = s.Store.AdminTextCalls(ctx, actor, f)
		case "tasks", "image_tasks":
			if kind == "image_tasks" {
				f.MediaType = "image"
			}
			p, err = s.Store.AdminTasks(ctx, actor, f)
		default:
			fail(w, r, 404, "NOT_FOUND", "Endpoint not found")
			return
		}
		if err != nil {
			s.adminError(w, r, err)
			return
		}
		if kind == "tasks" || kind == "image_tasks" {
			for _, row := range p.Rows {
				s.adminTaskURLs(r, row)
			}
		}
		ok(w, r, p)
	}
}

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	result, err := s.Store.AdminDashboard(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, result)
}
func (s *Server) adminTrends(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	result, err := s.Store.AdminTrends(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, result)
}
func (s *Server) adminCosts(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	result, err := s.Store.AdminCosts(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, result)
}
func (s *Server) adminUser(w http.ResponseWriter, r *http.Request) {
	result, err := s.Store.AdminUser(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, result)
}

func (s *Server) adminAdjustCredits(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChangeAmount int64  `json:"change_amount"`
		Remark       string `json:"remark"`
		RequestToken string `json:"request_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = in.RequestToken
	} else if in.RequestToken != "" && key != in.RequestToken {
		fail(w, r, 400, "INVALID_REQUEST", "Idempotency keys must agree")
		return
	}
	if len(key) < 8 || len(key) > 128 || in.ChangeAmount == 0 || len(in.Remark) > 1000 {
		fail(w, r, 400, "INVALID_REQUEST", "Provide a nonzero amount and an idempotency key of 8 to 128 characters")
		return
	}
	entry, replay, err := s.Store.AdminAdjustCredits(r.Context(), currentUser(r).ID, r.PathValue("id"), in.ChangeAmount, key, in.Remark, requestID(r))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"user_id": entry.UserID, "balance_after": entry.Balance, "ledger_id": entry.ID, "idempotent": replay})
}
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Status == "banned" {
		in.Status = "disabled"
	}
	if !oneOf(in.Status, "active", "disabled") {
		fail(w, r, 400, "INVALID_STATUS", "Invalid account status")
		return
	}
	if err := s.Store.AdminSetUserStatus(r.Context(), currentUser(r).ID, r.PathValue("id"), in.Status, requestID(r)); err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"user_id": r.PathValue("id"), "status": in.Status})
}
func (s *Server) adminPassword(w http.ResponseWriter, r *http.Request) {
	if !s.authRate(w, r) {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		fail(w, r, 400, "PASSWORD_INVALID", "Password must contain 12 to 72 UTF-8 bytes")
		return
	}
	if err = s.Store.AdminResetPassword(r.Context(), currentUser(r).ID, r.PathValue("id"), hash, requestID(r)); err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"user_id": r.PathValue("id"), "updated": true, "requires_login": true})
}

func (s *Server) adminRiskConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.AdminRiskConfig(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, c)
}
func (s *Server) adminUpdateRisk(w http.ResponseWriter, r *http.Request) {
	var in store.AdminRiskPatch
	if !decode(w, r, &in) {
		return
	}
	c, err := s.Store.AdminUpdateRiskConfig(r.Context(), currentUser(r).ID, requestID(r), in)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, c)
}
func (s *Server) adminRiskKeyword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level   string `json:"level"`
		Keyword string `json:"keyword"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := s.Store.AdminRiskKeyword(r.Context(), currentUser(r).ID, requestID(r), in.Level, in.Keyword, strings.HasSuffix(r.URL.Path, "/delete"))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, c)
}
func (s *Server) adminClearRisk(w http.ResponseWriter, r *http.Request) {
	count, status, err := s.Store.AdminClearRiskEvents(r.Context(), currentUser(r).ID, r.PathValue("id"), requestID(r))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"user_id": r.PathValue("id"), "status": status, "cleared": count})
}

func (s *Server) adminTaskURLs(r *http.Request, row map[string]any) {
	user, _ := row["user_id"].(string)
	id, _ := row["artifact_id"].(string)
	if id != "" {
		if a, err := s.Store.OwnedAsset(r.Context(), user, id); err == nil {
			row["result_url"] = s.assetView(a)["url"]
		}
	}
	if refs, ok := row["reference_images"].([]map[string]any); ok {
		for _, ref := range refs {
			if id, ok := ref["id"].(string); ok {
				if a, err := s.Store.OwnedAsset(r.Context(), user, id); err == nil {
					ref["url"] = s.assetView(a)["url"]
				}
			}
		}
	}
}
func (s *Server) adminTaskDetail(w http.ResponseWriter, r *http.Request) {
	capability := ""
	if strings.Contains(r.URL.Path, "/image-tasks/") {
		capability = "image"
	}
	row, err := s.Store.AdminTaskDetail(r.Context(), currentUser(r).ID, r.PathValue("id"), capability, requestID(r))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	s.adminTaskURLs(r, row)
	ok(w, r, row)
}
func (s *Server) adminRetryTask(w http.ResponseWriter, r *http.Request) {
	t, replay, err := s.Store.AdminRetryImageTask(r.Context(), currentUser(r).ID, r.PathValue("id"), requestID(r))
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"task_id": t.ID, "original_task_id": r.PathValue("id"), "status": t.Status, "credits_cost": t.CreditsCost, "idempotent": replay})
}
func (s *Server) adminDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.AdminDeleteImageTask(r.Context(), currentUser(r).ID, r.PathValue("id"), requestID(r)); err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"task_id": r.PathValue("id"), "deleted": true})
}

func (s *Server) adminSendMessage(w http.ResponseWriter, r *http.Request) {
	var in store.AdminMessageInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Store.AdminPublishMessage(r.Context(), currentUser(r).ID, requestID(r), in)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, m)
}
func (s *Server) adminPromptList(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	paged := r.URL.Query().Has("page") || r.URL.Query().Has("page_size") || r.URL.Query().Has("pageSize")
	if !paged {
		f.PageSize = 100
	}
	p, err := s.Store.AdminPrompts(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	if paged {
		ok(w, r, p)
	} else {
		w.Header().Set("X-Total-Count", strconv.FormatInt(p.Total, 10))
		ok(w, r, p.Rows)
	}
}
func (s *Server) adminSavePrompt(w http.ResponseWriter, r *http.Request) {
	var in store.AdminPromptInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.Store.AdminSavePrompt(r.Context(), currentUser(r).ID, r.PathValue("id"), requestID(r), in)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, p)
}
func (s *Server) adminImageConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.AdminImageConfigs(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, rows)
}
func (s *Server) adminSaveImageConfig(w http.ResponseWriter, r *http.Request) {
	var in store.AdminImageConfigInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.Store.AdminSaveImageConfig(r.Context(), currentUser(r).ID, r.PathValue("id"), requestID(r), in)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, p)
}
