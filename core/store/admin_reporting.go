package store

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
)

type TextCallMetric struct {
	ID, UserID, TaskID, Feature, TargetType, Provider, Model, Status, ErrorType string
	HTTPStatus                                                                  int
	DurationMS, InputChars, OutputChars                                         int64
}

// RecordUserActivity is an optional authenticated-request hook. Updates are
// coalesced to one per minute and never retain IPs, headers or request bodies.
func (s *Store) RecordUserActivity(ctx context.Context, userID string) error {
	if s.Edition != config.Platform {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO admin_user_activity(user_id,last_request_at) SELECT id,now() FROM users WHERE id=$1 AND status='active' ON CONFLICT(user_id) DO UPDATE SET last_request_at=EXCLUDED.last_request_at WHERE admin_user_activity.last_request_at<now()-interval '1 minute'`, userID)
	return err
}

// RecordTextCall accepts only accounting fields, never prompt/response bodies.
// Reuse ID for queued -> terminal updates and for duplicate callbacks.
func (s *Store) RecordTextCall(ctx context.Context, m TextCallMetric) error {
	if s.Edition != config.Platform {
		return ErrAdminForbidden
	}
	if !adminLabel.MatchString(m.ID) || !adminLabel.MatchString(m.Feature) || m.DurationMS < 0 || m.InputChars < 0 || m.OutputChars < 0 || m.HTTPStatus != 0 && (m.HTTPStatus < 100 || m.HTTPStatus > 599) {
		return ErrConflict
	}
	for _, v := range []string{m.TargetType, m.Provider, m.Model, m.ErrorType} {
		if v != "" && !adminLabel.MatchString(v) {
			return ErrConflict
		}
	}
	if m.Status != "queued" && m.Status != "success" && m.Status != "failed" && m.Status != "uncertain" {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if m.TaskID != "" {
		var owns bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND user_id=$2 AND capability='text')`, m.TaskID, m.UserID).Scan(&owns); err != nil {
			return err
		}
		if !owns {
			return ErrNotFound
		}
	}
	res, err := tx.Exec(ctx, `INSERT INTO admin_text_calls(id,user_id,task_id,feature,target_type,provider,model,status,http_status,duration_ms,input_chars,output_chars,error_type)
 VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
 ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,http_status=EXCLUDED.http_status,duration_ms=EXCLUDED.duration_ms,input_chars=EXCLUDED.input_chars,output_chars=EXCLUDED.output_chars,error_type=EXCLUDED.error_type,updated_at=now()
 WHERE admin_text_calls.user_id IS NOT DISTINCT FROM EXCLUDED.user_id AND admin_text_calls.task_id IS NOT DISTINCT FROM EXCLUDED.task_id AND admin_text_calls.feature=EXCLUDED.feature AND admin_text_calls.target_type=EXCLUDED.target_type AND admin_text_calls.provider=EXCLUDED.provider AND admin_text_calls.model=EXCLUDED.model
 AND (admin_text_calls.status='queued' OR (admin_text_calls.status=EXCLUDED.status AND admin_text_calls.http_status=EXCLUDED.http_status AND admin_text_calls.duration_ms=EXCLUDED.duration_ms AND admin_text_calls.input_chars=EXCLUDED.input_chars AND admin_text_calls.output_chars=EXCLUDED.output_chars AND admin_text_calls.error_type=EXCLUDED.error_type))`, m.ID, m.UserID, m.TaskID, m.Feature, m.TargetType, m.Provider, m.Model, m.Status, m.HTTPStatus, m.DurationMS, m.InputChars, m.OutputChars, m.ErrorType)
	if err != nil {
		return normalizeError(err)
	}
	if res.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

var adminCostAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,13})(\.[0-9]{1,6})?$`)

// RecordTaskCost records a confirmed CNY observation under a stable attempt or
// invoice-line ID. No currency conversion or guessed provider pricing occurs.
func (s *Store) RecordTaskCost(ctx context.Context, observationID, taskID, amountCNY string) error {
	if s.Edition != config.Platform {
		return ErrAdminForbidden
	}
	if !adminLabel.MatchString(observationID) || !adminCostAmount.MatchString(amountCNY) {
		return ErrConflict
	}
	res, err := s.Pool.Exec(ctx, `INSERT INTO admin_task_costs(observation_id,task_id,amount_cny) VALUES($1,$2,$3::numeric) ON CONFLICT(observation_id) DO UPDATE SET observation_id=EXCLUDED.observation_id WHERE admin_task_costs.task_id=EXCLUDED.task_id AND admin_task_costs.amount_cny=EXCLUDED.amount_cny`, observationID, taskID, amountCNY)
	if err != nil {
		return normalizeError(err)
	}
	if res.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) AdminTextCalls(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"c.id", "c.feature", "c.target_type", "c.provider", "c.model", "c.error_type"}, f.Keyword)
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	q.exact("c.user_id", f.UserID)
	q.exact("c.status", f.Status)
	q.exact("c.provider", f.Provider)
	q.exact("c.model", f.Model)
	q.exact("c.feature", f.Feature)
	q.dates("c.created_at", f)
	from := "admin_text_calls c LEFT JOIN users u ON u.id=c.user_id"
	p, err := adminPaged(ctx, tx, f, from, `to_jsonb(c)||jsonb_build_object('user_email',COALESCE(u.email,''),'user_nickname',COALESCE(u.nickname,''),'attributed',c.user_id IS NOT NULL)`, "c.created_at DESC,c.id DESC", q)
	if err != nil {
		return p, err
	}
	p.Summary, err = adminJSON(tx.QueryRow(ctx, `SELECT jsonb_build_object('calls',count(*),'users',count(DISTINCT c.user_id),'success',count(*) FILTER(WHERE c.status='success'),'failed',count(*) FILTER(WHERE c.status='failed'),'queued',count(*) FILTER(WHERE c.status='queued'),'uncertain',count(*) FILTER(WHERE c.status='uncertain'),'success_rate',COALESCE(round(100.0*count(*) FILTER(WHERE c.status='success')/NULLIF(count(*),0),2),0),'avg_duration_ms',COALESCE(round(avg(c.duration_ms)),0)) FROM `+from+" WHERE "+q.sql(), q.args...))
	return p, err
}

func adminReportCTE(f AdminFilter, includeDeleted bool) (string, []any) {
	q := adminTaskWhere(f)
	if includeDeleted {
		q.parts[0] = "TRUE"
	}
	return `WITH filtered AS (SELECT t.id,t.capability,t.kind,t.provider_id,t.status,t.credits_cost,t.created_at,
 (SELECT sum(amount_cny) FROM admin_task_costs c WHERE c.task_id=t.id) AS cost,
 (SELECT COALESCE(sum(-change_amount),0) FROM credit_ledger l WHERE l.user_id=t.user_id AND l.biz_id=t.id AND l.biz_type='generation_charge') AS charged,
 (SELECT COALESCE(sum(change_amount),0) FROM credit_ledger l WHERE l.user_id=t.user_id AND l.biz_id=t.id AND l.biz_type='generation_refund') AS refunded
 FROM tasks t JOIN users u ON u.id=t.user_id WHERE ` + q.sql() + `) `, q.args
}

func (s *Store) AdminDashboard(ctx context.Context, actor string, f AdminFilter) (map[string]any, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	cte, args := adminReportCTE(f, false)
	result, err := adminJSON(tx.QueryRow(ctx, cte+`SELECT jsonb_build_object(
 'users',(SELECT count(*) FROM users),'projects',(SELECT count(*) FROM projects WHERE deleted_at IS NULL),'credits',(SELECT COALESCE(sum(credits),0) FROM users),
 'total_tasks',count(*),'image_tasks',count(*) FILTER(WHERE capability='image'),'video_tasks',count(*) FILTER(WHERE capability='video'),'text_tasks',count(*) FILTER(WHERE capability='text'),
 'queued_tasks',count(*) FILTER(WHERE status='queued'),'queued_image_tasks',count(*) FILTER(WHERE status='queued' AND capability='image'),'queued_video_tasks',count(*) FILTER(WHERE status='queued' AND capability='video'),
 'active_tasks',count(*) FILTER(WHERE status IN ('queued','running','polling','waiting_worker')),'active_image_tasks',count(*) FILTER(WHERE status IN ('queued','running','polling','waiting_worker') AND capability='image'),'active_video_tasks',count(*) FILTER(WHERE status IN ('queued','running','polling','waiting_worker') AND capability='video'),
 'failed_tasks',count(*) FILTER(WHERE status='failed'),'failed_image_tasks',count(*) FILTER(WHERE status='failed' AND capability='image'),'failed_video_tasks',count(*) FILTER(WHERE status='failed' AND capability='video'),'uncertain_tasks',count(*) FILTER(WHERE status='uncertain'),
 'video_credits_income',COALESCE(sum(charged-refunded) FILTER(WHERE capability='video'),0),'video_refunded_credits',COALESCE(sum(refunded) FILTER(WHERE capability='video'),0),
 'actual_cost',sum(cost),'image_actual_cost',sum(cost) FILTER(WHERE capability='image'),'video_actual_cost',sum(cost) FILTER(WHERE capability='video'),'unknown_cost_tasks',count(*) FILTER(WHERE cost IS NULL),
 'pending_orders',(SELECT count(*) FROM recharge_orders WHERE status='pending'),'billing_metrics_available',true) FROM filtered`, args...))
	if err != nil {
		return nil, err
	}
	billing, err := adminBillingSummary(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	for key, value := range billing {
		result[key] = value
	}
	// Trends share the dashboard's snapshot and filters.
	trends, err := adminTrends(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	result["trends"] = trends
	return result, nil
}

func adminBillingSummary(ctx context.Context, tx pgx.Tx, f AdminFilter) (map[string]any, error) {
	q := adminWhere{}
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	q.exact("o.user_id", f.UserID)
	q.dates("CASE WHEN o.status='paid' THEN COALESCE(o.paid_at,o.updated_at) ELSE o.created_at END", f)
	return adminJSON(tx.QueryRow(ctx, `SELECT jsonb_build_object(
 'pending_orders',count(*) FILTER(WHERE o.status='pending'),
 'paid_orders',count(*) FILTER(WHERE o.status='paid'),
 'recharge_users',count(DISTINCT o.user_id) FILTER(WHERE o.status='paid'),
 'recharge_credits',COALESCE(sum(o.credits) FILTER(WHERE o.status='paid'),0),
 'recharge',COALESCE(sum(o.amount_cents) FILTER(WHERE o.status='paid' AND o.currency='CNY'),0)/100.0,
 'revenue',COALESCE(sum(o.amount_cents) FILTER(WHERE o.status='paid' AND o.currency='CNY'),0)/100.0,
 'billing_currency','CNY','other_currency_paid_orders',count(*) FILTER(WHERE o.status='paid' AND o.currency<>'CNY'),
 'billing_metrics_available',true) FROM recharge_orders o JOIN users u ON u.id=o.user_id WHERE `+q.sql(), q.args...))
}

func (s *Store) AdminTrends(ctx context.Context, actor string, f AdminFilter) ([]map[string]any, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	return adminTrends(ctx, tx, f)
}

func adminTrends(ctx context.Context, tx pgx.Tx, f AdminFilter) ([]map[string]any, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().In(zone)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	if f.Until != nil {
		v := f.Until.Add(-time.Nanosecond).In(zone)
		end = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, zone)
	}
	start := end.AddDate(0, 0, -6)
	if f.From != nil {
		v := f.From.In(zone)
		start = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, zone)
	}
	if start.After(end) || end.Sub(start) > 365*24*time.Hour {
		return nil, ErrConflict
	}
	cte, args := adminReportCTE(f, false)
	q := adminWhere{args: args, parts: []string{"o.status='paid'"}}
	q.search([]string{"bu.id", "bu.email", "bu.nickname"}, f.UserKeyword)
	q.exact("o.user_id", f.UserID)
	q.dates("COALESCE(o.paid_at,o.updated_at)", f)
	billingWhere := q.sql()
	uq := adminWhere{args: q.args}
	uq.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	uq.exact("u.id", f.UserID)
	uq.dates("u.created_at", f)
	args = uq.args
	args = append(args, start.Format("2006-01-02"), end.Format("2006-01-02"))
	query := cte + fmt.Sprintf(`, days AS (SELECT generate_series($%d::date,$%d::date,interval '1 day')::date AS day),
 daily AS (SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS day,count(*) AS usage,
 count(*) FILTER(WHERE capability='image') AS image_usage,count(*) FILTER(WHERE capability='video') AS video_usage,
 count(*) FILTER(WHERE status='failed') AS failures,count(*) FILTER(WHERE status='failed' AND capability='image') AS image_failures,count(*) FILTER(WHERE status='failed' AND capability='video') AS video_failures,
 sum(charged-refunded) AS credits,sum(cost) AS cost,sum(cost) FILTER(WHERE capability='image') AS image_cost,sum(cost) FILTER(WHERE capability='video') AS video_cost,
 count(*) FILTER(WHERE cost IS NULL) AS unknown_cost_tasks FROM filtered GROUP BY 1),
 billing AS (SELECT (COALESCE(o.paid_at,o.updated_at) AT TIME ZONE 'Asia/Shanghai')::date AS day,
 count(*) AS paid_orders,count(DISTINCT o.user_id) AS recharge_users,sum(o.credits) AS recharge_credits,
 COALESCE(sum(o.amount_cents) FILTER(WHERE o.currency='CNY'),0)/100.0 AS revenue,
 count(*) FILTER(WHERE o.currency<>'CNY') AS other_currency_paid_orders FROM recharge_orders o JOIN users bu ON bu.id=o.user_id WHERE `+billingWhere+` GROUP BY 1)
 SELECT jsonb_build_object('date',d.day::text,'label',to_char(d.day,'MM-DD'),'usage',COALESCE(a.usage,0),'image_usage',COALESCE(a.image_usage,0),'video_usage',COALESCE(a.video_usage,0),
 'users',(SELECT count(*) FROM users u WHERE (`+uq.sql()+`) AND u.created_at>=d.day::timestamp AT TIME ZONE 'Asia/Shanghai' AND u.created_at<(d.day+1)::timestamp AT TIME ZONE 'Asia/Shanghai'),
 'credits',COALESCE(a.credits,0),'failures',COALESCE(a.failures,0),'image_failures',COALESCE(a.image_failures,0),'video_failures',COALESCE(a.video_failures,0),
 'cost',a.cost,'image_cost',a.image_cost,'video_cost',a.video_cost,'unknown_cost_tasks',COALESCE(a.unknown_cost_tasks,0),
 'recharge',COALESCE(b.revenue,0),'revenue',COALESCE(b.revenue,0),'paid_orders',COALESCE(b.paid_orders,0),
 'recharge_users',COALESCE(b.recharge_users,0),'recharge_credits',COALESCE(b.recharge_credits,0),
 'other_currency_paid_orders',COALESCE(b.other_currency_paid_orders,0),'billing_currency','CNY','billing_metrics_available',true)
 FROM days d LEFT JOIN daily a ON a.day=d.day LEFT JOIN billing b ON b.day=d.day ORDER BY d.day`, len(args)-1, len(args))
	return adminJSONRows(ctx, tx, query, args...)
}

func (s *Store) AdminCosts(ctx context.Context, actor string, f AdminFilter) (map[string]any, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	cte, args := adminReportCTE(f, true)
	result, err := adminJSON(tx.QueryRow(ctx, cte+`SELECT jsonb_build_object(
 'total_cost',sum(cost),'image_cost',sum(cost) FILTER(WHERE capability='image'),'video_cost',sum(cost) FILTER(WHERE capability='video'),
 'today_cost',sum(cost) FILTER(WHERE (created_at AT TIME ZONE 'Asia/Shanghai')::date=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'today_image_cost',sum(cost) FILTER(WHERE capability='image' AND (created_at AT TIME ZONE 'Asia/Shanghai')::date=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'today_video_cost',sum(cost) FILTER(WHERE capability='video' AND (created_at AT TIME ZONE 'Asia/Shanghai')::date=(now() AT TIME ZONE 'Asia/Shanghai')::date),
 'average_task_cost',avg(cost),'average_image_cost',avg(cost) FILTER(WHERE capability='image'),'average_video_cost',avg(cost) FILTER(WHERE capability='video'),
 'failure_rate',COALESCE(round(100.0*count(*) FILTER(WHERE status='failed')/NULLIF(count(*),0),2),0),
 'image_failure_rate',COALESCE(round(100.0*count(*) FILTER(WHERE status='failed' AND capability='image')/NULLIF(count(*) FILTER(WHERE capability='image'),0),2),0),
 'video_failure_rate',COALESCE(round(100.0*count(*) FILTER(WHERE status='failed' AND capability='video')/NULLIF(count(*) FILTER(WHERE capability='video'),0),2),0),
 'credits_cost',COALESCE(sum(credits_cost),0),'credits_charged',COALESCE(sum(charged),0),'credits_refunded',COALESCE(sum(refunded),0),'net_video_credits',COALESCE(sum(charged-refunded) FILTER(WHERE capability='video'),0),
 'task_count',count(*),'image_task_count',count(*) FILTER(WHERE capability='image'),'video_task_count',count(*) FILTER(WHERE capability='video'),'known_cost_tasks',count(cost),'unknown_cost_tasks',count(*) FILTER(WHERE cost IS NULL)) FROM filtered`, args...))
	if err != nil {
		return nil, err
	}
	groups := cte + `, groups AS (SELECT capability AS media_type,provider_id AS provider,kind AS task_type,count(*) AS calls,sum(cost) AS cost,count(*) FILTER(WHERE cost IS NULL) AS unknown_cost_tasks,round(100.0*count(*) FILTER(WHERE status='failed')/NULLIF(count(*),0),2) AS failure_rate FROM filtered GROUP BY capability,provider_id,kind) `
	var total int64
	if err = tx.QueryRow(ctx, groups+"SELECT count(*) FROM groups", args...).Scan(&total); err != nil {
		return nil, err
	}
	pagedArgs := append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := adminJSONRows(ctx, tx, groups+fmt.Sprintf("SELECT to_jsonb(g) FROM groups g ORDER BY media_type,provider,task_type LIMIT $%d OFFSET $%d", len(pagedArgs)-1, len(pagedArgs)), pagedArgs...)
	if err != nil {
		return nil, err
	}
	result["rows"] = rows
	result["total"] = total
	result["page"] = f.Page
	result["page_size"] = f.PageSize
	result["total_pages"] = (total + int64(f.PageSize) - 1) / int64(f.PageSize)
	result["cost_policy"] = nil
	result["includes_deleted_tasks"] = true
	result["accounting_policy"] = map[string]any{"currency": "CNY", "source": "recorded_observations", "unknown_costs_excluded": true}
	return result, nil
}
