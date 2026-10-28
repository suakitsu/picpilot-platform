package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/storage"
	wp "github.com/suakitsu/picpilot-platform/core/workerprotocol"
)

// WorkerOptions contains server configuration, never worker-supplied values.
type WorkerOptions struct {
	PublicBaseURL   string
	SigningKey      []byte
	GraceSeconds    int
	InputTTLSeconds int
}

// Empty/absent allowlists are unrestricted. Invalid stored configuration fails
// closed instead of silently broadening which machines may receive a payload.
func workerAllowedIDs(settings json.RawMessage) ([]string, error) {
	var values map[string]json.RawMessage
	if len(settings) == 0 {
		return nil, nil
	}
	if json.Unmarshal(settings, &values) != nil || values == nil {
		return nil, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	raw, ok := values["worker_ids"]
	if !ok {
		return nil, nil
	}
	var ids []string
	if json.Unmarshal(raw, &ids) != nil || ids == nil {
		return nil, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if wp.ValidateWorkerID(id) != nil || seen[id] {
			return nil, wp.NewError("REQUEST_SCHEMA_INVALID")
		}
		seen[id] = true
	}
	return ids, nil
}
func workerProviderProtocolMatches(p Provider, cap wp.Capability, protocol wp.Protocol) bool {
	if p.Capability != string(cap) {
		return false
	}
	if cap == wp.Image {
		return (p.Protocol == string(wp.ImageGenerations) || p.Protocol == string(wp.ImageEdits)) && (protocol == wp.ImageGenerations || protocol == wp.ImageEdits)
	}
	return p.Protocol == string(protocol)
}
func workerProviderProblem(p Provider, j wp.Job) string {
	if !p.Enabled || p.ExecutionMode != "pull_worker" {
		return "WORKER_PROVIDER_DISABLED"
	}
	if p.Model != j.Model || !workerProviderProtocolMatches(p, j.Capability, j.Protocol) {
		return "WORKER_PROVIDER_CHANGED"
	}
	if _, err := workerAllowedIDs(p.Settings); err != nil {
		return "WORKER_PROVIDER_CHANGED"
	}
	return ""
}

func (o WorkerOptions) policy(seconds int) wp.LeasePolicy {
	grace := o.GraceSeconds
	if grace == 0 {
		grace = 15
	}
	return wp.LeasePolicy{LeaseSeconds: seconds, GraceSeconds: grace}
}

// WorkerTextInput identifies one structured image URL in an already normalized
// text payload. JSONPointer is an RFC 6901 pointer, not an arbitrary search string.
// The URL is replaced by an asset placeholder in storage and refreshed on claim.
type WorkerTextInput struct {
	AssetID     string `json:"assetId"`
	JSONPointer string `json:"jsonPointer"`
}
type EnqueueWorkerJobInput struct {
	TaskID, ProviderScope, ProviderID string
	Capability                        wp.Capability
	Protocol                          wp.Protocol
	Model                             string
	Request                           json.RawMessage
	InputAssetIDs                     []string
	TextInputs                        []WorkerTextInput
}
type WorkerInputSource struct {
	AssetID     string `json:"assetId"`
	SourceURL   string `json:"sourceUrl"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	FileSize    int64  `json:"fileSize"`
	JSONPointer string `json:"jsonPointer,omitempty"`
}

// WorkerJob is an internal runner snapshot. Never serialize it on worker routes.
type WorkerJob struct {
	Job                                      wp.Job
	TaskID, UserID, ProjectID, ProviderScope string
	InputSources                             []WorkerInputSource
}

const workerJobColumns = `j.job_id,j.task_id,j.provider_scope,j.provider_id,j.capability,j.protocol,j.model,j.request_json,j.input_sources,j.offline_action,j.status,COALESCE(j.worker_id,''),COALESCE(j.lease_hash,''),COALESCE(j.previous_lease_hash,''),j.lease_until,j.renew_after,j.grace_until,j.attempt,j.queued_at,j.leased_at,j.completed_at,j.completion_key,j.completion_hash,j.result_json,j.error_code,j.error_message,t.user_id,COALESCE(t.project_id,'')`

func scanWorkerJob(row pgx.Row) (WorkerJob, error) {
	var w WorkerJob
	var input []byte
	var lease, renew, grace, leased, done *time.Time
	j := &w.Job
	err := row.Scan(&j.JobID, &w.TaskID, &w.ProviderScope, &j.ProviderID, &j.Capability, &j.Protocol, &j.Model, &j.Request, &input, &j.OfflineAction, &j.Status, &j.WorkerID, &j.LeaseHash, &j.PreviousLeaseHash, &lease, &renew, &grace, &j.Attempt, &j.QueuedAt, &leased, &done, &j.CompletionKey, &j.CompletionHash, &j.ResultJSON, &j.ErrorCode, &j.ErrorMessage, &w.UserID, &w.ProjectID)
	if err != nil {
		return w, normalizeError(err)
	}
	for _, p := range []struct {
		dst *time.Time
		src *time.Time
	}{{&j.LeaseUntil, lease}, {&j.RenewAfter, renew}, {&j.GraceUntil, grace}, {&j.LeasedAt, leased}, {&j.CompletedAt, done}} {
		if p.src != nil {
			*p.dst = *p.src
		}
	}
	if err = json.Unmarshal(input, &w.InputSources); err != nil {
		return w, wp.NewError("WORKER_INTERNAL_ERROR")
	}
	return w, nil
}
func workerTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func saveWorkerJob(ctx context.Context, tx pgx.Tx, j wp.Job) error {
	result := j.ResultJSON
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `UPDATE worker_jobs SET status=$2,worker_id=NULLIF($3,''),lease_hash=NULLIF($4,''),previous_lease_hash=NULLIF($5,''),lease_until=$6,renew_after=$7,grace_until=$8,attempt=$9,leased_at=$10,completed_at=$11,completion_key=$12,completion_hash=$13,result_json=$14,error_code=$15,error_message=$16,updated_at=clock_timestamp() WHERE job_id=$1`, j.JobID, j.Status, j.WorkerID, j.LeaseHash, j.PreviousLeaseHash, workerTime(j.LeaseUntil), workerTime(j.RenewAfter), workerTime(j.GraceUntil), j.Attempt, workerTime(j.LeasedAt), workerTime(j.CompletedAt), j.CompletionKey, j.CompletionHash, []byte(result), j.ErrorCode, j.ErrorMessage)
	return err
}
func workerBusinessActive(ctx context.Context, tx pgx.Tx, taskID string) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx, `SELECT status IN ('queued','running','waiting_worker','polling') AND cancel_requested_at IS NULL AND deleted_at IS NULL FROM tasks WHERE id=$1 FOR SHARE`, taskID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return active, err
}

func workerOrigin(raw string) (string, error) {
	if err := wp.ValidatePublicHTTPSURL(raw); err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", wp.NewError("INPUT_URL_INVALID")
	}
	return strings.TrimRight(raw, "/"), nil
}

// rewriteWorkerText changes only the two supported image URL structures. It
// retains JSON numbers exactly, and cannot modify prose or credentials by match.
func rewriteWorkerText(raw json.RawMessage, inputs []WorkerInputSource, value func(WorkerInputSource) string) (json.RawMessage, error) {
	if len(inputs) == 0 {
		return bytes.Clone(raw), nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var root any
	if d.Decode(&root) != nil {
		return nil, wp.NewError("PAYLOAD_INVALID")
	}
	seen := map[string]bool{}
	for _, ref := range inputs {
		if ref.JSONPointer == "" || !strings.HasPrefix(ref.JSONPointer, "/") || seen[ref.JSONPointer] {
			return nil, wp.NewError("PAYLOAD_INVALID")
		}
		seen[ref.JSONPointer] = true
		parts := strings.Split(ref.JSONPointer[1:], "/")
		for i, p := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		}
		node := root
		var parent map[string]any
		for _, p := range parts[:len(parts)-1] {
			switch v := node.(type) {
			case map[string]any:
				parent = v
				node = v[p]
			case []any:
				n, e := strconv.Atoi(p)
				if e != nil || n < 0 || n >= len(v) {
					return nil, wp.NewError("PAYLOAD_INVALID")
				}
				node = v[n]
				parent = nil
			default:
				return nil, wp.NewError("PAYLOAD_INVALID")
			}
		}
		object, ok := node.(map[string]any)
		if !ok {
			return nil, wp.NewError("PAYLOAD_INVALID")
		}
		key := parts[len(parts)-1]
		chat := key == "url" && len(parts) > 1 && parts[len(parts)-2] == "image_url" && parent != nil && parent["type"] == "image_url"
		responses := key == "image_url" && object["type"] == "input_image"
		if !chat && !responses {
			return nil, wp.NewError("PAYLOAD_INVALID")
		}
		old, ok := object[key].(string)
		if !ok || old == "" {
			return nil, wp.NewError("PAYLOAD_INVALID")
		}
		object[key] = value(ref)
	}
	b, err := json.Marshal(root)
	if err != nil {
		return nil, wp.NewError("PAYLOAD_INVALID")
	}
	return b, nil
}

// EnqueueWorkerJob accepts ONLY a normalized protocol request. It does not parse
// core/jobs.Payload, reserve credits, settle business tasks, or call providers.
func (s *Store) EnqueueWorkerJob(ctx context.Context, in EnqueueWorkerJobInput, options WorkerOptions) (WorkerJob, bool, error) {
	if !s.validWorkerScope(in.ProviderScope) || in.TaskID == "" || in.ProviderID == "" {
		return WorkerJob{}, false, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	if err := wp.ValidatePayload(in.Capability, in.Protocol, in.Model, in.Request); err != nil {
		return WorkerJob{}, false, err
	}
	if len(in.InputAssetIDs) > 10 || len(in.TextInputs) > 10 {
		return WorkerJob{}, false, wp.NewError("INPUT_FILES_TOO_MANY")
	}
	if in.Protocol == wp.ImageEdits && len(in.InputAssetIDs) == 0 {
		return WorkerJob{}, false, wp.NewError("INPUT_FILES_REQUIRED")
	}
	if in.Protocol != wp.ImageEdits && len(in.InputAssetIDs) > 0 || in.Capability != wp.Text && len(in.TextInputs) > 0 {
		return WorkerJob{}, false, wp.NewError("INPUT_FILES_NOT_ALLOWED")
	}
	origin := ""
	var err error
	if len(in.InputAssetIDs)+len(in.TextInputs) > 0 {
		origin, err = workerOrigin(options.PublicBaseURL)
		if err != nil {
			return WorkerJob{}, false, err
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return WorkerJob{}, false, err
	}
	defer tx.Rollback(context.Background())
	p, err := scanProvider(tx.QueryRow(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 AND id=$2 FOR SHARE", in.ProviderScope, in.ProviderID))
	if err != nil {
		return WorkerJob{}, false, err
	}
	if !p.Enabled || p.ExecutionMode != "pull_worker" {
		return WorkerJob{}, false, ErrDisabled
	}
	if !workerProviderProtocolMatches(p, in.Capability, in.Protocol) || p.Model != in.Model {
		return WorkerJob{}, false, wp.NewError("PROTOCOL_CAPABILITY_MISMATCH")
	}
	if _, err := workerAllowedIDs(p.Settings); err != nil {
		return WorkerJob{}, false, err
	}
	if p.MaxConcurrency < 1 || p.MaxConcurrency > 32 || options.policy(p.LeaseSeconds).Validate() != nil {
		return WorkerJob{}, false, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	var user, project, capability string
	var active bool
	err = tx.QueryRow(ctx, `SELECT user_id,COALESCE(project_id,''),capability,status IN ('queued','running','waiting_worker','polling') AND cancel_requested_at IS NULL AND deleted_at IS NULL FROM tasks WHERE id=$1 FOR SHARE`, in.TaskID).Scan(&user, &project, &capability, &active)
	if err != nil {
		return WorkerJob{}, false, normalizeError(err)
	}
	if s.Edition == config.Personal && user != in.ProviderScope {
		return WorkerJob{}, false, ErrNotFound
	}
	if !active {
		return WorkerJob{}, false, wp.NewError("JOB_CANCELLED")
	}
	if capability != string(in.Capability) {
		return WorkerJob{}, false, wp.NewError("PROTOCOL_CAPABILITY_MISMATCH")
	}
	refs := []WorkerInputSource{}
	ids := append([]string{}, in.InputAssetIDs...)
	for _, r := range in.TextInputs {
		ids = append(ids, r.AssetID)
	}
	for i, id := range ids {
		a, err := scanAsset(tx.QueryRow(ctx, "SELECT "+assetColumns+" FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR SHARE", id, user))
		if err != nil {
			return WorkerJob{}, false, normalizeError(err)
		}
		if a.ContentType != "image/png" && a.ContentType != "image/jpeg" && a.ContentType != "image/webp" {
			return WorkerJob{}, false, wp.NewError("ARTIFACT_MIME_UNSUPPORTED")
		}
		ref := WorkerInputSource{AssetID: a.ID, SourceURL: origin + "/api/files/assets/" + url.PathEscape(a.ID), Filename: a.OriginalFilename, ContentType: a.ContentType, FileSize: a.FileSize}
		if i >= len(in.InputAssetIDs) {
			ref.JSONPointer = in.TextInputs[i-len(in.InputAssetIDs)].JSONPointer
		}
		refs = append(refs, ref)
	}
	request := bytes.Clone(in.Request)
	if len(in.TextInputs) > 0 {
		request, err = rewriteWorkerText(request, refs, func(r WorkerInputSource) string { return "picpilot-asset:" + r.AssetID })
		if err != nil {
			return WorkerJob{}, false, err
		}
	}
	canonical, _ := json.Marshal(map[string]any{"protocol": in.Protocol, "model": in.Model, "request": request, "inputs": refs})
	hash, err := wp.CanonicalHash(canonical)
	if err != nil {
		return WorkerJob{}, false, err
	}
	inputs, _ := json.Marshal(refs)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "worker-task:"+in.TaskID); err != nil {
		return WorkerJob{}, false, err
	}
	var pendingHash, pendingScope, pendingProvider string
	err = tx.QueryRow(ctx, `SELECT request_hash,provider_scope,provider_id FROM worker_jobs WHERE task_id=$1 AND status IN ('queued','leased','expired')`, in.TaskID).Scan(&pendingHash, &pendingScope, &pendingProvider)
	if err == nil && (pendingHash != hash || pendingScope != in.ProviderScope || pendingProvider != in.ProviderID) {
		return WorkerJob{}, false, ErrConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return WorkerJob{}, false, err
	}
	id := security.NewID("awj")
	tag, err := tx.Exec(ctx, `INSERT INTO worker_jobs(job_id,task_id,provider_scope,provider_id,capability,protocol,model,request_json,request_hash,input_sources,offline_action) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(task_id,provider_scope,provider_id,capability,request_hash) DO NOTHING`, id, in.TaskID, in.ProviderScope, in.ProviderID, in.Capability, in.Protocol, in.Model, []byte(request), hash, inputs, p.OfflineAction)
	if err != nil {
		return WorkerJob{}, false, err
	}
	w, err := scanWorkerJob(tx.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.task_id=$1 AND j.provider_scope=$2 AND j.provider_id=$3 AND j.capability=$4 AND j.request_hash=$5`, in.TaskID, in.ProviderScope, in.ProviderID, in.Capability, hash))
	if err != nil {
		return WorkerJob{}, false, err
	}
	return w, tag.RowsAffected() == 1, tx.Commit(ctx)
}

func prepareWorkerClaim(w WorkerJob, worker string, now time.Time, o WorkerOptions) (wp.Job, error) {
	j := w.Job
	j.InputFiles = []wp.InputFile{}
	if len(w.InputSources) == 0 {
		return j, nil
	}
	origin, err := workerOrigin(o.PublicBaseURL)
	if err != nil {
		return j, err
	}
	if len(o.SigningKey) < 32 {
		return j, wp.NewError("WORKER_INTERNAL_ERROR")
	}
	ttl := o.InputTTLSeconds
	if ttl == 0 {
		ttl = 300
	}
	if ttl < 60 || ttl > 3600 {
		return j, wp.NewError("REQUEST_SCHEMA_INVALID")
	}
	expires := now.Add(time.Duration(ttl) * time.Second).Unix()
	if j.Capability == wp.Text {
		j.Request, err = rewriteWorkerText(j.Request, w.InputSources, func(ref WorkerInputSource) string {
			q := url.Values{"expiresAt": {strconv.FormatInt(expires, 10)}, "signature": {storage.SignAsset(o.SigningKey, ref.AssetID, w.UserID, expires)}}
			return origin + "/api/files/assets/" + url.PathEscape(ref.AssetID) + "?" + q.Encode()
		})
		return j, err
	}
	for i, ref := range w.InputSources {
		sign, err := wp.InputSignature(o.SigningKey, j.JobID, i, worker, expires, ref.SourceURL)
		if err != nil {
			return j, err
		}
		q := url.Values{"workerId": {worker}, "expires": {strconv.FormatInt(expires, 10)}, "signature": {sign}}
		j.InputFiles = append(j.InputFiles, wp.InputFile{Index: i, URL: origin + wp.BasePath + "/inputs/" + j.JobID + "/" + strconv.Itoa(i) + "?" + q.Encode(), Filename: ref.Filename, ContentType: ref.ContentType, FileSize: ref.FileSize, URLExpiresAt: time.Unix(expires, 0).UTC(), URLChecksum: sign, URLSignatureAlgorithm: "HMAC-SHA256"})
	}
	return j, nil
}

// ClaimWorkerJob is one atomic poll. The HTTP adapter owns cancellable long polling.
func (s *Store) ClaimWorkerJob(ctx context.Context, p WorkerPrincipal, r wp.ClaimRequest, o WorkerOptions) (*wp.ClaimedTask, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.RecoverWorkerJobs(ctx, p.OwnerScope); err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if err = workerTokenLock(ctx, tx, p); err != nil {
		return nil, err
	}
	worker, err := workerBinding(ctx, tx, p, r.WorkerID, true)
	if err != nil {
		return nil, err
	}
	now, err := workerNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE worker_id=$1 AND status='leased' AND grace_until>$2`, r.WorkerID, now).Scan(&active); err != nil {
		return nil, err
	}
	if active >= worker.MaxConcurrency {
		return nil, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM providers WHERE owner_scope=$1 AND enabled AND execution_mode='pull_worker' ORDER BY priority,id`, p.OwnerScope)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "worker-provider:"+p.OwnerScope+":"+id); err != nil {
			return nil, err
		}
		provider, err := scanProvider(tx.QueryRow(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 AND id=$2 FOR SHARE", p.OwnerScope, id))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		cap := wp.Capability(provider.Capability)
		if !provider.Enabled || provider.ExecutionMode != "pull_worker" || !slices.Contains(worker.Capabilities, cap) || !slices.Contains(r.Capabilities, cap) {
			continue
		}
		ids, allowErr := workerAllowedIDs(provider.Settings)
		if allowErr != nil || len(ids) > 0 && !slices.Contains(ids, r.WorkerID) {
			continue
		}
		now, err = workerNow(ctx, tx)
		if err != nil {
			return nil, err
		}
		var busy int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM worker_jobs WHERE provider_scope=$1 AND provider_id=$2 AND status='leased' AND grace_until>$3`, p.OwnerScope, id, now).Scan(&busy); err != nil {
			return nil, err
		}
		if !(wp.Capacity{WorkerActive: active, ProviderActive: busy, WorkerMax: worker.MaxConcurrency, ProviderMax: provider.MaxConcurrency}).Available() {
			continue
		}
		w, err := scanWorkerJob(tx.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.provider_scope=$1 AND j.provider_id=$2 AND j.status='queued' AND j.capability=$3 AND (j.protocol=$4 OR (j.capability='image' AND j.protocol IN ('openai_images_generations','openai_images_edits') AND $4::text IN ('openai_images_generations','openai_images_edits'))) AND j.model=$5 AND t.status IN ('queued','running','waiting_worker','polling') AND t.cancel_requested_at IS NULL AND t.deleted_at IS NULL ORDER BY j.queued_at,j.job_id FOR UPDATE OF j SKIP LOCKED LIMIT 1`, p.OwnerScope, id, provider.Capability, provider.Protocol, provider.Model))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		live, err := workerBusinessActive(ctx, tx, w.TaskID)
		if err != nil {
			return nil, err
		}
		if !live {
			continue
		}
		now, err = workerNow(ctx, tx)
		if err != nil {
			return nil, err
		}
		prepared, err := prepareWorkerClaim(w, r.WorkerID, now, o)
		if err != nil {
			return nil, err
		}
		lease, err := security.RandomToken("awl_", 32)
		if err != nil {
			return nil, err
		}
		next, task, err := wp.Acquire(prepared, wp.LeaseRequest{WorkerID: r.WorkerID, LeaseID: lease}, o.SigningKey, now, o.policy(provider.LeaseSeconds))
		if err != nil {
			return nil, err
		}
		if err = saveWorkerJob(ctx, tx, next); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &task, nil
	}
	return nil, tx.Commit(ctx)
}

// RecoverWorkerJobs never settles business tasks or starts fallback work.
// It is safe to invoke from the parent's scheduler and before each claim.
func (s *Store) RecoverWorkerJobs(ctx context.Context, scope string) (int64, error) {
	if !s.validWorkerScope(scope) {
		return 0, ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.provider_scope=$1 AND j.status IN ('queued','leased','expired') AND (j.status='expired' OR j.grace_until<=clock_timestamp() OR t.status NOT IN ('queued','running','waiting_worker','polling') OR t.cancel_requested_at IS NOT NULL OR t.deleted_at IS NOT NULL) ORDER BY j.job_id FOR UPDATE OF j SKIP LOCKED LIMIT 100`, scope)
	if err != nil {
		return 0, err
	}
	jobs := []WorkerJob{}
	for rows.Next() {
		w, e := scanWorkerJob(rows)
		if e != nil {
			rows.Close()
			return 0, e
		}
		jobs = append(jobs, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var count int64
	for _, w := range jobs {
		live, err := workerBusinessActive(ctx, tx, w.TaskID)
		if err != nil {
			return 0, err
		}
		now, err := workerNow(ctx, tx)
		if err != nil {
			return 0, err
		}
		next := w.Job
		changed := false
		if !live {
			next, changed = wp.Cancel(next, now)
		} else {
			next, changed = wp.Expire(next, now)
			if next.Status == wp.Expired && next.OfflineAction == wp.Queue {
				var enabled bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM providers WHERE owner_scope=$1 AND id=$2 AND enabled AND execution_mode='pull_worker')`, scope, next.ProviderID).Scan(&enabled); err != nil {
					return 0, err
				}
				if enabled {
					var requeued bool
					next, requeued = wp.Requeue(next, now)
					changed = changed || requeued
				}
			}
		}
		if changed {
			if err = saveWorkerJob(ctx, tx, next); err != nil {
				return 0, err
			}
			count++
		}
	}
	return count, tx.Commit(ctx)
}

// WorkerJobForTask is a runner-only lookup with explicit business ownership.
func (s *Store) WorkerJobForTask(ctx context.Context, taskID, jobID string) (WorkerJob, error) {
	return scanWorkerJob(s.Pool.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.task_id=$1 AND j.job_id=$2`, taskID, jobID))
}

// LatestWorkerJobForTask lets the business runner resume before provider
// filtering. Disabling/changing a provider must not hide an outstanding lease
// or a successful result and accidentally authorize another submission.
func (s *Store) LatestWorkerJobForTask(ctx context.Context, taskID string) (WorkerJob, error) {
	return scanWorkerJob(s.Pool.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.task_id=$1 ORDER BY j.queued_at DESC,j.job_id DESC LIMIT 1`, taskID))
}

// ReconcileWorkerJob returns authoritative state and whether the next provider
// may be considered. Besides genuine failure/offline fallback, a never-claimed
// fallback-policy job may advance after its provider becomes unavailable.
// It never settles the task/ledger or authorizes fallback from a live lease.
func (s *Store) ReconcileWorkerJob(ctx context.Context, taskID, jobID string, localWaitSeconds int) (WorkerJob, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return WorkerJob{}, false, err
	}
	defer tx.Rollback(context.Background())
	w, err := scanWorkerJob(tx.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.task_id=$1 AND j.job_id=$2 FOR UPDATE OF j`, taskID, jobID))
	if err != nil {
		return w, false, err
	}
	live, err := workerBusinessActive(ctx, tx, taskID)
	if err != nil {
		return w, false, err
	}
	now, err := workerNow(ctx, tx)
	if err != nil {
		return w, false, err
	}
	next := w.Job
	if !live {
		next, _ = wp.Cancel(next, now)
	} else {
		next, _ = wp.Expire(next, now)
		if next.Status == wp.Queued || next.Status == wp.Expired && next.OfflineAction == wp.Queue {
			p, e := scanProvider(tx.QueryRow(ctx, "SELECT "+providerColumns+" FROM providers WHERE owner_scope=$1 AND id=$2", w.ProviderScope, next.ProviderID))
			problem := "WORKER_PROVIDER_REMOVED"
			if e == nil {
				problem = workerProviderProblem(p, next)
			} else if !errors.Is(e, ErrNotFound) {
				return w, false, e
			}
			if problem != "" && next.Status == wp.Queued {
				next, _ = wp.Cancel(next, now)
				next.ErrorCode = problem
				next.ErrorMessage = "Worker provider configuration is no longer available"
			} else if problem == "" && next.Status == wp.Expired {
				next, _ = wp.Requeue(next, now)
			}
		}
		next, _ = wp.OfflineFallback(next, now, localWaitSeconds)
	}
	if err = saveWorkerJob(ctx, tx, next); err != nil {
		return w, false, err
	}
	w.Job = next
	configFallback := next.Status == wp.Cancelled && strings.HasPrefix(next.ErrorCode, "WORKER_PROVIDER_") && next.Attempt == 0 && next.OfflineAction == wp.Fallback
	allowed := live && (next.Status == wp.Failed || next.Status == wp.Cancelled && next.ErrorCode == "LOCAL_WORKER_OFFLINE" || configFallback)
	if err = tx.Commit(ctx); err != nil {
		return w, false, err
	}
	return w, allowed, nil
}

// FallbackWorkerJob atomically cancels ONLY a never-claimed queued job. The
// caller may start HTTP fallback only after allowed=true and a successful commit.
func (s *Store) FallbackWorkerJob(ctx context.Context, taskID, jobID string, waitSeconds int) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	w, err := scanWorkerJob(tx.QueryRow(ctx, `SELECT `+workerJobColumns+` FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.task_id=$1 AND j.job_id=$2 FOR UPDATE OF j`, taskID, jobID))
	if err != nil {
		return false, err
	}
	live, err := workerBusinessActive(ctx, tx, taskID)
	if err != nil {
		return false, err
	}
	now, err := workerNow(ctx, tx)
	if err != nil {
		return false, err
	}
	if !live {
		next, changed := wp.Cancel(w.Job, now)
		if changed {
			if err = saveWorkerJob(ctx, tx, next); err != nil {
				return false, err
			}
		}
		return false, tx.Commit(ctx)
	}
	next, allowed := wp.OfflineFallback(w.Job, now, waitSeconds)
	if allowed {
		if err = saveWorkerJob(ctx, tx, next); err != nil {
			return false, err
		}
	}
	return allowed, tx.Commit(ctx)
}
