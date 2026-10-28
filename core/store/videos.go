package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/video"
)

type VideoTask struct {
	Task
	Progress       int
	SubmittedAt    *time.Time
	HiddenAt       *time.Time
	PendingResult  json.RawMessage
	UpstreamTaskID string
	RefundedAt     *time.Time
	UserAccount    string
	QueueAhead     int
}

const videoColumns = taskColumns + `,COALESCE(v.progress,0),v.submitted_at,v.hidden_at,v.pending_result,
 COALESCE((SELECT upstream_task_id FROM task_attempts a WHERE a.task_id=tasks.id AND a.attempt=tasks.attempt),''),
 (SELECT created_at FROM credit_ledger l WHERE l.user_id=tasks.user_id AND l.biz_id=tasks.id AND l.biz_type='generation_refund'),
 (SELECT email FROM users u WHERE u.id=tasks.user_id),
 (SELECT count(*) FROM tasks q WHERE q.capability='video' AND q.status='queued' AND q.deleted_at IS NULL
 AND q.request_json->'video'->>'provider'='agnes' AND (q.created_at,q.id)<(tasks.created_at,tasks.id))`
const videoFrom = ` FROM tasks LEFT JOIN video_task_state v ON v.task_id=tasks.id `

func scanVideo(row pgx.Row) (VideoTask, error) {
	var v VideoTask
	t := &v.Task
	err := row.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Capability, &t.Kind, &t.Title, &t.Status, &t.Request, &t.RequestHash, &t.IdempotencyKey, &t.CreditsCost, &t.Result, &t.ErrorCode, &t.ProviderScope, &t.ProviderID, &t.Model, &t.Attempt, &t.LeaseOwner, &t.LeaseID, &t.LeaseUntil, &t.NextRunAt, &t.CancelRequestedAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt,
		&v.Progress, &v.SubmittedAt, &v.HiddenAt, &v.PendingResult, &v.UpstreamTaskID, &v.RefundedAt, &v.UserAccount, &v.QueueAhead)
	return v, normalizeError(err)
}

func (s *Store) VideoTask(ctx context.Context, userID, id string) (VideoTask, error) {
	return scanVideo(s.Pool.QueryRow(ctx, "SELECT "+videoColumns+videoFrom+`WHERE tasks.id=$1 AND tasks.user_id=$2 AND tasks.capability='video' AND tasks.deleted_at IS NULL AND v.hidden_at IS NULL`, id, userID))
}

// Idempotency lookup deliberately includes hidden/deleted records. Hiding a task
// must never allow the same key to create another paid task.
func (s *Store) VideoTaskByKey(ctx context.Context, userID, key string) (VideoTask, error) {
	return scanVideo(s.Pool.QueryRow(ctx, "SELECT "+videoColumns+videoFrom+`WHERE tasks.user_id=$1 AND tasks.idempotency_key=$2 AND tasks.capability='video'`, userID, key))
}

type VideoTaskFilter struct {
	UserID, Keyword, Status, Provider string
	Admin                             bool
	Page, PageSize                    int
}

type VideoTaskPage struct {
	Rows       []VideoTask
	Total      int64
	Page       int
	PageSize   int
	TotalPages int64
}

func (s *Store) VideoTasks(ctx context.Context, f VideoTaskFilter) (VideoTaskPage, error) {
	page := VideoTaskPage{Rows: []VideoTask{}, Page: max(1, min(f.Page, 1000000)), PageSize: max(1, min(f.PageSize, 100))}
	if f.PageSize < 1 {
		page.PageSize = 20
	}
	if (!f.Admin && f.UserID == "") || len(f.Keyword) > 200 {
		return page, ErrConflict
	}
	where := ` WHERE tasks.capability='video' AND tasks.deleted_at IS NULL AND ($1 OR (tasks.user_id=$2 AND v.hidden_at IS NULL))
 AND ($3='' OR tasks.status=$3) AND ($4='' OR tasks.request_json->'video'->>'provider'=$4)
 AND ($5='' OR strpos(lower(tasks.id),lower($5))>0 OR strpos(lower(tasks.user_id),lower($5))>0 OR strpos(lower(tasks.title),lower($5))>0)`
	args := []any{f.Admin, f.UserID, f.Status, f.Provider, f.Keyword}
	if err := s.Pool.QueryRow(ctx, "SELECT count(*)"+videoFrom+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	args = append(args, page.PageSize, (page.Page-1)*page.PageSize)
	rows, err := s.Pool.Query(ctx, "SELECT "+videoColumns+videoFrom+where+` ORDER BY tasks.created_at DESC,tasks.id DESC LIMIT $6 OFFSET $7`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return page, err
		}
		page.Rows = append(page.Rows, v)
	}
	page.TotalPages = (page.Total + int64(page.PageSize) - 1) / int64(page.PageSize)
	return page, rows.Err()
}

func videoTerminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}

// DeleteVideoTask hides an owned terminal task. Administrative deletion is a
// separate soft delete and audit, with the role checked inside the transaction.
func (s *Store) DeleteVideoTask(ctx context.Context, actor, id string, admin bool, requestID string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if admin {
		var allowed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active' AND role IN ('admin','super_admin'))`, actor).Scan(&allowed); err != nil {
			return err
		}
		if s.Edition != config.Platform || !allowed {
			return ErrNotFound
		}
	}
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+` FROM tasks WHERE id=$1 AND capability='video' AND deleted_at IS NULL AND ($3 OR user_id=$2) FOR UPDATE`, id, actor, admin))
	if err != nil {
		return err
	}
	if !videoTerminal(t.Status) {
		return ErrTerminalConflict
	}
	if admin {
		if _, err = tx.Exec(ctx, `UPDATE tasks SET deleted_at=now(),updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,request_id,metadata) VALUES($1,$2,'video_task_delete','video_task',$3,$4,jsonb_build_object('status',$5::text))`, security.NewID("aud"), actor, id, requestID, t.Status); err != nil {
			return err
		}
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO video_task_state(task_id,hidden_at) VALUES($1,now()) ON CONFLICT(task_id) DO UPDATE SET hidden_at=COALESCE(video_task_state.hidden_at,now())`, id)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CancelVideoTask(ctx context.Context, userID, id string) (VideoTask, error) {
	if _, err := s.VideoTask(ctx, userID, id); err != nil {
		return VideoTask{}, err
	}
	if _, err := s.CancelTask(ctx, userID, id); err != nil {
		return VideoTask{}, err
	}
	return s.VideoTask(ctx, userID, id)
}

// DiscardVideoReferences removes only newly uploaded, unreferenced video inputs.
// Checking persisted requests also protects a submission whose commit reply was
// lost. The caller removes the returned storage keys after this DB operation.
func (s *Store) DiscardVideoReferences(ctx context.Context, userID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if userID == "" || len(ids) > 4 {
		return nil, ErrConflict
	}
	rows, err := s.Pool.Query(ctx, `DELETE FROM assets a WHERE a.id=ANY($2::text[]) AND a.user_id=$1
 AND a.image_type='video_reference' AND a.task_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM tasks t WHERE t.request_json @> jsonb_build_object('video',jsonb_build_object('references',jsonb_build_array(jsonb_build_object('asset_id',a.id)))))
 RETURNING storage_key`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

type VideoPollClaim struct {
	Task       Task
	Submission video.Submission
	Pending    *video.PollResult
	Provider   Provider `json:"-"`
}

type videoAttemptSnapshot struct {
	Provider   Provider `json:"provider"`
	OwnerScope string   `json:"owner_scope"`
	Ciphertext string   `json:"credential_ciphertext"`
}

// BeginVideoSubmission atomically persists the same paid boundary as
// BeginSubmission and the provider snapshot returned by ReserveAttempt. The
// encrypted credential remains bound to its original vault AAD (scope/id).
// Polling must not reload a rotated credential from the mutable provider row.
func (s *Store) BeginVideoSubmission(ctx context.Context, lease Task, a Attempt) error {
	p := a.Provider
	if lease.Capability != "video" || a.TaskID != lease.ID || p.Capability != "video" || p.ExecutionMode != "http" || p.Version < 1 || !p.Enabled || p.OwnerScope != a.ProviderScope || p.ID != a.ProviderID || p.Protocol != a.Protocol || p.Model != a.Model || videoFamily(p.Protocol, p.Model) == "" {
		return ErrConflict
	}
	expected := "platform"
	if s.Edition == config.Personal {
		expected = lease.UserID
	}
	if a.ProviderScope != expected {
		return ErrNotFound
	}
	snapshot, err := json.Marshal(videoAttemptSnapshot{Provider: p, OwnerScope: p.OwnerScope, Ciphertext: p.SecretCiphertext})
	if err != nil || len(snapshot) > 65536 {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockRunningTask(ctx, tx, lease, false); err != nil {
		return err
	}
	r, err := tx.Exec(ctx, `UPDATE task_attempts SET status='submitting',metadata=jsonb_set(metadata,'{video_provider}',$8::jsonb),updated_at=now()
 WHERE id=$1 AND task_id=$2 AND status='reserved' AND provider_scope=$3 AND provider_id=$4 AND protocol=$5 AND model=$6 AND attempt=$7`, a.ID, lease.ID, a.ProviderScope, a.ProviderID, a.Protocol, a.Model, a.Number, snapshot)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return ErrLeaseInvalid
	}
	return tx.Commit(ctx)
}

func videoFamily(protocol, model string) video.Provider {
	switch protocol {
	case "hfsy_video":
		return video.HFSY
	case "agnes_video":
		return video.Agnes
	case "apimart_video":
		if strings.HasPrefix(model, "gemini-") {
			return video.Gemini
		}
		if strings.HasPrefix(model, "doubao-seedance-") {
			return video.Seedance
		}
	}
	return ""
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

func callbackLock(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "video-callback/"+id)
	return err
}

// RecordVideoSubmission is called after BeginSubmission and one successful (or
// later reconciled) POST. It accepts a late acknowledgement for the same fenced
// attempt, including one already quarantined as uncertain; it never reposts.
func (s *Store) RecordVideoSubmission(ctx context.Context, lease Task, attemptID string, sub video.Submission, delay time.Duration) (Task, error) {
	if !videoIDPattern.MatchString(sub.UpstreamTaskID) || delay < 0 || delay > time.Hour {
		return Task{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	if err = callbackLock(ctx, tx, sub.UpstreamTaskID); err != nil {
		return Task{}, err
	}
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+` FROM tasks WHERE id=$1 AND capability='video' FOR UPDATE`, lease.ID))
	if err != nil {
		return Task{}, err
	}
	var a Attempt
	err = tx.QueryRow(ctx, `SELECT id,task_id,attempt,provider_scope,provider_id,protocol,model,status,COALESCE(upstream_task_id,'') FROM task_attempts WHERE id=$1 AND task_id=$2 AND attempt=$3`, attemptID, t.ID, t.Attempt).Scan(&a.ID, &a.TaskID, &a.Number, &a.ProviderScope, &a.ProviderID, &a.Protocol, &a.Model, &a.Status, &a.UpstreamTaskID)
	if err != nil {
		return Task{}, normalizeError(err)
	}
	if videoFamily(a.Protocol, a.Model) != sub.Provider || a.Model != sub.Model || a.ProviderScope != t.ProviderScope || a.ProviderID != t.ProviderID {
		return Task{}, ErrConflict
	}
	if a.UpstreamTaskID == sub.UpstreamTaskID && (t.Status == "polling" || videoTerminal(t.Status)) {
		return t, tx.Commit(ctx)
	}
	if a.UpstreamTaskID != "" || (a.Status != "submitting" && a.Status != "uncertain") ||
		(t.Status != "running" && t.Status != "uncertain") || t.LeaseID == "" || !security.ConstantEqual(t.LeaseID, lease.LeaseID) || t.LeaseOwner != lease.LeaseOwner {
		return Task{}, ErrLeaseInvalid
	}
	if _, err = tx.Exec(ctx, `UPDATE task_attempts SET upstream_task_id=$2,status='submitted',error_code='',updated_at=now() WHERE id=$1`, a.ID, sub.UpstreamTaskID); err != nil {
		return Task{}, normalizeError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO video_task_state(task_id,progress,submitted_at) VALUES($1,10,now()) ON CONFLICT(task_id) DO UPDATE SET progress=GREATEST(video_task_state.progress,10),submitted_at=COALESCE(video_task_state.submitted_at,now())`, t.ID); err != nil {
		return Task{}, err
	}
	t, err = scanTask(tx.QueryRow(ctx, `UPDATE tasks SET status='polling',error_code='',lease_owner=NULL,lease_id=NULL,lease_until=NULL,next_run_at=now()+$2*interval '1 second',updated_at=now() WHERE id=$1 RETURNING `+taskColumns, t.ID, int64(delay/time.Second)))
	if err != nil {
		return Task{}, err
	}
	var pending []byte
	err = tx.QueryRow(ctx, `SELECT result_json FROM video_callback_inbox WHERE upstream_task_id=$1 AND received_at>=now()-interval '48 hours'`, sub.UpstreamTaskID).Scan(&pending)
	if err == nil && a.Protocol == "apimart_video" {
		var matches int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM task_attempts WHERE protocol='apimart_video' AND upstream_task_id=$1`, sub.UpstreamTaskID).Scan(&matches); err != nil {
			return Task{}, err
		}
		if matches == 1 {
			var result video.PollResult
			if json.Unmarshal(pending, &result) != nil {
				return Task{}, ErrConflict
			}
			t, err = applyVideoResult(ctx, tx, t, result, 0)
			if err != nil {
				return Task{}, err
			}
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Task{}, err
	}
	return t, tx.Commit(ctx)
}

// ClaimVideoPoll reclaims expired polling leases without ever returning the task
// to the submission queue. The parent runner owns scheduling and renewal.
func (s *Store) ClaimVideoPoll(ctx context.Context, owner string, ttl time.Duration) (VideoPollClaim, error) {
	if owner == "" || ttl < 10*time.Second || ttl > 30*time.Minute {
		return VideoPollClaim{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return VideoPollClaim{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, `UPDATE tasks SET lease_owner=$1,lease_id=$2,lease_until=now()+$3*interval '1 second',updated_at=now()
 WHERE id=(SELECT id FROM tasks WHERE capability='video' AND status='polling' AND next_run_at<=now() AND (lease_until IS NULL OR lease_until<=now())
 AND EXISTS(SELECT 1 FROM task_attempts a WHERE a.task_id=tasks.id AND a.attempt=tasks.attempt AND a.status='submitted' AND a.upstream_task_id IS NOT NULL)
 ORDER BY next_run_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+taskColumns, owner, security.NewID("lease"), int64(ttl/time.Second)))
	if err != nil {
		return VideoPollClaim{}, err
	}
	claim, err := loadVideoPoll(ctx, tx, t)
	if err != nil {
		return VideoPollClaim{}, err
	}
	return claim, tx.Commit(ctx)
}

// VideoPollState re-reads staged callback evidence under the current fence.
// A callback can win while a GET is in flight; download only this stored result.
func (s *Store) VideoPollState(ctx context.Context, lease Task) (VideoPollClaim, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return VideoPollClaim{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := lockVideoPoll(ctx, tx, lease)
	if err != nil {
		return VideoPollClaim{}, err
	}
	claim, err := loadVideoPoll(ctx, tx, t)
	if err != nil {
		return VideoPollClaim{}, err
	}
	return claim, tx.Commit(ctx)
}

func loadVideoPoll(ctx context.Context, tx pgx.Tx, t Task) (VideoPollClaim, error) {
	claim := VideoPollClaim{Task: t}
	var protocol string
	var pending, snapshotJSON []byte
	err := tx.QueryRow(ctx, `SELECT a.upstream_task_id,a.protocol,a.model,v.pending_result,a.metadata->'video_provider' FROM task_attempts a LEFT JOIN video_task_state v ON v.task_id=a.task_id WHERE a.task_id=$1 AND a.attempt=$2`, t.ID, t.Attempt).Scan(&claim.Submission.UpstreamTaskID, &protocol, &claim.Submission.Model, &pending, &snapshotJSON)
	if err != nil {
		return VideoPollClaim{}, err
	}
	claim.Submission.Provider = videoFamily(protocol, claim.Submission.Model)
	if len(snapshotJSON) > 0 {
		var snapshot videoAttemptSnapshot
		if json.Unmarshal(snapshotJSON, &snapshot) != nil || snapshot.Provider.ID != t.ProviderID || snapshot.OwnerScope != t.ProviderScope || snapshot.Provider.Model != claim.Submission.Model || snapshot.Provider.Protocol != protocol {
			return VideoPollClaim{}, ErrConflict
		}
		claim.Provider = snapshot.Provider
		claim.Provider.OwnerScope = snapshot.OwnerScope
		claim.Provider.SecretCiphertext = snapshot.Ciphertext
		claim.Provider.HasAPIKey = snapshot.Ciphertext != ""
	}
	if len(pending) > 0 {
		claim.Pending = new(video.PollResult)
		if json.Unmarshal(pending, claim.Pending) != nil {
			return VideoPollClaim{}, ErrConflict
		}
	}
	return claim, nil
}

func lockVideoPoll(ctx context.Context, tx pgx.Tx, lease Task) (Task, error) {
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+` FROM tasks WHERE id=$1 AND capability='video' AND status='polling' AND lease_owner=$2 AND lease_id=$3 AND lease_until>clock_timestamp() FOR UPDATE`, lease.ID, lease.LeaseOwner, lease.LeaseID))
	if errors.Is(err, ErrNotFound) {
		err = ErrLeaseInvalid
	}
	return t, err
}

func (s *Store) ReleaseVideoPoll(ctx context.Context, lease Task, delay time.Duration) error {
	if delay < 0 || delay > time.Hour {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = lockVideoPoll(ctx, tx, lease); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE tasks SET lease_owner=NULL,lease_id=NULL,lease_until=NULL,
 next_run_at=now()+$2*interval '1 second',updated_at=now() WHERE id=$1`, lease.ID, int64(delay/time.Second))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VideoResultAsset permits restart recovery between storing a result and
// completing its task. The pending completion is immutable, so a previously
// verified generated-video asset for this task can be reused without fetching.
func (s *Store) VideoResultAsset(ctx context.Context, lease Task) (Asset, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := lockVideoPoll(ctx, tx, lease)
	if err != nil {
		return Asset{}, err
	}
	a, err := scanAsset(tx.QueryRow(ctx, `SELECT `+assetColumns+` FROM assets WHERE task_id=$1 AND user_id=$2
 AND image_type='generated-video' AND content_type IN ('video/mp4','video/webm') AND deleted_at IS NULL
 AND EXISTS(SELECT 1 FROM video_task_state v WHERE v.task_id=$1 AND v.pending_result->>'status'='completed')
 ORDER BY created_at DESC,id DESC LIMIT 1`, t.ID, t.UserID))
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}

var videoNumber = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,18})?([eE][+-]?[0-9]{1,2})?$`)

func safeVideoResult(in video.PollResult) (video.PollResult, error) {
	out := video.PollResult{Status: in.Status, Progress: max(10, min(in.Progress, 95)), Metadata: map[string]string{}}
	if in.Status != video.Completed && in.Status != video.Failed && in.Status != video.Submitted && in.Status != video.Generating {
		return out, ErrConflict
	}
	if in.Status == video.Failed {
		out.Progress, out.Error = 100, "upstream video generation failed"
	}
	if in.Status == video.Completed {
		u, err := url.Parse(in.ResultURL)
		if err != nil || len(in.ResultURL) > 8192 || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return out, ErrConflict
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return out, ErrConflict
		}
		for k := range q {
			switch strings.ToLower(k) {
			case "api_key", "apikey", "authorization", "prompt", "negative_prompt", "password", "secret", "access_token":
				return out, ErrConflict
			}
		}
		out.ResultURL, out.Progress = in.ResultURL, 100
		if in.Width != nil && *in.Width > 0 && *in.Width <= 65536 {
			n := *in.Width
			out.Width = &n
		}
		if in.Height != nil && *in.Height > 0 && *in.Height <= 65536 {
			n := *in.Height
			out.Height = &n
		}
		if videoNumber.MatchString(in.Seconds) {
			out.Seconds = in.Seconds
		}
	}
	for key, value := range in.Metadata {
		switch key {
		case "actual_cost", "upstream_cost", "cost_conversion_rate", "upstream_credits_cost", "upstream_actual_time_seconds", "upstream_completed_epoch", "upstream_result_expires_at", "upstream_quota", "mapped_width", "mapped_height", "seconds":
			if videoNumber.MatchString(value) {
				out.Metadata[key] = value
			}
		case "upstream_cost_unit":
			if value == "USD" || value == "APIMART_CREDIT" || value == "CNY_PER_CALL" {
				out.Metadata[key] = value
			}
		case "cost_source":
			if value == "upstream_cost" || value == "upstream_credits_cost" || value == "hfsy_public_fixed_price" {
				out.Metadata[key] = value
			}
		}
	}
	return out, nil
}

// ApplyVideoPoll stages completed results for verified storage. Definitive
// failures settle/refund atomically; a transport error must use ReleaseVideoPoll.
func (s *Store) ApplyVideoPoll(ctx context.Context, lease Task, result video.PollResult, delay time.Duration) (Task, error) {
	result, err := safeVideoResult(result)
	if err != nil || delay < 0 || delay > time.Hour {
		return Task{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := lockVideoPoll(ctx, tx, lease)
	if err != nil {
		return Task{}, err
	}
	t, err = applyVideoResult(ctx, tx, t, result, delay)
	if err != nil {
		return Task{}, err
	}
	return t, tx.Commit(ctx)
}

func applyVideoResult(ctx context.Context, tx pgx.Tx, t Task, result video.PollResult, delay time.Duration) (Task, error) {
	if videoTerminal(t.Status) {
		return t, nil
	}
	if t.Status != "polling" {
		return Task{}, ErrConflict
	}
	var existing []byte
	if err := tx.QueryRow(ctx, `SELECT pending_result FROM video_task_state WHERE task_id=$1`, t.ID).Scan(&existing); err != nil {
		return Task{}, err
	}
	var prior video.PollResult
	_ = json.Unmarshal(existing, &prior)
	if prior.Status == video.Completed {
		return t, nil // First terminal evidence wins, including before publication.
	}
	data, _ := json.Marshal(result)
	progress := min(result.Progress, 95)
	if result.Status == video.Failed {
		if err := refundTask(ctx, tx, t); err != nil {
			return Task{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE task_attempts SET status='failed',error_code='VIDEO_GENERATION_FAILED',retryable=false,updated_at=now() WHERE task_id=$1 AND attempt=$2 AND status='submitted'`, t.ID, t.Attempt); err != nil {
			return Task{}, err
		}
		progress = 100
		if _, err := tx.Exec(ctx, `UPDATE video_task_state SET progress=100,pending_result=$2 WHERE task_id=$1`, t.ID, data); err != nil {
			return Task{}, err
		}
		return scanTask(tx.QueryRow(ctx, `UPDATE tasks SET status='failed',error_code='VIDEO_GENERATION_FAILED',result_json=$2,completed_at=now(),updated_at=now() WHERE id=$1 RETURNING `+taskColumns, t.ID, data))
	}
	var pending any
	if result.Status == video.Completed {
		pending, delay = data, 0
	}
	if _, err := tx.Exec(ctx, `UPDATE video_task_state SET progress=GREATEST(progress,$2),pending_result=COALESCE($3::jsonb,pending_result) WHERE task_id=$1`, t.ID, progress, pending); err != nil {
		return Task{}, err
	}
	return scanTask(tx.QueryRow(ctx, `UPDATE tasks SET next_run_at=now()+$2*interval '1 second',updated_at=now() WHERE id=$1 RETURNING `+taskColumns, t.ID, int64(delay/time.Second)))
}

// CompleteVideoTask publishes only an owned, persisted video asset. The parent
// downloads/validates/saves it before this call. A repeated same-asset completion
// is idempotent; callbacks cannot reverse terminal settlement or re-charge.
func (s *Store) CompleteVideoTask(ctx context.Context, lease Task, assetID string) (Task, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(context.Background())
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+` FROM tasks WHERE id=$1 AND capability='video' FOR UPDATE`, lease.ID))
	if err != nil {
		return Task{}, err
	}
	var stored struct {
		AssetID string `json:"asset_id"`
	}
	_ = json.Unmarshal(t.Result, &stored)
	if t.Status == "succeeded" && stored.AssetID == assetID && t.LeaseOwner == lease.LeaseOwner && security.ConstantEqual(t.LeaseID, lease.LeaseID) {
		return t, tx.Commit(ctx)
	}
	if _, err = lockVideoPoll(ctx, tx, lease); err != nil {
		return Task{}, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id=$1 AND task_id=$2 AND user_id=$3 AND deleted_at IS NULL AND content_type LIKE 'video/%')`, assetID, t.ID, t.UserID).Scan(&valid); err != nil || !valid {
		return Task{}, ErrNotFound
	}
	var pending []byte
	if err = tx.QueryRow(ctx, `SELECT pending_result FROM video_task_state WHERE task_id=$1`, t.ID).Scan(&pending); err != nil {
		return Task{}, err
	}
	var result map[string]any
	if json.Unmarshal(pending, &result) != nil || result["status"] != "completed" {
		return Task{}, ErrConflict
	}
	delete(result, "result_url")
	result["asset_id"] = assetID
	data, _ := json.Marshal(result)
	if _, err = tx.Exec(ctx, `UPDATE task_attempts SET status='succeeded',updated_at=now() WHERE task_id=$1 AND attempt=$2 AND status='submitted'`, t.ID, t.Attempt); err != nil {
		return Task{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE video_task_state SET progress=100,pending_result=NULL WHERE task_id=$1`, t.ID); err != nil {
		return Task{}, err
	}
	t, err = scanTask(tx.QueryRow(ctx, `UPDATE tasks SET status='succeeded',result_json=$2,error_code='',completed_at=now(),updated_at=now() WHERE id=$1 RETURNING `+taskColumns, t.ID, data))
	if err != nil {
		return Task{}, err
	}
	return t, tx.Commit(ctx)
}

// ReconcileVideoCallback accepts only already-authenticated APIMart callbacks.
// Unknown IDs are retained for the acknowledgement race. IDs matching multiple
// accounts fail closed; a shared webhook secret cannot disambiguate accounts.
func (s *Store) ReconcileVideoCallback(ctx context.Context, callback video.Callback) (bool, error) {
	if !videoIDPattern.MatchString(callback.UpstreamTaskID) {
		return false, ErrConflict
	}
	result, err := safeVideoResult(callback.Result)
	if err != nil {
		return false, err
	}
	data, _ := json.Marshal(result)
	if len(data) > 16384 {
		return false, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	if err = callbackLock(ctx, tx, callback.UpstreamTaskID); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT t.id FROM tasks t JOIN task_attempts a ON a.task_id=t.id AND a.attempt=t.attempt WHERE t.capability='video' AND a.protocol='apimart_video' AND a.upstream_task_id=$1`, callback.UpstreamTaskID)
	if err != nil {
		return false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(ids) > 1 {
		return false, ErrConflict
	}
	if len(ids) == 1 {
		t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskColumns+` FROM tasks WHERE id=$1 FOR UPDATE`, ids[0]))
		if err != nil {
			return false, err
		}
		if videoTerminal(t.Status) {
			return false, tx.Commit(ctx)
		}
		if _, err = applyVideoResult(ctx, tx, t, result, 0); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	// Serialize capacity decisions too, to keep the early-callback inbox bounded.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(731804921)`); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM video_callback_inbox WHERE received_at<now()-interval '48 hours'`); err != nil {
		return false, err
	}
	var full bool
	if err = tx.QueryRow(ctx, `SELECT count(*)>=10000 AND NOT EXISTS(SELECT 1 FROM video_callback_inbox WHERE upstream_task_id=$1) FROM video_callback_inbox`, callback.UpstreamTaskID).Scan(&full); err != nil {
		return false, err
	}
	if full {
		return false, ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO video_callback_inbox(upstream_task_id,result_json) VALUES($1,$2)
 ON CONFLICT(upstream_task_id) DO UPDATE SET result_json=EXCLUDED.result_json,received_at=now()
 WHERE video_callback_inbox.result_json->>'status' NOT IN ('completed','failed')`, callback.UpstreamTaskID, data)
	if err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

type VideoProviderUpdate struct {
	Provider   Provider
	Ciphertext *string
}

// SaveVideoConfiguration atomically updates the nonsecret catalog and managed
// provider rows. Keys must already be encrypted with the provider's vault AAD.
func (s *Store) SaveVideoConfiguration(ctx context.Context, actor string, value json.RawMessage, updates []VideoProviderUpdate) error {
	return s.saveVideoConfiguration(ctx, actor, nil, value, updates)
}

// SaveVideoConfigurationCAS rejects a stale partial-edit snapshot. expected is
// the unmodified value returned by Setting, including {} before first setup.
func (s *Store) SaveVideoConfigurationCAS(ctx context.Context, actor string, expected, value json.RawMessage, updates []VideoProviderUpdate) error {
	if !json.Valid(expected) {
		return ErrConflict
	}
	return s.saveVideoConfiguration(ctx, actor, expected, value, updates)
}

func (s *Store) saveVideoConfiguration(ctx context.Context, actor string, expected, value json.RawMessage, updates []VideoProviderUpdate) error {
	if s.Edition != config.Platform || !json.Valid(value) || len(value) > 65536 {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(731804922)`); err != nil {
		return err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active' AND role IN ('admin','super_admin'))`, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrNotFound
	}
	if expected != nil {
		var unchanged bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT value=$1::jsonb FROM app_settings WHERE key='video_runtime_config'),$1::jsonb='{}'::jsonb)`, []byte(expected)).Scan(&unchanged); err != nil {
			return err
		}
		if !unchanged {
			return ErrConflict
		}
	}
	for _, update := range updates {
		p := update.Provider
		if p.OwnerScope != "platform" || p.Capability != "video" || !strings.HasPrefix(p.ID, "video-") {
			return ErrConflict
		}
		var prior string
		err = tx.QueryRow(ctx, `SELECT secret_ciphertext FROM providers WHERE owner_scope='platform' AND id=$1 FOR UPDATE`, p.ID).Scan(&prior)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if update.Ciphertext != nil && *update.Ciphertext != prior {
			var busy bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_attempts WHERE provider_scope='platform' AND provider_id=$1 AND status IN ('reserved','submitting','submitted','uncertain'))`, p.ID).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return ErrConflict
			}
		}
		var cipher any
		if update.Ciphertext != nil {
			cipher = *update.Ciphertext
		}
		_, err = tx.Exec(ctx, `INSERT INTO providers(owner_scope,id,name,capability,protocol,profile,base_url,secret_ciphertext,model,enabled,max_concurrency,settings)
 VALUES('platform',$1,$2,'video',$3,$4,$5,COALESCE($6,''),$7,$8,$9,'{}') ON CONFLICT(owner_scope,id) DO UPDATE SET
 name=EXCLUDED.name,model=EXCLUDED.model,enabled=EXCLUDED.enabled,max_concurrency=EXCLUDED.max_concurrency,secret_ciphertext=COALESCE($6,providers.secret_ciphertext),version=providers.version+1,updated_at=now()`, p.ID, p.Name, p.Protocol, p.Profile, p.BaseURL, cipher, p.Model, p.Enabled, p.MaxConcurrency)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM provider_health WHERE owner_scope='platform' AND provider_id=$1`, p.ID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES('video_runtime_config',$1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,version=app_settings.version+1,updated_by=$2,updated_at=now()`, []byte(value), actor); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id) VALUES($1,$2,'video_config_update','video_config','video_runtime_config')`, security.NewID("aud"), actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type VideoKeyMetric struct {
	Active, Today, Succeeded, Failed int64
}

func (s *Store) VideoKeyMetrics(ctx context.Context, scope string) (map[string]VideoKeyMetric, error) {
	rows, err := s.Pool.Query(ctx, `SELECT provider_id,
 count(*) FILTER(WHERE status IN ('reserved','submitting','submitted','uncertain')),
 count(*) FILTER(WHERE created_at>=date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),
 count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status='failed')
 FROM task_attempts WHERE provider_scope=$1 AND protocol='agnes_video' GROUP BY provider_id`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics := map[string]VideoKeyMetric{}
	for rows.Next() {
		var id string
		var m VideoKeyMetric
		if err := rows.Scan(&id, &m.Active, &m.Today, &m.Succeeded, &m.Failed); err != nil {
			return nil, err
		}
		metrics[id] = m
	}
	return metrics, rows.Err()
}

func (s *Store) VideoMetrics(ctx context.Context) (map[string]any, error) {
	var summary, models []byte
	err := s.Pool.QueryRow(ctx, `WITH v AS (SELECT t.*,s.submitted_at,
 COALESCE((t.result_json->'metadata'->>'actual_cost')::numeric,0) AS actual_cost,
 EXISTS(SELECT 1 FROM credit_ledger l WHERE l.biz_id=t.id AND l.biz_type='generation_refund') AS refunded
 FROM tasks t LEFT JOIN video_task_state s ON s.task_id=t.id WHERE t.capability='video' AND t.deleted_at IS NULL AND t.created_at>=now()-interval '30 days')
 SELECT jsonb_build_object('agnes_queued',count(*) FILTER(WHERE request_json->'video'->>'provider'='agnes' AND status='queued'),
 'agnes_active',count(*) FILTER(WHERE request_json->'video'->>'provider'='agnes' AND status IN ('running','polling','uncertain')),
 'agnes_fallback_gemini',0,'average_queue_seconds',COALESCE(round(avg(extract(epoch FROM (submitted_at-created_at)))::numeric,1),0),
 'average_generation_seconds',COALESCE(round(avg(extract(epoch FROM (completed_at-submitted_at)))::numeric,1),0),
 'daily_total_cost',COALESCE(sum(actual_cost) FILTER(WHERE created_at>=date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),0),
 'credits_income',COALESCE(sum(credits_cost) FILTER(WHERE NOT refunded),0),'upstream_total_cost',COALESCE(sum(actual_cost),0),
 'income_cost_difference',COALESCE(sum(credits_cost) FILTER(WHERE NOT refunded),0)-COALESCE(sum(actual_cost),0)) FROM v`).Scan(&summary)
	if err != nil {
		return nil, err
	}
	err = s.Pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT t.request_json->'video'->>'provider' AS provider,a.model,
 count(*) FILTER(WHERE a.upstream_task_id IS NOT NULL) AS call_count,count(*) FILTER(WHERE t.status='succeeded') AS success_count,
 count(*) FILTER(WHERE a.status='failed') AS failure_count,
 COALESCE(round(avg(extract(epoch FROM(v.submitted_at-t.created_at)))::numeric,1),0) AS average_queue_seconds,
 COALESCE(round(avg(extract(epoch FROM(t.completed_at-v.submitted_at)))::numeric,1),0) AS average_generation_seconds,
 COALESCE(avg((t.result_json->'metadata'->>'actual_cost')::numeric),0) AS average_cost,
 COALESCE(sum((t.result_json->'metadata'->>'actual_cost')::numeric),0) AS total_cost
 FROM tasks t JOIN task_attempts a ON a.task_id=t.id LEFT JOIN video_task_state v ON v.task_id=t.id
 WHERE t.capability='video' AND t.deleted_at IS NULL AND t.created_at>=now()-interval '30 days'
 GROUP BY t.request_json->'video'->>'provider',a.model ORDER BY call_count DESC,provider) x`).Scan(&models)
	if err != nil {
		return nil, err
	}
	return map[string]any{"summary": json.RawMessage(summary), "models": json.RawMessage(models)}, nil
}
